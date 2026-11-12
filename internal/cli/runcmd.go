package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"
)

const runUsage = "usage: mock run [flags] <file.http|-d dir> [file.http...] -- command [args...]"

// runRun serves the request files on free ports, runs a command with
// MOCK_URL (and MOCK_ADMIN_URL) set, then stops and prints the strict report.
// It exits with the command's status, or 1 when the command succeeded but
// requests went unmatched.
func runRun(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	split := slices.Index(args, "--")
	if split < 0 || split == len(args)-1 {
		return usageError("missing the command to run after --\n%s", runUsage)
	}
	flags, command := args[:split], args[split+1:]
	// Free ports unless -p or -a say otherwise: later flags win.
	cfg, err := parseConfig(append([]string{"-p", "0", "-a", "0"}, flags...))
	if err != nil {
		return err
	}
	if cfg.Help != "" {
		fmt.Fprintln(stdout, runUsage)
		fmt.Fprint(stdout, "\nThe flags are mock's own (see mock -h); -s is always on.\n")
		return nil
	}
	if len(cfg.Args) == 0 && len(cfg.Dirs) == 0 {
		return usageError("mock run needs request files or -d directories\n%s", runUsage)
	}
	cfg.Strict, cfg.quiet = true, true
	var handler slog.Handler = slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn})
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn})
		cfg.LogFormat = "text" // Keep stdout for the command.
	}
	logger := slog.New(handler)

	serveCtx, stopServing := context.WithCancel(ctx)
	defer stopServing()
	var (
		mu       sync.Mutex
		cmd      *exec.Cmd
		startErr error
		done     = make(chan struct{})
	)
	cfg.onReady = func(ready serving) {
		fmt.Fprintf(stderr, "mock: serving %s at %s", plural(uint64(ready.Routes), "route"), ready.MockURL)
		if ready.AdminURL != "" {
			fmt.Fprintf(stderr, " (admin UI %s)", ready.AdminURL)
		}
		fmt.Fprintln(stderr)
		c := exec.CommandContext(ctx, command[0], command[1:]...)
		c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
		c.Env = append(os.Environ(), "MOCK_URL="+ready.MockURL)
		if ready.AdminURL != "" {
			c.Env = append(c.Env, "MOCK_ADMIN_URL="+ready.AdminURL)
		}
		// On Ctrl-C, ask the command to stop before killing it.
		c.Cancel = func() error { return c.Process.Signal(os.Interrupt) }
		c.WaitDelay = 5 * time.Second
		mu.Lock()
		cmd = c
		startErr = c.Start()
		mu.Unlock()
		if startErr != nil {
			close(done)
			stopServing()
			return
		}
		go func() {
			_ = c.Wait()
			close(done)
			stopServing()
		}()
	}
	serveErr := serve(serveCtx, cfg, stdin, stdout, stderr, logger)

	mu.Lock()
	started, failed := cmd != nil, startErr
	mu.Unlock()
	if !started {
		return serveErr // The server never came up.
	}
	if failed != nil {
		return runError("mock run: cannot run %s: %v", command[0], failed)
	}
	select {
	case <-done:
	default:
		// The server stopped first (it failed, or Ctrl-C): stop the command too.
		_ = cmd.Process.Signal(os.Interrupt)
		<-done
	}
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		if code < 0 {
			code = 1 // Killed by a signal.
		}
		return &exitError{code: code, msg: fmt.Sprintf("mock run: %s exited with status %d", command[0], code)}
	}
	return serveErr
}
