//go:build !linux

package wg

import "context"

// openKernel reports that only Linux has an in-kernel WireGuard adapter.
func openKernel(context.Context, Options) (Device, error) {
	return nil, ErrKernelUnavailable
}
