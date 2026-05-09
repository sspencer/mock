# Optimization Plan

This repository is already in a good baseline state: `go test ./...` and `go vet ./...` pass, and the package boundaries are clear enough for a small tool. The highest-value improvements are therefore incremental hardening, portability, and maintainability work rather than broad rewrites.

## Goals

- Keep each step small enough to review independently.
- Preserve the current CLI and `.http` file behavior unless a step explicitly documents a behavior change.
- Prefer standard-library Go solutions and focused tests.
- Verify every behavior change with `go test ./...`, and use `make all` for steps that touch build or CI behavior.

## Step 1: Make The Installed Binary Serve Its UI Reliably

**Why:** `main.go` always serves static files from the relative `static` directory. That works from the repo root and in the Docker image because `/app/static` exists, but an installed `mock` binary run from another directory will not find the request-log UI.

**Current touchpoints:** `main.go:43`, `main.go:84`, `static/index.html`, `static/style.css`, `static/favicon.ico`

**Minimal change:**

- Add `//go:embed static` in `main.go` or a small new `assets.go` in package `main`.
- Change `newHandler` to accept an `http.FileSystem` or `fs.FS` instead of a string directory.
- Serve the embedded `static` subtree with `http.FileServer`.
- Keep tests able to inject a temporary filesystem so `TestHandlerServesStaticFilesUnderConfiguredMount` stays focused.

**Verification:**

- Add or adjust a test proving `/mock/` serves embedded content without depending on the process working directory.
- Run `go test ./...`.
- Run `go run . examples/user.http` from the repo root and confirm `/mock/` still responds.

## Step 2: Bound Logged Request And Response Bodies

**Why:** `readRequestBody` uses an unbounded `io.ReadAll`, and `responseCapture` stores every byte written to the response. A mock server is usually local, but a large upload or file-backed response can still create unnecessary memory pressure.

**Current touchpoints:** `mockhttp/server.go:380`, `mockhttp/server.go:398`, `mockhttp/server.go:420`, `mockhttp/server.go:521`, `mockhttp/server.go:543`

**Minimal change:**

- Introduce a small constant such as `maxLoggedBodyBytes`.
- Capture only the first N bytes for request and response details.
- Append a clear truncation marker in the detail text when logging truncates.
- Preserve the full request body for downstream handler compatibility by restoring `r.Body` after reading.
- Preserve full response delivery to clients while only truncating the captured copy.

**Verification:**

- Add tests for oversized request bodies and oversized responses.
- Assert client responses are complete while UI event details are bounded and marked as truncated.
- Run `go test ./...`.

## Step 3: Make Delays Cancellation-Aware

**Why:** `$delay` currently calls `time.Sleep`, so a disconnected client still occupies a handler goroutine until the delay expires.

**Current touchpoints:** `mockhttp/server.go:82`, `mockhttp/server.go:253`

**Minimal change:**

- Change `delay` to accept `context.Context`.
- Replace `time.Sleep(delay)` with a `time.Timer` and `select` on `ctx.Done()`.
- Decide the exact response behavior on cancellation; the simplest path is to return from `ServeHTTP` immediately without attempting to write the configured response.

**Verification:**

- Add a test with a canceled request context and a long `$delay`, asserting the handler returns quickly.
- Keep the existing positive delay test.
- Run `go test ./...`.

## Step 4: Report File-Backed Response Failures Clearly

**Why:** `renderBody` silently returns an empty body when `$file` points to a missing or unreadable file. That makes broken mocks look like successful empty responses and slows diagnosis.

**Current touchpoints:** `mockhttp/server.go:83`, `mockhttp/server.go:276`, `mockhttp/server.go:303`

**Minimal change:**

- Make `renderBody` return `(string, error)`.
- When `$file` cannot be read, return `500 Internal Server Error` with a concise text body, and log the file path error.
- Keep path traversal rejection as a distinct safe-empty behavior unless the product decision is to surface that as a startup validation error.

**Verification:**

- Add a test for a missing `$file` returning `500`.
- Keep the path traversal test explicit so the security behavior is not accidentally weakened.
- Run `go test ./...`.

## Step 5: Tighten Parser Rules Around Variables And Request Lines

**Why:** The comment variable regexp is not anchored, so any comment containing `$name=value` is treated as a variable. The parser also accepts request lines with extra fields without using or rejecting them.

**Current touchpoints:** `restclient/restclient.go:26`, `restclient/restclient.go:120`, `restclient/restclient.go:132`

**Minimal change:**

- Anchor `commentVariablePattern` to the full comment after trimming, for example `^\$name=value`.
- Require the request line to have exactly two fields unless a documented HTTP version field is intentionally supported.
- If HTTP versions are accepted, parse and ignore only valid versions such as `HTTP/1.1`; reject arbitrary trailing tokens.

**Verification:**

- Add parser tests for ordinary comments containing dollar signs.
- Add parser tests for malformed request lines with extra tokens.
- Run `go test ./...`.

## Step 6: Split `mockhttp/server.go` By Responsibility

**Why:** `mockhttp/server.go` contains routing, matching, rendering, generated values, SSE publishing, body capture, and detail formatting in one large file. The package is still understandable, but future changes will be cheaper if related code lives together.

**Current touchpoints:** `mockhttp/server.go`

**Minimal change:**

- Move path/query matching to `mockhttp/match.go`.
- Move response rendering and generated placeholders to `mockhttp/render.go`.
- Move SSE event storage and publishing to `mockhttp/events.go`.
- Move request/response detail formatting and response capture to `mockhttp/details.go`.
- Keep exported API unchanged: `mockhttp.New`, `(*Server).ServeHTTP`, and `(*Server).ServeEvents`.

**Verification:**

- No behavior changes expected.
- Run `gofmt` and `go test ./...`.
- Review the diff carefully to ensure it is mostly moved code.

## Step 7: Improve UI Event Handling And Mobile Usability

**Why:** The request log UI is useful, but it has a few polish gaps: `console.log` is left in production UI, the clear button only clears the local browser state while server-side replay still exists, and the mobile layout hides the request column, which removes the most important scanning information.

**Current touchpoints:** `static/index.html:107`, `static/index.html:113`, `static/index.html:140`, `static/style.css:444`

**Minimal change:**

- Remove the `console.log(data)` statement.
- Replace `innerHTML` for the empty row with DOM construction for consistency and safety.
- On narrow screens, keep request method/path visible and shorten either status text or time instead of hiding the request column.
- Consider adding a small `DELETE /mock/events` or `POST /mock/events/clear` endpoint only if server-side clear is desired; otherwise rename the button behavior in UI copy or documentation so it is clearly local-only.

**Verification:**

- Run `go test ./...` for any server endpoint changes.
- Manually verify `/mock/` in desktop and mobile-width browser views after generating at least one request.
- Confirm reconnect behavior matches the chosen clear semantics.

## Step 8: Strengthen CI To Match Local Senior-Developer Checks

**Why:** `.github/workflows/go.yml` runs build and tests, while the local `make all` path also runs formatting and `go vet`. CI should enforce the same minimum quality gate contributors are asked to run locally.

**Current touchpoints:** `.github/workflows/go.yml`, `Makefile`

**Minimal change:**

- Replace separate build/test workflow commands with `make all`, or add explicit `go vet ./...` and formatting checks.
- Add Go build cache configuration through `actions/setup-go` if not already implicit enough for the project.
- Keep the workflow simple because there are no external dependencies.

**Verification:**

- Run `make all` locally.
- Confirm workflow syntax remains valid.

## Step 9: Add A Small Route Index For Faster Matching Only If Needed

**Why:** `findMethod` scans every configured mock on each request. That is fine for small files, but the cost grows linearly as users add many examples or generated mocks.

**Current touchpoints:** `mockhttp/server.go:28`, `mockhttp/server.go:60`, `mockhttp/server.go:140`

**Minimal change:**

- Defer this until there is a real scale need or a benchmark showing route matching matters.
- If needed, add an unexported index keyed by HTTP method, and scan only methods for that verb.
- Preserve duplicate response rotation order exactly.

**Verification:**

- Add a benchmark for route lookup before changing the implementation.
- Add tests proving duplicate rotation still follows file order.
- Run `go test ./...`.

## Recommended Order

1. Embedded UI assets.
2. Bounded body logging.
3. Cancellation-aware delays.
4. File-backed response errors.
5. Parser tightening.
6. `mockhttp` file split.
7. UI event/mobile polish.
8. CI parity with `make all`.
9. Optional route index after benchmarking.

This order fixes user-visible reliability first, then resource safety, then maintainability. The file split intentionally comes after behavior hardening so the mechanical move can be reviewed against stronger tests.

## Implementation Progress

### Step 1: Embedded UI Assets

**Status:** Complete.

**Performed:**

- Added embedded static asset support through `assets.go`.
- Changed `newHandler` to accept an `fs.FS`, allowing production to use embedded assets and tests to inject temporary filesystems.
- Added coverage proving the embedded dashboard is served from `/mock/`.

**Verification:**

- `go test ./...`
- `go vet ./...`
- `go run . -p 18085 examples/user.http`
- `curl http://127.0.0.1:18085/mock/` returned `200`.
- `curl http://127.0.0.1:18085/users/42` returned `200`.

### Step 2: Bounded Body Logging

**Status:** Complete.

**Performed:**

- Added a `maxLoggedBodyBytes` cap and shared truncation marker for request-log details.
- Changed request body logging to sample only the preview bytes and replay the sampled bytes plus the unread stream through `r.Body`.
- Changed response capture to send full responses to clients while storing only a bounded preview for UI events.
- Preserved response detail `Content-Length` as the full delivered body length.
- Added tests for oversized request and response bodies.

**Verification:**

- `go test ./...`
- `go vet ./...`

### Step 3: Cancellation-Aware Delays

**Status:** Complete.

**Performed:**

- Changed `$delay` handling to use a `time.Timer` and the request context instead of unconditional `time.Sleep`.
- Returned early without writing the configured response when the request context is already canceled during the delay.
- Added coverage proving a canceled delayed request exits quickly and does not publish a completed response event.

**Verification:**

- `go test ./...`
- `go vet ./...`
