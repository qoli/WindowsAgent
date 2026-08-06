package observer

import (
	"context"
	"fmt"
	"io"
)

type RouterBackend struct {
	Memory Backend
	File   Backend
	Screen Backend
}

func (r RouterBackend) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (BackendResult, error) {
	switch namespace {
	case "memory":
		if r.Memory == nil {
			return BackendResult{}, fmt.Errorf("memory namespace is not initialized")
		}
		return r.Memory.Call(ctx, namespace, operation, arguments)
	case "file":
		if r.File == nil {
			return BackendResult{}, fmt.Errorf("file namespace is not initialized")
		}
		return r.File.Call(ctx, namespace, operation, arguments)
	case "screen":
		if r.Screen == nil {
			return BackendResult{}, fmt.Errorf("screen namespace is not initialized")
		}
		return r.Screen.Call(ctx, namespace, operation, arguments)
	default:
		return BackendResult{}, fmt.Errorf("unsupported namespace %q", namespace)
	}
}

func (r RouterBackend) Estimate(namespace, operation string, arguments map[string]any) (uint64, uint64, error) {
	var backend Backend
	switch namespace {
	case "memory":
		backend = r.Memory
	case "file":
		backend = r.File
	case "screen":
		// ScreenBackend validates MaxPixels before capturing and reports exact
		// pixel accounting after the call. It does not consume memory/file budgets.
		return 0, 0, nil
	default:
		return 0, 0, fmt.Errorf("unsupported namespace %q", namespace)
	}
	if backend == nil {
		return 0, 0, fmt.Errorf("%s namespace is not initialized", namespace)
	}
	estimator, ok := backend.(ByteEstimator)
	if !ok {
		return 0, 0, fmt.Errorf("%s backend cannot pre-authorize byte usage", namespace)
	}
	return estimator.Estimate(namespace, operation, arguments)
}

func (r RouterBackend) Close() error {
	var first error
	for _, backend := range []Backend{r.Memory, r.File, r.Screen} {
		closer, ok := backend.(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
