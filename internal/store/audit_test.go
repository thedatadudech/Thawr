package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAuditAppendListPrune(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	entries := []AuditEntry{
		{At: base, Actor: "markus", ActorRole: RoleAdmin, Action: "token.create", Target: "tk_1", Details: map[string]string{"owner": "markus", "kind": "human"}},
		{At: base.Add(time.Minute), Actor: "peer:nas", ActorRole: "peer", Action: "peer.rotate_key", Target: "p1", Details: map[string]string{"key": "abcd1234"}},
		{At: base.Add(2 * time.Minute), Actor: "markus", ActorRole: RoleAdmin, Action: "peer.rename", Target: "p1", Details: map[string]string{"from": "nas", "to": "nas2"}},
		{At: base.Add(3 * time.Minute), Actor: "eve", ActorRole: "anonymous", Action: "login.failed", Target: "eve"},
	}
	for _, e := range entries {
		if err := s.Audit().Append(ctx, e); err != nil {
			t.Fatalf("append %s: %v", e.Action, err)
		}
	}
	if err := s.Audit().Append(ctx, AuditEntry{Actor: "x"}); err == nil {
		t.Error("entry without action accepted")
	}

	all, err := s.Audit().List(ctx, AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Action != "login.failed" || all[3].Action != "token.create" {
		t.Fatalf("order: %+v", all)
	}
	if all[3].Details["kind"] != "human" || !all[3].At.Equal(base) || all[3].ID == 0 || all[0].Details == nil || len(all[0].Details) != 0 {
		t.Errorf("round trip: %+v %+v", all[3], all[0])
	}

	cases := []struct {
		name string
		q    AuditQuery
		want []string
	}{
		{"limit", AuditQuery{Limit: 2}, []string{"login.failed", "peer.rename"}},
		{"since", AuditQuery{Since: base.Add(2 * time.Minute)}, []string{"login.failed", "peer.rename"}},
		{"before id", AuditQuery{BeforeID: all[1].ID}, []string{"peer.rotate_key", "token.create"}},
		{"action", AuditQuery{Action: "peer.rename"}, []string{"peer.rename"}},
		{"actor", AuditQuery{Actor: "markus"}, []string{"peer.rename", "token.create"}},
		{"combined", AuditQuery{Actor: "markus", Since: base.Add(time.Minute)}, []string{"peer.rename"}},
		{"none", AuditQuery{Action: "nothing"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Audit().List(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries, want %v", len(got), tc.want)
			}
			for i, e := range got {
				if e.Action != tc.want[i] {
					t.Errorf("entry %d: %s, want %s", i, e.Action, tc.want[i])
				}
			}
		})
	}
	if got, _ := s.Audit().List(ctx, AuditQuery{Limit: MaxAuditLimit + 5}); len(got) != 4 {
		t.Errorf("oversized limit: %d", len(got))
	}

	n, err := s.Audit().Prune(ctx, base.Add(2*time.Minute))
	if err != nil || n != 2 {
		t.Fatalf("prune: n=%d err=%v", n, err)
	}
	if left, _ := s.Audit().List(ctx, AuditQuery{}); len(left) != 2 || left[1].Action != "peer.rename" {
		t.Errorf("after prune: %+v", left)
	}
}

// TestAuditInTransaction: an entry appended inside a rolled-back
// transaction is gone with the mutation it described.
func TestAuditInTransaction(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	boom := errors.New("boom")
	err := s.InTx(ctx, func(tx *Store) error {
		if err := tx.Audit().Append(ctx, AuditEntry{Actor: "local", ActorRole: RoleAdmin, Action: "peer.delete", Target: "p1"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx: %v", err)
	}
	if got, _ := s.Audit().List(ctx, AuditQuery{}); len(got) != 0 {
		t.Errorf("rolled-back entry survived: %+v", got)
	}
	if err := s.InTx(ctx, func(tx *Store) error {
		return tx.Audit().Append(ctx, AuditEntry{Actor: "local", ActorRole: RoleAdmin, Action: "peer.delete", Target: "p1"})
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Audit().List(ctx, AuditQuery{}); len(got) != 1 {
		t.Errorf("committed entry missing: %+v", got)
	}
}
