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
mock check -upstream URL [-all] [-json] [-header 'Name: value'] [-var name=value] [-env NAME [-env-file FILE]] [-timeout 10s] <file.http|dir> [file.http|dir...]
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
| `-r`, `--record` | (off) | With `-u`, append a draft `.http` section to this file for each method and path the upstream answered (see [Recording forwarded requests](real-apis.md#recording-forwarded-requests)) |
| `-s`, `--strict` | (off) | At shutdown, list requests no route matched and routes that never matched, and exit `1` if any request was unmatched (see [Strict mode and route coverage](testing.md#strict-mode-and-route-coverage)) |
| `-v`, `--var` | | `NAME=VALUE`: set the `@NAME` variable in every request file, overriding its definitions. Repeat for more variables (see [Variables from the command line](request-files.md#variables-from-the-command-line)) |
| `-e`, `--env` | | Use this REST Client environment for `{{NAME}}` references no `@NAME` defines (see [REST Client environments](request-files.md#rest-client-environments)) |
| `-E`, `--env-file` | `http-client.env.json`, else `.vscode/settings.json` | With `-e`, read the environments from this file |
| `-d`, `--dir` | | Load every `.http` file under this directory, after any file arguments, and pick up added or removed files on reload. Repeat for more directories (see [A directory of request files](#a-directory-of-request-files)) |
| `-i`, `--poll` | (off) | Also check the request files for changes this often, such as `1s` (at least `100ms`), for filesystems that do not deliver change events (see [Reloading](#reloading)) |
| `-w`, `--delay` | (off) | Delay every route that has no `@delay` of its own, such as `250ms` or `50ms..300ms` (see [Latency and faults for every route](responses.md#latency-and-faults-for-every-route)) |
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

## Other commands

`mock check` and `mock import` are described in
[Working with real APIs](real-apis.md): see
[Checking mocks against the real API](real-apis.md#checking-mocks-against-the-real-api)
and [Importing traffic from a HAR file](real-apis.md#importing-traffic-from-a-har-file).
`mock init` is covered in [Getting started](getting-started.md).
