//go:build !windows

package main

import "os"

// isRoot reports whether the process runs with root privileges.
func isRoot() bool { return os.Geteuid() == 0 }
