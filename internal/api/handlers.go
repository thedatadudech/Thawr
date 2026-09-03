package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/thedatadudech/thawr/internal/control"
	"github.com/thedatadudech/thawr/internal/store"
)

const (
	timeFormat  = time.RFC3339
	maxBodySize = 64 << 10
)

// UsersService is what the REST layer needs from control.Users.
type UsersService interface {
	Create(ctx context.Context, name, role, password string) (store.User, error)
	List(ctx context.Context) ([]store.User, error)
	Get(ctx context.Context, id string) (store.User, error)
}

// Authenticator verifies a login.
type Authenticator interface {
	Authenticate(ctx context.Context, name, password string) (store.User, error)
}

// TokensService is what the REST layer needs from control.Tokens.
type TokensService interface {
	Create(ctx context.Context, by control.Principal, req control.TokenRequest) (control.CreatedToken, error)
	List(ctx context.Context, by control.Principal) ([]store.Token, error)
	Revoke(ctx context.Context, by control.Principal, id string) error
}

// PeersService is what the REST layer needs from control.Registry.
type PeersService interface {
	List(ctx context.Context, by control.Principal) ([]store.Peer, error)
	Get(ctx context.Context, by control.Principal, name string) (store.Peer, error)
	Rename(ctx context.Context, by control.Principal, name, newName string) error
	Delete(ctx context.Context, by control.Principal, name string) error
}

// JoinInfo is what a new client needs besides the token: where the
// server is and which certificate to trust.
type JoinInfo struct {
	ServerURL   string
	Fingerprint string
}

// JoinCommand renders the one-line client command for a token secret.
func (j JoinInfo) JoinCommand(secret string) string {
	return fmt.Sprintf("thawr client up --server %s --token %s --fingerprint %s", j.ServerURL, secret, j.Fingerprint)
}

func readJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxBodySize {
		return errors.New("body too large")
	}
	if len(body) == 0 {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// writeControlError maps control and store errors to HTTP codes.
func (h *rest) writeControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrValidation):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), control.ErrValidation.Error()+": "))
	case errors.Is(err, control.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, control.ErrNotFound), errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, control.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		h.deps.Logger.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// --- users ---

func (h *rest) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.deps.Users.List(r.Context())
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	out := make([]userView, 0, len(users))
	for _, u := range users {
		out = append(out, newUserView(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *rest) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := h.deps.Users.Create(r.Context(), body.Name, body.Role, body.Password)
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newUserView(u))
}

// --- tokens ---

type tokenView struct {
	ID        string   `json:"id"`
	Owner     string   `json:"owner"`
	Kind      string   `json:"kind"`
	Tags      []string `json:"tags"`
	PeerName  string   `json:"peer_name,omitempty"`
	CreatedAt string   `json:"created_at"`
	ExpiresAt string   `json:"expires_at"`
	UsedAt    string   `json:"used_at,omitempty"`
	UsedBy    string   `json:"used_by_peer_id,omitempty"`
}

// createdTokenView is returned exactly once; it is the only place the
// secret and the join command appear.
type createdTokenView struct {
	tokenView
	Secret      string `json:"secret"`
	JoinCommand string `json:"join_command"`
}

func (h *rest) tokenView(ctx context.Context, t store.Token, names map[string]string) tokenView {
	v := tokenView{ID: t.ID, Owner: h.userName(ctx, t.OwnerID, names), Kind: t.Kind, Tags: t.Tags, PeerName: t.PeerName,
		CreatedAt: t.CreatedAt.UTC().Format(timeFormat), ExpiresAt: t.ExpiresAt.UTC().Format(timeFormat), UsedBy: t.UsedByPeerID}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if t.UsedAt != nil {
		v.UsedAt = t.UsedAt.UTC().Format(timeFormat)
	}
	return v
}

// userName resolves an id to a name with a per-request cache.
func (h *rest) userName(ctx context.Context, id string, cache map[string]string) string {
	if id == "" {
		return ""
	}
	if n, ok := cache[id]; ok {
		return n
	}
	u, err := h.deps.Users.Get(ctx, id)
	name := id
	if err == nil {
		name = u.Name
	}
	cache[id] = name
	return name
}

func (h *rest) handleListTokens(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tokens, err := h.deps.Tokens.List(r.Context(), p)
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	names := map[string]string{}
	out := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, h.tokenView(r.Context(), t, names))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *rest) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var body struct {
		Owner    string   `json:"owner"`
		Kind     string   `json:"kind"`
		Tags     []string `json:"tags"`
		PeerName string   `json:"peer_name"`
		Expires  string   `json:"expires"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ttl, err := ParseTTL(body.Expires)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.deps.Tokens.Create(r.Context(), p, control.TokenRequest{
		OwnerName: body.Owner, Kind: body.Kind, Tags: body.Tags, PeerName: body.PeerName, TTL: ttl,
	})
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createdTokenView{
		tokenView:   h.tokenView(r.Context(), created.Token, map[string]string{}),
		Secret:      created.Secret,
		JoinCommand: h.deps.Join.JoinCommand(created.Secret),
	})
}

func (h *rest) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	if err := h.deps.Tokens.Revoke(r.Context(), p, r.PathValue("id")); err != nil {
		h.writeControlError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ParseTTL accepts Go durations plus a "d" suffix for days; empty means
// the default.
func ParseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("expires %q: use a duration like 1h, 30m or 7d", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("expires %q: use a duration like 1h, 30m or 7d", s)
	}
	return d, nil
}

// --- peers ---

type peerView struct {
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

func (h *rest) peerView(ctx context.Context, p store.Peer, names map[string]string) peerView {
	v := peerView{ID: p.ID, Name: p.Name, Kind: p.Kind, Mode: p.Mode, Owner: h.userName(ctx, p.OwnerID, names), Tags: p.Tags,
		PublicKey: p.PublicKey, IPv4: p.IPv4, CreatedAt: p.CreatedAt.UTC().Format(timeFormat)}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if p.LastSeenAt != nil {
		v.LastSeenAt = p.LastSeenAt.UTC().Format(timeFormat)
	}
	return v
}

func (h *rest) handleListPeers(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	peers, err := h.deps.Peers.List(r.Context(), p)
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	names := map[string]string{}
	out := make([]peerView, 0, len(peers))
	for _, peer := range peers {
		out = append(out, h.peerView(r.Context(), peer, names))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *rest) handleGetPeer(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	peer, err := h.deps.Peers.Get(r.Context(), p, r.PathValue("name"))
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.peerView(r.Context(), peer, map[string]string{}))
}

func (h *rest) handleRenamePeer(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.deps.Peers.Rename(r.Context(), p, r.PathValue("name"), body.Name); err != nil {
		h.writeControlError(w, err)
		return
	}
	peer, err := h.deps.Peers.Get(r.Context(), p, body.Name)
	if err != nil {
		h.writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.peerView(r.Context(), peer, map[string]string{}))
}

func (h *rest) handleDeletePeer(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	if err := h.deps.Peers.Delete(r.Context(), p, r.PathValue("name")); err != nil {
		h.writeControlError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
