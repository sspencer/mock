package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sspencer/mock/restclient"
)

type inputSource struct {
	Methods   []restclient.Method
	StaticDir string
	// Files are the request files given as arguments, and Dirs the --dir
	// directories whose .http files are loaded after them.
	Files, Dirs []string
}

// watched reports whether the input came from files that can be watched.
func (s inputSource) watched() bool { return len(s.Files) > 0 || len(s.Dirs) > 0 }

// requestFiles lists the request files to load: the file arguments, then the
// .http files under each directory in lexical path order.
func (s inputSource) requestFiles() ([]string, error) {
	files := append([]string(nil), s.Files...)
	for _, dir := range s.Dirs {
		found, err := findRequestFiles(dir)
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	return files, nil
}

// findRequestFiles walks dir for .http files, skipping hidden files and
// directories such as .git.
func findRequestFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != dir && hiddenName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && isRequestFile(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func hiddenName(name string) bool { return strings.HasPrefix(name, ".") }

func isRequestFile(path string) bool { return strings.EqualFold(filepath.Ext(path), ".http") }

// describe names the input for messages: the files and directories, or stdin.
func (s inputSource) describe() string {
	names := append(append([]string(nil), s.Files...), s.Dirs...)
	if len(names) == 0 {
		return "stdin"
	}
	return strings.Join(names, ", ")
}

func loadInput(args, dirs []string, stdin io.Reader, opts restclient.Options) (inputSource, error) {
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return inputSource{}, err
		}
		if !info.IsDir() {
			return inputSource{}, fmt.Errorf("--dir %q is not a directory", dir)
		}
	}
	if len(args) == 1 && len(dirs) == 0 {
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
	src := inputSource{Files: append([]string(nil), args...), Dirs: append([]string(nil), dirs...)}
	methods, err := src.load(stdin, opts)
	if err != nil {
		return inputSource{}, err
	}
	src.Methods = methods
	return src, nil
}

// load parses the request files, or stdin when there are none.
func (s inputSource) load(stdin io.Reader, opts restclient.Options) ([]restclient.Method, error) {
	if !s.watched() {
		return opts.Parse("<stdin>", stdin)
	}
	files, err := s.requestFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	return opts.Load(files)
}

func validateMethods(methods []restclient.Method, source string) error {
	if len(methods) > 0 {
		return nil
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
