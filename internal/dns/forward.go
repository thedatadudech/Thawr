package dns

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// forward sends req to each upstream in turn over the transport it
// arrived on and returns the first reply whose ID matches. It never
// caches and never recurses on its own.
func (s *Server) forward(ctx context.Context, req []byte, tcp bool) ([]byte, error) {
	if len(req) < 2 {
		return nil, errors.New("dns: query too short to forward")
	}
	var errs []error
	for _, up := range s.opts.Upstreams {
		resp, err := s.forwardOne(ctx, up.String(), req, tcp)
		if err == nil {
			return resp, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", up, err))
	}
	return nil, errors.Join(errs...)
}

func (s *Server) forwardOne(ctx context.Context, upstream string, req []byte, tcp bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	network := "udp"
	if tcp {
		network = "tcp"
	}
	c, err := s.dial(ctx, network, upstream)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	deadline, _ := ctx.Deadline()
	_ = c.SetDeadline(deadline)
	if tcp {
		if err := writeTCP(c, req); err != nil {
			return nil, err
		}
		resp, err := readTCP(c)
		if err != nil {
			return nil, err
		}
		return matchID(req, resp)
	}
	if _, err := c.Write(req); err != nil {
		return nil, err
	}
	buf := make([]byte, maxMessage)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return nil, err
		}
		if resp, err := matchID(req, buf[:n]); err == nil {
			return resp, nil
		}
		if time.Until(deadline) <= 0 {
			return nil, errors.New("dns: no matching reply before the deadline")
		}
	}
}

// matchID accepts a reply only when its ID is the query's, so a stale
// datagram on a reused socket cannot answer a different question.
func matchID(req, resp []byte) ([]byte, error) {
	if len(resp) < 12 {
		return nil, errors.New("dns: short reply")
	}
	if resp[0] != req[0] || resp[1] != req[1] {
		return nil, errors.New("dns: reply id mismatch")
	}
	return append([]byte(nil), resp...), nil
}
