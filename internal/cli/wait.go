package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// writeReadyFile writes the bound addresses as JSON (--ready-file). It
// writes a temporary file and renames it, so a reader never sees half of it.
func writeReadyFile(path string, ready serving) error {
	ready.PID = os.Getpid()
	data, err := json.MarshalIndent(ready, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mock-ready-*")
	if err != nil {
		return fmt.Errorf("--ready-file: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("--ready-file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("--ready-file: %w", err)
	}
	return nil
}

// waitPollInterval is how often mock wait checks again.
const waitPollInterval = 100 * time.Millisecond

// runWait is mock wait: block until a running mock answers. With -file it
// waits for a --ready-file to appear and then for that server's admin
// /state; with a URL (or the default admin address) it waits for /state.
func runWait(ctx context.Context, args []string, stdout io.Writer) error {
	flagSet := flag.NewFlagSet("mock wait", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	timeout := flagSet.Duration("timeout", 10*time.Second, "")
	file := flagSet.String("file", "", "")
	token := flagSet.String("token", os.Getenv("MOCK_UI_TOKEN"), "")
	quiet := flagSet.Bool("q", false, "")
	const usage = "usage: mock wait [-timeout 10s] [-token TOKEN] [-q] [-file FILE | URL]"
	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, usage)
			return nil
		}
		return usageError("%v\n%s", err, usage)
	}
	if flagSet.NArg() > 1 || (*file != "" && flagSet.NArg() > 0) {
		return usageError("name a ready file (-file) or one admin URL, not both\n%s", usage)
	}
	if *timeout <= 0 {
		return usageError("-timeout must be positive\n%s", usage)
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	base := flagSet.Arg(0)
	if *file == "" && base == "" {
		port, _, err := portFromEnv("MOCK_UI_PORT", defaultAdminPort)
		if err != nil {
			return err
		}
		base = "http://127.0.0.1:" + strconv.Itoa(port)
	}
	if !strings.Contains(base, "://") && base != "" {
		base = "http://" + base
	}
	var ready serving
	if *file != "" {
		var err error
		if ready, err = waitForReadyFile(ctx, *file); err != nil {
			return runError("mock wait: %s did not appear within %s", *file, *timeout)
		}
		base = ready.AdminURL
	}
	if base != "" {
		if _, err := url.Parse(base); err != nil {
			return usageError("invalid URL %q: %v", base, err)
		}
		if err := waitForState(ctx, strings.TrimRight(base, "/")+"/state", strings.TrimSpace(*token)); err != nil {
			return runError("mock wait: %s did not answer within %s: %v", base, *timeout, err)
		}
	}
	if !*quiet {
		if ready.MockURL != "" {
			fmt.Fprintln(stdout, ready.MockURL)
		} else {
			fmt.Fprintln(stdout, "ready")
		}
	}
	return nil
}

// waitForReadyFile polls until path holds a complete ready file.
func waitForReadyFile(ctx context.Context, path string) (serving, error) {
	for {
		var ready serving
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &ready) == nil && ready.MockURL != "" {
			return ready, nil
		}
		select {
		case <-ctx.Done():
			return serving{}, ctx.Err()
		case <-time.After(waitPollInterval):
		}
	}
}

// waitForState polls the admin /state endpoint until it answers 200.
func waitForState(ctx context.Context, stateURL, token string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	last := errors.New("no answer")
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, stateURL, nil)
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if resp, err := client.Do(req); err != nil {
			last = err
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("status %s", resp.Status)
			if resp.StatusCode == http.StatusUnauthorized {
				return errors.New("401 Unauthorized: pass -token or set MOCK_UI_TOKEN")
			}
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(waitPollInterval):
		}
	}
}
