FROM golang:1.26-alpine3.23 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/mock .

FROM alpine:3.23

WORKDIR /app
COPY --from=builder /out/mock /usr/local/bin/mock
COPY --from=builder /src/examples ./examples

EXPOSE 8080
ENTRYPOINT ["mock"]
CMD ["examples/user.http"]
