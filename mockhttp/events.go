package mockhttp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sspencer/mock/restclient"
)

const maxRequestEvents = 200

type RequestEvent struct {
	ID       uint64        `json:"id"`
	Request  EventRequest  `json:"request"`
	Response EventResponse `json:"response"`
}

type EventRequest struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Time    string `json:"time"`
	Details string `json:"details"`
}

type EventResponse struct {
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Time       string `json:"time"`
	Details    string `json:"details"`
}

// RouteInfo is a JSON-friendly description of a configured mock route.
type RouteInfo struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query,omitzero"`
}

func (s *Server) ServeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastID := parseLastEventID(r)
	events, subscriber := s.subscribe()
	defer s.unsubscribe(subscriber)

	for _, event := range events {
		if event.ID <= lastID {
			continue
		}
		if !writeEvent(w, event) {
			return
		}
	}
	flusher.Flush()

	for {
		select {
		case event := <-subscriber:
			if event.ID <= lastID {
				continue
			}
			if !writeEvent(w, event) {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ServeClear handles POST to clear the in-memory request log and rotation counters.
func (s *Server) ServeClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.ClearEvents()
	s.ResetCounters()
	w.WriteHeader(http.StatusNoContent)
}

// ServeRoutes handles GET of the currently configured mock routes.
func (s *Server) ServeRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	methods := s.Methods()
	routes := make([]RouteInfo, 0, len(methods))
	for _, method := range methods {
		routes = append(routes, RouteInfo{
			Name:   method.Name,
			Method: method.Method,
			Path:   method.Path,
			Query:  method.Query.Encode(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(routes)
}

func (s *Server) subscribe() ([]RequestEvent, chan RequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := append([]RequestEvent(nil), s.events...)
	subscriber := make(chan RequestEvent, 16)
	s.subscribers[subscriber] = struct{}{}
	return events, subscriber
}

func (s *Server) unsubscribe(subscriber chan RequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.subscribers, subscriber)
}

func (s *Server) publishRequest(event RequestEvent) {
	if event.ID == 0 {
		event.ID = s.nextEventID.Add(1)
	}

	s.mu.Lock()
	if len(s.events) == maxRequestEvents {
		copy(s.events, s.events[1:])
		s.events[len(s.events)-1] = event
	} else {
		s.events = append(s.events, event)
	}

	subscribers := make([]chan RequestEvent, 0, len(s.subscribers))
	for subscriber := range s.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.Unlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func writeEvent(w io.Writer, event RequestEvent) bool {
	data, err := json.Marshal(event)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.ID, data)
	return err == nil
}

func newRequestEvent(r *http.Request, requestBody loggedBody, response *responseCapture, status int, arrivedAt time.Time, elapsed time.Duration) RequestEvent {
	return RequestEvent{
		Request: EventRequest{
			Method:  r.Method,
			URL:     r.URL.RequestURI(),
			Time:    formatRequestTime(arrivedAt),
			Details: requestDetails(r, requestBody),
		},
		Response: EventResponse{
			Status:     status,
			StatusText: statusText(status),
			Time:       elapsed.Round(time.Microsecond).String(),
			Details:    responseDetails(r, response, status),
		},
	}
}

func formatRequestTime(t time.Time) string {
	return t.Local().Format("15:04:05")
}

func parseLastEventID(r *http.Request) uint64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// RoutesFromMethods is a helper for tests and CLI summaries.
func RoutesFromMethods(methods []restclient.Method) []RouteInfo {
	routes := make([]RouteInfo, 0, len(methods))
	for _, method := range methods {
		routes = append(routes, RouteInfo{
			Name:   method.Name,
			Method: method.Method,
			Path:   method.Path,
			Query:  method.Query.Encode(),
		})
	}
	return routes
}
