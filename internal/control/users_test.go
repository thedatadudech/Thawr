package control

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestArgon2Params(t *testing.T) {
	hash, err := hashPassword("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("unexpected PHC prefix: %s", hash)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || len(parts[4]) < 20 {
		t.Errorf("salt missing or short: %s", hash)
	}
	ok, err := verifyPassword(hash, "secret-password")
	if err != nil || !ok {
		t.Errorf("verify correct: %v %v", ok, err)
	}
	ok, err = verifyPassword(hash, "wrong")
	if err != nil || ok {
		t.Errorf("verify wrong: %v %v", ok, err)
	}
	if _, err := verifyPassword("$bcrypt$x", "p"); err == nil {
		t.Error("foreign hash format should be rejected")
	}
	if strings.Contains(hash, "secret-password") {
		t.Error("hash contains the password")
	}
}

func TestUserCreateValidation(t *testing.T) {
	st := openStore(t)
	users, err := NewUsers(st, newClock().Now, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cases := []struct{ name, role, pw string }{
		{"Alice", "admin", "long enough"},
		{"", "admin", "long enough"},
		{"al ice", "admin", "long enough"},
		{"alice", "root", "long enough"},
		{"alice", "admin", "short"},
	}
	for _, c := range cases {
		if _, err := users.Create(ctx, c.name, c.role, c.pw); !errors.Is(err, ErrValidation) {
			t.Errorf("%+v: got %v, want ErrValidation", c, err)
		}
	}
	u, err := users.Create(ctx, "alice", "admin", "long enough")
	if err != nil || u.ID == "" || u.PasswordHash == "" {
		t.Fatalf("valid create: %+v %v", u, err)
	}
	if _, err := users.Create(ctx, "alice", "member", "long enough"); err == nil {
		t.Error("duplicate name accepted")
	}
}

func TestAuthenticate(t *testing.T) {
	st := openStore(t)
	users, err := NewUsers(st, newClock().Now, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mustUser(t, users, "alice", "member")
	if u, err := users.Authenticate(ctx, "alice", "correct horse battery"); err != nil || u.Name != "alice" {
		t.Errorf("good login: %+v %v", u, err)
	}
	if _, err := users.Authenticate(ctx, "alice", "wrong"); !errors.Is(err, ErrForbidden) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := users.Authenticate(ctx, "nobody", "correct horse battery"); !errors.Is(err, ErrForbidden) {
		t.Errorf("unknown user: %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	users, err := NewUsers(st, clk.Now, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mustUser(t, users, "alice", "member")
	for i := 0; i < loginThreshold; i++ {
		if _, err := users.Authenticate(ctx, "alice", "wrong"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := users.Authenticate(ctx, "alice", "correct horse battery"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("after %d failures: got %v, want ErrRateLimited", loginThreshold, err)
	}
	clk.Advance(2 * time.Second)
	if _, err := users.Authenticate(ctx, "alice", "wrong"); !errors.Is(err, ErrForbidden) {
		t.Errorf("after backoff a new attempt is allowed: %v", err)
	}
	// 11 failures: backoff 2^1 s, so 1 s later still blocked.
	clk.Advance(time.Second)
	if _, err := users.Authenticate(ctx, "alice", "correct horse battery"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("exponential backoff not applied: %v", err)
	}
	clk.Advance(loginWindow)
	if u, err := users.Authenticate(ctx, "alice", "correct horse battery"); err != nil || u.Name != "alice" {
		t.Errorf("window expired, login should succeed: %v", err)
	}
	if _, err := users.Authenticate(ctx, "bob", "x"); errors.Is(err, ErrRateLimited) {
		t.Error("limit leaked to another user")
	}
}
