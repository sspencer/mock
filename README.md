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
go install github.com/sspencer/mock@v0.2.1
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
  GET     /users                         List admins
  GET     /users                         List users
  GET     /users/:id                     Get user 0
  GET     /users/:id                     Get a user
  ...
```

While it is running, it watches the given `.http` file(s) and any `< path`
response bodies they reference. Save a file and the server reloads the routes
and reprints the updated method list. A failed reload keeps the previous routes
and logs the error.

```sh
curl http://localhost:8080/users/42
curl 'http://localhost:8080/users?role=admin'
curl -X POST -d '{"name":"Ann","email":"ann@example.test"}' http://localhost:8080/users  # 201
curl -X POST -d '{"name":"Ann"}' http://localhost:8080/users                            # 400
curl -X PATCH -d '{"role":"editor"}' http://localhost:8080/users/42
```

`examples/user.http` is a small users API: list, filter by role, get, create,
replace, update, delete, and a password reset. `examples/static.http` serves
HTML and text pages, from external files and inline.

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

It sends 16 requests that walk through the users API: listing and filtering,
fetching, creating (`201`, `409` and `400` chosen by body matching), replacing,
updating, a password reset, and a `204` deletion.
It prints status/timing summaries and leaves response bodies for inspection in
the console. Rerun it whenever you need more traffic.

To see request matching in action, serve the files in `examples/matching/`.
Each one shows one area: `headers.http`, `json.http`, `forms.http`,
`paths-and-queries.http` and `webhooks.http` (raw bodies plus combined
matchers). Then run their request script, which prints the status and body
each scenario gets:

```sh
mock examples/matching/*.http
./examples/matching/requests.sh      # -p PORT or MOCK_PORT, like send-requests.sh
```

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

A plain `go build` or `go install` from a git checkout reports a Go
pseudo-version, such as `v0.2.1-0.20261030055820-57bd1986cbf7` (with `+dirty`
appended when the tree has uncommitted changes), and `go run .` reports `dev`.
Only `make build` reports the `git describe` form, such as `v0.2.0-4-g57bd198`.

## Docker

Build the image:

```sh
docker build -t mock .
```

The image exposes `8080` (mock API) and `8081` (admin UI). With no arguments
it serves the bundled `examples/user.http` on `0.0.0.0`:

```sh
docker run --rm -p 8080:8080 -p 8081:8081 mock
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

`examples/user.http` references external body files, so with only that one file
mounted the routes that use them return `500`; mount the directory as shown
below to serve them. The bind mount keeps the request file outside the image. Rebuild the image only
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
mock validate [-strict] [-json] [-var name=value] <file.http> [file.http...]
mock -version
mock -h
```

`mock -h` (or `--help`) prints this usage and the flag list, then exits `0`.

Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-p`, `--port` | `8080`, or `MOCK_PORT` | Default HTTP port, for routes that do not declare a port (see [Multiple ports](#multiple-ports)) |
| `-a`, `--ui-port` | `MOCK_UI_PORT`, else `8081`, or random if `-p` / `MOCK_PORT` is set | Admin UI port. `0` picks a random free port |
| `-x`, `--no-ui` | (off) | Do not start the admin UI. Cannot be combined with `-a` |
| `-b`, `--bind` | `127.0.0.1`, or `MOCK_BIND` | Bind address. Use `0.0.0.0` to listen on all interfaces |
| `-u`, `--upstream` | (off) | Forward requests no route matches to this `http` or `https` URL and return its response (see [Passing unmatched requests to a real API](#passing-unmatched-requests-to-a-real-api)) |
| `-s`, `--strict` | (off) | At shutdown, list requests no route matched and routes that never matched, and exit `1` if any request was unmatched (see [Strict mode and route coverage](#strict-mode-and-route-coverage)) |
| `-v`, `--var` | | `NAME=VALUE`: set the `@NAME` variable in every request file, overriding its definitions. Repeat for more variables (see [Variables from the command line](#variables-from-the-command-line)) |
| `--version` | | Print version and exit |
| `-h`, `--help` | | Print usage and exit |

Every flag has a short and a long name. Flags take one or two dashes
(`-port` and `--port` are the same), and short flags cannot be combined
(write `-x -b 0.0.0.0`, not `-xb`).

If `-p` is omitted, `mock` uses the `MOCK_PORT` environment variable when it
is set. An explicit `-p` flag always wins. Likewise, `MOCK_BIND` sets the
default bind address and an explicit `-b` overrides it.

`mock` binds to `127.0.0.1` by default, so only this machine can reach it. To
reach it from another computer, bind to `0.0.0.0` (or one interface's address)
and allow the ports through any firewall. The admin UI has no authentication
and shows captured headers and bodies, so `mock` prints a warning when it binds
to all interfaces. To make that your default, set `MOCK_BIND=0.0.0.0` in your
shell profile.

The admin UI listens on its own port so it never shadows mock routes. Without
`-a`, it uses the `MOCK_UI_PORT` environment variable when set; otherwise `8081`
when the mock port is the default, and a random free port when you set `-p` (or
`MOCK_PORT`). An explicit `-a` always wins. The startup banner prints the actual
admin address. If the admin port is the same as the mock port, or as a port a
route declares, `mock` exits with an error. Pass `-x` to skip the admin UI and its port entirely. With a directory
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

### Validating request files

`mock validate` loads the files without starting a server and prints every
problem it finds, one per line as `file:line: level: message`. It checks the
same things `mock` checks at startup: syntax errors, invalid `@delay` or `@port`
values, a URL port that conflicts with `@port`, invalid placeholder arguments, unresolved placeholders, unused variables, and request
headers used as response headers.

| Flag | Description |
|------|-------------|
| `-strict` | Treat warnings as errors |
| `-json` | Print a JSON report (`ok`, `routes`, `diagnostics`) instead of text |
| `-var name=value` | Set a variable as `mock -v` does, so the same warnings apply. Repeatable |

It exits `0` when the files are valid, `1` when there are errors (or warnings
with `-strict`), and `2` for usage mistakes, so it fits in CI:

```sh
mock validate -strict path/to/*.http
```

The bundled `examples/small.http` intentionally uses `{{$delay}}`, which is
supplied only by a request query (`?delay=`), so it reports two warnings and
fails under `-strict`.

A file literally named `validate` must be passed as `./validate`.

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
`GET {{host}}/users`. Variables are expanded first. The path and query string
are used for matching, and a port in the URL picks the listener the route is
served on (see [Multiple ports](#multiple-ports)). The scheme and host name are
ignored. This lets one file serve both a REST client and `mock`, as in
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
| `@name = value` file and method variables, `-v name=value` overrides and `{{$env.NAME}}` | Environment files / `{{env}}` from IDE |
| Request line `METHOD /path` | Full multi-step scripts |
| Response headers after the request line | Separate request vs response documents |
| `@header.Name`, `@query.name`, `@path.name`, `@body`, `@json.path`, `@form.name` request matchers (exact, `*`, `!`, `~regex`) | Multipart and XML body matchers |
| `{{name}}`, `{{%name}}` and `{{$placeholder}}` in bodies, response headers and `< path` file paths, with [filters](#filters) and [`{{#repeat}}` blocks](#generated-lists) | Request variables (`{{login.response.body.token}}`) |
| `< path` external body files | Mixing inline text and `< path` in one body; imports of other `.http` files |

Inline bodies normalize line endings to LF and discard leading and trailing
blank lines. With `@trim = off` a section's inline body is kept exactly as
written instead: every line after the blank line that ends the headers,
including its original line endings (LF or CRLF) and any blank lines at the
end, up to the next `###` line. A line beginning `###` is always a section
boundary, including within a body. Use an external body file (`< path`) for
content that must hold literal section delimiters. Individual inline lines are limited
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
when possible.

Placeholders (`{{name}}`, `{{$name}}`) in the file are expanded when the
content type is text, JSON, XML or JavaScript (explicit or inferred) and the
file is valid UTF-8 without binary control bytes. Other files are served as raw
bytes. Set `@template` in the section to choose instead:

- `@template = auto` (the default) decides as above.
- `@template = on` always expands placeholders, whatever the content type.
- `@template = off` never expands them. This also applies to the inline body
  and response headers, so `{{...}}` text is served exactly as written.

### Placeholders in file paths

The path can hold placeholders, so one route serves a different file per
request:

```http
### User by id
GET /users/:id
Content-Type: application/json

< ./users/{{$id}}.json
```

The path is expanded for each request with the same values as a body (see
[Placeholders](#placeholders)), then checked like a fixed path: if the expanded
path is absolute, has a `..` segment, still holds a placeholder, or names no
file, the request gets a `404`. A request for `/users/..%2Fsecret` therefore
cannot read outside the fixture directories. Templated paths are not watched
for reloads, because they name no fixed file; each request reads the file
afresh anyway.

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

- `@delay`: response delay parsed with Go duration syntax, such as `250ms` or `2s`, or a random range such as `100ms..800ms` (both ends inclusive, picked afresh per request). Negative or invalid durations are load errors. Set at file scope, it is the default for every section. See [Delays, streaming and faults](#delays-streaming-and-faults).
- `@stream`: send the body in pieces with this pause between them, such as `50ms`. See [Delays, streaming and faults](#delays-streaming-and-faults).
- `@fault`: break the response on purpose: `reset` or `truncate`, optionally followed by the share of requests, such as `reset 25%`. See [Delays, streaming and faults](#delays-streaming-and-faults).
- `@seed`: make generated values reproducible. See [Reproducible data](#reproducible-data).
- `@template`: `auto`, `on` or `off`, whether placeholders are expanded. See [External response bodies](#external-response-bodies).
- `@trim`: `on` (the default) or `off`. With `off` the inline body keeps its exact text and line endings. See [Supported dialect](#supported-dialect).
- `@port`: serve the section on this port instead of the default port (`-p`). Set at file scope, it applies to every section. See [Multiple ports](#multiple-ports).
- `@header.Name`: require the incoming request header to match the value: exact text, `*` (any non-empty value), `!` (absent), or `~regex`. Set at file scope, it applies to every section; a method-scoped header of the same name replaces it. See [Header matching](#header-matching).
- `@query.name`: require the query parameter to match the value, with the same value forms as `@header.*`. See [Query and path parameter matching](#query-and-path-parameter-matching).
- `@path.name`: require the `:name` path parameter to match the value: exact text or `~regex`. Set at file scope, it applies only to sections whose path has `:name`.
- `@body`: require the request body to match the value. See [Body matching](#body-matching).
- `@json.field.path`: require a field of a JSON request body to match the value; `@json.roles.* = admin` matches any array element. See [Body matching](#body-matching).
- `@form.name`: require a field of a URL-encoded form body to match the value. See [Body matching](#body-matching).

Every other variable is only used through `{{name}}` references. `mock` warns
at load time about variables that nothing references.

### Variables from the command line

`-v NAME=VALUE` (`--var`) sets `@NAME` in every request file. It overrides
file and method definitions of that name, and supplies a value for
`{{NAME}}` references that no `@NAME` defines. Repeat it for more variables:

```sh
mock -v host=http://localhost:9000 -v token=dev-token api.http
```

The value is used as written (no backslash escapes) and may reference other
variables or placeholders. Special variables can be set too, so
`-v delay=0s` removes every file-level and section `@delay`. Request matchers
(`@header.*`, `@query.*`, `@path.*`, `@body`, `@json.*`, `@form.*`) cannot be
set this way. Reloads keep the command-line values.

To read the server's environment instead, use `{{$env.NAME}}` in a body,
response header, `< path` or variable value:

```http
@token = {{$env.API_TOKEN | default dev-token}}

### Profile
GET /me
X-Token: {{token}}

{"token": "{{$env.API_TOKEN}}"}
```

`{{$env.NAME}}` is read when the response is rendered and never comes from the
request. An unset variable leaves the placeholder literal and warns at load
time, unless it has a `| default` filter. Anything a route renders is visible
to whoever can reach the mock port, so only reference variables you are
willing to serve.

## Placeholders

Response bodies **and response headers** can contain placeholders:

- `{{name}}` inserts the value of the `@name` variable in scope.
- `{{%name}}` inserts it percent-encoded (a space becomes `%20`, `&` becomes `%26`).
- `{{$name}}` inserts a request or generated value. `mock` resolves it from:
  - Path parameters, such as `:id` in `/users/:id` or `*path` in `/files/*path`.
  - Named groups in matcher regexes, such as `token` in
    `@header.Authorization = ~Bearer (?P<token>.+)`.
  - Query parameters, such as `type` in `/names?type=cat`.
  - Request body fields: `{{$json.user.name}}` reads a JSON body field (same
    paths as [`@json.*`](#body-matching)) and `{{$form.username}}` reads a
    URL-encoded form field.
  - Built-in generated values.

Earlier sources in that list win when names collide: path parameters beat
regex groups, which beat query parameters. To name the source explicitly, use
a namespace:

- `{{$path.id}}` reads only the `:id` path parameter.
- `{{$query.id}}` reads only the `id` query parameter (its first value).
- `{{$header.X-Request-Id}}` reads a request header (case-insensitive).
- `{{$env.API_TOKEN}}` reads an environment variable of the `mock` process
  (see [Variables from the command line](#variables-from-the-command-line)).

A namespaced value that the request lacks leaves the placeholder literal.
`{{$path.name}}` warns at load time when the route has no `:name` parameter. A missing body field leaves the
placeholder literal. A JSON string field is inserted as its text, and other
JSON values as compact JSON, so `"name":"{{$json.name}}"` copies a string and
`"profile":{{$json.profile}}` copies a whole object.
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

Unknown placeholders are left as written (for example `{{$nope}}`) and
produce a load-time warning. Other REST Client dynamic variables, such as
`{{$processEnv}}`, `{{$dotenv}}` and `{{$aadToken}}`, are not supported and are
treated as unknown placeholders; use `{{$env.NAME}}` in place of
`{{$processEnv NAME}}`.

### Filters

A placeholder can pipe its value through filters, applied left to right:

```text
{{$name | upper}}                 ADA LOVELACE
{{$query.q | trim | urlencode}}   cats%20and%20dogs
{{$id | add 1}}                   43
{{$query.page | default 1}}       1 when the request has no ?page=
{{$header.Authorization | base64}}
{{$json.note | json}}             "a \"quoted\" note", as a JSON string
```

| Filter | Effect |
|--------|--------|
| `upper`, `lower` | Change case. |
| `trim` | Remove leading and trailing whitespace. |
| `urlencode` | Percent-encode (a space becomes `%20`). |
| `base64` | Standard base64 encoding. |
| `json` | Encode as a quoted JSON string; use it outside quotes. |
| `add n` | Add the integer `n` (negative to subtract). A non-integer value is unchanged. |
| `default value` | Use `value` when nothing resolves the placeholder. Later filters still apply. A placeholder with a default never warns at load time. |
| `keep [name]` | Reuse the first generated value, see below. |

Quote a filter argument that holds spaces or `|`: `{{$q | default "no query"}}`.
An unknown filter, or one with the wrong arguments, leaves the placeholder
literal and logs a load-time warning.

Generated values such as `{{$uuid}}` are fresh at each use. Add `| keep` to
reuse one value: every `{{$uuid | keep}}` in the response, its headers and its
`< path` gets the same UUID. `keep` with a name, such as `| keep owner`, keeps
a separate value per name, so a response can hold two stable UUIDs. Inside a
`{{#repeat}}` block each copy keeps its own values, unless the value was
already kept before the block.

```http
### Create order
POST /orders
Location: /orders/{{$uuid | keep}}
Content-Type: application/json

{"id": "{{$uuid | keep}}", "customer": "{{$uuid | keep customer}}"}
```

## Generated lists

`{{#repeat n}} ... {{/repeat}}` renders its contents `n` times, and
`{{#repeat min max}}` a random number of times from `min` to `max`
(inclusive). Counts run from 0 to 1000; blocks can nest. Generated values are
fresh in each copy, and `{{$index}}` is the copy's position, from 0 (the
innermost block's, when nested).

```http
### List users
GET /users
Content-Type: application/json

[
  {{#repeat 20}}
  {"id": {{$index | add 1}}, "name": "{{$name}}", "email": "{{$email}}"}
  {{/repeat}}
]
```

In a JSON response (by `Content-Type` or file extension), copies outside a
JSON string are separated by commas, so the example is a valid array of 20
objects. Other responses join copies with nothing in between. A `{{#repeat}}`
or `{{/repeat}}` tag alone on its line takes the line with it, so it leaves no
blank line in the output. A tag without its partner, or with a bad count, is
left literal and logs a load-time warning. A rendered body over 16 MiB returns
a 500.

## Reproducible data

Generated values are random by default. Set `@seed` to any text to make them
reproducible: the same request (method, URL and body) then gets the same
names, UUIDs, `{{$randomInt}}` numbers and `{{#repeat min max}}` counts every
time, while different requests still differ. Generated timestamps
(`{{$timestamp}}` and `{{$isoTimestamp}}` without arguments) are fixed to
2025-01-01 under a seed; `{{$datetime ...}}` and other clock functions with
arguments still use the current time.

```http
@seed = demo

### User
GET /users/:id
Content-Type: application/json

{"id": "{{$path.id}}", "name": "{{$name}}", "token": "{{$uuid}}"}
```

Changing the seed changes every value. The data may change between `mock`
releases.

## Delays, streaming and faults

`@delay` waits before responding: `@delay = 250ms`, or `@delay = 100ms..800ms`
for a random wait in that range on each request.

`@stream` sends the body in pieces with a pause between them, which is useful
for testing progress indicators and server-sent events:

```http
### Events
@stream = 500ms
GET /events
Content-Type: text/event-stream

data: {"step": 1}

data: {"step": 2}

data: {"step": 3}
```

A `text/event-stream` body is sent one event (up to a blank line) at a time;
any other body one line at a time. Streamed responses have no
`Content-Length`. The pause comes before every piece but the first, after any
`@delay`.

`@fault` breaks the response, to test client error handling:

- `@fault = reset` drops the connection without a response; on TCP the client
  sees a connection reset.
- `@fault = truncate` sends the headers with the full `Content-Length` and
  then only half the body before closing the connection. It does nothing for
  responses without a body.
- A percentage, such as `@fault = reset 10%`, applies the fault to that share
  of requests at random; the rest are answered normally.

Faulted requests appear in the admin UI with the fault noted (`RST` in the
status column for a reset).

## Multiple ports

One set of request files can mock several services, each on its own port.
`mock` reads every file before it starts, then listens on each port that has
routes:

```http
@users = http://localhost:8080
@auth = http://localhost:9090

### List users
GET {{users}}/users

[{"id": 42}]

### Issue a token
POST {{auth}}/token

{"token": "abc123"}

### Metrics
@port = 9100
GET /metrics

requests_total 0
```

```text
$ mock examples/services.http
starting mock HTTP server on 127.0.0.1:8080
starting mock HTTP server on 127.0.0.1:9090
starting mock HTTP server on 127.0.0.1:9100
...
$ curl http://localhost:8080/users
$ curl -X POST http://localhost:9090/token
```

A route's port comes from, in order:

1. The port of a full-URL request target, such as `GET http://localhost:3030/user`
   or `GET {{auth}}/token`.
2. The `@port` variable, set on the section or at file scope.
3. The default port, `-p` (or `MOCK_PORT`), when neither is given. This covers
   path-only targets like `GET /users` and URLs without a port.

A URL port and an `@port` that disagree are a load error. Only the port of a
URL is used; its host name and scheme are ignored. `-b` still chooses the
interface every listener binds to.

Each route only matches requests that arrive on its port, so the same path can
return different responses on different ports. A request sent to the wrong port
gets `404`, and the admin UI's match diagnostics say which port the route is
served on.

The default port is only opened when some route uses it. A route can declare
the default port explicitly; it is served by the same listener. `mock` binds
every port before it serves anything. If a port a route declares is already in
use or is the admin UI port, it exits naming the file and section; a busy
default port reports a plain `server failed` error.

Hot reload can add, change, or remove routes on the ports that are already open,
but cannot open new ones: a reload that needs a new port is rejected, the
previous routes stay active, and the error asks you to restart `mock`. Ports
that no longer have routes keep listening and return `404`.

In Docker, publish every port the request files use, such as
`-p 8080:8080 -p 9090:9090 -p 9100:9100`.

## Matching

Routes match on the listener port, HTTP method, path, any query parameters
declared in the `.http` file, and any `@header.*`, `@query.*`, `@path.*`,
`@body`, `@json.*` and `@form.*` matchers. The first route that matches, in load order,
answers the request; see [Route order](#route-order). For worked examples of
every matcher, see [`examples/matching/`](examples/matching/).

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

A `*` as the last path segment matches the rest of the path, including
nothing: `/api/*` matches `/api`, `/api/users` and `/api/users/7/posts`. Name it
(`*path`) to use the rest as `{{$path}}`, unescaped. A wildcard anywhere but
the end is a load error. Write `%2A` for a literal `*` segment. Because the first
matching route wins, a wildcard route makes a fallback when it comes last:

```http
### Download a file
GET /files/*path

You asked for {{$path}}

### [404] Unknown API path
GET /api/*
Content-Type: application/json

{"error":"not_found"}
```

### Header matching

An `@header.Name = value` matcher requires the incoming request to carry that
header. The header name is case-insensitive, the value must match exactly
(case-sensitive), and `*` accepts any non-empty value. A request that does not
match gets a `404`.

```http
### Secure read
@header.Authorization = Bearer secret
GET /secure
Content-Type: application/json

{"ok":true}
```

### Matcher values

`@header.*`, `@query.*`, `@path.*`, `@body`, `@json.*` and `@form.*` matchers share one value syntax:

| Value | Matches when |
|-------|--------------|
| `text` | a value equals `text` exactly (case-sensitive) |
| `*` | the value is present and not blank |
| `!` | the value is absent or blank (the opposite of `*`) |
| `~regex` | a value matches the Go regular expression `regex` in full |

A regex must match the whole value; it is anchored at both ends, so
`~[0-9]+` rejects `12a`. Write `~.*token.*` to match anywhere in the value. Start the
pattern with `(?i)` for a case-insensitive match: `~(?i)bearer .+`.
When a header or query parameter is repeated, exact and regex matchers accept
any one of the values. An invalid or empty regex is a load error. `\\` in a
variable value is an escaped backslash, so write `\\\\` for a literal backslash in
a pattern; other escapes such as `\d` pass through unchanged. To match a value
that literally is `*` or `!`, or starts with `~`, use a regex such as `~\*`.

`!` lets one route handle requests without a header:

```http
### Signed in
@header.Authorization = ~Bearer [A-Za-z0-9._-]+
GET /me
Content-Type: application/json

{"name":"alice"}

### [401] Not signed in
@header.Authorization = !
GET /me
Content-Type: application/json

{"error":"unauthorized"}
```

A request with `Authorization: Basic abc` matches neither route and gets a
`404`. The admin UI explains why.

### Query and path parameter matching

`@query.name` constrains a query parameter beyond the exact values written in
the request target. `@path.name` constrains a `:name` path parameter or a `*name` wildcard. `*` and `!`
make no sense for path parameters, so they are load errors there, and so is an
`@path.name` in a section without `:name`.

```http
### Numeric user
@path.id = ~[0-9]+
GET /users/:id
Content-Type: application/json

{"id":{{$id}}}

### [400] Bad user id
GET /users/:id
Content-Type: application/json

{"error":"id must be numeric"}

### Search results
@query.q = *
GET /search

results for {{$q}}

### Search form
@query.q = !
GET /search

<form>...</form>
```

The bad-id route catches every id the numeric route above it did not take,
because the first matching route wins.

### Body matching

`@body` matches the whole request body as text, `@json.field.path` matches
one field of a JSON body, and `@form.name` matches a field of a URL-encoded
form body. All three take the [matcher values](#matcher-values) above.

```http
### Admin login
@json.user.name = admin
POST /login
Content-Type: application/json

{"role":"admin"}

### Login
@json.user.name = *
@json.user.password = *
POST /login
Content-Type: application/json

{"role":"user"}

### [400] Missing credentials
POST /login
Content-Type: application/json

{"error":"name and password are required"}

### Ping
@body = ~.*\bping\b.*
POST /echo

pong
```

- **`@body`**: `*` means a non-blank body, `!` an empty or blank one. In a
  `@body` regex, `.` also matches newlines, so `~.*ping.*` finds `ping` on any
  line of the body. Exact text must equal the entire body, byte for byte.
- **`@json.field.path`**: the path is dot-separated, and numeric segments index
  arrays: `@json.items.0.sku` is the `sku` of the first item. A string field is
  compared as its text. Numbers, `true`, `false`, objects and arrays are
  compared as compact JSON text, so `@json.age = 42` and `@json.active = true`
  work, and `@json.age = ~[0-9]+` accepts `42` and `"42"`. A missing field or
  `null` counts as absent for `*` and `!`.
- **`*` in a `@json` path** selects every element of an array, and the matcher
  accepts the request when any selected element matches:
  `@json.roles.* = admin` accepts `{"roles":["dev","admin"]}`, and
  `@json.items.*.sku = ~A[0-9]+` checks the `sku` of every item. Wildcards can
  nest (`@json.items.*.tags.* = sale`). `*` means at least one element is
  present and non-blank; `!` means none is, so it accepts an empty array or a
  missing field. A `*` segment only selects array elements, never object
  fields. Named regex groups capture from the first element that matches.
  Response placeholders don't take `*`; use an index such as `{{$json.roles.0}}`.
- **`@form.name`**: matches a field of an `a=1&b=2` body, decoded, in any
  order. A repeated field matches when any of its values does. Field names must
  be valid variable names, so `user[name]` cannot be matched.
- Bodies are parsed as JSON or form data whatever their `Content-Type`. A blank
  body has no fields, so `@json.name = !` and `@form.name = !` match it. A body
  that does not parse matches no `@json.*` (or `@form.*`) matcher, not even
  `!`.
- Body matchers see the first 1 MiB of a request. A larger body matches no
  `@body`, `@json.*` or `@form.*` matcher. The admin log still captures 64 KiB per body.

Matchers and body placeholders combine into an OAuth-style token endpoint:

```http
### Client credentials
@form.grant_type = client_credentials
@form.client_id = ~(?P<client>[a-z]+)-[0-9]+
POST /oauth/token
Content-Type: application/json

{"access_token":"{{$client}}-{{$uuid}}","scope":"{{$form.scope}}"}

### [400] Unsupported grant
POST /oauth/token
Content-Type: application/json

{"error":"unsupported_grant_type"}
```

## Route order

`mock` tries routes in load order: top to bottom within a file, and files in
the order they are given. The first route that matches answers. Put specific
routes before general ones, and finish with a catch-all if unmatched requests
should get something other than a `404`.

A `HEAD` request tries the `HEAD` routes first. If none matches, it is answered
by the first matching `GET` route, with that route's status and headers
(including the `Content-Length` its body would have) and no body.

```http
### [201] Create user
@json.email = ~[^@\s]+@[^@\s]+
POST /users
Content-Type: application/json

{"id":1}

### [400] Create user without an email
POST /users
Content-Type: application/json

{"error":"a valid email is required"}
```

A route that can never answer gets a load-time warning, and `mock validate
-strict` fails on it. That happens when an earlier route has the same method
and port, its path matches every path the later route's does, and all of its
matchers are also on the later route. Paths are compared segment by segment: a
`:param` segment covers any segment (whatever the parameter is called), and a
trailing `*` covers the rest of the path. So each of these later routes is
reported:

- a second, identical route;
- `GET /api/users/:id` after a catch-all `GET /api/*`;
- `GET /users/me` after `GET /users/:id` (put the literal path first);
- `GET /users/:userId` after `GET /users/:id`.

A `@path.name` matcher on the earlier route narrows what it covers:
`GET /users/:id` with `@path.id = ~[0-9]+` hides a later `/users/42`, but not
`/users/me`. The check errs towards silence, so a route it does not report may
still be unreachable. Earlier versions of `mock` rotated through duplicate
routes; they now always return the first.

## Passing unmatched requests to a real API

Pass `-u` (`--upstream`) to mock only part of an API. Requests a route matches
get the mock response; everything else is forwarded to the real service, and
its response is returned unchanged:

```sh
mock --upstream https://api.example.com fixtures.http
```

- The request path and query are appended to the upstream URL, so with
  `--upstream https://api.example.com/v2` a request for `/users?role=admin`
  goes to `https://api.example.com/v2/users?role=admin`. The upstream URL may
  have a scheme, host, port and base path, but no query, fragment or
  credentials.
- The method, headers and body are forwarded as received, including
  `Authorization` and cookies. `Host` is set to the upstream's host, and
  `X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto` and `Via` are
  added.
- `mock` itself still serves plain HTTP; only the upstream may be HTTPS.
- One upstream serves every mock port. A catch-all route answers before the
  upstream is reached.
- If the upstream can't be reached, or sends no response headers within 30
  seconds, the client gets `502 Bad Gateway`. If the upstream sends a request
  back to the same `mock` (for example `--upstream` points at `mock`'s own
  port), the client gets `508 Loop Detected` instead of looping.
- Hot reload changes routes, not the upstream. Restart `mock` to change it.
- `--upstream` needs request files; it cannot be combined with a directory
  argument.

The admin UI marks forwarded requests as **proxied** and shows the upstream
URL, the real response, and the nearby routes that didn't match.

## Strict mode and route coverage

`mock` counts how many requests each route answered. The admin UI's routes
list shows the count next to each route, and `/routes` reports it as `hits`.
Counts start at zero when `mock` starts and are kept when a reload leaves a
route unchanged; a route is the same one if its file, name, method, port, path
and query are the same, even if its line moved. Clearing the request log does
not reset them.

Pass `-s` (`--strict`) to use `mock` in tests and CI. When `mock` shuts down
on SIGINT or SIGTERM it writes a report to stderr:

```text
strict: 3 requests no route matched
  2x GET /v1/invoices
  1x POST /v1/refunds
strict: 1 of 6 routes never matched a request
  api.http:42  DELETE /v1/users/:id  Delete user
```

- Unmatched requests are listed by method and path, in the order first seen,
  so a call to an endpoint you haven't mocked fails the build instead of hiding
  as a `404` in a log. Up to 100 distinct method and path pairs are listed;
  later ones are still counted.
- If any request was unmatched, `mock` exits `1`. Requests forwarded with
  `--upstream` count as unmatched too, and are marked `(proxied)`.
- Routes that never matched are listed but do not change the exit code. They
  usually mean a stale fixture or a test that no longer runs.
- When every request matched and every route was used, `mock` prints one line
  saying so.
- `--strict` needs request files; it cannot be combined with a directory
  argument.

For example, in a CI script:

```sh
mock --strict -x fixtures.http &
mock_pid=$!
npm test
kill "$mock_pid"     # SIGTERM
wait "$mock_pid"   # fails the job if any request went unmatched
```

## Admin UI And API

The UI is served at the root of the admin port (`-a`, default `8081`):

| Path | Purpose |
|------|---------|
| `/` | Request log UI |
| `/events` | Server-sent events stream (with event `id` / `Last-Event-ID`) |
| `/clear` | `POST` clears stored events |
| `/state` | `GET` active revision, route count, last successful reload, and reload error |
| `/routes` | `GET` route configuration, including source location, port, matchers, status, delay, body preview, and `hits` (requests answered) |
| `/requests` | `GET` the request log as JSON, optionally filtered (see below) |

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
When `mock` listens on more than one port, the request table shows each
request's host and port, and routes are labeled with their port.
The `/clear` POST endpoint requires an `X-Requested-With` header or a JSON
`Content-Type`, and reject cross-origin and cross-site requests.

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
| `since=42` | with an event `id` greater than this |

```sh
curl -s 'http://127.0.0.1:8081/requests?method=POST&path=/users' | jq length
curl -s 'http://127.0.0.1:8081/requests?matched=false' | jq -r '.[].request.url'
```

A test can assert what its code under test sent: `POST /clear` (with
`X-Requested-With`) before the test, then read `/requests` after it. An
invalid `matched` or `since` value returns `400`.

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
  and elapsed time. Unmatched requests show up to five
  nearby routes with method, path, query, header, and body mismatch explanations.
  Requests forwarded with `--upstream` are marked **proxied** in the table (the
  filter matches `proxied`), and the inspector names the upstream URL.
- **Copy as route** appears on unmatched requests and copies a draft `.http`
  section to paste into a request file. The draft has the request's method and
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
[Route order](#route-order)), so health and link checkers work without extra
routes. The admin
UI rejects cross-origin clear requests.

Binding to all interfaces (`-b 0.0.0.0`) prints a warning: the admin UI is
unauthenticated and request logs may include `Authorization` headers and bodies.

Normal HTTP writes have a 30-second budget beginning after any configured
response delay. SSE writes have a renewable 10-second deadline and idle streams
send heartbeats. Shutdown stops accepting connections and allows 400 ms for
active requests before closing remaining connections.

## Development

This repository is intentionally small:

- `main.go` is the thin entry point; `internal/cli/` holds CLI flags, input loading, ports, watching, lifecycle, and `mock validate`.
- `restclient/` parses `.http` files.
- `mockhttp/` matches requests, renders responses, and streams request-log events.
- `internal/ui/` contains the request log UI (`static/`, embedded at build time), its admin handler, and its Node tests.
- `examples/` contains request files you can run locally; `examples/matching/` shows off request matching.
- `docs/potential-features.md` collects ideas for future work, and `CHANGELOG.md` records release history.

Before sending a change around:

```sh
make verify
```

`make fmt` formats Go files explicitly; verification never rewrites them.
`make all` also installs the binary. Dashboard tests require Node.js 22+.

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and pull-request notes. Release
history is in [CHANGELOG.md](CHANGELOG.md).
