package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	if RequireToken(inner, "") == nil {
		t.Fatal("empty token should return the handler")
	}
	h := RequireToken(inner, "s3cret")
	do := func(target string, edit func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Host = "127.0.0.1:8081"
		if edit != nil {
			edit(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("/routes", nil); rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := do("/routes", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	if rec := do("/routes", func(r *http.Request) { r.Header.Set("Authorization", "Bearer s3cret") }); rec.Code != 200 {
		t.Fatalf("bearer: %d", rec.Code)
	}
	if rec := do("/routes?token=s3cret", nil); rec.Code != 200 {
		t.Fatalf("query on API: %d", rec.Code)
	}
	if rec := do("/routes?token=nope", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong query: %d", rec.Code)
	}
	rec := do("/?token=s3cret&x=1", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/?x=1" {
		t.Fatalf("page login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "mock_ui_token_8081" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie %+v", cookies)
	}
	if rec := do("/events", func(r *http.Request) { r.AddCookie(cookies[0]) }); rec.Code != 200 {
		t.Fatalf("cookie: %d", rec.Code)
	}
	if rec := do("/events", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "mock_ui_token_8081", Value: "x"}) }); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad cookie: %d", rec.Code)
	}
}
