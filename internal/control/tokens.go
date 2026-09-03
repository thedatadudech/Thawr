package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/thedatadudech/thawr/internal/store"
)

// Token lifetime policy.
const (
	DefaultTokenTTL = time.Hour
	MaxTokenTTL     = 30 * 24 * time.Hour
	// TokenPrefix marks enrollment token secrets.
	TokenPrefix = "thawr_"
	tokenIDLen  = 8 // hex characters after "tk_"
)

// TokenRequest describes a token to create.
type TokenRequest struct {
	OwnerName string
	Kind      string
	Tags      []string
	PeerName  string
	TTL       time.Duration
}

// CreatedToken is the result of Create; Secret is shown once.
type CreatedToken struct {
	Token  store.Token
	Secret string
}

// Tokens issues and lists one-time enrollment tokens.
type Tokens struct {
	store *store.Store
	now   func() time.Time
	log   *slog.Logger
}

// NewTokens builds the token service.
func NewTokens(st *store.Store, now func() time.Time, log *slog.Logger) *Tokens {
	return &Tokens{store: st, now: now, log: log}
}

// Create issues a token. Members may only create tokens for themselves.
func (t *Tokens) Create(ctx context.Context, by Principal, req TokenRequest) (CreatedToken, error) {
	owner, err := t.store.Users().GetByName(ctx, req.OwnerName)
	if errors.Is(err, store.ErrNotFound) {
		return CreatedToken{}, fmt.Errorf("%w: owner %q does not exist", ErrValidation, req.OwnerName)
	}
	if err != nil {
		return CreatedToken{}, err
	}
	if !by.IsAdmin() && owner.ID != by.UserID {
		return CreatedToken{}, fmt.Errorf("%w: members can only create tokens for themselves", ErrForbidden)
	}
	if err := validKind(req.Kind); err != nil {
		return CreatedToken{}, err
	}
	tags, err := normalizeTags(req.Tags)
	if err != nil {
		return CreatedToken{}, err
	}
	if req.PeerName != "" && !validLabel(req.PeerName) {
		return CreatedToken{}, fmt.Errorf("%w: peer name %q must be a DNS label", ErrValidation, req.PeerName)
	}
	ttl := req.TTL
	switch {
	case ttl == 0:
		ttl = DefaultTokenTTL
	case ttl < 0:
		return CreatedToken{}, fmt.Errorf("%w: expiry must be positive", ErrValidation)
	case ttl > MaxTokenTTL:
		return CreatedToken{}, fmt.Errorf("%w: expiry exceeds the maximum of %s", ErrValidation, MaxTokenTTL)
	}
	id, err := newID()
	if err != nil {
		return CreatedToken{}, err
	}
	secret, err := newSecret()
	if err != nil {
		return CreatedToken{}, err
	}
	secret = TokenPrefix + secret
	creator := by.UserID
	if by.Local {
		creator = owner.ID // the socket has no user record; attribute to the owner
	}
	now := t.now()
	tok := store.Token{
		ID:         "tk_" + id[:tokenIDLen],
		SecretHash: hashSecret(secret),
		OwnerID:    owner.ID,
		Kind:       req.Kind,
		Tags:       tags,
		PeerName:   req.PeerName,
		CreatedBy:  creator,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}
	if err := t.store.Tokens().Create(ctx, tok); err != nil {
		return CreatedToken{}, err
	}
	t.log.Info("token created", "token", tok.ID, "owner", owner.Name, "kind", tok.Kind, "expires_at", tok.ExpiresAt, "by", by.Name)
	return CreatedToken{Token: tok, Secret: secret}, nil
}

// List returns tokens visible to the principal: all for admins, own for
// members.
func (t *Tokens) List(ctx context.Context, by Principal) ([]store.Token, error) {
	all, err := t.store.Tokens().List(ctx)
	if err != nil {
		return nil, err
	}
	if by.IsAdmin() {
		return all, nil
	}
	var own []store.Token
	for _, tok := range all {
		if tok.OwnerID == by.UserID {
			own = append(own, tok)
		}
	}
	return own, nil
}

// Revoke deletes a token. Members may only revoke their own.
func (t *Tokens) Revoke(ctx context.Context, by Principal, id string) error {
	if !by.IsAdmin() {
		own, err := t.List(ctx, by)
		if err != nil {
			return err
		}
		found := false
		for _, tok := range own {
			if tok.ID == id {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("token %s: %w", id, ErrNotFound)
		}
	}
	if err := t.store.Tokens().Delete(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("token %s: %w", id, ErrNotFound)
		}
		return err
	}
	t.log.Info("token revoked", "token", id, "by", by.Name)
	return nil
}

func validKind(kind string) error {
	switch kind {
	case store.KindHuman, store.KindServer, store.KindAgent:
		return nil
	}
	return fmt.Errorf("%w: kind %q must be human, server or agent", ErrValidation, kind)
}

// normalizeTags validates "tag:<label>" entries and removes duplicates.
func normalizeTags(tags []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		label, ok := strings.CutPrefix(tag, "tag:")
		if !ok || !validLabel(label) {
			return nil, fmt.Errorf("%w: tag %q must be tag:<label>", ErrValidation, tag)
		}
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out, nil
}
