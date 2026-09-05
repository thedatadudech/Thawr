package api

import (
	"fmt"

	thawrv1 "github.com/thedatadudech/thawr/internal/api/proto/thawr/v1"
	"github.com/thedatadudech/thawr/internal/control"
)

func kindToProto(k control.EndpointKind) thawrv1.EndpointKind {
	switch k {
	case control.EndpointLocal:
		return thawrv1.EndpointKind_ENDPOINT_KIND_LOCAL
	case control.EndpointReflexive:
		return thawrv1.EndpointKind_ENDPOINT_KIND_REFLEXIVE
	case control.EndpointStable:
		return thawrv1.EndpointKind_ENDPOINT_KIND_STABLE
	}
	return thawrv1.EndpointKind_ENDPOINT_KIND_UNSPECIFIED
}

func kindFromProto(k thawrv1.EndpointKind) control.EndpointKind {
	switch k {
	case thawrv1.EndpointKind_ENDPOINT_KIND_LOCAL:
		return control.EndpointLocal
	case thawrv1.EndpointKind_ENDPOINT_KIND_REFLEXIVE:
		return control.EndpointReflexive
	case thawrv1.EndpointKind_ENDPOINT_KIND_STABLE:
		return control.EndpointStable
	}
	return 0
}

func endpointsToProto(eps []control.Endpoint) []*thawrv1.Endpoint {
	out := make([]*thawrv1.Endpoint, 0, len(eps))
	for _, e := range eps {
		out = append(out, &thawrv1.Endpoint{Addr: e.Addr.String(), Kind: kindToProto(e.Kind)})
	}
	return out
}

func endpointsFromProto(eps []*thawrv1.Endpoint) ([]control.Endpoint, error) {
	if len(eps) > control.MaxEndpoints {
		return nil, fmt.Errorf("%w: at most %d endpoints", control.ErrValidation, control.MaxEndpoints)
	}
	out := make([]control.Endpoint, 0, len(eps))
	for _, e := range eps {
		ep, err := control.ParseEndpoint(e.GetAddr(), kindFromProto(e.GetKind()))
		if err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, nil
}

// netMapToProto converts the control netmap to its wire form.
func netMapToProto(nm control.NetMap) *thawrv1.NetMap {
	out := &thawrv1.NetMap{
		Generation: nm.Generation,
		Self:       &thawrv1.SelfInfo{Id: nm.SelfID, Name: nm.SelfName, Kind: nm.SelfKind, Ipv4: nm.SelfIPv4.String(), OverlayCidr: nm.Overlay.String(), StunAddrs: append([]string{}, nm.STUN...)},
		Hub:        &thawrv1.HubPeer{PublicKey: nm.Hub.PublicKey, Endpoint: nm.Hub.Endpoint},
	}
	for _, p := range nm.Hub.AllowedIPs {
		out.Hub.AllowedIps = append(out.Hub.AllowedIps, p.String())
	}
	for _, p := range nm.Peers {
		np := &thawrv1.NetPeer{
			Id: p.ID, Name: p.Name, Kind: p.Kind, Owner: p.Owner, PublicKey: p.PublicKey, Ipv4: p.IPv4.String(),
			Online: p.Online, Endpoints: endpointsToProto(p.Endpoints), Symmetric: p.Symmetric, Keepalive: p.Keepalive, ViaHub: p.ViaHub,
		}
		for _, a := range p.AllowedIPs {
			np.AllowedIps = append(np.AllowedIps, a.String())
		}
		out.Peers = append(out.Peers, np)
	}
	for _, f := range nm.Filter {
		out.Filter = append(out.Filter, &thawrv1.FilterRule{SrcIpv4: f.SrcIPv4.String(), Proto: f.Proto, PortLo: uint32(f.PortLo), PortHi: uint32(f.PortHi)})
	}
	return out
}
