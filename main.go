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

type config struct {
	Mount, Bind, CORS, CertFile, KeyFile string
	Port                                 int
	Version                              bool
	Args                                 []string
}

func parseConfig(args []string) (config, error) {
	flagSet := flag.NewFlagSet("mock", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var cfg config
	portDefault, err := portFromEnv()
	if err != nil {
		return config{}, err
	}
	flagSet.StringVar(&cfg.Mount, "l", "mock", "URL path for the admin web UI")
	flagSet.IntVar(&cfg.Port, "p", portDefault, "HTTP port")
	flagSet.StringVar(&cfg.Bind, "b", "127.0.0.1", "bind address (default 127.0.0.1; use 0.0.0.0 for all interfaces)")
	flagSet.StringVar(&cfg.CORS, "cors", "", "Access-Control-Allow-Origin value (e.g. * or https://app.local); only real preflights short-circuit; * exposes SSE to any origin")
	flagSet.StringVar(&cfg.CertFile, "cert", "", "TLS certificate file (enables HTTPS)")
	flagSet.StringVar(&cfg.KeyFile, "key", "", "TLS private key file")
	flagSet.BoolVar(&cfg.Version, "version", false, "print version and exit")
	if err := flagSet.Parse(args); err != nil {
		return config{}, usageError("failed to parse flags: %v", err)
	}
	cfg.Args = flagSet.Args()
	return cfg, nil
}

func portFromEnv() (int, error) {
	raw := strings.TrimSpace(os.Getenv("MOCK_PORT"))
	if raw == "" {
		return 8080, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, usageError("invalid MOCK_PORT %q: must be an integer", raw)
	}
	return port, nil
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
			return usageError("missing request input\nusage: mock [-l mock] [-p 8080] [-b addr] [-cors *] [-cert c -key k] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		}
	}
	input, err := loadInput(cfg.Args, stdin)
	if err != nil {
		return runError("%v", err)
	}
	var handler http.Handler
	var mockServer *mockhttp.Server
	var watchCloser io.Closer
	if input.StaticDir != "" {
		handler = newStaticFileHandler(input.StaticDir)
		logger.Info("starting static HTTP server", "addr", listenAddress(cfg.Bind, cfg.Port), "dir", input.StaticDir)
	} else {
		if err := validateMethods(input.Methods, cfg.Args); err != nil {
			return runError("%v", err)
		}
		staticFS, err := staticFileSystem()
		if err != nil {
			return runError("failed to load static files: %v", err)
		}
		mockServer = mockhttp.New(input.Methods, logger)
		handler = newHandler(mockServer, cfg.Mount, staticFS)
		if files := input.WatchFiles; len(files) > 0 {
			reload := func() {
				reloadMockFiles(mockServer, files, logger, stdout, stderr)
			}
			paths := resolveWatchPaths(files, restclient.FileDependencies(input.Methods))
			watchCloser, err = watchFiles(paths, reload, logger)
			if err != nil {
				return runError("failed to watch request files: %v", err)
			}
		}
		addr := listenAddress(cfg.Bind, cfg.Port)
		fmt.Fprintf(stdout, "starting mock HTTP server on %s\n", addr)
		fmt.Fprintf(stdout, "admin UI at %s://%s%s/\n", listenScheme(cfg.CertFile, cfg.KeyFile), addr, normalizeMountPath(cfg.Mount))
		if bindsAllInterfaces(cfg.Bind) {
			fmt.Fprintln(stderr, "warning: bound to all interfaces; admin UI is unauthenticated and request logs may include Authorization headers and bodies")
		}
		if watchCloser != nil {
			fmt.Fprintln(stdout, "watching request files for changes")
		}
		printMethods(stdout, input.Methods)
	}
	if cfg.CORS != "" {
		handler = withCORS(handler, cfg.CORS)
	}
	server := &http.Server{Addr: listenAddress(cfg.Bind, cfg.Port), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		var serveErr error
		if cfg.CertFile != "" {
			logger.Info("TLS enabled", "cert", cfg.CertFile)
			serveErr = server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else {
			serveErr = server.ListenAndServe()
		}
		errCh <- serveErr
	}()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case sig := <-sigCh:
		logger.Info("shutting down", "signal", sig.String())
		if err := shutdownHTTPServer(server, 400*time.Millisecond); err != nil {
			if watchCloser != nil {
				_ = watchCloser.Close()
			}
			return runError("server shutdown failed: %v", err)
		}
		if watchCloser != nil {
			_ = watchCloser.Close()
		}
		<-errCh
		return nil
	case err := <-errCh:
		if watchCloser != nil {
			_ = watchCloser.Close()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return runError("server failed: %v", err)
		}
		return nil
	}
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

func newHandler(mockServer *mockhttp.Server, mount string, staticFS fs.FS) http.Handler {
	mountPath := normalizeMountPath(mount)
	mountRoot := mountPath + "/"
	mux := http.NewServeMux()
	mux.HandleFunc(mountPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mountRoot, http.StatusMovedPermanently)
	})
	mux.HandleFunc(mountRoot+"events", mockServer.ServeEvents)
	mux.HandleFunc(mountRoot+"clear", mockServer.ServeClear)
	mux.HandleFunc(mountRoot+"routes", mockServer.ServeRoutes)
	mux.Handle(mountRoot, http.StripPrefix(mountRoot, uiFileServer(staticFS, currentVersion())))
	mux.Handle("/", mockServer)
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
		return
	}
	if err := validateMethods(methods, files); err != nil {
		fmt.Fprintln(errOut, err.Error())
		return
	}
	mockServer.SetMethods(methods)
	logger.Info("reloaded request files", "files", files, "methods", len(methods))
	printMethods(out, methods)
}

func normalizeMountPath(mount string) string {
	mount = strings.TrimSpace(mount)
	mount = "/" + strings.Trim(mount, "/")
	if mount == "/" {
		return "/mock"
	}
	return mount
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
