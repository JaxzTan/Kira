package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The methods in this file satisfy internal/auth.Store (primitives only, so
// there is no import cycle). Times are stored as UTC TimeFormat strings.

// CreateSession stores a new session keyed by the SHA-256 of its token.
func (s *Store) CreateSession(ctx context.Context, tokenSHA256, email, sub, userAgent string, createdAt, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_sha256, email, google_sub, created_at, expires_at, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		tokenSHA256, email, sub, FormatTime(createdAt), FormatTime(expiresAt), userAgent)
	if err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// GetSession looks up a session. found is false (with nil err) when absent.
// Expiry is not checked here; callers compare expiresAt with now.
func (s *Store) GetSession(ctx context.Context, tokenSHA256 string) (email, sub string, expiresAt time.Time, found bool, err error) {
	var exp sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT email, google_sub, expires_at FROM sessions WHERE token_sha256 = ?`, tokenSHA256).
		Scan(&email, &sub, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", time.Time{}, false, nil
	}
	if err != nil {
		return "", "", time.Time{}, false, fmt.Errorf("store: get session: %w", err)
	}
	if exp.Valid {
		expiresAt, err = ParseTime(exp.String)
		if err != nil {
			return "", "", time.Time{}, false, fmt.Errorf("store: session expires_at: %w", err)
		}
	}
	return email, sub, expiresAt, true, nil
}

// DeleteSession removes a session (logout). Deleting a missing session is not an error.
func (s *Store) DeleteSession(ctx context.Context, tokenSHA256 string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_sha256 = ?`, tokenSHA256); err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes sessions with expires_at <= now (and rows with no expiry).
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at IS NULL OR expires_at <= ?`, FormatTime(now)); err != nil {
		return fmt.Errorf("store: delete expired sessions: %w", err)
	}
	return nil
}

// OwnerSub returns the pinned Google sub, if any.
func (s *Store) OwnerSub(ctx context.Context) (sub string, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT google_sub FROM owner WHERE id = 1`).Scan(&sub)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: owner: %w", err)
	}
	return sub, true, nil
}

// PinOwner records the owner's sub once. Calling it again with the same sub
// is a no-op; with a different sub it returns ErrOwnerMismatch.
func (s *Store) PinOwner(ctx context.Context, sub, email string, at time.Time) error {
	if sub == "" {
		return fmt.Errorf("store: pin owner: empty sub: %w", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE
	if err != nil {
		return fmt.Errorf("store: pin owner: %w", err)
	}
	defer tx.Rollback()
	var got string
	err = tx.QueryRowContext(ctx, `SELECT google_sub FROM owner WHERE id = 1`).Scan(&got)
	switch {
	case err == nil:
		if got != sub {
			return ErrOwnerMismatch
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("store: pin owner: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO owner (id, google_sub, email, pinned_at) VALUES (1, ?, ?, ?)`,
		sub, email, FormatTime(at)); err != nil {
		return fmt.Errorf("store: pin owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: pin owner: %w", err)
	}
	return nil
}

// LogAuth appends an auth_log row (append-only table).
func (s *Store) LogAuth(ctx context.Context, at time.Time, email, sub, result string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_log (at, email, google_sub, result) VALUES (?, ?, ?, ?)`,
		FormatTime(at), email, sub, result); err != nil {
		return fmt.Errorf("store: log auth: %w", err)
	}
	return nil
}

// AuthLogEntry is one auth_log row.
type AuthLogEntry struct {
	ID     int64
	At     string
	Email  string
	Sub    string
	Result string
}

// RecentAuthLog returns up to limit auth_log rows, newest first.
func (s *Store) RecentAuthLog(ctx context.Context, limit int) ([]AuthLogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(at,''), COALESCE(email,''), COALESCE(google_sub,''), COALESCE(result,'') FROM auth_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: auth log: %w", err)
	}
	defer rows.Close()
	var out []AuthLogEntry
	for rows.Next() {
		var e AuthLogEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Email, &e.Sub, &e.Result); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
