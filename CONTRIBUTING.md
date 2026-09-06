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
`make vulncheck` downloads the pinned Go vulnerability scanner and checks dependencies.

```sh
go run . examples/user.http
```

The request log is at <http://localhost:8080/mock/>. Example files live in
`examples/`.

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

The dashboard event-wiring tests use a small DOM adapter. They do not replace
visual checks in a real browser at desktop/mobile widths, keyboard navigation,
and native EventSource integration.

## Releases

Tagged versions (`vX.Y.Z`) are published by GoReleaser. `-version` reports the
tag for release binaries and the module version for `go install`. See
`CHANGELOG.md` when cutting a release.
