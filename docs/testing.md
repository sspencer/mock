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
  usually mean a stale fixture or a test that no longer runs.
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
- `Reset()` starts [`@times`](matching.md#routes-that-answer-a-few-times)
  counts, rate limits and chance draws over, and removes run-time overrides,
  so a test can reuse one server; other languages `POST /reset` to the admin
  port.

From other languages, read the same data from [`/requests`](admin-ui.md#reading-the-request-log)
on the admin port.
