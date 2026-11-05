// Command mock serves REST Client .http files as a local HTTP mock server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sspencer/mock/internal/cli"
)

// version is reported by -version. Override at link time:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	cli.Version = version
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, logger)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(cli.ExitCode(err))
	}
}
