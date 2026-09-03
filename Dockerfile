FROM golang:1.26-alpine3.23 AS builder

ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/mock .

FROM alpine:3.23

WORKDIR /app
COPY --from=builder /out/mock /usr/local/bin/mock
COPY --from=builder /src/examples ./examples

USER nobody

EXPOSE 8080
ENTRYPOINT ["mock"]
CMD ["-b", "0.0.0.0", "examples/user.http"]
