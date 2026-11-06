# Potential features

Planning notes checked against the current codebase on 2026-10-29. These are
ideas for future work, not committed release plans. Implemented items are kept
separately below so they do not remain in the backlog.

## Suggested order

1. **Close small test gaps.** `warnUnknownPlaceholders` (unresolved
   placeholders and invalid generator arguments) has no direct test in
   `mockhttp`; the bad-argument warning is only exercised through the
   `mock validate` tests. Add direct tests there.
2. **JSON array matching.** `@json.roles.* = admin` (any element matches);
   today only fixed indexes such as `roles.0` work.
3. **Seeded generated data.**
4. **Named scenarios.**
5. **Aggregate validation diagnostics.** `mock validate` stops at the first load
   error.

## Deferred ideas

- **Broader REST Client import:** unnamed sections, comments before the first
  section, HTTP versions on request lines (all currently load errors),
  environment files and request variables, and explicit request-to-response
  conversion warnings. Named `###` sections, `@header.*` matchers,
  `@name = value` variables, and `{{host}}` or full URLs in the request target
  (whose port selects the listener) are supported; warnings already flag likely request headers used as response
  headers.
- **Richer matching:** JSON paths with dotted keys or any-element wildcards,
  cookies (`@cookie.name`), "present but not X" predicates, and multipart or
  XML bodies. Current matching covers methods, encoded paths and
  path parameters, declared query values, and exact, present (`*`), absent
  (`!`) and regex (`~regex`) matchers on headers, query and path parameters,
  the raw body, JSON fields and form fields, plus trailing `*` path wildcards. The first matching route in load order wins,
  so catch-alls go last; shadowed routes get a load warning.
- **Response modes and scenarios:** named stateful scenarios (for example,
  "second call fails") to replace the removed response rotation for retry
  tests.
- **Reproducible generated data:** seeded faker values and
  `{{$randomInt}}`, and configurable reuse of one generated placeholder within a
  response. Generated values are currently random on every render.
- **Request assertions:** structured assertions for integration tests, such as
  expected request counts, ordering, and matching constraints.
- **Template operators and namespaces:** context-aware operators and explicit
  path/query/fixture namespaces. JSON string escaping, header expansion, and
  preservation of unresolved placeholders already exist; path parameters take
  precedence over query parameters with the same name. The generator functions
  with arguments (see below) are the only callable placeholders; there are no
  general operators.
- **Credential redaction:** optional redaction for diagnostic history, copied
  requests, and exports. Current captures and Copy cURL include credentials.
- **Explicit body modes:** selectable raw/template response-file modes and exact
  inline-body preservation. File templating currently depends on content type
  and text detection; inline bodies normalize line endings and trim trailing
  blank lines. File-backed bodies already preserve bytes when served raw.
- **Aggregate validation diagnostics:** `mock validate` reports warnings for
  every route but stops at the first load error, so fixing a file can reveal the
  next error. Collecting several parse errors per run would need the parser to
  recover after an error.
- **Graphical fixture editing and proxy/recording:** lower-priority ideas.

## Implemented from the original ideas

- **Form matchers, wildcards and request placeholders:** `@form.name`
  matchers, trailing `*`/`*name` path wildcards, `{{$json.*}}` and
  `{{$form.*}}` body placeholders, and named regex captures as placeholders.
- **Body matchers and route order:** `@body` and `@json.field.path` matchers.
  Response rotation, `POST /reset` and the Reset responses button were
  removed; the first matching route answers, and routes an earlier route
  always beats are warned about.
- **Absent and regex matchers:** `@header.Name = !` matches a missing or blank
  header, and `~regex` values match in full. `@query.name` and `@path.name`
  use the same value syntax.

- **Multiple ports:** routes are served on the port of a full-URL request
  target, or the `@port` variable, else the default `-p` port. Every port is
  bound at startup; reloads cannot open new ports.

- **Separate admin port:** the request console listens on its own port (`-a`,
  `MOCK_UI_PORT`, default `8081`) so any path can be mocked, and `-x` skips it.
  Mock and admin ports that collide are a startup error.
- **REST Client-style request files:** `@name = value` file and method
  variables, `{{name}}` and `{{%name}}` references, a `[status]` section-name
  prefix, `< path` external bodies, `@delay` and `@header.Name` directives, and
  variables or full URLs in the request target.
- **Sample curl for routes:** selecting a route shows a copyable `curl` command
  for the mock server, with sample values for path parameters and any required
  `@header.*` headers.
- **REST Client generators:** `{{$guid}}`, `{{$randomInt min max}}`,
  `{{$timestamp [offset option]}}`, `{{$datetime rfc1123|iso8601 [offset option]}}`,
  and `{{$localDatetime ...}}`. Invalid arguments stay literal with a load-time
  warning. Only these functions take arguments; other REST Client dynamic
  variables (`$processEnv`, `$dotenv`, `$aadToken`) are not supported.
- **Command-line help:** `-h`, `-help` and `--help` print usage and exit `0`,
  including for `mock validate`.
- **Validation CLI:** `mock validate` loads request files without serving them,
  reports load errors and startup warnings, supports `-strict` and `-json`, and
  exits non-zero for CI. It stops at the first load error rather than
  aggregating several.
- **Inspector content tools:** Pretty/Raw views for complete JSON, Copy body for
  text, Copy base64 for binary captures, and shell-quoted Copy cURL for complete
  text requests. Binary or incomplete requests receive an explanation instead
  of a replay command. Clipboard errors remain visible; routine copy-success
  notices have been removed.
- **Route inspection and match diagnostics:** clickable route configuration,
  source file/line, configuration revision, and up
  to five nearby candidates explaining unmatched requests. Route previews show
  inline templates or file references; they do not load response-file contents.
- **Reload diagnostics:** `/state` exposes the active revision, route count,
  last successful reload, and reload error. Failed reloads keep working routes
  and show a dashboard error banner; the routine configuration banner is hidden.
- **Clear log:** `POST /clear` clears diagnostic history across connected
  tabs.
- **Resizable inspector:** traffic/inspector, traffic/routes, and request/response
  dividers, with remembered sizes, keyboard controls, and double-click reset.
- **Mobile navigation:** traffic and route selections open the detail view with
  a back button to return to the lists.

Admin paths above (`/events`, `/clear`, `/state`, `/routes`) are served on the admin port.

## Related improvements already available

- Server history, dashboard traffic, and the paused-traffic buffer retain up to
  1,000 requests. SSE supports ordered replay, restart/history-gap resets, and
  clear notifications across connected clients.
- Captures include byte counts, truncation metadata, and base64 for binary data.
  HAR export preserves those details, with a 64 KiB capture limit per body.
- Reloads are serialized and dependency watches follow the active source files.
  Each request is matched against a single configuration revision.
- The traffic list omits the duration beneath each request; elapsed time remains
  available in the inspector.

See [README.md](README.md) for supported behavior and controls, and
[docs/recommendations.md](docs/recommendations.md) for improvement history.
