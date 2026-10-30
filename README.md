# mock

[![CI](https://github.com/sspencer/mock/actions/workflows/go.yml/badge.svg)](https://github.com/sspencer/mock/actions/workflows/go.yml)
[![Release](https://img.shields.io/github/v/release/sspencer/mock)](https://github.com/sspencer/mock/releases/latest)
[![License: Unlicense](https://img.shields.io/badge/license-Unlicense-blue.svg)](LICENSE)

`mock` turns REST Client-style `.http` files into a local HTTP server with hot
reload and a request-log UI. It is a single binary for the gap between
hand-written test doubles and a real backend — not a replacement for WireMock,
Prism, or Mockoon, and not the Go unit-test mocking libraries (gomock, mockery).

## Install

Requires [Go](https://go.dev/dl/) 1.26 or newer:

```sh
go install github.com/sspencer/mock@v0.1.4
mock -version
```

Binaries for Linux, macOS, and Windows are on the
[Releases](https://github.com/sspencer/mock/releases) page. Docker instructions
are [below](#docker). To build from a clone, see [Building From Source](#building-from-source).

This binary is named `mock`. If you already have another `mock` on your `PATH`,
the new one will shadow it or be shadowed, depending on install order.

## Quick Start

Run the example API:

```sh
go run . examples/user.http
```

The server prints a short banner with the listen address, the admin UI URL, and
whether it is watching files, then the route list. It serves the mock API on
`127.0.0.1:8080` and the admin UI on `127.0.0.1:8081` by default:

```text
starting mock HTTP server on 127.0.0.1:8080
admin UI at http://127.0.0.1:8081/
watching request files for changes
Available mock methods:
  GET     /                              Return home page external html file
  POST    /users                         Create random user
  GET     /users/:id                     Return any user
```

While it is running, it watches the given `.http` file(s) and any `< path`
response bodies they reference. Save a file and the server reloads the routes
and reprints the updated method list. A failed reload keeps the previous routes
and logs the error.

```sh
curl http://localhost:8080/users/42
curl -X POST http://localhost:8080/users
curl http://localhost:8080/names?type=cat
```

Open the request console at [http://localhost:8081/](http://localhost:8081/).
The responsive UI shows each request and response with raw HTTP-style details,
a collapsible live-routes panel, filter/pause/clear controls, HAR export, light
and dark themes, and a Help dialog that explains those controls.

To populate the console during UI development, start the server with
`examples/user.http`, then run this Bash/curl script in another terminal:

```sh
./examples/send-requests.sh          # MOCK_PORT, or 8080 if unset
./examples/send-requests.sh -p 9000  # overrides MOCK_PORT
```

It sends 15 requests across the example routes, including JSON request bodies,
the rotating `201`/`400` responses, a `204` deletion, and the delayed response.
It prints status/timing summaries and leaves response bodies for inspection in
the console. Rerun it whenever you need more traffic.

![Web Interface](./docs/web.png)

*Dark-mode request console populated with `examples/send-requests.sh`, showing a successful POST request and its response.*

You can also pipe a request file through stdin:

```sh
cat examples/user.http | go run .
```

## Building From Source

Common development commands:

```sh
make test
make all
make build
```

`make build` always writes the `mock` binary into `GOBIN`, or `GOPATH/bin` when
`GOBIN` is not set. `make` / `make all` does the same after tests pass, even if
Make thinks the sources are unchanged. The binary reports `git describe` via
`-version` when built this way.

After building:

```sh
mock -p 9090 -a 9091 -b 127.0.0.1 examples/user.http
```

That serves the mock API on `127.0.0.1:9090` and the request log at
`http://127.0.0.1:9091/`.

```sh
mock -version
```

## Docker

Build the image:

```sh
docker build -t mock .
```

Run it with a `.http` file from your host machine. Containers must bind
`0.0.0.0` so published ports are reachable from the host (`-b` defaults to
localhost):

```sh
docker run --rm \
  -p 8080:8080 -p 8081:8081 \
  -v "$PWD/examples/user.http:/mock/user.http:ro" \
  mock -b 0.0.0.0 /mock/user.http
```

The bind mount keeps the request file outside the image. Rebuild the image only
when the `mock` binary or static UI changes. Because the file is mounted from
the host, edits to the host `.http` file are picked up by the running container
and reloaded automatically.

If the request file uses `< path` external response bodies, mount the whole
directory so relative file references are available inside the container:

```sh
docker run --rm \
  -p 8080:8080 -p 8081:8081 \
  -v "$PWD/examples:/mock/examples:ro" \
  mock -b 0.0.0.0 /mock/examples/user.http
```

## CLI

```text
mock [flags] <file.http> [file.http...]
mock [flags] <directory>
cat file.http | mock
mock -version
```

Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-p` | `8080`, or `MOCK_PORT` | HTTP port |
| `-x` | (off) | Do not start the admin UI. Cannot be combined with `-a` |
| `-b` | `127.0.0.1` | Bind address. Use `0.0.0.0` to listen on all interfaces |
| `-a` | `MOCK_UI_PORT`, else `8081`, or random if `-p` / `MOCK_PORT` is set | Admin UI port. `0` picks a random free port |
| `-cors` | (off) | `Access-Control-Allow-Origin` value (`*` or an origin). Only browser preflight `OPTIONS` are short-circuited |
| `-cert` / `-key` | (off) | Enable HTTPS with the given certificate and key |
| `-version` | | Print version and exit |

If `-p` is omitted, `mock` uses the `MOCK_PORT` environment variable when it
is set. An explicit `-p` flag always wins.

The admin UI listens on its own port so it never shadows mock routes. Without
`-a`, it uses the `MOCK_UI_PORT` environment variable when set; otherwise `8081`
when the mock port is the default, and a random free port when you set `-p` (or
`MOCK_PORT`). An explicit `-a` always wins. The startup banner prints the actual
admin address. If the two ports resolve to the same number, `mock` exits with an
error. Pass `-x` to skip the admin UI and its port entirely. With a directory
argument (static file server) there is no admin UI.

You can pass one or more `.http` files. When no files are passed, `mock` reads
from stdin. Empty input fails fast with an error that points at the expected
request-section format.

Request files passed on the command line are watched for changes, including
relative `< path` response bodies. On save, `mock` reloads those files, swaps in
the new routes without restarting the process, and prints the updated route list.
SIGINT/SIGTERM shut the server down cleanly and stop the file watcher.

If you pass a single directory, `mock` serves that directory as a static file
server from `/` instead of loading mock routes or the request-log UI.

## Request File Format

**Headers after the request line are response headers.** To match incoming
request headers such as `Authorization`, `Cookie`, or `Accept`, use
`@header.Name = value` above the request line — not a header below it.
`Content-Type` after the request line is the usual way to set the mock response
content type. `mock` warns at load time if a well-known incoming header is used
as a response header without a matching `@header.*` matcher.

Each response starts with `###`, followed by an optional `[status]` and a name,
optional variables, an HTTP request line, optional **response** headers, a blank
line, and an optional body. File-wide variables may be defined above the first
`###` (see [Variables](#variables)).

```http
### Return user
GET /users/:id
Content-Type: application/json

{
  "id": "{{$id}}",
  "name": "{{$name}}",
  "requestId": "{{$uuid}}"
}

### [204] Delete user
DELETE /users/:id
```

The request target may be a path or a full URL, and may use variables such as
`GET {{host}}/users`. Variables are expanded first, then everything before the
path (scheme and host) is dropped; only the path and query string are used for
matching. This lets one file serve both a REST client and `mock`, as in
[`examples/restclient.http`](examples/restclient.http). Headers and bodies are
still read as the mock's response, so a REST client's request headers there
trigger a warning.

Start a section name with a status code in square brackets to set the response
status: `### [204] Delete user` returns `204` and is named "Delete user". The
bracketed code is optional and defaults to `200`. The supported final status
range is `200`–`999`; invalid codes are load errors. Brackets that do not hold
only digits, such as `### [beta] Users`, stay part of the name. The old
`$status` variable is no longer supported and is also a load error.

### Supported dialect

`mock` supports a practical subset of JetBrains REST Client / `.http` files:

| Supported | Not supported |
|-----------|----------------|
| `###` named sections | Request separators beyond `###` |
| `@name = value` file and method variables | Environment files / `{{env}}` from IDE |
| Request line `METHOD /path` | Full multi-step scripts |
| Response headers after the request line | Separate request vs response documents |
| `@header.Name = value` request header matchers | Body-content matchers |
| `{{name}}`, `{{%name}}` and `{{$placeholder}}` in bodies and response headers | Request variables (`{{login.response.body.token}}`) |
| `< path` external body files | Mixing inline text and `< path` in one body; imports of other `.http` files |

Inline bodies normalize line endings to LF and discard trailing blank lines.
A line beginning `###` is always a section boundary, including within a body.
Use an external body file (`< path`) for content that must preserve line endings, trailing
whitespace, or literal section delimiters. Individual inline lines are limited
to 1 MiB. Malformed queries, invalid headers, and invalid directives
produce source-and-line errors; a failed reload retains the working routes.

## External response bodies

To serve a file as the response body, make the body a single line that starts
with `<` and a space, followed by the file path:

```http
### Return users
GET /users
Content-Type: application/json

< ./users.json
```

The path is everything after `< `, whitespace included, so names with spaces
work as written. It must be relative, and is looked up:

- next to the `.http` file that names it; then
- relative to the workspace root, which is the directory `mock` runs from. This
  is tried only when the file does not exist next to the `.http` file.

Absolute paths and `..` path segments are load errors. A file must stay inside
the directory it is found in (the `.http` file's directory or the workspace
root); symlinks cannot escape it, including when a file is replaced during a
reload.

The body must be only that line; extra text after it is a load error. A first
line that starts with `<` without a following space, such as `<html>`, is an
ordinary inline body. The old `@file` and `$file` variables are no longer
supported and are a load error.

The file is read on every request, so edits show up immediately, and `mock`
watches it for reloads. A missing file returns a 500 for that route. If no
explicit `Content-Type` header is set, it is inferred from the file extension
when possible. Placeholders (`{{name}}`, `{{$name}}`) are expanded when the
content type is text, JSON, XML or JavaScript (explicit or inferred) and the
file is valid UTF-8 without binary control bytes. Other files are served as raw
bytes.

## Variables

A variable is defined on a line of its own as `@name = value`. The name cannot
contain spaces. The value can hold any characters, including spaces; leading
and trailing whitespace is trimmed. Use a backslash to write a line break
(`\n`), a tab (`\t`), a carriage return (`\r`) or a literal backslash (`\\`).
A value may reference other variables, such as `@url = {{host}}/users`, and
generated values, such as `@id = {{$uuid}}`.

Where a variable is defined sets its scope:

- **File scope:** above the first `###` line, or in a `###` section that holds
  only variable definitions (no request line). File variables apply to every
  section in the file, wherever they are defined.
- **Method scope:** between a `###` line and its request line. These apply only
  to that section and override a file variable of the same name.

```http
@host = example.com
@delay = 250ms

### Home
GET /

< index.html

### Slow users
@delay = 2s
@header.Authorization = Bearer secret
GET /users
Content-Type: application/json

{"link": "https://{{host}}/users?q={{%query}}"}
```

A blank line between the definitions and the request line is allowed but not
required. Plain `# comment` lines may also appear there. The old
`# $name=value` comment variables are no longer supported and are a load error
that points to the `@name = value` form.

Variable names with a special meaning, which `mock` consumes itself:

- `@delay`: response delay parsed with Go duration syntax, such as `250ms` or `2s`. Negative or invalid durations are load errors. Set at file scope, it is the default for every section.
- `@header.Name`: require the incoming request to include that header. Use `*` as the value to accept any non-empty header. Set at file scope, it applies to every section; a method-scoped header of the same name replaces it.

Every other variable is only used through `{{name}}` references. `mock` warns
at load time about variables that nothing references.

## Placeholders

Response bodies **and response headers** can contain placeholders:

- `{{name}}` inserts the value of the `@name` variable in scope.
- `{{%name}}` inserts it percent-encoded (a space becomes `%20`, `&` becomes `%26`).
- `{{$name}}` inserts a request or generated value. `mock` resolves it from:
  - Path parameters, such as `:id` in `/users/:id`.
  - Query parameters, such as `type` in `/names?type=cat`.
  - Built-in generated values.

Path parameters take precedence over query parameters with the same name.
Variable and placeholder names may contain dots and hyphens after their first character.
Unknown placeholders remain literal and produce load-time warnings; query
parameters supplied only at request time can resolve those warnings at runtime.
Inside JSON strings, inserted values are JSON-escaped. Outside strings, values
are inserted literally, so numeric and boolean placeholders retain their types.
Response headers containing invalid characters after expansion return an error.

Useful generated values:

```text
{{$name}}          {{$firstName}}      {{$lastName}}
{{$user}}          {{$email}}          {{$phone}}
{{$url}}           {{$server}}         {{$hash}}
{{$bool}}          {{$integer}}        {{$float}}
{{$uuid}}          {{$guid}}           {{$timestamp}}
{{$isoTimestamp}}  {{$file}}           {{$sentence}}
{{$paragraph}}     {{$article}}
```

Generated values are random faker data and are recalculated each time a
response body is rendered.

These REST Client–style generators take arguments:

```text
{{$randomInt min max}}
{{$timestamp [offset option]}}
{{$datetime rfc1123|iso8601 [offset option]}}
{{$localDatetime rfc1123|iso8601 [offset option]}}
```

- `$randomInt` returns an integer from `min` (inclusive) up to `max` (exclusive).
  `max` must be greater than `min`.
- `$timestamp` returns the current Unix time in seconds.
- `$datetime` returns the current UTC time as an RFC 1123 or ISO 8601 string;
  `$localDatetime` uses the local time zone.
- The optional offset is an integer (negative for the past) followed by one of
  `y` (years), `Q` (quarters), `M` (months), `w` (weeks), `d` (days), `h`,
  `m`, `s`, or `ms`. For example, `{{$timestamp -1 d}}` is one day ago and
  `{{$datetime iso8601 2 h}}` is two hours from now.
- Invalid arguments leave the placeholder literal and log a load-time warning.
- A path or query parameter named like a function (for example `?timestamp=`)
  wins over the bare placeholder, but not over a call with arguments.

Unknown placeholders resolve to an empty string.

## Matching

Routes match on HTTP method, path, any query parameters declared in the
`.http` file, and any `@header.*` matchers.

```http
### Cat names
GET /names?type=cat
Content-Type: application/json

{"type":"{{$type}}","names":["miso","taco"]}
```

`GET /names?type=cat` matches. `GET /names?type=dog` does not.

Path parameters are introduced with `:`.

```http
### User profile
GET /users/:id/profile
Content-Type: application/json

{"id":"{{$id}}"}
```

### Header matching

```http
### Secure read
@header.Authorization = Bearer secret
GET /secure
Content-Type: application/json

{"ok":true}
```

## Multiple Responses

If more than one response has the same method and URL (including across multiple
input files, in load order), `mock` rotates through the matching responses.
This is useful for retry paths and stateful client behavior without building a
stateful fake server.

```http
### [201] First create succeeds
POST /users
Content-Type: application/json

{"id":1}

### [400] Second create fails
POST /users
Content-Type: application/json

{"error":"duplicate user"}
```

Repeated `POST /users` requests return `201`, then `400`, then `201` again.
Use **Reset responses** to restart rotation counters. **Clear log** only removes traffic.

## Admin UI And API

The UI is served at the root of the admin port (`-a`, default `8081`):

| Path | Purpose |
|------|---------|
| `/` | Request log UI |
| `/events` | Server-sent events stream (with event `id` / `Last-Event-ID`) |
| `/clear` | `POST` clears stored events without changing rotation counters |
| `/reset` | `POST` resets response sequences without clearing traffic |
| `/state` | `GET` active revision, route count, last successful reload, and reload error |
| `/routes` | `GET` route configuration, including source location, match headers, status, delay, and body preview |

Selecting a route shows a sample `curl` command for the mock port, ready to paste
into a shell (**Copy cURL** copies it). The host is the one you used to open the
UI, path parameters get sample values (`:id` becomes `1`), and required request
headers from `@header.*` are included.

UI features: theme toggle, filter, pause stream, independent clear/reset controls,
HAR export, a Help dialog, and a routes panel that refreshes after hot-reload.
Both administrative POST endpoints require `X-Requested-With` or JSON
`Content-Type` (or a same-origin browser request), and reject cross-origin requests.

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
  instead of producing a misleading replay command. Copied credentials are included.
- Matched requests show the fixture name, source file/line, configuration revision,
  selected response position, and elapsed time. Unmatched requests show up to five
  nearby routes with method, path, query, and header mismatch explanations.
- Select a configured route to inspect its match requirements, response headers,
  variables, status, delay, and body template or file reference. File contents
  are not loaded into the route preview; large inline previews are truncated.
- Failed reloads retain active routes and show an error banner. The banner is
  hidden during normal operation to leave more room for traffic and details.
Pause freezes the table while buffering the latest 1,000 new requests. Resume
shows that traffic; the status indicates how many older paused requests were
omitted. The **Clear log** button clears server history across connected tabs
without changing response rotation. Traffic arriving after the clear boundary remains.

SSE cursors include a server session and sequence number. Slow subscribers
reconnect to replay retained events; a history gap or restart emits a `reset`
event before replay. Clear emits a `clear` event. Request events include the selected route,
sequence position, and configuration revision (or mismatch candidates). History is limited to 1,000
events; gaps are reported rather than silently hidden. Event bodies include
separate byte counts, truncation metadata, and base64 encoding for binary data.

Export HAR downloads captured requests, including their actual HTTP/HTTPS
scheme, byte sizes and binary response encoding. Capture is limited to 64 KiB
per body. HAR extension fields mark truncated bodies and binary requests; these
are diagnostic captures, not complete replay fixtures. Only total server handler
time is measured, so individual network timing phases remain unknown.

## CORS And TLS

```sh
mock -cors '*' examples/user.http
mock -cert cert.pem -key key.pem -p 8443 examples/user.http
```

`-cors` adds CORS headers to mock responses (or static-directory responses),
including `Vary: Origin`. Browser preflights (`OPTIONS` with
`Access-Control-Request-Method`) return `204` and reflect requested headers.
Other `OPTIONS` requests run the mock route. The admin port is never affected by
`-cors`, including when `-cors '*'` is used. Cross-origin clear requests are rejected.

With `-cert`/`-key`, the startup banner prints `https://` for the admin UI.
Binding to all interfaces (`-b 0.0.0.0`) prints a warning: the admin UI is
unauthenticated and request logs may include `Authorization` headers and bodies.

Normal HTTP writes have a 30-second budget beginning after any configured
response delay. SSE writes have a renewable 10-second deadline and idle streams
send heartbeats. Shutdown stops accepting connections and allows 400 ms for
active requests before closing remaining connections.

## Development

This repository is intentionally small:

- `main.go` / `watch.go` / `version.go` wire CLI flags, input loading, watching, and lifecycle.
- `restclient/` parses `.http` files.
- `mockhttp/` matches requests, renders responses, and streams request-log events.
- `static/` contains the request log UI (embedded at build time).
- `examples/` contains request files you can run locally.
- `docs/recommendations.md` tracks improvement history.

Before sending a change around:

```sh
make verify
```

`make fmt` formats Go files explicitly; verification never rewrites them.
`make all` also installs the binary. Dashboard tests require Node.js 22+.

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and pull-request notes. Release
history is in [CHANGELOG.md](CHANGELOG.md).
