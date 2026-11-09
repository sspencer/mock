# Contributing

Thanks for looking at `mock`. Small, focused changes are welcome.

## Setup

Go 1.26 or newer is required (`go.mod`). Node.js 22+ runs the dependency-free dashboard tests; it is not needed to build or run the binary.

```sh
git clone https://github.com/sspencer/mock.git
cd mock
make all
```

`make verify` checks formatting, runs `go vet`, and runs Go and JavaScript tests
without changing source files or installing anything. `make all` also installs
`mock` into `GOBIN` (or `GOPATH/bin`). Use `make fmt` to format explicitly.
`make test` runs Go tests; `make test-ui` runs dashboard behavior and HAR tests.
`make test-browser` builds `mock` and checks the admin UI in headless Chrome or
Chromium: layout at desktop and phone widths, dark mode contrast, dragging and
keyboard-resizing dividers, keyboard navigation, the help dialog, and resend and
compare against the running server. It needs no npm packages; the tests are
skipped when no browser is found (set `CHROME_BIN` to choose one), and a
failing test saves a screenshot to `$BROWSER_ARTIFACTS` or the temp directory.
`make vulncheck` downloads the pinned Go vulnerability scanner and checks dependencies.

```sh
go run . examples/user.http
```

The request log is at <http://localhost:8081/>. Example files live in
`examples/`. Tests load every example with `mock validate -strict` checks and
replay `examples/matching/requests.sh` against an in-process server, so when
you add or change a scenario there, update `matchingScenarios` in
`mockhttp/examples_test.go` with its expected status and route. An example
that warns on purpose goes in `knownExampleWarnings` in
`internal/cli/examples_test.go`.

## Pull requests

- Keep the change scoped to one concern.
- Add or update tests when behavior changes.
- Run `make verify` and `go test -race ./...` before you push.
- Match the surrounding code style. Run `make fmt` before verification.

CI checks formatting without rewriting files, runs Go and JavaScript tests,
builds the binary, checks dependencies, and runs race tests. Releases perform the
same verification before publishing.

Fuzz targets cover the parser and encoded path matching:

```sh
go test ./restclient -run '^$' -fuzz FuzzParse -fuzztime 10s
go test ./mockhttp -run '^$' -fuzz FuzzMatchEscapedPath -fuzztime 10s
```

Benchmarks cover matching against 3,000 routes (first, last, a miss with its
near-miss ranking, and `@json` matchers), rendering (placeholders,
`{{#repeat}}`, literal bodies), a whole `ServeHTTP` request and parsing. Run
them with `make bench`. To check a change for regressions, compare runs with
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):

```sh
go test ./mockhttp ./restclient -run '^$' -bench . -count 10 > old.txt
# make the change
go test ./mockhttp ./restclient -run '^$' -bench . -count 10 > new.txt
benchstat old.txt new.txt
```

The dashboard event-wiring tests use a small DOM adapter. They do not replace
visual checks in a real browser at desktop/mobile widths, keyboard navigation,
and native EventSource integration.

## Claude Code

`CLAUDE.md` and `.claude/` configure [Claude Code](https://claude.com/claude-code) for this
repository. If you don't use it, you can ignore them. They contain:

- `.claude/settings.json`: hooks that run `gofmt -w` on edited Go files and
  `go run . validate` on edited `.http` files. They need `jq` on `PATH`.
- `.claude/skills/smoke/`: an end-to-end smoke test. Run it directly with
  `.claude/skills/smoke/smoke.sh`. It needs ports 18080–18199 free.
- `.claude/skills/release/`: the maintainer release checklist.
- `.claude/agents/docs-sync-reviewer.md`: a read-only reviewer that checks docs
  against code changes.

Put personal overrides in `.claude/settings.local.json`, which git ignores.

## Releases

Tagged versions (`vX.Y.Z`) are published by GoReleaser. `-version` reports the
tag for release binaries and the module version for `go install`. See
`CHANGELOG.md` when cutting a release.
