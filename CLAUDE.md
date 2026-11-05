# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`mock` is a single Go binary. It turns REST Client-style `.http` files into a local HTTP mock server with hot reload and an admin request-log UI. README.md documents the user-facing behavior (file dialect, placeholders, matching, ports, admin API) and is the spec. Keep it in sync when behavior changes.

## Commands

```sh
make verify        # fmt-check + go vet + go test ./... + node UI tests (what CI runs)
make fmt           # format Go files (verify never rewrites)
make test          # Go tests only
make test-ui       # node --test tests/*.test.mjs (Node 22+, no deps)
go test -race ./...                         # also run before pushing
go test ./mockhttp -run TestName            # single Go test
node --test tests/app.test.mjs              # single UI test file
go run . examples/user.http                 # run locally; UI at http://127.0.0.1:8081/
go run . validate -strict examples/*.http   # lint request files without serving
```

Fuzz targets: `go test ./restclient -run '^$' -fuzz FuzzParse -fuzztime 10s` and `go test ./mockhttp -run '^$' -fuzz FuzzMatchEscapedPath -fuzztime 10s`.

A project hook in `.claude/settings.json` runs `gofmt -w` on every `.go` file after you edit it.

## Architecture

The pipeline is: parse `.http` → assign each route a port → serve all ports from one handler → stream request events to a separate admin UI.

- **`restclient/`**: parser. `Parse`/`Load` turn `.http` text into `[]restclient.Method`. A Method holds the name, status, variables, `MatchHeaders` (from `@header.*`), response `Headers`, body or `< file` ref, delay, and `Source`/`Line` for diagnostics. File-scoped `@vars` can be defined after the sections that use them, so sections are buffered as `pendingSection` and resolved at the end. Errors are `file:line:` formatted.
- **`main.go`**: `run(ctx, args, stdin, stdout, stderr, logger)` is the testable entry point; `main` only wires signals. It loads the input (files, stdin, or a directory, which runs as a plain static file server with no admin UI), builds a `portPlan` (`ports.go`), binds every mock port before serving, then serves one `mockhttp.Server` on all mock listeners plus a separate admin listener.
- **`ports.go`**: a route's port comes from the port in its URL target, else `@port`, else `-p`. Hot reload may change routes but can't open new ports, so `plan.resolve` rejects that case.
- **`watch.go`**: fsnotify watcher on the request files and their `< path` bodies. On change, `reloadMockFiles` re-parses and calls `Server.SetMethods` (an atomic swap that bumps the revision and resets rotation counters). A failed reload keeps the old routes and records the error for `/state` and the UI banner. `Reconcile` updates the watched dependency set after each reload.
- **`mockhttp/`**: runtime. `server.go` handles ServeHTTP: match, delay, render, write, then `logRequest`. Matching is in `match.go`; it is port-aware, and when nothing matches it records near-miss candidates. Body and header rendering with `{{$placeholder}}` functions is in `render.go`/`functions.go`. The request-event ring buffer (1,000 events) and SSE with session+sequence cursors are in `events.go`. The admin endpoints `/routes`, `/state`, `/clear`, `/reset` are in `inspection.go`/`details.go`. Admin POSTs need `X-Requested-With` or a JSON content type and reject cross-origin requests.
- **`validate.go`**: `mock validate` reuses the same load and warning checks as startup. Exit codes: 0 = ok, 1 = errors (or warnings with `-strict`), 2 = usage error.
- **`static/`**: the admin UI, in vanilla ES modules embedded via `assets.go`. The server injects `MOCK_CONFIG` (`uiConfig`: version, mock port, `multiPort`) into the page. Keep pure logic in small modules (`traffic.mjs`, `har.mjs`, `inspector.mjs`) so `tests/*.test.mjs` can unit test it. `app.mjs` loads once per test run, so config-dependent behavior must be extracted into testable helpers.

Design constraints:
- Plain HTTP only. TLS and CORS support were removed on purpose; don't add them back.
- Headers after the request line are *response* headers. Request matching uses `@header.Name`.
- Releases are tagged `vX.Y.Z` and published by GoReleaser. Record release history in `CHANGELOG.md`.
