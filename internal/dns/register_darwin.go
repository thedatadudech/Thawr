package dns

func newPlatformRegistrar(o RegistrarOptions) Registrar { return &resolverFile{opts: o} }
