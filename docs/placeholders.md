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
- `{{$query.tag[]}}` and `{{$header.X-Id[]}}` are every value of a repeated
  query parameter or header as a JSON array, such as `["red","blue"]` (`[]`
  when there is none); `tag[]=` parameters count as values of `tag`.
- `{{$header.X-Request-Id}}` reads a request header (case-insensitive).
- `{{$cookie.session}}` reads a request cookie (case-sensitive).
- `{{$env.API_TOKEN}}` reads an environment variable of the `mock` process
  (see [Variables from the command line](request-files.md#variables-from-the-command-line)).
- `{{$fixture.users.0.name}}` reads a value from the route's `@fixture` data
  file, and `{{$fixture.users[id=$path.id].name}}` looks a record up by a
  field (see [Fixture data](#fixture-data)).
- `{{$request}}` is the whole request as a JSON object (see
  [Echoing the request](#echoing-the-request)).
- `{{$hits}}` is how many requests the route has answered, counting this
  one, so a polling or retry route can say `{"attempt": {{$hits}}}`. It goes
  back to zero on a reset (`POST /reset` or `Reset()` in `mocktest`) and is
  kept by a reload that keeps the route; the hit counts in `/routes` and the
  `--strict` report are not reset.

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
{{$paragraph}}     {{$article}}     {{$company}}
{{$city}}          {{$country}}       {{$address}}
{{$ip}}            {{$word}}          {{$price}}
{{$ulid}}
```

`{{$price}}` is an amount from `1.00` to `999.99` with two decimals, `{{$ip}}`
an IPv4 address, and `{{$ulid}}` a [ULID](https://github.com/ulid/spec): 26
characters that sort by creation time.

Generated values are random faker data and are recalculated each time a
response body is rendered.

These REST Client–style generators take arguments:

```text
{{$randomInt min max}}
{{$timestamp [offset option]}}
{{$datetime rfc1123|iso8601 [offset option]}}
{{$localDatetime rfc1123|iso8601 [offset option]}}
{{$oneOf value value ...}}
{{$randomFloat min max [decimals]}}
{{$randomDate from to}}
{{$jwt claim=value ...}}
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
- `$oneOf` picks one of its arguments, for enum fields:
  `"status": "{{$oneOf pending paid "on hold"}}"`.
- `$randomFloat` returns a number from `min` to `max` with `decimals` places
  (default 2).
- `$randomDate` picks a date from `from` to `to`, inclusive. With two dates
  (`2024-01-31`) it returns a date; with RFC 3339 times
  (`2024-01-31T09:00:00Z`) it returns a UTC time to the second.
- `$jwt` builds a JSON Web Token; see [Signed tokens](#signed-tokens).
- Invalid arguments leave the placeholder literal and log a load-time warning.
- A path or query parameter named like a function (for example `?timestamp=`)
  wins over the bare placeholder, but not over a call with arguments.

Unknown placeholders are left as written (for example `{{$nope}}`) and
produce a load-time warning. Other REST Client dynamic variables, such as
`{{$processEnv}}`, `{{$dotenv}}` and `{{$aadToken}}`, are not supported and are
treated as unknown placeholders; use `{{$env.NAME}}` in place of
`{{$processEnv NAME}}`.

### Fixture data

`@fixture = path` names a data file whose values `{{$fixture.path}}`
placeholders read, so canned data can live in one file and be reused across
routes, and edited without touching the `.http` file. The path is relative to
the `.http` file, like a `< path` body. Set it at file scope for every
section, or in a section to override it:

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
  `{{$fixture}}` alone is the whole file.
- The format follows the extension. `.csv` is read as an array of objects, one
  per row, keyed by the header row, with string values; a row may have fewer
  fields than the header but not more. `.ndjson` and `.jsonl` are read as an
  array of the values on each line (blank lines are skipped). Any other
  extension is JSON.
- The file is read for each response that uses it, so edits apply to the next
  request. `mock` also watches it and reports problems on reload. The admin UI
  can [edit](admin-ui.md#editing-fixtures) JSON fixtures; CSV and NDJSON files
  are edited in an editor.
- A missing file, invalid data or a path with no value warns at load time and
  leaves the placeholder literal unless it has a `| default`.

#### Looking up a record

A `[field=value]` selector keeps the array elements whose `field` equals
`value`. The value can be a `$reference` to the request, so one route serves
every record in the file:

```http
@fixture = data/users.json

### User
@require = $fixture.users[id=$path.id]
GET /users/:id
Content-Type: application/json

{"id": {{$path.id}}, "name": "{{$fixture.users[id=$path.id].name}}"}

### [404] No such user
GET /users/:id

{"error": "no user {{$path.id}}"}
```

- Values compare as text, so `[id=42]` matches `"id": 42` and `"id": "42"`.
  The value can't hold spaces, quotes, `]`, `{` or `}`; a reference whose value
  does, or has no value, selects nothing.
- `@require = $reference` makes the route match only when the reference has a
  value for this request. Without one, the next route answers, here the
  `404`. The near-miss list in the admin UI says which `@require` failed.
  `{{...}}` around the reference is optional.
- Selectors work in every path: `{{$json.items[sku=A1].qty}}` reads the
  request body, and `@json.items[sku=A1].qty = 2` matches on it.

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
  when the body is JSON. A body over 1 MiB (`--max-body`) has `"body":null` and
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
{{$json.total | mul 1.2 | round 2}}  59.88
{{$timestamp | date date}}        2026-11-11
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
| `base64decode` | Decode standard or URL-safe base64, with or without padding. Invalid input is unchanged. |
| `length` | The number of elements of a JSON array, members of a JSON object, or characters of anything else: `{{$fixture.users \| length}}`. |
| `slice a [b]` | Characters from position `a` up to (not including) `b`, or to the end. Negative positions count from the end: `{{$name \| slice 0 1}}` is the initial, `slice -4` the last four. |
| `replace a b` | Replace every `a` with `b`. |
| `sha256` | Hex SHA-256 digest. |
| `hmac key` | Hex HMAC-SHA256 with `key`, to sign a webhook body the way a real service does. |
| `add n`, `sub n`, `mul n` | Arithmetic. Whole numbers stay whole; otherwise the result is a decimal. A value that isn't a number is unchanged. |
| `int` | Drop the fractional part: `3.9` becomes `3`. |
| `round [n]` | Round to `n` decimal places (default 0), always writing `n` places: `{{$price \| round 1}}`. |
| `date layout` | Reformat a timestamp. It reads Unix seconds or milliseconds, RFC 3339, RFC 1123 or `2006-01-02`, and writes `unix`, `unixms`, `iso8601` (RFC 3339), `rfc1123`, `date`, `time`, or a [Go layout](https://pkg.go.dev/time#pkg-constants) such as `"Jan 2, 2006"`. An unreadable value is unchanged. |
| `default value` | Use `value` when nothing resolves the placeholder. Later filters still apply. A placeholder with a default never warns at load time. |
| `keep [name]` | Reuse the first generated value, see below. |

Quote a filter argument that holds spaces or `|`: `{{$q | default "no query"}}`.
An argument that is a `$reference` is read from the request, such as
`{{$json.amount | mul $query.rate}}` or `{{$header.X-Id | default $query.id}}`;
one with no value counts as empty text. An unknown filter, or one with the
wrong arguments, leaves the placeholder literal and logs a load-time warning.

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
- `| offset n` skips the first `n` elements and `| limit n` stops after `n`,
  for paginated lists from one fixture. `n` is a whole number or a
  `$reference`; a value that isn't a whole number is ignored, so a request
  without `?limit=` gets everything:

  ```http
  ### Users page
  @fixture = data/users.json
  GET /users
  Content-Type: application/json

  {"total": {{$fixture.users | length}}, "users": [
    {{#each $fixture.users | offset $query.offset | limit $query.limit}}
    {"id": {{$item.id}}, "name": "{{$item.name}}"}
    {{/each}}
  ]}
  ```
- Commas, standalone tag lines, nesting and the 16 MiB limit work as for
  `{{#repeat}}`; `{{$index}}` and `{{$item}}` belong to the innermost block.

## Conditional blocks

`{{#if condition}} ... {{/if}}` renders its contents only when the condition
holds, and `{{else}}` adds what to render otherwise. One route can then cover
an optional field or a variant instead of two near-identical sections:

```http
### User
GET /users/:id
Content-Type: application/json

{
  "id": "{{$path.id}}",
  {{#if $query.expand}}
  "profile": {"bio": "{{$sentence}}"},
  {{/if}}
  {{#if $header.X-Plan == pro}}
  "limits": {"requests": 10000}
  {{else}}
  "limits": {"requests": 100}
  {{/if}}
}
```

- `{{#if $ref}}` holds when the reference has a value other than empty,
  `false`, `0` or `null`, so `?expand=false` reads as off.
- `{{#if $ref == value}}` and `{{#if $ref != value}}` compare text. The value
  can be quoted (`"site admin"`) or another `$reference`. A reference with no
  value equals nothing.
- The reference is anything a placeholder reads, such as `$query.name`,
  `$json.user.role`, `$fixture.flags.beta`, `$item.active` inside an
  `{{#each}}`, or a plain `@variable` name. Conditions stop there: there is no
  `and`, `or` or arithmetic, so templates stay templates.
- Blocks nest with each other and with `{{#repeat}}` and `{{#each}}`. Tags
  alone on their line take the line with them, as for `{{#repeat}}`. A tag
  without its partner, a second `{{else}}`, or a malformed condition is left
  literal and logs a load-time warning.

## Signed tokens

`{{$jwt claim=value ...}}` builds a JSON Web Token, so a mocked login endpoint
returns a token that client libraries can decode and check for expiry:

```http
@jwt.secret = {{$env.JWT_SECRET | default dev-secret}}

### Login
POST /login
Content-Type: application/json

{"token": "{{$jwt sub=$json.username role=admin exp=1h}}"}
```

- Each argument is a claim. A value is literal text (quote it when it holds
  spaces) or a `$reference`; a claim whose reference has no value is left out.
- `iat` is the time of the response unless you set it. `exp`, `nbf` and `iat`
  take a duration from now, such as `exp=1h` or `nbf=-5m`, or Unix seconds.
- `sub`, `iss`, `aud` and `jti` are always strings. Other values that read as
  JSON numbers, booleans, objects or arrays keep that type
  (`admin=true`, `uid=$json.id`); the rest are strings.
- With `@jwt.secret` the token is signed with HS256. The secret is a template,
  so it can come from the environment as above. Without it the token is
  unsigned (`"alg": "none"`) and ends with a dot.
- Under `@seed`, `iat` and relative times count from 2025-01-01, so tokens are
  reproducible.

## Reproducible data

Generated values are random by default. Set `@seed` to any text to make them
reproducible: the same request (method, URL and body) then gets the same
names, UUIDs, `{{$randomInt}}` numbers and `{{#repeat min max}}` counts every
time, while different requests still differ. Generated timestamps
(`{{$timestamp}}`, `{{$isoTimestamp}}` and the time in `{{$ulid}}`) are fixed to
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
