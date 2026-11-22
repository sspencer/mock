package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpMatchesHelpFlagAndListsDescriptions(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	var byFlag, byCmd bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, strings.NewReader(""), &byFlag, io.Discard, logger); err != nil {
		t.Fatalf("Run(--help) error = %v", err)
	}
	if err := Run(context.Background(), []string{"help"}, strings.NewReader(""), &byCmd, io.Discard, logger); err != nil {
		t.Fatalf("Run(help) error = %v", err)
	}
	if byCmd.String() != byFlag.String() {
		t.Fatalf("mock help output differs from mock --help")
	}
	// Every subcommand and its description from subcommandSummaries should appear.
	got := byCmd.String()
	if !strings.Contains(got, "\ncommands:\n") {
		t.Fatalf("mock help missing commands section:\n%s", got)
	}
	for _, sub := range commandLineModel().subcommandNames() {
		// Check the first few words of the summary (allowing line wraps).
		words := strings.Fields(subcommandSummaries[sub])
		snippet := strings.Join(words[:min(4, len(words))], " ")
		if !strings.Contains(got, snippet) {
			t.Errorf("mock help missing description for %s (%q):\n%s", sub, snippet, got)
		}
	}
}

func TestHelpCoversEverySubcommand(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for _, sub := range commandLineModel().subcommandNames() {
		var out bytes.Buffer
		err := Run(context.Background(), []string{"help", sub}, strings.NewReader(""), &out, io.Discard, logger)
		if err != nil {
			t.Errorf("mock help %s error = %v", sub, err)
			continue
		}
		if !strings.Contains(out.String(), "usage:") {
			t.Errorf("mock help %s output lacks usage:\n%s", sub, out.String())
		}
		if !strings.Contains(out.String(), subcommandSummaries[sub]) {
			t.Errorf("mock help %s output lacks description %q:\n%s", sub, subcommandSummaries[sub], out.String())
		}
	}
}

func TestHelpAndMissingCommandErrors(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for _, args := range [][]string{
		{},
		{"api.http"},
		{"-p", "8080", "api.http"},
		{"help", "nope"},
		{"help", "run", "extra"},
	} {
		err := Run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard, logger)
		if ExitCode(err) != 2 {
			t.Errorf("Run(%q) ExitCode = %d (%v), want 2", args, ExitCode(err), err)
		}
	}
	// Bare filename should point the user at `mock serve`.
	err := Run(context.Background(), []string{"api.http"}, strings.NewReader(""), io.Discard, io.Discard, logger)
	if !strings.Contains(err.Error(), "mock serve") {
		t.Errorf("Run(api.http) error = %q, want hint about mock serve", err)
	}
}

func TestServeSubcommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	logger := slog.New(slog.DiscardHandler)
	if err := Run(cancelledContext(), []string{"serve", "-p", "0", "-a", "0", path}, strings.NewReader(""), &out, io.Discard, logger); err != nil {
		t.Fatalf("Run(serve) error = %v", err)
	}
	for _, want := range []string{"starting mock HTTP server", "admin UI at", "Available mock methods:", "GET", "/users"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout = %q, want %q", out.String(), want)
		}
	}

	// mock serve -h prints serve help without starting a server.
	out.Reset()
	if err := Run(context.Background(), []string{"serve", "-h"}, strings.NewReader(""), &out, io.Discard, logger); err != nil {
		t.Fatalf("Run(serve -h) error = %v", err)
	}
	if out.String() != serveHelpText() {
		t.Errorf("mock serve -h did not print serveHelpText()")
	}

	// mock run -h also prints run help without requiring --.
	out.Reset()
	if err := Run(context.Background(), []string{"run", "-h"}, strings.NewReader(""), &out, io.Discard, logger); err != nil {
		t.Fatalf("Run(run -h) error = %v", err)
	}
	if !strings.Contains(out.String(), runUsage) {
		t.Errorf("mock run -h = %q, want %q", out.String(), runUsage)
	}
}
