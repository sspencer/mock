package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// reloadOnHangup reloads the request files on each SIGHUP until ctx is done.
func reloadOnHangup(ctx context.Context, reload func(), logger *slog.Logger) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	defer signal.Stop(signals)
	reloadOnSignal(ctx, signals, reload, logger)
}

func reloadOnSignal(ctx context.Context, signals <-chan os.Signal, reload func(), logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-signals:
			logger.Info("reloading request files", "signal", sig.String())
			reload()
		}
	}
}
