//go:build !windows

package main

import "errors"

func runOwnedComponent(componentConfig) error {
	return errors.New("windows-agent-pi-component is supported only on Windows")
}
