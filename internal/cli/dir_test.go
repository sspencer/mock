package cli

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func route(name, path string) string { return "### " + name + "\nGET " + path + "\n\nok\n" }

func TestFindRequestFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"b.http":           route("B", "/b"),
		"a.http":           route("A", "/a"),
		"users/list.HTTP":  route("List", "/users"),
		"users/notes.txt":  "not a request file",
		".hidden.http":     route("Hidden", "/hidden"),
		".git/config.http": route("Git", "/git"),
	})
	files, err := findRequestFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, f := range files {
		r, _ := filepath.Rel(root, f)
		rel = append(rel, filepath.ToSlash(r))
	}
	if want := []string{"a.http", "b.http", "users/list.HTTP"}; !slices.Equal(rel, want) {
		t.Fatalf("files = %q, want %q", rel, want)
	}
}

func TestLoadInputDirsAfterFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"dir/a.http": route("Dir A", "/a"),
		"first.http": route("First", "/first"),
	})
	input, err := loadInput([]string{filepath.Join(root, "first.http")}, []string{filepath.Join(root, "dir")}, nil, restclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range input.Methods {
		names = append(names, m.Name)
	}
	if want := []string{"First", "Dir A"}; !slices.Equal(names, want) {
		t.Fatalf("routes = %q, want %q", names, want)
	}
	if input.StaticDir != "" || !input.watched() {
		t.Fatalf("--dir should load request files, got %+v", input)
	}

	if _, err := loadInput(nil, []string{filepath.Join(root, "first.http")}, nil, restclient.Options{}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("--dir on a file error = %v", err)
	}
	empty := t.TempDir()
	input, err = loadInput(nil, []string{empty}, nil, restclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMethods(input.Methods, input.describe()); err == nil || !strings.Contains(err.Error(), empty) {
		t.Fatalf("empty --dir should name the directory: %v", err)
	}
}

func TestReloadPicksUpNewFilesInDir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.http": route("A", "/a")})
	input, err := loadInput(nil, []string{root}, nil, restclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := mockhttp.New(input.Methods, slog.New(slog.NewTextHandler(io.Discard, nil)))

	writeTree(t, root, map[string]string{"nested/b.http": route("B", "/b")})
	reloadMockFiles(server, input, restclient.Options{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, io.Discard)
	if got := len(server.Methods()); got != 2 {
		t.Fatalf("routes after adding a file = %d, want 2", got)
	}
	if err := os.Remove(filepath.Join(root, "a.http")); err != nil {
		t.Fatal(err)
	}
	reloadMockFiles(server, input, restclient.Options{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, io.Discard)
	if methods := server.Methods(); len(methods) != 1 || methods[0].Name != "B" {
		t.Fatalf("routes after removing a file = %+v, want only B", methods)
	}
}

func TestWatcherSeesNewFilesInTree(t *testing.T) {
	root := t.TempDir()
	events := make(chan struct{}, 10)
	w, err := watchFiles(nil, func() { events <- struct{}{} }, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	wait := func(what string) {
		t.Helper()
		select {
		case <-events:
		case <-time.After(3 * time.Second):
			t.Fatalf("no reload after %s", what)
		}
	}
	quiet := func(what string) {
		t.Helper()
		select {
		case <-events:
			t.Fatalf("unexpected reload after %s", what)
		case <-time.After(400 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)

	writeTree(t, root, map[string]string{"notes.txt": "x", ".swap.http": "x"})
	quiet("writing non-request and hidden files")

	writeTree(t, root, map[string]string{"a.http": route("A", "/a")})
	wait("adding a.http")

	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	wait("adding a subdirectory")
	if err := w.Reconcile(nil, root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"sub/b.http": route("B", "/b")})
	wait("adding sub/b.http")
}

func TestParseConfigDirs(t *testing.T) {
	cfg, err := parseConfig([]string{"-d", "fixtures", "--dir", "more", "extra.http"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Dirs, []string{"fixtures", "more"}) || !slices.Equal(cfg.Args, []string{"extra.http"}) {
		t.Fatalf("Dirs = %q, Args = %q", cfg.Dirs, cfg.Args)
	}
}

func TestValidateDirectoryArgument(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.http": route("A", "/a"), "sub/b.http": route("B", "/b")})
	out, err := runValidateArgs(t, root)
	if err != nil || !strings.Contains(out, "ok: 2 routes") {
		t.Fatalf("validate dir = %v\n%s", err, out)
	}
	if out, err := runValidateArgs(t, t.TempDir()); err == nil {
		t.Fatalf("validate of an empty directory should fail:\n%s", out)
	}
}
