package mockhttp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// maxFixtureBytes caps a fixture file read or written through /fixture.
const maxFixtureBytes = 4 << 20

// FixtureFile is a route's @fixture file as GET /fixture returns it and PUT
// /fixture accepts it.
type FixtureFile struct {
	// File is the absolute path of the fixture on disk.
	File string `json:"file"`
	// Fixture is the path as the route wrote it (GET only).
	Fixture string `json:"fixture,omitempty"`
	Content string `json:"content"`
	// Version is the SHA-256 of the content on disk, or "" when the file does
	// not exist. A PUT must send the version it edited; a different version on
	// disk means someone else changed the file, and the write is refused.
	Version string `json:"version"`
}

// fixtureVersion is the version of fixture content, or "" for a missing file.
func fixtureVersion(content []byte, exists bool) string {
	if !exists {
		return ""
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ServeFixture handles the admin /fixture endpoint. GET ?route=ID returns
// that route's @fixture file. PUT writes a fixture file: only a file that is
// the @fixture of a current route, only valid JSON, and only when the file
// has not changed since it was read. Fixtures are re-read for every
// response, so a write applies to the next request. PUT applies the same
// cross-site checks as /clear.
func (s *Server) ServeFixture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		s.serveFixtureRead(w, r)
	case http.MethodPut:
		s.serveFixtureWrite(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) serveFixtureRead(w http.ResponseWriter, r *http.Request) {
	method, ok := s.routeByID(r.URL.Query().Get("route"))
	if !ok {
		http.Error(w, "no such route; the routes may have been reloaded", http.StatusNotFound)
		return
	}
	if method.Fixture == "" {
		http.Error(w, "the route has no @fixture", http.StatusNotFound)
		return
	}
	file := fixtureFilePath(method)
	content, exists, err := readFixtureFile(file)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errFixtureTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, FixtureFile{File: file, Fixture: method.Fixture, Content: string(content), Version: fixtureVersion(content, exists)})
}

func (s *Server) serveFixtureWrite(w http.ResponseWriter, r *http.Request) {
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var edit FixtureFile
	// JSON escaping can double the size of the content.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxFixtureBytes+4096)).Decode(&edit); err != nil {
		http.Error(w, "invalid fixture request: "+err.Error(), http.StatusBadRequest)
		return
	}
	file := filepath.Clean(edit.File)
	if !s.isFixtureFile(file) {
		http.Error(w, "the file is not the @fixture of a current route", http.StatusForbidden)
		return
	}
	if len(edit.Content) > maxFixtureBytes {
		http.Error(w, errFixtureTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	if err := checkFixtureJSON([]byte(edit.Content)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.fixtureMu.Lock()
	defer s.fixtureMu.Unlock()
	current, exists, err := readFixtureFile(file)
	if err != nil && !errors.Is(err, errFixtureTooLarge) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if version := fixtureVersion(current, exists); version != edit.Version {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the file changed on disk since it was opened; reload it to see the changes", "version": version})
		return
	}
	if err := writeFileAtomic(file, []byte(edit.Content)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": fixtureVersion([]byte(edit.Content), true)})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// routeByID returns the current route with a RouteInfo ID ("revision:index").
func (s *Server) routeByID(id string) (restclient.Method, bool) {
	rawRevision, rawIndex, ok := strings.Cut(id, ":")
	if !ok {
		return restclient.Method{}, false
	}
	revision, err1 := strconv.ParseUint(rawRevision, 10, 64)
	index, err2 := strconv.Atoi(rawIndex)
	if err1 != nil || err2 != nil {
		return restclient.Method{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision != s.config.Revision || index < 0 || index >= len(s.methods) {
		return restclient.Method{}, false
	}
	return s.methods[index], true
}

// fixtureFilePath is the absolute path a route's @fixture resolves to.
func fixtureFilePath(method restclient.Method) string {
	path := restclient.ResolveFile(method.Source, method.Fixture)
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// isFixtureFile reports whether file is the @fixture of a current route.
func (s *Server) isFixtureFile(file string) bool {
	s.mu.Lock()
	methods := s.methods
	s.mu.Unlock()
	for _, method := range methods {
		if method.Fixture != "" && fixtureFilePath(method) == file {
			return true
		}
	}
	return false
}

var errFixtureTooLarge = fmt.Errorf("the fixture is larger than %d MiB", maxFixtureBytes>>20)

// readFixtureFile reads a fixture; a missing file is empty and not an error.
func readFixtureFile(file string) ([]byte, bool, error) {
	info, err := os.Stat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Size() > maxFixtureBytes {
		return nil, true, errFixtureTooLarge
	}
	content, err := os.ReadFile(file)
	return content, err == nil, err
}

// checkFixtureJSON reports where content stops being a single JSON value.
func checkFixtureJSON(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	var value any
	err := decoder.Decode(&value)
	if err == nil && decoder.More() {
		offset := decoder.InputOffset()
		line, column := lineColumn(content, offset)
		return fmt.Errorf("invalid JSON at line %d, column %d: more than one JSON value", line, column)
	}
	if err == nil {
		return nil
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		line, column := lineColumn(content, syntax.Offset)
		return fmt.Errorf("invalid JSON at line %d, column %d: %s", line, column, syntax.Error())
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		line, column := lineColumn(content, int64(len(content)))
		return fmt.Errorf("invalid JSON at line %d, column %d: unexpected end of input", line, column)
	}
	return fmt.Errorf("invalid JSON: %w", err)
}

func lineColumn(content []byte, offset int64) (int, int) {
	if offset > int64(len(content)) {
		offset = int64(len(content))
	}
	before := content[:offset]
	line := bytes.Count(before, []byte("\n")) + 1
	column := len(before) - bytes.LastIndexByte(before, '\n')
	return line, column
}

// writeFileAtomic replaces file with content through a temporary file in the
// same directory, so a response never reads a half-written fixture. It keeps
// the existing file's permissions.
func writeFileAtomic(file string, content []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, file)
}
