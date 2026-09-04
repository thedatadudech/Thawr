package client

import (
	"sync"
	"time"
)

// dropWindow turns a monotonic drop counter into "drops in the last
// span": it keeps timestamped samples and reports the difference to the
// sample taken span ago.
type dropWindow struct {
	span time.Duration
	// minGap bounds the sample rate so probe-speed ticks do not bloat
	// the window.
	minGap time.Duration

	mu      sync.Mutex
	samples []dropSample
}

type dropSample struct {
	at    time.Time
	total uint64
}

func newDropWindow(span time.Duration) *dropWindow {
	return &dropWindow{span: span, minGap: span / 60}
}

// Record adds a sample and forgets samples older than the span, keeping
// the newest of them as the baseline.
func (w *dropWindow) Record(now time.Time, total uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n := len(w.samples); n > 0 && now.Sub(w.samples[n-1].at) < w.minGap {
		return
	}
	w.samples = append(w.samples, dropSample{at: now, total: total})
	cutoff := now.Add(-w.span)
	i := 0
	for i+1 < len(w.samples) && !w.samples[i+1].at.After(cutoff) {
		i++
	}
	w.samples = w.samples[i:]
}

// Delta returns how many drops happened within the span before now,
// given the current total.
func (w *dropWindow) Delta(now time.Time, total uint64) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.samples) == 0 {
		return 0
	}
	cutoff := now.Add(-w.span)
	base := w.samples[0]
	for _, s := range w.samples {
		if s.at.After(cutoff) {
			break
		}
		base = s
	}
	if total < base.total {
		return 0 // counter reset (filter reinstalled)
	}
	return total - base.total
}
