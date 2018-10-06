ifeq ($(GOPATH),)
GOPATH := ~/go
endif

SRC=$(shell find . -name '*.go')
PKG=$(shell go list ./...)

APP_NAME=mock
APP_MAIN=cmd/main.go
BINARY=${GOPATH}/bin/${APP_NAME}

all: $(BINARY) fmt vet lint

$(BINARY): $(SRC)
	CGO_ENABLED=0 go build -o ${BINARY} ${APP_MAIN}

fmt:
	go fmt $(PKG)

vet:
	go vet $(PKG)

lint:
	@for p in $(PKG); do \
		echo "==> Linting $$p"; \
		golint $$p | { grep -vwE "exported (var|function|method|type|const) \S+ should have comment" || true; } \
	done

clean:
	rm -f ${BINARY}
