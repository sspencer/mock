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

	"github.com/sspencer/mock/internal/util"
)

func main() {
	var serverPort int
	var logRequest bool

	flag.IntVar(&serverPort, "p", util.EnvInt("MOCK_PORT", 7777), "port")
	flag.BoolVar(&logRequest, "r", util.EnvBool("MOCK_LOG", false), "log request body")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Start mock MockServer with mock file, directory or <stdin>.\nmock [flags] [input_file]")
		flag.PrintDefaults()
	}

	flag.Parse()
	filename := flag.Arg(0)

	var mockServer *MockServer

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

		mockServer = newMockServerReader(serverPort, bufio.NewReader(os.Stdin), logRequest)
		startMockServer(mockServer.Server)

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
			mockServer = newMockServerFile(serverPort, f, logRequest)
			startMockServer(mockServer.Server)
		}
	}
}

func checkErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func startMockServer(s *http.Server) {
	checkErr(s.ListenAndServe())
}

// serve single directory as static file assets (html, css, js, whatever)
func startStaticServer(fn string, port int) {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	log.Printf("Serving %q on localhost:%d\n", fn, port)
	checkErr(http.ListenAndServe(fmt.Sprintf(":%d", port), http.FileServer(http.Dir(fn))))
}
