package wgtest

import (
	"context"
	"testing"

	"github.com/thedatadudech/thawr/internal/wg"
)

func TestFakeRecordsAndCloses(t *testing.T) {
	f := New("thawr0")
	var _ wg.Device = f
	ctx := context.Background()
	if err := f.Configure(ctx, wg.Config{ListenPort: 1}); err != nil {
		t.Fatal(err)
	}
	if last, ok := f.Last(); !ok || last.ListenPort != 1 {
		t.Errorf("Last: %+v %v", last, ok)
	}
	if err := f.Close(); err != nil || !f.Closed() {
		t.Errorf("Close: %v closed=%v", err, f.Closed())
	}
	if err := f.Configure(ctx, wg.Config{}); err == nil {
		t.Error("configure after close should fail")
	}
}
