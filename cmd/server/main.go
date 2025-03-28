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
	mockAddr   string
	eventsAddr string
	//logRequest  bool
	//logResponse bool
}

func main() {
	var mockPort int
	var eventsPort int
	var cfg config

	flag.Usage = printUsageMessage
	flag.IntVar(&mockPort, "p", 7777, "port")
	flag.IntVar(&eventsPort, "e", 7778, "events port")
	flag.Parse()

	fn := ""
	if len(flag.Arg(0)) > 0 {
		fn = filepath.Clean(flag.Arg(0))
	}

	cfg.eventsAddr = fmt.Sprintf(":%d", eventsPort)
	cfg.mockAddr = fmt.Sprintf(":%d", mockPort)

	es := newEventServer()
	go es.startServer(cfg)

	if fn == "" {
		log.Fatal(startMockReader(es, cfg))
	} else {
		fi, err := os.Stat(fn)

		if err == nil {
			if fi.Mode().IsDir() {
				log.Fatal(startMockDir(es, cfg, fn))
			} else if fi.Mode().IsRegular() {
				log.Fatal(startMockFile(es, cfg, fn))
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

// startMockReader reads from mock file from <stdin>
func startMockReader(es *eventServer, cfg config) error {
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
func startMockFile(es *eventServer, cfg config, fn string) error {
	mock := newMockFile(es, cfg, fn)
	return mock.Server.ListenAndServe()
}

// startMockDir serves directory of standard html files
func startMockDir(es *eventServer, cfg config, fn string) error {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	mock := newMockDir(es, cfg, fn)
	log.Printf("Serving %q on localhost%s\n", fn, cfg.mockAddr)
	return mock.Server.ListenAndServe()
}

func printUsageMessage() {
	message := "Start mock mockServer with mock file, directory or <stdin>.\nmock [flags] [input_file]"
	fmt.Fprintln(os.Stderr, message)
	flag.PrintDefaults()
}
