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
	"time"

	"github.com/sspencer/mock/internal/ui"
	"github.com/sspencer/mock/mockhttp"
)

// Run executes the mock command line: it serves until ctx is done or a server
// fails, or runs a subcommand such as validate.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) error {
	if len(args) > 0 && args[0] == "validate" {
		return runValidate(args[1:], stdout)
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
	if len(cfg.Args) == 0 {
		if f, ok := stdin.(*os.File); ok && stdinIsTerminal(f) {
			return usageError("missing request input\nusage: mock [-p 8080] [-a 8081 | -x] [-b addr] [-u url] [-s] [-v name=value] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		}
	}
	input, err := loadInput(cfg.Args, stdin, cfg.Vars.options())
	if err != nil {
		return runError("%v", err)
	}
	var handler, adminHandler http.Handler
	var staticFS fs.FS
	var mockServer *mockhttp.Server
	var watchCloser io.Closer
	var mockListeners []net.Listener
	var plan *portPlan
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
		if err := validateMethods(input.Methods, cfg.Args); err != nil {
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
		mockServer.SetUpstream(cfg.Upstream)
		handler = mockServer
		if files := input.WatchFiles; len(files) > 0 {
			var watcher *fileWatcher
			ready := make(chan struct{})
			reload := func() {
				<-ready
				reloadMockFiles(mockServer, files, cfg.Vars.options(), plan, logger, stdout, stderr)
				if err := watcher.Reconcile(resolveWatchPaths(files, mockServer.Methods())); err != nil {
					logger.Error("failed to update dependency watches", "error", err)
				}
			}
			watcher, err = watchFiles(resolveWatchPaths(files, input.Methods), reload, logger)
			close(ready)
			if watcher != nil {
				watchCloser = watcher
			}
			if err != nil {
				closeAll()
				return runError("failed to watch request files: %v", err)
			}
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
		})
		adminListener, err = net.Listen("tcp", listenAddress(cfg.Bind, cfg.AdminPort))
		if err != nil {
			closeAll()
			return runError("admin server failed: %v", err)
		}
		servers = append(servers, newHTTPServer(adminHandler))
		listeners = append(listeners, adminListener)
	}

	if mockServer == nil {
		logger.Info("starting static HTTP server", "addr", boundAddress(cfg.Bind, mockListeners[0]), "dir", input.StaticDir)
	} else {
		for _, ln := range mockListeners {
			fmt.Fprintf(stdout, "starting mock HTTP server on %s\n", boundAddress(cfg.Bind, ln))
		}
		if adminHandler != nil {
			fmt.Fprintf(stdout, "admin UI at http://%s/\n", boundAddress(cfg.Bind, adminListener))
		}
		if adminHandler != nil && bindsAllInterfaces(cfg.Bind) {
			fmt.Fprintln(stderr, "warning: bound to all interfaces; admin UI is unauthenticated and request logs may include Authorization headers and bodies")
		}
		if cfg.Upstream != nil {
			fmt.Fprintf(stdout, "forwarding unmatched requests to %s\n", cfg.Upstream)
		}
		if watchCloser != nil {
			fmt.Fprintln(stdout, "watching request files for changes")
		}
		printMethods(stdout, input.Methods)
	}

	errCh := make(chan error, len(servers))
	for i, server := range servers {
		go func() { errCh <- server.Serve(listeners[i]) }()
	}
	select {
	case <-ctx.Done():
		logger.Info("shutting down")
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
		closeWatcher()
		_ = shutdownHTTPServers(servers, 400*time.Millisecond)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return runError("server failed: %v", err)
		}
		return nil
	}
}
