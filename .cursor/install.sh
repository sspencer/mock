#!/usr/bin/env bash
# Cloud Agent environment bootstrap for the `mock` Go project.
# Idempotent: safe to run repeatedly against cached or partially prepared state.
set -euo pipefail

# go.mod requires Go 1.26; the base image ships an older toolchain, so install a
# pinned 1.26 patch release into /usr/local and expose it ahead of any system Go.
GO_VERSION="1.26.8"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"

if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go${GO_VERSION} "; then
  echo "Installing Go ${GO_VERSION}..."
  curl -fsSL -o "/tmp/${GO_TARBALL}" "https://go.dev/dl/${GO_TARBALL}"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "/tmp/${GO_TARBALL}"
  rm -f "/tmp/${GO_TARBALL}"
fi

# /usr/local/bin precedes /usr/bin on PATH, so these symlinks make `go` resolve
# to 1.26 in every shell without editing shell profiles.
sudo ln -sf /usr/local/go/bin/go /usr/local/bin/go
sudo ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

go version

# Repository bootstrap: fetch modules, install the binary, and run the suite so
# a broken environment fails setup instead of surfacing later.
go mod download
make build
go test ./...
