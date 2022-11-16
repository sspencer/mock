ifeq ($(GOPATH),)
GOPATH := ~/go
endif

SRC=$(shell find . -name '*.go')
PKG=$(shell go list ./...)

APP_NAME=mock
APP_MAIN=./cmd/server
BINARY=${GOPATH}/bin/${APP_NAME}

all: fmt vet $(BINARY)

$(BINARY): $(SRC)
	CGO_ENABLED=0 go build -o ${BINARY} ${APP_MAIN}

fmt:
	go fmt $(PKG)

vet:
	go vet $(PKG)

update:
	@echo updating go.mod packages
	go get -u -v ./...
	go mod tidy

clean:
	rm -f $(BINARY)

mod:
	go mod tidy
	go mod vendor

test:
	go test ./...

docker:
	docker build -t test .

run:
	docker run -p 7777:8080 test

dockerize: mod docker