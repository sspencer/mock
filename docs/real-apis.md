[Documentation](README.md) › Working with real APIs

# Working with real APIs

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
- A proxied request counts as matched for [strict mode](testing.md#strict-mode-and-route-coverage)
  and route hit counts. The admin UI marks it **proxied** and names the URL it
  was sent to.

## Checking mocks against the real API

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
         json address: missing from mock: postcode
         json id: mock has a string, upstream a number
ok     GET /users (api.http:3 List users)
skip   POST /users (api.http:20 Create user): not a safe method; use -all to send it
checked 3 routes: 1 ok, 1 drifted, 0 errors, 1 skipped
```

It compares:

- the status code;
- the `Content-Type` media type (parameters such as `charset` are ignored);
- any response headers named with `-check-header`, by value;
- when both answers are JSON with the same status, the shape of the bodies:
  object keys missing on either side, and values of different types (string,
  number, boolean, object or array), with a path such as `user.address` or
  `items[0].sku` in each message. Arrays compare their first elements. A
  `null` on either side matches any type, since APIs often send `null` for an
  optional field. Keys are compared down to `-depth` levels of nested objects
  (3 by default; `-depth 1` compares only the top-level keys); deeper values
  are still compared by type.

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
| `-env NAME`, `-env-file FILE` | Use a [REST Client environment](request-files.md#rest-client-environments), as with `mock -e` and `-E` |
| `-timeout 10s` | Time limit for each upstream request |
| `-depth 3` | How many levels of nested objects to compare keys in. Lower it to cut noise from deep payloads |
| `-check-header Name` | Also compare this response header's value, such as `Cache-Control` or `Location`. Repeatable |
| `-json` | Print the report as JSON: `ok` and one result per route with `result` (`ok`, `drift`, `skipped` or `error`), `differences`, and each side's `status` and `contentType` |

`@proxy` routes are skipped. A route whose sample request cannot match it (a
`~regex` or negated matcher the sample cannot guess) is an error. Redirects are
not followed. The exit code is `0` when nothing drifted, `1` on drift or
errors, and `2` for usage errors.

## Importing traffic from a HAR file

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
