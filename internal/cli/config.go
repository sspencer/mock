package cli

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
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
	// UIToken, when set, is required by the admin UI and API (-t, or
	// MOCK_UI_TOKEN).
	UIToken string
	// Redact masks credentials in the request log (-R).
	Redact bool
	Args   []string
	// Upstream receives requests no route matches (-u); nil answers 404.
	Upstream *url.URL
	// Record is the file that requests --upstream answered are appended to
	// as draft sections (-r).
	Record string
	// Strict reports unmatched requests and unused routes at shutdown and
	// fails the run when any request went unmatched (-s).
	Strict bool
	// Vars override or supply @name variables in every request file (-v).
	Vars varFlag
	// Headers are response headers for every route that sets no header of
	// that name and no @default.header for it (-H).
	Headers headerFlag
	// Env selects a REST Client environment whose values fill in variables
	// no @name defines (-e, -E).
	Env envFlags
	// Dirs are directories whose .http files are all loaded and watched (-d).
	Dirs listFlag
	// Poll, when set, checks the request files for changes on this interval
	// in addition to file events (-i).
	Poll time.Duration
	// Defaults is the latency and fault for routes that set no @delay or
	// @fault (-w, -f).
	Defaults mockhttp.RouteDefaults
	// LogFormat is "text" (the default) or "json" (-l). JSON writes one
	// object per line to stdout, including the startup banner and routes.
	LogFormat string
	// Help holds the usage text when -h or --help was requested.
	Help string
}

const defaultBind = "127.0.0.1"

// networkProfile is a named --delay, --fault and --throttle combination
// (-P). Keep usageText's list in step.
type networkProfile struct{ delay, fault, throttle string }

var networkProfiles = map[string]networkProfile{
	"slow":  {delay: "p50 800ms p99 3s"},
	"flaky": {delay: "p50 100ms p99 1s", fault: "reset 5%"},
	"3g":    {delay: "p50 300ms p99 1s", throttle: "96kb/s"},
}

// minPollInterval keeps -i from turning into a busy loop.
const minPollInterval = 100 * time.Millisecond

// usageText is printed by -h. Every flag has a short and a long name; Go's
// flag package accepts either with one or two dashes.
const usageText = `usage:
  mock [flags] <file.http> [file.http...]
  mock [flags] -d <dir> [-d <dir>...] [file.http...]
  mock [flags] <directory>
  cat file.http | mock
  mock validate [-strict] [-json] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...]
  mock init [-force] [dir]
  mock import [-host HOST] [-path PREFIX] <file.har|->
  mock check -upstream URL [-all] [-json] [-header 'Name: value'] [-var name=value] [-env NAME [-env-file FILE]] [-timeout 10s] <file.http|dir> [file.http|dir...]

flags:
  -p, --port PORT      default HTTP port, for routes whose request target or
                       @port gives no port (default 8080, or MOCK_PORT)
  -a, --ui-port PORT   admin UI port; 0 picks a random port (default
                       MOCK_UI_PORT, else 8081, or random if -p or MOCK_PORT
                       is set)
  -x, --no-ui          do not start the admin UI
  -t, --ui-token TOKEN require TOKEN for the admin UI and API: open the UI
                       as /?token=TOKEN or send "Authorization: Bearer TOKEN"
                       (default MOCK_UI_TOKEN)
  -R, --redact         mask credential-like headers, cookies, query
                       parameters and JSON or form fields in the request
                       log, the admin UI and exports
  -b, --bind ADDR      bind address; use 0.0.0.0 for all interfaces
                       (default 127.0.0.1, or MOCK_BIND)
  -u, --upstream URL   forward requests no route matches to this http or
                       https URL and return its response
  -r, --record FILE    with -u, append a draft section to FILE for each
                       method and path the upstream answered
  -s, --strict         at shutdown, list requests no route matched and routes
                       that never matched, and exit 1 if any request was
                       unmatched
  -v, --var NAME=VALUE set @NAME in every request file, overriding its
                       definitions; repeat for more variables
  -H, --header 'NAME: VALUE'
                       add this response header to every route that sets no
                       header of that name (and no @default.header for it);
                       repeat for more headers
  -e, --env NAME       use the REST Client environment NAME (with $shared)
                       from http-client.env.json and http-client.private.env.json,
                       else .vscode/settings.json, for variables no @name
                       defines
  -E, --env-file FILE  read -e environments from FILE instead
  -d, --dir DIR        load every .http file under DIR, after any file
                       arguments, and pick up added or removed files on
                       reload; repeat for more directories
  -i, --poll INTERVAL  also check request files for changes this often, such
                       as 1s, where file events do not arrive (Docker bind
                       mounts, network filesystems)
  -w, --delay DELAY    delay every route without its own @delay, such as
                       250ms, 50ms..300ms or "p50 80ms p99 900ms"
  -f, --fault FAULT    inject a fault into every route without its own
                       @fault: reset, truncate, hang, empty or garbage, with
                       an optional share such as "reset 5%"
  -T, --throttle RATE  send every route's body without its own @throttle at
                       RATE, such as 32kb/s
  -P, --profile NAME   a bad network in one word; -w, -f and -T override
                       its parts:
                         slow   delay p50 800ms p99 3s
                         flaky  delay p50 100ms p99 1s, fault reset 5%
                         3g     delay p50 300ms p99 1s, throttle 96kb/s
  -l, --log-format FMT text (default) or json: one JSON object per line on
                       stdout for startup, routes and each request
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
	for _, name := range []string{"t", "ui-token"} {
		flagSet.StringVar(&cfg.UIToken, name, os.Getenv("MOCK_UI_TOKEN"), "")
	}
	for _, name := range []string{"R", "redact"} {
		flagSet.BoolVar(&cfg.Redact, name, false, "")
	}
	for _, name := range []string{"b", "bind"} {
		flagSet.StringVar(&cfg.Bind, name, bindDefault, "")
	}
	var upstream string
	for _, name := range []string{"u", "upstream"} {
		flagSet.StringVar(&upstream, name, "", "")
	}
	for _, name := range []string{"r", "record"} {
		flagSet.StringVar(&cfg.Record, name, "", "")
	}
	for _, name := range []string{"s", "strict"} {
		flagSet.BoolVar(&cfg.Strict, name, false, "")
	}
	for _, name := range []string{"v", "var"} {
		flagSet.Var(&cfg.Vars, name, "")
	}
	for _, name := range []string{"H", "header"} {
		flagSet.Var(&cfg.Headers, name, "")
	}
	for _, name := range []string{"e", "env"} {
		flagSet.StringVar(&cfg.Env.Name, name, "", "")
	}
	for _, name := range []string{"E", "env-file"} {
		flagSet.StringVar(&cfg.Env.File, name, "", "")
	}
	for _, name := range []string{"d", "dir"} {
		flagSet.Var(&cfg.Dirs, name, "")
	}
	for _, name := range []string{"i", "poll"} {
		flagSet.DurationVar(&cfg.Poll, name, 0, "")
	}
	var delay, fault, throttle, profile string
	for _, name := range []string{"w", "delay"} {
		flagSet.StringVar(&delay, name, "", "")
	}
	for _, name := range []string{"f", "fault"} {
		flagSet.StringVar(&fault, name, "", "")
	}
	for _, name := range []string{"T", "throttle"} {
		flagSet.StringVar(&throttle, name, "", "")
	}
	for _, name := range []string{"P", "profile"} {
		flagSet.StringVar(&profile, name, "", "")
	}
	for _, name := range []string{"l", "log-format"} {
		flagSet.StringVar(&cfg.LogFormat, name, "text", "")
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
	if cfg.Record != "" && cfg.Upstream == nil {
		return config{}, usageError("-r (--record) needs -u (--upstream): only forwarded requests are recorded")
	}
	if err := cfg.Env.check("-E (--env-file)", "-e (--env)"); err != nil {
		return config{}, err
	}
	if profile != "" {
		p, ok := networkProfiles[strings.ToLower(profile)]
		if !ok {
			return config{}, usageError("-P (--profile) must be one of %s, not %q", strings.Join(slices.Sorted(maps.Keys(networkProfiles)), ", "), profile)
		}
		delay, fault, throttle = cmp.Or(delay, p.delay), cmp.Or(fault, p.fault), cmp.Or(throttle, p.throttle)
	}
	if throttle != "" {
		if cfg.Defaults.Throttle, err = restclient.ParseThrottle(throttle); err != nil {
			return config{}, usageError("-T (--throttle) %v", err)
		}
	}
	if delay != "" {
		parsed, err := restclient.ParseDelay(delay)
		if err != nil {
			return config{}, usageError("-w (--delay) %v", err)
		}
		cfg.Defaults.Delay, cfg.Defaults.DelayMax, cfg.Defaults.DelaySigma = parsed.Min, parsed.Max, parsed.Sigma
	}
	if fault != "" {
		if cfg.Defaults.Fault, cfg.Defaults.FaultRate, err = restclient.ParseFault(fault); err != nil {
			return config{}, usageError("-f (--fault) %v", err)
		}
	}
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return config{}, usageError("-l (--log-format) must be text or json, not %q", cfg.LogFormat)
	}
	if cfg.Poll != 0 && cfg.Poll < minPollInterval {
		return config{}, usageError("-i (--poll) must be at least %s", minPollInterval)
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return config{}, usageError("port must be between 0 and 65535")
	}
	if cfg.AdminPort < 0 || cfg.AdminPort > 65535 {
		return config{}, usageError("admin port must be between 0 and 65535")
	}
	var adminSet, portSet, tokenSet bool
	flagSet.Visit(func(f *flag.Flag) {
		adminSet = adminSet || f.Name == "a" || f.Name == "ui-port"
		portSet = portSet || f.Name == "p" || f.Name == "port"
		tokenSet = tokenSet || f.Name == "t" || f.Name == "ui-token"
	})
	if cfg.NoAdmin && adminSet {
		return config{}, usageError("-a (--ui-port) and -x (--no-ui) cannot be used together")
	}
	if cfg.NoAdmin && tokenSet {
		return config{}, usageError("-t (--ui-token) and -x (--no-ui) cannot be used together")
	}
	// Surrounding spaces (a trailing newline from a secrets file) are not
	// part of the token; ui.RequireToken trims the same way.
	cfg.UIToken = strings.TrimSpace(cfg.UIToken)
	if tokenSet && cfg.UIToken == "" {
		return config{}, usageError("-t (--ui-token) must not be empty")
	}
	if !adminSet && !envAdminSet && (portSet || envPortSet) {
		cfg.AdminPort = 0
	}
	if !cfg.NoAdmin && cfg.AdminPort != 0 && cfg.AdminPort == cfg.Port {
		return config{}, usageError("admin port %d is the same as the mock port; change -a or MOCK_UI_PORT (or -p or MOCK_PORT)", cfg.AdminPort)
	}
	return cfg, nil
}

// varFlag collects repeated -v NAME=VALUE flags.
type varFlag map[string]string

func (v *varFlag) String() string { return "" }

func (v *varFlag) Set(raw string) error {
	name, value, ok := strings.Cut(raw, "=")
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "@"))
	if !ok {
		return fmt.Errorf("%q: use NAME=VALUE", raw)
	}
	if err := restclient.CheckVariableOverride(name); err != nil {
		return err
	}
	if *v == nil {
		*v = varFlag{}
	}
	(*v)[name] = value
	return nil
}

// listFlag collects a repeated string flag.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("empty value")
	}
	*l = append(*l, raw)
	return nil
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
