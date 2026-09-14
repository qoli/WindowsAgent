//go:build !windows

package processinventory

import (
	"context"
	"errors"
)

type osSource struct{}

func (osSource) Processes(context.Context) ([]Process, error) {
	return nil, errors.New("process inventory is supported only on Windows")
}

func (osSource) Services(context.Context) ([]Service, error) {
	return nil, errors.New("process inventory is supported only on Windows")
}
