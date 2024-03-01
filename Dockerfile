# builder image
FROM golang:1.22-alpine3.19 as builder
RUN mkdir /build
RUN mkdir /build/cmd
WORKDIR /build

#RUN go get -d -v
#COPY go.mod .
#COPY go.sum .
#RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -o golang-mock cmd/main.go

# generate clean, final image for end users
FROM alpine:3.19
COPY --from=builder /build/golang-mock .
COPY --from=builder /build/examples/ .

# executable
ENTRYPOINT [ "./golang-mock" ]
# arguments that can be overridden
CMD ["user.api"]

# docker build -t test .
# docker run -p 7777:8080 test