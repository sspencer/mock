GO ?= go
APP_NAME := mock
APP_MAIN := .
DOCKER_IMAGE := $(APP_NAME)
PKG := ./...
SRC := $(shell find . -name '*.go' -not -path './.git/*')
GOBIN ?= $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif
BINARY := $(GOBIN)/$(APP_NAME)

.PHONY: all build fmt vet update clean mod test docker run lint dockerize

all: fmt vet test build

build: $(BINARY)

$(BINARY): $(SRC)
	CGO_ENABLED=0 $(GO) build -o $(BINARY) $(APP_MAIN)

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
	docker build -t $(DOCKER_IMAGE) .

run:
	docker run --rm -p 7777:8080 $(DOCKER_IMAGE)

lint:
	golangci-lint run --config=~/.golangci.yaml ./...

dockerize: mod docker
