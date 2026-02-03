package main

import (
	"bufio"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

//go:embed static/*
var embeddedFiles embed.FS

// Config holds the configuration for the mock server.
// It includes settings for the server address, log path, and static file system.
type Config struct {
	mockAddr string
	logPath  string
	staticFS fs.FS
}

// main initializes the mock server based on command-line arguments.
// It supports running from stdin, a file, or a directory.
// Usage: mock [flags] [input_file]
// Flags:
//
//	-p int: port (default 7777)
//	-l string: URL path to view the request log (default "/mock")
func main() {
	var mockPort int
	var logPath string
	var cfg Config

	flag.Usage = printUsageMessage
	flag.IntVar(&mockPort, "p", 7777, "port")
	flag.StringVar(&logPath, "l", "/mock", "URL path to view the request log")

	flag.Parse()

	staticFS, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		log.Fatalf("failed to prepare embedded filesystem: %v", err)
	}

	cfg.staticFS = staticFS
	cfg.mockAddr = fmt.Sprintf(":%d", mockPort)
	cfg.logPath = normalizeMountPath(logPath)

	fn := ""
	if len(flag.Arg(0)) > 0 {
		fn = filepath.Clean(flag.Arg(0))
	}

	if fn == "" {
		log.Fatal(startStdinServer(cfg))
	} else {
		fi, err := os.Stat(fn)

		if err == nil {
			if fi.Mode().IsDir() {
				log.Fatal(startStaticServer(cfg, fn))
			} else if fi.Mode().IsRegular() {
				log.Fatal(startFileServer(cfg, fn))
			} else {
				fmt.Printf("File %q is an unknown file type\n", fn)
			}
		} else if os.IsNotExist(err) {
			fmt.Printf("File %q does not exist\n", fn)
		} else {
			fmt.Println(err.Error())
		}

		os.Exit(1)
	}
}

// startStdinServer reads from stdin and starts the mock server.
// It uses bufio.Scanner for better error handling and resource management.
func startStdinServer(cfg Config) error {
	info, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat stdin: %w", err)
	}

	if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
		flag.Usage()
		os.Exit(1)
	}

	s, err := newStdinServer(cfg, bufio.NewReader(os.Stdin))
	if err != nil {
		return fmt.Errorf("failed to create stdin server: %w", err)
	}

	return s.ListenAndServe()
}

// startFileServer serves mock files from the specified file.
// It watches for changes and reloads routes dynamically.
func startFileServer(cfg Config, fn string) error {
	s, err := newFileServer(cfg, fn)
	if err != nil {
		return fmt.Errorf("failed to create file server: %w", err)
	}
	return s.ListenAndServe()
}

// startStaticServer serves a static directory.
// It does not support mocking; only serves files.
func startStaticServer(cfg Config, fn string) error {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	s := newStaticServer(cfg, fn)
	log.Printf("Serving %q on localhost%s\n", fn, cfg.mockAddr)
	return s.ListenAndServe()
}

func printUsageMessage() {
	message := "Start mock server with REST Client file, directory or <stdin>.\nmock [flags] [input_file]"
	fmt.Fprintln(os.Stderr, message)
	flag.PrintDefaults()
}

// normalizeMountPath ensures the path begins with and does not end with a forward slash.
// It normalizes the log path for consistent routing.
func normalizeMountPath(path string) string {
	// Add leading slash if missing
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Strip all trailing slashes
	for strings.HasSuffix(path, "/") && len(path) > 1 {
		path = path[:len(path)-1]
	}

	return path
}
