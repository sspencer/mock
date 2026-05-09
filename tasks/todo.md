# Mock HTTP Server

## README Refresh

- [x] Review the current README against CLI behavior and repository layout.
- [x] Rewrite stale sections for developer-friendly usage and examples.
- [x] Update checked-in example request files to match documented variable syntax.
- [ ] Run verification for docs-referenced commands and examples.
- [ ] Record review results.

## Makefile Review

- [x] Inspect current Makefile targets against the repository layout.
- [x] Update reliability issues with minimal target changes.
- [x] Run Makefile verification targets.
- [x] Record review results.

## Makefile Review Results

- Added phony target declarations so command targets do not conflict with same-named files.
- Switched Go commands through a configurable `GO` variable and derived the install directory from `go env`, instead of hardcoding a `~/go` fallback.
- Added an explicit `build` target and made `all` run `fmt`, `vet`, `test`, and `build`.
- Verified with `make test`, `make vet`, `make all`, and `make -B build`.

## GitHub Action Review

- [x] Check for checked-in GitHub Actions workflow files.
- [x] Inspect CI-relevant project commands and current repository shape.
- [x] Run local verification matching likely CI behavior.
- [x] Record review findings and residual risks.

## GitHub Action Review Results

- No GitHub Actions workflow is present in the repository: `.github/` does not exist and `git ls-files '.github/**'` returns no files.
- Local CI-equivalent checks pass with Go 1.26.3: `go test ./...`, `make test`, and `make all`.
- Because no workflow is checked in, there is no GitHub Action that can currently run on pushes or pull requests for this repository.
- If CI is expected, add `.github/workflows/ci.yml` to run at least `go test ./...`; `make all` is also currently healthy if format, vet, and build should be enforced.

## GitHub Action Verification

- [x] Read the newly added workflow.
- [x] Compare workflow commands to the current repository layout and Go version.
- [x] Fix workflow drift that would break CI.
- [x] Run local verification for the workflow-equivalent commands.
- [x] Record final review results.

## GitHub Action Verification Results

- The added workflow would have failed because it pinned Go 1.25 while `go.mod` requires Go 1.26, and it ran dependency/build commands against `./cmd/server`, which does not exist in the current repository.
- Updated `.github/workflows/go.yml` to use `actions/checkout@v4`, `actions/setup-go@v5`, `go-version-file: go.mod`, `go build -v ./...`, and `go test -v ./...`.
- Verified locally with `go build -v ./...`, `go test -v ./...`, `go vet ./...`, and `make all`.

## Configurable HTTP Port

- [x] Add a `-p` command-line flag with a default of `8080`.
- [x] Use the configured port when building the HTTP server address.
- [x] Update usage text to mention the port flag.
- [x] Add focused test coverage for default and custom listen addresses.
- [x] Run verification and record the result.

## Configurable HTTP Port Review

- Added a `-p` flag in `main.go` with default port `8080`.
- The HTTP server now builds its listen address from the configured port.
- Updated missing-input usage text to include `-p`.
- Added focused `listenAddress` coverage for default and custom ports.
- Verified with `go test ./...`.

## Empty Request Input Error

- [x] Add an explicit validation helper for zero parsed requests.
- [x] Include actionable error text that points at the required `###` request sections.
- [x] Cover empty file and empty stdin cases with focused tests.
- [x] Run verification and record the result.

## Empty Request Input Error Review

- Added `validateMethods` in `main.go` so startup fails before serving when parsed input contains no mock requests.
- The error now names the source (`stdin` or the provided file paths) and explains that input needs at least one `###` request section followed by an HTTP request line.
- Added focused tests for accepted parsed requests, empty file input, and empty stdin input.
- Verified with `go test ./...`.

## CLI Request Input Sources

- [x] Add a small input-loading helper for positional `.http` files and stdin.
- [x] Preserve existing file argument behavior for `mock examples/user.http`.
- [x] Support piped and redirected stdin when no request file is provided.
- [x] Add focused tests for file and stdin input selection.
- [x] Run verification and record the result.

## CLI Request Input Sources Review

- Added `loadMethods` in `main.go` to load positional file arguments or parse stdin as `<stdin>` when no files are provided.
- `main` now accepts piped or redirected input while still reporting usage when launched with no file and an interactive stdin.
- Added focused coverage in `main_test.go` for file-backed input and stdin-backed input.
- Verified with `go test ./...`.

## Plan

- [x] Parse one or more RestClient-style `.http` files from command-line arguments.
- [x] Capture method name, comments, variables, request line, headers, and optional body.
- [x] Serve parsed methods on port `8080`, including `:param` path variables and query variables.
- [x] Substitute supported `{{$...}}` placeholders in response bodies.
- [x] Log requests with Go's `slog` package.
- [x] Add focused tests for parsing and request handling.
- [x] Run verification and record the result.

## Review

- Implemented in `main.go`.
- Added parser and handler coverage in `main_test.go`.
- Verified with `go test ./...`.
- `git diff`/`git status` could not run because `/Users/steve/dev/go/mock` is not currently a Git repository.

## Refactor: RestClient Package

- [x] Move RestClient parser code out of `main.go`.
- [x] Add a `restclient` package with exported load/parse functions and method type.
- [x] Update server code and tests to use the new package.
- [x] Run verification and record the result.

## Refactor Review

- Parser code now lives in `restclient/restclient.go`.
- Mock serving code now lives in `mockhttp/server.go`, keeping `main.go` limited to startup wiring.
- Parser tests moved to `restclient/restclient_test.go`; server tests moved to `mockhttp/server_test.go`.
- Verified with `go test ./...`.

## Startup Method Summary

- [x] Print parsed mock methods after loading request files.
- [x] Include enough route detail for users to know what requests can be sent.
- [x] Add focused test coverage for the printed summary.
- [x] Run verification and record the result.

## Startup Method Summary Review

- Added `printMethods` in `main.go`.
- The app now prints each parsed mock route as `METHOD path[?query] name` before starting the server.
- Added `TestPrintMethods` in `main_test.go`.
- Verified with `go test ./...`.

## Duplicate Route Rotation

- [x] Detect when multiple parsed requests match the same incoming HTTP method and URL.
- [x] Alternate responses between matching request definitions.
- [x] Keep selection safe for concurrent HTTP requests.
- [x] Add focused test coverage using duplicate `POST /users` methods.
- [x] Run verification and record the result.

## Duplicate Route Rotation Review

- `mockhttp.Server` now tracks a per-request-URI counter protected by a mutex.
- When multiple parsed methods match the same incoming HTTP method and URL, responses rotate in file order.
- Added `TestServerAlternatesDuplicateMethodAndURL`.
- Verified with `go test ./...`.

## Delay Variable

- [x] Support `$delay=<duration>` variables parsed from comments before headers.
- [x] Parse delay values with Go's `time.ParseDuration`.
- [x] Delay matched responses before writing response headers/body.
- [x] Add focused server test coverage for delayed responses.
- [x] Run verification and record the result.

## Delay Variable Review

- `$delay` is read from parsed method variables in `mockhttp.Server`.
- Delay values are parsed with `time.ParseDuration`; invalid or non-positive values are ignored.
- The server sleeps after selecting a matching method and before writing response headers/body.
- Added `TestServerDelaysResponse`.
- Verified with `go test ./...`.

## File Body Variable

- [x] Resolve `$file=<path>` relative to the RestClient file path.
- [x] Reject file paths that are absolute or contain `..` path segments.
- [x] Serve the file contents as the method body when no inline body is present.
- [x] Automatically set `Content-Type` from the file extension.
- [x] Add focused tests for file serving and unsafe paths.
- [x] Run verification and record the result.

## File Body Variable Review

- `$file` paths now resolve relative to the source `.http` file's directory.
- Absolute file paths and paths containing `..` segments are rejected.
- File-backed responses set `Content-Type` from the file extension when no explicit content type was provided.
- Added tests for relative file serving and traversal rejection.
- Verified with `go test ./...`.

## Index Route Fallback

- [x] Match `/index.html` when a method route ending in `/` matches `/`.
- [x] Preserve existing exact and parameterized route matching behavior.
- [x] Add regression coverage for file-backed home page requests to `/` and `/index.html`.
- [x] Run verification and record the result.

## Index Route Fallback Review

- `matchPath` now treats `/index.html` as `/` for routes whose parsed path ends in `/`.
- The file-backed home page test now exercises both `/` and `/index.html`.
- Verified with `go test ./...`.

## Static Web UI Mount

- [x] Add a `-m` command-line flag that defaults to `mock` and controls the static UI mount path.
- [x] Serve files from `static/` under the configured mount path.
- [x] Stream incoming request log entries to the static UI under the same mount path.
- [x] Keep existing mock route behavior intact.
- [x] Add focused tests for the static mount and event stream.
- [x] Run verification and record the result.

## Static Web UI Mount Review

- Added `-m` flag parsing in `main.go`; `-m admin` mounts the static UI at `/admin/`.
- `main.go` now builds a mux that serves `static/`, exposes `/mock/events` or the overridden equivalent, and leaves normal mock routes on the fallback handler.
- `mockhttp.Server` now keeps a bounded in-memory request event list and streams events as server-sent events for the UI.
- Added tests for mount path normalization, configured static serving, unchanged mock route handling, and configured event streaming.
- Verified with `go test ./...`.

## Request/Response Body Event Details

- [x] Capture request bodies for mock requests without consuming them before handler logic can use them.
- [x] Capture response status, headers, and body as written to the client.
- [x] Format request details like a raw HTTP request with headers, blank line, and body.
- [x] Format response details like a raw HTTP response with headers, blank line, and body.
- [x] Add focused test coverage for SSE request and response body details.
- [x] Run verification and record the result.

## Request/Response Body Event Details Review

- `mockhttp.Server` now reads and restores request bodies before routing so event capture does not consume the request stream.
- Mock responses are written through a capturing `ResponseWriter`, preserving client behavior while recording status, headers, and body.
- SSE `request.details` now includes the request line, host, headers, blank line, and request body.
- SSE `response.details` now includes the response status line, headers including synthesized `Date` and `Content-Length` when needed, blank line, and response body.
- Added coverage for request and response body details in `mockhttp/server_test.go`.
- Verified with `go test ./...`.

## Static UI Redesign

- [x] Simplify `static/index.html` structure while preserving SSE IDs and behavior.
- [x] Replace the current visual style with a neutral minimalist palette and one accent color.
- [x] Use clean sans-serif typography with a polished mono style for HTTP details.
- [x] Improve responsive layout for smaller screens.
- [x] Verify tests and inspect the static page visually.

## Static UI Redesign Review

- Reworked `static/index.html` into a simpler app shell with semantic panels and quieter labels.
- Replaced `static/style.css` with a neutral, high-end visual system using one blue accent and clean sans-serif typography.
- Simplified method/status badges, detail panes, light/dark themes, and responsive behavior.
- Verified with `go test ./...`.
- Visually checked the page in the browser via a temporary localhost static server, including the theme toggle.
