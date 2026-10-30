package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

const validateUsage = "usage: mock validate [-strict] [-json] <file.http> [file.http...]"

// diagnostic is one problem found while validating request files.
type diagnostic struct {
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Source  string            `json:"source,omitempty"`
	Line    int               `json:"line,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

type validateReport struct {
	OK          bool         `json:"ok"`
	Routes      int          `json:"routes"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

// diagnosticCollector is a slog handler that records warnings instead of
// printing them, so validation can report the same checks the server logs.
type diagnosticCollector struct {
	diagnostics *[]diagnostic
}

func (diagnosticCollector) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (c diagnosticCollector) Handle(_ context.Context, r slog.Record) error {
	d := diagnostic{Level: "warning", Message: r.Message}
	r.Attrs(func(a slog.Attr) bool {
		switch {
		case a.Key == "source":
			d.Source = a.Value.String()
		case a.Key == "line" && a.Value.Kind() == slog.KindInt64:
			d.Line = int(a.Value.Int64())
		default:
			if d.Details == nil {
				d.Details = map[string]string{}
			}
			d.Details[a.Key] = a.Value.String()
		}
		return true
	})
	*c.diagnostics = append(*c.diagnostics, d)
	return nil
}

func (c diagnosticCollector) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c diagnosticCollector) WithGroup(string) slog.Handler      { return c }

// runValidate loads the request files, reports every diagnostic, and fails on
// errors (or on warnings with -strict) without starting a server.
func runValidate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	strict := fs.Bool("strict", false, "treat warnings as errors")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fmt.Fprintln(stdout, validateUsage)
			fs.PrintDefaults()
			return nil
		}
		return usageError("%v\n%s", err, validateUsage)
	}
	if fs.NArg() == 0 {
		return usageError("missing request files\n%s", validateUsage)
	}

	report := validateReport{Diagnostics: []diagnostic{}}
	var found []diagnostic
	methods, err := loadValidationMethods(fs.Args())
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, diagnostic{Level: "error", Message: err.Error()})
	} else {
		report.Routes = len(methods)
		logger := slog.New(diagnosticCollector{diagnostics: &found})
		mockhttp.New(methods, logger)
		report.Diagnostics = append(report.Diagnostics, found...)
	}

	warnings, errs := 0, 0
	for _, d := range report.Diagnostics {
		if d.Level == "error" {
			errs++
		} else {
			warnings++
		}
	}
	report.OK = errs == 0 && !(*strict && warnings > 0)

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return runError("%v", err)
		}
	} else {
		for _, d := range report.Diagnostics {
			fmt.Fprintln(stdout, d.String())
		}
		if report.OK {
			fmt.Fprintf(stdout, "ok: %d routes, %d warnings\n", report.Routes, warnings)
		}
	}
	if !report.OK {
		return &exitError{code: 1, msg: fmt.Sprintf("validation failed: %d errors, %d warnings", errs, warnings)}
	}
	return nil
}

func loadValidationMethods(paths []string) ([]restclient.Method, error) {
	methods, err := restclient.Load(paths)
	if err != nil {
		return nil, err
	}
	if err := validateMethods(methods, paths); err != nil {
		return nil, err
	}
	return methods, nil
}

// String formats a diagnostic as a single line, such as
// "users.http:12: warning: unused variable (...) variable=@x".
func (d diagnostic) String() string {
	var b strings.Builder
	if d.Source != "" {
		b.WriteString(d.Source)
		if d.Line > 0 {
			fmt.Fprintf(&b, ":%d", d.Line)
		}
		b.WriteString(": ")
	}
	b.WriteString(d.Level + ": " + d.Message)
	keys := make([]string, 0, len(d.Details))
	for k := range d.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%s", k, d.Details[k])
	}
	return b.String()
}
