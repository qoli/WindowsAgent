//go:build windows && amd64

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/qoli/WindowsAgent/internal/wgcworker"
)

func main() {
	parentPID := flag.Int("parent-pid", 0, "owning Windows Capture Agent process ID")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "[FATAL] positional arguments are forbidden")
		os.Exit(1)
	}
	if err := wgcworker.StartParentLifetimeGuard(*parentPID); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With(
		"component", "persistent_wgc_worker",
		"process_id", os.Getpid(),
		"parent_process_id", *parentPID,
	)
	if err := wgcworker.Serve(os.Stdin, os.Stdout, logger); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}
