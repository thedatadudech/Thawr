package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/thedatadudech/thawr/internal/store"
)

// Registry lists and manages registered peers.
type Registry struct {
	store *store.Store
	log   *slog.Logger
}

// NewRegistry builds the registry service.
func NewRegistry(st *store.Store, log *slog.Logger) *Registry {
	return &Registry{store: st, log: log}
}

// List returns peers visible to the principal: all for admins, own for
// members.
func (r *Registry) List(ctx context.Context, by Principal) ([]store.Peer, error) {
	all, err := r.store.Peers().List(ctx)
	if err != nil {
		return nil, err
	}
	if by.IsAdmin() {
		return all, nil
	}
	var own []store.Peer
	for _, p := range all {
		if p.OwnerID == by.UserID {
			own = append(own, p)
		}
	}
	return own, nil
}

// Get returns one peer by name, subject to the same visibility as List.
func (r *Registry) Get(ctx context.Context, by Principal, name string) (store.Peer, error) {
	p, err := r.store.Peers().GetByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !by.IsAdmin() && p.OwnerID != by.UserID) {
		return store.Peer{}, fmt.Errorf("peer %q: %w", name, ErrNotFound)
	}
	return p, err
}

// Rename changes a peer's name (admins only).
func (r *Registry) Rename(ctx context.Context, by Principal, name, newName string) error {
	if !by.IsAdmin() {
		return ErrForbidden
	}
	if !validLabel(newName) {
		return fmt.Errorf("%w: name %q must be a DNS label", ErrValidation, newName)
	}
	p, err := r.Get(ctx, by, name)
	if err != nil {
		return err
	}
	err = r.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.Peers().Rename(ctx, p.ID, newName); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return fmt.Errorf("%w: name %q is taken", ErrValidation, newName)
			}
			return err
		}
		_, err := tx.Meta().IncrementGeneration(ctx)
		return err
	})
	if err != nil {
		return err
	}
	r.log.Info("peer renamed", "peer_id", p.ID, "from", name, "to", newName, "by", by.Name)
	return nil
}

// Delete removes a peer (admins only) and bumps the netmap generation so
// every client drops it. Closing its streams is spec 003's job.
func (r *Registry) Delete(ctx context.Context, by Principal, name string) error {
	if !by.IsAdmin() {
		return ErrForbidden
	}
	p, err := r.Get(ctx, by, name)
	if err != nil {
		return err
	}
	err = r.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.Peers().Delete(ctx, p.ID); err != nil {
			return err
		}
		_, err := tx.Meta().IncrementGeneration(ctx)
		return err
	})
	if err != nil {
		return err
	}
	r.log.Info("peer deleted", "peer", name, "peer_id", p.ID, "by", by.Name)
	return nil
}

// Generation returns the current netmap generation.
func (r *Registry) Generation(ctx context.Context) (int64, error) {
	return r.store.Meta().Generation(ctx)
}
