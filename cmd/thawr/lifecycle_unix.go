//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// lifecycleContext ends the returned context on SIGINT or SIGTERM.
func lifecycleContext(ctx context.Context) (context.Context, func()) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}
