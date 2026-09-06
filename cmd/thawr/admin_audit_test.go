package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// fakeAuditAPI serves two entries and echoes the query it received.
func fakeAuditAPI(t *testing.T) (sock string, lastQuery func() string) {
	t.Helper()
	var q string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		if r.URL.Query().Get("since") == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "since: must be an RFC 3339 time or a positive duration such as 24h"})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 7, "at": "2026-09-06T12:03:00Z", "actor": "markus", "actor_role": "admin", "action": "peer.rename", "target": "p_1", "details": map[string]string{"to": "nas2", "from": "nas"}},
			{"id": 6, "at": "2026-09-06T12:00:00Z", "actor": "peer:nas", "actor_role": "peer", "action": "peer.rotate_key", "target": "p_1", "details": map[string]string{"key": "abcd1234", "generation": "9", "name": ""}},
		})
	})
	return fakeDaemonSocket(t, mux), func() string { return q }
}

func TestAdminAudit(t *testing.T) {
	sock, query := fakeAuditAPI(t)
	out, code, err := runCLI(t, "admin", "audit", "--socket", sock)
	if code != 0 {
		t.Fatalf("exit %d: %v", code, err)
	}
	for _, want := range []string{"ID  TIME", "ACTOR", "ACTION", "TARGET", "DETAILS", "7   2026-09-06T12:03:00Z  markus    peer.rename      p_1     from=nas to=nas2", "peer:nas  peer.rotate_key  p_1     generation=9 key=abcd1234"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if query() != "" {
		t.Errorf("query without flags: %q", query())
	}
	out, code, _ = runCLI(t, "admin", "audit", "--since", "24h", "--action", "peer.rename", "--actor", "markus", "--limit", "5", "--before-id", "9", "--json", "--socket", sock)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var entries []auditJSON
	if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) != 2 || entries[0].ID != 7 || entries[0].Details["to"] != "nas2" {
		t.Errorf("--json: %v %s", err, out)
	}
	if got := query(); got != "action=peer.rename&actor=markus&before_id=9&limit=5&since=24h" {
		t.Errorf("query: %q", got)
	}
	if _, code, err := runCLI(t, "admin", "audit", "--since", "bad", "--socket", sock); code == 0 || err == nil || !strings.Contains(err.Error(), "since:") {
		t.Errorf("server error not surfaced: code=%d err=%v", code, err)
	}
	if detailsText(nil) != "" || detailsText(map[string]string{"b": "2", "a": "1", "c": ""}) != "a=1 b=2" {
		t.Error("detailsText")
	}
}
