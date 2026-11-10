[Documentation](README.md) › Placeholders and generated data

# Placeholders and generated data

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
    paths as [`@json.*`](matching.md#body-matching)), `{{$form.username}}` reads a
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
  (see [Variables from the command line](request-files.md#variables-from-the-command-line)).
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

- Paths work like [`@json.*`](matching.md#body-matching) paths: dot-separated fields,
  numeric array indexes, and `["key"]` for keys with dots
  (`{{$fixture["site.name"]}}`). A string is inserted as its text, and other
  values as compact JSON, so `{{$fixture.featured.tags}}` copies an array.
- The file is read for each response that uses it, so edits apply to the next
  request. `mock` also watches it and reports problems on reload. The admin UI
  can [edit it](admin-ui.md#editing-fixtures).
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
- A path reads one part, using [`@json.*`](matching.md#body-matching) paths:
  `{{$request.method}}`, `{{$request.headers.User-Agent.0}}`,
  `{{$request.json.items.0.sku}}`.
- Outside a JSON string the object is inserted as JSON; inside one it is
  escaped, as for any value.

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

### One copy per request item

`{{#each $json.path}} ... {{/each}}` renders its contents once per element of
an array in the JSON request body, so a batch endpoint can answer each item it
was sent. Inside, `{{$item}}` is the element, `{{$item.field}}` reads a field of
it (same paths as [`@json.*`](matching.md#body-matching)), and `{{$index}}` is its position
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
