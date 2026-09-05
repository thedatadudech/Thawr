package main

import "golang.org/x/sys/windows"

// isRoot reports whether the process runs elevated (as Administrator).
func isRoot() bool { return windows.GetCurrentProcessToken().IsElevated() }
