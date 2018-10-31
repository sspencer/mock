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

	"github.com/fsnotify/fsnotify"
	"github.com/sspencer/mock"
)

func main() {

	portPtr := flag.Int("p", 8080, "port to run server on")
	reqPtr := flag.Bool("r", false, "log the request")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Create a mock HTTP server.\nmock [flags] schema.api")
		flag.PrintDefaults()
	}

	flag.Parse()

	fn := flag.Arg(0)
	if fn == "" {
		fmt.Fprintln(os.Stderr, "Schema file must be specified.")
		os.Exit(1)
	}

	fn = filepath.Clean(fn)

	fi, err := os.Stat(fn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch mode := fi.Mode(); {
	case mode.IsDir():
		serveDirectory(fn, *portPtr)
	case mode.IsRegular():
		mockAPI(fn, *portPtr, *reqPtr)
	}
}

func serveDirectory(fn string, port int) {
	fn = strings.Replace(fn, " ", "\\ ", -1)

	fmt.Printf("Serving %q on localhost:%d\n", fn, port)
	panic(http.ListenAndServe(fmt.Sprintf(":%d", port), http.FileServer(http.Dir(fn))))
}

func mockAPI(fn string, port int, dbg bool) {
	log.Printf("Serving MOCK API on localhost:%d\n", port)

	server := mock.NewServer(port, dbg)
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
