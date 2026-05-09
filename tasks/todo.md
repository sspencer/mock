# Mock HTTP Server

## Optimization Plan Analysis

- [x] Inspect Go package structure, tests, docs, and static UI.
- [x] Identify discrepancies and senior-level improvement opportunities.
- [x] Create `tasks/optimize.md` with staged minimal-change recommendations.
- [x] Review the generated plan for specificity and low-risk sequencing.

## Optimization Plan Analysis Review

- Created `tasks/optimize.md` with a nine-step optimization plan focused on portability, memory safety, cancellation behavior, parser correctness, package organization, UI polish, CI parity, and optional matching performance.
- Kept every step scoped to minimal file changes and included verification guidance for each step.
- Verified the current code baseline with `go test ./...` and `go vet ./...`; both pass.

## Optimization Implementation

- [x] Step 1: Embed request-log UI assets so installed binaries serve `/mock/` without a working-directory dependency.
- [ ] Step 2: Bound logged request and response bodies.
- [ ] Step 3: Make configured response delays cancellation-aware.
- [ ] Step 4: Report file-backed response failures clearly.
- [ ] Step 5: Tighten parser rules around variables and request lines.
- [ ] Step 6: Split `mockhttp/server.go` by responsibility.
- [ ] Step 7: Improve UI event handling and mobile usability.
- [ ] Step 8: Strengthen CI to match local checks.
- [ ] Step 9: Add route indexing only if benchmarking justifies it.

## Optimization Implementation Review

- Step 1 added embedded static assets through `assets.go` and changed `newHandler` to accept `fs.FS`.
- Added an embedded dashboard test while preserving temporary filesystem injection for mount-path tests.
- Verified Step 1 with `go test ./...`, `go vet ./...`, and a live `go run . -p 18085 examples/user.http` check where `/mock/` and `/users/42` both returned `200`.

## Docker Startup Diagnosis

- [x] Check Docker client and daemon connectivity.
- [x] Inspect local Docker process and context state.
- [x] Identify the most likely startup failure point.
- [x] Record findings and recommended recovery steps.

## Docker Startup Diagnosis Review

- Docker Desktop is currently running and the Docker engine is reachable from the host context.
- `docker ps` shows one running Postgres container, and `docker run --rm postgres postgres --version` successfully starts a short-lived container.
- The sandboxed Codex process sees `permission denied while trying to connect to the docker API at unix:///Users/steve/.docker/run/docker.sock`, but the same Docker commands work outside the sandbox.
- `docker info` reports Swarm is broken because `/var/lib/docker/swarm/certificates/swarm-node.crt` expired on Thu, 21 Sep 2023 23:56:00 UTC.
- Swarm state is inconsistent: `docker node ls` says this node is not a manager, while `docker swarm init` says the node is already part of a swarm.
- Recommended recovery, if Swarm is not intentionally used on this machine: run `docker swarm leave --force` to clear stale Swarm membership. Do not run it if this machine is part of an active Swarm cluster without first confirming the cluster plan.

## Docker Full Cleanup

- [x] Snapshot existing Docker containers, images, volumes, networks, and disk usage.
- [x] Stop/remove containers and prune all unused Docker-managed artifacts.
- [x] Remove build cache and builder remnants.
- [x] Verify Docker is empty afterward.
- [x] Record cleanup results.

## Docker Full Cleanup Review

- Stopped the old `todoapi-db-1` Postgres container.
- Removed all containers, images, user-created networks, volumes, and Docker builder cache.
- Main prune reclaimed 5.507 GB; explicit named-volume prune reclaimed another 134.2 MB.
- Final verification: `docker ps -a` is empty, `docker images` is empty, `docker volume ls` is empty, `docker system df -v` reports no images/containers/volumes/cache, and `docker info` reports `Swarm: inactive`.
- Only Docker's built-in default networks remain: `bridge`, `host`, and `none`.

## Dockerfile Runtime Test

- [x] Build the `mock` Docker image from the repository Dockerfile.
- [x] Run the image with the bundled example request file.
- [x] Verify an example route responds through the published port.
- [x] Clean up the test container and record results.

## Dockerfile Runtime Test Review

- Built `mock:latest` successfully from `Dockerfile`.
- Ran `mock-dockerfile-test` with `-p 18080:8080`; container startup printed 13 available mock methods and listened on `:8080`.
- Verified `GET http://127.0.0.1:18080/users/42` returned `HTTP/1.1 200 OK` with JSON content.
- Verified `GET http://127.0.0.1:18080/mock/` returned `HTTP/1.1 200 OK` with the request-log UI HTML.
- Removed the temporary `mock-dockerfile-test` container after verification.
- Left the successfully built `mock:latest` image in place for local use; final image size is 17.5 MB.

## Docker README Example

- [x] Add README usage showing a host `.http` file mounted into the container.
- [x] Verify the documented Docker command works.
- [x] Record results.

## Docker README Example Review

- Added a `Docker` section to `README.md` with `docker build -t mock .`.
- Documented running the image with a bind-mounted host `.http` file: `mock /mock/user.http`.
- Added a second example that mounts the whole `examples/` directory for request files that use relative `$file` response bodies.
- Verified the directory-mount command with `mock-readme-test` on port `8080`.
- Confirmed `GET /users/42` returned `HTTP/1.1 200 OK` and `GET /users` returned the file-backed JSON response.
- Removed the temporary verification container.

## Request Table Arrival Time

- [x] Add a request arrival time value to request-log events.
- [x] Move the request table `Time` column before `Request` and `Status`.
- [x] Render the arrival time as a time-only value in the first column.
- [x] Verify with automated tests and a live browser check.

## Request Table Arrival Time Review

- Added `request.time` to request-log events using local `HH:MM:SS` formatting captured at request arrival.
- Kept response duration available as `response.time`, but the table now displays the request arrival time instead.
- Updated the request-log table order to `Time`, `Request`, `Status`.
- Adjusted table CSS so the time column stays compact and the request column truncates.
- Verified with `go test ./...`.
- Verified in the browser by opening `/mock/`, sending `GET /users/42`, and confirming the row cells showed `22:02:06`, `GET /users/42`, `200 OK`.

## Request Table Badge Colors

- [x] Restore colorful visual treatment for HTTP method badges.
- [x] Restore colorful visual treatment for response status badges.
- [x] Verify the table still renders cleanly in the web UI.
- [x] Record results.

## Request Table Badge Colors Review

- Replaced the subdued method/status chip styling with colorful light and dark mode badge colors inspired by the previous CSS.
- Kept compact fixed badge sizing so the request table columns remain stable.
- Verified in the browser with GET, POST, and DELETE rows in both light and dark modes.
- Verified with `go test ./...`.

## Method Badge Borders

- [x] Add color-matched borders to HTTP method badges.
- [x] Verify the CSS still keeps method/status badges visually consistent.
- [x] Record results.

## Method Badge Borders Review

- Added color-matched borders for GET, POST, PUT, PATCH, and DELETE method badges in light and dark modes.
- Kept the shared badge border sizing consistent between method and status chips.
- Verified with `go test ./...`.

## Request Table Column Widths

- [x] Make the status column compact.
- [x] Give the freed space to the request column.
- [x] Verify CSS and record results.

## Request Table Column Widths Review

- Set the request log table to fixed layout.
- Made the time column `98px` and the status column `132px`, leaving the request column to take the remaining width.
- Preserved nowrap behavior for time/status and ellipsis truncation for request paths.
- Verified with `go test ./...`.

## Stylesheet Cache Busting

- [x] Add a cache-busting version query to the web UI stylesheet link.
- [x] Verify tests still pass.
- [x] Record results.

## Stylesheet Cache Busting Review

- Updated the request-log UI to load `style.css?v=20260508`.
- Verified with `go test ./...`.

## Request Table Status Second

- [x] Move `Status` to the second request table column.
- [x] Make the status column about 10% wider.
- [x] Keep `Request` as the final flexible column.
- [x] Verify tests and record results.

## Request Table Status Second Review

- Reordered the request table header and row cells to `Time`, `Status`, `Request`.
- Increased the status column from `132px` to `145px`.
- Left the request column as the final flexible column with ellipsis behavior.
- Verified with `go test ./...`.

## Placeholder Regexp Warning

- [x] Remove the redundant closing-brace escape from `placeholderPattern`.
- [x] Verify placeholder behavior still passes tests.

## Placeholder Regexp Warning Review

- Updated `placeholderPattern` from `\}\}` to literal `}}` closing braces.
- Verified with `go test ./...`.

## Dockerfile Review

- [x] Inspect the current Dockerfile against the repository layout and runtime needs.
- [x] Update build and runtime paths.
- [x] Verify the Docker build command and runtime layout as far as the local environment allows.
- [x] Record review results.

## Dockerfile Review Results

- Replaced the stale `cmd/main.go` build path with a root package build: `go build -trimpath -o /out/mock .`.
- Kept the Go builder on Go 1.26 and pinned it to Alpine 3.23 to match the runtime image line.
- Copied both `examples/` and `static/` into `/app` so the default example file and request log UI work in the container.
- Changed the image entrypoint to `mock` and the default command to `examples/user.http`.
- Updated Makefile Docker targets to build and run a `mock` image instead of a generic `test` image.
- Verified the Dockerfile build command with a Linux static binary build, simulated the `/app` runtime layout locally, and confirmed `/users/42` plus `/mock/` respond.
- A real `docker build -t mock .` could not run because the Docker client cannot connect to the local Docker daemon.

## README Refresh

- [x] Review the current README against CLI behavior and repository layout.
- [x] Rewrite stale sections for developer-friendly usage and examples.
- [x] Update checked-in example request files to match documented variable syntax.
- [x] Run verification for docs-referenced commands and examples.
- [x] Record review results.

## README Refresh Results

- Rewrote the README around the current `go run .`, `mock -p`, and `mock -l` usage.
- Removed stale references to `cmd/main.go`, directory serving, missing `docs/events.png`, unsupported global variables, and old `@` variable syntax.
- Documented request-file sections, control variables, placeholders, matching, duplicate response rotation, and the current project layout.
- Updated example `.http` files so they parse with the current `$` variable syntax.
- Verified with `go test ./...`, `make all`, stale-text scans, and a brief `go run . -p 0 examples/*.http` startup check.

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
