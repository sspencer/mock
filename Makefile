GO           ?= go
APP_NAME     := mock
APP_MAIN     := .
PKG          := ./...
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS      := -s -w -X main.version=$(VERSION)
DOCKER_IMAGE := $(APP_NAME)

BUILD_DIR     := build
BUILD_BIN     := $(BUILD_DIR)/$(APP_NAME)
RELEASE_FILES := $(BUILD_DIR)/release
DOCS_VENV     := $(BUILD_DIR)/docs-venv

GOBIN ?= $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif
INSTALL_BIN := $(GOBIN)/$(APP_NAME)

# Man page and shell completion install paths (override PREFIX or any *DIR).
PREFIX              ?= $(shell command -v brew >/dev/null 2>&1 && brew --prefix || echo /usr/local)
MANDIR              ?= $(PREFIX)/share/man/man1
BASH_COMPLETION_DIR ?= $(PREFIX)/etc/bash_completion.d
ZSH_COMPLETION_DIR  ?= $(PREFIX)/share/zsh/site-functions
FISH_COMPLETION_DIR ?= $(PREFIX)/share/fish/vendor_completions.d

.DEFAULT_GOAL := all

.PHONY: all help \
	build install install-man install-completions install-completions-bash install-completions-zsh install-completions-fish install-all uninstall clean \
	fmt fmt-check vet lint vulncheck \
	test test-race test-cover test-ui test-browser verify check bench fuzz \
	mod update \
	docs docs-serve \
	release-files docker run dockerize dev

##@ Default & Help

all: vet build install ## Vet, build, and install mock (default)

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*## "} \
		/^##@ / { printf "\n%s:\n", substr($$0, 5); next } \
		/^[a-zA-Z0-9_-]+:.*## / { printf "  %-24s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

##@ Build & Install

# Always rewrite $(BUILD_BIN) so embedded static files and ldflags stay fresh.
build: ## Build the mock binary into build/mock
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BUILD_BIN)" $(APP_MAIN)

install: build ## Install mock into GOBIN (or GOPATH/bin)
	install -d "$(GOBIN)"
	install -m 755 "$(BUILD_BIN)" "$(INSTALL_BIN)"

install-man: build ## Generate and install the mock(1) man page into MANDIR
	install -d "$(MANDIR)"
	"$(BUILD_BIN)" man > "$(BUILD_DIR)/$(APP_NAME).1"
	install -m 644 "$(BUILD_DIR)/$(APP_NAME).1" "$(MANDIR)/$(APP_NAME).1"

install-completions-bash: build ## Install bash completion into BASH_COMPLETION_DIR
	install -d "$(BASH_COMPLETION_DIR)"
	"$(BUILD_BIN)" completion bash > "$(BUILD_DIR)/$(APP_NAME).bash"
	install -m 644 "$(BUILD_DIR)/$(APP_NAME).bash" "$(BASH_COMPLETION_DIR)/$(APP_NAME)"

install-completions-zsh: build ## Install zsh completion into ZSH_COMPLETION_DIR
	install -d "$(ZSH_COMPLETION_DIR)"
	"$(BUILD_BIN)" completion zsh > "$(BUILD_DIR)/_$(APP_NAME)"
	install -m 644 "$(BUILD_DIR)/_$(APP_NAME)" "$(ZSH_COMPLETION_DIR)/_$(APP_NAME)"

install-completions-fish: build ## Install fish completion into FISH_COMPLETION_DIR
	install -d "$(FISH_COMPLETION_DIR)"
	"$(BUILD_BIN)" completion fish > "$(BUILD_DIR)/$(APP_NAME).fish"
	install -m 644 "$(BUILD_DIR)/$(APP_NAME).fish" "$(FISH_COMPLETION_DIR)/$(APP_NAME).fish"

install-completions: install-completions-bash install-completions-zsh install-completions-fish ## Install bash, zsh, and fish completions

install-all: install install-man install-completions ## Install mock binary, man page, and shell completions

uninstall: ## Remove installed binary, man page, and shell completions
	rm -f "$(INSTALL_BIN)" \
		"$(MANDIR)/$(APP_NAME).1" \
		"$(BASH_COMPLETION_DIR)/$(APP_NAME)" \
		"$(ZSH_COMPLETION_DIR)/_$(APP_NAME)" \
		"$(FISH_COMPLETION_DIR)/$(APP_NAME).fish"

clean: ## Remove built binaries, coverage reports, and generated site/release files
	rm -rf "$(BUILD_BIN)" "$(BUILD_DIR)/$(APP_NAME).1" \
		"$(BUILD_DIR)/$(APP_NAME).bash" "$(BUILD_DIR)/_$(APP_NAME)" "$(BUILD_DIR)/$(APP_NAME).fish" \
		"$(BUILD_DIR)/coverage.out" "$(BUILD_DIR)/site" "$(RELEASE_FILES)" "$(INSTALL_BIN)"

##@ Quality & Formatting

fmt: ## Format Go source files in place
	$(GO) fmt $(PKG)

fmt-check: ## Check Go formatting without modifying files
	@files="$$(git ls-files --cached --others --exclude-standard '*.go' | xargs gofmt -l)"; if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

vet: ## Run go vet
	$(GO) vet $(PKG)

lint: ## Run golangci-lint
	golangci-lint run --config=~/.golangci.yaml $(PKG)

# Pin the scanner independently of runtime dependencies.
vulncheck: ## Run govulncheck for known dependency vulnerabilities
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 $(PKG)

##@ Testing & Benchmarks

test: ## Run Go tests
	$(GO) test $(PKG)

test-race: ## Run Go tests with the race detector
	$(GO) test -race -count=1 $(PKG)

test-cover: ## Run Go tests and report total statement coverage
	mkdir -p "$(BUILD_DIR)"
	$(GO) test -coverprofile="$(BUILD_DIR)/coverage.out" $(PKG)
	$(GO) tool cover -func="$(BUILD_DIR)/coverage.out" | tail -n 1

test-ui: ## Run admin UI JavaScript unit tests (Node 22+)
	node --test internal/ui/tests/*.test.mjs

# Admin UI layout, theming and input in headless Chrome against a built mock.
# Not part of verify: it needs Chrome or Chromium (skipped when none is found;
# set CHROME_BIN to choose one).
test-browser: ## Run admin UI tests in headless Chrome
	node --test internal/ui/browser/*.test.mjs

verify: fmt-check vet test test-ui ## Run format check, vet, Go tests, and UI tests (CI)

check: verify test-race ## Run full local pre-push check (verify + race tests)

# Matching and rendering benchmarks (a few thousand routes) plus the parser.
# Compare runs with benchstat to spot regressions.
bench: ## Run matching, rendering, and parser benchmarks
	$(GO) test ./mockhttp ./restclient -run '^$$' -bench . -benchmem

fuzz: ## Run short fuzz tests on parser and path matcher
	$(GO) test ./restclient -run '^$$' -fuzz FuzzParse -fuzztime 10s
	$(GO) test ./mockhttp -run '^$$' -fuzz FuzzMatchEscapedPath -fuzztime 10s

##@ Dependencies

mod: ## Tidy go.mod and go.sum
	$(GO) mod tidy

update: ## Update Go module dependencies and tidy
	@echo updating go.mod packages
	$(GO) get -u -v $(PKG)
	$(GO) mod tidy

##@ Documentation

# The documentation site (mkdocs.yml): docs/ rendered with MkDocs Material
# into build/site. Strict mode fails on broken links and anchors. The tools
# live in a virtualenv under build/, so nothing is installed globally; needs
# Python 3.
$(DOCS_VENV)/.installed: docs-site/requirements.txt
	python3 -m venv $(DOCS_VENV)
	$(DOCS_VENV)/bin/pip install -q -r docs-site/requirements.txt
	touch $@

docs: $(DOCS_VENV)/.installed ## Build the MkDocs site in strict mode
	NO_MKDOCS_2_WARNING=1 $(DOCS_VENV)/bin/mkdocs build --strict

docs-serve: $(DOCS_VENV)/.installed ## Serve the MkDocs site locally with live reload
	NO_MKDOCS_2_WARNING=1 $(DOCS_VENV)/bin/mkdocs serve

##@ Release, Docker & Dev

# Shell completions and the man page for the release archives, generated
# from the usage text. GoReleaser runs this before building.
release-files: ## Generate completions and mock.1 into build/release (GoReleaser hook)
	rm -rf "$(RELEASE_FILES)"
	mkdir -p "$(RELEASE_FILES)/completions" "$(RELEASE_FILES)/man"
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o "$(RELEASE_FILES)/$(APP_NAME)" $(APP_MAIN)
	"$(RELEASE_FILES)/$(APP_NAME)" completion bash > "$(RELEASE_FILES)/completions/$(APP_NAME).bash"
	"$(RELEASE_FILES)/$(APP_NAME)" completion zsh > "$(RELEASE_FILES)/completions/_$(APP_NAME)"
	"$(RELEASE_FILES)/$(APP_NAME)" completion fish > "$(RELEASE_FILES)/completions/$(APP_NAME).fish"
	"$(RELEASE_FILES)/$(APP_NAME)" completion powershell > "$(RELEASE_FILES)/completions/$(APP_NAME).ps1"
	"$(RELEASE_FILES)/$(APP_NAME)" man > "$(RELEASE_FILES)/man/$(APP_NAME).1"
	rm "$(RELEASE_FILES)/$(APP_NAME)"

docker: ## Build the Docker image
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

run: ## Run the Docker image on port 7777
	docker run --rm -p 7777:8080 $(DOCKER_IMAGE)

dockerize: mod docker ## Tidy modules and build the Docker image

dev: ## Run mock locally with examples/user.http
	$(GO) run $(APP_MAIN) serve examples/user.http
