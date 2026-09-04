package client

import (
	"testing"
	"time"
)

func TestDropWindow(t *testing.T) {
	w := newDropWindow(5 * time.Minute)
	t0 := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	if got := w.Delta(t0, 10); got != 0 {
		t.Errorf("empty window: %d", got)
	}
	w.Record(t0, 10)
	w.Record(t0.Add(time.Minute), 14)
	w.Record(t0.Add(time.Minute+time.Second), 99) // inside minGap, ignored
	if got := w.Delta(t0.Add(2*time.Minute), 20); got != 10 {
		t.Errorf("within window: got %d, want 10 (since first sample)", got)
	}
	w.Record(t0.Add(4*time.Minute), 30)
	w.Record(t0.Add(6*time.Minute), 45)
	// Baseline is the newest sample at or before now-5m (t0+1m → 14).
	if got := w.Delta(t0.Add(6*time.Minute), 45); got != 31 {
		t.Errorf("sliding: got %d, want 31", got)
	}
	if got := w.Delta(t0.Add(6*time.Minute), 3); got != 0 {
		t.Errorf("counter reset: got %d, want 0", got)
	}
	if n := len(w.samples); n > 3 {
		t.Errorf("stale samples kept: %d", n)
	}
}
