package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

// These cases mirror internal/ui/tests/draft.test.mjs.

func TestDraftSectionPlaceholder(t *testing.T) {
	got := draftSection(exchange{Method: "GET", Target: "/users/42?page=2&page=3&sort=name", Port: 8080}, 8080)
	want := "### [200] GET /users/42\n@query.page = *\n@query.sort = *\nGET /users/42\nContent-Type: application/json\n\n{}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDraftSectionJSONBody(t *testing.T) {
	got := draftSection(exchange{
		Method: "POST", Target: "/users",
		RequestHeaders: http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer x"}},
		RequestBody:    `{"name":"Alex","age":3,"nick":null,"a.b":1,"tags":[]}`,
	}, 0)
	want := "### [200] POST /users\n@header.Authorization = *\n@json.name = *\n@json.age = *\n@json.tags = *\nPOST /users\nContent-Type: application/json\n\n{}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	for _, body := range []string{"[1,2]", "not json", `"x"`} {
		if got := draftSection(exchange{Method: "POST", Target: "/", RequestBody: body, RequestHeaders: http.Header{}}, 0); strings.Contains(got, "@json") {
			t.Errorf("body %s drafted %s", body, got)
		}
	}
}

func TestDraftSectionFormBody(t *testing.T) {
	got := draftSection(exchange{
		Method: "POST", Target: "/login",
		RequestHeaders: http.Header{"Content-Type": {"application/x-www-form-urlencoded; charset=utf-8"}},
		RequestBody:    "user=a&pass=b&user=c&bad%20name=1",
	}, 0)
	if !strings.Contains(got, "\n@form.user = *\n@form.pass = *\nPOST /login\n") || strings.Contains(got, "bad") {
		t.Fatalf("got\n%s", got)
	}
}

func TestDraftSectionRealResponse(t *testing.T) {
	got := draftSection(exchange{
		Method: "GET", Target: "/a/:b/*", Port: 9000,
		Status: 201, ResponseHeaders: http.Header{"Content-Type": {"application/json"}},
		ResponseBody: "{\"t\":\"{{x}}\"}\n\n", ResponseOK: true,
	}, 8080)
	want := "### [201] GET /a/%3Ab/%2A\n@port = 9000\n@template = off\nGET /a/%3Ab/%2A\nContent-Type: application/json\n\n{\"t\":\"{{x}}\"}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// A body that would end the section falls back to the placeholder.
	unsafe := draftSection(exchange{Method: "GET", Target: "/", Status: 200, ResponseBody: "a\n### b", ResponseOK: true}, 0)
	if !strings.HasSuffix(unsafe, "Content-Type: application/json\n\n{}\n") {
		t.Fatalf("got\n%s", unsafe)
	}
}

func TestDraftSectionsDeduplicate(t *testing.T) {
	text, count := draftSections([]exchange{
		{Method: "GET", Target: "/users?page=1"},
		{Method: "GET", Target: "/users?page=2", Status: 201, ResponseHeaders: http.Header{"Content-Type": {"application/json"}}, ResponseBody: `{"id":1}`, ResponseOK: true},
		{Method: "POST", Target: "/users"},
		{Method: "GET", Target: "/users"},
	}, 0)
	want := "### [201] GET /users\n@query.page = *\nGET /users\nContent-Type: application/json\n\n{\"id\":1}\n" +
		"\n### [200] POST /users\nPOST /users\nContent-Type: application/json\n\n{}\n"
	if count != 2 || text != want {
		t.Fatalf("count %d, got\n%s\nwant\n%s", count, text, want)
	}
	methods, err := restclient.Parse("drafts.http", strings.NewReader(text))
	if err != nil || len(methods) != 2 {
		t.Fatalf("drafts do not load: %v (%d routes)", err, len(methods))
	}
}
