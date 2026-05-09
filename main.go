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
		logger.Error("missing request input", "usage", "mock [-l mock] [-p 8080] <file.http> [file.http...] or cat file.http | mock")
		os.Exit(2)
	}

	methods, err := loadMethods(flagSet.Args(), os.Stdin)
	if err != nil {
		logger.Error("failed to load request input", "error", err)
		os.Exit(1)
	}
	if err := validateMethods(methods, flagSet.Args()); err != nil {
		logger.Error("failed to start mock server", "error", err)
		os.Exit(1)
	}
	printMethods(os.Stdout, methods)

	staticFS, err := staticFileSystem()
	if err != nil {
		logger.Error("failed to load static files", "error", err)
		os.Exit(1)
	}
	handler := newHandler(methods, logger, *mount, staticFS)
	server := &http.Server{
		Addr:              listenAddress(*port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting mock HTTP server", "addr", server.Addr, "methods", len(methods), "ui", normalizeMountPath(*mount))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func listenAddress(port int) string {
	return fmt.Sprintf(":%d", port)
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

func newHandler(methods []restclient.Method, logger *slog.Logger, mount string, staticFS fs.FS) http.Handler {
	mockServer := mockhttp.New(methods, logger)
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
