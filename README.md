# mock

[![CI](https://github.com/sspencer/mock/actions/workflows/go.yml/badge.svg)](https://github.com/sspencer/mock/actions/workflows/go.yml)
[![Release](https://img.shields.io/github/v/release/sspencer/mock)](https://github.com/sspencer/mock/releases/latest)
[![License: Unlicense](https://img.shields.io/badge/license-Unlicense-blue.svg)](LICENSE)

`mock` turns REST Client-style `.http` files into a local HTTP server with hot
reload and a request-log UI. It is a single binary for the gap between
hand-written test doubles and a real backend — not a replacement for WireMock,
Prism, or Mockoon, and not the Go unit-test mocking libraries (gomock, mockery).

## Contents

- [Install](#install)
- [Quick Start](#quick-start)
- [Building From Source](#building-from-source)
- [Docker](#docker)
- [CLI](#cli)
- [Request File Format](#request-file-format)
- [External response bodies](#external-response-bodies)
- [Variables](#variables)
- [Placeholders](#placeholders)
- [Generated lists](#generated-lists)
- [Reproducible data](#reproducible-data)
- [Delays, streaming and faults](#delays-streaming-and-faults)
- [Multiple ports](#multiple-ports)
- [Matching](#matching)
- [Route order](#route-order)
- [Passing unmatched requests to a real API](#passing-unmatched-requests-to-a-real-api)
- [Webhook callbacks](#webhook-callbacks)
- [Strict mode and route coverage](#strict-mode-and-route-coverage)
- [Recipes](#recipes)
- [Using mock in Go tests](#using-mock-in-go-tests)
- [Admin UI And API](#admin-ui-and-api)
- [Clients and safety](#clients-and-safety)
- [Development](#development)

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

Start a new mock API from a commented starter file:

```sh
mock init          # writes api.http and users.json here
mock api.http
curl localhost:8080/users/7
```

`api.http` has four routes showing a path parameter, generated values, a
variable, a `< file` body, a `@json` matcher and route order. Its comments
explain each part. `mock init dir` writes into `dir` (creating it), and
`mock init` refuses to overwrite either file unless you pass `-force`.

Or run the example API from a checkout:

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
and reloaded automatically. Docker Desktop does not always pass file change
events through a bind mount; if edits are not picked up, add `-i 1s` to poll
for changes (see [Reloading](#reloading)).

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
mock [flags] -d <dir> [-d <dir>...] [file.http...]
mock [flags] <directory>
cat file.http | mock
mock validate [-strict] [-json] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...]
mock init [-force] [dir]
mock import [-host HOST] [-path PREFIX] <file.har|->
mock check -upstream URL [-all] [-json] [-header 'Name: value'] [-var name=value] [-env NAME [-env-file FILE]] [-timeout 10s] <file.http|dir> [file.http|dir...]
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
| `-t`, `--ui-token` | `MOCK_UI_TOKEN` | Require this token for the admin UI and API (see [Admin access token](#admin-access-token)). Cannot be combined with `-x` |
| `-R`, `--redact` | | Mask credentials in the request log, the admin UI, copies and exports (see [Credential redaction](#credential-redaction)) |
| `-b`, `--bind` | `127.0.0.1`, or `MOCK_BIND` | Bind address. Use `0.0.0.0` to listen on all interfaces |
| `-u`, `--upstream` | (off) | Forward requests no route matches to this `http` or `https` URL and return its response (see [Passing unmatched requests to a real API](#passing-unmatched-requests-to-a-real-api)) |
| `-r`, `--record` | (off) | With `-u`, append a draft `.http` section to this file for each method and path the upstream answered (see [Recording forwarded requests](#recording-forwarded-requests)) |
| `-s`, `--strict` | (off) | At shutdown, list requests no route matched and routes that never matched, and exit `1` if any request was unmatched (see [Strict mode and route coverage](#strict-mode-and-route-coverage)) |
| `-v`, `--var` | | `NAME=VALUE`: set the `@NAME` variable in every request file, overriding its definitions. Repeat for more variables (see [Variables from the command line](#variables-from-the-command-line)) |
| `-e`, `--env` | | Use this REST Client environment for `{{NAME}}` references no `@NAME` defines (see [REST Client environments](#rest-client-environments)) |
| `-E`, `--env-file` | `http-client.env.json`, else `.vscode/settings.json` | With `-e`, read the environments from this file |
| `-d`, `--dir` | | Load every `.http` file under this directory, after any file arguments, and pick up added or removed files on reload. Repeat for more directories (see [A directory of request files](#a-directory-of-request-files)) |
| `-i`, `--poll` | (off) | Also check the request files for changes this often, such as `1s` (at least `100ms`), for filesystems that do not deliver change events (see [Reloading](#reloading)) |
| `-w`, `--delay` | (off) | Delay every route that has no `@delay` of its own, such as `250ms` or `50ms..300ms` (see [Latency and faults for every route](#latency-and-faults-for-every-route)) |
| `-f`, `--fault` | (off) | Inject a fault into every route that has no `@fault` of its own: `reset` or `truncate`, optionally with a share such as `"reset 5%"` |
| `-l`, `--log-format` | `text` | `text` or `json`. With `json`, stdout carries only JSON lines (see [JSON logs](#json-logs)) |
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

### JSON logs

`-l json` (`--log-format json`) makes every line on stdout one JSON object, so
CI jobs and log tools can parse a test run. The startup banner and the route
list become records (`"msg": "route"` with `method`, `path`, `route`, `port`
and `source`), and each request is one record:

```json
{"time":"2026-11-08T14:03:27.13-08:00","level":"INFO","msg":"http request","method":"GET","path":"/users/7","status":200,"route":"Find user","duration":412000}
```

`route` is the matched section's name (empty when nothing matched), and
`duration` is in nanoseconds. Faulted and proxied requests add `fault` and
`upstream`. Warnings, reload errors and the `--strict` report still go to
stderr as text.

### Reloading

File change events do not reach `mock` on every filesystem: Docker Desktop bind
mounts, some network filesystems, and editors that save by renaming through
another directory can miss them. Three other ways to reload:

- `-i 1s` (`--poll`) checks the size and modification time of every request
  and body file (and, with `-d`, the files under each directory) at that
  interval, and reloads when any changed. File events still work alongside
  it.
- `kill -HUP <pid>` reloads at once.
- `POST /reload` on the admin port reloads at once and answers with the same
  JSON as `/state`: `200` when the reload succeeded, `422` when it failed
  (the previous routes stay active and `error` says why). Like `/clear`, it
  needs an `X-Requested-With` header or a JSON `Content-Type`, and rejects
  cross-site requests:

  ```sh
  curl -s -X POST -H 'X-Requested-With: curl' http://127.0.0.1:8081/reload
  ```

A reload from any trigger re-reads every file, and only one runs at a time.
With routes read from stdin there is nothing to reload: `-i` is rejected and
`/reload` answers `409`.

If you pass a single directory, `mock` serves that directory as a static file
server from `/` instead of loading mock routes or the request-log UI.

### A directory of request files

`-d fixtures` (`--dir`) loads every `.http` file under `fixtures`, including
subdirectories, so you can keep one file per resource or team and add files
without changing the command:

```sh
mock -d fixtures
mock -d fixtures -d shared overrides.http
```

- Files load in lexical path order within each directory (`a.http`,
  `b.http`, `users/list.http`), after any file arguments, and directories in
  the order given. Because the first matching route wins, a file argument is
  a place for routes that must beat everything in the directories.
- Only files ending in `.http` (any case) are loaded. Hidden files and
  directories, such as `.git` or an editor's `.swap.http`, are skipped.
- The whole tree is watched. Adding, changing, removing or renaming a `.http`
  file, or adding a subdirectory, reloads every file. If the directories end
  up with no routes, the reload fails and the old routes stay.
- A positional directory still means a static file server; `-d` is what
  loads request files. `mock validate` accepts directories and checks every
  `.http` file under them.

### Validating request files

`mock validate` loads the files (or every `.http` file under a directory
argument) without starting a server and prints every
problem it finds, one per line as `file:line: level: message`. It checks the
same things `mock` checks at startup: syntax errors, invalid `@delay` or `@port`
values, a URL port that conflicts with `@port`, invalid placeholder arguments, unresolved placeholders, unused variables,
request headers used as response headers, routes that can never match, and
JSON bodies that do not parse.

A route whose `Content-Type` is JSON (`application/json` or `+json`, or a
`< file.json` with no `Content-Type`) has its body rendered once without a
request and parsed, so a trailing comma or a broken `{{#repeat}}` warns at
load time instead of failing in the client under test. Placeholders that read
the request (or an unset `$env` variable) render as `1`, which is valid both
bare and inside a string, so `{"id": {{$id}}}` passes; the check cannot tell
whether an unquoted placeholder will be a number at request time. Bodies with
`@template = off` are parsed as written, and a body file whose path has a
placeholder is not checked. The warning gives the line within the rendered
body.

| Flag | Description |
|------|-------------|
| `-strict` | Treat warnings as errors |
| `-json` | Print a JSON report (`ok`, `routes`, `diagnostics`) instead of text |
| `-var name=value` | Set a variable as `mock -v` does, so the same warnings apply. Repeatable |
| `-env NAME`, `-env-file FILE` | Use a [REST Client environment](#rest-client-environments) as `mock -e` and `-E` do |

It exits `0` when the files are valid, `1` when there are errors (or warnings
with `-strict`), and `2` for usage mistakes, so it fits in CI:

```sh
mock validate -strict path/to/*.http
```

The bundled `examples/small.http` intentionally uses `{{$delay}}`, which is
supplied only by a request query (`?delay=`), so it reports two warnings and
fails under `-strict`.

A file literally named `validate` must be passed as `./validate`.

### Checking mocks against the real API

Mocks go stale when the real API changes. `mock check` sends each route's
sample request (the one the admin UI's route details show) to the mock and to
the real service, and reports where they differ:

```sh
mock check -upstream https://staging.example.com/api -header "Authorization: Bearer $TOKEN" api.http
```

```text
drift  GET /users/1 (api.http:12 Find user)
         json: missing from mock: created_at
         json: only in mock: nickname
ok     GET /users (api.http:3 List users)
skip   POST /users (api.http:20 Create user): not a safe method; use -all to send it
checked 3 routes: 1 ok, 1 drifted, 0 errors, 1 skipped
```

It compares:

- the status code;
- the `Content-Type` media type (parameters such as `charset` are ignored);
- when both answers are JSON with the same status, the top-level object
  fields, or the first element's fields when both are arrays.

The sample request fills path parameters with an exact `@path` value or a
sample (`1` for names such as `id` or `user_id`, else `example`), and adds
required `@query`, `@header` and `@cookie` values and a body from `@json`,
`@form` or an exact `@body`. The request path goes under the `-upstream` base
path. The mock answers in-process without `@delay`, `@fault`, `@stream`,
`@rate-limit` or `@callback`.

| Flag | Description |
|------|-------------|
| `-upstream URL` | The real API's base URL (required) |
| `-all` | Also send routes with unsafe methods (`POST`, `PUT`, `PATCH`, `DELETE`). Without it only `GET`, `HEAD` and `OPTIONS` routes are sent |
| `-header 'Name: value'` | Add a header to every upstream request, such as credentials. Repeatable |
| `-var name=value` | Set a variable in every request file, as with `mock -v` |
| `-env NAME`, `-env-file FILE` | Use a [REST Client environment](#rest-client-environments), as with `mock -e` and `-E` |
| `-timeout 10s` | Time limit for each upstream request |
| `-json` | Print the report as JSON: `ok` and one result per route with `result` (`ok`, `drift`, `skipped` or `error`), `differences`, and each side's `status` and `contentType` |

`@proxy` routes are skipped. A route whose sample request cannot match it (a
`~regex` or negated matcher the sample cannot guess) is an error. Redirects are
not followed. The exit code is `0` when nothing drifted, `1` on drift or
errors, and `2` for usage errors.

### Importing traffic from a HAR file

`mock import` turns a HAR file, saved from browser devtools ("Save all as
HAR"), a proxy such as mitmproxy or Charles, or `mock`'s own **Export HAR**,
into `.http` sections on stdout:

```sh
mock import -host api.example.com traffic.har > api.http
```

Each distinct method and path becomes one section, drafted the same way as the
admin UI's **Copy as route**: the section requires the query parameters,
`Authorization` header and top-level JSON or form fields the request had
(`@query.page = *`, without fixing their values) and answers with the captured
status, `Content-Type` and body. When a path was requested more than once, the
first request with a usable response is drafted. A body that is binary, was
left out of the HAR, or can't be pasted inline becomes a placeholder `[200]`
with `{}`; a body containing `{{` gets `@template = off`. Path characters that
mean something in routes (`*`, a leading `:`, `{`, `}`) are escaped.

| Flag | Description |
|------|-------------|
| `-host HOST` | Import only requests to this host (`api.example.com`, or with a port) |
| `-path PREFIX` | Import only request paths starting with this prefix, such as `/api/` |

CORS preflight requests and non-HTTP entries are skipped. Pass `-` to read the
HAR from stdin. A summary of imported and skipped requests goes to stderr. The
exit code is `1` when nothing could be imported.

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

### REST Client files

Files written for the VS Code REST Client or JetBrains HTTP Client load with a
few allowances:

- The name after `###` is optional. An unnamed section (`###` or `### [204]`
  alone) is named after its request line, such as `GET /users`, or after a
  `# @name login` comment above the request line.
- A request before the first `###` starts an unnamed section, so a file of
  requests separated only by `###` loads. Comments directly above it (no blank
  line in between) belong to it.
- `#` and `//` lines are comments, before the first section and above a
  request line.
- An HTTP version after the target, as in `GET /users HTTP/1.1`, is ignored.
- A request line that is only a target, such as `https://example.com/users`
  or `/health`, is a `GET`.

Some REST Client features have no meaning in a mock and get load warnings
instead of being ignored silently:

- request variables such as `{{login.response.body.$.token}}`: a mock answers
  a single request, so there is no earlier response to read. Use
  [`{{$json.*}}`, `{{$header.*}}` or `{{$query.*}}`](#placeholders) for the
  current request's values;
- request settings in comments, such as `# @no-redirect`, `# @prompt` or
  `# @timeout`;
- request headers after the request line (`Authorization`, `Accept`, ...),
  which `mock` reads as response headers.

### REST Client environments

`-e NAME` (`--env`) reads the variables of environment `NAME` from the files
the REST Client extensions use, looked up in the working directory:

- `http-client.env.json`, with `http-client.private.env.json` overriding it
  (JetBrains HTTP Client);
- otherwise `rest-client.environmentVariables` in `.vscode/settings.json`
  (VS Code REST Client; comments and trailing commas are allowed).

`-E FILE` (`--env-file`) reads either format from another file. A file named
`http-client.env.json` still picks up its private sibling.

```json
{
  "$shared": { "version": "v1" },
  "local": { "host": "http://localhost:9000", "base": "{{$shared version}}/api" },
  "staging": { "host": "http://localhost:9100" }
}
```

```sh
mock -e local api.http
```

The values in `$shared` apply to every environment, and an environment's own
values win. `{{$shared name}}` in a value reads `$shared`. An environment
value is used only where no `@name` in the file defines that variable, and
`-v` overrides both. Values that are objects or arrays are skipped, and so
are names `mock` reserves: request matchers and special variables such as
`port` or `delay`. An unknown environment name is an error. The environment
is read at startup; restart `mock` to pick up changes to it.

### Supported dialect

`mock` supports a practical subset of JetBrains REST Client / `.http` files:

| Supported | Not supported |
|-----------|----------------|
| `###` sections, named or [unnamed](#rest-client-files), and a request before the first `###` | Request separators beyond `###` |
| `@name = value` file and method variables, `-v name=value` overrides, [environment files](#rest-client-environments) (`-e`) and `{{$env.NAME}}` | `{{$dotenv}}`, `{{$processEnv}}` and other IDE-only system variables |
| Request line `METHOD /path`, a full URL, an optional `HTTP/1.1` version, or a bare target (`GET`) | Full multi-step scripts |
| Response headers after the request line | Separate request vs response documents |
| `@header.Name`, `@cookie.name`, `@query.name`, `@path.name`, `@body`, `@json.path`, `@xml.path`, `@form.name` request matchers (exact, `*`, `!`, `~regex`, `!text`, `!~regex`) | none |
| `{{name}}`, `{{%name}}` and `{{$placeholder}}` in bodies, response headers and `< path` file paths, with [filters](#filters) and [`{{#repeat}}` blocks](#generated-lists) | Request variables (`{{login.response.body.token}}`; a load warning) |
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

### Range and conditional requests

A file served as raw bytes (no placeholders expanded) to a `GET` or `HEAD`
request with a `200` status supports partial and conditional downloads, as a
static file server would:

- The response has `Accept-Ranges: bytes` and a `Last-Modified` time from the
  file.
- `Range: bytes=0-1023` gets `206 Partial Content` with that slice, and a range
  past the end gets `416 Range Not Satisfiable`. Multiple ranges get a
  `multipart/byteranges` body.
- `If-Range`, `If-Modified-Since` and `If-Unmodified-Since` are honored, so an
  unchanged file answers `304 Not Modified`. `If-None-Match` and `If-Match`
  are honored when the section sets an `ETag` response header.

Templated files, inline bodies, other statuses, other methods, and sections
with `@stream` or `@fault` ignore these headers and always send the whole body.
Use `@template = off` to serve a text file this way.

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

### Directive reference

Some variable names have a special meaning; `mock` consumes them itself. Every
directive can be set at file scope, where it is the default for every section
in the file, or in a section, where it replaces the file value.

| Directive | Values | Default | Effect |
|-----------|--------|---------|--------|
| `@delay` | Go duration such as `250ms` or `2s`, or a range `100ms..800ms` (both ends inclusive, picked afresh per request) | none | Wait before responding. Negative or invalid durations are load errors. See [Delays, streaming and faults](#delays-streaming-and-faults). |
| `@compress` | `gzip`, `deflate`, `auto` or `off` | `off` | Compress the response body; `auto` follows the request's `Accept-Encoding`. See [Compressed responses](#compressed-responses). |
| `@stream` | non-negative duration such as `50ms` | off | Send the body in pieces (one event or line at a time) with this pause between them. See [Delays, streaming and faults](#delays-streaming-and-faults). |
| `@fault` | `reset` or `truncate`, optionally followed by a share such as `reset 25%` | off | Break the response on purpose. See [Delays, streaming and faults](#delays-streaming-and-faults). |
| `@rate-limit` | A count and a window, such as `5/1s`, `100/1m` or `10/s` | none | Answer `429` with `Retry-After` once the route is over its budget. See [Rate limits](#rate-limits). |
| `@seed` | any non-empty text | random | Make generated values reproducible per request. See [Reproducible data](#reproducible-data). |
| `@template` | `auto`, `on` or `off` | `auto` | Whether placeholders are expanded in the body. See [External response bodies](#external-response-bodies). |
| `@trim` | `on` or `off` | `on` | With `off` the inline body keeps its exact text and line endings. See [Supported dialect](#supported-dialect). |
| `@fixture` | JSON file path | none | Data file that `{{$fixture.path}}` placeholders read. See [Fixture data](#fixture-data). |
| `@callback` | `METHOD URL` | none | Send a request after responding, with `@callback.delay`, `@callback.header.Name` and `@callback.body`. See [Webhook callbacks](#webhook-callbacks). |
| `@proxy` | `http` or `https` URL | none | Forward the requests this section matches to the URL instead of answering. See [Per-route proxying](#per-route-proxying). |
| `@port` | port number | `-p` (`8080`) | Serve the section on this port. A port in the request URL takes precedence. See [Multiple ports](#multiple-ports). |
| `@header.Name` | [matcher value](#matcher-values): `text`, `*`, `!`, `~regex`, `!text` or `!~regex` | none | Require the incoming request header to match. A section's header matcher replaces a file one of the same name. See [Header matching](#header-matching). |
| `@cookie.name` | matcher value | none | Require the request cookie to match. See [Cookie matching](#cookie-matching). |
| `@query.name` | matcher value | none | Require the query parameter to match. See [Query and path parameter matching](#query-and-path-parameter-matching). |
| `@path.name` | `text` or `~regex` | none | Require the `:name` path parameter or `*name` wildcard to match. At file scope it applies only to sections whose path has that parameter. |
| `@body` | matcher value | none | Require the whole request body to match. See [Body matching](#body-matching). |
| `@json.field.path` | matcher value | none | Require a field of a JSON request body to match; `@json.roles.* = admin` matches any array element. See [Body matching](#body-matching). |
| `@form.name` | matcher value | none | Require a field of a URL-encoded form body to match. See [Body matching](#body-matching). |
| `@xml.element.path` | matcher value | none | Require an element's text, or an attribute with `["@name"]`, in an XML request body to match. See [Body matching](#body-matching). |

The removed `@status` and `@file` names are load errors that point to their
replacements (`### [204] Name` and `< path`).

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
    paths as [`@json.*`](#body-matching)), `{{$form.username}}` reads a
    URL-encoded form field, and `{{$xml.order.id}}` reads an XML element or
    attribute (same paths as `@xml.*`).
  - Built-in generated values.

Earlier sources in that list win when names collide: path parameters beat
regex groups, which beat query parameters. To name the source explicitly, use
a namespace:

- `{{$path.id}}` reads only the `:id` path parameter.
- `{{$query.id}}` reads only the `id` query parameter (its first value).
- `{{$header.X-Request-Id}}` reads a request header (case-insensitive).
- `{{$cookie.session}}` reads a request cookie (case-sensitive).
- `{{$env.API_TOKEN}}` reads an environment variable of the `mock` process
  (see [Variables from the command line](#variables-from-the-command-line)).
- `{{$fixture.users.0.name}}` reads a value from the route's `@fixture` JSON
  file (see [Fixture data](#fixture-data)).
- `{{$request}}` is the whole request as a JSON object (see
  [Echoing the request](#echoing-the-request)).

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

### Fixture data

`@fixture = path` names a JSON file whose values `{{$fixture.path}}`
placeholders read, so canned data can live in one file and be reused across
routes. The path is relative to the `.http` file, like a `< path` body. Set it
at file scope for every section, or in a section to override it:

```http
@fixture = data/catalog.json

### Product
GET /products/featured
Content-Type: application/json

{"name":"{{$fixture.featured.name}}","price":{{$fixture.featured.price}},"tags":{{$fixture.featured.tags}}}
```

- Paths work like [`@json.*`](#body-matching) paths: dot-separated fields,
  numeric array indexes, and `["key"]` for keys with dots
  (`{{$fixture["site.name"]}}`). A string is inserted as its text, and other
  values as compact JSON, so `{{$fixture.featured.tags}}` copies an array.
- The file is read for each response that uses it, so edits apply to the next
  request. `mock` also watches it and reports problems on reload. The admin UI
  can [edit it](#editing-fixtures).
- A missing file, invalid JSON or a path with no value warns at load time and
  leaves the placeholder literal unless it has a `| default`.
- Path segments are literal, so a fixture can't be indexed by a request value
  such as `{{$path.id}}`.

### Echoing the request

`{{$request}}` inserts the request as a JSON object, for an httpbin-style
route that shows a client exactly what it sent:

```http
### Echo
POST /echo
Content-Type: application/json

{{$request}}
```

A `POST /echo?a=1` with a JSON body answers:

```json
{"body":"{\"sku\":\"A1\"}","headers":{"Content-Type":["application/json"],"User-Agent":["curl/8.7.1"]},"host":"localhost:8080","json":{"sku":"A1"},"method":"POST","path":"/echo","query":{"a":["1"]},"url":"/echo?a=1"}
```

- `method`, `url` (path and query as sent), `path` (decoded), `host` and
  `body` (as text) are strings. `query` and `headers` map each name to an array
  of values, since both can repeat. `json` is the decoded body, present only
  when the body is JSON. A body over 1 MiB has `"body":null` and
  `"bodyUnavailable":true`.
- A path reads one part, using [`@json.*`](#body-matching) paths:
  `{{$request.method}}`, `{{$request.headers.User-Agent.0}}`,
  `{{$request.json.items.0.sku}}`.
- Outside a JSON string the object is inserted as JSON; inside one it is
  escaped, as for any value.


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

### One copy per request item

`{{#each $json.path}} ... {{/each}}` renders its contents once per element of
an array in the JSON request body, so a batch endpoint can answer each item it
was sent. Inside, `{{$item}}` is the element, `{{$item.field}}` reads a field of
it (same paths as [`@json.*`](#body-matching)), and `{{$index}}` is its position
from 0:

```http
### Batch create
POST /orders/batch
Content-Type: application/json

{"orders":[
  {{#each $json.orders}}
  {"id":"ord-{{$index | add 1}}","sku":"{{$item.sku}}","qty":{{$item.qty | default 1}},"status":"accepted"}
  {{/each}}
]}
```

- `{{#each $fixture.path}}` iterates over an array in the route's
  [`@fixture`](#fixture-data) file, and `{{#each $item.path}}` over an array
  inside the current element, for nested lists.
- A path with `*` segments iterates over every value it selects:
  `{{#each $json.orders.*.sku}}`. A value that is not an array renders the
  block once, with that value as `{{$item}}`.
- A missing array, `null`, or a body that isn't JSON renders no copies.
- Commas, standalone tag lines, nesting and the 16 MiB limit work as for
  `{{#repeat}}`; `{{$index}}` and `{{$item}}` belong to the innermost block.

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

### Rate limits

`@rate-limit = 5/1s` lets a route answer five requests in any one-second
window; further requests get `429 Too Many Requests` with a `Retry-After`
header (whole seconds until the oldest request leaves the window) until there
is room again:

```http
### Search
@rate-limit = 10/1m
GET /search

{"results": []}
```

The window slides: each answered request counts for one window after it
arrives, and rejected requests do not count. The count is a request count and
the window a Go duration, or `s`, `m` or `h` for one of that unit. Each section
has its own budget, even when `@rate-limit` is defined at file scope. A reload
that keeps the route keeps its count. Rejected requests appear in the admin UI
as matched by the route, before any `@delay`.

### Latency and faults for every route

`-w` (`--delay`) and `-f` (`--fault`) apply a delay or fault to every route
without editing the files, for a resilience run against the same fixtures your
normal tests use:

```sh
mock --delay 50ms..300ms --fault "reset 5%" api.http
```

They take the same values as `@delay` and `@fault`. A route's own `@delay` or
`@fault` takes precedence, so `@delay = 0` or `@fault = reset 0%` keeps one
route fast and reliable. Unmatched requests (404s and `--upstream`
forwarding) are not affected.

### Compressed responses

`@compress` compresses the response body, to test how a client decodes
`Content-Encoding`:

```http
### Large report
@compress = auto
GET /reports/annual
Content-Type: application/json

< annual-report.json
```

- `@compress = gzip` or `@compress = deflate` always sends that encoding,
  whatever the request's `Accept-Encoding` says, to test a client that must
  cope with it. `deflate` is the zlib format.
- `@compress = auto` sends gzip, or else deflate, when the request's
  `Accept-Encoding` allows it (q-values and `*` are honored), and the body
  as written otherwise. It always adds `Vary: Accept-Encoding`.
- `@compress = off` (the default) sends the body as written.
- An empty body, a status without a body, or a section whose response headers
  already set `Content-Encoding` is sent as written. `@compress` is ignored
  with `@stream`, with a load warning.
- `Content-Length` is the compressed size, and `Content-Type` is worked out
  from the uncompressed body. A compressed file body does not support
  [range requests](#range-and-conditional-requests).
- The request log shows the compressed bytes as sent, so the admin UI labels
  the body binary.

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

### Cookie matching

An `@cookie.name = value` matcher looks at one cookie from the request's
`Cookie` header, so it works whatever order or other cookies the client
sends. Cookie names are case-sensitive. It takes the same
[values](#matcher-values) as the other matchers, and a regex can capture part
of the cookie:

```http
### Dashboard
@cookie.session = ~(?P<user>[a-z]+)-[0-9]+
GET /dashboard
Content-Type: application/json

{"user":"{{$user}}","theme":"{{$cookie.theme}}"}

### [401] Signed out
@cookie.session = !
GET /dashboard
```

Mismatch explanations in the admin UI never repeat cookie values, which often
carry sessions.

### Matcher values

`@header.*`, `@cookie.*`, `@query.*`, `@path.*`, `@body`, `@json.*` and `@form.*` matchers share one value syntax:

| Value | Matches when |
|-------|--------------|
| `text` | a value equals `text` exactly (case-sensitive) |
| `*` | the value is present and not blank |
| `!` | the value is absent or blank (the opposite of `*`) |
| `~regex` | a value matches the Go regular expression `regex` in full |
| `!text` | the value is present and not blank, and no value equals `text` |
| `!~regex` | the value is present and not blank, and no value matches `regex` in full |

A regex must match the whole value; it is anchored at both ends, so
`~[0-9]+` rejects `12a`. Write `~.*token.*` to match anywhere in the value. Start the
pattern with `(?i)` for a case-insensitive match: `~(?i)bearer .+`.
When a header or query parameter is repeated, exact and regex matchers accept
any one of the values. An invalid or empty regex is a load error. `\\` in a
variable value is an escaped backslash, so write `\\\\` for a literal backslash in
a pattern; other escapes such as `\d` pass through unchanged. To match a value
that literally is `*`, or starts with `!` or `~`, use a regex such as `~\*` or
`~!draft`.

`!text` and `!~regex` mean "present, but not this". A missing or blank value
never matches them; add a separate route with `!` for that case. When a header
or query parameter is repeated, every value must avoid the excluded one, and a
`@json` path with `*` must select no excluded element:

```http
### Paying customer
@header.X-Plan = !free
GET /reports

### Real orders only
@json.id = !~test-.*
POST /orders
```

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
one field of a JSON body, `@xml.element.path` matches an element or attribute
of an XML body, and `@form.name` matches a field of a URL-encoded form body.
All of them take the [matcher values](#matcher-values) above.

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
- **Keys with dots** go in `["key"]`: `@json.labels["app.kubernetes.io/name"] = web`
  matches `{"labels":{"app.kubernetes.io/name":"web"}}`, and
  `{{$json.labels["app.kubernetes.io/name"]}}` reads it. A quoted key is always
  an object key, so `["0"]` and `["*"]` name the keys `0` and `*`. Quoted keys
  cannot hold double quotes or whitespace, and the path must start with a
  plain field.
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
- **Multipart uploads**: when the request's `Content-Type` is
  `multipart/form-data`, `@form.name` and `{{$form.name}}` read its parts
  instead. A text part's value is its content; a file part's value is its file
  name, so `@form.avatar = ~(?i).*\.png` matches a PNG upload. File contents
  are not matched.
- **`@xml.element.path`**: the first segment names the root element, and each
  later segment a child element: `@xml.order.customer.id = 42` matches
  `<order><customer><id>42</id></customer></order>`. An element's value is its
  text with surrounding whitespace trimmed, including the text of nested
  elements. A final `["@name"]` segment selects an attribute:
  `@xml.order["@status"] = new`. Names are compared without their namespace
  prefix, so `@xml.Envelope.Body.GetQuote.symbol` reads a SOAP request whatever
  prefix it uses. Repeated elements are all selected, and `*` selects any
  child, so `@xml.order.item = pen` accepts an order with any `pen` item. A
  body that is not well-formed XML with one root element matches no `@xml.*`
  matcher; a blank body matches `!`.
- Other bodies are parsed as JSON or URL-encoded form data whatever their `Content-Type`. A blank
  body has no fields, so `@json.name = !` and `@form.name = !` match it. A body
  that does not parse matches no `@json.*` (or `@form.*`) matcher, not even
  `!`.
- Body matchers see the first 1 MiB of a request. A larger body matches no
  `@body`, `@json.*`, `@xml.*` or `@form.*` matcher. The admin log still captures 64 KiB per body.

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

### Recording forwarded requests

Add `-r` (`--record`) to turn forwarded traffic into a request file. Each time
the upstream answers a method and path that isn't recorded yet, `mock` appends
a draft section for it, the same draft as the admin UI's **Copy as route**:

```sh
mock --upstream https://api.example.com --record recorded.http fixtures.http
```

```http
### [200] GET /users
@query.page = *
@header.Authorization = *
GET /users
Content-Type: application/json

[{"id":1,"name":"Alex"}]
```

- Only requests `--upstream` answered are recorded: not requests a route
  answered (including `@proxy` routes), and not `mock`'s own `502` or `508`
  answers when the upstream fails. Upstream errors such as `404` are recorded
  like any other answer.
- Each method and path is recorded once, from the first answer; query strings
  don't make separate sections. Sections already in the file count as
  recorded, so restarting with the same file only adds new routes. The file
  must load as a request file if it already exists.
- The section requires the query parameters, `Authorization` header and
  top-level JSON or form fields the request had, without fixing their values,
  and answers with the upstream's status, `Content-Type` and body. A gzip or
  deflate response is decoded. A body that is binary, longer than the 64 KiB
  capture limit, or can't be pasted inline becomes a placeholder `[200]` with
  `{}`; a body containing `{{` gets `@template = off`. `@port` is set when the
  request arrived on a port other than `-p`.
- Review the file before using it: recorded bodies may contain real data, and
  matchers are deliberately loose. To replay while recording, also load the
  recorded file once it exists (`mock -u URL -r recorded.http recorded.http`):
  hot reload picks up each new section, so the next matching request is
  answered by the recording instead of the upstream.

### Per-route proxying

`@proxy = URL` on a section forwards the requests that section matches to a
real service, so matchers decide which requests go there. Routes without
`@proxy` still answer with mock responses, and `--upstream` still handles
requests no route matches:

```http
@sandbox = https://sandbox.payments.example.com

### Sandbox charges
@header.X-Test-Mode = live
@proxy = {{sandbox}}/v1
POST /payments/*rest

### Mock charges
POST /payments/*rest
Content-Type: application/json

{"id":"ch_{{$uuid}}","status":"succeeded"}

### Old API
@proxy = {{sandbox}}/v2/accounts/{{$path.id}}
GET /legacy/users/:id
```

- A plain URL works like `--upstream`: the request path and query are appended
  to it, so a request for `/payments/charge?x=1` goes to
  `https://sandbox.payments.example.com/v1/payments/charge?x=1`.
- A URL with placeholders rewrites the path. It is rendered with the request's
  values and used as the whole target path; the request's query is appended
  unless the URL has its own. Above, `/legacy/users/7?page=2` goes to
  `.../v2/accounts/7?page=2`. Placeholders are inserted unencoded, so a `*rest`
  wildcard keeps its slashes; use `{{%name}}` to percent-encode a value. The
  scheme and host must be literal (file `{{variables}}` are fine), and a
  placeholder the request doesn't provide answers `502 Bad Gateway`.
- The method, headers and body are forwarded, with the same `Host`,
  `X-Forwarded-*` and `Via` headers, timeouts, `502` and `508` responses as
  `--upstream`. `@delay` waits before forwarding. The section's response body,
  `< file`, response headers, `@stream` and `@fault` are ignored, with a load
  warning.
- A proxied request counts as matched for [strict mode](#strict-mode-and-route-coverage)
  and route hit counts. The admin UI marks it **proxied** and names the URL it
  was sent to.

## Webhook callbacks

Some services answer at once and report the outcome later by calling your
webhook: payment providers, async job runners. `@callback` makes a route do
the same. After the route responds, `mock` sends a request of its own:

```http
### [202] Create payment
@callback = POST {{$json.callbackUrl}}
@callback.delay = 2s
@callback.header.X-Signature = test-signature
@callback.body = {"id": "{{$json.id}}", "status": "succeeded"}
POST /payments
Content-Type: application/json

{"id": "{{$json.id}}", "status": "pending"}
```

| Directive | Value | Default |
|-----------|-------|---------|
| `@callback` | `METHOD URL`, such as `POST https://localhost:3000/hooks` or `POST {{$json.callbackUrl}}` | none |
| `@callback.delay` | non-negative duration, such as `500ms` | `0s` (right after the response) |
| `@callback.header.Name` | header value | none |
| `@callback.body` | body template on one line | empty |

- The URL, header values and body are templates rendered against the request
  that triggered the callback, with every [placeholder](#placeholders) the
  response body can use, including `{{variables}}`. A callback body is
  JSON-escaped like a JSON response body when its `Content-Type` is JSON.
- The URL must be an absolute `http` or `https` URL without credentials. A
  literal URL is checked when the file loads; a URL from a placeholder is
  checked when the callback is sent.
- Without `@callback.header.Content-Type`, a body that starts with `{` or `[`
  is sent as `application/json`, anything else as `text/plain`. Callbacks
  carry `User-Agent: mock-callback` unless you set one, and a `Via` header.
- One request is sent per response, with no retries. It times out after 10
  seconds and does not follow redirects. A route whose response failed to
  render sends no callback; one with `@fault` still does.
- Callbacks are logged in the admin UI, marked **callback** (the filter
  matches `callback`), with the request sent and the response received, or
  `ERR` and the reason when none came back: a timeout, a refused connection,
  an invalid URL or a placeholder with no value. The inspector names the
  route and the request that triggered it.
- A request carrying this `mock`'s own `Via` header sends no callback, so a
  callback aimed back at its own route doesn't loop; the log records why.
- Callbacks still waiting for their delay are cancelled when `mock` shuts
  down. A route with `@proxy` ignores `@callback`, with a load warning.
- Callbacks are not requests the mock received, so `/requests` leaves them
  out unless you ask for `callbacks=true`, and they don't count for
  [strict mode](#strict-mode-and-route-coverage) or route hits.

`mock` sends callbacks to whatever URL the request names, so only point
clients you trust at a `mock` that has `@callback` routes.

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

## Recipes

These combine features documented above for needs that come up often.

### GraphQL operations

GraphQL clients send every query to one URL, so match on the operation name in
the JSON body. Routes are tried in order, so a fallback without a matcher goes
last:

```http
### Get user
@json.operationName = GetUser
POST /graphql
Content-Type: application/json

{"data": {"user": {"id": "{{$json.variables.id}}", "name": "{{$name}}"}}}

### List orders
@json.operationName = ListOrders
POST /graphql
Content-Type: application/json

{"data": {"orders": []}}

### [400] Unknown operation
POST /graphql
Content-Type: application/json

{"errors": [{"message": "unknown operation"}]}
```

### Content negotiation

Match the `Accept` header with a regex. Regexes are anchored, so wrap the media
type in `.*`, and start with `(?i)` because clients differ in case:

```http
### Report as CSV
@header.Accept = ~(?i).*text/csv.*
GET /report
Content-Type: text/csv

id,total
1,9.99

### Report as JSON
GET /report
Content-Type: application/json

[{"id": 1, "total": 9.99}]
```

### Case-insensitive values

`(?i)` works in every `~regex` matcher, including query parameters and body
fields. `@query.sort = ~(?i)asc|desc` accepts `sort=ASC` and `sort=desc` but
not `sort=up`.

### Different responses per test

To give each test its own answer from one mock, have the test send a header
that names the scenario, and put the default route last:

```http
### Empty cart
@header.X-Test = empty-cart
GET /cart
Content-Type: application/json

{"items": []}

### [503] Cart service down
@header.X-Test = cart-down
GET /cart
Content-Type: application/json

{"error": "unavailable"}

### Cart
GET /cart
Content-Type: application/json

{"items": [{"sku": "A1", "qty": 2}]}
```

`curl -H 'X-Test: cart-down' localhost:8080/cart` gets the `503`; a request
without the header gets the full cart. In Go tests, [`mocktest`](#using-mock-in-go-tests)
starts a fresh server per test instead.

### Waiting until mock is ready

`mock` binds every port and loads every route before it answers on the admin
port, so a successful `GET /state` means it is ready. In a CI script:

```sh
mock -i 1s api.http &
until curl -fs -o /dev/null http://127.0.0.1:8081/state; do sleep 0.2; done
```

The Docker image is based on Alpine, which has `wget` but not `curl`. In
Docker Compose:

```yaml
services:
  api:
    image: mock
    command: ["-b", "0.0.0.0", "-i", "1s", "/mock/api.http"]
    volumes:
      - ./mocks:/mock:ro
    ports: ["8080:8080", "8081:8081"]
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8081/state"]
      interval: 2s
      retries: 10
  app:
    depends_on:
      api:
        condition: service_healthy
```

## Using mock in Go tests

The `mocktest` package runs a mock server inside a Go test, from the same
`.http` files, and asserts on what the code under test sent:

```go
import "github.com/sspencer/mock/mocktest"

func TestSignup(t *testing.T) {
	api := mocktest.Start(t, "testdata/api.http")
	client := NewClient(api.URL)

	if err := client.Signup("avery"); err != nil {
		t.Fatal(err)
	}

	api.AssertCalled(t, "POST /users", 1)
	api.AssertNotCalled(t, "DELETE /users/1")
	api.AssertAllMatched(t)
}
```

- `Start(t, files...)` loads request files and `StartSource(t, text)` loads
  `.http` text; `StartWith(t, mocktest.Options{...})` takes both plus `Vars`
  (as `-v` sets them). A file that fails to load fails the test. Load
  warnings are written with `t.Log`; set `Quiet` to drop them.
- The server listens on a random local port, given by `URL`, and closes when
  the test ends. Every route answers on that port; ports in request targets
  and `@port` are ignored.
- A call is a method and path, `"POST /users"`, which matches that exact URL
  path whatever the query (add `?page=2` to require that exact query too), or
  a route name, `"Create user"`, which counts the requests that route
  answered.
- `AssertCalled(t, call, n)`, `AssertNotCalled(t, call)` and
  `AssertAllMatched(t)` report failures with `t.Errorf` and list every request
  received. `Calls(call)` and `Requests(filter)` return the request events to
  check bodies and headers yourself, `Clear()` forgets requests received so
  far, and `Mock` is the underlying `mockhttp.Server`.

From other languages, read the same data from [`/requests`](#reading-the-request-log)
on the admin port.

## Admin UI And API

The UI is served at the root of the admin port (`-a`, default `8081`):

| Path | Purpose |
|------|---------|
| `/` | Request log UI |
| `/events` | Server-sent events stream (with event `id` / `Last-Event-ID`) |
| `/clear` | `POST` clears stored events |
| `/reload` | `POST` reloads the request files now and returns the new state (see [Reloading](#reloading)) |
| `/state` | `GET` active revision, route count, last successful reload, and reload error |
| `/routes` | `GET` route configuration, including source location, port, matchers, status, delay, body preview, and `hits` (requests answered) |
| `/requests` | `GET` the request log as JSON, optionally filtered (see below) |
| `/resend` | `POST` answers a request as if a client sent it to the mock port (see [Resending a request](#resending-a-request)) |
| `/fixture` | `GET ?route=ID` reads a route's `@fixture` file; `PUT` writes it (see [Editing fixtures](#editing-fixtures)) |

### Admin access token

The admin UI and API are open to anyone who can reach the admin port.
`-t TOKEN` (`--ui-token`, or the `MOCK_UI_TOKEN` environment variable)
requires a token for every admin page and endpoint:

```sh
MOCK_UI_TOKEN=$(openssl rand -hex 16) mock -b 0.0.0.0 api.http
```

- In a browser, open the UI once as `http://HOST:8081/?token=TOKEN`. `mock`
  sets an HTTP-only, same-site cookie and redirects to the plain URL, so the
  token leaves the address bar.
- API clients send `Authorization: Bearer TOKEN` (or add `?token=TOKEN`):

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
[`-r`](#recording-forwarded-requests) recordings always use the real request, so recorded routes
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
| `callbacks=true` | the outbound [webhook callbacks](#webhook-callbacks) instead, with `callback.trigger` (the event `id` of the request that sent each) and `callback.error` |
| `since=42` | with an event `id` greater than this |

```sh
curl -s 'http://127.0.0.1:8081/requests?method=POST&path=/users' | jq length
curl -s 'http://127.0.0.1:8081/requests?matched=false' | jq -r '.[].request.url'
```

A test can assert what its code under test sent: `POST /clear` (with
`X-Requested-With`) before the test, then read `/requests` after it. An
invalid `matched`, `callbacks` or `since` value returns `400`.

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
| `host` | `Host` header; its port picks the mock port. Without a port, routes on every port can match |
| `headers` | Request headers, `{"Name": ["value"]}`. `Host`, `Content-Length`, `Transfer-Encoding` and `Connection` are ignored |
| `body` | Request body text, or base64 with `"encoding": "base64"` |
| `from` | The event `id` this request was copied from |

Invalid input returns `400`. With [`-R`](#credential-redaction), logged
credentials are `[redacted]`, so a resent copy carries that text unless you
edit it.

### Editing fixtures

A route with [`@fixture`](#fixture-data) shows **Edit fixture** in the route
inspector. The editor has two views:

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
  names the URL they were sent to. [Webhook callbacks](#webhook-callbacks) are
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
[Route order](#route-order)), so health and link checkers work without extra
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
