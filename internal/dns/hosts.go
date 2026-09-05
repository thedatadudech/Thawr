package dns

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// hostsPath is the hosts file on Unix-like systems.
const hostsPath = "/etc/hosts"

// Markers delimit the managed block; everything outside it is kept byte
// for byte.
const (
	hostsBegin = "# thawr begin"
	hostsEnd   = "# thawr end"
)

// hostsFile keeps a managed block of <ip> <name>.thawr lines in the
// hosts file for systems without a way to route a zone to a resolver.
type hostsFile struct {
	opts RegistrarOptions
	path string
}

func newHostsFile(o RegistrarOptions) *hostsFile {
	return &hostsFile{opts: o, path: filepath.Join(o.Root, hostsPath)}
}

func (h *hostsFile) Register(context.Context, string, netip.Addr) (string, error) {
	return MethodHosts, nil
}

func (h *hostsFile) Update(_ context.Context, entries []Entry) error {
	return h.write(renderHostsBlock(h.opts.Zone, entries))
}

func (h *hostsFile) Unregister(context.Context, string) error {
	return h.write("")
}

// renderHostsBlock renders the managed block, sorted by address, with
// only the zone name per entry so a peer never shadows a LAN host of
// the same bare name.
func renderHostsBlock(zone string, entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	sorted := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Addr.IsValid() && e.Name != "" {
			sorted = append(sorted, e)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if c := sorted[i].Addr.Compare(sorted[j].Addr); c != 0 {
			return c < 0
		}
		return sorted[i].Name < sorted[j].Name
	})
	var b strings.Builder
	b.WriteString(hostsBegin + "\n")
	for _, e := range sorted {
		fmt.Fprintf(&b, "%s %s.%s\n", e.Addr, e.Name, zone)
	}
	b.WriteString(hostsEnd + "\n")
	return b.String()
}

// write replaces the managed block with block (empty removes it) and
// leaves every other line untouched. The file is rewritten through a
// temporary file in the same directory and renamed into place.
func (h *hostsFile) write(block string) error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) && block == "" {
			return nil
		}
		return fmt.Errorf("dns: read %s: %w", h.path, err)
	}
	out, changed := spliceHostsBlock(data, block)
	if !changed {
		return nil
	}
	info, err := os.Stat(h.path)
	if err != nil {
		return fmt.Errorf("dns: stat %s: %w", h.path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".hosts.thawr-*")
	if err != nil {
		return fmt.Errorf("dns: temp file for %s: %w", h.path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("dns: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("dns: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("dns: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, h.path); err != nil {
		cleanup()
		return fmt.Errorf("dns: replace %s: %w", h.path, err)
	}
	return nil
}

// spliceHostsBlock returns data with the managed block replaced by
// block (removed when empty) and whether anything changed. A block
// that is not present yet is appended after a trailing newline.
func spliceHostsBlock(data []byte, block string) ([]byte, bool) {
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	start, end := -1, -1
	for i, l := range lines {
		switch strings.TrimRight(l, "\r\n") {
		case hostsBegin:
			if start < 0 {
				start = i
			}
		case hostsEnd:
			if start >= 0 && end < 0 {
				end = i
			}
		}
	}
	// A begin marker without its end marker is a truncated block (an
	// interrupted write, a hand edit): it runs to the end of the file,
	// so it is replaced or removed instead of accumulating.
	if start >= 0 && end < 0 {
		end = len(lines) - 1
	}
	var out bytes.Buffer
	switch {
	case start >= 0 && end >= start:
		for _, l := range lines[:start] {
			out.WriteString(l)
		}
		out.WriteString(block)
		for _, l := range lines[end+1:] {
			out.WriteString(l)
		}
	case block == "":
		return data, false
	default:
		out.WriteString(text)
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			out.WriteString("\n")
		}
		out.WriteString(block)
	}
	if bytes.Equal(out.Bytes(), data) {
		return data, false
	}
	return out.Bytes(), true
}
