//go:build !windows

package observer

import "errors"

type MemoryBackend struct{}

func NewMemoryBackend(ProcessIdentity, uint64) (*MemoryBackend, error) {
	return nil, errors.New("live process-memory observation is supported only on Windows")
}
