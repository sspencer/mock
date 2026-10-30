# Changelog

## v0.2.0 - 2026-10-29

The request console moves to its own port, `.http` files get `@name = value` variables, and `mock validate` and REST Client–style generator placeholders arrive. This release has breaking changes; see below.

- **Breaking:** serve the request console on its own port instead of under `/mock/` on the mock port, so any path (including `/mock`) can be mocked. The console is at the root of the admin port, with the same events, clear, reset, state, and routes endpoints.
- Add `-a PORT` for the admin port. It defaults to `8081`, or to a random free port when `-p` or `MOCK_PORT` is set. `-a 0` always picks a random port. The `MOCK_UI_PORT` environment variable sets the admin port when `-a` is omitted. `mock` exits with an error if the mock and admin ports are the same. The startup banner prints the actual address.
- Add `-x` to run without the admin UI. It cannot be combined with `-a`.
- Remove the `-l` flag.
- Show a copyable sample `curl` command for the mock server when a route is selected in the admin UI, with sample values for path parameters.
- Add an optional `[status]` prefix on section names (for example `### [204] Delete a user`) to set the response status. It defaults to `200`.
- Define variables as `@name = value`. Variables above the first `###` (or in a `###` section with no request line) are file-scoped; variables between a `###` line and its request line are method-scoped and override file ones. Values may be escaped (`\n`) and may reference other variables.
- Reference variables as `{{name}}`, or `{{%name}}` to percent-encode. `{{$name}}` now means only path/query parameters and generated values.
- Replace the `# $delay=` and `# $header.Name=` directives with `@delay` and `@header.Name`. All `# $name=value` comment variables are now a load error that points to the `@` form.
- Replace the `$file` variable with an external body: start the body with `< ` followed by the file path, such as `< ./users.json`. Paths must be relative, without `..` segments, and are looked up next to the `.http` file and then in the workspace root; symlinks cannot escape either directory. `$file` and `@file` are now load errors that point to the new syntax.
- Warn about unused variables once per definition instead of per section.
- Remove the `$status` variable; using it is now a load error that points to the section-name status.
- Expand `{{name}}` variables in the request target and ignore the scheme and host of a full URL, so `GET {{host}}/users` in a shared REST Client file matches `/users`. See `examples/restclient.http`.
- Add REST Client–style generator placeholders with arguments: `{{$randomInt min max}}`, `{{$timestamp [offset option]}}`, `{{$datetime rfc1123|iso8601 [offset option]}}`, and `{{$localDatetime rfc1123|iso8601 [offset option]}}`. `{{$guid}}` already worked. Invalid arguments stay literal and log a load-time warning.
- Add `mock validate [-strict] [-json] <file.http>...` to check request files without starting a server. It reports load errors and the same warnings `mock` logs at startup, exits non-zero on errors (or warnings with `-strict`), and can print a JSON report for CI.
- Convert the example `.http` files to the new syntax.

## v0.1.4 - 2026-09-06

Audit cleanup after v0.1.3: tighter admin CSRF checks and small correctness/housekeeping fixes.

- Tighten CSRF checks on admin clear/reset (`POST /mock/clear`, `POST /mock/reset`)
- Fix a tautological `statusAllowsBody` check
- Remove dead helpers / leftover scaffolding and the unused `astra` CI branch reference

## v0.1.3 - 2026-09-06

Expanded request inspection, more reliable traffic capture and reloads, and
correctness and security fixes since v0.1.2.

- Add draggable, keyboard-accessible panel dividers with remembered sizes.
- Add Pretty/Raw views, body and cURL copy actions, clickable route configuration,
  matched fixture/sequence metadata, mismatch explanations, and mobile navigation.
- Show active configuration revisions and reload errors in the dashboard.
- Separate clearing traffic from resetting fixture sequences (`POST /mock/reset`).
- Increase retained traffic to 1,000 requests and show the retention limit in the counter.
- Give HTTP methods distinct colors in both themes, use light inspector backgrounds
  and dark text in light mode, and match inspector heading sizes to Traffic and Routes.
- Reclaim dashboard space by showing reload errors only when needed, keeping request
  duration in inspector metadata, and removing copy-success notices.
- Add `examples/send-requests.sh` to populate the console with sample traffic and
  a PUT user-creation example; refresh the README screenshot with the dark-mode UI.
- Correct encoded path matching, isolate header-based response sequences, and own route snapshots.
- Reject malformed query/header constraints and invalid response directives at load time.
- Preserve unresolved placeholders, escape JSON string interpolation, and contain response-file reads.
- Serialize reloads and update dependency watches after configuration changes.
- Recover SSE overflow/restarts with ordered session cursors and synchronized clear boundaries.
- Export structured, byte-aware HAR captures including TLS, binary encoding, and truncation metadata.
- Buffer paused traffic, batch stable dashboard rows, and clear evicted request details.
- Isolate admin CORS, bound response writes, and validate CLI mount/port values.
- Update `golang.org/x/sys` to v0.44.0 for the Windows Unicode conversion advisory.
- Check formatting without mutation and verify Go/dashboard tests and dependencies before release.

## v0.1.2 - 2026-09-03

Security and correctness hardening, plus further request-console refinements
after the v0.1.1 redesign.

- Defaulted the bind address to localhost (`127.0.0.1`) and warned when binding
  all interfaces (`-b 0.0.0.0`), since the admin UI is unauthenticated
- Limited CORS short-circuiting to genuine browser preflight requests (`OPTIONS`
  with `Access-Control-Request-Method`); other `OPTIONS` now run the mock route
- Required `X-Requested-With` or a JSON `Content-Type` on `POST /mock/clear` to
  guard the admin clear endpoint against cross-site requests
- Keyed response rotation by route (method, path, and query) so repeated
  requests rotate through the intended set of responses
- Made the request-log export produce valid HAR (HTTP Archive) files
- Ran the Docker image as the unprivileged `nobody` user
- Added load-time warnings for `.http` dialect issues, such as using a
  well-known incoming request header as a response header without a matching
  `$header.*` matcher
- Reworked the request console into a denser operator layout: request and route
  counts on the panel titles, larger headings, a sticky table header,
  keyboard-navigable log rows, clearer Pause help text, and enabling Clear only
  after the server confirms the log was cleared
- Removed unused table headers from the request log
- Gave the selected request row in the Traffic panel its own bolder jade-green
  background and accent bar so it is clearly distinct from the lighter hover
  highlight, including when the selected row itself is hovered
- Removed OpenAPI stub seeding (`-openapi`, `examples/openapi.json`, `examples/openapi.yaml`). Use `.http` files.
- Updated the README screenshots to show the current console

## v0.1.1 - 2026-09-02

This release redesigns the request-log web interface as a clearer, more
practical console for inspecting local HTTP traffic.

- Reworked the interface into a responsive two-column workspace with the
  request log and configured routes on the left and full-width request and
  response inspectors on the right
- Added a persistent Hide / Show control for the configured-routes panel so
  the request log can use the available vertical space
- Added live connection state, request and route counters, compact HTTP method
  and status badges, and a `/` keyboard shortcut for filtering
- Refined light and dark themes, focus states, empty states, responsive
  behavior, and control accessibility
- Removed inspector transition flashes when selecting or receiving requests
- Updated the README screenshot to show the redesigned interface in use

## v0.1.0 - 2026-08-29

First tagged release.

`mock` serves REST Client-style `.http` files as a local HTTP server, with a
request-log UI, hot reload, and OpenAPI stub seeding.

- CLI: `-p` / `MOCK_PORT`, `-b`, `-l`, `-cors`, `-cert`/`-key`, `-openapi`, `-version`
- Request files: named `###` sections, `$status`, `$delay`, `$file`, `$header.*`, placeholders, rotation
- Hot reload of command-line `.http` files and relative `$file` bodies
- Admin UI: live log, routes panel, filter, pause, clear, HAR export, theme toggle, Help
- Docker image for running examples or a mounted request file
- `make build` installs into `GOBIN` and always rewrites the binary
