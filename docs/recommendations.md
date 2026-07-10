# Improvement Recommendations

Captured from a full project review (2026-07-10). Status tracks implementation on `master`.

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
| 4 | Basic OpenAPI → route summary import helper | done |
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
