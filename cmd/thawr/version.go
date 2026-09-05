package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// buildDetails is what `thawr version` reports. Release builds set
// version, commit and builtAt through -ldflags; development builds fall
// back to the VCS stamp Go records in the binary.
type buildDetails struct {
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

// details assembles buildDetails from the linker variables and, where
// those are empty, from info (nil means no build info is available).
func details(version, commit, builtAt string, info *debug.BuildInfo) buildDetails {
	d := buildDetails{Version: version, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Commit: commit, BuiltAt: builtAt}
	if d.Version == "" {
		d.Version = "dev"
	}
	if info == nil {
		return d
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			if d.BuiltAt == "" {
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					d.BuiltAt = t.UTC().Format(time.DateOnly)
				}
			}
		case "vcs.modified":
			modified = s.Value
		}
	}
	if d.Commit == "" && revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		d.Commit = revision
		if modified == "true" && !strings.HasSuffix(d.Version, "-dirty") {
			d.Commit += "-dirty"
		}
	}
	return d
}

// String renders "thawr v0.1.0 (go1.26.8, linux/amd64, commit 5f795a3,
// built 2026-09-05)"; commit and date are omitted when unknown.
func (d buildDetails) String() string {
	parts := []string{d.Go, d.OS + "/" + d.Arch}
	if d.Commit != "" {
		parts = append(parts, "commit "+d.Commit)
	}
	if d.BuiltAt != "" {
		parts = append(parts, "built "+d.BuiltAt)
	}
	return fmt.Sprintf("thawr %s (%s)", d.Version, strings.Join(parts, ", "))
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version, Go toolchain, platform and commit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, _ := debug.ReadBuildInfo()
			d := details(version, commit, builtAt, info)
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(d)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), d.String())
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}
