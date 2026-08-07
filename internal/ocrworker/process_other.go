//go:build !windows

package ocrworker

import "os/exec"

func configureWorkerCommand(_ *exec.Cmd) {}
