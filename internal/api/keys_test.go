package api

import (
	"testing"

	"github.com/thedatadudech/thawr/internal/wg"
)

func newKey(t *testing.T) string {
	t.Helper()
	k, err := wg.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k.PublicKey().String()
}
