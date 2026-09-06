# Improvement Recommendations

## September 2026 audit follow-up (`astra`)

The July checklist below records historical implementation, not proof that every
edge case was covered. The follow-up audit found and corrected these gaps:

| Area | Follow-up implementation and verification |
|---|---|
| Matching | Decode path parameters once; isolate rotation by matched route identity; preserve path values over query values; own mutable route configuration. Encoded-path and tenant-sequence regression tests. |
| Parsing | Reject malformed queries, invalid headers, unsafe dependencies, and invalid status/delay directives with source lines. Shared variable grammar and parser fuzz target. |
| Rendering | Preserve unknown placeholders; warn on unresolved names and inspect text dependencies; escape JSON string values; check complete text bodies and content types; contain symlinks within the fixture directory. |
| Watcher | Serialize callbacks, wait on shutdown, resolve dependencies against their source only, and reconcile watches after reload. Missing-directory and close-during-reload tests. |
| SSE | Ordered publication, bounded subscriber queues with reconnect recovery, session-aware cursors, visible history-gap notifications, clear boundaries across tabs, bounded writes, and heartbeats. |
| Capture/HAR | Structured headers and byte metadata, TLS scheme, binary encoding, explicit truncation, honest timing, and correct HEAD suppression. Go and JavaScript regression tests. |
| Dashboard | Bounded pause buffer, batched keyed rows, selection cleanup, storage failure tolerance, unchanged-route polling suppression, clearer action labels, and more traffic space. DOM-adapter behavior tests; live visual verification remains necessary. |
| HTTP/admin | Separate mock CORS from admin access, reject cross-origin clear, validate mount/port configuration, and bound normal response writes without consuming configured delays. |
| Engineering | Non-mutating format checks, dependency scanning, dashboard tests, and release verification. |

New product capabilities are deferred. Existing dialect constraints and capture
limits are documented in README.md. No claim of full REST Client compatibility
or lossless capture beyond the bounded history/body limits is made.

## Historical July 2026 checklist

Captured from a full project review (2026-07-10); the statuses below describe the
original implementation and are superseded by the follow-up notes above.

## Overall take

`mock` is in solid shape for a small tool: clear package boundaries (`restclient` / `mockhttp` / CLI), good README, embedded UI, hot-reload, and strong `mockhttp` coverage. Highest-value work is operational hardening, UI/SSE correctness, and a few product features—not a rewrite.

---

## P0 — Bugs / correctness

| # | Item | Status |
|---|------|--------|
| 1 | Graceful shutdown (SIGINT/SIGTERM, HTTP `Shutdown`, close watcher) | done |
| 2 | SSE reconnect duplicates (event IDs / controlled replay) | done |
| 3 | Clear button only clears browser (server clear endpoint) | done |
| 4 | Unbounded client-side event list | done |
| 5 | Fragile UI row indexing (stable event IDs) | done |
| 6 | Invalid `$status` / `$delay` silently ignored (warn at load/serve) | done |

## P1 — Reliability & operations

| # | Item | Status |
|---|------|--------|
| 7 | Complete HTTP server timeouts (read/write/idle; SSE-aware) | done |
| 8 | Dockerfile: copy `go.sum` before `go mod download` | done |
| 9 | Reload debounce + single retry on parse race | done |
| 10 | Watch `$file` response dependencies | done |
| 11 | Extract `run()` for testable main wiring | done |
| 12 | CI: race tests | done |

## P2 — Code quality & organization

| # | Item | Status |
|---|------|--------|
| 13 | Module path `github.com/sspencer/mock` | done |
| 14 | Keep packages small; watcher stays in main with `run()` boundary | done |
| 15 | Archive completed `tasks/` notes; remove `todo.md` | done |
| 16 | Reduce global faker lock contention (per-call generator under mutex still; pool optional) | done |
| 17 | `responseCapture.Unwrap()` | done |
| 18 | Placeholder expansion in response headers | done |
| 19 | Binary-safe `$file` bodies (`[]byte`) | done |

## P3 — Testing

| # | Item | Status |
|---|------|--------|
| — | E2E reload test | done |
| — | SSE id / clear / client contract coverage | done |
| — | Concurrent `SetMethods` under race | done |
| — | Parser negative paths | done |
| — | Watcher multi-file / dependency paths | done |

## P4 — Product features

| # | Item | Status |
|---|------|--------|
| 1 | CORS (`-cors`) | done |
| 2 | Request header matching from `.http` headers | done |
| 3 | Reset rotation counters + clear events API | done |
| 4 | Basic OpenAPI → route summary import helper | removed (feature deleted) |
| 5 | Bind address (`-b`) | done |
| 6 | Optional TLS (`-cert` / `-key`) | done |
| 7 | UI filter, pause stream, export HAR | done |
| 8 | UI routes panel (live route list) | done |
| 9 | Watch `$file` deps (see P1) | done |
| 10 | Document REST Client dialect subset | done |

## P5 — Docs & DX

| # | Item | Status |
|---|------|--------|
| — | UI path vs mock path conflicts | done |
| — | Rotation semantics across files | done |
| — | Hot-reload failure behavior | done |
| — | Supported `.http` dialect | done |
| — | Version flag | done |

---

## Suggested historical priority order

1. Graceful shutdown + signal handling  
2. SSE reconnect + Clear + client event cap  
3. Warn on invalid `$status` / `$delay`  
4. Watch `$file` dependencies  
5. Docker `go.sum` + bind/CORS flags  
6. Extract `run()` for main testability  
7. Archive `tasks/` history  
8. Matching / CORS / routes panel  

These were implemented as feature branches merged to `master`.
