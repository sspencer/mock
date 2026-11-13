package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, WriteTimeout: 30 * time.Second}
}

// newMockHTTPServer is newHTTPServer for a mock port; with h2c it also
// accepts unencrypted HTTP/2 (prior knowledge, no Upgrade).
func newMockHTTPServer(handler http.Handler, h2c bool) *http.Server {
	server := newHTTPServer(handler)
	if h2c {
		server.Protocols = new(http.Protocols)
		server.Protocols.SetHTTP1(true)
		server.Protocols.SetUnencryptedHTTP2(true)
	}
	return server
}

func shutdownHTTPServers(servers []*http.Server, grace time.Duration) error {
	var errs []error
	for _, server := range servers {
		errs = append(errs, shutdownHTTPServer(server, grace))
	}
	return errors.Join(errs...)
}

func shutdownHTTPServer(server *http.Server, grace time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := server.Shutdown(ctx)
	closeErr := server.Close()
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.DeadlineExceeded) {
		if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			return closeErr
		}
		return nil
	}
	return err
}

func listenAddress(bind string, port int) string {
	return net.JoinHostPort(bind, fmt.Sprintf("%d", port))
}

// boundAddress reports the bind host with the port the listener actually got,
// which differs from the requested port when it was 0.
func boundAddress(bind string, ln net.Listener) string {
	return listenAddress(bind, ln.Addr().(*net.TCPAddr).Port)
}

func bindsAllInterfaces(bind string) bool {
	host := strings.Trim(bind, "[]")
	return host == "" || host == "0.0.0.0" || host == "::" || host == "*"
}

func newStaticFileHandler(dir string) http.Handler {
	return http.FileServer(http.Dir(dir))
}
