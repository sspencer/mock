[Documentation](README.md) › Matching, route order and ports

# Matching, route order and ports

## Matching

Routes match on the listener port, HTTP method, path, any query parameters
declared in the `.http` file, and any `@header.*`, `@query.*`, `@path.*`,
`@body`, `@json.*` and `@form.*` matchers. The first route that matches, in load order,
answers the request; see [Route order](#route-order). For worked examples of
every matcher, see [`examples/matching/`](../examples/matching/).

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
