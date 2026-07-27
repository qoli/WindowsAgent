//go:build !windows

package observationlauncher

import "errors"

type Limits struct {
	ActiveProcesses    uint32
	ProcessMemoryBytes uintptr
	JobMemoryBytes     uintptr
}

type Group struct{}
type Child struct{}

func NewGroup(Limits) (*Group, error) {
	return nil, errors.New("observation process launching is supported only on Windows")
}
