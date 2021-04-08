package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sspencer/mock"
)

func main() {
	var err error
	portPtr := flag.Int("p", envInt("MOCK_PORT", 8080), "port to run server on")
	reqPtr := flag.Bool("r", envBool("MOCK_LOG", false), "log the request")
	delayPtr := flag.String("d", env("MOCK_DELAY", "0ms"), "delay server responses")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Start the mock HTTP server with the API file or <stdin>.\nmock [flags] [input_file]")
		flag.PrintDefaults()
	}

	flag.Parse()

	fn := flag.Arg(0)
	if fn == "" {
		info, err := os.Stdin.Stat()
		if err != nil {
			panic(err)
		}

		if info.Mode()&os.ModeCharDevice != 0 || info.Size() <= 0 {
			flag.Usage()
			os.Exit(1)
		}
	}

	delay := time.Duration(0)
	if *delayPtr != "" {
		if delay, err = time.ParseDuration(*delayPtr); err != nil {
			fmt.Fprintln(os.Stderr, "Deplay format error (e.g. '500ms').")
			os.Exit(1)
		}
	}

	if fn == "" {
		mockReaderAPI(bufio.NewReader(os.Stdin), *portPtr, *reqPtr, delay)
	} else {
		fn = filepath.Clean(fn)

		fi, err := os.Stat(fn)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		switch mode := fi.Mode(); {
		case mode.IsDir():
			serveDirectory(fn, *portPtr, delay)
		case mode.IsRegular():
			mockFileAPI(fn, *portPtr, *reqPtr, delay)
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

	server := mock.NewServer(port, dbg, delay)
	server.WatchRoutes(routes)
	panic(server.ListenAndServe())
}

func mockFileAPI(fn string, port int, dbg bool, delay time.Duration) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	server := mock.NewServer(port, dbg, delay)
	routesCh := make(chan []*mock.Route)
	server.Watch(routesCh)

	watch(fn, routesParser(fn, delay, routesCh))

	panic(server.ListenAndServe())
}

func watch(fn string, parser func()) {

	// parse file at start
	parser()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Write == fsnotify.Write && path.Base(fn) == path.Base(event.Name) {
					parser()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Error watching file:", err)
			}
		}
	}()

	err = watcher.Add(filepath.Dir(fn))
	if err != nil {
		log.Fatal(err)
	}
}

func routesParser(fn string, delay time.Duration, ch chan []*mock.Route) func() {
	return func() {
		routes, err := mock.RoutesFile(fn, delay)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		} else {
			ch <- routes
		}
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
