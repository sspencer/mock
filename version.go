package main

// version is reported by -version. Override at link time:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "dev"
