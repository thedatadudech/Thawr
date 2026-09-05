//go:build !linux && !darwin && !windows

package dns

func newPlatformRegistrar(RegistrarOptions) Registrar { return unsupported{} }
