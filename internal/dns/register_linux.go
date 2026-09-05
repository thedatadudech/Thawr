package dns

func newPlatformRegistrar(o RegistrarOptions) Registrar { return newLinuxRegistrar(o) }
