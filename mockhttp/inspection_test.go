package mockhttp

import (
	"encoding/json"
	"errors"
	"github.com/sspencer/mock/restclient"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectionCapturesMatchAndSequence(t *testing.T) {
	methods, err := restclient.Parse("fixtures.http", strings.NewReader("### first\nGET /x/:id\n\none\n### second\nGET /x/:id\n\ntwo"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, nil)
	for position := 1; position <= 2; position++ {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x/7", nil))
		match := s.events[len(s.events)-1].Match
		if match.Position != position || match.Total != 2 || match.Revision != 1 || match.Route.Source != "fixtures.http" || match.Route.Line < 1 {
			t.Fatalf("bad match: %+v", match)
		}
	}
	s.SetMethods(methods)
	if s.events[0].Match.Revision != 1 {
		t.Fatal("reload changed historical match metadata")
	}
}

func TestUnmatchedRequestExplainsConstraints(t *testing.T) {
	methods, err := restclient.Parse("fixtures.http", strings.NewReader("### protected\n# $header.Authorization=secret\nGET /x?kind=cat\n\nok"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, nil)
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x?kind=dog", nil))
	match := s.events[0].Match
	if match.Route != nil || len(match.Candidates) != 1 || len(match.Candidates[0].Reasons) != 3 {
		t.Fatalf("bad mismatch: %+v", match)
	}
	reasons := strings.Join(match.Candidates[0].Reasons, " ")
	for _, want := range []string{"method", "Query kind", "Header Authorization"} {
		if !strings.Contains(reasons, want) {
			t.Fatal(reasons)
		}
	}
	if strings.Contains(reasons, "secret") {
		t.Fatal("credential repeated in diagnostic")
	}
}

func TestClearAndResetAreIndependent(t *testing.T) {
	s := New(nil, nil)
	s.publishRequest(RequestEvent{})
	s.counters["sequence"] = 1
	clear := httptest.NewRequest("POST", "/clear", nil)
	clear.Header.Set("X-Requested-With", "xhr")
	s.ServeClear(httptest.NewRecorder(), clear)
	if len(s.events) != 0 || s.counters["sequence"] != 1 {
		t.Fatal("clear changed sequence")
	}
	s.publishRequest(RequestEvent{})
	reset := httptest.NewRequest("POST", "/reset", nil)
	reset.Header.Set("X-Requested-With", "xhr")
	s.ServeReset(httptest.NewRecorder(), reset)
	if len(s.events) != 1 || len(s.counters) != 0 {
		t.Fatal("reset changed traffic")
	}
	forbidden := httptest.NewRecorder()
	s.ServeReset(forbidden, httptest.NewRequest("POST", "/reset", nil))
	if forbidden.Code != 403 {
		t.Fatal("reset lacks origin protection")
	}
}

func TestReloadStatePreservesActiveRevisionOnFailure(t *testing.T) {
	s := New(nil, nil)
	s.BeginReload()
	s.ReloadFailed(errors.New("fixtures.http:3: invalid header"))
	response := httptest.NewRecorder()
	s.ServeState(response, httptest.NewRequest("GET", "/state", nil))
	var state ConfigState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Revision != 1 || state.LastAttempt == "" || state.LastSuccess == "" || state.Error == "" || state.Loading {
		t.Fatalf("bad state: %+v", state)
	}
	s.SetMethods(nil)
	if s.config.Revision != 2 || s.config.Error != "" {
		t.Fatal("successful reload did not clear error")
	}
}

func TestRouteInspectionPreservesEncodedLiteralPaths(t *testing.T) {
	methods, err := restclient.Parse("fixture.http", strings.NewReader("### literal\nGET /x/a%2Fb/%3Aid\n\nok"))
	if err != nil {
		t.Fatal(err)
	}
	info := describeRoute(methods[0], 0, 1)
	if info.Path != "/x/a%2Fb/%3Aid" {
		t.Fatalf("inspector changed path semantics: %s", info.Path)
	}
}
