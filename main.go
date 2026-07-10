package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"mock/mockhttp"
	"mock/restclient"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	flagSet := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	mount := flagSet.String("l", "mock", "URL path for the admin web UI")
	port := flagSet.Int("p", 8080, "HTTP port")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		logger.Error("failed to parse flags", "error", err)
		os.Exit(2)
	}
	if flagSet.NArg() == 0 && stdinIsTerminal(os.Stdin) {
		logger.Error("missing request input", "usage", "mock [-l mock] [-p 8080] <file.http> [file.http...] | mock [-p 8080] <directory> | cat file.http | mock")
		os.Exit(2)
	}

	input, err := loadInput(flagSet.Args(), os.Stdin)
	if err != nil {
		logger.Error("failed to load request input", "error", err)
		os.Exit(1)
	}

	var handler http.Handler
	if input.StaticDir != "" {
		handler = newStaticFileHandler(input.StaticDir)
		logger.Info("starting static HTTP server", "addr", listenAddress(*port), "dir", input.StaticDir)
	} else {
		if err := validateMethods(input.Methods, flagSet.Args()); err != nil {
			logger.Error("failed to start mock server", "error", err)
			os.Exit(1)
		}
		printMethods(os.Stdout, input.Methods)

		staticFS, err := staticFileSystem()
		if err != nil {
			logger.Error("failed to load static files", "error", err)
			os.Exit(1)
		}
		mockServer := mockhttp.New(input.Methods, logger)
		handler = newHandler(mockServer, *mount, staticFS)
		logger.Info("starting mock HTTP server", "addr", listenAddress(*port), "methods", len(input.Methods), "ui", normalizeMountPath(*mount))

		if files := flagSet.Args(); len(files) > 0 {
			closer, err := watchFiles(files, func() {
				reloadMockFiles(mockServer, files, logger, os.Stdout)
			}, logger)
			if err != nil {
				logger.Error("failed to watch request files", "error", err)
				os.Exit(1)
			}
			defer closer.Close()
			logger.Info("watching request files for changes", "files", files)
		}
	}

	server := &http.Server{
		Addr:              listenAddress(*port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func listenAddress(port int) string {
	return fmt.Sprintf(":%d", port)
}

type inputSource struct {
	Methods   []restclient.Method
	StaticDir string
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
	return inputSource{Methods: methods}, nil
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
	mux.Handle(mountRoot, http.StripPrefix(mountRoot, http.FileServer(http.FS(staticFS))))
	mux.Handle("/", mockServer)
	return mux
}

func reloadMockFiles(mockServer *mockhttp.Server, files []string, logger *slog.Logger, out io.Writer) {
	methods, err := restclient.Load(files)
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
