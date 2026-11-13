[Documentation](README.md) › Admin UI and API

# Admin UI and API

## Admin UI And API

The UI is served at the root of the admin port (`-a`, default `8081`):

| Path | Purpose |
|------|---------|
| `/` | Request log UI |
| `/events` | Server-sent events stream (with event `id` / `Last-Event-ID`) |
| `/clear` | `POST` clears stored events |
| `/reload` | `POST` reloads the request files now and returns the new state (see [Reloading](cli.md#reloading)) |
| `/state` | `GET` active revision, route count, last successful reload, reload error, and the number of route `switches` in effect |
| `/routes` | `POST` adds temporary routes and `DELETE /routes/{id}` removes them (see [Temporary routes](#temporary-routes)); `GET` route configuration, including source location, port, matchers, status, delay, `throttle`, `override` (a description of the `/faults` override in effect), `switch` (the route's own override, as set), `fallback`, `chance`, `defaultContentType`, `statusTemplate`, `require`, `missing`, `etag`, `times`, `timesLeft`, `tags` and `temporary` when set, body preview, and `hits` (requests answered); `GET /routes?tag=billing` lists only the routes with that `@tag` |
| `/requests` | `GET` the request log as JSON, optionally filtered (see below) |
| `/verify` | `POST` checks how often calls were received and answers `200` or `422` (see [Verifying calls](#verifying-calls)) |
| `/resend` | `POST` answers a request as if a client sent it to the mock port (see [Resending a request](#resending-a-request)) |
| `/fixture` | `GET ?route=ID` reads a route's `@fixture` file; `PUT` writes it (see [Editing fixtures](#editing-fixtures)) |
| `/reset` | `POST` starts `@times` and `{{$hits}}` counts, `@rate-limit` budgets and `@match.chance` draws over, and removes `/faults` overrides and temporary routes, without a reload; answers like `/state` |
| `/faults` | `GET` lists run-time route switches and fault overrides; `POST` sets one for a route or `*` (delay, fault, throttle, `off`, `status`); `DELETE` clears them (see [Changing faults at run time](responses.md#changing-faults-at-run-time)) |

### Admin access token

The admin UI and API are open to anyone who can reach the admin port.
`-t TOKEN` (`--ui-token`, or the `MOCK_UI_TOKEN` environment variable)
requires a token for every admin page and endpoint (spaces around it, such
as a trailing newline read from a file, are ignored):

```sh
MOCK_UI_TOKEN=$(openssl rand -hex 16) mock -b 0.0.0.0 api.http
```

- In a browser, open the UI once as `http://HOST:8081/?token=TOKEN`. `mock`
  sets an HTTP-only, same-site cookie and redirects to the plain URL, so the
  token leaves the address bar. The cookie holds a SHA-256 digest of the
  token, not the token itself.
- API clients send `Authorization: Bearer TOKEN` (the scheme in any case) or
  add `?token=TOKEN`:

  ```sh
  curl -H "Authorization: Bearer $MOCK_UI_TOKEN" http://127.0.0.1:8081/requests
  ```

Other requests get `401`. The banner says a token is required but does not
print it. The mock ports are not affected. This is access control on plain
HTTP: anyone who can read the traffic between you and `mock` can read the
token too.

### Credential redaction

The request log keeps what clients sent, including `Authorization` headers,
cookies and passwords in bodies. `-R` (`--redact`) masks credentials before a
request is logged, so the admin UI, **Copy cURL**, **Copy as route**, HAR
exports, `GET /requests`, the event stream and the request and `@callback`
log lines never see them. Responses are sent unchanged, and matching and
[`-r`](real-apis.md#recording-forwarded-requests) recordings always use the real request, so recorded routes
replay what was sent. A value is masked as `[redacted]` when its name looks
like a credential: it contains `token`, `secret`, `passw`, `apikey`,
`authorization`, `credential`, `cookie`, `signature`, `session` or
`privatekey`, or is `auth`, `key`, `sig`, `pwd`, `jwt`, `otp` or `pin`
(ignoring case, `-` and `_`).

- Headers: `Authorization: Bearer [redacted]` keeps the scheme, `Cookie` keeps
  the cookie names, and `Set-Cookie` keeps its attributes. Other
  credential-like headers, such as `X-Api-Key`, are masked whole.
- Query parameters, such as `?access_token=[redacted]`.
- Members of JSON bodies, such as `"password": "[redacted]"`, at any depth.
  The whole value is masked, whether it is a string, a number, `true`, `null`,
  an object or an array. Fields of URL-encoded forms too. Other bodies are
  logged as sent.
- Query parameters in the URL of a `@callback` request and in its error.
- Values that near-miss explanations quote for those names.

Redaction is a guard against accidental sharing, not a guarantee: a secret in
a header, field or body with an ordinary name is still logged.

Selecting a route shows a sample `curl` command for that route's port (the
default mock port when the route declares none), ready to paste
into a shell (**Copy cURL** copies it). The host is the one you used to open the
UI, path parameters get sample values (`:id` becomes `1`), and required request
headers from `@header.*` and parameters from `@query.*` are included. `@json.*`
matchers become a sample JSON body (sent with `Content-Type: application/json`),
`@form.*` matchers a form body, and an exact `@body` value is sent as the body.
Wildcards get `example`. A `*` or `~regex` value becomes
a sample value (`example` for headers and JSON fields), which may not satisfy
the regex, and `!` matchers are left out.

UI features: theme toggle, filter, pause stream, clear log,
HAR export, a Help dialog, and a routes panel that refreshes after hot-reload.

The traffic log reads like a line map, newest request at the top. Each request
is a stop on the line, lit by its status: green for `2xx`, blue for `3xx`,
amber for `4xx` and red for `5xx` or a connection fault. A hollow stop is a
request no route answered. Each HTTP method has its own color plate (`GET`
blue, `POST` green, `PUT` orange, `PATCH` pink, `DELETE` red, others grey),
in the traffic log and the route list alike. The selected request or route is
highlighted in yellow. Request and response text is colored: header names and
JSON keys violet, strings green, numbers orange, and the start line in its
method or status color (bodies over 200,000 characters stay plain). Selecting
a route shows it as a sheet: its method and path, a table of its settings,
then its requirements, sample request and response.

The UI's typeface, Overpass (SIL Open Font License), is built into the binary,
so the page loads nothing from the network.
When `mock` listens on more than one port, the request table shows each
request's host and port, and routes are labeled with their port.
The `/clear`, `/reload` and `/resend` POST endpoints and `PUT /fixture` require an `X-Requested-With` header
or a JSON `Content-Type`, and reject cross-origin and cross-site requests.

### Reading the request log

`GET /requests` returns the request log as a JSON array, oldest first: the
same events the UI shows, with the request, the response, and the route that
answered (`match.route`, or `null`). It holds the latest 1,000 requests since
`mock` started or the log was last cleared. Query parameters narrow it, and
combine:

| Parameter | Keeps requests |
|-----------|----------------|
| `method=POST` | with this method (any case) |
| `path=/users/7` | whose URL path is exactly this, ignoring the query string |
| `route=Create user` | answered by the route with this name |
| `matched=true` / `matched=false` | a route answered, or none did (requests forwarded with `--upstream` count as unmatched) |
| `callbacks=true` | the outbound [webhook callbacks](responses.md#webhook-callbacks) instead, with `callback.trigger` (the event `id` of the request that sent each) and `callback.error` |
| `since=42` | with an event `id` greater than this |

```sh
curl -s 'http://127.0.0.1:8081/requests?method=POST&path=/users' | jq length
curl -s 'http://127.0.0.1:8081/requests?matched=false' | jq -r '.[].request.url'
```

A test can assert what its code under test sent: `POST /clear` (with
`X-Requested-With`) before the test, then read `/requests` after it. An
invalid `matched`, `callbacks` or `since` value returns `400`.

### Verifying calls

`POST /verify` checks how often requests were received, so a test in any
language can assert without parsing `/requests`. The body is one expectation
or an array of them:

```sh
curl -s localhost:8081/verify -H 'Content-Type: application/json' \
  -d '[{"call": "POST /orders", "times": 1}, {"call": "Search", "atLeast": 2}, {"allMatched": true}]'
```

| Field | Meaning |
|-------|---------|
| `call` | A method and path, such as `POST /orders` (exact path; add `?query` to require that exact query string), or the name of the route that answered |
| `times` | Received exactly this many times (`0`: never) |
| `atLeast`, `atMost` | Received at least, or at most, this many times; combine them for a range |
| `allMatched` | `true` on its own: no request went unmatched (requests forwarded with `--upstream` count as unmatched) |

A `call` with no count must have been received at least once. Counts cover the
request log (the latest 1,000 requests since `mock` started or `POST /clear`)
and leave out webhook callbacks. `mock` answers `200` when every expectation
holds and `422` when any fails, with `ok`, a `results` entry per expectation
(`want`, `got`, `ok` and a failure `message`), and a `message` that lists the
failures and the requests received:

```text
verify: 1 of 3 expectations failed
  POST /orders: called 0 times, want exactly 1 time
requests received:
  GET /search?q=a -> 200 ("Search")
  GET /search?q=b -> 200 ("Search")
```

With `Accept: text/plain`, `mock` sends only that message, so `curl -f` in a
shell script fails the step and prints why. A malformed body or unknown field
returns `400`. Like other admin `POST`s it needs a JSON content type or
`X-Requested-With`.

### Resending a request

The admin UI runs on its own port and `mock` sends no CORS headers, so the
browser cannot call the mock port itself. `POST /resend` answers a request
in-process instead, exactly as if a client had sent it to the mock port:
routes match (on the port in `host`), delays, faults, rate limits,
`--upstream`, callbacks and `--strict` counts apply, and the request is logged
with `resendOf` set to `from` (or `0`). The response is `{"id": N}`, the new
event's `id`; read the response from the request log.

```sh
curl -s -X POST -H 'Content-Type: application/json' http://127.0.0.1:8081/resend \
  -d '{"method":"POST","url":"/users?notify=1","host":"localhost:8080",
       "headers":{"Content-Type":["application/json"]},"body":"{\"name\":\"Ada\"}"}'
```

| Field | Meaning |
|-------|---------|
| `method` | Request method (required) |
| `url` | Path and query, starting with `/` (required) |
| `host` | `Host` header; its port picks the mock port. Without a port, or with a port no route is served on (a client behind a proxy), routes on every port can match |
| `headers` | Request headers, `{"Name": ["value"]}`. `Host`, `Content-Length`, `Transfer-Encoding` and `Connection` are ignored |
| `body` | Request body text, or base64 with `"encoding": "base64"` |
| `from` | The event `id` this request was copied from |

Invalid input returns `400`. With [`-R`](#credential-redaction), logged
credentials are `[redacted]`, so a resent copy carries that text unless you
edit it.

### Temporary routes

`POST /routes` with `.http` text adds routes in front of every route so far,
so a test written in any language can change one answer without its own
request file:

```sh
curl -s localhost:8081/routes -H 'X-Requested-With: curl' --data-binary $'### [500] Users are down\nGET /users\n\n{"error": "down"}\n'
```

```json
{"id":"t1","routes":["Users are down"]}
```

- The body is `.http` text (send `X-Requested-With`, as for other admin
  `POST`s), or JSON `{"source": "..."}`. The answer is `201` with the group's
  `id`, the route names and any load warnings; text that does not parse is a
  `400` with the error.
- The newest group is tried first, then older ones, then the file's routes.
  Routes stay until `DELETE /routes/{id}` (or `DELETE /routes` for all of
  them), a [`POST /reset`](#admin-ui-and-api), or a restart. Reloads keep them.
- They are parsed on their own: `-v`, `-e` and `-H` don't apply. Relative
  `< path` bodies and fixtures are found from the directory `mock` runs in.
- `/routes` lists them first, with `temporary` set to their group id, and the
  route inspector says how to remove them.
- Go tests use [`Stub`](testing.md#using-mock-in-go-tests) instead.

### Editing fixtures

A route with a JSON [`@fixture`](placeholders.md#fixture-data) shows **Edit fixture** in the route
inspector (CSV and NDJSON fixtures are edited in an editor; `/fixture`
answers `409` for them). The editor has two views:

- **Tree** shows the JSON as rows you can expand and collapse. Each value has
  a type (string, number, boolean, null, object or array) and an editor for
  it; changing the type converts the value where it can (`42` becomes `"42"`).
  Field names can be renamed in place, **+** adds a field to an object (or a
  copy of the last item to an array), **⧉** duplicates an array item and
  **×** removes a value. Edits apply when a field loses focus or Enter is
  pressed.
- **JSON** is the file as text. **Format** indents it with two spaces.
  Invalid JSON is reported with its line and column, and must be fixed before
  switching back to the tree or saving. Fixtures with more than 2,000 values
  open in this view only, and so do files the tree would change on save:
  integers beyond 2^53, numbers spelled differently from how JavaScript
  writes them (`1.0`, `1e3`), a field name repeated in one object, or a
  numeric field name (`"10"`) after other fields, which JavaScript would
  move first. The editor says which.

**Save** writes the file; fixtures are read for every response, so the next
request uses it. A file saved after a tree edit is written as two-space
indented JSON; a file saved from the JSON view is written exactly as typed.
**Reload** discards edits
and reads the file again, and **Close** hides the editor and keeps the draft
until another fixture is opened. Saving also triggers a hot reload (the
fixture is watched), and the route stays selected.

`GET /fixture?route=ID` takes a route `id` from `/routes` and returns
`{"file", "fixture", "content", "version"}`: the absolute path, the path as
the route wrote it, the file's text, and a version (`""` when the file does
not exist yet). `PUT /fixture` takes `{"file", "content", "version"}` and
returns the new `{"version"}`. A write is refused with `403` unless the file
is the `@fixture` of a current route, `400` unless the content is one valid
JSON value, `413` above 4 MiB, and `409` (with the current `version`) when
the file changed since that version was read. The file is replaced
atomically and keeps its permissions; a symlinked fixture has its target
replaced and the link kept.

```sh
curl -s -X PUT -H 'Content-Type: application/json' http://127.0.0.1:8081/fixture \
  -d '{"file":"/srv/api/data/catalog.json","content":"{\"featured\":{}}\n","version":"<version from GET>"}'
```

### Filtering traffic

The filter above the traffic list keeps requests that match every term,
separated by spaces (case does not matter):

| Term | Keeps requests |
|------|----------------|
| `2xx`, `3xx`, `4xx`, `5xx` | with a status in that class |
| `matched` / `unmatched` | a route answered, or none did (callbacks are neither) |
| `get`, `POST`, ... | with this method |
| `route:Users`, `route:"User detail"` | answered by the route with this name (quote names with spaces) |
| `tag:billing` | answered by a route with this [`@tag`](matching.md#route-tags) |
| anything else | whose method, URL, status, status text, label (`proxied`, `callback`) or fault contains it |

A `-` in front excludes instead: `post -2xx` shows failed POSTs. Clicking a
route's hit count in the routes list sets the filter to `route:"Name"`, and
clicking it again clears the filter. Press `/` to focus the filter.

### Resizing and mobile navigation

Drag the vertical divider to enlarge the traffic list or inspector. Horizontal
dividers resize traffic versus routes and request versus response. Divider sizes
are remembered locally. Keyboard users can focus a divider and use arrow keys,
Shift+arrow for larger steps, or Home/End. Double-click restores the default.
On narrow screens, selecting traffic or a route opens the inspector; use
**Back to traffic & routes** to return to the list.

### Inspecting requests and routes

- **Pretty / Raw** formats valid, complete JSON or shows the original captured
  content. Headers remain visible, and binary/truncated bodies are labeled.
- **Copy body** copies original captured text; binary captures offer **Copy base64**.
- **Copy cURL** produces a shell-quoted command including captured headers and
  complete text bodies. Incomplete or binary request bodies show an explanation
  instead of producing a misleading replay command. Copied credentials are
  included unless `mock` runs with [`-R`](#credential-redaction).
- Matched requests show the fixture name, source file/line, configuration revision,
  and elapsed time. Unmatched requests show up to five
  nearby routes with method, path, query, header, and body mismatch explanations.
  Requests forwarded with `--upstream` or by a `@proxy` route are marked
  **proxied** in the table (the filter matches `proxied`), and the inspector
  names the URL they were sent to. [Webhook callbacks](responses.md#webhook-callbacks) are
  marked **callback**, with the route and request that sent them.
- **Copy as route** copies a draft `.http` section to paste into a request
  file. It appears on every request except callbacks. The draft has the request's method and
  path, and requires the fields the request had without fixing their values:
  `@query.name = *` for each query parameter, `@header.Authorization = *` when
  the request sent one, and `@json.key = *` (or `@form.name = *`) for each
  top-level body field, up to eight of each. Path characters that have a
  meaning in routes (`*`, a leading `:`, `{`, `}`) are escaped, and `@port` is
  set when the request arrived on a port other than the default. The response
  is a placeholder `[200]` with an empty JSON object, except for a request
  proxied with `--upstream`, whose real status, `Content-Type` and body are
  used when the body can be pasted inline (with `@template = off` if it
  contains `{{`).
- On a request a route answered, **Copy as route** starts a more specific
  variant of that route: the draft reuses the response the route sent (as for
  a proxied request) and begins with a `# Variant of "Name" (file:line)`
  comment. Place it above that route, since the first matching route wins,
  and tighten its matchers, such as `@query.page = 2` instead of `*`.
- **Switches**, on a selected route, changes how it answers while `mock`
  runs, without editing files: **Off** lets later routes answer as if this
  one did not match, **Status** forces the response status, **Delay** takes any
  `@delay` value, and **Fault** injects a fault into a share of responses.
  Empty fields keep the route's own settings. **Apply** switches the route,
  which gets a red badge (`off`, or `switched`) in the route list; **Switch back
  on** removes the switch. Switches last until removed or a `POST /reset`, and
  survive reloads. They are the same overrides as the
  [`/faults`](responses.md#changing-faults-at-run-time) admin endpoint, and
  `/state` counts them as `switches`.
- **Resend** opens an editor with the request's method, path and query,
  headers and body; **Send** replays it (see
  [Resending a request](#resending-a-request)) and selects the new request,
  marked **resent**. It appears on every request except callbacks and requests
  whose body was not captured completely. Binary bodies are resent unchanged.
- **Compare…** picks the shown request as side A; select another request and
  choose **Compare with #N** to see both side by side. The comparison lists
  the method, host, path, status, route and revision, then query parameters,
  request headers and response headers as tables, and the request and
  response bodies as a line diff (`-` only in A, `+` only in B). JSON bodies
  are compared with sorted keys, so key order and formatting don't count as
  differences; binary bodies are only reported as identical or not. The
  summary counts differences, ignoring timing and the `Date` header.
  **Only differences** hides unchanged rows, **Swap** exchanges the sides, and
  **Close** returns to request B. A comparison stays open while the filter
  hides either request, until one leaves the log.
- **Edit fixture**, on a route with `@fixture`, edits its JSON file as a tree
  or as text and saves it (see [Editing fixtures](#editing-fixtures)).
- **Copy unmatched as routes**, above the traffic list, copies one draft
  section per distinct method and path among every unmatched request in the
  log, oldest first, so a session of traffic becomes a starting request file.
  Query strings don't split a path into separate sections, and a proxied
  request with a usable upstream response is preferred over one without.
  Callbacks and requests a route answered are skipped.
- The routes list shows how many requests each route has answered; dimmed `0`s
  are routes nothing has matched yet.
- Select a configured route to inspect its match requirements, response headers,
  variables, status, delay, and body template or file reference. File contents
  are not loaded into the route preview; large inline previews are truncated.
- Failed reloads retain active routes and show an error banner. The banner is
  hidden during normal operation to leave more room for traffic and details.
Pause freezes the table while buffering the latest 1,000 new requests. Resume
shows that traffic; the status indicates how many older paused requests were
omitted. The **Clear log** button clears server history across connected tabs.
Traffic arriving after the clear boundary remains.

SSE cursors include a server session and sequence number. Slow subscribers
reconnect to replay retained events; a history gap or restart emits a `reset`
event before replay. Clear emits a `clear` event. Request events include the selected route
and configuration revision (or mismatch candidates, plus `upstream` when the
request was forwarded). History is limited to 1,000
events; gaps are reported rather than silently hidden. Event bodies include
separate byte counts, truncation metadata, and base64 encoding for binary data.

Export HAR downloads captured requests, including byte sizes and binary
response encoding. Capture is limited to 64 KiB
per body. HAR extension fields mark truncated bodies and binary requests; these
are diagnostic captures, not complete replay fixtures. Only total server handler
time is measured, so individual network timing phases remain unknown.

## Clients and safety

`mock` serves plain HTTP only; put a TLS-terminating proxy in front of it if a
client needs HTTPS. It is meant to be called by HTTP clients such as curl, test suites, and
backend services, not by web pages in a browser, so it adds no CORS headers.
`OPTIONS` requests are matched against routes like any other method. `HEAD`
requests fall back to `GET` routes when no `HEAD` route matches (see
[Route order](matching.md#route-order)), so health and link checkers work without extra
routes. The admin
UI rejects cross-origin clear requests.

Binding to all interfaces (`-b 0.0.0.0`) without `-t` prints a warning: the
admin UI is unauthenticated and request logs may include `Authorization`
headers and bodies. Set an [admin access token](#admin-access-token) to
require a token for the UI and API, and [`-R`](#credential-redaction) to keep
credentials out of the log.

Normal HTTP writes have a 30-second budget beginning after any configured
response delay. SSE writes have a renewable 10-second deadline and idle streams
send heartbeats. Shutdown stops accepting connections and allows 400 ms for
active requests before closing remaining connections.
