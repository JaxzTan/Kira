// Package memstore is an in-memory auth.Store for tests (auth and kira-api).
package memstore

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Session is a stored session row.
type Session struct {
	Email, Sub, UserAgent string
	CreatedAt, ExpiresAt  time.Time
}

// AuthLogRow is one auth_log row.
type AuthLogRow struct {
	At                 time.Time
	Email, Sub, Result string
}

// ErrOwnerPinned is returned by PinOwner when an owner already exists.
var ErrOwnerPinned = errors.New("memstore: owner already pinned")

// Store implements auth.Store. The zero value is not usable; call New.
type Store struct {
	mu         sync.Mutex
	sessions   map[string]Session
	ownerSub   string
	ownerEmail string
	ownerSet   bool
	log        []AuthLogRow
}

// New returns an empty store.
func New() *Store { return &Store{sessions: map[string]Session{}} }

func (s *Store) CreateSession(_ context.Context, tokenSHA256, email, sub, userAgent string, createdAt, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[tokenSHA256]; ok {
		return errors.New("memstore: duplicate session")
	}
	s.sessions[tokenSHA256] = Session{Email: email, Sub: sub, UserAgent: userAgent, CreatedAt: createdAt, ExpiresAt: expiresAt}
	return nil
}

func (s *Store) GetSession(_ context.Context, tokenSHA256 string) (string, string, time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[tokenSHA256]
	return v.Email, v.Sub, v.ExpiresAt, ok, nil
}

func (s *Store) DeleteSession(_ context.Context, tokenSHA256 string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenSHA256)
	return nil
}

func (s *Store) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if !now.Before(v.ExpiresAt) {
			delete(s.sessions, k)
		}
	}
	return nil
}

func (s *Store) OwnerSub(context.Context) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ownerSub, s.ownerSet, nil
}

func (s *Store) PinOwner(_ context.Context, sub, email string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerSet {
		return ErrOwnerPinned
	}
	s.ownerSub, s.ownerEmail, s.ownerSet = sub, email, true
	return nil
}

func (s *Store) LogAuth(_ context.Context, at time.Time, email, sub, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, AuthLogRow{At: at, Email: email, Sub: sub, Result: result})
	return nil
}

// AuthLog returns a copy of all auth_log rows.
func (s *Store) AuthLog() []AuthLogRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AuthLogRow(nil), s.log...)
}

// Sessions returns a copy of all sessions keyed by token SHA-256.
func (s *Store) Sessions() map[string]Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Session, len(s.sessions))
	for k, v := range s.sessions {
		out[k] = v
	}
	return out
}
