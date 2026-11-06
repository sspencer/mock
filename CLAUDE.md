# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`mock` is a single Go binary. It turns REST Client-style `.http` files into a local HTTP mock server with hot reload and an admin request-log UI. README.md documents the user-facing behavior (file dialect, placeholders, matching, ports, admin API) and is the spec. Keep it in sync when behavior changes.

## Commands

```sh
make verify        # fmt-check + go vet + go test ./... + node UI tests (what CI runs)
make fmt           # format Go files (verify never rewrites)
make test          # Go tests only
make test-ui       # node --test internal/ui/tests/*.test.mjs (Node 22+, no deps)
go test -race ./...                         # also run before pushing
go test ./mockhttp -run TestName            # single Go test
node --test internal/ui/tests/app.test.mjs  # single UI test file
go run . examples/user.http                 # run locally; UI at http://127.0.0.1:8081/
go run . validate -strict examples/*.http   # lint request files without serving
```

Fuzz targets: `go test ./restclient -run '^$' -fuzz FuzzParse -fuzztime 10s` and `go test ./mockhttp -run '^$' -fuzz FuzzMatchEscapedPath -fuzztime 10s`.

Project hooks in `.claude/settings.json` run `gofmt -w` on edited `.go` files and `go run . validate` on edited `.http` files (both need `jq`).

## Architecture

The pipeline is: parse `.http` → assign each route a port → serve all ports from one handler → stream request events to a separate admin UI.

- **`restclient/`**: parser. `Parse`/`Load` turn `.http` text into `[]restclient.Method`. A Method holds the name, status, variables, `MatchHeaders`/`MatchQuery`/`MatchPath`/`MatchBody`/`MatchJSON`/`MatchForm` (raw matcher values from `@header.*`, `@query.*`, `@path.*`, `@body`, `@json.*`, `@form.*`; parsed by `ParseMatcher`/`ParseBodyMatcher` in `matcher.go`: exact, `*`, `!`, `~regex`), response `Headers`, body or `< file` ref, delay, and `Source`/`Line` for diagnostics. File-scoped `@vars` can be defined after the sections that use them, so sections are buffered as `pendingSection` and resolved at the end. Errors are `file:line:` formatted.
- **`main.go`**: only wires signals, sets `cli.Version` from the link-time `main.version`, calls `cli.Run`, and maps errors to exit codes with `cli.ExitCode`.
- **`internal/cli/`**: the command. `Run(ctx, args, stdin, stdout, stderr, logger)` is the testable entry point (`run.go`). It parses flags (`config.go`), loads the input (`input.go`: files, stdin, or a directory, which runs as a plain static file server with no admin UI), builds a `portPlan` (`ports.go`), binds every mock port before serving, then serves one `mockhttp.Server` on all mock listeners plus a separate admin listener (`serve.go`).
- **`internal/cli/ports.go`**: a route's port comes from the port in its URL target, else `@port`, else `-p`. Hot reload may change routes but can't open new ports, so `plan.resolve` rejects that case.
- **`internal/cli/watch.go`**: fsnotify watcher on the request files and their `< path` bodies. On change, `reloadMockFiles` (`reload.go`) re-parses and calls `Server.SetMethods` (an atomic swap that bumps the revision). A failed reload keeps the old routes and records the error for `/state` and the UI banner. `Reconcile` updates the watched dependency set after each reload.
- **`mockhttp/`**: runtime. `server.go` handles ServeHTTP: match, delay, render, write, then `logRequest`. Matching is in `match.go`: the first route in load order whose port, method, path and matchers all accept the request wins (no rotation). The body is read once (up to 1 MiB) and JSON/form data is decoded lazily per request (`requestFacts`). A trailing `*`/`*name` path segment is a wildcard. Templates resolve `{{$name}}` through `templateValues.lookup`: path params, then named regex captures from the matched route's matchers, then query values, then `$json.*`/`$form.*` from the body. When nothing matches it records near-miss candidates. `warnShadowedRoutes` flags routes an earlier route always beats. Body and header rendering with `{{$placeholder}}` functions is in `render.go`/`functions.go`. The request-event ring buffer (1,000 events) and SSE with session+sequence cursors are in `events.go`. The admin endpoints `/routes`, `/state`, `/clear` are in `inspection.go`/`details.go`. Admin POSTs need `X-Requested-With` or a JSON content type and reject cross-origin requests.
- **`internal/cli/validate.go`**: `mock validate` reuses the same load and warning checks as startup. Exit codes: 0 = ok, 1 = errors (or warnings with `-strict`), 2 = usage error.
- **`internal/ui/`**: the admin UI. `ui.NewHandler` mounts the admin API endpoints and serves `static/`, vanilla ES modules embedded via `embed.go` (`ui.Assets`). It injects `ui.Config` (version, mock port, `multiPort`) into the page as the `<script id="mock-config">` JSON. Keep pure logic in small modules (`traffic.mjs`, `har.mjs`, `inspector.mjs`) so `internal/ui/tests/*.test.mjs` can unit test it. `app.mjs` loads once per test run, so config-dependent behavior must be extracted into testable helpers.

Design constraints:
- Plain HTTP only. TLS and CORS support were removed on purpose; don't add them back.
- Headers after the request line are *response* headers. Request matching uses `@header.Name`.
- Releases are tagged `vX.Y.Z` and published by GoReleaser. Record release history in `CHANGELOG.md`.
