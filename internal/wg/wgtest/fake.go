// Package wgtest provides an in-memory wg.Device for tests.
package wgtest

import (
	"context"
	"errors"
	"sync"

	"github.com/thedatadudech/thawr/internal/wg"
)

// Fake records every configuration change and serves scripted Stats.
// Configs receives a snapshot of the full configuration after every
// Configure, SetPeer and RemovePeer call, so tests can follow the exact
// sequence of endpoints a daemon tried.
type Fake struct {
	mu sync.Mutex
	// Configs holds a snapshot after every change, oldest first.
	Configs []wg.Config
	// StatsResult overrides, by public key, the stats derived from the
	// current peer set (handshake time, counters, endpoint when valid).
	StatsResult []wg.PeerStats
	// ConfigureErr, when set, is returned by Configure.
	ConfigureErr error
	closed       bool
	name         string
	current      wg.Config
}

// New returns a Fake named name.
func New(name string) *Fake { return &Fake{name: name} }

// Configure records cfg as the complete state.
func (f *Fake) Configure(_ context.Context, cfg wg.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("wgtest: configure after close")
	}
	if f.ConfigureErr != nil {
		return f.ConfigureErr
	}
	// Mirror the adapters: a zero endpoint keeps the current one.
	for i, p := range cfg.Peers {
		if !p.Endpoint.IsValid() {
			for _, old := range f.current.Peers {
				if old.PublicKey == p.PublicKey {
					cfg.Peers[i].Endpoint = old.Endpoint
				}
			}
		}
	}
	f.current = cloneConfig(cfg)
	f.Configs = append(f.Configs, cloneConfig(cfg))
	return nil
}

// SetPeer creates or updates one peer.
func (f *Fake) SetPeer(_ context.Context, p wg.Peer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("wgtest: set peer after close")
	}
	replaced := false
	for i, old := range f.current.Peers {
		if old.PublicKey == p.PublicKey {
			if !p.Endpoint.IsValid() {
				p.Endpoint = old.Endpoint
			}
			f.current.Peers[i] = p
			replaced = true
		}
	}
	if !replaced {
		f.current.Peers = append(f.current.Peers, p)
	}
	f.Configs = append(f.Configs, cloneConfig(f.current))
	return nil
}

// RemovePeer deletes one peer.
func (f *Fake) RemovePeer(_ context.Context, key wg.Key) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("wgtest: remove peer after close")
	}
	kept := f.current.Peers[:0]
	for _, p := range f.current.Peers {
		if p.PublicKey != key {
			kept = append(kept, p)
		}
	}
	f.current.Peers = kept
	f.Configs = append(f.Configs, cloneConfig(f.current))
	return nil
}

// Stats derives one entry per current peer and applies StatsResult
// overrides by key.
func (f *Fake) Stats(context.Context) ([]wg.PeerStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]wg.PeerStats, 0, len(f.current.Peers))
	for _, p := range f.current.Peers {
		s := wg.PeerStats{PublicKey: p.PublicKey, Endpoint: p.Endpoint}
		for _, o := range f.StatsResult {
			if o.PublicKey == p.PublicKey {
				s.LastHandshake, s.RxBytes, s.TxBytes = o.LastHandshake, o.RxBytes, o.TxBytes
				if o.Endpoint.IsValid() {
					s.Endpoint = o.Endpoint
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// SetStats replaces the StatsResult overrides atomically.
func (f *Fake) SetStats(stats ...wg.PeerStats) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StatsResult = append([]wg.PeerStats(nil), stats...)
}

// Peers returns a copy of the current peer set.
func (f *Fake) Peers() []wg.Peer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneConfig(f.current).Peers
}

// Backend reports "fake".
func (f *Fake) Backend() string { return "fake" }

// Name returns the name given to New.
func (f *Fake) Name() string { return f.name }

// Close marks the device closed; further changes fail.
func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// Closed reports whether Close was called.
func (f *Fake) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// Last returns the most recent snapshot and whether one exists.
func (f *Fake) Last() (wg.Config, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Configs) == 0 {
		return wg.Config{}, false
	}
	return f.Configs[len(f.Configs)-1], true
}

func cloneConfig(c wg.Config) wg.Config {
	out := c
	out.Addresses = append(out.Addresses[:0:0], c.Addresses...)
	out.Peers = make([]wg.Peer, len(c.Peers))
	for i, p := range c.Peers {
		out.Peers[i] = p
		out.Peers[i].AllowedIPs = append(p.AllowedIPs[:0:0], p.AllowedIPs...)
	}
	return out
}
