//go:build !linux && !darwin && !windows

package svc

import (
	"fmt"
	"runtime"
)

// New reports that this platform has no service manager adapter.
func New(Options) (Manager, error) {
	return nil, fmt.Errorf("%w (%s)", ErrUnsupported, runtime.GOOS)
}
