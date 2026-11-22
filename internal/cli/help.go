package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const helpUsage = "usage: mock help [subcommand]"

// topHelpText renders the top-level help printed by mock --help and mock help:
// the synopsis lines, every subcommand with its description (shared with mock
// man), and the server flags.
func topHelpText() string {
	usage, flagsText, _ := strings.Cut(usageText, "\nflags:\n")
	model := commandLineModel()
	var b strings.Builder
	b.WriteString(strings.TrimRight(usage, "\n"))
	b.WriteString("\n\ncommands:\n")
	for _, sub := range model.subcommands {
		writeWrappedCommand(&b, sub.name, subcommandSummaries[sub.name])
	}
	b.WriteString("\nflags (for serve and run):\n")
	b.WriteString(flagsText)
	b.WriteString("\nRun \"mock help <command>\" for more information about a command.\n")
	return b.String()
}

// serveHelpText renders the help for mock serve (and mock help serve).
func serveHelpText() string {
	_, flagsText, _ := strings.Cut(usageText, "\nflags:\n")
	var b strings.Builder
	b.WriteString("usage:\n")
	b.WriteString("  mock serve [flags] <file.http> [file.http...]\n")
	b.WriteString("  mock serve [flags] -d <dir> [-d <dir>...] [file.http...]\n")
	b.WriteString("  mock serve [flags] <directory>\n")
	b.WriteString("  cat file.http | mock serve [flags]\n\n")
	b.WriteString(subcommandSummaries["serve"] + "\n\n")
	b.WriteString("flags:\n")
	b.WriteString(flagsText)
	return b.String()
}

// writeWrappedCommand formats "  name        description..." wrapped at ~78 columns.
func writeWrappedCommand(b *strings.Builder, name, summary string) {
	const (
		colWidth  = 14 // "  " + 10-char name + "  "
		lineLimit = 78
	)
	prefix := fmt.Sprintf("  %-10s  ", name)
	indent := strings.Repeat(" ", colWidth)
	b.WriteString(prefix)
	col := len(prefix)
	for i, word := range strings.Fields(summary) {
		if i > 0 {
			if col+1+len(word) > lineLimit {
				b.WriteByte('\n')
				b.WriteString(indent)
				col = colWidth
			} else {
				b.WriteByte(' ')
				col++
			}
		}
		b.WriteString(word)
		col += len(word)
	}
	b.WriteByte('\n')
}

// runHelp prints the top-level usage (like mock --help), or the help text
// (with description) for a single subcommand.
func runHelp(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mock help", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeSubcommandHelp(stdout, "help", helpUsage+"\n")
		}
		return usageError("%v\n%s", err, helpUsage)
	}
	if fs.NArg() == 0 {
		_, err := io.WriteString(stdout, topHelpText())
		return err
	}
	if fs.NArg() > 1 {
		return usageError("too many arguments\n%s", helpUsage)
	}
	sub := fs.Arg(0)
	if sub == "serve" {
		_, err := io.WriteString(stdout, serveHelpText())
		return err
	}
	if sub == "help" {
		return writeSubcommandHelp(stdout, "help", helpUsage+"\n")
	}

	var buf bytes.Buffer
	var err error
	switch sub {
	case "validate":
		err = runValidate([]string{"-h"}, &buf)
	case "check":
		err = runCheck(ctx, []string{"-h"}, &buf)
	case "import":
		err = runImport([]string{"-h"}, stdin, &buf, stderr)
	case "run":
		err = runRun(ctx, []string{"-h"}, stdin, &buf, stderr)
	case "match":
		err = runMatch([]string{"-h"}, &buf)
	case "routes":
		err = runRoutes([]string{"-h"}, &buf)
	case "send":
		err = runSend(ctx, []string{"-h"}, &buf)
	case "client":
		err = runClient([]string{"-h"}, &buf)
	case "wait":
		err = runWait(ctx, []string{"-h"}, &buf)
	case "init":
		err = runInit([]string{"-h"}, &buf)
	case "skill":
		err = runSkill([]string{"-h"}, &buf)
	case "fmt":
		err = runFmt([]string{"-h"}, stdin, &buf, stderr)
	case "completion":
		err = runCompletion([]string{"-h"}, &buf)
	case "man":
		err = runMan([]string{"-h"}, &buf)
	default:
		return usageError("unknown command %q\nrun \"mock help\" for usage", sub)
	}
	if err != nil {
		return err
	}
	return writeSubcommandHelp(stdout, sub, buf.String())
}

// writeSubcommandHelp inserts the subcommand's summary after its usage line.
func writeSubcommandHelp(w io.Writer, name, body string) error {
	summary := subcommandSummaries[name]
	first, rest, hasRest := strings.Cut(body, "\n")
	var b strings.Builder
	b.WriteString(first)
	b.WriteByte('\n')
	if summary != "" {
		b.WriteByte('\n')
		b.WriteString(summary)
		b.WriteByte('\n')
	}
	if hasRest && strings.TrimSpace(rest) != "" {
		if !strings.HasPrefix(rest, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(rest)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
