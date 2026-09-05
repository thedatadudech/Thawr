package client

import (
	"fmt"
	"os"
)

// secureSocket sets the socket mode; Windows has no thawr group to hand
// it to.
func secureSocket(path string) error {
	if err := os.Chmod(path, 0o660); err != nil { //nolint:gosec // group access is intended
		return fmt.Errorf("client: chmod socket: %w", err)
	}
	return nil
}
