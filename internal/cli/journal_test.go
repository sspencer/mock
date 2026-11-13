package cli

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestJournalAppendsRedactedEvents(t *testing.T) {
	file := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(file, []byte("{\"id\":0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	methods, err := restclient.Parse("api.http", strings.NewReader("### Users\nGET /users\n\n[]\n"))
	if err != nil {
		t.Fatal(err)
	}
	server := mockhttp.New(methods, slog.New(slog.DiscardHandler))
	t.Cleanup(server.Close)
	server.SetRedact(true)
	j, err := openJournal(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.SetJournal(j.write)
	front := httptest.NewServer(server)
	t.Cleanup(front.Close)
	send(t, "GET", front.URL+"/users", "", http.Header{"Authorization": {"Bearer secret"}})
	send(t, "GET", front.URL+"/nowhere", "", nil)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	var events []mockhttp.RequestEvent
	scanner := bufio.NewScanner(data)
	for scanner.Scan() {
		var event mockhttp.RequestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("line %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if len(events) != 3 {
		t.Fatalf("got %d lines, want the old one and 2 events", len(events))
	}
	first, second := events[1], events[2]
	if first.ID != 1 || second.ID != 2 || first.Match.Route == nil || first.Match.Route.Name != "Users" || second.Match.Route != nil {
		t.Fatalf("events = %+v", events[1:])
	}
	if got := first.Request.Headers.Get("Authorization"); strings.Contains(got, "secret") {
		t.Errorf("Authorization = %q, want it masked", got)
	}
}

func TestJournalFlag(t *testing.T) {
	cfg, err := parseConfig([]string{"--journal", "r.jsonl", "a.http"})
	if err != nil || cfg.Journal != "r.jsonl" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	if _, err := openJournal(filepath.Join(t.TempDir(), "missing", "r.jsonl"), nil); err == nil {
		t.Fatal("opened a journal in a missing directory")
	}
}
