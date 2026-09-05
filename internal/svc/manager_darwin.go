package svc

// New returns the launchd manager.
func New(opts Options) (Manager, error) { return newLaunchd(opts), nil }
