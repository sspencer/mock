package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	addr        string
	logRequest  bool
	logResponse bool
}

func main() {
	var serverPort int
	var cfg config

	flag.Usage = printUsageMessage
	flag.IntVar(&serverPort, "p", 7777, "port")
	flag.BoolVar(&cfg.logRequest, "r", false, "log request")
	flag.BoolVar(&cfg.logResponse, "s", false, "log response")
	flag.Parse()

	filename := filepath.Clean(flag.Arg(0))
	cfg.addr = fmt.Sprintf(":%d", serverPort)

	if filename == "" {
		err := serveReader(cfg)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		fi, err := os.Stat(filename)

		if err == nil {
			if fi.Mode().IsDir() {
				err = serveDirectory(cfg, filename)
			} else {
				err = serveFile(cfg, filename)
			}
		}

		if err != nil {
			log.Fatalln(err.Error())
		}
	}
}

// serveReader reads from mock file from <stdin>
func serveReader(cfg config) error {
	// read input from stdin
	info, err := os.Stdin.Stat()
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
		flag.Usage()
		os.Exit(1)
	}

	mock := newMockReader(cfg, bufio.NewReader(os.Stdin))
	return mock.Server.ListenAndServe()
}

// serveFile serves mock file passed in from command line
func serveFile(cfg config, fn string) error {
	mock := newMockFile(cfg, fn)
	return mock.Server.ListenAndServe()
}

// serveDirectory serves directory of standard html files
func serveDirectory(cfg config, fn string) error {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	log.Printf("Serving %q on localhost%s\n", fn, cfg.addr)
	return http.ListenAndServe(cfg.addr, http.FileServer(http.Dir(fn)))
}

func printUsageMessage() {
	message := "Start mock MockServer with mock file, directory or <stdin>.\nmock [flags] [input_file]"
	fmt.Fprintln(os.Stderr, message)
	flag.PrintDefaults()
}
