[Documentation](README.md) › Delays, faults and callbacks

# Delays, faults and callbacks

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
### ETags

`@etag = auto` adds an `ETag` response header, a hash of the body as sent, and
answers a `GET` or `HEAD` whose `If-None-Match` lists it with `304 Not
Modified` and no body. Clients that cache can then be tested against inline
and templated bodies too:

```http
@etag = auto

### Settings
GET /settings
Content-Type: application/json

{"theme": "dark", "user": "{{$header.X-User}}"}
```

- Only `200` responses get an ETag, and a section's own `ETag` header wins.
- The tag changes whenever the rendered body does, so a templated body gets a
  new tag when the request changes what it renders. A compressed body has its
  own tag per encoding.
- A `< path` file served as raw bytes already supports
  [conditional requests](request-files.md#range-and-conditional-requests);
  with `@etag = auto` it also answers `If-None-Match`.

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
  [range requests](request-files.md#range-and-conditional-requests).
- The request log shows the compressed bytes as sent, so the admin UI labels
  the body binary.

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
| `@callback.retries` | retries for each failed callback, `0` to `10` | `0` |
| `@callback.N` | `METHOD URL` for callback number `N` (1 to 99), with `@callback.N.delay`, `.header.Name`, `.body` and `.retries` | none |

- The URL, header values and body are templates rendered against the request
  that triggered the callback, with every [placeholder](placeholders.md#placeholders) the
  response body can use, including `{{variables}}`. A callback body is
  JSON-escaped like a JSON response body when its `Content-Type` is JSON.
- The URL must be an absolute `http` or `https` URL without credentials. A
  literal URL is checked when the file loads; a URL from a placeholder is
  checked when the callback is sent.
- Without `@callback.header.Content-Type`, a body that starts with `{` or `[`
  is sent as `application/json`, anything else as `text/plain`. Callbacks
  carry `User-Agent: mock-callback` unless you set one, and a `Via` header.
- Each callback is one request per response. It times out after 10 seconds
  and does not follow redirects. A route whose response failed to render sends
  no callback; one with `@fault` still does.
- `@callback.retries = 3` sends a failed callback again, up to three more
  times, after 1s, 2s, 4s and so on (at most 30s). A callback fails when no
  response comes back, or the response is `429` or `5xx`. Each attempt is
  logged; retries are marked **retry**.
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
  [strict mode](testing.md#strict-mode-and-route-coverage) or route hits.

### Callback sequences

Webhook providers often send several events for one request, such as
`payment.pending` and then `payment.succeeded`. Number further callbacks
`@callback.1`, `@callback.2` and so on. `@callback` (if any) goes first, then
the numbered ones in order, each waiting for its own delay after the previous
one finished, including its retries:

```http
### [202] Create payment
@callback.retries = 2
@callback = POST {{$json.callbackUrl}}
@callback.body = {"type": "payment.pending", "id": "{{$json.id}}"}
@callback.1 = POST {{$json.callbackUrl}}
@callback.1.delay = 2s
@callback.1.body = {"type": "payment.succeeded", "id": "{{$json.id}}"}
POST /payments

{"id": "{{$json.id}}", "status": "pending"}
```

`@callback.retries` applies to every callback of the section;
`@callback.N.retries` overrides it for one. A failed callback doesn't stop the
ones after it.

`mock` sends callbacks to whatever URL the request names, so only point
clients you trust at a `mock` that has `@callback` routes.
