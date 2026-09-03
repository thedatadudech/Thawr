package wg

import (
	"context"
	"testing"
)

func TestOpenRequiresName(t *testing.T) {
	if _, err := Open(context.Background(), Options{}); err == nil {
		t.Fatal("expected error without a name")
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{Name: "x"}.withDefaults()
	if o.MTU != DefaultMTU || o.Logger == nil {
		t.Errorf("defaults not applied: %+v", o)
	}
}
