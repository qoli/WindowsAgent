//go:build !windows

package observationlauncher

import (
	"errors"
	"io"
)

type Limits struct {
	ActiveProcesses    uint32
	ProcessMemoryBytes uintptr
	JobMemoryBytes     uintptr
}

type Group struct{}
type Child struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

func NewGroup(Limits) (*Group, error) {
	return nil, errors.New("observation process launching is supported only on Windows")
}

func (g *Group) Start(string, ...string) (*Child, error) {
	return nil, errors.New("observation process launching is supported only on Windows")
}

func (g *Group) Close() error {
	return nil
}

func (c *Child) Wait() (uint32, error) {
	return 0, errors.New("observation process launching is supported only on Windows")
}
