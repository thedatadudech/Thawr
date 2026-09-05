package dns

func newPlatformRegistrar(o RegistrarOptions) Registrar { return &nrpt{opts: o} }
