# Potential features

Ideas for future work, last checked against the codebase on 2026-11-08 (after
v0.4.0, including the unreleased work in the changelog). They are not committed release plans. Implemented items are kept
separately below so they do not remain in the backlog; release history is in
[CHANGELOG.md](../CHANGELOG.md).

## Suggested order

Reviewed 2026-11-08. Highest value first; sizes are rough (S, M, L). Each item
is described in full under [Ideas](#ideas).

1. ~~**Validate the examples in CI** (S).~~ Done; see
   [Implemented](#implemented-from-the-original-ideas).
2. ~~**Answer `HEAD` from `GET` routes** (S).~~ Done.
3. ~~**Variables from the command line and environment** (S).~~ Done.
4. ~~**Smarter shadow warnings** (S).~~ Done.
5. ~~**Request journal API** (S–M).~~ Done.
6. ~~**Go test helper and request assertions** (M).~~ Done.
7. ~~**Lint rendered JSON bodies** (S).~~ Done.
8. ~~**Directory of request files** (S–M).~~ Done.
9. ~~**Reload trigger and polling fallback** (S).~~ Done.
10. ~~**Directive reference and recipes** (S, docs only).~~ Done.

Stateful scenarios (`@times`, then named scenarios) remain deliberately
deferred; see [stateful-scenarios.md](stateful-scenarios.md). They are the
largest remaining gap in what routes can express (retry tests in particular),
and with the items above done they are the next thing to revisit.

## Ideas

### Matching and routing

(All matching and routing ideas are implemented; see below.)

### Responses and templating

(All response and templating ideas are implemented; see below.)

### Testing and CI

- **Stateful scenarios** (deferred): `@times` for retry tests, then named
  scenarios with `@scenario`, `@state` and `@next-state`. Full plan in
  [stateful-scenarios.md](stateful-scenarios.md).

### Working with real APIs

(All Working with real APIs ideas are implemented; see below.)

### Configuration and inputs

- **Shared definitions across files** (S–M): `@import ./common.http` (or a
  variables-only file) so several request files can share `@host`, tokens and
  common response headers. The dialect table lists imports as unsupported.

### Admin UI

- **Credential redaction:** optional redaction for diagnostic history, copied
  requests, and exports. Current captures and Copy cURL include credentials.
- **Browser-level UI tests:** the admin UI's logic is unit tested in Node
  (`internal/ui/tests/`), but layout, theming and real browser interaction
  (splitters, keyboard navigation, mobile views) have never been checked in a
  real browser. Headless browser tests or screenshot checks would cover that.
- **Resend a request** (S–M): replay a captured request from the inspector,
  optionally edited. The UI is on the admin port and `mock` sends no CORS
  headers, so the browser cannot call the mock port directly; the resend has
  to go through an admin endpoint that issues the request server-side.
- **Compare two requests** (S–M): a side-by-side diff of headers and bodies for
  two selected requests, for "why did this one match and that one not".
- **Admin access token** (S): `--ui-token` (or `MOCK_UI_TOKEN`) required by the
  admin UI and API. Binding to `0.0.0.0` currently only prints a warning,
  although captured requests can contain credentials. This is access control
  on plain HTTP, not a step back toward TLS.
- **Graphical fixture editing:** lower priority.

### Tooling, distribution and docs

- **Publish the Docker image and a Homebrew formula** (S): the image is only
  built locally (`make docker`). GoReleaser can push
  `ghcr.io/sspencer/mock:<version>` and a Homebrew tap from the existing release
  workflow, so `docker run ghcr.io/sspencer/mock` works without a checkout.
  Deferred: not wanted yet, but worth doing later.
- **Aggregate validation diagnostics:** `mock validate` reports warnings for
  every route but stops at the first load error, so fixing a file can reveal the
  next error. Collecting several parse errors per run would need the parser to
  recover after an error.
- **Editor support** (M–L): `mock validate -json` already produces
  machine-readable diagnostics. A small VS Code extension or language server
  could highlight directives and show load errors and shadow warnings while
  editing.
- **Request file formatter** (M): `mock fmt` normalizes spacing, directive
  order and blank lines in `.http` files, like `gofmt`. Lower priority; most
  value once files are large or shared across a team.

## Considered and rejected

Recorded so these are not proposed again without a new reason:

- **TLS and CORS:** removed on purpose. `mock` serves plain HTTP to HTTP
  clients; put a TLS-terminating proxy in front when HTTPS is needed.
- **OpenAPI import:** an OpenAPI-to-routes helper existed and was deleted.
- **Scripted or exec responses:** a JavaScript or shell handler would turn
  `mock` into an application server. Matchers, captures and templates cover
  most needs.
- **gRPC and WebSockets:** different protocols, out of scope for a REST
  Client-style tool.

## Implemented from the original ideas

- **Filter traffic by route and status:** the traffic filter takes status
  classes, `matched`/`unmatched`, methods, `route:Name` and `-term`
  (`filter.mjs`); a route's hit count filters to its requests.

- **Copy as route for matched requests:** the draft reuses the matched
  route's response and names the route it should go above.

- **REST Client environments:** `-e NAME` reads `http-client.env.json` (and
  its private override) or VS Code `rest-client.environmentVariables`,
  merging `$shared`; `-E FILE` names the file. Values fill in variables no
  `@name` defines (`restclient.Options.Defaults`).

- **REST Client file compatibility:** unnamed sections (named from `# @name`
  or the request line), requests and comments before the first `###`, `//`
  comments, HTTP versions and bare targets on request lines, and load
  warnings for request variables and request settings.
- **Recording:** `mock -u URL -r recorded.http fixtures.http` appends a draft
  section (the Copy as route draft, with the real response) for each method
  and path the upstream answers, once each, through `Server.SetRequestHook`.
- **HAR import:** `mock import -host api.example.com traffic.har > api.http`
  drafts one section per distinct method and path from a HAR file, with the
  captured responses. The Go draft logic (`internal/cli/draft.go`) is a port
  of the UI's Copy as route (`draft.mjs`).
- **Copy all unmatched requests as routes:** the traffic panel's **Copy
  unmatched as routes** button drafts one section per distinct method and path
  among unmatched (or proxied) requests, using the Copy as route logic
  (`buildRouteDrafts` in `draft.mjs`).
- **Drift check against the real API:** `mock check -upstream URL api.http`
  sends each route's sample request to the mock and the real service and
  reports differences in status, `Content-Type` and top-level JSON fields.
- **Benchmarks:** `make bench` runs matching benchmarks against 3,000 routes,
  rendering benchmarks, a whole `ServeHTTP` request and the parser.
- **Rate limit simulation:** `@rate-limit = 5/1s` answers `429` with
  `Retry-After` once a route is over its sliding-window budget. Counts are
  per route and survive reloads that keep the route.
- **Global latency and faults:** `-w` (`--delay`) and `-f` (`--fault`) apply
  to every route without its own `@delay` or `@fault`.
- **Structured JSON logs:** `-l json` (`--log-format json`) makes stdout pure
  JSON lines: the startup banner, one `route` record per route, and one
  `http request` record per request (method, path, status, route, duration).
- **Starter file:** `mock init [-force] [dir]` writes a commented `api.http`
  and `users.json` showing routes, matchers, placeholders and a file body.
- **Webhook callbacks:** `@callback = POST {{$json.callbackUrl}}` with
  `@callback.delay`, `.header.*` and `.body`, sent once after the response
  and logged in the admin UI.
- **Compressed responses:** `@compress = gzip | deflate | auto` compresses
  bodies; `auto` honors `Accept-Encoding`.
- **Range requests for file bodies:** unchanged `< path` files answer
  `Range`, `If-Range` and `If-Modified-Since` through `http.ServeContent`.
- **Echo the request:** `{{$request}}` inserts the request as a JSON object,
  and `{{$request.path}}` reads one part of it.
- **Iterate over request arrays:** `{{#each $json.items}} ... {{/each}}` with
  `{{$item.*}}` and `{{$index}}`, also over `$fixture` and nested `$item` arrays.
- **Fixture namespace:** `@fixture = data.json` and `{{$fixture.path}}`
  placeholders read values from a JSON data file.
- **Per-route proxying:** `@proxy = URL` forwards the requests a section
  matches; a URL with placeholders rewrites the target path.
- **XML body matchers:** `@xml.element.path` matchers and
  `{{$xml.element.path}}` placeholders, starting at the root element, ignoring
  namespace prefixes, with `*` for any child and `["@name"]` for attributes.
- **Multipart bodies:** `@form.name` and `{{$form.name}}` read
  `multipart/form-data` parts (file parts by file name).
- **JSON paths with dotted keys:** `["key"]` segments in `@json` matchers and
  `{{$json.*}}` placeholders, such as `labels["app.kubernetes.io/name"]`.
- **Cookie matchers:** `@cookie.name` matchers and `{{$cookie.name}}`
  placeholders read one cookie from the `Cookie` header.
- **"Present but not X" matchers:** `!text` and `!~regex` accept a present,
  non-blank value other than `text` (or not matching `regex`), for every
  matcher type. A value starting with `!` is no longer an exact match.
- **Directive reference and recipes:** a README table of contents, one
  reference table of every `@` directive, and recipes for GraphQL operations,
  content negotiation, case-insensitive matchers, per-test `X-Test` routing
  and readiness checks (`GET /state`) in CI and Docker Compose.
- **Reload trigger and polling fallback:** `POST /reload` on the admin port
  (same cross-site rules as `/clear`, `422` on a failed reload), `SIGHUP`,
  and `-i` / `--poll 1s`, which compares sizes and modification times of the
  request and body files. One reload runs at a time whatever triggers it.
- **Directory of request files:** `-d` / `--dir` loads every `.http` file
  under a directory (lexical order, after file arguments, hidden entries
  skipped) and watches the tree, so new files and subdirectories reload.
  `mock validate` takes directories too. A positional directory still serves
  static files, so existing commands keep working.
- **Lint rendered JSON bodies:** startup and `mock validate` render each JSON
  route's body (inline or a fixed `< file`) with request placeholders set to
  `1` and warn, with the route and body line, when it does not parse. An
  unquoted request placeholder cannot be judged, since `1` fits both a bare
  value and a string.
- **Go test helper and request assertions:** the `mocktest` package starts a
  server per test (`mocktest.Start(t, "api.http")`, `srv.URL`) and asserts
  with `AssertCalled(t, "POST /users", 1)`, `AssertNotCalled` and
  `AssertAllMatched`, backed by `Server.Requests`.
- **Request journal API:** `GET /requests` on the admin port returns the
  retained request events as JSON, filtered by `method`, `path`, `route`,
  `matched` and `since`; `Server.Requests` exposes the same query in Go.
- **Smarter shadow warnings:** paths are compared segment by segment, so a
  catch-all such as `/api/*`, a `:param` before a literal, or a renamed
  parameter now produces the warning, and `@path.*` matchers on the earlier
  route narrow what it covers. Regex matchers are still compared as text, so
  `~[0-9]+` does not cover `~[1-9][0-9]*`.
- **Variables from the command line and environment:** `-v` / `--var
  NAME=VALUE` (and `mock validate -var`) sets `@NAME` in every file,
  overriding its definitions, and `{{$env.NAME}}` reads the server's
  environment at render time (never from the request), warning at load when
  the variable is unset. The flag is `-v` rather than `-var` because every
  serve flag has a short and a long name.
- **Answer `HEAD` from `GET` routes:** a `HEAD` request tries `HEAD` routes
  first, then the first matching `GET` route, which answers with its status
  and headers and no body. Unmatched `HEAD` diagnostics no longer blame the
  method of a `GET` candidate.
- **Validate the examples in CI:** `go test ./...` validates every
  `examples/**/*.http` file with the `mock validate -strict` checks (only
  `small.http` warns, on purpose) and replays every scenario in
  `examples/matching/requests.sh` against an in-process server, checking the
  status and the route that answered. A test fails if the script and the
  Go scenario table drift apart.

- **Close small test gaps:** the invalid generator argument warning has a
  direct `mockhttp` test, not only coverage through `mock validate`.
- **JSON array matching:** a `*` segment in a `@json` path matches any array
  element, as in `@json.roles.* = admin` or `@json.items.*.sku = ~A[0-9]+`.
- **Strict mode and route coverage:** `-s` / `--strict` reports unmatched
  requests and never-matched routes at shutdown and exits `1` on unmatched
  requests; `/routes` and the routes panel show per-route hit counts.
- **Copy an unmatched request as a route:** the inspector's **Copy as route**
  button copies a draft `.http` section for an unmatched request, with
  presence matchers for its query, `Authorization` header and top-level body
  fields, and either a placeholder `[200]` or the real upstream response.
- **Responses and templating:** placeholders in body file paths
  (`< ./users/{{$id}}.json`, `404` when no file matches), `{{#repeat}}`
  generated lists with `{{$index}}`, placeholder filters (`upper`, `default`,
  `add`, `keep` and more), `$path.`/`$query.`/`$header.` namespaces, `@seed`
  for reproducible generated data, `@delay` ranges, `@stream`, `@fault = reset
  | truncate`, `@template = auto|on|off` and `@trim = off`.

- **Pass unmatched requests to a real API:** `mock --upstream
  https://api.example.com fixtures.http` forwards anything no route matches to
  the real service (HTTP or HTTPS) and returns its response, marked as proxied
  in the admin UI. Upstream failures answer `502`, and requests that loop back
  to `mock` answer `508`.
- **Form matchers, wildcards and request placeholders:** `@form.name`
  matchers, trailing `*`/`*name` path wildcards, `{{$json.*}}` and
  `{{$form.*}}` body placeholders, and named regex captures as placeholders.
- **Body matchers and route order:** `@body` and `@json.field.path` matchers.
  Response rotation, `POST /reset` and the Reset responses button were
  removed; the first matching route answers, and routes an earlier route
  always beats are warned about.
- **Absent and regex matchers:** `@header.Name = !` matches a missing or blank
  header, and `~regex` values match in full. `@query.name` and `@path.name`
  use the same value syntax.

- **Multiple ports:** routes are served on the port of a full-URL request
  target, or the `@port` variable, else the default `-p` port. Every port is
  bound at startup; reloads cannot open new ports.

- **Separate admin port:** the request console listens on its own port (`-a`,
  `MOCK_UI_PORT`, default `8081`) so any path can be mocked, and `-x` skips it.
  Mock and admin ports that collide are a startup error.
- **REST Client-style request files:** `@name = value` file and method
  variables, `{{name}}` and `{{%name}}` references, a `[status]` section-name
  prefix, `< path` external bodies, `@delay` and `@header.Name` directives, and
  variables or full URLs in the request target.
- **Sample curl for routes:** selecting a route shows a copyable `curl` command
  for the mock server, with sample values for path parameters and any required
  `@header.*` headers.
- **REST Client generators:** `{{$guid}}`, `{{$randomInt min max}}`,
  `{{$timestamp [offset option]}}`, `{{$datetime rfc1123|iso8601 [offset option]}}`,
  and `{{$localDatetime ...}}`. Invalid arguments stay literal with a load-time
  warning. Only these functions take arguments; other REST Client dynamic
  variables (`$processEnv`, `$dotenv`, `$aadToken`) are not supported.
- **Command-line help:** `-h`, `-help` and `--help` print usage and exit `0`,
  including for `mock validate`.
- **Validation CLI:** `mock validate` loads request files without serving them,
  reports load errors and startup warnings, supports `-strict` and `-json`, and
  exits non-zero for CI. It stops at the first load error rather than
  aggregating several.
- **Inspector content tools:** Pretty/Raw views for complete JSON, Copy body for
  text, Copy base64 for binary captures, and shell-quoted Copy cURL for complete
  text requests. Binary or incomplete requests receive an explanation instead
  of a replay command. Clipboard errors remain visible; routine copy-success
  notices have been removed.
- **Route inspection and match diagnostics:** clickable route configuration,
  source file/line, configuration revision, and up
  to five nearby candidates explaining unmatched requests. Route previews show
  inline templates or file references; they do not load response-file contents.
- **Reload diagnostics:** `/state` exposes the active revision, route count,
  last successful reload, and reload error. Failed reloads keep working routes
  and show a dashboard error banner; the routine configuration banner is hidden.
- **Clear log:** `POST /clear` clears diagnostic history across connected
  tabs.
- **Resizable inspector:** traffic/inspector, traffic/routes, and request/response
  dividers, with remembered sizes, keyboard controls, and double-click reset.
- **Mobile navigation:** traffic and route selections open the detail view with
  a back button to return to the lists.

Admin paths above (`/events`, `/clear`, `/state`, `/routes`) are served on the admin port.

## Related improvements already available

- Server history, dashboard traffic, and the paused-traffic buffer retain up to
  1,000 requests. SSE supports ordered replay, restart/history-gap resets, and
  clear notifications across connected clients.
- Captures include byte counts, truncation metadata, and base64 for binary data.
  HAR export preserves those details, with a 64 KiB capture limit per body.
- Reloads are serialized and dependency watches follow the active source files.
  Each request is matched against a single configuration revision.
- The traffic list omits the duration beneath each request; elapsed time remains
  available in the inspector.

See [README.md](../README.md) for supported behavior and controls, and
[CHANGELOG.md](../CHANGELOG.md) for release history.
