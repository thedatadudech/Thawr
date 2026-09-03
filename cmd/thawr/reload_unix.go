//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyReload forwards SIGHUP as reload requests until the returned
// stop function is called.
func notifyReload(reload chan<- struct{}) (stop func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sigs:
				select {
				case reload <- struct{}{}:
				default: // a reload is already pending
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(done)
	}
}
