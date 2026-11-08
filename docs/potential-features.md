# Potential features

Ideas for future work, last checked against the codebase on 2026-11-06 (after
v0.4.0). They are not committed release plans. Implemented items are kept
separately below so they do not remain in the backlog; release history is in
[CHANGELOG.md](../CHANGELOG.md).

## Suggested order

Reviewed 2026-11-06. Highest value first; sizes are rough (S, M, L).

1. **Publish the Docker image and a Homebrew formula** (S). Mostly GoReleaser
   configuration.
2. **Close small test gaps** (S). The invalid generator argument warning in
   `warnUnknownPlaceholders` is only exercised through the `mock validate`
   tests; add a direct test in `mockhttp`.

Everything else is grouped by theme below. Stateful scenarios are deliberately
deferred; see [stateful-scenarios.md](stateful-scenarios.md).

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

### Responses and templating

- **Fixture namespace:** `{{$fixture.*}}` values read from a data file, left
  over from the template namespaces item. Filters, `{{#repeat}}`, and explicit
  `$path`/`$query`/`$header` namespaces exist.

### Testing and CI

- **Go test helper and request assertions** (M): expose the server as a test
  helper, for example `srv := mocktest.Start(t, "testdata/api.http")` with
  `srv.URL` and `srv.AssertCalled(t, "POST /users", 1)`, backed by the captured
  requests. The same data could serve an admin endpoint for assertions from
  other languages: expected request counts, ordering, and matching
  constraints.
- **Structured JSON logs** (S): `--log-format json` writes one object per
  request (method, path, status, route, duration) for CI log collectors and
  `jq`. The server already logs through `slog`.
- **Stateful scenarios** (deferred): `@times` for retry tests, then named
  scenarios with `@scenario`, `@state` and `@next-state`. Full plan in
  [stateful-scenarios.md](stateful-scenarios.md).

### Working with real APIs

- **Recording** (L): capture proxied traffic and write it out as `.http`
  sections to start a fixture file. Builds on `--upstream` pass-through; lower
  priority.
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

### Admin UI

- **Credential redaction:** optional redaction for diagnostic history, copied
  requests, and exports. Current captures and Copy cURL include credentials.
- **Browser-level UI tests:** the admin UI's logic is unit tested in Node
  (`internal/ui/tests/`), but layout, theming and real browser interaction
  (splitters, keyboard navigation, mobile views) have never been checked in a
  real browser. Headless browser tests or screenshot checks would cover that.
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
- **Recipes in the README** (S, docs only): common needs that already work but
  are not obvious: GraphQL operations (`@json.operationName = GetUser`),
  content negotiation (`@header.Accept = ~(?i).*csv.*`), case-insensitive
  regexes, and per-test routing with an `X-Test` header matcher.

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
