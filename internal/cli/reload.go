package cli

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// reloadMockFiles re-reads the input's request files, rescanning its
// directories, and swaps in the new routes. On failure the old routes stay.
func reloadMockFiles(mockServer *mockhttp.Server, input inputSource, opts restclient.Options, plan *portPlan, logger *slog.Logger, out, errOut io.Writer) {
	mockServer.BeginReload()
	load := func() ([]restclient.Method, error) { return input.load(nil, opts) }
	methods, err := load()
	if err != nil {
		time.Sleep(50 * time.Millisecond)
		methods, err = load()
	}
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	if err := validateMethods(methods, input.describe()); err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	if methods, err = plan.resolve(methods); err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	mockServer.SetMethods(methods)
	logger.Info("reloaded request files", "input", input.describe(), "methods", len(methods))
	printMethods(out, methods)
}
