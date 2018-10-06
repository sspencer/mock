package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

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

	fn = path.Clean(fn)

	//log.SetFlags(log.Ltime)
	log.Printf("Serving MOCK API on localhost:%d\n", *portPtr)
	//panic(http.ListenAndServe(port, http.FileServer(http.Dir(dir))))

	server := mock.NewServer(*portPtr, *reqPtr)
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
