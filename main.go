package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, logger); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		var exitErr *exitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}
		os.Exit(1)
	}
}

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func usageError(format string, args ...any) error {
	return &exitError{code: 2, msg: fmt.Sprintf(format, args...)}
}

func runError(format string, args ...any) error {
	return &exitError{code: 1, msg: fmt.Sprintf(format, args...)}
}

const defaultAdminPort = 8081

type config struct {
	Bind, CORS, CertFile, KeyFile string
	Port, AdminPort               int
	Version, NoAdmin              bool
	Args                          []string
}

func parseConfig(args []string) (config, error) {
	flagSet := flag.NewFlagSet("mock", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var cfg config
	portDefault, envPortSet, err := portFromEnv("MOCK_PORT", 8080)
	if err != nil {
		return config{}, err
	}
	adminDefault, envAdminSet, err := portFromEnv("MOCK_UI_PORT", defaultAdminPort)
	if err != nil {
		return config{}, err
	}
	flagSet.IntVar(&cfg.Port, "p", portDefault, "HTTP port")
	flagSet.IntVar(&cfg.AdminPort, "a", adminDefault, "admin UI port (0 picks a random port; default 8081, or random when -p or MOCK_PORT is set; MOCK_UI_PORT sets the default)")
	flagSet.BoolVar(&cfg.NoAdmin, "x", false, "do not start the admin UI")
	flagSet.StringVar(&cfg.Bind, "b", "127.0.0.1", "bind address (default 127.0.0.1; use 0.0.0.0 for all interfaces)")
	flagSet.StringVar(&cfg.CORS, "cors", "", "Access-Control-Allow-Origin value (e.g. * or https://app.local); only real preflights short-circuit; the admin UI is not affected")
	flagSet.StringVar(&cfg.CertFile, "cert", "", "TLS certificate file (enables HTTPS)")
	flagSet.StringVar(&cfg.KeyFile, "key", "", "TLS private key file")
	flagSet.BoolVar(&cfg.Version, "version", false, "print version and exit")
	if err := flagSet.Parse(args); err != nil {
		return config{}, usageError("failed to parse flags: %v", err)
	}
	cfg.Args = flagSet.Args()
	if cfg.Port < 0 || cfg.Port > 65535 {
		return config{}, usageError("port must be between 0 and 65535")
	}
	if cfg.AdminPort < 0 || cfg.AdminPort > 65535 {
		return config{}, usageError("admin port must be between 0 and 65535")
	}
	var adminSet, portSet bool
	flagSet.Visit(func(f *flag.Flag) {
		adminSet = adminSet || f.Name == "a"
		portSet = portSet || f.Name == "p"
	})
	if cfg.NoAdmin && adminSet {
		return config{}, usageError("-a and -x cannot be used together")
	}
	if !adminSet && !envAdminSet && (portSet || envPortSet) {
		cfg.AdminPort = 0
	}
	if !cfg.NoAdmin && cfg.AdminPort != 0 && cfg.AdminPort == cfg.Port {
		return config{}, usageError("admin port %d is the same as the mock port; change -a or MOCK_UI_PORT (or -p or MOCK_PORT)", cfg.AdminPort)
	}
	return cfg, nil
}

// portFromEnv returns the port named by the environment variable, or def when
// it is unset, and whether the variable supplied it.
func portFromEnv(name string, def int) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, false, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, usageError("invalid %s %q: must be an integer", name, raw)
	}
	return port, true, nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if cfg.Version {
		fmt.Fprintln(stdout, currentVersion())
		return nil
	}
	if cfg.CertFile != "" && cfg.KeyFile == "" || cfg.KeyFile != "" && cfg.CertFile == "" {
		return usageError("both -cert and -key are required for TLS")
	}
	if len(cfg.Args) == 0 {
		if f, ok := stdin.(*os.File); ok && stdinIsTerminal(f) {
			return usageError("missing request input\nusage: mock [-p 8080] [-a 8081 | -x] [-b addr] [-cors *] [-cert c -key k] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		}
	}
	input, err := loadInput(cfg.Args, stdin)
	if err != nil {
		return runError("%v", err)
	}
	var handler, adminHandler http.Handler
	var mockServer *mockhttp.Server
	var watchCloser io.Closer
	closeWatcher := func() {
		if watchCloser != nil {
			_ = watchCloser.Close()
		}
	}
	if input.StaticDir != "" {
		handler = newStaticFileHandler(input.StaticDir)
	} else {
		if err := validateMethods(input.Methods, cfg.Args); err != nil {
			return runError("%v", err)
		}
		staticFS, err := staticFileSystem()
		if err != nil {
			return runError("failed to load static files: %v", err)
		}
		mockServer = mockhttp.New(input.Methods, logger)
		handler = mockServer
		if !cfg.NoAdmin {
			adminHandler = newAdminHandler(mockServer, staticFS)
		}
		if files := input.WatchFiles; len(files) > 0 {
			var watcher *fileWatcher
			ready := make(chan struct{})
			reload := func() {
				<-ready
				reloadMockFiles(mockServer, files, logger, stdout, stderr)
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
				return runError("failed to watch request files: %v", err)
			}
		}
	}
	if cfg.CORS != "" {
		handler = withCORS(handler, cfg.CORS)
	}

	mockListener, err := net.Listen("tcp", listenAddress(cfg.Bind, cfg.Port))
	if err != nil {
		closeWatcher()
		return runError("server failed: %v", err)
	}
	servers := []*http.Server{newHTTPServer(handler)}
	listeners := []net.Listener{mockListener}
	if adminHandler != nil {
		adminListener, err := net.Listen("tcp", listenAddress(cfg.Bind, cfg.AdminPort))
		if err != nil {
			_ = mockListener.Close()
			closeWatcher()
			return runError("admin server failed: %v", err)
		}
		servers = append(servers, newHTTPServer(adminHandler))
		listeners = append(listeners, adminListener)
	}

	if mockServer == nil {
		logger.Info("starting static HTTP server", "addr", boundAddress(cfg.Bind, mockListener), "dir", input.StaticDir)
	} else {
		fmt.Fprintf(stdout, "starting mock HTTP server on %s\n", boundAddress(cfg.Bind, mockListener))
		if adminHandler != nil {
			fmt.Fprintf(stdout, "admin UI at %s://%s/\n", listenScheme(cfg.CertFile, cfg.KeyFile), boundAddress(cfg.Bind, listeners[1]))
		}
		if adminHandler != nil && bindsAllInterfaces(cfg.Bind) {
			fmt.Fprintln(stderr, "warning: bound to all interfaces; admin UI is unauthenticated and request logs may include Authorization headers and bodies")
		}
		if watchCloser != nil {
			fmt.Fprintln(stdout, "watching request files for changes")
		}
		printMethods(stdout, input.Methods)
	}

	errCh := make(chan error, len(servers))
	for i, server := range servers {
		go func() {
			if cfg.CertFile != "" {
				logger.Info("TLS enabled", "cert", cfg.CertFile)
				errCh <- server.ServeTLS(listeners[i], cfg.CertFile, cfg.KeyFile)
			} else {
				errCh <- server.Serve(listeners[i])
			}
		}()
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case sig := <-sigCh:
		logger.Info("shutting down", "signal", sig.String())
		if err := shutdownHTTPServers(servers, 400*time.Millisecond); err != nil {
			closeWatcher()
			return runError("server shutdown failed: %v", err)
		}
		closeWatcher()
		for range servers {
			<-errCh
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

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, WriteTimeout: 30 * time.Second}
}

func shutdownHTTPServers(servers []*http.Server, grace time.Duration) error {
	var errs []error
	for _, server := range servers {
		errs = append(errs, shutdownHTTPServer(server, grace))
	}
	return errors.Join(errs...)
}

func shutdownHTTPServer(server *http.Server, grace time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := server.Shutdown(ctx)
	closeErr := server.Close()
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.DeadlineExceeded) {
		if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			return closeErr
		}
		return nil
	}
	return err
}

func listenAddress(bind string, port int) string {
	return net.JoinHostPort(bind, fmt.Sprintf("%d", port))
}

// boundAddress reports the bind host with the port the listener actually got,
// which differs from the requested port when it was 0.
func boundAddress(bind string, ln net.Listener) string {
	return listenAddress(bind, ln.Addr().(*net.TCPAddr).Port)
}

func listenScheme(certFile, keyFile string) string {
	if certFile != "" && keyFile != "" {
		return "https"
	}
	return "http"
}

func bindsAllInterfaces(bind string) bool {
	host := strings.Trim(bind, "[]")
	return host == "" || host == "0.0.0.0" || host == "::" || host == "*"
}

type inputSource struct {
	Methods    []restclient.Method
	StaticDir  string
	WatchFiles []string
}

func loadInput(args []string, stdin io.Reader) (inputSource, error) {
	if len(args) == 1 {
		info, err := os.Stat(args[0])
		if err != nil {
			return inputSource{}, err
		}
		if info.IsDir() {
			return inputSource{StaticDir: args[0]}, nil
		}
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return inputSource{}, err
		}
		if info.IsDir() {
			return inputSource{}, fmt.Errorf("cannot mix static directory %q with other request inputs", arg)
		}
	}
	methods, err := loadMethods(args, stdin)
	if err != nil {
		return inputSource{}, err
	}
	src := inputSource{Methods: methods}
	if len(args) > 0 {
		src.WatchFiles = append([]string(nil), args...)
	}
	return src, nil
}

func loadMethods(args []string, stdin io.Reader) ([]restclient.Method, error) {
	if len(args) > 0 {
		return restclient.Load(args)
	}
	return restclient.Parse("<stdin>", stdin)
}

func validateMethods(methods []restclient.Method, args []string) error {
	if len(methods) > 0 {
		return nil
	}
	source := "stdin"
	if len(args) > 0 {
		source = strings.Join(args, ", ")
	}
	return fmt.Errorf("no mock requests found in %s; add at least one request section starting with ### followed by an HTTP request line", source)
}

func stdinIsTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newStaticFileHandler(dir string) http.Handler {
	return http.FileServer(http.Dir(dir))
}

// newAdminHandler serves the request console and its API at the root of the
// admin listener.
func newAdminHandler(mockServer *mockhttp.Server, staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", mockServer.ServeEvents)
	mux.HandleFunc("/clear", mockServer.ServeClear)
	mux.HandleFunc("/routes", mockServer.ServeRoutes)
	mux.HandleFunc("/state", mockServer.ServeState)
	mux.HandleFunc("/reset", mockServer.ServeReset)
	mux.Handle("/", uiFileServer(staticFS, currentVersion()))
	return mux
}

func uiFileServer(staticFS fs.FS, version string) http.Handler {
	files := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			files.ServeHTTP(w, r)
			return
		}
		data, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			http.Error(w, "index.html missing", http.StatusInternalServerError)
			return
		}
		payload, err := json.Marshal(map[string]string{"version": version})
		if err != nil {
			http.Error(w, "failed to encode UI config", http.StatusInternalServerError)
			return
		}
		data = bytes.Replace(data, []byte(`{"version":"dev"}`), payload, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reloadMockFiles(mockServer *mockhttp.Server, files []string, logger *slog.Logger, out, errOut io.Writer) {
	mockServer.BeginReload()
	load := func() ([]restclient.Method, error) {
		if len(files) == 0 {
			return nil, nil
		}
		return restclient.Load(files)
	}
	methods, err := load()
	if err != nil {
		time.Sleep(50 * time.Millisecond)
		methods, err = load()
	}
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	if err := validateMethods(methods, files); err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	mockServer.SetMethods(methods)
	logger.Info("reloaded request files", "files", files, "methods", len(methods))
	printMethods(out, methods)
}

func printMethods(w io.Writer, methods []restclient.Method) {
	fmt.Fprintln(w, "Available mock methods:")
	for _, method := range methods {
		target := method.Path
		if query := method.Query.Encode(); query != "" {
			target += "?" + query
		}
		fmt.Fprintf(w, "  %-7s %-30s %s\n", method.Method, target, method.Name)
	}
}

func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
