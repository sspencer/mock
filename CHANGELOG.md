# Changelog

## Unreleased

- `mock check` compares nested JSON keys and value types (string versus number and so on), with paths such as `user.address` and `items[0].sku`, down to `-depth` levels (3 by default; `null` matches any type), and compares chosen response headers with `-check-header Name`.

- `mock run api.http -- npm test` starts `mock` on free ports, runs the command with `MOCK_URL` and `MOCK_ADMIN_URL` set, then stops, prints the strict report and exits with the command's status (or `1` when the command passed but requests went unmatched). One CI step instead of background jobs and traps.

- Route switches in the admin UI: **Switches** in the route inspector turns a route off (later routes answer), forces its status, or adds a delay or fault while `mock` runs, until switched back on or reset. Switched routes get a badge in the route list, and `/state` counts them. `/faults` takes the same `off` and `status` fields, and `/routes` reports each route's own `switch`.

- Temporary routes: `POST /routes` with `.http` text adds routes in front of the file's routes until `DELETE /routes/{id}`, `POST /reset` or a restart, so tests in any language can stub one answer. Go tests call `mocktest`'s `Stub(t, text)`, removed when the test ends, or `Server.AddRoutes`.

- `@import = shared/common.http` brings in another file's file-level definitions (variables and directives such as `@delay` or `@default.header.*`), so request files can share hosts, tokens and defaults. Imports nest, cycles are load errors, imported variables a file doesn't use aren't reported as unused, and editing an imported file reloads the files that import it.

- `mock match api.http GET '/users?role=admin' -H 'X-Tenant: a'` prints the route that would answer a request, or the closest routes and why each turned it away (exit 1), without starting a server. `-response` renders the response too, and `-json` prints it all as JSON. Go code can call `Server.Explain`.

- Releases publish a multi-arch image, `ghcr.io/sspencer/mock` (linux/amd64 and linux/arm64, tagged with the version and `latest`), and a Homebrew cask in `sspencer/homebrew-tap` (`brew install --cask sspencer/tap/mock`).

- `@times = n` lets a route answer `n` requests, then steps aside for later routes, for retry tests such as "fail twice, then succeed". `POST /reset` on the admin API (and `Reset()` in `mocktest`) starts counts, rate limits and chance draws over and removes run-time overrides, without a reload.

- An example gallery in `examples/gallery/`: an OAuth 2.0 token endpoint, a paginated catalog from a fixture, file uploads, a webhook sender with signed, retried callbacks, and a card payment API with test cards, idempotency keys, per-key rate limits and random failures. `docs/examples.md` walks through each, and CI sends every route its documented requests.

- The documentation in `docs/` builds into a searchable site with MkDocs Material (`make docs`, `make docs-serve`), checked in strict mode on pull requests and published to GitHub Pages from master by the new Docs workflow.

- In JSON bodies, `{{#each}}` and `{{#repeat}}` copies that render nothing (an `{{#if}}` that did not hold) no longer leave stray commas.

- `mock validate` warns about a placeholder that could not be read, such as one nested inside another (`{{$jwt iss={{host}}}}`), instead of serving it literally without a word.

- **Breaking (Go API):** `restclient.ParseDelay` returns a `restclient.Delay` (with the new lognormal `Sigma`) instead of two durations.

- New faults: `@fault = hang` (read the request and never answer, for timeout tests), `empty` (close without a byte) and `garbage` (send bytes that are not HTTP). They take a share like the others and work with `-f`.

- `@throttle = 32kb/s` sends the body at a fixed rate, including file bodies and range requests, and `-T` (`--throttle`) sets one for every route.

- Long-tail delays: `@delay = p50 80ms p99 900ms` or `@delay = lognormal 90ms 0.4`, also for `-w`. Draws are capped at the 99.99th percentile.

- `-P` (`--profile`) `slow`, `flaky` or `3g` presets a delay, fault and throttle; `-w`, `-f` and `-T` override its parts.

- Rate-limited routes send `X-RateLimit-Limit`, `X-RateLimit-Remaining` and `X-RateLimit-Reset`, and `@rate-limit.key = {{$header.Authorization}}` gives each client its own budget.

- The admin API's `/faults` endpoint switches a delay, fault or throttle on for a route (by name or id) or for every route at run time, until cleared, across reloads; `GET` lists them and `DELETE` clears them. `/routes` and the route inspector show the active override. Go code can call `Server.SetFault`, `Server.ClearFaults` and `Server.Faults`.

- The response status can come from the request: `### [{{$query.status | default 200}}] Echo` or `@status = {{$json.code}}`. `@status = 202` sets a fixed status for sections whose name has none.

- `{{#if $ref}} ... {{else}} ... {{/if}}` blocks, with presence tests and `==` / `!=` comparisons, keep optional fields and variants in one route.

- Fixture records by key: `{{$fixture.users[id=$path.id].name}}`. `[field=value]` selectors work in every JSON path, including `{{$json.*}}` and `@json.*` matchers. `@require = $reference` makes a route match only when the reference has a value, so a missing record falls through to a `404` route; the near-miss list says why.

- `{{#each ... | offset n | limit n}}` pages through an array, with `$reference` arguments such as `$query.limit`, and `| length` counts it.

- New filters: `length`, `slice`, `replace`, `base64decode`, `sha256`, `hmac`, `int`, `round`, `sub`, `mul` and `date`. `add` now takes decimals too. Filter arguments can be `$references`, such as `| mul $query.rate`.

- New generators: `{{$oneOf a b c}}`, `{{$randomFloat min max [decimals]}}`, `{{$randomDate from to}}`, `{{$company}}`, `{{$city}}`, `{{$country}}`, `{{$address}}`, `{{$ip}}`, `{{$word}}`, `{{$price}}` and `{{$ulid}}`.

- `{{$jwt sub=$json.user exp=1h}}` builds a JSON Web Token, signed with HS256 when `@jwt.secret` is set and unsigned otherwise.

- `@fixture` reads CSV (an array of row objects keyed by the header) and NDJSON (`.ndjson`, `.jsonl`) files, chosen by extension. `{{$fixture}}` is the whole file. The fixture editor stays JSON-only.

- `@etag = auto` adds a hash `ETag` and answers a matching `If-None-Match` with `304`, for inline and templated bodies too.

- `@callback.1`, `@callback.2` ... send further callbacks in order after `@callback`, each after the previous one finished, and `@callback.retries` (or `@callback.N.retries`) retries a callback that got no response, a `429` or a `5xx`, with exponential backoff. Callback events carry `index` and `attempt`; retries are marked **retry** in the admin UI.

- `@missing = next` lets later routes answer when a route's `< path` file is missing, and `@missing = 410` (or another status) changes the status it answers with.

- `/routes` reports `statusTemplate`, `require`, `missing` and `etag`, and the route inspector shows them.

- `@default.header.Name = value` adds a response header to every section that does not set it (usually at file scope, such as `@default.header.Content-Type = application/json`), and `-H 'Name: value'` (`--header`) does the same for every request file. A section's own headers win, then `@default.header`, then `-H`. A default `Content-Type` is used only when the type cannot be guessed from the `< path` file's extension or by sniffing the body with `http.DetectContentType` (which finds types such as `image/png` and `text/html`, but sees JSON as plain text); `/routes` shows it as `defaultContentType`. `@proxy` sections get none.

- A request line may list several methods, such as `GET,HEAD,OPTIONS /health`, or use `*` for any method, instead of one copy of the section per method.

- `@fallback = true` tries a route after every route without it, whatever the load order, so a catch-all `404` or a wide `@proxy` route stays last when `-d` loads files alphabetically.

- `@match.chance = 20%` lets a route answer only that share of the requests it matches; the rest go on to later routes. Under `@seed` the draws are reproducible from a fresh start. `/routes` and the route inspector show `fallback` and `chance`, and the near-miss list says when a route lost its draw.

## v0.5.1 - 2026-11-10

A new look for the admin UI and a reorganized manual: the UI gets a bolder ink, violet and lime design with SVG icons, and the README is now a short introduction that links to reference pages under `docs/`.

- Docs: the README is now a short introduction (install, a first mock, the admin UI, a feature list). The reference moved to pages under `docs/`, indexed by `docs/README.md`: getting started, install, command line, request files, placeholders, matching, delays and faults, real APIs, testing, recipes and the admin UI. Release archives include the `docs/` pages.

- Redesign the admin UI with its own look: ink, electric violet and acid lime on warm paper (deep violet in dark mode). The traffic log and inspector are two hard-edged cards under an ink toolbar, section titles are mono capitals, methods are outlined tags, statuses are solid blocks, and the selected request or route is highlighted in lime. **Pretty**/**Raw** and **Tree**/**JSON** are segmented switches and the traffic filter is full width. Icons are now inline SVG (toolbar, fixture editor, favicon). Labels, shortcuts and behavior are unchanged.

## v0.5.0 - 2026-11-08

Work with real APIs and richer responses: forward unmatched requests upstream, record or import traffic as routes, check mocks for drift, and serve fixtures, callbacks, streams, faults, rate limits and compression. The admin UI gains resend, compare, fixture editing, filtering, an access token and credential redaction. This release has breaking changes; see below.

- **Breaking:** `@stream`, `@fault`, `@seed`, `@template` and `@trim` are now control variables, not `{{name}}` variables, and invalid values are load errors. A `|` inside a `{{$...}}` placeholder now starts a filter, and `{{#repeat` and `{{/repeat}}` are now block tags. A `< path` containing `{{...}}` is now a templated path rather than a literal file name.

- **Edit fixture** in the admin UI's route inspector edits a route's `@fixture` JSON file as a tree (typed values; rename, add, duplicate and remove fields) or as text, and saves it through the new `GET`/`PUT /fixture` admin endpoint. Writes are limited to current routes' fixture files, must be valid JSON, are atomic (through a symlink, the target is replaced), and are refused with `409` if the file changed since it was read. Files the tree view would rewrite (integers beyond 2^53, numbers such as `1.0`, repeated or reordered field names) open in the JSON view and are saved as written. The next response uses the saved file.

- The admin UI's primary buttons (**Send**, **Save**) now show their accent background instead of the plain button gradient.

- Add browser-level admin UI tests (`make test-browser`, run in CI): headless Chrome, driven over the DevTools Protocol with no npm dependencies, checks layout at desktop and phone widths, dark mode, divider dragging, keyboard navigation and resend/compare against a running `mock`.

- **Compare…** in the admin UI inspector shows two logged requests side by side: overview, query parameters and headers as tables, and the request and response bodies as a line diff (JSON compared with sorted keys), with a difference count and an **Only differences** toggle.

- **Resend** in the admin UI inspector replays a logged request, optionally edited (method, path, headers, body), through the new `POST /resend` admin endpoint, which answers it in-process as if a client had sent it to the mock port. The new request is selected and marked **resent** (`resendOf` in the event).

- Add `-R` (`--redact`) to mask credentials in the request log: credential-like headers (keeping the `Authorization` scheme and cookie names), query parameters, JSON members (whatever their type) and form fields become `[redacted]` in the admin UI, copies, HAR exports, `GET /requests`, the event stream and the request log line, and `@callback` URLs are masked in callback events and log lines. `-r` recordings keep the real values so recorded routes replay. Library users call `Server.SetRedact`; `Server.SetRequestHook` still gets the real event.

- Add `-t` (`--ui-token`, or `MOCK_UI_TOKEN`) to require a token for the admin UI and API: browsers open `/?token=TOKEN` once and get a same-site cookie holding a digest of the token; API clients send `Authorization: Bearer TOKEN` (any case of `Bearer`). Spaces around the token are ignored. Binding to all interfaces with a token no longer warns. Library users can wrap a handler with `ui.RequireToken`.

- The admin UI's traffic filter understands `2xx`..`5xx`, `matched`, `unmatched`, method names and `route:Name`, combines terms, and excludes with `-term`. Clicking a route's hit count filters the traffic to the requests it answered.

- **Copy as route** in the admin UI now also works on requests a route answered: the draft reuses that route's response and starts with a comment naming the route, as a starting point for a more specific variant.

- Add `-e` (`--env`) to use a REST Client environment: variables come from `http-client.env.json` (with `http-client.private.env.json` overriding it) or `.vscode/settings.json` `rest-client.environmentVariables`, with `$shared` values and `{{$shared name}}` references. `-E` (`--env-file`) names another file. `mock validate` and `mock check` take `-env` and `-env-file`. Environment values fill in variables no `@name` defines.

- Load more REST Client files: the name after `###` is optional (unnamed sections are named after `# @name` or their request line), a request may come before the first `###`, `//` lines are comments, an `HTTP/1.1` version on the request line is ignored, and a bare target is a `GET`. Request variables such as `{{login.response.body.$.token}}` and request settings such as `# @no-redirect` now get load warnings.

- Add `-r` (`--record`) to record forwarded traffic: with `--upstream`, each method and path the upstream answers is appended to the file as a draft `.http` section with the real response (gzip and deflate decoded). Sections already in the file are not recorded again. Library users can watch requests with `Server.SetRequestHook`.

- Add `mock import <file.har>`, which converts a HAR file from browser devtools, a proxy or **Export HAR** into `.http` sections on stdout, one per distinct method and path, drafted like **Copy as route** with the captured responses. `-host` and `-path` filter the requests.

- Add **Copy unmatched as routes** to the admin UI's traffic panel. It copies one draft `.http` section (the same draft as **Copy as route**) per distinct method and path among the unmatched requests in the log, preferring proxied requests' upstream responses.

- Add `mock check -upstream URL <files>`, a drift check: it sends each route's sample request to the mock and to the real API and reports differences in status, `Content-Type` and top-level JSON fields. Only safe methods are sent unless `-all` is given; `-header` adds credentials and `-json` prints a machine-readable report. It exits `1` on drift.

- Add benchmarks for matching against 3,000 routes, template rendering, `ServeHTTP` and parsing (`make bench`).

- Add `@rate-limit = N/window` (such as `5/1s` or `10/s`): once a route has answered N requests in a sliding window it answers `429 Too Many Requests` with `Retry-After`. The route details and `/routes` show the limit (`rateLimit`).

- Add `-w` (`--delay`) and `-f` (`--fault`) to delay or fault every route that has no `@delay` or `@fault` of its own, such as `--delay 50ms..300ms --fault "reset 5%"`. Library users call `Server.SetRouteDefaults`.

- Add `-l json` (`--log-format json`): stdout carries only JSON lines, with the startup banner and route list as records and one `http request` record per request. The request log's matched-route key is now `route` (was `mock`), and `duration` is logged as a duration value (nanoseconds in JSON).

- Add `mock init [-force] [dir]`, which writes a commented starter `api.http` and the `users.json` it serves, so a new API runs in one command. It refuses to overwrite existing files without `-force`.

- Add `@callback = METHOD URL` webhook callbacks, with `@callback.delay`, `@callback.header.Name` and `@callback.body` templates rendered from the triggering request. Each callback is sent once after the response, times out after 10 seconds, and is logged in the admin UI marked **callback**. `GET /requests?callbacks=true` and `RequestFilter.Callbacks` list them.

- Add `@compress = gzip | deflate | auto | off` to compress response bodies. `gzip` and `deflate` always compress; `auto` follows the request's `Accept-Encoding` and adds `Vary: Accept-Encoding`.

- `< path` response files served unchanged to `GET` and `HEAD` with a `200` status now support `Range`, `If-Range` and `If-Modified-Since` (via `http.ServeContent`): they answer `206`, `416` and `304` and send `Accept-Ranges` and `Last-Modified`.

- Add a `{{$request}}` placeholder, which inserts the request (method, URL, path, query, host, headers, body and decoded JSON body) as a JSON object, for echo routes. `{{$request.headers.Accept.0}}` and other paths read one part.

- Add `{{#each $json.path}} ... {{/each}}` blocks, which render once per element of a request body array (or a `$fixture` or `$item` array), with `{{$item}}`, `{{$item.field}}` and `{{$index}}` inside. JSON responses get commas between copies.

- Add `@fixture = data.json` and `{{$fixture.path}}` placeholders, which read values from a JSON data file using the same paths as `{{$json.*}}`. The file is re-read for each response and watched for reloads.

- Add `@proxy = URL`, which forwards the requests a section matches to a real service. A plain URL gets the request path appended, like `--upstream`; a URL with placeholders, such as `https://api.example.com/v2/{{$path.rest}}`, rewrites the path. Proxied requests count as matched and are marked **proxied** in the admin UI.

- Add `@xml.element.path` request matchers and `{{$xml.element.path}}` placeholders for XML bodies. Paths start at the root element, ignore namespace prefixes, accept `*` for any child, and end in `["@name"]` to read an attribute.

- `@form.name` matchers and `{{$form.name}}` placeholders read `multipart/form-data` bodies: text parts by content, file parts by file name.

- `@json` matchers and `{{$json.*}}` placeholders accept `["key"]` segments for object keys that contain dots or other punctuation, such as `@json.labels["app.kubernetes.io/name"] = web`.

- Add `@cookie.name` request matchers and `{{$cookie.name}}` placeholders, which read one cookie from the `Cookie` header. The admin UI's sample `curl` sends required cookies, and mismatch explanations never show cookie values.

- Add "present but not X" matcher values: `!text` and `!~regex` accept a present, non-blank value that is not `text` (or does not match `regex`), for every matcher type. **Breaking:** a matcher value that starts with `!`, such as `@header.X = !draft`, used to be an exact match for that text; write `~!draft` for that now.

- Docs: the README has a table of contents, one directive reference table for every `@` directive (values, defaults, effect), and a Recipes section: GraphQL operations, content negotiation, case-insensitive matchers, per-test routing with an `X-Test` header, and readiness checks for CI and Docker Compose.

- Add three ways to reload request files when file change events do not arrive (Docker Desktop bind mounts, network filesystems): `-i` / `--poll INTERVAL` checks file sizes and modification times on an interval, `SIGHUP` reloads at once, and `POST /reload` on the admin port reloads and returns the new state (`422` if the reload failed). `/reload` has the same cross-site protections as `/clear`.

- Add `-d` / `--dir DIR` to load every `.http` file under a directory, after any file arguments and in lexical path order. The directory tree is watched, so added, removed or renamed request files and new subdirectories are picked up on reload. `mock validate` also accepts directories. A positional directory still serves static files.

- Warn at load time, and in `mock validate`, when a route with a JSON `Content-Type` has a body that does not parse once rendered with sample request values, naming the route and the line within the body.

- Add the `mocktest` Go package for running a mock server from `.http` files inside a Go test: `mocktest.Start(t, "api.http")` serves on a random port, and `AssertCalled(t, "POST /users", 1)`, `AssertNotCalled` and `AssertAllMatched` check the requests it received.

- `GET /requests` on the admin port returns the request log as JSON, filtered
  by `method`, `path`, `route`, `matched` and `since`.

- Shadowed-route warnings now compare paths segment by segment, so they also catch a later route hidden by an earlier catch-all (`/api/*` before `/api/users/:id`), by a parameter (`/users/:id` before `/users/me`), or by the same path with a renamed parameter. `@path.*` matchers on the earlier route are taken into account.

- Add `-v` / `--var NAME=VALUE` (and `mock validate -var`) to set a variable in every request file from the command line. It overrides file and section definitions, supplies values for `{{name}}` references with no definition, and can set special variables such as `@delay`; it cannot set request matchers.
- Add `{{$env.NAME}}` placeholders, which read an environment variable of the `mock` process when the response is rendered. A request can never supply the value. An unset variable stays literal and warns at load time unless the placeholder has a `| default`.

- Answer `HEAD` requests from `GET` routes. When no `HEAD` route matches, the first matching `GET` route answers with its status and headers and no body, so health checkers, link checkers and clients that probe with `HEAD` no longer get a `404`. Explicit `HEAD` routes still win.

- Add any-element matching to `@json` paths: a `*` segment selects every element of an array, so `@json.roles.* = admin` matches when any role is `admin` and `@json.items.*.sku = ~A[0-9]+` checks every item. With a wildcard, `*` requires at least one non-blank element and `!` requires none. Mismatch reasons quote at most five received values. The admin UI's sample request for such a route puts the value in the first element.

- Add `-s` / `--strict`. At shutdown, `mock` writes a report to stderr listing requests no route matched (including ones forwarded with `--upstream`) and routes that never matched a request, and exits `1` if any request was unmatched, so a call to an unmocked endpoint can fail a CI run.
- Count how many requests each route answered. The admin UI's routes list and route details show the count, and `/routes` and request events report it as `hits`. Counts carry over a reload for routes that did not change.

- Add **Copy as route** to the admin UI's inspector for unmatched requests. It copies a draft `.http` section with the request's method and path, `@query.*`, `@header.Authorization`, and `@json.*` or `@form.*` matchers that require the fields the request sent, and a placeholder `[200]` JSON response. A request proxied with `--upstream` drafts the upstream's real response instead.

- Add `-u` / `--upstream URL` to pass requests that no route matches to a real API and return its response, so you can mock a few endpoints and use the real service for the rest. The upstream may be HTTP or HTTPS and may include a base path; the request path and query are appended to it. Forwarded requests keep their method, headers and body, and get the upstream's `Host` plus `X-Forwarded-*` and `Via` headers. An unreachable upstream answers `502`, and a request that loops back to `mock` answers `508`. The admin UI marks forwarded requests as **proxied**, and request events carry `match.upstream`. `--upstream` cannot be combined with a directory argument.

- Add placeholders in body file paths: `< ./users/{{$id}}.json` serves a different file per request. The expanded path gets the same containment checks as a fixed path, and a path that names no usable file (missing, absolute, or with a `..` segment) answers `404`.
- Add `{{#repeat n}} ... {{/repeat}}` and `{{#repeat min max}}` blocks for generated lists, with fresh generated values in each copy and `{{$index}}` for the copy's position. Copies in JSON responses are separated by commas.
- Add placeholder filters: `upper`, `lower`, `trim`, `urlencode`, `base64`, `json`, `add n`, `default value` and `keep [name]`, as in `{{$query.page | default 1}}`. `keep` reuses one generated value across a response, its headers and its file path.
- Add explicit placeholder namespaces `{{$path.name}}`, `{{$query.name}}` and `{{$header.Name}}`.
- Add `@seed` to make generated values (faker data, `$uuid`, `$randomInt`, repeat counts, timestamps) reproducible per request.
- Add random delays: `@delay = 100ms..800ms` picks a new delay in the range for each request.
- Add `@stream = 50ms` to send a body one line, or one server-sent event, at a time.
- Add `@fault = reset` and `@fault = truncate`, optionally applied to a share of requests (`@fault = reset 25%`). Request events carry `response.fault`, and the admin UI shows a reset as `RST`. Routes report `stream` and `fault`, and a delay range as `"100ms..800ms"`.
- Add `@template = auto|on|off` to choose whether placeholders are expanded in a body file, or to serve a body and headers exactly as written.
- Add `@trim = off` to keep an inline body exactly as written, including its line endings and trailing blank lines.

## v0.4.0 - 2026-11-06

Choose responses from the request itself: new header, query, path, body, JSON and form matchers, regex captures and request-body placeholders, wildcard paths, and a simple rule for which route answers. This release has breaking changes; see below.

- **Breaking:** remove response rotation. The first route in load order that matches a request now always answers it, so put specific routes before catch-alls and use matchers to return different responses. A route that an earlier route always beats gets a load warning, and `mock validate -strict` fails on it. Retry-style "fail, then succeed" mocks have no replacement yet; a plan is in `docs/stateful-scenarios.md`.
- **Breaking:** remove the `/reset` admin endpoint and the **Reset responses** button. Request events no longer include a response position (`match.position`, `match.total`).
- **Breaking:** new matcher syntax takes over some values and names. An `@header.*` value of exactly `!`, or one starting with `~`, is now a matcher. Variables named `query.*`, `path.*`, `body`, `json.*` and `form.*` are now matchers instead of `{{name}}` variables. A `*` path segment is now a wildcard; write `%2A` for a literal `*`.
- Add matcher values for every request matcher: exact text, `*` (present and not blank), `!` (absent or blank), and `~regex` (a Go regular expression matching the whole value; start with `(?i)` for case-insensitive). An invalid regex is a load error.
- Add `@query.name` and `@path.name` matchers alongside `@header.Name`.
- Add body matchers. `@body` matches the whole request body (`.` in a `@body` regex also matches newlines), `@json.field.path` matches a field of a JSON body (numeric segments index arrays), and `@form.name` matches a field of a URL-encoded form body. Body matchers see the first 1 MiB of a request.
- Add wildcard paths. A trailing `*` segment matches the rest of the path, including nothing, so `GET /api/*` placed last makes a fallback route. `*name` makes the rest available as `{{$name}}`.
- Add request-body placeholders: `{{$json.user.name}}` and `{{$form.username}}` insert fields of the request body into response bodies and headers (JSON objects and arrays as compact JSON).
- Add regex captures: named groups in any matcher regex, such as `@header.Authorization = ~Bearer (?P<token>.+)`, become `{{$token}}` placeholders. Path parameters win over captures, and captures over query parameters.
- Unmatched-request diagnostics explain which query, path, header, body, JSON or form matcher failed, without repeating request header values. Route details in the admin UI list every requirement, and sample `curl` commands send a matching JSON, form or text body and fill wildcards.
- Rework the examples. `examples/user.http` is a complete users API (list with a role filter, get, `HEAD`, create with `201`/`409`/`400`, `PUT`, `PATCH`, `DELETE`, password reset); the HTML, chords, status and delay routes move to a new `examples/static.http`. The new `examples/matching/` folder has one file each for headers, JSON, forms, paths and queries, and webhooks, plus `requests.sh`, which sends a request for every scenario. Every section of every example has a `# Run: curl ...` line.
- Reorganize the source tree: `main.go` is now a thin entry point, the command lives in `internal/cli/`, and the admin UI (embedded files, handler, and Node tests) lives in `internal/ui/`. Install paths, flags, and build flags are unchanged.

## v0.3.1 - 2026-11-04

A refreshed admin UI with new light and dark themes, plus long names for every flag and a `MOCK_BIND` environment variable.

- Restyle the admin UI with a simpler, flatter look: a warm parchment light theme, a deep "midnight" dark theme with a teal accent and clearly layered panels, and a smaller header. The inspector drops its title bar: Request and Response get the same headers as Traffic and Routes, and Copy cURL moves next to the Request buttons. By default the Routes panel is as tall as the Response pane.
- Add a long name for every flag: `--port` (`-p`), `--ui-port` (`-a`), `--no-ui` (`-x`), and `--bind` (`-b`). The short names are unchanged. `mock -h` now lists both names on one line.
- Add the `MOCK_BIND` environment variable to set the default bind address, matching `MOCK_PORT` and `MOCK_UI_PORT`. An explicit `-b` still wins, and the default stays `127.0.0.1`.

## v0.3.0 - 2026-11-04

Serve several mock services, each on its own port, from one set of request files. This release has breaking changes; see below.

- **Breaking:** the port of a full-URL request target now picks the port the route is served on. `GET http://localhost:9090/token` listens on `9090` even when `-p` is set; `-p` (or `MOCK_PORT`) is now the default port for routes that do not declare one. The scheme and host name are still ignored.
- Add the `@port` control variable to put a section, or a whole file, on a port without writing a URL. A URL port that disagrees with `@port` is a load error.
- Bind every port the request files use before serving, and exit naming the file and section when a port a route declares is in use or is the admin UI port. The default port is only opened when a route uses it.
- Match each route only on its own port. Unmatched-request diagnostics explain when a route is served on another port.
- Reject a hot reload that needs a port `mock` is not listening on, keeping the previous routes, and ask for a restart.
- Print one startup line per listener and group the route list by port when there is more than one. The admin UI labels routes with their port, shows each request's host and port when there is more than one listener, and sample `curl` commands target each route's port.
- **Breaking:** remove the `-cors` flag. `mock` is for HTTP clients such as curl, tests, and services, not browser pages; `OPTIONS` requests, including preflights, now always run the matching route.
- **Breaking:** remove the `-cert` and `-key` flags. `mock` now serves plain HTTP only; put a TLS-terminating proxy in front of it when a client needs HTTPS.
- Convert `examples/user.http`, `small.http`, `delay.http`, `strategy.http`, and `secure.http` to path-only targets so `-p` still moves them. `examples/restclient.http` keeps its `{{host}}` URLs. Add `examples/services.http`, a multi-port example.

## v0.2.1 - 2026-10-29

Help flag fix and documentation corrections after v0.2.0.

- Print usage and exit `0` for `-h`, `-help` and `--help` (and `mock validate -h`) instead of failing with "flag: help requested".
- Expose the admin port (`8081`) in the Docker image.
- Correct the README: version reporting for `go run`, `go build` and `make build`, the `send-requests.sh` description, the admin POST protection, header-match semantics, and the Docker and Quick Start examples.

## v0.2.0 - 2026-10-29

The request console moves to its own port, `.http` files get `@name = value` variables, and `mock validate` and REST Client–style generator placeholders arrive. This release has breaking changes; see below.

- **Breaking:** serve the request console on its own port instead of under `/mock/` on the mock port, so any path (including `/mock`) can be mocked. The console is at the root of the admin port, with the same events, clear, reset, state, and routes endpoints.
- Add `-a PORT` for the admin port. It defaults to `8081`, or to a random free port when `-p` or `MOCK_PORT` is set. `-a 0` always picks a random port. The `MOCK_UI_PORT` environment variable sets the admin port when `-a` is omitted. `mock` exits with an error if the mock and admin ports are the same. The startup banner prints the actual address.
- Add `-x` to run without the admin UI. It cannot be combined with `-a`.
- Remove the `-l` flag.
- Show a copyable sample `curl` command for the mock server when a route is selected in the admin UI, with sample values for path parameters.
- Add an optional `[status]` prefix on section names (for example `### [204] Delete a user`) to set the response status. It defaults to `200`.
- Define variables as `@name = value`. Variables above the first `###` (or in a `###` section with no request line) are file-scoped; variables between a `###` line and its request line are method-scoped and override file ones. Values may be escaped (`\n`) and may reference other variables.
- Reference variables as `{{name}}`, or `{{%name}}` to percent-encode. `{{$name}}` now means only path/query parameters and generated values.
- Replace the `# $delay=` and `# $header.Name=` directives with `@delay` and `@header.Name`. All `# $name=value` comment variables are now a load error that points to the `@` form.
- Replace the `$file` variable with an external body: start the body with `< ` followed by the file path, such as `< ./users.json`. Paths must be relative, without `..` segments, and are looked up next to the `.http` file and then in the workspace root; symlinks cannot escape either directory. `$file` and `@file` are now load errors that point to the new syntax.
- Warn about unused variables once per definition instead of per section.
- Remove the `$status` variable; using it is now a load error that points to the section-name status.
- Expand `{{name}}` variables in the request target and ignore the scheme and host of a full URL, so `GET {{host}}/users` in a shared REST Client file matches `/users`. See `examples/restclient.http`.
- Add REST Client–style generator placeholders with arguments: `{{$randomInt min max}}`, `{{$timestamp [offset option]}}`, `{{$datetime rfc1123|iso8601 [offset option]}}`, and `{{$localDatetime rfc1123|iso8601 [offset option]}}`. `{{$guid}}` already worked. Invalid arguments stay literal and log a load-time warning.
- Add `mock validate [-strict] [-json] <file.http>...` to check request files without starting a server. It reports load errors and the same warnings `mock` logs at startup, exits non-zero on errors (or warnings with `-strict`), and can print a JSON report for CI.
- Convert the example `.http` files to the new syntax.

## v0.1.4 - 2026-09-06

Audit cleanup after v0.1.3: tighter admin CSRF checks and small correctness/housekeeping fixes.

- Tighten CSRF checks on admin clear/reset (`POST /mock/clear`, `POST /mock/reset`)
- Fix a tautological `statusAllowsBody` check
- Remove dead helpers / leftover scaffolding and the unused `astra` CI branch reference

## v0.1.3 - 2026-09-06

Expanded request inspection, more reliable traffic capture and reloads, and
correctness and security fixes since v0.1.2.

- Add draggable, keyboard-accessible panel dividers with remembered sizes.
- Add Pretty/Raw views, body and cURL copy actions, clickable route configuration,
  matched fixture/sequence metadata, mismatch explanations, and mobile navigation.
- Show active configuration revisions and reload errors in the dashboard.
- Separate clearing traffic from resetting fixture sequences (`POST /mock/reset`).
- Increase retained traffic to 1,000 requests and show the retention limit in the counter.
- Give HTTP methods distinct colors in both themes, use light inspector backgrounds
  and dark text in light mode, and match inspector heading sizes to Traffic and Routes.
- Reclaim dashboard space by showing reload errors only when needed, keeping request
  duration in inspector metadata, and removing copy-success notices.
- Add `examples/send-requests.sh` to populate the console with sample traffic and
  a PUT user-creation example; refresh the README screenshot with the dark-mode UI.
- Correct encoded path matching, isolate header-based response sequences, and own route snapshots.
- Reject malformed query/header constraints and invalid response directives at load time.
- Preserve unresolved placeholders, escape JSON string interpolation, and contain response-file reads.
- Serialize reloads and update dependency watches after configuration changes.
- Recover SSE overflow/restarts with ordered session cursors and synchronized clear boundaries.
- Export structured, byte-aware HAR captures including TLS, binary encoding, and truncation metadata.
- Buffer paused traffic, batch stable dashboard rows, and clear evicted request details.
- Isolate admin CORS, bound response writes, and validate CLI mount/port values.
- Update `golang.org/x/sys` to v0.44.0 for the Windows Unicode conversion advisory.
- Check formatting without mutation and verify Go/dashboard tests and dependencies before release.

## v0.1.2 - 2026-09-03

Security and correctness hardening, plus further request-console refinements
after the v0.1.1 redesign.

- Defaulted the bind address to localhost (`127.0.0.1`) and warned when binding
  all interfaces (`-b 0.0.0.0`), since the admin UI is unauthenticated
- Limited CORS short-circuiting to genuine browser preflight requests (`OPTIONS`
  with `Access-Control-Request-Method`); other `OPTIONS` now run the mock route
- Required `X-Requested-With` or a JSON `Content-Type` on `POST /mock/clear` to
  guard the admin clear endpoint against cross-site requests
- Keyed response rotation by route (method, path, and query) so repeated
  requests rotate through the intended set of responses
- Made the request-log export produce valid HAR (HTTP Archive) files
- Ran the Docker image as the unprivileged `nobody` user
- Added load-time warnings for `.http` dialect issues, such as using a
  well-known incoming request header as a response header without a matching
  `$header.*` matcher
- Reworked the request console into a denser operator layout: request and route
  counts on the panel titles, larger headings, a sticky table header,
  keyboard-navigable log rows, clearer Pause help text, and enabling Clear only
  after the server confirms the log was cleared
- Removed unused table headers from the request log
- Gave the selected request row in the Traffic panel its own bolder jade-green
  background and accent bar so it is clearly distinct from the lighter hover
  highlight, including when the selected row itself is hovered
- Removed OpenAPI stub seeding (`-openapi`, `examples/openapi.json`, `examples/openapi.yaml`). Use `.http` files.
- Updated the README screenshots to show the current console

## v0.1.1 - 2026-09-02

This release redesigns the request-log web interface as a clearer, more
practical console for inspecting local HTTP traffic.

- Reworked the interface into a responsive two-column workspace with the
  request log and configured routes on the left and full-width request and
  response inspectors on the right
- Added a persistent Hide / Show control for the configured-routes panel so
  the request log can use the available vertical space
- Added live connection state, request and route counters, compact HTTP method
  and status badges, and a `/` keyboard shortcut for filtering
- Refined light and dark themes, focus states, empty states, responsive
  behavior, and control accessibility
- Removed inspector transition flashes when selecting or receiving requests
- Updated the README screenshot to show the redesigned interface in use

## v0.1.0 - 2026-08-29

First tagged release.

`mock` serves REST Client-style `.http` files as a local HTTP server, with a
request-log UI, hot reload, and OpenAPI stub seeding.

- CLI: `-p` / `MOCK_PORT`, `-b`, `-l`, `-cors`, `-cert`/`-key`, `-openapi`, `-version`
- Request files: named `###` sections, `$status`, `$delay`, `$file`, `$header.*`, placeholders, rotation
- Hot reload of command-line `.http` files and relative `$file` bodies
- Admin UI: live log, routes panel, filter, pause, clear, HAR export, theme toggle, Help
- Docker image for running examples or a mounted request file
- `make build` installs into `GOBIN` and always rewrites the binary
