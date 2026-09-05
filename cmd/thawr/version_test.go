package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestVersionString(t *testing.T) {
	d := details("v0.1.0", "5f795a3", "2026-09-05", nil)
	want := "thawr v0.1.0 (" + runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH + ", commit 5f795a3, built 2026-09-05)"
	if got := d.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := details("", "", "", nil).String(); got != "thawr dev ("+runtime.Version()+", "+runtime.GOOS+"/"+runtime.GOARCH+")" {
		t.Errorf("empty build: %q", got)
	}
}

func TestVersionFallbackBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "5f795a3c0ffee0000000000000000000deadbeef"},
		{Key: "vcs.time", Value: "2026-09-05T03:50:32Z"},
		{Key: "vcs.modified", Value: "true"},
	}}
	d := details("dev", "", "", info)
	if d.Commit != "5f795a3-dirty" || d.BuiltAt != "2026-09-05" || d.Version != "dev" {
		t.Errorf("fallback: %+v", d)
	}
	// Linker values win over the VCS stamp.
	d = details("v0.1.0", "abc1234", "2026-01-01", info)
	if d.Commit != "abc1234" || d.BuiltAt != "2026-01-01" {
		t.Errorf("ldflags not preferred: %+v", d)
	}
}

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	root := newRootCmd(&out, &errOut)
	root.SetArgs([]string{"version", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var d map[string]string
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if d["version"] != "dev" || d["go"] != runtime.Version() || d["os"] != runtime.GOOS || d["arch"] != runtime.GOARCH {
		t.Errorf("fields: %v", d)
	}
}
