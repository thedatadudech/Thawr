package svc

// New returns the systemd manager.
func New(opts Options) (Manager, error) { return newSystemd(opts), nil }
