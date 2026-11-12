[Documentation](README.md) › Ideas

# Ideas

Feature enhancements and other ideas for `mock`, collected on 2026-11-10
(after v0.5.1) by reviewing this project and comparable tools: WireMock,
Mockoon, Prism, Hoverfly, Mountebank, MockServer, Smocker, json-server and
MSW. These are suggestions, not a release plan. Ideas are removed from this
page once they are implemented; the [docs](README.md) describe those. Check
the code before relying on any detail.

## How to read this page

Each idea has an ID to refer to it by (for example `ST-1`), an effort flag and
a short description. Where another tool has the feature, it is named as a
reference point.

| Flag | Effort | Roughly |
|------|--------|---------|
| **S** | Small | A day or two. One package, no new concepts, little doc change |
| **M** | Medium | Up to a week. New directive or command, several packages, new docs and tests |
| **L** | Large | More than a week. New concept or subsystem, design decisions to make first |

Areas:

| Prefix | Area |
|--------|------|
| [`ST`](#stateful-behavior) | Stateful behavior |
| [`MA`](#matching-and-routing) | Matching and routing |
| [`RE`](#responses-and-templating) | Responses and templating |
| [`FA`](#latency-and-faults) | Latency and faults |
| [`AP`](#working-with-real-apis) | Working with real APIs |
| [`CL`](#command-line-and-configuration) | Command line and configuration |
| [`TE`](#testing-and-ci) | Testing and CI |
| [`UI`](#admin-ui) | Admin UI |
| [`PR`](#protocols) | Protocols |
| [`DI`](#distribution-and-editor-support) | Distribution and editor support |
| [`DO`](#documentation-and-examples) | Documentation and examples |

## Top picks

The ideas with the best value for their effort, in suggested order:

| ID | Idea | Effort | Why |
|----|------|--------|-----|
| [`ST-1`](#stateful-behavior) | `@times` | M | Retry tests ("fail twice, then succeed") cannot be expressed today |
| [`DI-1`](#distribution-and-editor-support) | Published Docker image and Homebrew formula | S | Removes the Go toolchain from the install path |
| [`TE-1`](#testing-and-ci) | `mock run -- command` | M | Start, test, stop and report coverage in one CI step |
| [`MA-1`](#matching-and-routing) | `mock match` | S | Answers "which route gets this request?" without serving |
| [`CL-1`](#command-line-and-configuration) | `@import` | M | Shared variables across files; the only "not supported" dialect item users ask for |
| [`UI-1`](#admin-ui) | Route switches in the UI | M | Turn a route off or add a fault while demonstrating or exploring |
| [`TE-4`](#testing-and-ci) | Temporary routes through the admin API | M | Opens per-test stubbing to every language, not only Go |
| [`AP-1`](#working-with-real-apis) | Deeper drift check | M | `mock check` compares only top-level JSON keys today |

## Stateful behavior

Routes are stateless today: the first matching route always answers, and
response rotation was removed on purpose. State is the largest gap in what a
route can express. These ideas keep the "first match wins" model; state only
decides whether a route is eligible.

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `ST-1` | `@times = n` | **M** | A route answers `n` times, then falls through to the next matching route. Covers "fail twice, then succeed" and "time out once". Counts live beside the hit counts in `coverage.go`; the shadow warnings must learn that a limited route does not shadow the one below it. |
| `ST-2` | Named scenarios | **L** | `@scenario`, `@state` and `@next-state`: a route is eligible only while its scenario is in a given state, and answering can move it to another ("order is `pending` until `POST /orders/1/pay`"). WireMock's scenarios work this way. Builds on `ST-1`. |
| `ST-3` | Reset state | **S** | `POST /reset` on the admin API (and `mocktest.Server.Reset`) returns counts, scenarios and rate limits to their starting values without a reload, so each test starts clean. Needed by `ST-1` and `ST-2`; useful for `@rate-limit` already. |
| `ST-4` | State in the admin UI | **S** | Show a route's remaining `@times` count and each scenario's current state in the route inspector, with a reset button. Follows `ST-1` to `ST-3`. |
| `ST-5` | Hit count placeholder | **S** | `{{$hits}}` inserts how many times the route has answered, for bodies such as `{"attempt": 3}` and for polling endpoints that report progress. |
| `ST-6` | In-memory collections | **L** | `@collection = users.json` turns a fixture into a small store: `GET` lists and reads, `POST` adds, `PUT`/`PATCH` change and `DELETE` removes, in memory until restart or reset. This is json-server's whole purpose and Mockoon's "CRUD routes on data buckets". It is a second way to define behavior, so decide first whether it fits a tool built on explicit routes. |
| `ST-7` | Captured values across requests | **L** | Store a value from one request (`@store token = {{$json.token}}`) and match or insert it in later ones (`{{$store.token}}`). Makes login-then-use flows realistic. Needs a scoping rule (global, per client, per session; see `TE-7`). |

## Matching and routing

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `MA-1` | `mock match` | **S** | `mock match api.http GET '/users?role=admin' -H 'X-Tenant: a'` prints the route that would answer, or the near-miss reasons, without starting a server. The near-miss logic and in-process serving (`/resend`, `mock check`) already exist. |
| `MA-2` | Numeric and length comparisons | **M** | Matcher forms such as `@json.amount = >100`, `@query.limit = <=50` and `@json.items = #>0` (array length). Today this needs a regex. Mountebank and MockServer have comparison predicates. |
| `MA-3` | Whole-body JSON match | **M** | `@json = {"type": "card", "amount": 100}` accepts a body that contains those members, in any key order and with extra members allowed. Easier to read than several `@json.*` lines. WireMock's `equalToJson` with `ignoreExtraElements`. |
| `MA-4` | JSON Schema matcher | **L** | `@schema = order.schema.json` accepts a request whose body validates against the schema, so one route can return `400` for anything invalid. Needs a schema library (the first sizable dependency) or a small subset validator. |
| `MA-5` | Host matcher | **S** | `@match.host = api.example.test` accepts requests by `Host` header, so one port can stand in for several services behind a proxy or `/etc/hosts` entries. The host in a request URL stays ignored unless this is set. |
| `MA-8` | Matching every value of a repeated field | **S** | Matchers read the first value of a repeated query parameter or header. Add `@query.tag[] = ~^(a\|b)$` (all values) or `@query.tag[any]`, and `{{$query.tag[]}}` as a JSON array. |
| `MA-10` | Route tags | **S** | `@tag = billing, slow` on a section; `--only billing` and `--skip slow` choose what to serve, and the UI and `/routes` can group and filter by tag. |

## Responses and templating

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `RE-8` | YAML fixtures | **M** | `@fixture` reads JSON, CSV and NDJSON. YAML needs a third-party parser (the first new dependency besides `fsnotify` and `faker`), or a small subset reader for maps, lists and scalars. The fixture editor would stay JSON-only. |

## Latency and faults

`@delay`, `@stream`, `@fault = reset` / `truncate` and `@rate-limit` cover the
basics. Comparable tools go further in simulating bad networks.

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `FA-1` | More fault kinds | **M** | `@fault = hang` (accept and never answer, for timeout tests), `empty` (close after the request with no bytes), and `garbage` (send bytes that are not HTTP). WireMock has all three. |
| `FA-2` | Bandwidth throttling | **M** | `@throttle = 32kb/s` sends the body at a fixed rate, for download progress and slow-network tests. `@stream` pauses between lines but cannot model a slow large file. |
| `FA-3` | Latency distributions | **S** | `@delay = lognormal 90ms 0.4` or `@delay = p50 80ms p99 900ms` for a realistic long tail; a uniform range is all `@delay` offers now. |
| `FA-4` | Named fault profiles | **S** | `--profile flaky` (or `3g`, `slow`) as shorthand for a `--delay` and `--fault` pair, with the profiles listed in `mock -h`. |
| `FA-5` | Rate limit headers and keys | **S** | `@rate-limit.key = {{$header.Authorization}}` counts per client instead of per route, and the response carries `X-RateLimit-Limit`, `-Remaining` and `-Reset`, which real APIs send and clients read. |
| `FA-6` | Faults from the admin API | **M** | `POST /faults` sets or clears a delay or fault for a route (or all routes) at run time, so a test can break a dependency mid-run. Shares its switch with `UI-1`. |

## Working with real APIs

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `AP-1` | Deeper drift check | **M** | `mock check` compares status, media type and top-level JSON keys. Add nested keys, value types (`string` versus `number`), array element shape and a chosen list of headers, with `-depth` to limit noise. |
| `AP-2` | Check against a schema | **L** | `mock check -openapi spec.yaml` validates each route's rendered response against the operation's schema, without calling the real service. This is a check only; the OpenAPI *import* that was removed is not proposed again (see [Not planned](#not-planned)). Prism validates this way. |
| `AP-3` | Recording options | **M** | `-r` records each method and path once. Add `--record-by query,body` to keep variants as separate sections with matchers, `--record-bodies dir` to write large or binary bodies as `< files`, and `--record-refresh` to replace sections whose upstream response changed. |
| `AP-4` | Import from curl and Postman | **M** | `mock import` reads HAR only. `mock import -curl` (a pasted command, from stdin) is small; a Postman collection with saved example responses is the larger half. Both reuse `draftSection`. |
| `AP-5` | Import WireMock mappings | **M** | Convert WireMock stub JSON (`mappings/*.json` and `__files`) into `.http` sections, with a comment where a matcher has no equivalent. Lowers the cost of moving to `mock`. |
| `AP-6` | Export routes | **M** | `mock export -format openapi\|postman\|har` writes a skeleton from the routes and their sample requests (`buildSampleRequest`), to share a mocked API with people who use other tools. |
| `AP-7` | Live diff mode | **M** | `--upstream URL --diff` answers from the mock but also sends each matched request upstream and records the differences on the event, shown in the UI. Hoverfly calls this diff mode. Reuses `compareResponses` and the compare view. |
| `AP-8` | Upstream request rewriting | **S** | `--upstream-header 'Authorization: Bearer ...'` and a path prefix (`--upstream-strip /api`) for forwarded requests, so a client pointed at the mock can reach a real API that needs credentials it does not have. |
| `AP-9` | Forward proxy mode | **M** | Accept absolute-form requests (`GET http://host/path`) so a client configured with `HTTP_PROXY` is mocked without changing its base URL, with `MA-5` choosing routes by host. Plain HTTP only; `CONNECT` tunnels would need TLS interception, which is out of scope. |

## Command line and configuration

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `CL-1` | `@import` | **M** | `@import ./common.http` (or a variables-only file) shares `@host`, tokens and defaults across request files. The dialect table lists imports as not supported. Needs cycle detection and watcher support for the imported files. |
| `CL-2` | Project config file | **S** | A `mock.toml` (or `.mockrc`) in the working directory holding the flags a project always passes: files, directories, ports, upstream, redaction. Flags still win. |
| `CL-3` | Report every load error | **M** | `mock validate` stops at the first parse error, so fixing a file reveals the next one. Let the parser recover at the next `###` and report all of them. |
| `CL-4` | `mock fmt` | **M** | Normalize spacing, directive order and blank lines in `.http` files, like `gofmt`, with `-check` for CI. Most useful once files are shared across a team. |
| `CL-5` | `mock routes` | **S** | Print the route table (name, method, path, port, matchers, source line) as text or JSON without serving. `validate` reports problems; nothing prints the resolved routes offline. |
| `CL-6` | Shell completions and a man page | **S** | `mock completion bash\|zsh\|fish\|powershell` and a generated `mock.1`, shipped in the release archives. |
| `CL-7` | Readiness helpers | **S** | `--ready-file path` writes the bound addresses as JSON once every port is listening, and `mock wait [-timeout 10s]` blocks until the admin `/state` answers. Replaces `sleep 1` in scripts, and makes random ports (`-p 0`) usable. |
| `CL-8` | Open new ports on reload | **M** | A reload that adds a route on a new port is rejected today. Bind the new listener (and close ones no route uses) during reload, keeping the old plan when a bind fails. |
| `CL-9` | Configurable limits | **S** | `--max-body` (1 MiB today) and `--log-size` (1,000 events today) for large uploads and long sessions. |
| `CL-10` | Request log on disk | **S** | `--journal requests.jsonl` appends each event (redacted when `-R` is set) as a JSON line, so a run can be examined after `mock` exits and kept as a CI artifact. |
| `CL-11` | Reload environments | **S** | `-e` environment files are read once at startup. Watch them like request files. |
| `CL-12` | Listen on a Unix socket | **S** | `-b unix:/tmp/mock.sock` for sidecar and container setups that avoid TCP ports. |

## Testing and CI

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `TE-1` | `mock run -- command` | **M** | Start the server on free ports, export `MOCK_URL` (and `MOCK_ADMIN_URL`), run the command, stop, print the strict report and exit with the command's status (or `1` on unmatched requests). One line in CI instead of background jobs and traps. |
| `TE-2` | Richer `mocktest` assertions | **S** | `AssertCalledWith` (JSON body, header or query expectations), `LastCall`, `WaitFor(call, timeout)` for asynchronous clients, and `AssertAllRoutesCalled` using the existing hit counts. |
| `TE-3` | Routes from Go code | **M** | `srv.Add(mocktest.Route{...})` or `srv.AddSource(".http text")` for one test, removed on cleanup, in front of the file's routes. Lets a test override a single route without a second file. |
| `TE-4` | Temporary routes through the admin API | **M** | `POST /routes` with `.http` text adds routes that sit in front of the file's routes until `DELETE /routes/{id}`, a reset or a restart. The same idea as `TE-3` for tests in any language. WireMock, MockServer and Smocker are driven this way. |
| `TE-5` | Verification endpoint | **S** | `POST /verify` with expectations (`{"call": "POST /orders", "times": 1}`) answers `200` or `422` with a readable failure, so shell and non-Go tests need not parse `/requests`. |
| `TE-6` | Strict report formats | **S** | `--strict-report junit.xml` (or JSON) so CI systems show unmatched requests and unused routes as test results. Add `--strict=unused` to also fail on routes that never matched. |
| `TE-7` | Sessions for parallel tests | **L** | A header such as `X-Mock-Session: abc` separates the request log, counts and state per test, so parallel tests can share one server. Smocker has sessions. Only worth doing after the stateful ideas. |
| `TE-8` | Thin clients for other languages | **M** | Small packages for JavaScript and Python that start the binary, wait for it (`CL-7`) and wrap `/requests`, `/verify` and `/clear`: the `mocktest` experience outside Go. |
| `TE-9` | Testcontainers module | **S** | A documented container definition (image from `DI-1`, wait strategy on `/state`, mounted request files) for Java, .NET and Node test suites. |
| `TE-10` | Contract snapshot | **M** | `mock snapshot api.http > api.snap` renders every route with a fixed seed; `-check` fails when the output changes. Review of a request-file change then shows what clients will receive. |

## Admin UI

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `UI-1` | Route switches | **M** | In the route inspector: turn a route off, or set a delay, fault or forced status for it, until reset. Shown as a badge in the route list and in `/state`. Shares its mechanism with `FA-6`. |
| `UI-2` | Save a draft to the file | **M** | **Copy as route** copies text for the user to paste. Add **Add to file**, which appends the draft to a chosen request file through an admin endpoint guarded like `PUT /fixture` (current files only, version check, atomic write). |
| `UI-3` | Edit routes in the browser | **L** | Edit a section's `.http` text in the inspector with validation before saving. Larger than `UI-2` because of comments, variables and files the editor must not reformat. |
| `UI-4` | Route search and grouping | **S** | A filter box for the route list, and grouping by file, port or tag (`MA-10`). Long route lists are hard to scan today. |
| `UI-5` | Timing statistics | **M** | Per-route count, error share and p50/p95 duration, with a small timeline of request rate. Useful when `mock` stands in for a dependency in a load test. |
| `UI-6` | Body viewers | **M** | Image preview, a hex view for binary bodies, formatted XML, a decoded view for JWTs in `Authorization` headers, and a collapsible JSON tree (the fixture editor already has one). |
| `UI-7` | Import a HAR or journal | **M** | Load a HAR export or a `CL-10` journal into the traffic view to examine, filter and compare an earlier run. |
| `UI-8` | Copy in more formats | **S** | Beside **Copy cURL**: `fetch`, HTTPie, Go `net/http` and a REST Client request (the request half of an `.http` section). |
| `UI-9` | Pin and annotate requests | **S** | Pin requests so they stay when the log is cleared or the ring buffer wraps, with an optional note. Makes compare usable across a long session. |
| `UI-10` | Callback and proxy flow view | **M** | For a request that triggered a callback or was forwarded upstream, show the hops as a small sequence diagram with timings. Smocker draws one from its history. |
| `UI-11` | Saved filters | **S** | Keep recent and named traffic filters in `localStorage`, and put the current filter in the URL so a view can be bookmarked or shared. |
| `UI-12` | Unmatched request help | **S** | For an unmatched request, list the near-miss routes with the failing matcher highlighted and a one-click filter to that route. The data is already on the event. |

## Protocols

`mock` is an HTTP tool. These stay within HTTP; other protocols are under
[Not planned](#not-planned).

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `PR-1` | GraphQL matchers | **M** | `@graphql.operation = GetUser` and `@graphql.variables.id = 7` read the operation name, type and variables from a GraphQL-over-HTTP request (`POST` JSON or `GET` query), with `{{$graphql.variables.id}}` placeholders. `@json.operationName` works today only when the client sends the name. |
| `PR-2` | Timed event streams | **M** | `@stream` uses one pause for every piece. Let a body set its own timing (`# wait 2s` between events, `# repeat` to loop, `# close`), so server-sent event streams with heartbeats, bursts and reconnects (`Last-Event-ID`) can be modeled. |
| `PR-3` | HTTP/2 without TLS | **S** | Go's `http.Server.Protocols` can serve unencrypted HTTP/2 (`h2c`) on the same port. A `--h2c` flag lets HTTP/2-only clients, such as gRPC-Web or some service meshes, reach the mock. No TLS involved. |
| `PR-4` | JSON-RPC helper | **S** | `@jsonrpc.method = eth_getBalance` matches the method and `{{$jsonrpc.id}}` echoes the request ID, including for batch requests, which `@json.*` cannot address cleanly. |
| `PR-5` | SOAP conveniences | **S** | `@soap.action = GetQuote` matches the `SOAPAction` header or the body's first element, and a `{{$soap.fault code message}}` generator writes a fault envelope. `@xml.*` already does the matching work. |

## Distribution and editor support

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `DI-1` | Published Docker image and Homebrew formula | **S** | GoReleaser can push `ghcr.io/sspencer/mock:<version>` (multi-arch) and update a Homebrew tap from the existing release workflow. The image is only built locally today. |
| `DI-2` | More package formats | **S** | Scoop and WinGet manifests, `.deb`/`.rpm` through GoReleaser's `nfpms`, and an install script for CI. |
| `DI-3` | Signed releases | **S** | Sign `checksums.txt` with cosign and attach an SBOM and build provenance, for users whose policy requires them. |
| `DI-4` | GitHub Action | **S** | `uses: sspencer/setup-mock@v1` downloads the right binary, optionally starts it in the background and prints the strict report when the job ends. |
| `DI-5` | Syntax highlighting | **M** | A TextMate grammar (VS Code, and reusable by others) for the directives, matchers and `{{$placeholder}}` syntax on top of `.http` highlighting. |
| `DI-6` | Language server | **L** | Diagnostics from `mock validate -json` while typing, completion for directives, placeholders and filters, hover docs, and go-to-definition for `@variables` and `< files`. `CL-3` makes the diagnostics more useful. |
| `DI-7` | npm wrapper | **M** | An npm package that downloads the binary, so JavaScript projects can add `mock` as a dev dependency and run `npx`. Many front-end teams choose mock tools this way. |
| `DI-8` | Embeddable handler | **S** | Document and stabilize `mockhttp.Server` as an `http.Handler` for Go programs that want mock routes inside their own server (a dev mode, a demo binary), with an example. `mocktest` covers tests only. |

## Documentation and examples

| ID | Idea | Effort | Description |
|----|------|--------|-------------|
| `DO-1` | Example gallery | **M** | Realistic request files for common needs: OAuth token endpoint, paginated list, file upload, webhook sender, a payment API with failures. Each validated in CI like the existing examples. |
| `DO-2` | Migration guides | **S** | Short pages mapping WireMock stubs, json-server `db.json` and Mockoon environments to `.http` sections, with what does not carry over. Pairs with `AP-5`. |
| `DO-3` | Comparison page | **S** | When to choose `mock` and when another tool fits better (OpenAPI-first: Prism; in-browser: MSW; GUI: Mockoon). Sets expectations and explains the scope decisions below. |
| `DO-4` | Documentation site | **M** | Publish `docs/` with search (GitHub Pages and a static generator). The pages already have a breadcrumb and an index. |
| `DO-5` | Front-end recipes | **S** | Using `mock` behind the dev-server proxy of Vite, Next.js and webpack, which is also the answer to "why is there no CORS support". |
| `DO-6` | Troubleshooting page | **S** | "Why did my request not match", "why did reload fail", "port already in use", reading near-miss reasons. Much of the text exists across pages. |

## Not planned

Decisions already made. Listed so they are not proposed again without a new
reason.

- **TLS and CORS.** Removed on purpose. `mock` serves plain HTTP; put a
  TLS-terminating proxy in front of it, or use a dev-server proxy for browsers
  (`DO-5`).
- **Response rotation.** Removed in an earlier release in favor of "first
  match wins". `ST-1` and `@match.chance` cover its uses without it.
- **OpenAPI import.** A converter existed and was deleted. `AP-2` proposes
  checking against a schema, not generating routes from one.
- **Scripted or exec responses.** A JavaScript or shell handler would turn
  `mock` into an application server. Matchers, captures and templates are the
  intended tools, which is why `{{#if}}` blocks test only presence and
  equality.
- **gRPC and WebSockets.** Different protocols, out of scope for a REST
  Client-style tool.
