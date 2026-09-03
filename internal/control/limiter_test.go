package control

import (
	"testing"
	"time"
)

func TestLimiterWindowAndBackoff(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now, time.Minute, 3, 8*time.Second)
	for i := 0; i < 3; i++ {
		if !l.allow("k") {
			t.Fatalf("attempt %d blocked early", i)
		}
		l.fail("k")
	}
	if l.allow("k") {
		t.Error("threshold reached but still allowed")
	}
	clk.Advance(time.Second) // 2^0 = 1 s backoff
	if !l.allow("k") {
		t.Error("backoff elapsed but blocked")
	}
	l.fail("k") // 4 failures: 2^1 = 2 s
	clk.Advance(time.Second)
	if l.allow("k") {
		t.Error("2 s backoff not enforced")
	}
	clk.Advance(time.Second)
	if !l.allow("k") {
		t.Error("blocked after 2 s backoff")
	}
	for i := 0; i < 10; i++ {
		l.fail("k")
	}
	clk.Advance(8 * time.Second)
	if !l.allow("k") {
		t.Error("max delay cap not applied")
	}
	l.reset("k")
	if !l.allow("k") {
		t.Error("reset did not clear")
	}
	if !l.allow("other") {
		t.Error("unrelated key affected")
	}
	l.fail("k")
	clk.Advance(2 * time.Minute)
	if !l.allow("k") {
		t.Error("failures outside the window still count")
	}
}
