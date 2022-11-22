package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/sspencer/mock/internal/data"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	portPtr := flag.Int("p", envInt("MOCK_PORT", 8080), "port to run server on")
	reqPtr := flag.Bool("r", envBool("MOCK_LOG", false), "log the request")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Start the mock HTTP server with the API file or <stdin>.\nmock [flags] [input_file]")
		flag.PrintDefaults()
	}

	flag.Parse()

	filename := flag.Arg(0)
	if filename == "" {
		// check for input on stdin
		info, err := os.Stdin.Stat()
		if err != nil {
			panic(err)
		}

		if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
			flag.Usage()
			os.Exit(1)
		}

		mockReaderServer(bufio.NewReader(os.Stdin), *portPtr, *reqPtr)
	} else {
		files, dirs, err := readArgs()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		numDirs := len(dirs)
		numFiles := len(files)

		if numFiles > 0 && numDirs == 0 {
			mockFileServer(files, *portPtr, *reqPtr)
		} else if numFiles == 0 && numDirs == 1 {
			staticFileServer(dirs[0], *portPtr)
		} else {
			fmt.Fprintln(os.Stderr, "Only serves one directory, or one or more files")
			os.Exit(1)
		}
	}
}

// read rest of command line args, classified into Files and Directories
func readArgs() ([]string, []string, error) {
	var files, dirs []string
	for _, f := range flag.Args() {
		f = filepath.Clean(f)

		fi, err := os.Stat(f)
		if err != nil {
			return nil, nil, err
		}

		switch mode := fi.Mode(); {
		case mode.IsDir():
			dirs = append(dirs, f)
		case mode.IsRegular():
			files = append(files, f)
		}
	}
	return files, dirs, nil
}

// serve single directory as static file assets (html, css, js, whatever)
func staticFileServer(fn string, port int) {
	fn = strings.Replace(fn, " ", "\\ ", -1)

	log.Printf("Serving %q on localhost:%d\n", fn, port)
	panic(http.ListenAndServe(fmt.Sprintf(":%d", port), http.FileServer(http.Dir(fn))))
}

// serve <stdin> or piped file as mock file input
func mockReaderServer(reader io.Reader, port int, dbg bool) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	routes, err := data.GetEndpointsFromReader(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		os.Exit(1)
	}

	server := newServer(port, dbg)
	server.loadRoutes(routes)
	err = server.ListenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "server address %d already in use\n", port)
		os.Exit(1)
	}
}

// serve all files specified on command line as mock files
func mockFileServer(fn []string, port int, dbg bool) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	server := newServer(port, dbg)
	server.watchFiles(fn)
	err := server.ListenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "server address %d already in use\n", port)
		os.Exit(1)
	}
}
