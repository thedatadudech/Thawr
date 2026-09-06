package main

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// auditJSON mirrors the admin API's audit entry.
type auditJSON struct {
	ID        int64             `json:"id"`
	At        time.Time         `json:"at"`
	Actor     string            `json:"actor"`
	ActorRole string            `json:"actor_role"`
	Action    string            `json:"action"`
	Target    string            `json:"target"`
	Details   map[string]string `json:"details"`
}

func newAdminAuditCmd(flags *adminFlags) *cobra.Command {
	var (
		since, action, actor string
		limit                int
		beforeID             int64
	)
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "List the audit log of control-plane changes, newest first",
		Long: `Lists who did what on the server: tokens, enrolments, renames,
deletions, key rotations, user creation, logins and policy reloads.
--since takes a duration (24h, 30m) or an RFC 3339 time; --before-id
pages further back from an entry's id.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if since != "" {
				q.Set("since", since)
			}
			if action != "" {
				q.Set("action", action)
			}
			if actor != "" {
				q.Set("actor", actor)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			if beforeID > 0 {
				q.Set("before_id", strconv.FormatInt(beforeID, 10))
			}
			path := "/api/v1/audit"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			var entries []auditJSON
			if err := newAdminClient(flags.socket).do(cmd.Context(), "GET", path, nil, &entries); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), entries)
			}
			rows := make([][]string, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, []string{strconv.FormatInt(e.ID, 10), e.At.UTC().Format(time.RFC3339), e.Actor, e.Action, dash(e.Target), dash(detailsText(e.Details))})
			}
			return table(cmd.OutOrStdout(), []string{"ID", "TIME", "ACTOR", "ACTION", "TARGET", "DETAILS"}, rows)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only entries after this duration ago (24h) or RFC 3339 time")
	cmd.Flags().StringVar(&action, "action", "", "only this action (peer.rename, token.create, ...)")
	cmd.Flags().StringVar(&actor, "actor", "", "only this actor (a user name, local, or peer:<name>)")
	cmd.Flags().IntVar(&limit, "limit", 0, "at most this many entries (server default 100, max 1000)")
	cmd.Flags().Int64Var(&beforeID, "before-id", 0, "only entries older than this id (paging)")
	return cmd
}

// detailsText renders details as sorted key=value pairs.
func detailsText(d map[string]string) string {
	keys := make([]string, 0, len(d))
	for k, v := range d {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+d[k])
	}
	return strings.Join(parts, " ")
}
