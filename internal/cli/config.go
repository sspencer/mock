package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func usageError(format string, args ...any) error {
	return &exitError{code: 2, msg: fmt.Sprintf(format, args...)}
}

func runError(format string, args ...any) error {
	return &exitError{code: 1, msg: fmt.Sprintf(format, args...)}
}

const defaultAdminPort = 8081

type config struct {
	Bind             string
	Port, AdminPort  int
	Version, NoAdmin bool
	Args             []string
	// Upstream receives requests no route matches (-u); nil answers 404.
	Upstream *url.URL
	// Strict reports unmatched requests and unused routes at shutdown and
	// fails the run when any request went unmatched (-s).
	Strict bool
	// Help holds the usage text when -h or --help was requested.
	Help string
}

const defaultBind = "127.0.0.1"

// usageText is printed by -h. Every flag has a short and a long name; Go's
// flag package accepts either with one or two dashes.
const usageText = `usage:
  mock [flags] <file.http> [file.http...]
  mock [flags] <directory>
  cat file.http | mock
  mock validate [-strict] [-json] <file.http> [file.http...]

flags:
  -p, --port PORT      default HTTP port, for routes whose request target or
                       @port gives no port (default 8080, or MOCK_PORT)
  -a, --ui-port PORT   admin UI port; 0 picks a random port (default
                       MOCK_UI_PORT, else 8081, or random if -p or MOCK_PORT
                       is set)
  -x, --no-ui          do not start the admin UI
  -b, --bind ADDR      bind address; use 0.0.0.0 for all interfaces
                       (default 127.0.0.1, or MOCK_BIND)
  -u, --upstream URL   forward requests no route matches to this http or
                       https URL and return its response
  -s, --strict         at shutdown, list requests no route matched and routes
                       that never matched, and exit 1 if any request was
                       unmatched
      --version        print version and exit
  -h, --help           print this help and exit
`

func parseConfig(args []string) (config, error) {
	flagSet := flag.NewFlagSet("mock", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var cfg config
	portDefault, envPortSet, err := portFromEnv("MOCK_PORT", 8080)
	if err != nil {
		return config{}, err
	}
	adminDefault, envAdminSet, err := portFromEnv("MOCK_UI_PORT", defaultAdminPort)
	if err != nil {
		return config{}, err
	}
	bindDefault := defaultBind
	if raw := strings.TrimSpace(os.Getenv("MOCK_BIND")); raw != "" {
		bindDefault = raw
	}
	// Each long name shares its short name's variable. The help text is
	// hand-written in usageText, so the per-flag usage strings stay empty.
	for _, name := range []string{"p", "port"} {
		flagSet.IntVar(&cfg.Port, name, portDefault, "")
	}
	for _, name := range []string{"a", "ui-port"} {
		flagSet.IntVar(&cfg.AdminPort, name, adminDefault, "")
	}
	for _, name := range []string{"x", "no-ui"} {
		flagSet.BoolVar(&cfg.NoAdmin, name, false, "")
	}
	for _, name := range []string{"b", "bind"} {
		flagSet.StringVar(&cfg.Bind, name, bindDefault, "")
	}
	var upstream string
	for _, name := range []string{"u", "upstream"} {
		flagSet.StringVar(&upstream, name, "", "")
	}
	for _, name := range []string{"s", "strict"} {
		flagSet.BoolVar(&cfg.Strict, name, false, "")
	}
	flagSet.BoolVar(&cfg.Version, "version", false, "")
	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return config{Help: usageText}, nil
		}
		return config{}, usageError("failed to parse flags: %v", err)
	}
	cfg.Args = flagSet.Args()
	if upstream != "" {
		if cfg.Upstream, err = parseUpstream(upstream); err != nil {
			return config{}, err
		}
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return config{}, usageError("port must be between 0 and 65535")
	}
	if cfg.AdminPort < 0 || cfg.AdminPort > 65535 {
		return config{}, usageError("admin port must be between 0 and 65535")
	}
	var adminSet, portSet bool
	flagSet.Visit(func(f *flag.Flag) {
		adminSet = adminSet || f.Name == "a" || f.Name == "ui-port"
		portSet = portSet || f.Name == "p" || f.Name == "port"
	})
	if cfg.NoAdmin && adminSet {
		return config{}, usageError("-a (--ui-port) and -x (--no-ui) cannot be used together")
	}
	if !adminSet && !envAdminSet && (portSet || envPortSet) {
		cfg.AdminPort = 0
	}
	if !cfg.NoAdmin && cfg.AdminPort != 0 && cfg.AdminPort == cfg.Port {
		return config{}, usageError("admin port %d is the same as the mock port; change -a or MOCK_UI_PORT (or -p or MOCK_PORT)", cfg.AdminPort)
	}
	return cfg, nil
}

// parseUpstream accepts an absolute http or https URL, optionally with a base
// path that request paths are appended to.
func parseUpstream(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, usageError("invalid --upstream %q: %v", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, usageError("invalid --upstream %q: use an http or https URL such as https://api.example.com", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return nil, usageError("invalid --upstream %q: only a scheme, host, port and base path are allowed", raw)
	}
	return u, nil
}

// portFromEnv returns the port named by the environment variable, or def when
// it is unset, and whether the variable supplied it.
func portFromEnv(name string, def int) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, false, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, usageError("invalid %s %q: must be an integer", name, raw)
	}
	return port, true, nil
}

// ExitCode maps an error returned by Run to a process exit code: 0 for nil,
// 2 for usage errors, and 1 otherwise.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.code
	}
	return 1
}
