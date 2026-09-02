package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestStaticContainsIndex(t *testing.T) {
	root, err := Static()
	if err != nil {
		t.Fatalf("Static: %v", err)
	}
	b, err := fs.ReadFile(root, "index.html")
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(b), "Thawr") {
		t.Errorf("index.html does not mention Thawr")
	}
}
