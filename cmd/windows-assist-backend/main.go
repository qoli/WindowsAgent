package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/qoli/WindowsAgent/internal/assistbackend"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if helper, err := assistbackend.RunElevatedHelper(ctx, os.Args[1:]); helper {
		assistbackend.ShowHelperCompletion(os.Args[1:], err)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	request, err := assistbackend.DecodeRequest(os.Stdin)
	emitter := assistbackend.NewEmitter(os.Stdout, request)
	if err != nil {
		_ = assistbackend.EmitRequestError(emitter, fmt.Errorf("invalid request: %w", err))
		os.Exit(2)
	}
	if err := assistbackend.Execute(ctx, request, emitter); err != nil {
		_ = assistbackend.EmitError(emitter, err)
		os.Exit(1)
	}
}
