//go:build !windows

package windowsexec

import (
	"context"
	"errors"
)

func (OSExecutor) Execute(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, &Error{Code: "EXEC_CONTEXT_REQUIRED", Stage: "validating-request", Cause: errors.New("execution context is required")}
	}
	if _, err := validateRequest(request); err != nil {
		return Result{}, err
	}
	return Result{}, &Error{Code: "EXEC_RUNTIME_UNSUPPORTED", Stage: "starting-process", Cause: errors.New("windows-exec-v1 is supported only on Windows")}
}
