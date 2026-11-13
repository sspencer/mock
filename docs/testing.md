[Documentation](README.md) › Testing with mock

# Testing with mock

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
  usually mean a stale fixture or a test that no longer runs. With
  `--strict=unused`, they fail the run too.
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

[`mock run`](#running-a-test-suite-against-mock) does the same in one line:
`mock run fixtures.http -- npm test`.

### Strict reports for CI

`--strict-report FILE` also writes the report to a file that CI systems read,
and turns on `--strict`. The file name picks the format:

```sh
mock run --strict-report mock-junit.xml api.http -- npm test
mock run --strict=unused --strict-report strict.json api.http -- pytest
```

- `.xml` writes JUnit XML. The `mock requests` suite has a failed test case
  for each unmatched method and path (one passing case when every request
  matched), and the `mock routes` suite a case for each route, with its file
  and line. A route that never matched is skipped, or failed with
  `--strict=unused`.
- `.json` writes `passed`, `failUnused`, `unmatchedTotal`, `unmatched` (method,
  path, count and `proxied`), `routes` (each with its `hits`) and `unused`.
- The file is written at shutdown, before the report on stderr, and replaced
  if it exists.

## Running a test suite against mock

`mock run` does the whole CI step in one command: it starts `mock` on free
ports, runs your test command with the mock's address in the environment,
stops, prints the strict report, and exits with the command's status:

```sh
mock run api.http -- npm test
mock run -d mocks -- pytest -x
```

- The command gets `MOCK_URL` (the default mock port, such as
  `http://127.0.0.1:53124`) and `MOCK_ADMIN_URL` (the admin UI, unless `-x`).
  Point your client at `MOCK_URL` instead of a fixed port.
- Strict mode is always on. If the command succeeds but some request matched
  no route, `mock run` exits `1`; if the command fails, it exits with the
  command's status. The report goes to stderr either way.
- Everything before `--` is `mock`'s own flags and request files, so
  `-v`, `-e`, `-H`, `-P flaky` and `-d` work as usual; `-p` and `-a` choose
  fixed ports instead of free ones. Routes with their own `@port` still bind
  that port.
- The startup banner and per-request logs are left out, so the command's
  output reads cleanly; load warnings still show. Ctrl-C stops the command
  (with an interrupt, then a kill after five seconds) and `mock`.
- From the command, [`POST /routes`](admin-ui.md#temporary-routes) and
  `POST /reset` on `MOCK_ADMIN_URL` stub and reset routes between tests, and
  [`POST /verify`](admin-ui.md#verifying-calls) asserts the calls a test
  made.

## GitHub Actions

This repository is also a GitHub Action. It installs a released `mock` binary
on Linux, macOS or Windows runners (checking its SHA-256 against the
release's `checksums.txt`), adds it to `PATH`, and can wrap a test command in
`mock run`:

```yaml
- uses: sspencer/mock@v0.6.0
  with:
    args: -d mocks --strict=unused
    run: npm test
    strict-report: mock-junit.xml
```

| Input | Default | Meaning |
|-------|---------|---------|
| `version` | the action's tag, such as `v0.6.0`, else `latest` | The release to install |
| `run` | | A command to run with `mock run`, through `bash`; empty installs `mock` only |
| `args` | | `mock` flags and request files for `mock run`, split on spaces |
| `strict-report` | | With `run`, also write the [strict report](#strict-reports-for-ci) to this file |
| `token` | `github.token` | Used to look up the latest release without hitting API rate limits |

The step fails like `mock run` does: with the command's status, or `1` when
it passed but a request went unmatched (or a route never matched, with
`--strict=unused`). The `version` output is the installed tag. Without `run`,
later steps can call `mock` directly, for example
`mock validate -strict mocks`, `mock fmt -check mocks` (see
[Formatting request files](cli.md#formatting-request-files)) or `mock run mocks/api.http -- ./test.sh`. A
JUnit report can go to any step or action that publishes test results.

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
- `AssertCalledWith(t, call, mocktest.With{...})` passes when at least one
  request the call names carries everything in `With` and returns those
  requests. `Header` and `Query` map names to values in the
  [matcher syntax](matching.md#matcher-values) (`"acme"`, `"*"`, `"!"`, `"~[0-9]+"`, `">100"`),
  `Body` is an [`@body`](matching.md#body-matching) matcher and `JSON` a document the body
  must contain, as [`@json`](matching.md#body-matching) takes it. A failure lists why each
  call fell short:

  ```go
  api.AssertCalledWith(t, "POST /users", mocktest.With{
  	Header: map[string]string{"Authorization": "~Bearer .+"},
  	JSON:   `{"name": "avery", "plan": "free"}`,
  })
  ```

- `LastCall(t, call)` returns the most recent request a call names, and
  `WaitFor(t, call, timeout)` waits for the first one, for code that calls
  the server in the background. Both stop the test when there is none.
- `AssertAllRoutesCalled(t)` fails when a route from the request files
  answered nothing, as [`--strict=unused`](#strict-mode-and-route-coverage)
  does; stubs are not checked.
- `Stub(t, text)` serves the routes in `.http` text in front of the server's
  routes until the test (or subtest) ends, so one test can change an answer:

  ```go
  api.Stub(t, "### [503] Down\nGET /users\n\n{\"error\": \"down\"}\n")
  ```

  Later stubs win over earlier ones. Other languages use
  [`POST /routes`](admin-ui.md#temporary-routes) on the admin port.
- `Add(t, routes...)` does the same for routes built from Go values, for a
  test that computes its response; it returns the routes' names for
  `AssertCalled`:

  ```go
  api.Add(t, mocktest.Route{
      Method: "GET", Path: "/users/:id", Status: 503,
      JSON:        map[string]any{"error": "down", "retryAt": retryAt},
      MatchHeader: map[string]string{"Authorization": "*"},
  })
  ```

  `Route` has `Name`, `Method` (default `GET`), `Path` (with `:name`
  parameters, a trailing `*` and an optional `?query`), `Status`, response
  `Headers`, `Body` or `JSON` (marshaled, with `Content-Type:
  application/json` unless `Headers` sets one), `MatchHeader` and
  `MatchQuery` ([matcher values](matching.md#matcher-values)), `MatchJSON`
  (as `@json`), `Delay` and `Times`. The body is sent as is; set
  `Template: true` to render it as a template, so `{{$path.id}}` works.
- `Reset()` starts [`@times`](matching.md#routes-that-answer-a-few-times)
  and `{{$hits}}` counts, rate limits and chance draws over, and removes run-time overrides
  and temporary routes,
  so a test can reuse one server; other languages `POST /reset` to the admin
  port.

From other languages, [`POST /verify`](admin-ui.md#verifying-calls) makes the
same assertions on the admin port, and [`/requests`](admin-ui.md#reading-the-request-log)
returns the request log.
