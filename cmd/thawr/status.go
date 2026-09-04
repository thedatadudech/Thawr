package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/thedatadudech/thawr/internal/client"
)

// maxNameWidth is the widest peer name shown before truncation.
const maxNameWidth = 20

// renderStatus prints the human form of a status document. Ages are
// relative to st.RetrievedAt so the output is reproducible.
func renderStatus(w io.Writer, st client.Status) error {
	now := st.RetrievedAt
	if _, err := fmt.Fprintf(w, "thawr %s · %s %s · server %s %s\n", st.Version, st.Self.Name, st.Self.IPv4, st.Server.Addr, serverState(st.Server, now)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "WireGuard: %s · %s · listen %d · NAT: %s\n\n", dash(st.WireGuard.Backend), dash(st.WireGuard.Interface), st.WireGuard.ListenPort, natLine(st.NAT)); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	if _, err := fmt.Fprintln(tw, "PEER\tIP\tKIND\tOWNER\tPATH\tHANDSHAKE\tRX / TX"); err != nil {
		return err
	}
	rows := st.Peers
	if st.Hub != nil {
		rows = append(rows, *st.Hub)
	}
	for _, p := range rows {
		hs, rxtx := handshakeColumns(p, now)
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", truncateName(p.Name), p.IPv4, dash(p.Kind), dash(p.Owner), pathColumn(p), hs, rxtx); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\nFilter: %s\n", filterLine(st.Filter))
	return err
}

// serverState renders the control connection as
// "connected (netmap #42, 3s ago)", "reconnecting (attempt 3, next in
// 8s)" or "cached netmap (server unreachable since 14:02; attempt 3,
// next in 8s)".
func serverState(s client.ServerStatus, now time.Time) string {
	retry := fmt.Sprintf("attempt %d", s.Attempt)
	if s.NextRetryAt != nil {
		retry += ", next in " + humanDuration(s.NextRetryAt.Sub(now))
	}
	switch s.State {
	case client.ServerConnected:
		age := "never"
		if s.LastMessageAt != nil {
			age = humanDuration(now.Sub(*s.LastMessageAt)) + " ago"
		}
		return fmt.Sprintf("connected (netmap #%d, %s)", s.Generation, age)
	case client.ServerCached:
		since := "?"
		if s.UnreachableSince != nil {
			since = clockTime(*s.UnreachableSince, now)
		}
		return fmt.Sprintf("cached netmap (server unreachable since %s; %s)", since, retry)
	default:
		return fmt.Sprintf("reconnecting (%s)", retry)
	}
}

// clockTime shows t as HH:MM when it falls within the last day and as
// "Sep 3 14:02" otherwise, in now's zone.
func clockTime(t, now time.Time) string {
	t = t.In(now.Location())
	if now.Sub(t) < 24*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

func natLine(n client.NATStatus) string {
	switch n.Type {
	case client.NATUnknown:
		return "unknown"
	case client.NATNone:
		return "none (" + strings.Join(n.Reflexive, ", ") + ")"
	default:
		return n.Type + " (reflexive " + strings.Join(n.Reflexive, ", ") + ")"
	}
}

// pathColumn renders the PATH column: "direct <endpoint>", "relay",
// "probing", "via hub", "idle", "unreachable" or "offline".
func pathColumn(p client.PeerStatus) string {
	switch p.Path {
	case "direct":
		if p.PathEndpoint != "" {
			return "direct " + p.PathEndpoint
		}
		return "direct"
	case client.PathHub:
		return "via hub"
	case "":
		return "-"
	default:
		return p.Path
	}
}

// handshakeColumns renders HANDSHAKE and RX / TX; peers without a
// WireGuard session of their own show "-".
func handshakeColumns(p client.PeerStatus, now time.Time) (handshake, rxtx string) {
	if p.Path == client.PathHub {
		return "-", "-"
	}
	if p.LastHandshakeAt == nil {
		return "never", "-"
	}
	return humanDuration(now.Sub(*p.LastHandshakeAt)), humanBytes(p.RxBytes) + " / " + humanBytes(p.TxBytes)
}

func filterLine(f *client.FilterStatus) string {
	if f == nil {
		return "not supported by this backend"
	}
	return fmt.Sprintf("%d rules · %d dropped (last 5 min)", f.Rules, f.Dropped5m)
}

// truncateName cuts names longer than maxNameWidth to fit the column.
func truncateName(name string) string {
	if utf8.RuneCountInString(name) <= maxNameWidth {
		return name
	}
	runes := []rune(name)
	return string(runes[:maxNameWidth-1]) + "…"
}

// humanBytes formats n with SI units and at most one decimal: "0 B",
// "340 kB", "1.2 MB".
func humanBytes(n uint64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"kB", "MB", "GB", "TB", "PB"}
	v := float64(n) / unit
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0")
	return s + " " + units[i]
}

// humanDuration formats d in its largest whole unit: "12s", "3m", "2h",
// "5d"; negative durations read "0s".
func humanDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}
