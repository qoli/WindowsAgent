//go:build !windows

package wgcworker

import (
	"errors"
	"log/slog"
)

func startWorkerProcess(string, bool, *slog.Logger) (workerClient, error) {
	return nil, errors.New("persistent WGC worker is only available on Windows")
}
