// Package cli implements the mock command: flag parsing, port planning,
// serving, hot reload, and the validate subcommand.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sspencer/mock/internal/ui"
	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// Run executes the mock command line: it serves until ctx is done or a server
// fails, or runs a subcommand such as validate.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) error {
	if len(args) > 0 && args[0] == "validate" {
		return runValidate(args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "check" {
		return runCheck(ctx, args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "import" {
		return runImport(args[1:], stdin, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "init" {
		return runInit(args[1:], stdout)
	}
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if cfg.Help != "" {
		fmt.Fprint(stdout, cfg.Help)
		return nil
	}
	if cfg.Version {
		fmt.Fprintln(stdout, currentVersion())
		return nil
	}
	// With JSON logs every stdout line is a JSON object; routes listed on
	// reload become records too (a nil routesOut).
	jsonLogs := cfg.LogFormat == "json"
	routesOut := stdout
	if jsonLogs {
		logger = slog.New(slog.NewJSONHandler(stdout, nil))
		routesOut = nil
	}
	if len(cfg.Args) == 0 && len(cfg.Dirs) == 0 {
		if f, ok := stdin.(*os.File); ok && stdinIsTerminal(f) {
			return usageError("missing request input\nusage: mock [-p 8080] [-a 8081 | -x] [-b addr] [-u url] [-s] [-v name=value] [-e env] [-d dir] [-i 1s] [-w delay] [-f fault] [-l json] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		}
	}
	opts, err := cfg.Env.options(cfg.Vars)
	if err != nil {
		return runError("%v", err)
	}
	opts.Headers = http.Header(cfg.Headers)
	input, err := loadInput(cfg.Args, cfg.Dirs, stdin, opts)
	if err != nil {
		return runError("%v", err)
	}
	var handler, adminHandler http.Handler
	var staticFS fs.FS
	var mockServer *mockhttp.Server
	var watchCloser io.Closer
	var mockListeners []net.Listener
	var plan *portPlan
	var reloads *reloader
	closeAll := func() {
		if watchCloser != nil {
			_ = watchCloser.Close()
		}
		for _, ln := range mockListeners {
			_ = ln.Close()
		}
	}
	closeWatcher := func() {
		if watchCloser != nil {
			_ = watchCloser.Close()
		}
	}
	if input.StaticDir != "" && cfg.Upstream != nil {
		return usageError("-u (--upstream) needs request files; a directory argument serves static files only")
	}
	if cfg.Poll > 0 && (input.StaticDir != "" || !input.watched()) {
		return usageError("-i (--poll) needs request files; stdin and a static directory cannot be reloaded")
	}
	if input.StaticDir != "" && cfg.Defaults != (mockhttp.RouteDefaults{}) {
		return usageError("-w (--delay) and -f (--fault) need request files; a directory argument serves static files only")
	}
	if input.StaticDir != "" && cfg.Strict {
		return usageError("-s (--strict) needs request files; a directory argument serves static files only")
	}
	if input.StaticDir != "" {
		handler = newStaticFileHandler(input.StaticDir)
		ln, err := net.Listen("tcp", listenAddress(cfg.Bind, cfg.Port))
		if err != nil {
			return runError("server failed: %v", err)
		}
		mockListeners = []net.Listener{ln}
	} else {
		if err := validateMethods(input.Methods, input.describe()); err != nil {
			return runError("%v", err)
		}
		staticFS, err = ui.Assets()
		if err != nil {
			return runError("failed to load static files: %v", err)
		}
		mockListeners, plan, err = listenMock(cfg, input.Methods)
		if err != nil {
			return runError("%v", err)
		}
		methods, err := plan.resolve(input.Methods)
		if err != nil {
			closeAll()
			return runError("%v", err)
		}
		input.Methods = methods
		mockServer = mockhttp.New(input.Methods, logger)
		mockServer.SetRouteDefaults(cfg.Defaults)
		mockServer.SetUpstream(cfg.Upstream)
		mockServer.SetRedact(cfg.Redact)
		if cfg.Record != "" {
			rec, err := newRecorder(cfg.Record, cfg.Port, logger)
			if err != nil {
				closeAll()
				return runError("%v", err)
			}
			mockServer.SetRequestHook(rec.record)
		}
		handler = mockServer
		if input.watched() {
			reloads = &reloader{server: mockServer, input: input, opts: opts, plan: plan, logger: logger, out: routesOut, err: stderr}
			// Holding the lock until the watcher is assigned keeps an early
			// file event from reloading before the reloader is complete.
			reloads.mu.Lock()
			paths := reloads.watchPaths()
			reloads.snapshot = snapshotFiles(paths)
			watcher, err := watchFiles(paths, reloads.reload, logger, input.Dirs...)
			reloads.watcher = watcher
			reloads.mu.Unlock()
			if watcher != nil {
				watchCloser = watcher
			}
			if err != nil {
				closeAll()
				return runError("failed to watch request files: %v", err)
			}
			mockServer.SetReloader(reloads.reload)
		}
	}
	var servers []*http.Server
	listeners := append([]net.Listener(nil), mockListeners...)
	for range mockListeners {
		servers = append(servers, newHTTPServer(handler))
	}
	var adminListener net.Listener
	if mockServer != nil && !cfg.NoAdmin {
		// Built after the mock listeners bind so the UI knows the real mock port.
		adminHandler = ui.NewHandler(mockServer, staticFS, ui.Config{
			Version:   currentVersion(),
			MockPort:  plan.uiPort(),
			MultiPort: len(plan.ports) > 1,
			Redact:    cfg.Redact,
		})
		adminListener, err = net.Listen("tcp", listenAddress(cfg.Bind, cfg.AdminPort))
		if err != nil {
			closeAll()
			return runError("admin server failed: %v", err)
		}
		adminHandler = ui.RequireToken(adminHandler, cfg.UIToken)
		servers = append(servers, newHTTPServer(adminHandler))
		listeners = append(listeners, adminListener)
	}

	if mockServer == nil {
		logger.Info("starting static HTTP server", "addr", boundAddress(cfg.Bind, mockListeners[0]), "dir", input.StaticDir)
	} else {
		if adminHandler != nil && bindsAllInterfaces(cfg.Bind) && cfg.UIToken == "" {
			fmt.Fprintln(stderr, "warning: bound to all interfaces; admin UI is unauthenticated and request logs may include Authorization headers and bodies; set -t (--ui-token) to require a token")
		}
		if jsonLogs {
			announceJSON(logger, cfg, mockListeners, adminListener, watchCloser != nil, input.Methods)
		} else {
			for _, ln := range mockListeners {
				fmt.Fprintf(stdout, "starting mock HTTP server on %s\n", boundAddress(cfg.Bind, ln))
			}
			if adminHandler != nil {
				fmt.Fprintf(stdout, "admin UI at http://%s/%s\n", boundAddress(cfg.Bind, adminListener), tokenNote(cfg.UIToken))
			}
			if cfg.Upstream != nil {
				fmt.Fprintf(stdout, "forwarding unmatched requests to %s\n", cfg.Upstream)
			}
			if cfg.Record != "" {
				fmt.Fprintf(stdout, "recording forwarded requests to %s\n", cfg.Record)
			}
			if delay := cfg.Defaults.DelayString(); delay != "" {
				fmt.Fprintf(stdout, "delaying routes without @delay by %s\n", delay)
			}
			if fault := cfg.Defaults.FaultString(); fault != "" {
				fmt.Fprintf(stdout, "injecting fault %s into routes without @fault\n", fault)
			}
			if watchCloser != nil && cfg.Poll > 0 {
				fmt.Fprintf(stdout, "watching request files for changes (and polling every %s)\n", cfg.Poll)
			} else if watchCloser != nil {
				fmt.Fprintln(stdout, "watching request files for changes")
			}
			printMethods(stdout, input.Methods)
		}
	}

	// stopTriggers stops polling and SIGHUP reloads and waits for one in
	// progress, so none runs after the watcher closes or Run returns.
	stopTriggers := func() {}
	if reloads != nil {
		runCtx, stopReloads := context.WithCancel(ctx)
		var triggers sync.WaitGroup
		stopTriggers = func() {
			stopReloads()
			triggers.Wait()
		}
		defer stopTriggers()
		if cfg.Poll > 0 {
			triggers.Go(func() { reloads.poll(runCtx, cfg.Poll) })
		}
		triggers.Go(func() { reloadOnHangup(runCtx, reloads.reload, logger) })
	}

	if mockServer != nil {
		defer mockServer.Close() // Cancel @callback requests still waiting.
	}
	errCh := make(chan error, len(servers))
	for i, server := range servers {
		go func() { errCh <- server.Serve(listeners[i]) }()
	}
	select {
	case <-ctx.Done():
		logger.Info("shutting down")
		stopTriggers()
		if err := shutdownHTTPServers(servers, 400*time.Millisecond); err != nil {
			closeWatcher()
			return runError("server shutdown failed: %v", err)
		}
		closeWatcher()
		for range servers {
			<-errCh
		}
		if cfg.Strict && mockServer != nil {
			return strictReport(stderr, mockServer.Coverage())
		}
		return nil
	case err := <-errCh:
		stopTriggers()
		closeWatcher()
		_ = shutdownHTTPServers(servers, 400*time.Millisecond)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return runError("server failed: %v", err)
		}
		return nil
	}
}

// tokenNote marks the admin URL in the banner when it needs a token. The
// token itself is not printed, so it stays out of logs.
func tokenNote(token string) string {
	if token == "" {
		return ""
	}
	return " (token required: open /?token=TOKEN)"
}

// announceJSON writes the startup banner as log records for -l json.
func announceJSON(logger *slog.Logger, cfg config, mockListeners []net.Listener, adminListener net.Listener, watching bool, methods []restclient.Method) {
	for _, ln := range mockListeners {
		logger.Info("starting mock HTTP server", "addr", boundAddress(cfg.Bind, ln))
	}
	if adminListener != nil {
		logger.Info("admin UI", "url", "http://"+boundAddress(cfg.Bind, adminListener)+"/", "token", cfg.UIToken != "")
	}
	if cfg.Upstream != nil {
		logger.Info("forwarding unmatched requests", "upstream", cfg.Upstream.String())
	}
	if cfg.Record != "" {
		logger.Info("recording forwarded requests", "file", cfg.Record)
	}
	if delay := cfg.Defaults.DelayString(); delay != "" {
		logger.Info("delaying routes without @delay", "delay", delay)
	}
	if fault := cfg.Defaults.FaultString(); fault != "" {
		logger.Info("injecting fault into routes without @fault", "fault", fault)
	}
	if watching && cfg.Poll > 0 {
		logger.Info("watching request files for changes", "poll", cfg.Poll.String())
	} else if watching {
		logger.Info("watching request files for changes")
	}
	showMethods(nil, logger, methods)
}
