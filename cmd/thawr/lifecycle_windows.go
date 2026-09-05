package main

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/sys/windows/svc"
)

// lifecycleContext ends the returned context on Ctrl-C in a console, or
// on a stop or shutdown request when running as a Windows service.
func lifecycleContext(ctx context.Context) (context.Context, func()) {
	if isService, err := svc.IsWindowsService(); err != nil || !isService {
		return signal.NotifyContext(ctx, os.Interrupt)
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		// Run returns when the handler does; a dispatcher error means the
		// control manager will not talk to us, so stop rather than hang.
		if err := svc.Run("thawr", serviceHandler{cancel: cancel}); err != nil {
			cancel()
		}
	}()
	return ctx, cancel
}

// serviceHandler reports Running and cancels the process context on
// Stop or Shutdown.
type serviceHandler struct{ cancel func() }

func (h serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for req := range requests {
		switch req.Cmd {
		case svc.Interrogate:
			status <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			h.cancel()
			return false, 0
		}
	}
	return false, 0
}
