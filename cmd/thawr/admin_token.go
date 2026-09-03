package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type tokenJSON struct {
	ID          string   `json:"id"`
	Owner       string   `json:"owner"`
	Kind        string   `json:"kind"`
	Tags        []string `json:"tags"`
	PeerName    string   `json:"peer_name,omitempty"`
	CreatedAt   string   `json:"created_at"`
	ExpiresAt   string   `json:"expires_at"`
	UsedAt      string   `json:"used_at,omitempty"`
	UsedBy      string   `json:"used_by_peer_id,omitempty"`
	Secret      string   `json:"secret,omitempty"`
	JoinCommand string   `json:"join_command,omitempty"`
}

func newAdminTokenCmd(flags *adminFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage one-time enrollment tokens"}
	var (
		owner, kind, name, expires string
		tags                       []string
	)
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a one-time token and print the join command (shown once)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if owner == "" {
				return &exitError{code: exitConfigError, err: fmt.Errorf("--owner is required")}
			}
			var t tokenJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "POST", "/api/v1/tokens", map[string]any{
				"owner": owner, "kind": kind, "tags": tags, "peer_name": name, "expires": expires,
			}, &t); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), t)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Token id:   %s  (expires %s, single use)\nJoin with:  %s\n", t.ID, t.ExpiresAt, t.JoinCommand)
			return err
		},
	}
	create.Flags().StringVar(&owner, "owner", "", "user who will own the peer (required)")
	create.Flags().StringVar(&kind, "kind", "human", "human, server or agent")
	create.Flags().StringSliceVar(&tags, "tags", nil, "tags like tag:prod (comma separated)")
	create.Flags().StringVar(&name, "name", "", "peer name to assign instead of the hostname")
	create.Flags().StringVar(&expires, "expires", "1h", "validity, e.g. 30m, 1h, 7d (max 30d)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var tokens []tokenJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "GET", "/api/v1/tokens", nil, &tokens); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), tokens)
			}
			rows := make([][]string, 0, len(tokens))
			for _, t := range tokens {
				state := "unused"
				if t.UsedAt != "" {
					state = "used"
				}
				rows = append(rows, []string{t.ID, t.Owner, t.Kind, dash(strings.Join(t.Tags, ",")), dash(t.PeerName), t.ExpiresAt, state})
			}
			return table(cmd.OutOrStdout(), []string{"ID", "OWNER", "KIND", "TAGS", "NAME", "EXPIRES", "STATE"}, rows)
		},
	}
	revoke := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newAdminClient(flags.socket).do(cmd.Context(), "DELETE", "/api/v1/tokens/"+args[0], nil, nil); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "token %s revoked\n", args[0])
			return err
		},
	}
	cmd.AddCommand(create, list, revoke)
	return cmd
}
