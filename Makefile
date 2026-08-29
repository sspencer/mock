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

.PHONY: all build fmt vet update clean mod test docker run lint dockerize

all: fmt vet test build

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
