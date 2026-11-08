package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/sspencer/mock/restclient"
)

type inputSource struct {
	Methods    []restclient.Method
	StaticDir  string
	WatchFiles []string
}

func loadInput(args []string, stdin io.Reader, opts restclient.Options) (inputSource, error) {
	if len(args) == 1 {
		info, err := os.Stat(args[0])
		if err != nil {
			return inputSource{}, err
		}
		if info.IsDir() {
			return inputSource{StaticDir: args[0]}, nil
		}
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return inputSource{}, err
		}
		if info.IsDir() {
			return inputSource{}, fmt.Errorf("cannot mix static directory %q with other request inputs", arg)
		}
	}
	methods, err := loadMethods(args, stdin, opts)
	if err != nil {
		return inputSource{}, err
	}
	src := inputSource{Methods: methods}
	if len(args) > 0 {
		src.WatchFiles = append([]string(nil), args...)
	}
	return src, nil
}

func loadMethods(args []string, stdin io.Reader, opts restclient.Options) ([]restclient.Method, error) {
	if len(args) > 0 {
		return opts.Load(args)
	}
	return opts.Parse("<stdin>", stdin)
}

func validateMethods(methods []restclient.Method, args []string) error {
	if len(methods) > 0 {
		return nil
	}
	source := "stdin"
	if len(args) > 0 {
		source = strings.Join(args, ", ")
	}
	return fmt.Errorf("no mock requests found in %s; add at least one request section starting with ### followed by an HTTP request line", source)
}

func stdinIsTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// printMethods lists the routes, grouped by port when they are served on
// more than one.
func printMethods(w io.Writer, methods []restclient.Method) {
	var ports []int
	byPort := make(map[int][]restclient.Method)
	for _, method := range methods {
		if _, ok := byPort[method.Port]; !ok {
			ports = append(ports, method.Port)
		}
		byPort[method.Port] = append(byPort[method.Port], method)
	}
	slices.Sort(ports)
	if len(ports) <= 1 {
		fmt.Fprintln(w, "Available mock methods:")
		printMethodList(w, methods)
		return
	}
	for _, port := range ports {
		fmt.Fprintf(w, "Available mock methods on port %d:\n", port)
		printMethodList(w, byPort[port])
	}
}

func printMethodList(w io.Writer, methods []restclient.Method) {
	for _, method := range methods {
		target := method.Path
		if query := method.Query.Encode(); query != "" {
			target += "?" + query
		}
		fmt.Fprintf(w, "  %-7s %-30s %s\n", method.Method, target, method.Name)
	}
}
