---
name: smoke
description: End-to-end smoke test of the real mock binary. Builds it, serves the examples, and checks mock routes, admin endpoints, multi-port routing, and hot reload with curl. Use after changes to serving, matching, ports, reload, or the admin API, or before a release, when unit tests alone aren't enough.
---

# Smoke test

Run the script from the repo root:

```sh
.claude/skills/smoke/smoke.sh
```

What it does:

- Builds to a temp dir with `-X main.version=smoke`.
- Checks `-version` and runs `mock validate examples/*.http examples/matching/*.http`.
- Serves `examples/user.http` and `examples/static.http` on `18080`, with admin on `18081`. Checks:
  - an external file body
  - HTML
  - a query matcher
  - 201/409/400 by JSON body matching, with a JSON field echoed back
  - path params, with a regex matcher
  - a 404 for an unmatched route
  - the admin page (`id="mock-config"`)
  - `/state` and `/routes`
  - CSRF rejection on `/clear`, and a 204 from `/clear`
- Serves a copy of `examples/services.http` with ports shifted to `18090`–`18092` and admin on `18093`. Checks:
  - per-port routing, including `@port`
  - that a route is not served on other ports
  - that a hot reload adds a route
  - that a reload needing an unopened port is rejected while the old routes keep serving

It prints `ok`/`FAIL` per check. On the first failure it dumps the server logs and exits 1. Servers and temp files are cleaned up on exit.

## Rules

- Ports `18080`–`18199` must be free. If a bind fails, find the holder with `lsof -nP -iTCP:<port> -sTCP:LISTEN`. Don't kill processes you didn't start.
- A failure is a real finding. Investigate the server log, then fix the code or report the failure. Never weaken a check to make it pass.
- When behavior changes on purpose (status codes, admin responses, example files), update the matching check in `smoke.sh` in the same change, and say so.
- When you add a user-visible feature to serving or admin, consider adding a check for it.
- Report the pass count, or the failing check plus the relevant log lines.
