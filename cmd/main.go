package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sspencer/mock"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	var err error
	portPtr := flag.Int("p", envInt("MOCK_PORT", 8080), "port to run server on")
	reqPtr := flag.Bool("r", envBool("MOCK_LOG", false), "log the request")
	delayPtr := flag.String("d", env("MOCK_DELAY", "0ms"), "delay server responses")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Start the mock HTTP server with the API file or <stdin>.\nmock [flags] [input_file]")
		flag.PrintDefaults()
	}

	flag.Parse()

	delay := time.Duration(0)
	if *delayPtr != "" {
		if delay, err = time.ParseDuration(*delayPtr); err != nil {
			fmt.Fprintln(os.Stderr, "Delay format error (e.g. '500ms').")
			os.Exit(1)
		}
	}

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

		mockReaderAPI(bufio.NewReader(os.Stdin), *portPtr, *reqPtr, delay)
	} else {
		// one or more files specified on command line
		var files, dirs []string
		for _, f := range flag.Args() {
			f = filepath.Clean(f)

			fi, err := os.Stat(f)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			switch mode := fi.Mode(); {
			case mode.IsDir():
				dirs = append(dirs, f)
				//serveDirectory(f, *portPtr, delay)
			case mode.IsRegular():
				files = append(files, f)
				//mockFileAPI(f, *portPtr, *reqPtr, delay)
			}
		}

		numDirs := len(dirs)
		numFiles := len(files)
		if numFiles > 0 && numDirs == 0 {
			mockAPI(files, *portPtr, *reqPtr, delay)
		} else if numFiles == 0 && numDirs == 1 {
			serveDirectory(dirs[0], *portPtr, delay)
		} else {
			fmt.Fprintln(os.Stderr, "Only serves one directory, or one or more files")
			os.Exit(1)
		}
	}
}

func serveDirectory(fn string, port int, delay time.Duration) {
	fn = strings.Replace(fn, " ", "\\ ", -1)

	log.Printf("Serving %q on localhost:%d\n", fn, port)
	panic(http.ListenAndServe(fmt.Sprintf(":%d", port), delayer(delay, http.FileServer(http.Dir(fn)))))
}

func delayer(delay time.Duration, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		h.ServeHTTP(w, r)
	})
}

func mockReaderAPI(reader io.Reader, port int, dbg bool, delay time.Duration) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	routes, err := mock.RoutesReader(reader, delay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		os.Exit(1)
	}

	server := mock.NewServer(port, dbg)
	server.WatchRoutes(routes)
	err = server.ListenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server address %d already in use\n", port)
		os.Exit(1)
	}
}

func mockAPI(fn []string, port int, dbg bool, delay time.Duration) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	server := mock.NewServer(port, dbg)
	server.WatchFiles(fn, delay)
	err := server.ListenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server address %d already in use\n", port)
		os.Exit(1)
	}
}

func env(key, defaultValue string) string {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	return val
}

func envInt(key string, defaultValue int) int {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}

	if i, err := strconv.Atoi(val); err == nil {
		return i
	}

	return defaultValue
}

func envBool(key string, defaultValue bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}

	if b, err := strconv.ParseBool(val); err == nil {
		return b
	}

	return defaultValue
}
