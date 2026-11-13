GO ?= go
APP_NAME := mock
APP_MAIN := .
DOCKER_IMAGE := $(APP_NAME)
PKG := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBIN ?= $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif
BINARY := $(GOBIN)/$(APP_NAME)

.PHONY: all build fmt fmt-check vet update clean mod test test-ui test-browser bench verify vulncheck docker run lint dockerize docs docs-serve release-files

all: verify build

verify: fmt-check vet test test-ui

fmt-check:
	@files="$$(git ls-files --cached --others --exclude-standard '*.go' | xargs gofmt -l)"; if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

test-ui:
	node --test internal/ui/tests/*.test.mjs

# Admin UI layout, theming and input in headless Chrome against a built mock.
# Not part of verify: it needs Chrome or Chromium (skipped when none is found;
# set CHROME_BIN to choose one).
test-browser:
	node --test internal/ui/browser/*.test.mjs

# The documentation site (mkdocs.yml): docs/ rendered with MkDocs Material
# into build/site. Strict mode fails on broken links and anchors. The tools
# live in a virtualenv under build/, so nothing is installed globally; needs
# Python 3.
DOCS_VENV := build/docs-venv
$(DOCS_VENV)/.installed: docs-site/requirements.txt
	python3 -m venv $(DOCS_VENV)
	$(DOCS_VENV)/bin/pip install -q -r docs-site/requirements.txt
	touch $@

docs: $(DOCS_VENV)/.installed
	NO_MKDOCS_2_WARNING=1 $(DOCS_VENV)/bin/mkdocs build --strict

docs-serve: $(DOCS_VENV)/.installed
	NO_MKDOCS_2_WARNING=1 $(DOCS_VENV)/bin/mkdocs serve

# Pin the scanner independently of runtime dependencies.
vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

# Always rewrite $(BINARY). Make must not skip install when sources look current
# (embedded static files, go.mod, or an already-installed binary).
build:
	mkdir -p "$(GOBIN)"
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" $(APP_MAIN)

# Shell completions and the man page for the release archives, generated
# from the usage text. GoReleaser runs this before building.
RELEASE_FILES := build/release
release-files:
	rm -rf "$(RELEASE_FILES)"
	mkdir -p "$(RELEASE_FILES)/completions" "$(RELEASE_FILES)/man"
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o "$(RELEASE_FILES)/mock" $(APP_MAIN)
	"$(RELEASE_FILES)/mock" completion bash > "$(RELEASE_FILES)/completions/mock.bash"
	"$(RELEASE_FILES)/mock" completion zsh > "$(RELEASE_FILES)/completions/_mock"
	"$(RELEASE_FILES)/mock" completion fish > "$(RELEASE_FILES)/completions/mock.fish"
	"$(RELEASE_FILES)/mock" completion powershell > "$(RELEASE_FILES)/completions/mock.ps1"
	"$(RELEASE_FILES)/mock" man > "$(RELEASE_FILES)/man/mock.1"
	rm "$(RELEASE_FILES)/mock"

fmt:
	$(GO) fmt $(PKG)

vet:
	$(GO) vet $(PKG)

update:
	@echo updating go.mod packages
	$(GO) get -u -v ./...
	$(GO) mod tidy

clean:
	rm -f $(BINARY)

mod:
	$(GO) mod tidy

test:
	$(GO) test ./...

# Matching and rendering benchmarks (a few thousand routes) plus the parser.
# Compare runs with benchstat to spot regressions.
bench:
	$(GO) test ./mockhttp ./restclient -run '^$$' -bench . -benchmem

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

run:
	docker run --rm -p 7777:8080 $(DOCKER_IMAGE)

lint:
	golangci-lint run --config=~/.golangci.yaml ./...

dockerize: mod docker
