package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestStaticContainsUI(t *testing.T) {
	root, err := Static()
	if err != nil {
		t.Fatalf("Static: %v", err)
	}
	for name, want := range map[string]string{
		"index.html":  "Thawr",
		"app.js":      "/api/v1/login",
		"style.css":   "body",
		"favicon.svg": "<svg",
		"logo.svg":    "<svg",
	} {
		b, err := fs.ReadFile(root, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s does not contain %q", name, want)
		}
	}
	index, _ := fs.ReadFile(root, "index.html")
	for _, id := range []string{`id="login-form"`, `id="token-form"`, `id="peers"`, `id="join-command"`} {
		if !strings.Contains(string(index), id) {
			t.Errorf("index.html missing %s", id)
		}
	}
}
