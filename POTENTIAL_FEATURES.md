Ranked by value. 

**Do first**
1. **Cut a release.** "Unreleased" now holds several breaking changes: `-l` is gone, the console moved to its own port, and the variable syntax changed. Tag it `v0.2.0` so users get a clear boundary.
2. **`mock validate`.** It would load the files, print all diagnostics, and exit nonzero on errors, with an optional strict mode that treats warnings as errors and JSON output. Load-time validation and warnings already exist, so this is mostly plumbing, and it would be useful in CI.
3. **Close small test gaps.** There's no test for the load-time warning on bad placeholder arguments (`warnUnknownPlaceholders`). I only checked it by hand. The Docker examples may also still need a second `-p` mapping for the admin port, so check that.

**Next**
4. **Richer matching.** Matching on JSON body fields, absent-header checks, and explicit priority or fallback routes. Today you can only match on method, path, query, and exact headers. This is probably the most common reason someone outgrows the tool.
5. **Seeded generated data.** A seed makes faker output and `$randomInt` reproducible, which tests need. Reusing one generated value several times within a response would help too.
6. **Named scenarios.** Selecting a response mode explicitly, or switching stateful scenarios, instead of only rotating through responses.

**Later**
- Redact credentials in captures, copied cURL and HAR exports (they currently include them).
- Request assertions, such as expected counts and ordering, for integration tests.
- Broader REST Client import: unnamed sections, comments before the first section, HTTP versions on request lines.
- Proxy/recording mode and graphical fixture editing. These are bigger and lower priority.

I'd start with 1 and 2, then 4. Want me to take any of these on?




# Potential features

Planning notes checked against the current codebase on 2026-09-06. These are
ideas for future work, not committed release plans. Implemented items are kept
separately below so they do not remain in the backlog.

## Deferred ideas

- **Broader REST Client import:** unnamed sections, comments before the first
  section, HTTP versions on request lines, environment variables and request variables, and
  explicit request-to-response conversion warnings. Named `###` sections and
  `@header.*` matchers and `@name = value` file variables are supported; warnings already flag likely request
  headers used as response headers.
- **Richer matching:** JSON/body fields, regular expressions, absent-header
  predicates, explicit priorities, and fallback routes. Current matching covers
  methods, encoded paths and path parameters, declared query values, and exact
  or non-empty wildcard request headers.
- **Response modes and scenarios:** explicit selection modes and named stateful
  scenarios beyond the existing rotation through matching responses and manual
  sequence reset.
- **Reproducible generated data:** seeded values and configurable reuse of a
  generated placeholder within a response.
- **Request assertions:** structured assertions for integration tests, such as
  expected request counts, ordering, and matching constraints.
- **Template operators and namespaces:** context-aware operators and explicit
  path/query/fixture namespaces. JSON string escaping, header expansion, and
  preservation of unresolved placeholders already exist; path parameters take
  precedence over query parameters with the same name.
- **Credential redaction:** optional redaction for diagnostic history, copied
  requests, and exports. Current captures and Copy cURL include credentials.
- **Explicit body modes:** selectable raw/template response-file modes and exact
  inline-body preservation. File templating currently depends on content type
  and text detection; inline bodies normalize line endings and trim trailing
  blank lines. File-backed bodies already preserve bytes when served raw.
- **Graphical fixture editing and proxy/recording:** lower-priority ideas.

## Implemented from the original ideas

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
  source file/line, configuration revision, selected response position, and up
  to five nearby candidates explaining unmatched requests. Route previews show
  inline templates or file references; they do not load response-file contents.
- **Reload diagnostics:** `/state` exposes the active revision, route count,
  last successful reload, and reload error. Failed reloads keep working routes
  and show a dashboard error banner; the routine configuration banner is hidden.
- **Independent clear and reset:** `POST /clear` clears diagnostic history;
  `POST /reset` restarts response sequences. Both have separate UI controls.
- **Resizable inspector:** traffic/inspector, traffic/routes, and request/response
  dividers, with remembered sizes, keyboard controls, and double-click reset.
- **Mobile navigation:** traffic and route selections open the detail view with
  a back button to return to the lists.

Admin paths above are served on the admin port (`-a`, default `8081`).

## Related improvements already available

- Server history, dashboard traffic, and the paused-traffic buffer retain up to
  1,000 requests. SSE supports ordered replay, restart/history-gap resets, and
  clear notifications across connected clients.
- Captures include byte counts, truncation metadata, and base64 for binary data.
  HAR export preserves those details, with a 64 KiB capture limit per body.
- Reloads are serialized and dependency watches follow the active source files.
  Matching and response rotation stay within a single configuration revision.
- The traffic list omits the duration beneath each request; elapsed time remains
  available in the inspector.

See [README.md](README.md) for supported behavior and controls, and
[docs/recommendations.md](docs/recommendations.md) for improvement history.
