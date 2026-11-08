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
3. **Variables from the command line and environment** (S). Tokens and hosts
   are the most common things people need to vary without editing files.
4. **Smarter shadow warnings** (S). The current check misses the most common
   ordering mistake, a catch-all such as `/api/*` placed first.
5. **Request journal API** (S–M). `GET /requests` on the admin port turns the
   captured traffic into something tests in any language can assert on.
6. **Go test helper and request assertions** (M). Builds on the journal so Go
   tests can start a mock per test and assert on what it received.
7. **Lint rendered JSON bodies** (S). Catch a trailing comma or a placeholder
   that breaks JSON at load time instead of in the client under test.
8. **Directory of request files** (S–M). Load every `.http` file under a
   directory and pick up new files on reload.
9. **Reload trigger and polling fallback** (S). File events are unreliable on
   Docker bind mounts and some network filesystems; give users a way to force
   a reload.
10. **Directive reference and recipes** (S, docs only). One table of every
    `@` directive, plus recipes for needs that already work but are not obvious.

Stateful scenarios (`@times`, then named scenarios) remain deliberately
deferred; see [stateful-scenarios.md](stateful-scenarios.md). They are the
largest remaining gap in what routes can express (retry tests in particular),
so revisit them once the items above are done.

## Ideas

### Matching and routing

- **Richer matching:** JSON paths with dotted keys,
  cookies (`@cookie.name`), "present but not X" predicates, and multipart or
  XML bodies. Current matching covers methods, encoded paths and path
  parameters, declared query values, and exact, present (`*`), absent (`!`) and
  regex (`~regex`) matchers on headers, query and path parameters, the raw body,
  JSON fields and form fields, plus trailing `*` path wildcards. The first
  matching route in load order wins, so catch-alls go last; shadowed routes get
  a load warning.
- **Smarter shadow warnings** (S): a route is only reported as unreachable when
  an earlier route has the identical path pattern. It misses an earlier
  `/api/*` with no matchers hiding every later `/api/...` route, and
  `/users/:id` versus `/users/:userId`. Compare patterns segment by segment,
  treating wildcards and parameter names properly.
- **Per-route proxying** (M): `@proxy = https://api.example.com` on a section
  forwards requests that match it to that URL, optionally rewriting the path.
  `--upstream` is all or nothing for unmatched requests; this lets one file
  send `/payments/*` to a sandbox while mocking everything else, and lets
  matchers decide which requests go to the real service.

### Responses and templating

- **Fixture namespace:** `{{$fixture.*}}` values read from a data file, left
  over from the template namespaces item. Filters, `{{#repeat}}`, and explicit
  `$path`/`$query`/`$header` namespaces exist.
- **Iterate over request arrays** (M): `{{#each $json.items}} ... {{/each}}`
  with `{{$item.sku}}` inside, for batch endpoints that answer one entry per
  submitted item. `{{#repeat}}` only produces generated copies, and
  `{{$json.items}}` can only copy the whole array back unchanged.
- **Echo the request** (S): a `{{$request}}` placeholder (or a `| json`-ready
  object) with the method, path, query, headers and body, for an
  httpbin-style `/echo` route that shows a client exactly what it sent.
- **Range requests for file bodies** (S–M): `< path` bodies that are not
  templated could be served with `http.ServeContent`, giving `Accept-Ranges`,
  `206 Partial Content` and `If-Range`, so download-resume and media clients
  can be tested. The directory mode already gets this from `http.FileServer`.
- **Compressed responses** (S): `@compress = gzip` (or honoring
  `Accept-Encoding`) to test client decompression and `Content-Encoding`
  handling. Today a body is always sent as written.
- **Webhook callbacks** (M–L): `@callback = POST {{$json.callbackUrl}}` with a
  body template and delay, sent after the response, for flows where the real
  service calls back (payments, async jobs). Declarative, so it stays clear of
  the rejected scripted responses, but it is the first outbound traffic
  `mock` would originate: it needs its own events in the admin UI, timeouts,
  and no retries by default.

### Testing and CI

- **Request journal API** (S–M): `GET /requests` on the admin port returns the
  retained request events as JSON, filterable by method, path, route name,
  matched or unmatched, and a sequence cursor (`?since=`). Tests in any
  language can then assert "the client called `POST /users` once with this
  body" without parsing SSE. `/events` already holds the data; this is a
  query interface over it.
- **Go test helper and request assertions** (M): expose the server as a test
  helper, for example `srv := mocktest.Start(t, "testdata/api.http")` with
  `srv.URL` and `srv.AssertCalled(t, "POST /users", 1)`, backed by the captured
  requests. The same data could serve an admin endpoint for assertions from
  other languages: expected request counts, ordering, and matching
  constraints.
- **Structured JSON logs** (S): `--log-format json` writes one object per
  request (method, path, status, route, duration) for CI log collectors and
  `jq`. The server already logs through `slog`.
- **Lint rendered JSON bodies** (S): for routes whose `Content-Type` is JSON,
  `mock validate` (and startup) renders the body with sample values and warns
  when the result does not parse, naming the route and line. Hand-written
  fixtures with a trailing comma, or a `{{$json.name}}` placeholder left
  unquoted, currently fail only in the client under test.
- **Global latency and faults** (S): `--delay 50ms..300ms` and `--fault reset
  5%` apply to every route without editing files, for a resilience run
  against the same fixtures the normal tests use. Route-level `@delay` and
  `@fault` would take precedence.
- **Drift check against the real API** (M): `mock check --upstream URL
  api.http` sends each route's sample request (the one the admin UI already
  builds) to the real service and reports differences in status,
  `Content-Type` and top-level JSON fields. Mocks go stale when the real API
  changes; this tells you which fixtures to update.
- **Rate limit simulation** (M): `@rate-limit = 5/1s` answers `429` with
  `Retry-After` once a route is over its budget. It needs per-route counters,
  so it belongs with `@times` in the stateful work, although it is
  time-based, which the stateful plan currently leaves out.
- **Benchmarks** (S): `go test -bench` cases for matching and rendering with a
  few thousand routes, so performance regressions in the linear route scan or
  the template renderer show up in review.
- **Stateful scenarios** (deferred): `@times` for retry tests, then named
  scenarios with `@scenario`, `@state` and `@next-state`. Full plan in
  [stateful-scenarios.md](stateful-scenarios.md).

### Working with real APIs

- **Recording** (L): capture proxied traffic and write it out as `.http`
  sections to start a fixture file. Builds on `--upstream` pass-through; lower
  priority.
- **Copy all unmatched requests as routes** (S): a UI action that drafts one
  section per distinct method and path among unmatched (or proxied) requests,
  using the existing Copy as route logic. A lightweight form of recording that
  needs no new server code.
- **HAR import** (M): `mock import traffic.har > api.http` converts a HAR file
  saved from browser devtools or a proxy into `.http` sections, so fixtures
  can be built from traffic captured anywhere. Unlike the rejected OpenAPI
  import it starts from real responses, but it is another converter to
  maintain, so it should share the draft logic with Copy as route.
- **Broader REST Client import:** unnamed sections, comments before the first
  section, HTTP versions on request lines (all currently load errors),
  environment files and request variables, and explicit request-to-response
  conversion warnings. Named `###` sections, `@header.*` matchers,
  `@name = value` variables, and `{{host}}` or full URLs in the request target
  (whose port selects the listener) are supported; warnings already flag likely
  request headers used as response headers.

### Configuration and inputs

- **Variables from the command line and environment** (S): `-var
  host=staging.example.com` overrides `@host`, and `{{$env.API_TOKEN}}` reads an
  environment variable. A safer, explicit replacement for REST Client's
  unsupported `$processEnv`.
- **Directory of request files** (S–M): today a directory argument serves static
  files. A mode such as `mock --dir fixtures/` would load every `.http` file
  under it and pick up new files on reload; the watcher currently knows only
  the files it started with.
- **Reload trigger and polling fallback** (S): `POST /reload` on the admin
  port, a `SIGHUP` handler, and a `--poll 1s` option. fsnotify misses changes
  on Docker Desktop bind mounts, some network filesystems and editors that
  save through renames into other directories, and today the only remedy is a
  restart.
- **Shared definitions across files** (S–M): `@import ./common.http` (or a
  variables-only file) so several request files can share `@host`, tokens and
  common response headers. The dialect table lists imports as unsupported.
- **Starter file** (S): `mock init` writes a commented `api.http` showing a
  route, a matcher, a placeholder and a file body, so a new user has something
  that runs in one command.

### Admin UI

- **Credential redaction:** optional redaction for diagnostic history, copied
  requests, and exports. Current captures and Copy cURL include credentials.
- **Browser-level UI tests:** the admin UI's logic is unit tested in Node
  (`internal/ui/tests/`), but layout, theming and real browser interaction
  (splitters, keyboard navigation, mobile views) have never been checked in a
  real browser. Headless browser tests or screenshot checks would cover that.
- **Filter traffic by route and status** (S): clicking a route's hit count
  shows only the requests it answered, and the filter understands `4xx`,
  `5xx`, `unmatched` and method names. Today the filter is plain text search.
- **Resend a request** (S–M): replay a captured request from the inspector,
  optionally edited. The UI is on the admin port and `mock` sends no CORS
  headers, so the browser cannot call the mock port directly; the resend has
  to go through an admin endpoint that issues the request server-side.
- **Copy as route for matched requests** (S): start a new route from a request
  an existing route answered, for example to add a more specific variant.
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
- **Directive reference and recipes** (S, docs only): the README is about
  1,200 lines and each `@` directive is explained in its own section, so there
  is no single place to look one up. Add one reference table (`@port`,
  `@delay`, `@stream`, `@fault`, `@seed`, `@template`, `@trim` and the
  matcher prefixes, with values and defaults) and a table of contents. Then
  add recipes for common needs that already work but are not obvious:
  GraphQL operations (`@json.operationName = GetUser`), content negotiation
  (`@header.Accept = ~(?i).*csv.*`), case-insensitive regexes, per-test
  routing with an `X-Test` header matcher, and readiness checks in CI and
  Docker Compose (poll `/state` on the admin port).
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
