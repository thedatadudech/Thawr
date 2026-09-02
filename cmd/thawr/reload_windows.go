package main

// notifyReload is a no-op on Windows, which has no SIGHUP; policy reload
// there uses `thawr admin policy reload` (spec 006).
func notifyReload(chan<- struct{}) (stop func()) {
	return func() {}
}
