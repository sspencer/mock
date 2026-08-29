# Contributing

Thanks for looking at `mock`. Small, focused changes are welcome.

## Setup

Go 1.26 or newer is required (`go.mod`).

```sh
git clone https://github.com/sspencer/mock.git
cd mock
make all
```

`make all` formats, vets, tests, and installs the `mock` binary into `GOBIN`
(or `GOPATH/bin`). `make test` is enough if you only want the suite.

```sh
go run . examples/user.http
```

The request log is at <http://localhost:8080/mock/>. Example files live in
`examples/`.

## Pull requests

- Keep the change scoped to one concern.
- Add or update tests when behavior changes.
- Run `make all` before you push.
- Match the surrounding code style. `go fmt` is required; `make all` runs it.

CI on `master` and on pull requests runs `make all` and `go test -race`.

## Releases

Tagged versions (`vX.Y.Z`) are published by GoReleaser. `-version` reports the
tag for release binaries and the module version for `go install`. See
`CHANGELOG.md` when cutting a release.
