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
go install github.com/sspencer/mock@v0.1.2
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
whether it is watching files, then the route list, and listens on `127.0.0.1:8080`
by default:

```text
starting mock HTTP server on 127.0.0.1:8080
admin UI at http://127.0.0.1:8080/mock/
watching request files for changes
Available mock methods:
  GET     /                              Return home page external html file
  POST    /users                         Create random user
  GET     /users/:id                     Return any user
```

While it is running, it watches the given `.http` file(s) and any `$file`
response bodies they reference. Save a file and the server reloads the routes
and reprints the updated method list. A failed reload keeps the previous routes
and logs the error.

```sh
curl http://localhost:8080/users/42
curl -X POST http://localhost:8080/users
curl http://localhost:8080/names?type=cat
```

Open the request console at [http://localhost:8080/mock/](http://localhost:8080/mock/).
The responsive UI shows each request and response with raw HTTP-style details,
a collapsible live-routes panel, filter/pause/clear controls, HAR export, light
and dark themes, and a Help dialog that explains those controls.

![Web Interface](./docs/web.png)

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
mock -p 9090 -b 127.0.0.1 -l inspect examples/user.http
```

That serves the mock API on `127.0.0.1:9090` and the request log at
`http://127.0.0.1:9090/inspect/`.

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
  -p 8080:8080 \
  -v "$PWD/examples/user.http:/mock/user.http:ro" \
  mock -b 0.0.0.0 /mock/user.http
```

The bind mount keeps the request file outside the image. Rebuild the image only
when the `mock` binary or static UI changes. Because the file is mounted from
the host, edits to the host `.http` file are picked up by the running container
and reloaded automatically.

If the request file uses `$file` response bodies, mount the whole directory so
relative file references are available inside the container:

```sh
docker run --rm \
  -p 8080:8080 \
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
| `-b` | `127.0.0.1` | Bind address. Use `0.0.0.0` to listen on all interfaces |
| `-l` | `mock` | URL path for the request-log UI (`/mock/`) |
| `-cors` | (off) | `Access-Control-Allow-Origin` value (`*` or an origin). Only browser preflight `OPTIONS` are short-circuited |
| `-cert` / `-key` | (off) | Enable HTTPS with the given certificate and key |
| `-version` | | Print version and exit |

If `-p` is omitted, `mock` uses the `MOCK_PORT` environment variable when it
is set. An explicit `-p` flag always wins.

You can pass one or more `.http` files. When no files are passed, `mock` reads
from stdin. Empty input fails fast with an error that points at the expected
request-section format.

Request files passed on the command line are watched for changes, including
relative `$file` response bodies. On save, `mock` reloads those files, swaps in
the new routes without restarting the process, and prints the updated route list.
SIGINT/SIGTERM shut the server down cleanly and stop the file watcher.

If you pass a single directory, `mock` serves that directory as a static file
server from `/` instead of loading mock routes or the request-log UI.

## Request File Format

**Headers after the request line are response headers.** To match incoming
request headers such as `Authorization`, `Cookie`, or `Accept`, use
`# $header.Name=value` above the request line — not a header below it.
`Content-Type` after the request line is the usual way to set the mock response
content type. `mock` warns at load time if a well-known incoming header is used
as a response header without a matching `$header.*` matcher.

Each response starts with `###`, followed by a name, optional variables, an HTTP
request line, optional **response** headers, a blank line, and an optional body.

```http
### Return user
# $status=200
GET /users/:id
Content-Type: application/json

{
  "id": "{{$id}}",
  "name": "{{$name}}",
  "requestId": "{{$uuid}}"
}

### Delete user
# $status=204
DELETE /users/:id
```

The request target may be a path or a full URL. Only the path and query string
are used for matching.

### Supported dialect

`mock` supports a practical subset of JetBrains REST Client / `.http` files:

| Supported | Not supported |
|-----------|----------------|
| `###` named sections | `@name` / request separators beyond `###` |
| `# $var=value` control variables | Environment files / `{{env}}` from IDE |
| Request line `METHOD /path` | Full multi-step scripts |
| Response headers after the request line | Separate request vs response documents |
| `# $header.Name=value` request header matchers | Body-content matchers |
| `{{$placeholder}}` in bodies and response headers | Imports of other `.http` files |
| `$file` relative body files | Absolute `$file` paths |

## Variables

Variables live in comments directly below the `###` line:

```http
# $status=201
# $delay=500ms
# $file=users.json
# $header.Authorization=Bearer secret
```

Supported control variables:

- `$status`: response status code. Defaults to `200`. Invalid values warn and fall back to `200`.
- `$delay`: response delay parsed with Go duration syntax, such as `250ms` or `2s`. Invalid values warn and are ignored.
- `$file`: response body file, resolved relative to the `.http` file.
- `$header.Name=value`: require the incoming request to include that header. Use `*` as the value to accept any non-empty header.

`$file` paths must be relative and cannot contain `..` path segments. If no
explicit `Content-Type` header is set, file-backed responses infer it from the
file extension when possible. Text files also expand `{{$...}}` placeholders;
binary-looking files are served as raw bytes.

## Placeholders

Response bodies **and response headers** can contain `{{$name}}` placeholders.
`mock` resolves them from:

- Path parameters, such as `:id` in `/users/:id`.
- Query parameters, such as `type` in `/names?type=cat`.
- Variables declared in comments, such as `$delay`.
- Built-in generated values.

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

Unknown placeholders resolve to an empty string.

## Matching

Routes match on HTTP method, path, any query parameters declared in the
`.http` file, and any `$header.*` matchers.

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
# $header.Authorization=Bearer secret
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
### First create succeeds
# $status=201
POST /users
Content-Type: application/json

{"id":1}

### Second create fails
# $status=400
POST /users
Content-Type: application/json

{"error":"duplicate user"}
```

Repeated `POST /users` requests return `201`, then `400`, then `201` again.
Clearing the request log from the UI also resets rotation counters.

## Admin UI And API

The UI is mounted under `-l` (default `/mock/`):

| Path | Purpose |
|------|---------|
| `/mock/` | Request log UI |
| `/mock/events` | Server-sent events stream (with event `id` / `Last-Event-ID`) |
| `/mock/clear` | `POST` clears stored events and rotation counters. Requires `X-Requested-With` or JSON `Content-Type` |
| `/mock/routes` | `GET` JSON list of currently configured routes |

**Path conflicts:** mock routes are registered on `/`. If a mock defines
`GET /mock/...`, it can shadow or confuse UI paths. Prefer keeping API routes
outside the UI mount, or change `-l`.

UI features: theme toggle, filter, pause stream, clear (server + client), HAR
export, a Help dialog, and a routes panel that refreshes after hot-reload.
Pause only freezes the live log in the browser; the mock server keeps serving
traffic. Export HAR downloads the requests currently in the log as a HAR
(HTTP Archive) JSON file.

## CORS And TLS

```sh
mock -cors '*' examples/user.http
mock -cert cert.pem -key key.pem -p 8443 examples/user.http
```

`-cors` adds CORS headers to every response, including `Vary: Origin`. Browser
preflight requests (`OPTIONS` with `Access-Control-Request-Method`) return `204`
and reflect `Access-Control-Request-Headers`. Other `OPTIONS` requests run the
mock route. `-cors '*'` lets any origin read the admin UI event stream (SSE).

With `-cert`/`-key`, the startup banner prints `https://` for the admin UI.
Binding to all interfaces (`-b 0.0.0.0`) prints a warning: the admin UI is
unauthenticated and request logs may include `Authorization` headers and bodies.

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
make all
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and pull-request notes. Release
history is in [CHANGELOG.md](CHANGELOG.md).
