[Documentation](README.md) › Command line

# Command line

```text
mock [flags] <file.http> [file.http...]
mock [flags] -d <dir> [-d <dir>...] [file.http...]
mock [flags] <directory>
cat file.http | mock
mock validate [-strict] [-json] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...]
mock init [-force] [dir]
mock import [-host HOST] [-path PREFIX] <file.har|->
mock run [flags] <file.http|-d dir> [file.http...] -- command [args...]
mock match [-H 'Name: value'] [-d body|@file] [-port N] [-response] [-json] [-var name=value] [-env NAME] <file.http|dir> [file.http|dir...] METHOD URL
mock routes [-json] [-port N] [-only TAGS] [-skip TAGS] [-var name=value] [-env NAME] <file.http|dir> [file.http|dir...]
mock check -upstream URL [-all] [-json] [-depth 3] [-check-header Name] [-header 'Name: value'] [-var name=value] [-env NAME [-env-file FILE]] [-timeout 10s] <file.http|dir> [file.http|dir...]
mock -version
mock -h
```

`mock -h` (or `--help`) prints this usage and the flag list, then exits `0`.

Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-p`, `--port` | `8080`, or `MOCK_PORT` | Default HTTP port, for routes that do not declare a port (see [Multiple ports](matching.md#multiple-ports)) |
| `-a`, `--ui-port` | `MOCK_UI_PORT`, else `8081`, or random if `-p` / `MOCK_PORT` is set | Admin UI port. `0` picks a random free port |
| `-x`, `--no-ui` | (off) | Do not start the admin UI. Cannot be combined with `-a` |
| `-t`, `--ui-token` | `MOCK_UI_TOKEN` | Require this token for the admin UI and API (see [Admin access token](admin-ui.md#admin-access-token)). Cannot be combined with `-x` |
| `-R`, `--redact` | | Mask credentials in the request log, the admin UI, copies and exports (see [Credential redaction](admin-ui.md#credential-redaction)) |
| `-b`, `--bind` | `127.0.0.1`, or `MOCK_BIND` | Bind address. Use `0.0.0.0` to listen on all interfaces |
| `-u`, `--upstream` | (off) | Forward requests no route matches to this `http` or `https` URL and return its response (see [Passing unmatched requests to a real API](real-apis.md#passing-unmatched-requests-to-a-real-api)) |
| `--upstream-header` | | With `-u`, `NAME: VALUE`: set this header on every forwarded request, such as credentials. Repeat for more headers (see [Credentials and path prefixes](real-apis.md#credentials-and-path-prefixes)) |
| `--upstream-strip` | | With `-u`, remove this prefix, such as `/api`, from the start of forwarded request paths |
| `-r`, `--record` | (off) | With `-u`, append a draft `.http` section to this file for each method and path the upstream answered (see [Recording forwarded requests](real-apis.md#recording-forwarded-requests)) |
| `--journal` | (off) | Append every request event to this file as a JSON line, so the traffic outlives the request log (see [Request journal](#request-journal)) |
| `--max-body` | `1mb` | Read up to this much of each request body for body matchers, templates and the log, such as `10mb` or `512kb` (`kb` is 1,024 bytes; up to `1gb`). A larger body matches no body matcher |
| `--log-size` | `1000` | Keep the latest this many requests in the request log, `/requests`, `/verify` and the admin UI (up to 1,000,000) |
| `-s`, `--strict` | (off) | At shutdown, list requests no route matched and routes that never matched, and exit `1` if any request was unmatched; `--strict=unused` also exits `1` if a route never matched (see [Strict mode and route coverage](testing.md#strict-mode-and-route-coverage)) |
| `--strict-report` | | Turn on `-s` and also write the report to this file: JUnit XML for `.xml`, JSON for `.json` (see [Strict reports for CI](testing.md#strict-reports-for-ci)) |
| `-H`, `--header` | | `NAME: VALUE`: add this response header to every route that sets no header of that name and no `@default.header` for it. Repeat for more headers (see [Default response headers](request-files.md#default-response-headers)) |
| `-v`, `--var` | | `NAME=VALUE`: set the `@NAME` variable in every request file, overriding its definitions. Repeat for more variables (see [Variables from the command line](request-files.md#variables-from-the-command-line)) |
| `-e`, `--env` | | Use this REST Client environment for `{{NAME}}` references no `@NAME` defines (see [REST Client environments](request-files.md#rest-client-environments)) |
| `-E`, `--env-file` | `http-client.env.json`, else `.vscode/settings.json` | With `-e`, read the environments from this file |
| `-d`, `--dir` | | Load every `.http` file under this directory, after any file arguments, and pick up added or removed files on reload. Repeat for more directories (see [A directory of request files](#a-directory-of-request-files)) |
| `-i`, `--poll` | (off) | Also check the request files for changes this often, such as `1s` (at least `100ms`), for filesystems that do not deliver change events (see [Reloading](#reloading)) |
| `-w`, `--delay` | (off) | Delay every route that has no `@delay` of its own, such as `250ms`, `50ms..300ms` or `"p50 80ms p99 900ms"` (see [Latency and faults for every route](responses.md#latency-and-faults-for-every-route)) |
| `-f`, `--fault` | (off) | Inject a fault into every route that has no `@fault` of its own: `reset`, `truncate`, `hang`, `empty` or `garbage`, optionally with a share such as `"reset 5%"` |
| `-T`, `--throttle` | (off) | Send the body of every route that has no `@throttle` of its own at this rate, such as `32kb/s` |
| `-P`, `--profile` | (off) | A bad network in one word: `slow`, `flaky` or `3g`, a preset `-w`/`-f`/`-T` combination (see [Latency and faults for every route](responses.md#latency-and-faults-for-every-route)) |
| `-l`, `--log-format` | `text` | `text` or `json`. With `json`, stdout carries only JSON lines (see [JSON logs](#json-logs)) |
| `--only` | | Serve only routes with one of these [`@tag`](matching.md#route-tags) names, such as `billing,orders`. Repeat for more |
| `--skip` | | Leave out routes with any of these `@tag` names. Repeat for more |
| `--version` | | Print version and exit |
| `-h`, `--help` | | Print usage and exit |

Most flags have a short and a long name. Flags take one or two dashes
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

## JSON logs

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

## Request journal

`--journal FILE` appends every request event to `FILE` as one JSON line,
the same object [`GET /requests`](admin-ui.md#reading-the-request-log) returns
for it. Requests, [`@callback`](responses.md#webhook-callbacks) sends and
forwarded requests are all written, in event ID order. With
[`-R`](admin-ui.md#credential-redaction) the lines are masked like the
admin UI. The file is created if needed and appended to, so a restart adds to
it; event IDs start over at `1` with each run (the `session` field tells runs
apart). Clearing the log in the admin UI does not touch the file.

```sh
mock --journal requests.jsonl api.http
jq -r 'select(.match.route == null) | .request.method + " " + .request.url' requests.jsonl
```

## Reloading

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

## A directory of request files

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

## Validating request files

`mock validate` loads the files (or every `.http` file under a directory
argument) without starting a server and prints every
problem it finds, one per line as `file:line: level: message`. It checks the
same things `mock` checks at startup: syntax errors, invalid `@delay` or `@port`
values, a URL port that conflicts with `@port`, invalid placeholder arguments, unresolved placeholders, unused variables,
request headers used as response headers, routes that can never match, and
JSON bodies that do not parse.

A section with an error does not stop the check: `mock` skips that section,
carries on at the next `###`, and reports every error in every file (up to
50), so one run shows everything to fix. Warnings are checked once the files
load without errors. With `-json`, each error is its own diagnostic with its
`source` and `line`. Starting `mock`, or a reload, lists the same errors
and serves nothing new until they are fixed.

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
| `-env NAME`, `-env-file FILE` | Use a [REST Client environment](request-files.md#rest-client-environments) as `mock -e` and `-E` do |

It exits `0` when the files are valid, `1` when there are errors (or warnings
with `-strict`), and `2` for usage mistakes, so it fits in CI:

```sh
mock validate -strict path/to/*.http
```

The bundled `examples/small.http` intentionally uses `{{$delay}}`, which is
supplied only by a request query (`?delay=`), so it reports two warnings and
fails under `-strict`.

A file literally named `validate` must be passed as `./validate`.

## Which route answers a request

`mock match` answers "which route gets this request?" without starting a
server, from the same files and with the same matching as `mock` serving them:

```sh
mock match api.http GET '/users?role=admin' -H 'X-Tenant: acme'
```

```text
GET /users?role=admin
  answered by "Admin users" (api.http:12)
  GET /users  status 200
```

When nothing matches, it lists the closest routes with the reasons each one
turned the request away, as the admin UI's near-miss list does, and exits `1`:

```text
GET /users?role=guest
  no route matches

closest routes:
  "Admin users" (api.http:12)  GET /users
    - Query role: expected "admin"; received ["guest"]
```

| Flag | Description |
|------|-------------|
| `-H 'Name: value'` | A request header. Repeat for more. `-H 'Host: api.test'` sets the host that `@match.host` routes check |
| `-d body` | The request body, or `@file` to read it from a file. Without a `Content-Type` header it is sent as JSON when it starts with `{` or `[`, else as a URL-encoded form (as `curl -d`) |
| `-port N` | The mock port the request arrives on, for routes with a port. A full URL's port works too. Without either, routes on every port can match |
| `-response` | Also render the response: status, headers and body, without delays, faults, streaming, throttling, rate limits or callbacks |
| `-json` | Print the result, the route as `/routes` describes it, and the candidates as JSON |
| `-var`, `-env`, `-env-file` | Set variables as `mock -v` and `-e` do |

Flags may come before or after the files, method and URL. Nothing is
counted: a `@times` route is shown as answering, with a note, and so is a
`@match.chance` route, which answers only its share.

## Listing routes

`mock routes` prints the route table of request files without serving them,
in the order routes are tried (the first that matches answers):

```sh
mock routes api.http
```

```text
METHOD  PATH       PORT  STATUS  NAME         SOURCE       TAGS   MATCHES
GET     /users     8080  200     Admin users  api.http:1   admin  header X-Tenant: *; query role: admin
POST    /users     8080  201     Create user  api.http:9          json: {"kind":"person"}; json name: *
GET     /metrics   9100  200     Metrics      api.http:16  ops
GET     /legacy/*  8080  proxy   Forward      api.http:22
```

`PORT` is the route's own port, else the default (`-port`, or `MOCK_PORT`, else
`8080`). `STATUS` is `proxy` for an `@proxy` route and the placeholder for a
templated status. `MATCHES` lists the request matchers and the settings that
make a route skip requests (`@match.chance`, `@times`, `@require`,
`@fallback`). It loads files as `mock validate` does and fails (exit `1`) on
the same errors.

| Flag | Description |
|------|-------------|
| `-json` | Print the routes as JSON, as the admin `GET /routes` describes them |
| `-port N` | The default port shown for routes that set none |
| `-only TAGS`, `-skip TAGS` | List only routes with one of these `@tag` names, or leave them out, as `mock --only` and `--skip` serve them |
| `-var`, `-env`, `-env-file` | Set variables as `mock -v` and `-e` do |

## Other commands

`mock run` is described in [Testing with mock](testing.md#running-a-test-suite-against-mock).
`mock check` and `mock import` are described in
[Working with real APIs](real-apis.md): see
[Checking mocks against the real API](real-apis.md#checking-mocks-against-the-real-api)
and [Importing traffic from a HAR file](real-apis.md#importing-traffic-from-a-har-file).
`mock init` is covered in [Getting started](getting-started.md).
