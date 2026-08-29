# Changelog

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
