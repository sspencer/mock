package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseConfigDefaultBind(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	cfg, err := parseConfig([]string{"api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Bind != "127.0.0.1" {
		t.Fatalf("Bind = %q, want 127.0.0.1", cfg.Bind)
	}
}

func TestParseConfigAdminPort(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		args      []string
		port      int
		adminPort int
	}{
		{"defaults", "", nil, 8080, 8081},
		{"p flag picks random admin", "", []string{"-p", "9000"}, 9000, 0},
		{"MOCK_PORT picks random admin", "9091", nil, 9091, 0},
		{"explicit admin with p", "", []string{"-p", "9000", "-a", "9100"}, 9000, 9100},
		{"explicit admin only", "", []string{"-a", "9100"}, 8080, 9100},
		{"admin zero", "", []string{"-a", "0"}, 8080, 0},
		{"explicit admin 8081 with p", "", []string{"-p", "9000", "-a", "8081"}, 9000, 8081},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MOCK_PORT", tt.env)
			cfg, err := parseConfig(append(tt.args, "api.http"))
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if cfg.Port != tt.port || cfg.AdminPort != tt.adminPort {
				t.Fatalf("Port, AdminPort = %d, %d; want %d, %d", cfg.Port, cfg.AdminPort, tt.port, tt.adminPort)
			}
		})
	}
}

func TestParseConfigMockUIPort(t *testing.T) {
	tests := []struct {
		name      string
		portEnv   string
		uiEnv     string
		args      []string
		port      int
		adminPort int
	}{
		{"env sets admin", "", "9200", nil, 8080, 9200},
		{"env admin survives -p", "", "9200", []string{"-p", "9000"}, 9000, 9200},
		{"env admin survives MOCK_PORT", "9091", "9200", nil, 9091, 9200},
		{"flag overrides env", "", "9200", []string{"-a", "9300"}, 8080, 9300},
		{"env zero is random", "", "0", nil, 8080, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MOCK_PORT", tt.portEnv)
			t.Setenv("MOCK_UI_PORT", tt.uiEnv)
			cfg, err := parseConfig(append(tt.args, "api.http"))
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if cfg.Port != tt.port || cfg.AdminPort != tt.adminPort {
				t.Fatalf("Port, AdminPort = %d, %d; want %d, %d", cfg.Port, cfg.AdminPort, tt.port, tt.adminPort)
			}
		})
	}
}

func TestParseConfigRejectsPortCollisionsAndBadMockUIPort(t *testing.T) {
	tests := []struct {
		name    string
		portEnv string
		uiEnv   string
		args    []string
	}{
		{"env admin equals default mock port", "", "8080", nil},
		{"env admin equals -p", "", "9000", []string{"-p", "9000"}},
		{"env admin equals MOCK_PORT", "9000", "9000", nil},
		{"-a equals MOCK_PORT", "9000", "", []string{"-a", "9000"}},
		{"non-numeric", "", "nope", nil},
		{"out of range", "", "65536", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MOCK_PORT", tt.portEnv)
			t.Setenv("MOCK_UI_PORT", tt.uiEnv)
			if _, err := parseConfig(append(tt.args, "api.http")); err == nil {
				t.Fatal("parseConfig() error = nil, want error")
			}
		})
	}
}

func TestParseConfigNoAdmin(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	t.Setenv("MOCK_UI_PORT", "")
	cfg, err := parseConfig([]string{"-x", "api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.NoAdmin {
		t.Fatal("NoAdmin = false, want true")
	}
	// The admin port is unused, so a matching MOCK_UI_PORT is not a collision.
	t.Setenv("MOCK_UI_PORT", "8080")
	if _, err := parseConfig([]string{"-x", "api.http"}); err != nil {
		t.Fatalf("parseConfig(-x) with colliding MOCK_UI_PORT error = %v", err)
	}
	if _, err := parseConfig([]string{"-x", "-a", "9100", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted -x with -a")
	}
}

func TestParseConfigRejectsMountFlag(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	if _, err := parseConfig([]string{"-l", "inspect", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted removed -l flag")
	}
}

func TestParseConfigRejectsTLSFlags(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	for _, flag := range []string{"-cert", "-key"} {
		if _, err := parseConfig([]string{flag, "x.pem", "api.http"}); err == nil {
			t.Fatalf("parseConfig() accepted removed %s flag", flag)
		}
	}
}

func TestParseConfigRejectsCORSFlag(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	if _, err := parseConfig([]string{"-cors", "*", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted removed -cors flag")
	}
}

func TestParseConfigLongFlags(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	t.Setenv("MOCK_UI_PORT", "")
	t.Setenv("MOCK_BIND", "")
	for _, prefix := range []string{"-", "--"} {
		cfg, err := parseConfig([]string{prefix + "port", "9000", prefix + "ui-port", "9001", prefix + "bind", "0.0.0.0", "api.http"})
		if err != nil {
			t.Fatalf("parseConfig(%s long flags) error = %v", prefix, err)
		}
		if cfg.Port != 9000 || cfg.AdminPort != 9001 || cfg.Bind != "0.0.0.0" {
			t.Fatalf("parseConfig(%s long flags) = %+v", prefix, cfg)
		}
		cfg, err = parseConfig([]string{prefix + "no-ui", "api.http"})
		if err != nil || !cfg.NoAdmin {
			t.Fatalf("parseConfig(%sno-ui) = %+v, %v", prefix, cfg, err)
		}
	}
	// --port counts as setting the mock port, so the UI port turns random.
	cfg, err := parseConfig([]string{"--port", "9000", "api.http"})
	if err != nil || cfg.AdminPort != 0 {
		t.Fatalf("parseConfig(--port) AdminPort = %d, err = %v; want 0", cfg.AdminPort, err)
	}
	// --ui-port counts as setting the admin port, so it conflicts with --no-ui.
	if _, err := parseConfig([]string{"--no-ui", "--ui-port", "9001", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted --no-ui with --ui-port")
	}
	if _, err := parseConfig([]string{"--port", "9000", "--ui-port", "9000", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted the same --port and --ui-port")
	}
}

func TestParseConfigBindFromEnv(t *testing.T) {
	t.Setenv("MOCK_BIND", "")
	cfg, err := parseConfig([]string{"api.http"})
	if err != nil || cfg.Bind != "127.0.0.1" {
		t.Fatalf("default Bind = %q, err = %v; want 127.0.0.1", cfg.Bind, err)
	}
	t.Setenv("MOCK_BIND", " 0.0.0.0 ")
	if cfg, err = parseConfig([]string{"api.http"}); err != nil || cfg.Bind != "0.0.0.0" {
		t.Fatalf("MOCK_BIND Bind = %q, err = %v; want 0.0.0.0", cfg.Bind, err)
	}
	if cfg, err = parseConfig([]string{"-b", "192.168.1.5", "api.http"}); err != nil || cfg.Bind != "192.168.1.5" {
		t.Fatalf("-b over MOCK_BIND Bind = %q, err = %v; want 192.168.1.5", cfg.Bind, err)
	}
}

func TestParseConfigAndVersion(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	cfg, err := parseConfig([]string{"-p", "9090", "-b", "127.0.0.1", "api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Port != 9090 || cfg.Bind != "127.0.0.1" || len(cfg.Args) != 1 {
		t.Fatalf("config = %#v", cfg)
	}

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Run(context.Background(), []string{"-version"}, strings.NewReader(""), &out, io.Discard, logger); err != nil {
		t.Fatalf("Run(-version) error = %v", err)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Fatalf("version output = %q, want dev", out.String())
	}
}

func TestParseConfigDefaultPortWithoutEnv(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	cfg, err := parseConfig([]string{"api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", cfg.Port)
	}
}

func TestParseConfigUsesMockPortEnvAsDefault(t *testing.T) {
	t.Setenv("MOCK_PORT", "9091")
	cfg, err := parseConfig([]string{"api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Port != 9091 {
		t.Fatalf("Port = %d, want 9091", cfg.Port)
	}
}

func TestParseConfigFlagOverridesMockPortEnv(t *testing.T) {
	t.Setenv("MOCK_PORT", "9091")
	cfg, err := parseConfig([]string{"-p", "3000", "api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Port != 3000 {
		t.Fatalf("Port = %d, want 3000 from -p", cfg.Port)
	}
}

func TestParseConfigRejectsInvalidMockPort(t *testing.T) {
	t.Setenv("MOCK_PORT", "nope")
	_, err := parseConfig([]string{"api.http"})
	if err == nil {
		t.Fatal("parseConfig() error = nil, want invalid MOCK_PORT error")
	}
	if !strings.Contains(err.Error(), "MOCK_PORT") {
		t.Fatalf("error = %q, want MOCK_PORT", err)
	}
}

func TestConfigRejectsInvalidPorts(t *testing.T) {
	// The -a 8080 case relies on the default mock port, so ignore the caller's environment.
	t.Setenv("MOCK_PORT", "")
	t.Setenv("MOCK_UI_PORT", "")
	for _, args := range [][]string{{"-p", "65536"}, {"-p", "-1"}, {"-a", "65536"}, {"-a", "-1"}, {"-p", "9000", "-a", "9000"}, {"-a", "8080"}} {
		if _, err := parseConfig(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestParseConfigUpstream(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	t.Setenv("MOCK_UI_PORT", "")
	t.Setenv("MOCK_BIND", "")
	for _, flag := range []string{"-u", "--upstream", "-upstream"} {
		cfg, err := parseConfig([]string{flag, "https://api.example.com:8443/v2", "api.http"})
		if err != nil {
			t.Fatalf("parseConfig(%s) error = %v", flag, err)
		}
		if cfg.Upstream == nil || cfg.Upstream.String() != "https://api.example.com:8443/v2" {
			t.Fatalf("parseConfig(%s) Upstream = %v", flag, cfg.Upstream)
		}
	}
	cfg, err := parseConfig([]string{"api.http"})
	if err != nil || cfg.Upstream != nil {
		t.Fatalf("parseConfig() Upstream = %v, err = %v; want nil", cfg.Upstream, err)
	}
	for _, raw := range []string{
		"api.example.com",
		"ftp://api.example.com",
		"https://",
		"https://api.example.com/?key=1",
		"https://api.example.com/#top",
		"https://user:pass@api.example.com",
		"https://api.example.com/?",
	} {
		_, err := parseConfig([]string{"-u", raw, "api.http"})
		if err == nil {
			t.Fatalf("parseConfig(-u %q) error = nil, want usage error", raw)
		}
		if ExitCode(err) != 2 {
			t.Fatalf("parseConfig(-u %q) exit code = %d, want 2", raw, ExitCode(err))
		}
	}
}

func TestParseConfigVars(t *testing.T) {
	cfg, err := parseConfig([]string{"-v", "host=http://localhost:9000", "--var", "@token=a=b", "-v", "empty=", "api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	want := map[string]string{"host": "http://localhost:9000", "token": "a=b", "empty": ""}
	if len(cfg.Vars) != len(want) {
		t.Fatalf("Vars = %v, want %v", cfg.Vars, want)
	}
	for name, value := range want {
		if cfg.Vars[name] != value {
			t.Errorf("Vars[%q] = %q, want %q", name, cfg.Vars[name], value)
		}
	}
	for _, bad := range []string{"novalue", "header.Authorization=x", "1x=y", "=y"} {
		if _, err := parseConfig([]string{"-v", bad, "api.http"}); ExitCode(err) != 2 {
			t.Errorf("-v %q: error = %v, want usage error", bad, err)
		}
	}
}

func TestParseConfigDelayAndFault(t *testing.T) {
	cfg, err := parseConfig([]string{"--delay", "50ms..300ms", "-f", "reset 5%", "a.http"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Delay != 50*time.Millisecond || cfg.Defaults.DelayMax != 300*time.Millisecond ||
		cfg.Defaults.Fault != "reset" || cfg.Defaults.FaultRate != 0.05 {
		t.Errorf("defaults = %+v", cfg.Defaults)
	}
	for _, args := range [][]string{{"-w", "soon", "a.http"}, {"--fault", "explode", "a.http"}} {
		if _, err := parseConfig(args); err == nil {
			t.Errorf("parseConfig(%q) error = nil", args)
		}
	}
}

func TestParseConfigRedact(t *testing.T) {
	for _, args := range [][]string{{"-R", "api.http"}, {"--redact", "api.http"}} {
		if cfg, err := parseConfig(args); err != nil || !cfg.Redact {
			t.Errorf("%v: redact=%v, %v", args, cfg.Redact, err)
		}
	}
	if cfg, _ := parseConfig([]string{"api.http"}); cfg.Redact {
		t.Error("redaction should be off by default")
	}
}

func TestParseConfigUIToken(t *testing.T) {
	t.Setenv("MOCK_UI_TOKEN", "")
	cfg, err := parseConfig([]string{"-t", "abc", "api.http"})
	if err != nil || cfg.UIToken != "abc" {
		t.Fatalf("-t: %+v, %v", cfg.UIToken, err)
	}
	t.Setenv("MOCK_UI_TOKEN", "fromenv")
	if cfg, err := parseConfig([]string{"api.http"}); err != nil || cfg.UIToken != "fromenv" {
		t.Fatalf("env: %q, %v", cfg.UIToken, err)
	}
	if cfg, err := parseConfig([]string{"--ui-token", "flag", "api.http"}); err != nil || cfg.UIToken != "flag" {
		t.Fatalf("flag beats env: %q, %v", cfg.UIToken, err)
	}
	for _, args := range [][]string{{"-t", "x", "-x", "api.http"}, {"-t", " ", "api.http"}} {
		if _, err := parseConfig(args); ExitCode(err) != 2 {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestParseConfigHeaders(t *testing.T) {
	cfg, err := parseConfig([]string{"-H", "Content-Type: application/json", "--header", "X-Env:  dev", "api.http"})
	if err != nil {
		t.Fatal(err)
	}
	if got := http.Header(cfg.Headers); got.Get("Content-Type") != "application/json" || got.Get("X-Env") != "dev" {
		t.Errorf("Headers = %v", got)
	}
	if _, err := parseConfig([]string{"-H", "no colon", "api.http"}); ExitCode(err) != 2 {
		t.Errorf("bad -H error = %v", err)
	}
}

func TestParseConfigProfilesAndThrottle(t *testing.T) {
	cfg, err := parseConfig([]string{"-P", "flaky", "-f", "truncate 1%", "api.http"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Delay != 100*time.Millisecond || cfg.Defaults.DelaySigma == 0 || cfg.Defaults.Fault != "truncate" || cfg.Defaults.FaultRate != 0.01 || cfg.Defaults.Throttle != 0 {
		t.Errorf("flaky with -f = %+v", cfg.Defaults)
	}
	cfg, err = parseConfig([]string{"--profile", "3G", "--throttle", "8kb/s", "api.http"})
	if err != nil || cfg.Defaults.Throttle != 8*1024 || cfg.Defaults.Delay != 300*time.Millisecond {
		t.Errorf("3g with -T = %+v, %v", cfg.Defaults, err)
	}
	for _, args := range [][]string{{"-P", "dialup", "api.http"}, {"-T", "fast", "api.http"}, {"-w", "lognormal 1s 9", "api.http"}} {
		if _, err := parseConfig(args); ExitCode(err) != 2 {
			t.Errorf("parseConfig(%v) error = %v", args, err)
		}
	}
	if !strings.Contains(usageText, "flaky  delay p50 100ms p99 1s, fault reset 5%") {
		t.Error("usage text does not list the flaky profile as defined")
	}
	for name, p := range networkProfiles {
		if !strings.Contains(usageText, name+" ") || (p.throttle != "" && !strings.Contains(usageText, "throttle "+p.throttle)) {
			t.Errorf("usage text is out of step with profile %q", name)
		}
	}
}
