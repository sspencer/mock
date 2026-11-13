package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadyFileWaitAndH2C(t *testing.T) {
	dir := t.TempDir()
	api := filepath.Join(dir, "api.http")
	if err := os.WriteFile(api, []byte("### Hello\nGET /hello\n\nhi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(dir, "ready.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		done <- Run(ctx, []string{"-p", "0", "-a", "0", "--h2c", "--ready-file", readyPath, api}, nil, io.Discard, io.Discard, logger)
	}()

	var out bytes.Buffer
	if err := runWait(context.Background(), []string{"-timeout", "5s", "-file", readyPath}, &out); err != nil {
		cancel()
		t.Fatalf("mock wait: %v", err)
	}
	mockURL := strings.TrimSpace(out.String())
	if !strings.HasPrefix(mockURL, "http://127.0.0.1:") {
		t.Fatalf("mock wait printed %q, want the mock URL", mockURL)
	}

	// The mock port speaks HTTP/2 with prior knowledge.
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: protocols}, Timeout: 5 * time.Second}
	resp, err := client.Get(mockURL + "/hello")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.ProtoMajor != 2 || string(body) != "hi" {
		t.Fatalf("got %s %q, want HTTP/2 and hi", resp.Proto, body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(readyPath); !os.IsNotExist(err) {
		t.Fatalf("ready file should be removed at shutdown, stat err = %v", err)
	}
}

func TestWaitTimesOut(t *testing.T) {
	err := runWait(context.Background(), []string{"-timeout", "200ms", "-q", "-file", filepath.Join(t.TempDir(), "never.json")}, io.Discard)
	if ExitCode(err) != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	if err := runWait(context.Background(), []string{"-timeout", "200ms", "http://127.0.0.1:1"}, io.Discard); ExitCode(err) != 1 {
		t.Fatalf("err = %v, want exit 1 for an address that never answers", err)
	}
	if err := runWait(context.Background(), []string{"-file", "a", "http://x"}, io.Discard); ExitCode(err) != 2 {
		t.Fatalf("err = %v, want a usage error", err)
	}
}
