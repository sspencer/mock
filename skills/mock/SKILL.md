---
name: mock
description: Build and run local HTTP mock servers with the mock CLI, from REST Client-style .http files. Use when a task needs a fake or stub HTTP API (for tests, CI, front-end work, or a service that is not available), when editing .http files that mock serves, or when debugging why a mock returns 404 or the wrong response.
---

# mock: HTTP mocks from .http files

`mock` serves routes written in `.http` files on a local port and hot-reloads them
on save. Each route is a `###` section: the request it answers, then the
response it sends. A separate admin port (default `8081`) has a request log UI
and a JSON API for tests.

Check the installed version with `mock --version` and the flags with `mock -h`.
The full documentation is at https://sspencer.github.io/mock/.

## Workflow

1. Write or edit the `.http` file (dialect below).
2. Lint it: `mock validate -strict -json api.http`. Fix every diagnostic; each
   has a `source` and `line`. Exit `0` means clean.
3. Check routing without a server: `mock match api.http POST /users -d '{"email":"a@b.c"}' -response`.
   When nothing matches it lists the closest routes and why each refused the
   request. `mock routes api.http` prints the route table in match order.
4. Run against code:
   - Test suite, one shot: `mock run api.http -- npm test`. Serves on free
     ports, sets `MOCK_URL` and `MOCK_ADMIN_URL` for the command, exits with
     its status, or `1` if any request matched no route (the report is on stderr).
   - Long-running in the background:
     `mock serve -p 0 -a 0 --ready-file mock.json api.http &` then
     `URL=$(mock wait -file mock.json)`; `jq -r .adminUrl mock.json` is the admin URL.
     Stop it with `kill` when done. Don't use `sleep` to wait for startup.
   - Interactive, for a human: `mock serve api.http` (port `8080`, UI at
     `http://127.0.0.1:8081/`).

Don't start a server just to see whether a route matches or what it renders:
`mock match ... -response` answers that from the file.

## File format

```http
@host = example.test

### List users
GET /users
Content-Type: application/json

[{"id": 1, "name": "Ada"}]

### Get user
GET /users/:id
Content-Type: application/json

{"id": "{{$path.id}}", "name": "{{$name}}", "site": "{{host}}"}

### [201] Create user
@json.email = *
POST /users
Content-Type: application/json
Location: /users/{{$uuid | keep}}

{"id": "{{$uuid | keep}}", "email": "{{$json.email}}"}

### [400] Create user without an email
POST /users
Content-Type: application/json

{"error": "email is required"}
```

- `### [status] Name` starts a route. The status defaults to `200`.
- Between the `###` line and the request line: `@name = value` variables and
  `@directives`, plus `#` comments. Above the first `###`: file-wide variables
  and directives.
- The request line is `METHOD /path`. `:id` matches one segment and a final
  `*` (or `*rest`) the rest of the path. `GET,HEAD /x` lists methods and
  `* /x` takes any method. `?type=cat` in the target must be in the request.
- **Headers after the request line are RESPONSE headers.** To match a request
  header, write `@header.Authorization = ...` above the request line.
- A blank line, then the response body. `< users.json` as the only body line
  sends that file (relative to the `.http` file; no `..`, no absolute path).
- Routes are tried top to bottom and the first match answers. Put specific
  routes before general ones and catch-alls last.
- A line starting with `###` always starts a new route, even inside a body.

## Matching requests

```http
### Admin
@header.Authorization = ~Bearer (?P<token>.+)
@query.limit = <=100
@json.user.role = admin
POST /admin
Content-Type: application/json

{"token": "{{$token}}"}
```

Matchers: `@header.Name`, `@query.name`, `@path.name`, `@cookie.name`,
`@json.a.b.0.c`, `@form.name`, `@xml.root.child`, `@body`, `@match.host`, and
`@json = {"type":"card"}` (the body contains this document).

| Value | Matches |
|-------|---------|
| `text` | exactly `text` (case-sensitive) |
| `*` / `!` | present and non-blank / absent or blank |
| `~regex` | the whole value matches (anchored; `(?i)` for any case; `(?P<n>...)` captures `{{$n}}`) |
| `!text`, `!~regex` | present, but not this |
| `>100`, `<=50` | a number that compares so |
| `#>0`, `#3` | array, object or string length |

`@json.items.*.sku` checks every element. `@query.tag[] = ~(a|b)` requires
every repeated value to match.

## Responses

- `{{name}}`: the `@name` variable. `{{$path.id}}`, `{{$query.q}}`,
  `{{$header.X-Id}}`, `{{$cookie.s}}`, `{{$json.a.b}}`, `{{$form.f}}`: request values.
  `{{$request}}` is the whole request as JSON. `{{$env.NAME}}` reads the
  server's environment.
- Generated: `{{$uuid}}`, `{{$name}}`, `{{$email}}`, `{{$randomInt 1 100}}`,
  `{{$oneOf a b c}}`, `{{$isoTimestamp}}`, `{{$datetime iso8601 -1 d}}`,
  `{{$price}}`, `{{$jwt sub=42}}`, `{{$hits}}` (answers so far).
- Filters: `{{$query.page | default 1}}`, `| upper`, `| add 1`, `| json`,
  `| keep` (reuse one generated value across the response).
- Blocks: `{{#repeat 10}}...{{/repeat}}`, `{{#each $json.items}}{{$item.sku}}{{/each}}`,
  `{{#if $query.expand}}...{{else}}...{{/if}}`. In JSON responses, repeated
  copies get commas between them.
- In a JSON body, write string placeholders inside quotes and numbers bare:
  `{"id": {{$path.id}}, "name": "{{$name}}"}`. An unresolved placeholder is
  left as written and warns at load time.
- `@fixture = data/users.json` with `{{$fixture.users[id=$path.id].name}}` and
  `@require = $fixture.users[id=$path.id]` serves records from one data file;
  without the record the route steps aside so a later `[404]` route answers.

## Directives

| Directive | Effect |
|-----------|--------|
| `@delay = 200ms` or `100ms..800ms` | wait before answering |
| `@times = 2` | answer only twice, then let later routes answer (retry tests) |
| `@scenario = order`, `@state = paid`, `@next-state = paid` | state machine across requests; starts in `started` |
| `@fault = reset` (`truncate`, `hang`, `empty`, `garbage`, optional `25%`) | break the response |
| `@rate-limit = 5/1s` | `429` with `Retry-After` over budget |
| `@status = {{$query.code}}` | status from the request |
| `@port = 9100` | serve this route on another port |
| `@fallback = true` | try this route after all others |
| `@match.chance = 10%` | answer only that share of requests |
| `@callback = POST http://localhost:3000/hook` | send a webhook after answering |
| `@proxy = https://api.example.com` | forward to a real server |
| `@tag = billing` | select with `--only billing` / `--skip billing` |
| `@seed = 1` | reproducible generated data |

## Admin API (tests in any language)

Admin POSTs need `Content-Type: application/json` or an `X-Requested-With`
header. `$ADMIN` is `MOCK_ADMIN_URL` under `mock run`, else `http://127.0.0.1:8081`.

```sh
# assert calls: 200 when all hold, 422 with the reasons otherwise
curl -s --fail-with-body -H 'Accept: text/plain' -H 'Content-Type: application/json' "$ADMIN/verify" \
  -d '[{"call": "POST /users", "times": 1}, {"call": "Get user", "atLeast": 1}, {"allMatched": true}]'
# what was received (filters: method, path, route, matched=false, since=ID)
curl -s "$ADMIN/requests?matched=false"
# one test needs a different answer: a temporary route in front of the file's
curl -s -H 'X-Requested-With: x' "$ADMIN/routes" --data-binary $'### [503] Down\nGET /users\n\n{"error":"down"}\n'
# between tests: reset @times, scenarios, temporary routes and run-time faults
curl -s -X POST -H 'X-Requested-With: x' "$ADMIN/reset"
```

Also: `GET /routes` (routes with hit counts), `POST /clear` (empty the log),
`PUT /scenarios` (`{"name":"order","state":"paid"}`), `POST /faults`.

Go tests can skip the server process: `mocktest.Start(t, "testdata/api.http")`
from `github.com/sspencer/mock/mocktest`, then `api.URL`,
`api.AssertCalled(t, "POST /users", 1)`, `api.AssertAllMatched(t)` and
`api.Stub(t, httpText)`.

## Debugging

- **404**: `mock match api.http METHOD URL [-H ...] [-d ...]` names the
  closest routes and each reason. A running server's admin UI and
  `GET /requests?matched=false` show the same near misses.
- **Wrong route answers**: an earlier route matched first. Move the specific
  route up, or add a matcher to the general one. `mock validate` warns about
  routes an earlier one always beats.
- **Placeholder came out literally**: the name is misspelled, or the request
  lacked the value. Add `| default x`, or use an explicit namespace such as `$query.x`.
- **A header you meant to match is in the response**: move it above the
  request line as `@header.Name = value`.
- **Reload failed**: the server keeps the old routes. Run `mock validate` for the error.
- **Address in use**: pass `-p 0 -a 0` with `--ready-file`, or use `mock run`.

## Other commands

- `mock init [dir]`: write a commented starter `api.http` and `users.json`.
- `mock fmt api.http`: canonical layout; never changes routes.
- `mock import -host api.example.com traffic.har` or `mock import -curl cmds.sh`: draft routes from real traffic.
- `mock serve -u https://api.example.com -r recorded.http api.http`: forward unmatched requests to a real API and record the answers as routes.
- `mock check -upstream https://staging.example.com api.http`: report where the mock's responses differ from the real API's.
- `mock send api.http`: fire a sample request for each route against a running mock server (defaults to `MOCK_URL`, else `http://127.0.0.1:8080`). Only safe methods unless `-all`.
- `mock client api.http`: generate a REST Client `.http` file of sample requests to stdout.
- `mock skill -force DIR`: rewrite this skill from the installed `mock`, after an upgrade.

Plain HTTP only: mock has no TLS and sends no CORS headers.
