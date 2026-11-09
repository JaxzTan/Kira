package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Integrity event kinds.
const (
	IntegrityAcknowledge = "acknowledge"
)

// IntegrityEvent is one integrity_events row (append-only table, migration 0003).
// RecordedHead/ActualHead are "id:hash" (nil when unknown); Problems is JSON.
type IntegrityEvent struct {
	ID           int64
	At           string
	Kind         string
	RecordedHead *string
	ActualHead   *string
	Problems     string
}

// InsertIntegrityEvent appends an integrity_events row and returns its id.
// problemsJSON must be non-empty JSON (use "[]" for none).
func (s *Store) InsertIntegrityEvent(ctx context.Context, at time.Time, kind string, recordedHead, actualHead *string, problemsJSON string) (int64, error) {
	if kind == "" || problemsJSON == "" {
		return 0, fmt.Errorf("store: integrity event: %w", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO integrity_events (at, kind, recorded_head, actual_head, problems) VALUES (?, ?, ?, ?, ?)`,
		FormatTime(at), kind, nullable(recordedHead), nullable(actualHead), problemsJSON)
	if err != nil {
		return 0, fmt.Errorf("store: integrity event: %w", err)
	}
	return res.LastInsertId()
}

// RecentIntegrityEvents returns up to limit integrity_events rows, newest first.
func (s *Store) RecentIntegrityEvents(ctx context.Context, limit int) ([]IntegrityEvent, error) {
	if limit <= 0 {
		return []IntegrityEvent{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, kind, recorded_head, actual_head, problems FROM integrity_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: integrity events: %w", err)
	}
	defer rows.Close()
	out := []IntegrityEvent{}
	for rows.Next() {
		var e IntegrityEvent
		var rec, act sql.NullString
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &rec, &act, &e.Problems); err != nil {
			return nil, err
		}
		e.RecordedHead, e.ActualHead = nullStr(rec), nullStr(act)
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullable(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
