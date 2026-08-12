//go:build !windows

package wgcworker

import (
	"context"
	"errors"
	"log/slog"
)

func startWorkerProcess(context.Context, string, bool, *slog.Logger) (workerClient, error) {
	return nil, errors.New("persistent WGC worker is only available on Windows")
}
