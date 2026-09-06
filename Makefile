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

.PHONY: all build fmt fmt-check vet update clean mod test test-ui verify vulncheck docker run lint dockerize

all: verify build

verify: fmt-check vet test test-ui

fmt-check:
	@files="$$(git ls-files '*.go' | xargs gofmt -l)"; if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

test-ui:
	node --test tests/*.test.mjs

# Pin the scanner independently of runtime dependencies.
vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

# Always rewrite $(BINARY). Make must not skip install when sources look current
# (embedded static files, go.mod, or an already-installed binary).
build:
	mkdir -p "$(GOBIN)"
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" $(APP_MAIN)

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
	rm -f fake
	rm -f server


mod:
	$(GO) mod tidy

test:
	$(GO) test ./...

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

run:
	docker run --rm -p 7777:8080 $(DOCKER_IMAGE)

lint:
	golangci-lint run --config=~/.golangci.yaml ./...

dockerize: mod docker
