package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func now() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }

func TestUsersCRUD(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u := User{ID: "u1", Name: "alice", Role: RoleMember, PasswordHash: "$argon2id$x", CreatedAt: now()}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Users().Create(ctx, User{ID: "u2", Name: "alice", Role: RoleAdmin, PasswordHash: "h", CreatedAt: now()}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name: got %v, want ErrConflict", err)
	}
	got, err := s.Users().GetByName(ctx, "alice")
	if err != nil || got.ID != "u1" || got.Role != RoleMember || !got.CreatedAt.Equal(now()) {
		t.Errorf("GetByName: %+v, %v", got, err)
	}
	if _, err := s.Users().GetByName(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing user: %v", err)
	}
	if got, err := s.Users().GetByID(ctx, "u1"); err != nil || got.Name != "alice" {
		t.Errorf("GetByID: %+v, %v", got, err)
	}
	list, err := s.Users().List(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("List: %v, %v", list, err)
	}
	if n, err := s.Users().Count(ctx); err != nil || n != 1 {
		t.Errorf("Count: %d, %v", n, err)
	}
}

func TestTokensCRUD(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.Users().Create(ctx, User{ID: "u1", Name: "alice", Role: RoleAdmin, PasswordHash: "h", CreatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	tok := Token{ID: "tk_1", SecretHash: "hash1", OwnerID: "u1", Kind: KindHuman, Tags: []string{"tag:dev"},
		PeerName: "laptop", CreatedBy: "u1", CreatedAt: now(), ExpiresAt: now().Add(time.Hour)}
	if err := s.Tokens().Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Tokens().Create(ctx, Token{ID: "tk_2", SecretHash: "hash1", OwnerID: "u1", Kind: KindHuman, CreatedBy: "u1", CreatedAt: now(), ExpiresAt: now()}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate hash: %v", err)
	}
	got, err := s.Tokens().GetByHash(ctx, "hash1")
	if err != nil || got.ID != "tk_1" || len(got.Tags) != 1 || got.PeerName != "laptop" || got.UsedAt != nil {
		t.Errorf("GetByHash: %+v, %v", got, err)
	}
	if _, err := s.Tokens().GetByHash(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing token: %v", err)
	}
	if err := s.Users().Create(ctx, User{ID: "u2", Name: "bob", Role: RoleMember, PasswordHash: "h", CreatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.Tokens().Create(ctx, Token{ID: "tk_3", SecretHash: "hash3", OwnerID: "u2", Kind: KindServer, CreatedBy: "u1", CreatedAt: now().Add(time.Second), ExpiresAt: now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Tokens().List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "tk_3" {
		t.Errorf("List newest first: %+v, %v", list, err)
	}
	if got.Tags == nil {
		t.Error("tags should decode to an empty slice, not nil")
	}
	if err := s.Tokens().Delete(ctx, "tk_3"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if err := s.Tokens().Delete(ctx, "tk_3"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete twice: %v", err)
	}
}

func TestTokenMarkUsedSingleUse(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.Users().Create(ctx, User{ID: "u1", Name: "alice", Role: RoleAdmin, PasswordHash: "h", CreatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.Tokens().Create(ctx, Token{ID: "tk_1", SecretHash: "h1", OwnerID: "u1", Kind: KindHuman, CreatedBy: "u1", CreatedAt: now(), ExpiresAt: now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// used_by_peer_id references peers, so the peer must exist first.
	if err := s.Peers().Create(ctx, Peer{ID: "peer", Name: "p", Kind: KindHuman, Mode: ModeAgent, PublicKey: "k", IPv4: "100.64.0.2", CreatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.InTx(ctx, func(tx *Store) error {
				return tx.Tokens().MarkUsed(ctx, "tk_1", "peer", now())
			})
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Errorf("MarkUsed won %d times, want exactly 1", wins)
	}
	got, _ := s.Tokens().GetByHash(ctx, "h1")
	if got.UsedAt == nil || got.UsedByPeerID != "peer" {
		t.Errorf("token not marked used: %+v", got)
	}
}

func TestPeersCRUD(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.Users().Create(ctx, User{ID: "u1", Name: "alice", Role: RoleAdmin, PasswordHash: "h", CreatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	p := Peer{ID: "p1", Name: "laptop", Kind: KindHuman, Mode: ModeAgent, OwnerID: "u1", Tags: []string{"tag:dev"},
		PublicKey: "pk1", IPv4: "100.64.0.2", NodeSecretHash: "ns1", CreatedAt: now()}
	if err := s.Peers().Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for name, dup := range map[string]Peer{
		"name": {ID: "p2", Name: "laptop", Kind: KindHuman, Mode: ModeAgent, PublicKey: "pk2", IPv4: "100.64.0.3", CreatedAt: now()},
		"key":  {ID: "p3", Name: "other", Kind: KindHuman, Mode: ModeAgent, PublicKey: "pk1", IPv4: "100.64.0.4", CreatedAt: now()},
		"ip":   {ID: "p4", Name: "other2", Kind: KindHuman, Mode: ModeAgent, PublicKey: "pk4", IPv4: "100.64.0.2", CreatedAt: now()},
	} {
		if err := s.Peers().Create(ctx, dup); !errors.Is(err, ErrConflict) {
			t.Errorf("duplicate %s: %v", name, err)
		}
	}
	if err := s.Peers().Create(ctx, Peer{ID: "p5", Name: "laptop-2", Kind: KindServer, Mode: ModeStatic, PublicKey: "pk5", IPv4: "100.64.0.5", CreatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Peers().GetByName(ctx, "laptop")
	if err != nil || got.OwnerID != "u1" || got.NodeSecretHash != "ns1" || len(got.Tags) != 1 {
		t.Errorf("GetByName: %+v, %v", got, err)
	}
	if got, err := s.Peers().GetByNodeSecretHash(ctx, "ns1"); err != nil || got.ID != "p1" {
		t.Errorf("GetByNodeSecretHash: %+v, %v", got, err)
	}
	if got, err := s.Peers().GetByID(ctx, "p5"); err != nil || got.OwnerID != "" || got.NodeSecretHash != "" {
		t.Errorf("static peer nulls: %+v, %v", got, err)
	}
	ips, err := s.Peers().AllocatedIPv4s(ctx)
	if err != nil || len(ips) != 2 {
		t.Errorf("AllocatedIPv4s: %v, %v", ips, err)
	}
	names, err := s.Peers().NamesWithPrefix(ctx, "laptop")
	if err != nil || len(names) != 2 {
		t.Errorf("NamesWithPrefix: %v, %v", names, err)
	}
	if names, _ := s.Peers().NamesWithPrefix(ctx, "lap"); len(names) != 0 {
		t.Errorf("prefix must match whole label: %v", names)
	}
	if err := s.Peers().Rename(ctx, "p1", "laptop-2"); !errors.Is(err, ErrConflict) {
		t.Errorf("rename to taken name: %v", err)
	}
	if err := s.Peers().Rename(ctx, "p1", "alice-laptop"); err != nil {
		t.Errorf("Rename: %v", err)
	}
	if err := s.Peers().Rename(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename missing: %v", err)
	}
	if err := s.Peers().Delete(ctx, "p1"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if err := s.Peers().Delete(ctx, "p1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete twice: %v", err)
	}
	if n, _ := s.Peers().Count(ctx); n != 1 {
		t.Errorf("Count after delete: %d", n)
	}
}

func TestInTxRollback(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	err := s.InTx(ctx, func(tx *Store) error {
		if err := tx.Users().Create(ctx, User{ID: "u1", Name: "alice", Role: RoleAdmin, PasswordHash: "h", CreatedAt: now()}); err != nil {
			return err
		}
		return errors.New("abort")
	})
	if err == nil || err.Error() != "abort" {
		t.Fatalf("InTx: %v", err)
	}
	if n, _ := s.Users().Count(ctx); n != 0 {
		t.Errorf("rolled back tx left %d users", n)
	}
	if err := s.InTx(ctx, func(tx *Store) error { return tx.InTx(ctx, func(*Store) error { return nil }) }); err == nil {
		t.Error("nested tx should fail")
	}
}

func TestGeneration(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if g, err := s.Meta().Generation(ctx); err != nil || g != 0 {
		t.Errorf("initial generation: %d, %v", g, err)
	}
	if g, err := s.Meta().IncrementGeneration(ctx); err != nil || g != 1 {
		t.Errorf("first increment: %d, %v", g, err)
	}
	if g, err := s.Meta().IncrementGeneration(ctx); err != nil || g != 2 {
		t.Errorf("second increment: %d, %v", g, err)
	}
	if g, _ := s.Meta().Generation(ctx); g != 2 {
		t.Errorf("generation after increments: %d", g)
	}
}
