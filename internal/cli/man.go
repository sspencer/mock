package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const manUsage = "usage: mock man"

// subcommandSummaries describe each subcommand in the man page. Every
// subcommand in usageText needs one; TestManCoversSubcommands checks.
var subcommandSummaries = map[string]string{
	"validate":   "Load request files and report errors and warnings without serving. Exits 1 on errors, or on warnings with -strict.",
	"init":       "Write a starter api.http and users.json to the directory (default the current one).",
	"import":     "Draft .http sections from a HAR file, or from curl commands with -curl.",
	"run":        "Serve the request files on free ports, run the command after -- with MOCK_URL and MOCK_ADMIN_URL set, then print the strict report.",
	"match":      "Show which route answers a request, or why none does.",
	"routes":     "Print the route table without serving.",
	"wait":       "Block until a mock answers: its admin URL, or the --ready-file it writes.",
	"fmt":        "Rewrite request files in the canonical layout. -check lists files that need it and exits 1 without changing them; - formats stdin to stdout.",
	"completion": "Write a shell completion script for bash, zsh, fish or PowerShell.",
	"man":        "Write this manual page in roff format.",
	"check":      "Send each route's sample request to a real API and report where its responses differ from the mock's.",
}

// runMan writes mock's manual page (mock.1) in roff, built from usageText.
func runMan(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("mock man", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, manUsage)
			return nil
		}
		return usageError("%v\n%s", err, manUsage)
	}
	if flags.NArg() > 0 {
		return usageError("mock man takes no arguments\n%s", manUsage)
	}
	_, err := io.WriteString(stdout, manPage(commandLineModel(), currentVersion()))
	return err
}

func manPage(model commandLine, version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, ".TH MOCK 1 \"\" %s \"User Commands\"\n", roffQuote("mock "+version))
	b.WriteString(".SH NAME\nmock \\- turn REST Client .http files into a local HTTP mock server\n")
	b.WriteString(".SH SYNOPSIS\n.nf\n")
	for _, line := range model.synopsis {
		b.WriteString(roffLine(line) + "\n")
	}
	b.WriteString(".fi\n")
	b.WriteString(`.SH DESCRIPTION
.B mock
reads request files in the REST Client
.I .http
format and serves each section's response on a local port, with hot reload
when the files change and an admin UI that shows every request. Headers after
a section's request line are response headers; request matching uses
.B @header.Name
and the other matcher directives above the request line.
.PP
The full documentation, including the file format, placeholders, matching
and the admin API, is at
.UR https://github.com/sspencer/mock/tree/master/docs
.UE .
`)
	b.WriteString(".SH COMMANDS\n")
	for _, sub := range model.subcommands {
		fmt.Fprintf(&b, ".TP\n.B mock %s\n%s\n", sub.name, roffLine(subcommandSummaries[sub.name]))
	}
	b.WriteString(".SH OPTIONS\n")
	for _, f := range model.flags {
		fmt.Fprintf(&b, ".TP\n.B %s\n", roffLine(f.spec))
		var paragraph []string
		flush := func() {
			if len(paragraph) > 0 {
				b.WriteString(roffLine(strings.Join(paragraph, " ")) + "\n")
				paragraph = nil
			}
		}
		for _, line := range f.lines {
			if strings.HasPrefix(line, " ") {
				// A list under the flag, such as the -P profiles.
				flush()
				b.WriteString(".br\n" + roffLine(strings.TrimSpace(line)) + "\n")
				continue
			}
			paragraph = append(paragraph, line)
		}
		flush()
	}
	b.WriteString(`.SH ENVIRONMENT
.TP
.B MOCK_PORT
Default for \-p.
.TP
.B MOCK_UI_PORT
Default for \-a.
.TP
.B MOCK_BIND
Default for \-b.
.TP
.B MOCK_UI_TOKEN
Default for \-t, and the token mock wait sends.
.SH FILES
.TP
.B .mockrc
Flags for every run in this directory, one flag and its value per line, or
request files and directories; lines starting with # are comments. Flags on
the command line win. Read by mock and mock run unless \-\-no\-config is
given; \-\-config FILE reads another file.
.SH EXIT STATUS
0 on success, 1 on errors (and for mock \-s when a request went unmatched),
2 for usage errors. mock run exits with the command's status.
.SH EXAMPLES
.nf
mock api.http
mock \-d mocks \-u https://api.example.com \-R
mock run api.http \-\- go test ./...
mock fmt \-check mocks
.fi
`)
	return b.String()
}

// roffLine escapes text for a roff line: backslashes and dashes, and a
// leading dot or quote that roff would read as a request.
func roffLine(text string) string {
	text = strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(text)
	if strings.HasPrefix(text, ".") || strings.HasPrefix(text, "'") {
		text = `\&` + text
	}
	return text
}

func roffQuote(text string) string {
	return `"` + strings.ReplaceAll(roffLine(text), `"`, `\(dq`) + `"`
}
