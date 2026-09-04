# Changelog

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
