package control

import (
	"errors"
	"net/netip"
	"testing"
)

func TestAllocatorSequential(t *testing.T) {
	p := netip.MustParsePrefix("100.64.0.0/10")
	got, err := NextAddress(p, nil)
	if err != nil || got != netip.MustParseAddr("100.64.0.2") {
		t.Errorf("first: %v %v", got, err)
	}
	got, err = NextAddress(p, []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("100.64.0.3")})
	if err != nil || got != netip.MustParseAddr("100.64.0.4") {
		t.Errorf("after two: %v %v", got, err)
	}
	got, err = NextAddress(p, []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("100.64.0.4")})
	if err != nil || got != netip.MustParseAddr("100.64.0.3") {
		t.Errorf("fills gaps: %v %v", got, err)
	}
}

func TestAllocatorSkipsReserved(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/29") // .0 network, .1 hub, .2-.6 peers, .7 broadcast
	var used []netip.Addr
	for i := 2; i <= 6; i++ {
		got, err := NextAddress(p, used)
		if err != nil {
			t.Fatalf("alloc %d: %v", i, err)
		}
		if want := netip.MustParseAddr("10.0.0." + string(rune('0'+i))); got != want {
			t.Errorf("alloc %d: got %v, want %v", i, got, want)
		}
		used = append(used, got)
	}
	if _, err := NextAddress(p, used); !errors.Is(err, ErrExhausted) {
		t.Errorf("exhausted: %v", err)
	}
}

func TestAllocatorRejectsIPv6(t *testing.T) {
	if _, err := NextAddress(netip.MustParsePrefix("fd00::/64"), nil); err == nil {
		t.Error("expected error for IPv6")
	}
}
