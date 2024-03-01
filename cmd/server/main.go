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

func printUsageMessage() {
	message := "Start mock MockServer with mock file, directory or <stdin>.\nmock [flags] [input_file]"
	fmt.Fprintln(os.Stderr, message)
	flag.PrintDefaults()
}

func main() {
	var serverPort int
	var logRequest, logResponse bool

	flag.Usage = printUsageMessage
	flag.IntVar(&serverPort, "p", 7777, "port")
	flag.BoolVar(&logRequest, "r", false, "log request")
	flag.BoolVar(&logResponse, "s", false, "log response")
	flag.Parse()

	filename := flag.Arg(0)

	if filename == "" {
		// read input from stdin
		info, err := os.Stdin.Stat()
		if err != nil {
			panic(err)
		}

		if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
			flag.Usage()
			os.Exit(1)
		}

		mock := newMockServerReader(serverPort, bufio.NewReader(os.Stdin), logRequest, logResponse)
		checkErr(mock.Server.ListenAndServe())

	} else {
		// check command line input:
		//   file: start mock server
		//   directory: start static server
		f := filepath.Clean(filename)
		fi, err := os.Stat(f)
		checkErr(err)

		mode := fi.Mode()
		if mode.IsDir() {
			startStaticServer(f, serverPort)
		} else {
			mock := newMockServerFile(serverPort, f, logRequest, logResponse)
			checkErr(mock.Server.ListenAndServe())
		}
	}
}

func checkErr(err error) {
	if err != nil {
		log.Fatalln(err.Error())
	}
}

// serve single directory as static file assets (html, css, js, whatever)
func startStaticServer(fn string, port int) {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	log.Printf("Serving %q on localhost:%d\n", fn, port)
	checkErr(http.ListenAndServe(fmt.Sprintf(":%d", port), http.FileServer(http.Dir(fn))))
}
