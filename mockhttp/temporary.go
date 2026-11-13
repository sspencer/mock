package mockhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// tempSourcePrefix starts the Source of a temporary route, such as
// "(temporary t1)". Its directory is ".", so relative "< path" bodies and
// @fixture files are found from the directory mock runs in.
const tempSourcePrefix = "(temporary "

// tempGroup is the routes one AddRoutes call added.
type tempGroup struct {
	id      string
	methods []restclient.Method
}

// TemporaryRoutes is what AddRoutes (and POST /routes) report.
type TemporaryRoutes struct {
	// ID names the group, for RemoveRoutes and DELETE /routes/{id}.
	ID string `json:"id"`
	// Routes are the names of the routes added.
	Routes []string `json:"routes"`
	// Warnings are the load warnings for the routes, as mock prints them.
	Warnings []string `json:"warnings,omitempty"`
}

// maxTemporaryRoutesBytes caps a POST /routes body.
const maxTemporaryRoutesBytes = 1 << 20

// AddRoutes parses .http text and serves its routes in front of every route
// so far, so a test can stub a response without editing files. The newest
// group is tried first. Routes stay until RemoveRoutes, Reset or the server
// stops; reloads keep them. They are parsed on their own: command-line
// variables, environments and default headers do not apply.
func (s *Server) AddRoutes(source string) (TemporaryRoutes, error) {
	id := s.nextTempID()
	methods, err := restclient.Parse(tempSourcePrefix+id+")", strings.NewReader(source))
	if err != nil {
		return TemporaryRoutes{}, err
	}
	return s.addTemporary(id, methods)
}

// AddMethods serves already-parsed routes as one temporary group, as
// AddRoutes does for .http text. It is for callers that build routes in Go
// (mocktest's Add); each route's Source is replaced by the group's.
func (s *Server) AddMethods(methods []restclient.Method) (TemporaryRoutes, error) {
	id := s.nextTempID()
	methods = append([]restclient.Method(nil), methods...)
	for i := range methods {
		methods[i].Source = tempSourcePrefix + id + ")"
	}
	return s.addTemporary(id, methods)
}

func (s *Server) nextTempID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tempSeq++
	return fmt.Sprintf("t%d", s.tempSeq)
}

func (s *Server) addTemporary(id string, methods []restclient.Method) (TemporaryRoutes, error) {
	if len(methods) == 0 {
		return TemporaryRoutes{}, errors.New("no routes: add at least one ### section with a request line")
	}
	compiled := cloneMethods(methods)
	var warnings warningCollector
	warnMethodConfig(slog.New(&warnings), compiled)
	result := TemporaryRoutes{ID: id, Warnings: warnings.messages}
	for _, method := range compiled {
		result.Routes = append(result.Routes, method.Name)
	}
	s.mu.Lock()
	s.temps = append(s.temps, tempGroup{id: id, methods: compiled})
	s.installLocked()
	s.mu.Unlock()
	return result, nil
}

// RemoveRoutes removes a group of temporary routes, or all of them when id
// is "". It reports how many groups it removed.
func (s *Server) RemoveRoutes(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.temps[:0:0]
	for _, group := range s.temps {
		if id != "" && group.id != id {
			kept = append(kept, group)
		}
	}
	removed := len(s.temps) - len(kept)
	if removed > 0 {
		s.temps = kept
		s.installLocked()
	}
	return removed
}

// temporaryID returns the group of a temporary route, or "".
func temporaryID(method restclient.Method) string {
	id, ok := strings.CutPrefix(method.Source, tempSourcePrefix)
	if !ok {
		return ""
	}
	return strings.TrimSuffix(id, ")")
}

// serveRoutesWrite handles POST /routes (add temporary routes, from .http
// text, or JSON {"source": "..."}) and DELETE /routes[/{id}].
func (s *Server) serveRoutesWrite(w http.ResponseWriter, r *http.Request) {
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/routes"), "/")
	switch r.Method {
	case http.MethodPost:
		if id != "" {
			http.Error(w, "POST to /routes, not /routes/"+id, http.StatusMethodNotAllowed)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTemporaryRoutesBytes))
		if err != nil {
			http.Error(w, "invalid routes: "+err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		source := string(raw)
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			var body struct {
				Source string `json:"source"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			source = body.Source
		}
		result, err := s.AddRoutes(source)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Location", "routes/"+result.ID)
		writeJSON(w, http.StatusCreated, result)
	case http.MethodDelete:
		removed := s.RemoveRoutes(id)
		if id != "" && removed == 0 {
			http.Error(w, "no temporary routes "+id, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// warningCollector is a slog handler that keeps warning messages, with the
// line and placeholder they concern, for AddRoutes to report.
type warningCollector struct {
	messages []string
}

func (c *warningCollector) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (c *warningCollector) Handle(_ context.Context, record slog.Record) error {
	message := record.Message
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "line" || attr.Key == "placeholder" || attr.Key == "method" {
			message += " " + attr.Key + "=" + attr.Value.String()
		}
		return true
	})
	c.messages = append(c.messages, message)
	return nil
}

func (c *warningCollector) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *warningCollector) WithGroup(string) slog.Handler      { return c }
