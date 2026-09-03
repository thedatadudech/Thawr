package stun

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// Result of a discovery round.
type Result struct {
	// Mapped holds the server-reflexive address reported by each server
	// that answered, in server order.
	Mapped []netip.AddrPort
	// Symmetric is true when two servers saw different mappings, which
	// means the NAT allocates a new port per destination.
	Symmetric bool
}

// ErrNoResponse means no STUN server answered.
var ErrNoResponse = errors.New("stun: no response from any server")

// Discover asks every server for the transport's reflexive address. Each
// attempt waits timeout; two attempts are made. It succeeds when at
// least one server answers.
func Discover(ctx context.Context, tr Transport, servers []netip.AddrPort, timeout time.Duration) (Result, error) {
	if len(servers) == 0 {
		return Result{}, errors.New("stun: no servers configured")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	pending := make(map[TxID]int, len(servers))
	answers := make([]netip.AddrPort, len(servers))
	answered := 0
	for attempt := 0; attempt < 2 && answered < len(servers); attempt++ {
		for i, srv := range servers {
			if answers[i].IsValid() {
				continue
			}
			tx := NewTxID()
			pending[tx] = i
			if err := tr.Send(ctx, srv, Request(tx)); err != nil {
				return Result{}, err
			}
		}
		n, err := collect(ctx, tr, timeout, pending, answers)
		answered += n
		if err != nil {
			return Result{}, err
		}
	}
	if answered == 0 {
		return Result{}, ErrNoResponse
	}
	res := Result{}
	for _, a := range answers {
		if !a.IsValid() {
			continue
		}
		res.Mapped = append(res.Mapped, a)
		if a != res.Mapped[0] {
			res.Symmetric = true
		}
	}
	return res, nil
}

// collect reads responses until every pending request is answered or
// timeout passes. It returns how many new answers arrived; a context
// cancellation is an error, a plain timeout is not.
func collect(ctx context.Context, tr Transport, timeout time.Duration, pending map[TxID]int, answers []netip.AddrPort) (int, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	got := 0
	for len(pending) > 0 {
		pkt, _, err := tr.Recv(waitCtx)
		if err != nil {
			if ctx.Err() != nil {
				return got, fmt.Errorf("stun: discovery: %w", ctx.Err())
			}
			return got, nil
		}
		tx, mapped, err := ParseResponse(pkt)
		if err != nil {
			continue
		}
		i, ok := pending[tx]
		if !ok {
			continue
		}
		delete(pending, tx)
		answers[i] = mapped
		got++
	}
	return got, nil
}
