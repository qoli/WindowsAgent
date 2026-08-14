// Package puredecision executes permission-free Starlark decision packages in
// process for bounded runtime-internal composition.
package puredecision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/scriptrunner"
)

type deniedBroker struct{}

func (deniedBroker) Call(context.Context, string, string, map[string]any) (any, error) {
	return nil, errors.New("pure decision Actions cannot call the Observer")
}

func (deniedBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("pure decision Actions cannot resolve blobs")
}

func (deniedBroker) RecordNative(context.Context, scriptrunner.NativeRecord) error {
	return errors.New("pure decision Actions cannot call native libraries")
}

func Run(ctx context.Context, pkg *scriptpackage.Package, inputs map[string]any) (json.RawMessage, error) {
	if pkg == nil {
		return nil, errors.New("pure decision package is required")
	}
	permissions := pkg.Manifest.Permissions
	if permissions.Memory != nil || permissions.File != nil || permissions.Screen != nil || len(pkg.Manifest.NativeLibraries) != 0 {
		return nil, errors.New("pure decision package must not declare permissions or native libraries")
	}
	runner, err := scriptrunner.New(deniedBroker{})
	if err != nil {
		return nil, fmt.Errorf("initialize pure decision runner: %w", err)
	}
	output, err := runner.Run(ctx, pkg, inputs)
	if err != nil {
		return nil, fmt.Errorf("run pure decision package: %w", err)
	}
	return append(json.RawMessage(nil), output...), nil
}
