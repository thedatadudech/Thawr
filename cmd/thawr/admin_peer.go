package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// peerJSON mirrors the admin API's peer view.
type peerJSON struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Mode        string   `json:"mode"`
	Owner       string   `json:"owner"`
	Tags        []string `json:"tags"`
	PublicKey   string   `json:"public_key"`
	IPv4        string   `json:"ipv4"`
	Online      bool     `json:"online"`
	CreatedAt   string   `json:"created_at"`
	LastSeenAt  string   `json:"last_seen_at,omitempty"`
	Version     string   `json:"version"`
	OS          string   `json:"os"`
	PathSummary struct {
		Direct int `json:"direct"`
		Relay  int `json:"relay"`
		Other  int `json:"other"`
	} `json:"path_summary"`
}

// peerDetailJSON mirrors the admin API's peer detail.
type peerDetailJSON struct {
	peerJSON
	Paths []struct {
		Peer      string `json:"peer"`
		State     string `json:"state"`
		Endpoint  string `json:"endpoint"`
		UpdatedAt string `json:"updated_at"`
	} `json:"paths"`
	Endpoints []struct {
		Addr string `json:"addr"`
		Kind string `json:"kind"`
	} `json:"endpoints"`
	Symmetric bool `json:"symmetric"`
	Filter    []struct {
		Src    string `json:"src"`
		Proto  string `json:"proto"`
		PortLo uint16 `json:"port_lo"`
		PortHi uint16 `json:"port_hi"`
	} `json:"filter"`
}

func newAdminPeerCmd(flags *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "peer", Short: "Manage registered peers"}
	var onlineOnly bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List peers with presence, reported paths, version and OS",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var peers []peerJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "GET", "/api/v1/peers", nil, &peers); err != nil {
				return err
			}
			if onlineOnly {
				kept := peers[:0]
				for _, p := range peers {
					if p.Online {
						kept = append(kept, p)
					}
				}
				peers = kept
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), peers)
			}
			now := time.Now()
			rows := make([][]string, 0, len(peers))
			for _, p := range peers {
				rows = append(rows, []string{p.Name, p.IPv4, p.Kind, dash(p.Owner), dash(strings.Join(p.Tags, ",")), onlineWord(p.Online),
					lastSeen(p.LastSeenAt, now), pathSummaryText(p), dash(p.Version), dash(p.OS)})
			}
			return table(cmd.OutOrStdout(), []string{"NAME", "IP", "KIND", "OWNER", "TAGS", "STATE", "LAST SEEN", "PATHS", "VERSION", "OS"}, rows)
		},
	}
	list.Flags().BoolVar(&onlineOnly, "online", false, "only peers with a live sync stream")
	show := &cobra.Command{
		Use:   "show <name>",
		Short: "Show everything the server knows about a peer",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			var d peerDetailJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "GET", "/api/v1/peers/"+args[0], nil, &d); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), d)
			}
			return renderPeerDetail(cmd.OutOrStdout(), d, time.Now())
		},
	}
	rename := &cobra.Command{
		Use:   "rename <name> <new-name>",
		Short: "Rename a peer",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p peerJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "PATCH", "/api/v1/peers/"+args[0], map[string]string{"name": args[1]}, &p); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "peer %s renamed to %s\n", args[0], p.Name)
			return err
		},
	}
	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a peer; every client drops it on the next netmap",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newAdminClient(flags.socket).do(cmd.Context(), "DELETE", "/api/v1/peers/"+args[0], nil, nil); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "peer %s deleted\n", args[0])
			return err
		},
	}
	cmd.AddCommand(list, show, rename, del)
	return cmd
}

func onlineWord(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

// lastSeen renders an RFC 3339 timestamp as an age, or "never".
func lastSeen(ts string, now time.Time) string {
	if ts == "" {
		return "never"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return humanDuration(now.Sub(t)) + " ago"
}

// pathSummaryText renders "2 direct, 1 relay" from a peer's summary,
// or "-" when it reported nothing.
func pathSummaryText(p peerJSON) string {
	var parts []string
	if p.PathSummary.Direct > 0 {
		parts = append(parts, fmt.Sprintf("%d direct", p.PathSummary.Direct))
	}
	if p.PathSummary.Relay > 0 {
		parts = append(parts, fmt.Sprintf("%d relay", p.PathSummary.Relay))
	}
	if p.PathSummary.Other > 0 {
		parts = append(parts, fmt.Sprintf("%d other", p.PathSummary.Other))
	}
	return dash(strings.Join(parts, ", "))
}

// renderPeerDetail prints one peer as key: value lines followed by its
// candidates, reported paths and compiled filter.
func renderPeerDetail(w io.Writer, d peerDetailJSON, now time.Time) error {
	nat := "cone or none"
	if d.Symmetric {
		nat = "symmetric"
	}
	lines := [][2]string{
		{"name", d.Name}, {"ip", d.IPv4}, {"kind", d.Kind}, {"mode", d.Mode}, {"owner", dash(d.Owner)},
		{"tags", dash(strings.Join(d.Tags, ","))}, {"state", onlineWord(d.Online)}, {"last seen", lastSeen(d.LastSeenAt, now)},
		{"version", dash(d.Version)}, {"os", dash(d.OS)}, {"key", d.PublicKey}, {"created", d.CreatedAt}, {"nat", nat},
	}
	for _, l := range lines {
		if _, err := fmt.Fprintf(w, "%-10s %s\n", l[0]+":", l[1]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nCandidates (%d):\n", len(d.Endpoints)); err != nil {
		return err
	}
	for _, e := range d.Endpoints {
		if _, err := fmt.Fprintf(w, "  %s (%s)\n", e.Addr, e.Kind); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nPaths reported (%d):\n", len(d.Paths)); err != nil {
		return err
	}
	for _, p := range d.Paths {
		ep := ""
		if p.Endpoint != "" {
			ep = " " + p.Endpoint
		}
		if _, err := fmt.Fprintf(w, "  %s: %s%s (%s)\n", p.Peer, p.State, ep, lastSeen(p.UpdatedAt, now)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nFilter (%d rules, who may reach this peer):\n", len(d.Filter)); err != nil {
		return err
	}
	for _, f := range d.Filter {
		ports := fmt.Sprintf("%d", f.PortLo)
		if f.PortHi != f.PortLo {
			ports = fmt.Sprintf("%d-%d", f.PortLo, f.PortHi)
		}
		if _, err := fmt.Fprintf(w, "  %s -> %s %s\n", f.Src, f.Proto, ports); err != nil {
			return err
		}
	}
	return nil
}
