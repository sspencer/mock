package ui

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// tokenCookie is the cookie that holds the admin token after a browser
// opens the UI with ?token=. Cookies are shared by every port on a host, so
// the name carries the admin port to keep two mock servers apart.
func tokenCookie(r *http.Request) string {
	name := "mock_ui_token"
	if _, port, err := net.SplitHostPort(r.Host); err == nil && port != "" {
		name += "_" + port
	}
	return name
}

// RequireToken wraps the admin handler so every page and API request must
// present token: as an "Authorization: Bearer" header, a ?token= query
// parameter, or the cookie a browser receives after opening the UI with
// ?token=. An empty token returns h unchanged. Requests without the token
// get 401. This is access control on plain HTTP: anyone who can read the
// traffic can read the token.
func RequireToken(h http.Handler, token string) http.Handler {
	if token == "" {
		return h
	}
	want := sha256.Sum256([]byte(token))
	matches := func(got string) bool {
		sum := sha256.Sum256([]byte(got))
		return got != "" && subtle.ConstantTimeCompare(sum[:], want[:]) == 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query := r.URL.Query().Get("token"); query != "" {
			if !matches(query) {
				unauthorized(w)
				return
			}
			// A browser page load trades the query parameter for a cookie and
			// reloads without it, so the token leaves the address bar.
			if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html") {
				http.SetCookie(w, &http.Cookie{Name: tokenCookie(r), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				clean := *r.URL
				q := clean.Query()
				q.Del("token")
				clean.RawQuery = q.Encode()
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, clean.RequestURI(), http.StatusSeeOther)
				return
			}
			h.ServeHTTP(w, r)
			return
		}
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && matches(strings.TrimSpace(bearer)) {
			h.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie(tokenCookie(r)); err == nil && matches(cookie.Value) {
			h.ServeHTTP(w, r)
			return
		}
		unauthorized(w)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mock admin"`)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("mock admin UI: this server requires a token (--ui-token or MOCK_UI_TOKEN).\n" +
		"Open the UI as /?token=TOKEN, or send \"Authorization: Bearer TOKEN\".\n"))
}
