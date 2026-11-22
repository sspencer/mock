package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// sampleFlags holds the flags mock send and mock client share: where sample
// requests go (-url, -port), extra headers (-H), the tag filter and the
// variables used to load the request files.
type sampleFlags struct {
	fs      *flag.FlagSet
	usage   string
	baseRaw *string
	port    *int
	headers headerFlag
	tags    tagFilter
	vars    varFlag
	env     envFlags
}

// newSampleFlags registers the shared flags on fs. urlDoc describes -url and
// verb starts the -only description ("send", "include").
func newSampleFlags(fs *flag.FlagSet, usage, urlDoc, verb string) (*sampleFlags, error) {
	defaultPort, _, err := portFromEnv("MOCK_PORT", 8080)
	if err != nil {
		return nil, usageError("%v", err)
	}
	s := &sampleFlags{fs: fs, usage: usage}
	s.baseRaw = fs.String("url", "", urlDoc)
	s.port = fs.Int("port", defaultPort, "default mock `port`, for routes that set none (default MOCK_PORT, else 8080)")
	fs.Var(&s.headers, "H", "add request header `Name: value` to every request (repeatable)")
	fs.Var((*tagsFlag)(&s.tags.Only), "only", verb+" only routes with one of these @tag `names` (comma-separated, repeatable)")
	fs.Var((*tagsFlag)(&s.tags.Skip), "skip", "leave out routes with any of these @tag `names`")
	fs.Var(&s.vars, "var", "set `name=value` in every request file (repeatable)")
	fs.StringVar(&s.env.Name, "env", "", "use the REST Client environment `NAME`")
	fs.StringVar(&s.env.File, "env-file", "", "read -env environments from `FILE`")
	return s, nil
}

// parse parses args, where flags may come before, between or after the files.
// It returns help true after printing the usage for -h.
func (s *sampleFlags) parse(args []string, stdout io.Writer) (files []string, help bool, err error) {
	for rest := args; ; {
		if err := s.fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				s.fs.SetOutput(stdout)
				fmt.Fprintln(stdout, s.usage)
				s.fs.PrintDefaults()
				return nil, true, nil
			}
			return nil, false, usageError("%v\n%s", err, s.usage)
		}
		if s.fs.NArg() == 0 {
			break
		}
		files = append(files, s.fs.Arg(0))
		rest = s.fs.Args()[1:]
	}
	if len(files) == 0 {
		return nil, false, usageError("need request files\n%s", s.usage)
	}
	return files, false, nil
}

// target checks the shared flags and resolves the base URL: -url, else
// MOCK_URL unless -port is given, else none (127.0.0.1 and each route's port).
func (s *sampleFlags) target() (base *url.URL, explicit bool, err error) {
	var portSet bool
	s.fs.Visit(func(f *flag.Flag) {
		explicit = explicit || f.Name == "url"
		portSet = portSet || f.Name == "port"
	})
	if explicit && portSet {
		return nil, false, usageError("-url and -port cannot be used together\n%s", s.usage)
	}
	if *s.port < 1 || *s.port > 65535 {
		return nil, false, usageError("invalid -port %d (expected 1-65535)", *s.port)
	}
	if err := s.env.check("-env-file", "-env"); err != nil {
		return nil, false, err
	}
	if explicit {
		base, err = parseSendURL("-url", *s.baseRaw)
	} else if !portSet {
		if envURL := strings.TrimSpace(os.Getenv("MOCK_URL")); envURL != "" {
			base, err = parseSendURL("MOCK_URL", envURL)
		}
	}
	return base, explicit, err
}

// load reads the request files with the -var/-env variables and applies the
// tag filter.
func (s *sampleFlags) load(files []string) ([]restclient.Method, error) {
	opts, err := s.env.options(s.vars)
	if err != nil {
		return nil, runError("%v", err)
	}
	methods, err := loadValidationMethods(files, opts)
	if err != nil {
		return nil, runError("%v", err)
	}
	if methods, err = s.tags.apply(methods); err != nil {
		return nil, runError("%v", err)
	}
	return methods, nil
}

// mergeExtraHeaders returns a copy of headers with the -H headers set over it,
// and the Host to send: the last -H Host value, else host.
func mergeExtraHeaders(headers http.Header, host string, extra http.Header) (http.Header, string) {
	merged := headers.Clone()
	if merged == nil {
		merged = http.Header{}
	}
	for name, values := range extra {
		if strings.EqualFold(name, "Host") {
			if len(values) > 0 {
				host = values[len(values)-1]
			}
			continue
		}
		merged[name] = slices.Clone(values)
	}
	return merged, host
}
