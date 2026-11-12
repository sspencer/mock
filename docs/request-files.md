[Documentation](README.md) › Request files

# Request files

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
served on (see [Multiple ports](matching.md#multiple-ports)). The scheme and host name are
ignored. This lets one file serve both a REST client and `mock`, as in
[`examples/restclient.http`](../examples/restclient.http). Headers and bodies are
still read as the mock's response, so a REST client's request headers there
trigger a warning.

The method may be a comma-separated list with no spaces, such as
`GET,HEAD,OPTIONS /health`, or `*` for any method, as in `* /maintenance`. One
section then answers each of those methods instead of a copy of the section
per method. Methods are case-insensitive and shown upper-cased.

```http
### Health
GET,HEAD,OPTIONS /health

ok

### [503] Down for maintenance
* /admin/*

{"error":"maintenance"}
```

Start a section name with a status code in square brackets to set the response
status: `### [204] Delete user` returns `204` and is named "Delete user". The
bracketed code is optional and defaults to `200`. The supported final status
range is `200`–`999`; invalid codes are load errors. Brackets that do not hold
only digits, such as `### [beta] Users`, stay part of the name. The old
`$status` variable is no longer supported and is also a load error.

The status can also come from the request, so one route answers with whatever
status a client asks for: `### [{{$query.status | default 200}}] Echo status`.
`@status = {{$json.code}}` does the same as a directive. The placeholder is
rendered for each response; a result that isn't a status from `200` to `999`
answers `500` and logs why. `@status` with a plain number, such as
`@status = 202`, sets a fixed status. A status in the section name wins over a
file-level `@status`, and a section that sets both is a load error.

### Default response headers

`@default.header.Name = value` gives every section in the file a response
header it does not set itself. A header written after a section's request line
always wins, and a section can set its own `@default.header.Name` to replace the
file's. Header names compare case-insensitively.

```http
@default.header.Content-Type = application/json
@default.header.X-Request-Id = {{$uuid}}

### Users
GET /users

[{"id":1}]

### Home page
GET /
Content-Type: text/html

<h1>Home</h1>
```

The value may hold placeholders, rendered per response like any response
header. `@proxy` sections answer with the upstream's headers and get no
defaults.

A default `Content-Type` is a last resort. For a section with no `Content-Type`
of its own, `mock` picks the response type in this order:

1. The type of the `< path` file's extension, such as `text/html` for
   `< index.html` or `application/json` for `< users/{{$path.id}}.json`.
2. A specific type found by sniffing the response body with Go's
   [`http.DetectContentType`](https://pkg.go.dev/net/http#DetectContentType),
   such as `image/png` for PNG bytes or `text/html` for a body starting with
   `<!DOCTYPE html>`.
3. The default `Content-Type`.
4. What sniffing found otherwise: `text/plain; charset=utf-8` for text
   (sniffing cannot recognize JSON) or `application/octet-stream`.

Without a default, steps 1, 2 and 4 are how `mock` has always typed responses.
Before the body is rendered, the default also counts as the section's type
when there is no extension type: it decides whether a file with an unknown
extension has its placeholders expanded, and whether an inline body is rendered
in JSON mode (values inside JSON strings escaped).

[`-H 'Name: value'`](cli.md) (`--header`, repeatable) does the same for every
request file, below both a section's own headers and `@default.header`.

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
  [`{{$json.*}}`, `{{$header.*}}` or `{{$query.*}}`](placeholders.md#placeholders) for the
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
| Request line `METHOD /path`, a full URL, an optional `HTTP/1.1` version, or a bare target (`GET`); `mock` also accepts a method list (`GET,HEAD`) or `*` | Full multi-step scripts |
| Response headers after the request line | Separate request vs response documents |
| `@header.Name`, `@cookie.name`, `@query.name`, `@path.name`, `@body`, `@json.path`, `@xml.path`, `@form.name` request matchers (exact, `*`, `!`, `~regex`, `!text`, `!~regex`) | none |
| `{{name}}`, `{{%name}}` and `{{$placeholder}}` in bodies, response headers and `< path` file paths, with [filters](placeholders.md#filters) and [`{{#repeat}}` blocks](placeholders.md#generated-lists) | Request variables (`{{login.response.body.token}}`; a load warning) |
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
[Placeholders](placeholders.md#placeholders)), then checked like a fixed path: if the expanded
path is absolute, has a `..` segment, still holds a placeholder, or names no
file, the request gets a `404`. A request for `/users/..%2Fsecret` therefore
cannot read outside the fixture directories. Templated paths are not watched
for reloads, because they name no fixed file; each request reads the file
afresh anyway.

`@missing` sets what a missing file does, for templated and fixed paths alike:

- `@missing = next` treats the route as not matching, so a later route
  answers. With "one JSON file per record", the next route can be a `404`
  with a body of your own:

  ```http
  ### Order
  @missing = next
  GET /orders/:id

  < orders/{{$path.id}}.json

  ### [404] No such order
  GET /orders/:id
  Content-Type: application/json

  {"error": "no order {{$path.id}}"}
  ```

- `@missing = 410` (or any status from `200` to `999`) answers with that
  status instead of the default `404` (templated path) or `500` (fixed path).

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
| `@delay` | Go duration such as `250ms` or `2s`, a range `100ms..800ms` (both ends inclusive, picked afresh per request), or a long tail `p50 80ms p99 900ms` or `lognormal 90ms 0.4` | none | Wait before responding. Negative or invalid durations are load errors. See [Delays, streaming and faults](responses.md#delays-streaming-and-faults). |
| `@compress` | `gzip`, `deflate`, `auto` or `off` | `off` | Compress the response body; `auto` follows the request's `Accept-Encoding`. See [Compressed responses](responses.md#compressed-responses). |
| `@stream` | non-negative duration such as `50ms` | off | Send the body in pieces (one event or line at a time) with this pause between them. See [Delays, streaming and faults](responses.md#delays-streaming-and-faults). |
| `@fault` | `reset`, `truncate`, `hang`, `empty` or `garbage`, optionally followed by a share such as `reset 25%` | off |
| `@throttle` | a rate such as `32kb/s`, `1mb/s` or `512b/s` | off | Send the body at that rate. See [Bandwidth throttling](responses.md#bandwidth-throttling). | Break the response on purpose. See [Delays, streaming and faults](responses.md#delays-streaming-and-faults). |
| `@rate-limit` | A count and a window, such as `5/1s`, `100/1m` or `10/s` | none | Answer `429` with `Retry-After` once the route is over its budget, and send `X-RateLimit-*` headers. `@rate-limit.key = {{$header.Authorization}}` gives each client its own budget. See [Rate limits](responses.md#rate-limits). |
| `@seed` | any non-empty text | random | Make generated values reproducible per request. See [Reproducible data](placeholders.md#reproducible-data). |
| `@template` | `auto`, `on` or `off` | `auto` | Whether placeholders are expanded in the body. See [External response bodies](#external-response-bodies). |
| `@trim` | `on` or `off` | `on` | With `off` the inline body keeps its exact text and line endings. See [Supported dialect](#supported-dialect). |
| `@fixture` | JSON, CSV or NDJSON file path | none | Data file that `{{$fixture.path}}` placeholders read. See [Fixture data](placeholders.md#fixture-data). |
| `@callback` | `METHOD URL` | none | Send a request after responding, with `@callback.delay`, `@callback.header.Name` and `@callback.body`. `@callback.1`, `@callback.2` ... send more, in order, and `@callback.retries` retries failures. See [Webhook callbacks](responses.md#webhook-callbacks). |
| `@proxy` | `http` or `https` URL | none | Forward the requests this section matches to the URL instead of answering. See [Per-route proxying](real-apis.md#per-route-proxying). |
| `@fallback` | `true` or `false` | `false` | Try the section after every section without it, whatever the load order. See [Fallback routes](matching.md#fallback-routes). |
| `@match.chance` | a percentage above `0%`, such as `20%` | `100%` | Let the section answer only that share of the requests it matches; the rest go on to later routes. Reproducible with `@seed`. See [Occasional responses](matching.md#occasional-responses). |
| `@default.header.Name` | header value, which may hold placeholders | none | Add the response header to every section in scope that does not set it. See [Default response headers](#default-response-headers). |
| `@status` | status from `200` to `999`, or a placeholder such as `{{$query.status \| default 200}}` | `200` | The response status, when the section name has none. See [Request file format](#request-file-format). |
| `@etag` | `auto` or `off` | `off` | Add a hash `ETag` and answer a matching `If-None-Match` with `304`. See [ETags](responses.md#etags). |
| `@missing` | `next`, or a status such as `404` | `404` for a templated `< path`, else `500` | What a missing `< path` file does: `next` lets later routes answer. See [Placeholders in file paths](#placeholders-in-file-paths). |
| `@times` | a whole number from `1` | none | Answer that many requests, then let later routes answer until a reset. See [Routes that answer a few times](matching.md#routes-that-answer-a-few-times). |
| `@require` | a `$reference`, such as `$fixture.users[id=$path.id]` | none | Match only when the reference has a value for the request. See [Looking up a record](placeholders.md#looking-up-a-record). |
| `@jwt.secret` | text, which may hold placeholders | none (unsigned) | The HS256 secret for `{{$jwt}}` tokens. See [Signed tokens](placeholders.md#signed-tokens). |
| `@port` | port number | `-p` (`8080`) | Serve the section on this port. A port in the request URL takes precedence. See [Multiple ports](matching.md#multiple-ports). |
| `@header.Name` | [matcher value](matching.md#matcher-values): `text`, `*`, `!`, `~regex`, `!text` or `!~regex` | none | Require the incoming request header to match. A section's header matcher replaces a file one of the same name. See [Header matching](matching.md#header-matching). |
| `@cookie.name` | matcher value | none | Require the request cookie to match. See [Cookie matching](matching.md#cookie-matching). |
| `@query.name` | matcher value | none | Require the query parameter to match. See [Query and path parameter matching](matching.md#query-and-path-parameter-matching). |
| `@path.name` | `text` or `~regex` | none | Require the `:name` path parameter or `*name` wildcard to match. At file scope it applies only to sections whose path has that parameter. |
| `@body` | matcher value | none | Require the whole request body to match. See [Body matching](matching.md#body-matching). |
| `@json.field.path` | matcher value | none | Require a field of a JSON request body to match; `@json.roles.* = admin` matches any array element. See [Body matching](matching.md#body-matching). |
| `@form.name` | matcher value | none | Require a field of a URL-encoded form body to match. See [Body matching](matching.md#body-matching). |
| `@xml.element.path` | matcher value | none | Require an element's text, or an attribute with `["@name"]`, in an XML request body to match. See [Body matching](matching.md#body-matching). |

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
