package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	thawrv1 "github.com/thedatadudech/thawr/internal/api/proto/thawr/v1"
	"github.com/thedatadudech/thawr/internal/control"
)

// Enroller is the control-plane operation behind the Enroll RPC.
type Enroller interface {
	Enroll(ctx context.Context, req control.EnrollRequest) (control.EnrollResult, error)
}

// HubInfo describes the server's own WireGuard endpoint as advertised
// to peers.
type HubInfo struct {
	PublicKey string
	Endpoint  string
	Overlay   netip.Prefix
}

// GRPCDeps are the collaborators of the Control service.
type GRPCDeps struct {
	Enroller Enroller
	Hub      HubInfo
	Version  string
	Logger   *slog.Logger
}

// NewGRPC builds the gRPC server with the Control service registered.
// It is served through Combine on the HTTPS listener.
func NewGRPC(deps GRPCDeps) (*grpc.Server, error) {
	if deps.Enroller == nil {
		return nil, fmt.Errorf("api: Enroller required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	srv := grpc.NewServer()
	thawrv1.RegisterControlServer(srv, &controlServer{deps: deps})
	return srv, nil
}

type controlServer struct {
	thawrv1.UnimplementedControlServer
	deps GRPCDeps
}

func (s *controlServer) Enroll(ctx context.Context, req *thawrv1.EnrollRequest) (*thawrv1.EnrollResponse, error) {
	res, err := s.deps.Enroller.Enroll(ctx, control.EnrollRequest{
		Token:         req.GetToken(),
		PublicKey:     req.GetPublicKey(),
		Hostname:      req.GetHostname(),
		OS:            req.GetOs(),
		Arch:          req.GetArch(),
		ClientVersion: req.GetClientVersion(),
		Name:          req.GetName(),
		RemoteIP:      remoteIP(ctx),
	})
	if err != nil {
		return nil, s.toStatus(err)
	}
	return &thawrv1.EnrollResponse{
		PeerId:           res.Peer.ID,
		Name:             res.Peer.Name,
		Ipv4:             res.Peer.IPv4,
		OverlayCidr:      s.deps.Hub.Overlay.String(),
		NodeSecret:       res.NodeSecret,
		HubPublicKey:     s.deps.Hub.PublicKey,
		HubEndpoint:      s.deps.Hub.Endpoint,
		ServerVersion:    s.deps.Version,
		NetmapGeneration: res.Generation,
	}, nil
}

// toStatus maps control errors to gRPC codes without leaking internals.
func (s *controlServer) toStatus(err error) error {
	switch {
	case errors.Is(err, control.ErrInvalidToken):
		return status.Error(codes.PermissionDenied, control.ErrInvalidToken.Error())
	case errors.Is(err, control.ErrValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, control.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, control.ErrRateLimited.Error())
	case errors.Is(err, control.ErrVersion):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, control.ErrExhausted):
		return status.Error(codes.ResourceExhausted, "no free overlay address")
	}
	s.deps.Logger.Error("enroll failed", "err", err)
	return status.Error(codes.Internal, "internal error")
}

// remoteIP extracts the caller's IP from the gRPC peer info.
func remoteIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}
