// Package wgtest provides an in-memory wg.Device for tests.
package wgtest

import (
	"context"
	"errors"
	"sync"

	"github.com/thedatadudech/thawr/internal/wg"
)

// Fake records every Configure call and serves canned Stats.
type Fake struct {
	mu sync.Mutex
	// Configs holds every configuration applied, oldest first.
	Configs []wg.Config
	// StatsResult is returned by Stats.
	StatsResult []wg.PeerStats
	// ConfigureErr, when set, is returned by Configure.
	ConfigureErr error
	closed       bool
	name         string
}

// New returns a Fake named name.
func New(name string) *Fake { return &Fake{name: name} }

// Configure records cfg.
func (f *Fake) Configure(_ context.Context, cfg wg.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("wgtest: configure after close")
	}
	if f.ConfigureErr != nil {
		return f.ConfigureErr
	}
	f.Configs = append(f.Configs, cfg)
	return nil
}

// Stats returns StatsResult.
func (f *Fake) Stats(context.Context) ([]wg.PeerStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wg.PeerStats(nil), f.StatsResult...), nil
}

// Backend reports "fake".
func (f *Fake) Backend() string { return "fake" }

// Name returns the name given to New.
func (f *Fake) Name() string { return f.name }

// Close marks the device closed; further Configure calls fail.
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

// Last returns the most recent config and whether one exists.
func (f *Fake) Last() (wg.Config, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Configs) == 0 {
		return wg.Config{}, false
	}
	return f.Configs[len(f.Configs)-1], true
}
