package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sspencer/mock"
)

func main() {
	var err error
	portPtr := flag.Int("p", 8080, "port to run server on")
	reqPtr := flag.Bool("r", false, "log the request")
	delayPtr := flag.String("d", "0ms", "delay server responses")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Create a mock HTTP or local file server.\nmock [flags] <schema.api> OR <directory>")
		flag.PrintDefaults()
	}

	flag.Parse()

	fn := flag.Arg(0)
	if fn == "" {
		fmt.Fprintln(os.Stderr, "Schema file or local directory must be specified.")
		os.Exit(1)
	}

	delay := time.Duration(0)
	if *delayPtr != "" {
		if delay, err = time.ParseDuration(*delayPtr); err != nil {
			fmt.Fprintln(os.Stderr, "Deplay format error (expecting something like '500ms').")
			os.Exit(1)
		}
	}

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
		mockAPI(fn, *portPtr, *reqPtr, delay)
	}
}

func serveDirectory(fn string, port int, delay time.Duration) {
	fn = strings.Replace(fn, " ", "\\ ", -1)

	fmt.Printf("Serving %q on localhost:%d\n", fn, port)
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

func mockAPI(fn string, port int, dbg bool, delay time.Duration) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	server := mock.NewServer(port, dbg, delay)
	schemaChannel := make(chan []*mock.Schema)
	server.Watch(schemaChannel)

	watch(fn, schemaParser(fn, schemaChannel))

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
					//log.Println("modified file:", event.Name)
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

func schemaParser(fn string, ch chan []*mock.Schema) func() {
	return func() {
		schemas, err := mock.SchemaFile(fn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		} else {
			ch <- schemas
		}
	}
}
