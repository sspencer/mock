# mock documentation

New here? Start with the [project README](../README.md), then
[Getting started](getting-started.md).

## Guides

- [Getting started](getting-started.md): `mock init`, the example APIs, and scripts that fill the admin UI.
- [Install](install.md): Go install, release binaries, building from source, Docker.
- [Recipes](recipes.md): GraphQL, content negotiation, per-test responses, readiness checks.
- [Example gallery](examples.md): ready-made mocks for OAuth, pagination, uploads, webhooks and payments.

## Writing routes

- [Request files](request-files.md): the `.http` dialect, `< file` bodies, variables, and the [directive reference](request-files.md#directive-reference).
- [Placeholders and generated data](placeholders.md): `{{$...}}` values, filters, fixtures, generated lists, seeds.
- [Matching, route order and ports](matching.md): header, query, path, cookie and body matchers, which route wins, multiple ports.
- [Delays, faults and callbacks](responses.md): latency, streaming, faults, rate limits, compression, webhook callbacks.

## Running mock

- [Command line](cli.md): flags, JSON logs, reloading, directories of request files, `mock validate`.
- [Working with real APIs](real-apis.md): `--upstream`, recording, `@proxy`, `mock check`, `mock import`.
- [Testing with mock](testing.md): strict mode, route coverage, the `mocktest` Go package.
- [Admin UI and API](admin-ui.md): the UI, access token, credential redaction, admin endpoints, clients and safety.
- [Troubleshooting](troubleshooting.md): unmatched requests, near-miss reasons, shadowed routes, busy ports, failed reloads.

## Design notes

These describe ideas, not shipped behavior.

- [Ideas](ideas.md)
