package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// SessionTTL is how long an admin UI login stays valid.
const SessionTTL = 12 * time.Hour

// Session is one logged-in browser.
type Session struct {
	Token   string
	UserID  string
	CSRF    string
	Expires time.Time
}

// Sessions keeps admin UI sessions in memory; a restart logs everyone
// out, which is acceptable for v1.
type Sessions struct {
	now func() time.Time
	ttl time.Duration

	mu sync.Mutex
	m  map[string]Session
}

// NewSessions builds an empty session table.
func NewSessions(now func() time.Time) *Sessions {
	return &Sessions{now: now, ttl: SessionTTL, m: map[string]Session{}}
}

// Create starts a session for userID.
func (s *Sessions) Create(userID string) (Session, error) {
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	sess := Session{Token: token, UserID: userID, CSRF: csrf, Expires: s.now().Add(s.ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.m {
		if v.Expires.Before(now) {
			delete(s.m, k)
		}
	}
	s.m[token] = sess
	return sess, nil
}

// Get returns a live session.
func (s *Sessions) Get(token string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[token]
	if !ok || sess.Expires.Before(s.now()) {
		delete(s.m, token)
		return Session{}, false
	}
	return sess, true
}

// Delete ends a session.
func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, token)
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("api: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
