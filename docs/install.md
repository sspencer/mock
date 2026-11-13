[Documentation](README.md) › Install

# Install

Requires [Go](https://go.dev/dl/) 1.27 or newer:

```sh
go install github.com/sspencer/mock@latest
mock -version
```

Binaries for Linux, macOS, and Windows are on the
[Releases](https://github.com/sspencer/mock/releases) page. To build from a
clone, see [Building From Source](#building-from-source).

With [Homebrew](https://brew.sh) on macOS or Linux, from the project's tap:

```sh
brew install --cask sspencer/tap/mock
```

Or run the published image, for Linux on `amd64` and `arm64` (more under
[Docker](#docker)):

```sh
docker run --rm -p 8080:8080 -p 8081:8081 ghcr.io/sspencer/mock
```

In GitHub Actions, `uses: sspencer/mock@v0.6.0` installs the release binary
(see [GitHub Actions](testing.md#github-actions)).

This binary is named `mock`. If you already have another `mock` on your `PATH`,
the new one will shadow it or be shadowed, depending on install order.

## Building From Source

Common development commands:

```sh
make test
make all
make build
```

`make build` always writes the `mock` binary into `GOBIN`, or `GOPATH/bin` when
`GOBIN` is not set. `make` / `make all` does the same after tests pass, even if
Make thinks the sources are unchanged. The binary reports `git describe` via
`-version` when built this way.

After building:

```sh
mock -p 9090 -a 9091 -b 127.0.0.1 examples/user.http
```

That serves the mock API on `127.0.0.1:9090` and the request log at
`http://127.0.0.1:9091/`.

```sh
mock -version
```

A plain `go build` or `go install` from a git checkout reports a Go
pseudo-version, such as `v0.2.1-0.20261030055820-57bd1986cbf7` (with `+dirty`
appended when the tree has uncommitted changes), and `go run .` reports `dev`.
Only `make build` reports the `git describe` form, such as `v0.2.0-4-g57bd198`.

## Docker

Each release publishes `ghcr.io/sspencer/mock`, tagged with the version (such
as `0.6.0`) and `latest`. To build the image from a clone instead:

```sh
docker build -t mock .
```

The examples below use the local `mock` tag; replace it with
`ghcr.io/sspencer/mock` to use the published image.

The image exposes `8080` (mock API) and `8081` (admin UI). With no arguments
it serves the bundled `examples/user.http` on `0.0.0.0`:

```sh
docker run --rm -p 8080:8080 -p 8081:8081 mock
```

Run it with a `.http` file from your host machine. Containers must bind
`0.0.0.0` so published ports are reachable from the host (`-b` defaults to
localhost):

```sh
docker run --rm \
  -p 8080:8080 -p 8081:8081 \
  -v "$PWD/examples/user.http:/mock/user.http:ro" \
  mock -b 0.0.0.0 /mock/user.http
```

`examples/user.http` references external body files, so with only that one file
mounted the routes that use them return `500`; mount the directory as shown
below to serve them. The bind mount keeps the request file outside the image. Rebuild the image only
when the `mock` binary or static UI changes. Because the file is mounted from
the host, edits to the host `.http` file are picked up by the running container
and reloaded automatically. Docker Desktop does not always pass file change
events through a bind mount; if edits are not picked up, add `-i 1s` to poll
for changes (see [Reloading](cli.md#reloading)).

If the request file uses `< path` external response bodies, mount the whole
directory so relative file references are available inside the container:

```sh
docker run --rm \
  -p 8080:8080 -p 8081:8081 \
  -v "$PWD/examples:/mock/examples:ro" \
  mock -b 0.0.0.0 /mock/examples/user.http
```
