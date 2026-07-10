package main

import (
	"context"
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
	"strings"
	"syscall"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(os.Args[1:], os.Stdin, os.Stdout, logger); err != nil {
		var exitErr *exitError
		if errors.As(err, &exitErr) {
			logger.Error(exitErr.message, exitErr.attrs...)
			os.Exit(exitErr.code)
		}
		logger.Error("failed", "error", err)
		os.Exit(1)
	}
}

type exitError struct {
	code    int
	message string
	attrs   []any
}

func (e *exitError) Error() string { return e.message }

func usageError(msg string, attrs ...any) error {
	return &exitError{code: 2, message: msg, attrs: attrs}
}

func runError(msg string, attrs ...any) error {
	return &exitError{code: 1, message: msg, attrs: attrs}
}

type config struct {
	Mount    string
	Port     int
	Bind     string
	CORS     string
	CertFile string
	KeyFile  string
	OpenAPI  string
	Version  bool
	Args     []string
}

func parseConfig(args []string) (config, error) {
	flagSet := flag.NewFlagSet("mock", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var cfg config
	flagSet.StringVar(&cfg.Mount, "l", "mock", "URL path for the admin web UI")
	flagSet.IntVar(&cfg.Port, "p", 8080, "HTTP port")
	flagSet.StringVar(&cfg.Bind, "b", "", "bind address (default all interfaces)")
	flagSet.StringVar(&cfg.CORS, "cors", "", "Access-Control-Allow-Origin value (e.g. * or https://app.local)")
	flagSet.StringVar(&cfg.CertFile, "cert", "", "TLS certificate file (enables HTTPS)")
	flagSet.StringVar(&cfg.KeyFile, "key", "", "TLS private key file")
	flagSet.StringVar(&cfg.OpenAPI, "openapi", "", "OpenAPI 3 JSON/YAML file to seed stub routes")
	flagSet.BoolVar(&cfg.Version, "version", false, "print version and exit")
	if err := flagSet.Parse(args); err != nil {
		return config{}, usageError("failed to parse flags", "error", err)
	}
	cfg.Args = flagSet.Args()
	return cfg, nil
}

func run(args []string, stdin io.Reader, stdout io.Writer, logger *slog.Logger) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if cfg.Version {
		fmt.Fprintln(stdout, version)
		return nil
	}
	if cfg.CertFile != "" && cfg.KeyFile == "" || cfg.KeyFile != "" && cfg.CertFile == "" {
		return usageError("both -cert and -key are required for TLS")
	}
	if len(cfg.Args) == 0 && cfg.OpenAPI == "" {
		if f, ok := stdin.(*os.File); ok && stdinIsTerminal(f) {
			return usageError("missing request input", "usage", "mock [-l mock] [-p 8080] [-b addr] [-cors *] [-cert c -key k] [-openapi spec.yaml] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		}
	}

	input, err := loadInput(cfg.Args, stdin, cfg.OpenAPI)
	if err != nil {
		return runError("failed to load request input", "error", err)
	}

	var handler http.Handler
	var mockServer *mockhttp.Server
	var watchCloser io.Closer

	if input.StaticDir != "" {
		handler = newStaticFileHandler(input.StaticDir)
		logger.Info("starting static HTTP server", "addr", listenAddress(cfg.Bind, cfg.Port), "dir", input.StaticDir)
	} else {
		if err := validateMethods(input.Methods, cfg.Args); err != nil {
			return runError("failed to start mock server", "error", err)
		}
		printMethods(stdout, input.Methods)

		staticFS, err := staticFileSystem()
		if err != nil {
			return runError("failed to load static files", "error", err)
		}
		mockServer = mockhttp.New(input.Methods, logger)
		handler = newHandler(mockServer, cfg.Mount, staticFS)
		logger.Info("starting mock HTTP server",
			"addr", listenAddress(cfg.Bind, cfg.Port),
			"methods", len(input.Methods),
			"ui", normalizeMountPath(cfg.Mount),
		)

		if files := input.WatchFiles; len(files) > 0 {
			reload := func() {
				reloadMockFiles(mockServer, files, input.OpenAPI, logger, stdout)
			}
			paths := resolveWatchPaths(files, restclient.FileDependencies(input.Methods))
			watchCloser, err = watchFiles(paths, reload, logger)
			if err != nil {
				return runError("failed to watch request files", "error", err)
			}
			logger.Info("watching request files for changes", "files", paths)
		}
	}

	if cfg.CORS != "" {
		handler = withCORS(handler, cfg.CORS)
	}

	server := &http.Server{
		Addr:              listenAddress(cfg.Bind, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout must stay 0 so SSE streams and long $delay routes are not cut off.
		IdleTimeout: 60 * time.Second,
	}

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

	select {
	case sig := <-sigCh:
		logger.Info("shutting down", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			if watchCloser != nil {
				_ = watchCloser.Close()
			}
			return runError("server shutdown failed", "error", err)
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
			return runError("server failed", "error", err)
		}
		return nil
	}
}

func listenAddress(bind string, port int) string {
	return net.JoinHostPort(bind, fmt.Sprintf("%d", port))
}

type inputSource struct {
	Methods    []restclient.Method
	StaticDir  string
	WatchFiles []string
	OpenAPI    string
}

func loadInput(args []string, stdin io.Reader, openAPI string) (inputSource, error) {
	if openAPI != "" && len(args) == 0 {
		methods, err := restclient.LoadOpenAPI(openAPI)
		if err != nil {
			return inputSource{}, err
		}
		return inputSource{Methods: methods, OpenAPI: openAPI}, nil
	}

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

	var methods []restclient.Method
	if openAPI != "" {
		openAPIMethods, err := restclient.LoadOpenAPI(openAPI)
		if err != nil {
			return inputSource{}, err
		}
		methods = append(methods, openAPIMethods...)
	}
	fileMethods, err := loadMethods(args, stdin)
	if err != nil {
		return inputSource{}, err
	}
	methods = append(methods, fileMethods...)
	src := inputSource{Methods: methods, OpenAPI: openAPI}
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
	mux.Handle(mountRoot, http.StripPrefix(mountRoot, http.FileServer(http.FS(staticFS))))
	mux.Handle("/", mockServer)
	return mux
}

func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Last-Event-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reloadMockFiles(mockServer *mockhttp.Server, files []string, openAPI string, logger *slog.Logger, out io.Writer) {
	load := func() ([]restclient.Method, error) {
		var methods []restclient.Method
		if openAPI != "" {
			openAPIMethods, err := restclient.LoadOpenAPI(openAPI)
			if err != nil {
				return nil, err
			}
			methods = append(methods, openAPIMethods...)
		}
		if len(files) > 0 {
			fileMethods, err := restclient.Load(files)
			if err != nil {
				return nil, err
			}
			methods = append(methods, fileMethods...)
		}
		return methods, nil
	}

	methods, err := load()
	if err != nil {
		// Editors often emit Write before the full file is flushed; retry once.
		time.Sleep(50 * time.Millisecond)
		methods, err = load()
	}
	if err != nil {
		logger.Error("failed to reload request files", "error", err)
		return
	}
	if err := validateMethods(methods, files); err != nil {
		logger.Error("failed to reload request files", "error", err)
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

// absPath is used by tests.
func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
