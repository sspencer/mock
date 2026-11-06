# Plan: stateful scenarios

Status: **deferred** (2026-11-06). Written after response rotation was removed in
#16, when "first call fails, retry succeeds" became impossible to express. It was
set aside because it adds more complexity than `mock` needs right now. Nothing
here is implemented. Check the code before relying on any detail; matching may
have changed since.

## Goals

1. Retry tests: "fail twice, then succeed", "time out once, then succeed".
2. Simple stateful flows: "GET `/orders/1` is `pending` until
   `POST /orders/1/pay`, then `paid`".
3. Keep the matching model: routes are tried in order and the first match
   answers. State only decides whether a route is currently eligible.

Not in scope: per-client state, state that survives restarts, time-based
transitions.

## Phase 1: `@times`

A route that answers a limited number of times, then falls through to the next
matching route.

```http
### [503] First two attempts fail
@times = 2
GET /flaky

{"error": "try again"}

### Then it succeeds
GET /flaky

{"ok": true}
```

- A count is used only when the route actually answers, not when it matched
  the path but failed another matcher.
- It combines with matchers, for example `@header.X-Test = retry-case` plus
  `@times = 1` fails once for one client.

## Phase 2: named scenarios

For flows where one request changes what another returns.

```http
### Order is pending until it is paid
@scenario = order
@state = started
GET /orders/1

{"status": "pending"}

### [201] Pay for the order
@scenario = order
@next-state = paid
POST /orders/1/pay

{"status": "paid"}

### Order after payment
@scenario = order
@state = paid
GET /orders/1

{"status": "paid"}
```

- Every scenario starts in `started`.
- `@state` limits a route to requests made while the scenario is in that state.
  Without it, the route matches in any state.
- `@next-state` changes the scenario's state after the route answers.
- `@scenario` set at the top of a file applies to every section in that file.
- `@times` works with scenarios, so "fail once" doesn't need three states.

Phase 1 solves the retry case alone and builds the storage, locking and reset
machinery that phase 2 reuses.

## Decisions (recommended)

| Question | Recommendation | Why |
|---|---|---|
| Directive names | `@times`, `@scenario`, `@state`, `@next-state` | `times` follows MockServer; hyphens are valid in variable names |
| State scope | Global per server | Same as rotation was; per-client state can come later, keyed by a header |
| Hot reload | Reset all counts and scenario states | Routes may have changed; rotation reset its counters the same way |
| Reset control | Restore `POST /reset` (counts and scenario states) and a **Reset state** UI button | Test suites need a clean start between cases; same CSRF rules as `/clear` |
| Shadow warning | A route with `@times` or `@state` never shadows later routes | The retry pattern would otherwise warn "route can never match" |
| `@times` at file scope | Load error | "Every route answers twice" is almost certainly a mistake |
| Seeing state | `/routes` gains `remaining`; `/state` gains a `scenarios` map; request events record `attempt` (e.g. 2 of 3) and any state change | Stateful mocks are hard to debug without it |

## Implementation outline

**Parser (`restclient/`)**
- Add `Times int`, `Scenario`, `State` and `NextState string` to `Method`.
- Parse them in `applyControlVariable` (`@times` a positive integer, state names
  valid identifiers) and add them to `controlVariables` so `UnusedVariables`
  ignores them.
- Load errors: file-scoped `@times`; `@state` or `@next-state` without a
  `@scenario`.

**Runtime (`mockhttp/`)**
- A `routeState` holds per-route answer counts and the current state per
  scenario. `SetMethods` swaps it together with the routes, so a request that
  started before a reload can't change the new configuration's state.
- In `findMethod`, stateless routes keep the current lock-free path. For a
  candidate with `@times` or a scenario, take the state lock, re-check
  eligibility, consume the count and apply `@next-state` in one step. If another
  request took the last count, keep scanning. That way two simultaneous
  requests can't both get the last "fail" response.
- `shadows()` exempts stateful routes.
- New unmatched reasons: "Answered 2 of 2 times", "Scenario order is in state
  paid; route needs started".
- `MatchInfo` gains `attempt` and state-change fields; `/routes` and `/state`
  report remaining counts and scenario states.
- `ServeReset`, mounted at `/reset` in `internal/ui/ui.go`.

**UI (`internal/ui/`)**
- Restore a **Reset state** button.
- Route details show "Answers 2 more times" and scenario conditions; the match
  summary shows "attempt 2 of 3" and "order: started → paid".
- Node tests for the formatting.

**Docs and examples**
- README: a "Stateful routes" section after "Route order"; the admin API table
  (`/reset`, new fields); the directive list. CHANGELOG and
  `docs/potential-features.md`.
- `examples/matching/retries.http` (retry, timeout then success with `@delay`,
  the order flow) with `# Run:` lines; add it to
  `examples/matching/requests.sh`.
- Smoke: `/flaky` returns 503, 503, 200; after `POST /reset` it returns 503
  again.

**Tests**
- Counting, fall-through and combining with matchers.
- A concurrent "last count" race under `-race`: exactly N requests get the
  limited route.
- Reset and reload, scenario transitions, load errors, the shadow-warning
  exemption, diagnostic wording, and `FuzzParse` coverage for the new
  directives.

## Risks and open questions

1. **Parallel test suites share state.** Two tests hitting `/flaky` at once
   interfere, as they did with rotation. Per-client state (for example
   `@state-key = header.X-Test-Id`) would fix it; defer until someone needs it.
2. **Reusing `/reset`.** It was removed in #16 with different behavior (it
   restarted rotation). Old scripts would most likely want the new behavior, so
   reuse the name and note it in the CHANGELOG.
3. **Exhausted routes in diagnostics.** List them among nearby routes, ranked
   low. "Matched but already answered twice" is the key clue when debugging a
   retry test.

Rough size: phase 1 is about the size of the absent/regex matcher change;
phase 2 adds about half that again.
