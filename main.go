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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, logger)
	stop()
	if err != nil {
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
	Bind             string
	Port, AdminPort  int
	Version, NoAdmin bool
	Args             []string
	// Help holds the usage text when -h or --help was requested.
	Help string
}

const defaultBind = "127.0.0.1"

// usageText is printed by -h. Every flag has a short and a long name; Go's
// flag package accepts either with one or two dashes.
const usageText = `usage:
  mock [flags] <file.http> [file.http...]
  mock [flags] <directory>
  cat file.http | mock
  mock validate [-strict] [-json] <file.http> [file.http...]

flags:
  -p, --port PORT      default HTTP port, for routes whose request target or
                       @port gives no port (default 8080, or MOCK_PORT)
  -a, --ui-port PORT   admin UI port; 0 picks a random port (default
                       MOCK_UI_PORT, else 8081, or random if -p or MOCK_PORT
                       is set)
  -x, --no-ui          do not start the admin UI
  -b, --bind ADDR      bind address; use 0.0.0.0 for all interfaces
                       (default 127.0.0.1, or MOCK_BIND)
      --version        print version and exit
  -h, --help           print this help and exit
`

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
	bindDefault := defaultBind
	if raw := strings.TrimSpace(os.Getenv("MOCK_BIND")); raw != "" {
		bindDefault = raw
	}
	// Each long name shares its short name's variable. The help text is
	// hand-written in usageText, so the per-flag usage strings stay empty.
	for _, name := range []string{"p", "port"} {
		flagSet.IntVar(&cfg.Port, name, portDefault, "")
	}
	for _, name := range []string{"a", "ui-port"} {
		flagSet.IntVar(&cfg.AdminPort, name, adminDefault, "")
	}
	for _, name := range []string{"x", "no-ui"} {
		flagSet.BoolVar(&cfg.NoAdmin, name, false, "")
	}
	for _, name := range []string{"b", "bind"} {
		flagSet.StringVar(&cfg.Bind, name, bindDefault, "")
	}
	flagSet.BoolVar(&cfg.Version, "version", false, "")
	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return config{Help: usageText}, nil
		}
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
		adminSet = adminSet || f.Name == "a" || f.Name == "ui-port"
		portSet = portSet || f.Name == "p" || f.Name == "port"
	})
	if cfg.NoAdmin && adminSet {
		return config{}, usageError("-a (--ui-port) and -x (--no-ui) cannot be used together")
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

// run serves until ctx is done or a server fails.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) error {
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
			return usageError("missing request input\nusage: mock [-p 8080] [-a 8081 | -x] [-b addr] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		}
	}
	input, err := loadInput(cfg.Args, stdin)
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
		staticFS, err = staticFileSystem()
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
		handler = mockServer
		if files := input.WatchFiles; len(files) > 0 {
			var watcher *fileWatcher
			ready := make(chan struct{})
			reload := func() {
				<-ready
				reloadMockFiles(mockServer, files, plan, logger, stdout, stderr)
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
		adminHandler = newAdminHandler(mockServer, staticFS, uiConfig{
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
func newAdminHandler(mockServer *mockhttp.Server, staticFS fs.FS, ui uiConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", mockServer.ServeEvents)
	mux.HandleFunc("/clear", mockServer.ServeClear)
	mux.HandleFunc("/routes", mockServer.ServeRoutes)
	mux.HandleFunc("/state", mockServer.ServeState)
	mux.HandleFunc("/reset", mockServer.ServeReset)
	mux.Handle("/", uiFileServer(staticFS, ui))
	return mux
}

// uiConfig is the bootstrap data injected into the UI page. MockPort lets the
// UI build sample requests for routes on the default mock port, which differs
// from the UI port. MultiPort reports more than one mock listener, so the UI
// shows each request's host and port and each route's port.
type uiConfig struct {
	Version   string `json:"version"`
	MockPort  int    `json:"mockPort"`
	MultiPort bool   `json:"multiPort,omitempty"`
}

func uiFileServer(staticFS fs.FS, ui uiConfig) http.Handler {
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
		payload, err := json.Marshal(ui)
		if err != nil {
			http.Error(w, "failed to encode UI config", http.StatusInternalServerError)
			return
		}
		data = bytes.Replace(data, []byte(`{"version":"dev"}`), payload, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

func reloadMockFiles(mockServer *mockhttp.Server, files []string, plan *portPlan, logger *slog.Logger, out, errOut io.Writer) {
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
	if methods, err = plan.resolve(methods); err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	mockServer.SetMethods(methods)
	logger.Info("reloaded request files", "files", files, "methods", len(methods))
	printMethods(out, methods)
}

// printMethods lists the routes, grouped by port when they are served on
// more than one.
func printMethods(w io.Writer, methods []restclient.Method) {
	var ports []int
	byPort := make(map[int][]restclient.Method)
	for _, method := range methods {
		if _, ok := byPort[method.Port]; !ok {
			ports = append(ports, method.Port)
		}
		byPort[method.Port] = append(byPort[method.Port], method)
	}
	slices.Sort(ports)
	if len(ports) <= 1 {
		fmt.Fprintln(w, "Available mock methods:")
		printMethodList(w, methods)
		return
	}
	for _, port := range ports {
		fmt.Fprintf(w, "Available mock methods on port %d:\n", port)
		printMethodList(w, byPort[port])
	}
}

func printMethodList(w io.Writer, methods []restclient.Method) {
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
