package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type peerJSON struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Mode       string   `json:"mode"`
	Owner      string   `json:"owner"`
	Tags       []string `json:"tags"`
	PublicKey  string   `json:"public_key"`
	IPv4       string   `json:"ipv4"`
	CreatedAt  string   `json:"created_at"`
	LastSeenAt string   `json:"last_seen_at,omitempty"`
}

func newAdminPeerCmd(flags *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "peer", Short: "Manage registered peers"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List peers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var peers []peerJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "GET", "/api/v1/peers", nil, &peers); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), peers)
			}
			rows := make([][]string, 0, len(peers))
			for _, p := range peers {
				rows = append(rows, []string{p.Name, p.IPv4, p.Kind, p.Mode, dash(p.Owner), dash(strings.Join(p.Tags, ",")), p.CreatedAt})
			}
			return table(cmd.OutOrStdout(), []string{"NAME", "IP", "KIND", "MODE", "OWNER", "TAGS", "CREATED"}, rows)
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
	cmd.AddCommand(list, rename, del)
	return cmd
}
