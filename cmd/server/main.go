package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	mockAddr string
	logPath  string
}

func main() {
	var mockPort int
	var logPath string
	var cfg config

	flag.Usage = printUsageMessage
	flag.IntVar(&mockPort, "p", 7777, "port")
	flag.StringVar(&logPath, "l", "/mock", "URL path to view the request log")

	flag.Parse()

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

// startStdinServer reads from mock file from <stdin>
func startStdinServer(cfg config) error {
	// read input from stdin
	info, err := os.Stdin.Stat()
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
		flag.Usage()
		os.Exit(1)
	}

	s := newStdinServer(cfg, bufio.NewReader(os.Stdin))
	return s.ListenAndServe()
}

// startFileServer serves mock file passed in from command line
func startFileServer(cfg config, fn string) error {
	s := newFileServer(cfg, fn)
	return s.ListenAndServe()
}

// startStaticServer serves directory of standard html files
func startStaticServer(cfg config, fn string) error {
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

// normalizeMountPath ensures the path begins with and does not end with a forward slash
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
