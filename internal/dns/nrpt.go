package dns

import (
	"context"
	"fmt"
	"net/netip"
)

// nrptComment tags the rules this registrar owns.
const nrptComment = "thawr"

// nrpt registers the zone as a Name Resolution Policy Table rule on
// Windows, the equivalent of a routing domain.
type nrpt struct {
	opts RegistrarOptions
}

func (n *nrpt) powershell(ctx context.Context, script string) error {
	if _, err := n.opts.Runner(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		return fmt.Errorf("dns: powershell: %w", err)
	}
	return nil
}

func (n *nrpt) Register(ctx context.Context, _ string, server netip.Addr) (string, error) {
	script := fmt.Sprintf("Add-DnsClientNrptRule -Namespace '.%s' -NameServers '%s' -Comment '%s'", n.opts.Zone, server, nrptComment)
	if err := n.powershell(ctx, script); err != nil {
		return MethodNRPT, err
	}
	return MethodNRPT, nil
}

func (n *nrpt) Update(context.Context, []Entry) error { return nil }

func (n *nrpt) Unregister(ctx context.Context, _ string) error {
	script := fmt.Sprintf("Get-DnsClientNrptRule | Where-Object { $_.Comment -eq '%s' } | Remove-DnsClientNrptRule -Force", nrptComment)
	return n.powershell(ctx, script)
}
