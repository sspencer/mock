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
	mockAddr    string
	eventsAddr  string
	logRequest  bool
	logResponse bool
}

func main() {
	var mockPort int
	var adminPort int
	var cfg config

	flag.Usage = printUsageMessage
	flag.IntVar(&mockPort, "p", 7777, "port")
	flag.IntVar(&adminPort, "e", 7778, "events port")
	flag.BoolVar(&cfg.logRequest, "r", false, "log request")
	flag.BoolVar(&cfg.logResponse, "s", false, "log response")
	flag.Parse()

	filename := filepath.Clean(flag.Arg(0))
	cfg.eventsAddr = fmt.Sprintf(":%d", adminPort)
	cfg.mockAddr = fmt.Sprintf(":%d", mockPort)

	es := NewEventServer()
	go es.startServer(cfg)

	if filename == "" {
		log.Fatal(startMockReader(es, cfg))
	} else {
		fi, err := os.Stat(filename)

		if err == nil {
			if fi.Mode().IsDir() {
				log.Fatal(startMockDir(cfg, filename)) // TODO add AdminServer (and a logger)
			} else {
				log.Fatal(startMockFile(es, cfg, filename))
			}
		}
	}
}

// startMockReader reads from mock file from <stdin>
func startMockReader(es *EventServer, cfg config) error {
	// read input from stdin
	info, err := os.Stdin.Stat()
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
		flag.Usage()
		os.Exit(1)
	}

	mock := newMockReader(es, cfg, bufio.NewReader(os.Stdin))
	return mock.Server.ListenAndServe()
}

// startMockFile serves mock file passed in from command line
func startMockFile(es *EventServer, cfg config, fn string) error {
	mock := newMockFile(es, cfg, fn)
	return mock.Server.ListenAndServe()
}

// startMockDir serves directory of standard html files
func startMockDir(cfg config, fn string) error {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	log.Printf("Serving %q on localhost%s\n", fn, cfg.mockAddr)
	return http.ListenAndServe(cfg.mockAddr, http.FileServer(http.Dir(fn)))
}

func printUsageMessage() {
	message := "Start mock MockServer with mock file, directory or <stdin>.\nmock [flags] [input_file]"
	fmt.Fprintln(os.Stderr, message)
	flag.PrintDefaults()
}
