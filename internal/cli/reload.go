package cli

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func reloadMockFiles(mockServer *mockhttp.Server, files []string, plan *portPlan, logger *slog.Logger, out, errOut io.Writer) {
	mockServer.BeginReload()
	load := func() ([]restclient.Method, error) {
		if len(files) == 0 {
			return nil, nil
		}
		return restclient.Load(files)
	}
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
	if err := validateMethods(methods, files); err != nil {
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
	logger.Info("reloaded request files", "files", files, "methods", len(methods))
	printMethods(out, methods)
}
