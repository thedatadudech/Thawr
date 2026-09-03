package wg

import (
	"context"
	"errors"
	"fmt"
)

// Open creates the interface named in opts, preferring the kernel
// implementation on Linux and falling back to wireguard-go. The
// interface is created but unconfigured until Configure is called.
func Open(ctx context.Context, opts Options) (Device, error) {
	opts = opts.withDefaults()
	if opts.Name == "" {
		return nil, fmt.Errorf("wg: interface name required")
	}
	if !opts.ForceUserspace {
		dev, err := openKernel(ctx, opts)
		switch {
		case err == nil:
			opts.Logger.Info("wireguard device ready", "backend", dev.Backend(), "interface", dev.Name())
			return dev, nil
		case errors.Is(err, ErrKernelUnavailable):
			opts.Logger.Debug("kernel wireguard unavailable, using userspace", "reason", err)
		default:
			return nil, err
		}
	}
	dev, err := openUserspace(ctx, opts)
	if err != nil {
		return nil, err
	}
	opts.Logger.Info("wireguard device ready", "backend", dev.Backend(), "interface", dev.Name())
	return dev, nil
}
