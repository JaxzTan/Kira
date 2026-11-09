// Package store opens Kira's SQLite database, applies migrations, and holds
// the non-ledger tables: categories, sessions, owner and auth_log.
//
// Ledger rows (table entries) are written only by internal/ledger.Service.
//
// Connection settings (TR-5, CLAUDE.md rule 4), applied to every pooled
// connection via the DSN: busy_timeout=5000, journal_mode=WAL,
// synchronous=FULL, foreign_keys=ON, fullfsync/checkpoint_fullfsync=ON
// (macOS: F_FULLFSYNC so commits reach the platter; no-op elsewhere), and
// _txlock=immediate so every
// read-write transaction starts with BEGIN IMMEDIATE (read-only
// transactions, sql.TxOptions{ReadOnly: true}, use a plain BEGIN).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JaxzTan/kira/migrations"
	_ "modernc.org/sqlite" // pure-Go SQLite driver ("sqlite")
)

// TimeFormat is the fixed-width UTC timestamp format stored in TEXT columns.
// Fixed width keeps lexical order equal to time order.
const TimeFormat = "2006-01-02T15:04:05Z"

// FormatTime renders t as UTC, truncated to seconds, in TimeFormat.
func FormatTime(t time.Time) string { return t.UTC().Truncate(time.Second).Format(TimeFormat) }

// ParseTime parses a TimeFormat (RFC 3339) timestamp.
func ParseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// Sentinel errors.
var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicateName = errors.New("category name already exists")
	ErrInvalid       = errors.New("invalid input")
	ErrOwnerMismatch = errors.New("owner already pinned to a different sub")
)

// Store wraps the SQLite handle.
type Store struct {
	db   *sql.DB
	path string
}

// DSN returns the modernc.org/sqlite DSN used for path.
func DSN(path string) string {
	return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_pragma=fullfsync(1)&_pragma=checkpoint_fullfsync(1)&_txlock=immediate"
}

// Open opens (creating if needed) the database at path, checks the PRAGMAs
// took effect, and applies pending migrations. The parent directory is
// created with mode 0700 and the DB file is chmod 0600 (TR-12).
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") || strings.HasPrefix(path, "file:") || path == ":memory:" {
		return nil, fmt.Errorf("store: unsupported database path %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.checkPragmas(ctx); err != nil {
		db.Close()
		return nil, err
	}
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(f); err == nil {
			_ = os.Chmod(f, 0o600)
		}
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the handle for internal/ledger and tests. Do not write to
// entries through it; use ledger.Service.Append.
func (s *Store) DB() *sql.DB { return s.db }

// Path is the database file path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) checkPragmas(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: connect: %w", err)
	}
	defer conn.Close()
	var mode string
	var syncLevel, fk, busy int
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("store: pragma journal_mode: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&syncLevel); err != nil {
		return fmt.Errorf("store: pragma synchronous: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("store: pragma foreign_keys: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		return fmt.Errorf("store: pragma busy_timeout: %w", err)
	}
	if !strings.EqualFold(mode, "wal") || syncLevel != 2 || fk != 1 || busy != 5000 {
		return fmt.Errorf("store: pragmas not applied (journal_mode=%s synchronous=%d foreign_keys=%d busy_timeout=%d)", mode, syncLevel, fk, busy)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	ms, err := migrations.All()
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("store: schema_migrations: %w", err)
	}
	for _, m := range ms {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %s: %w", m.Name, err)
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&n); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: check migration %s: %w", m.Name, err)
		}
		if n > 0 {
			tx.Rollback()
			continue
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: apply migration %s: %w", m.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`, m.Version, m.Name, FormatTime(time.Now())); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: record migration %s: %w", m.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", m.Name, err)
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

// IsAppendOnlyError reports whether err came from an append-only/insert-once trigger.
func IsAppendOnlyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "append-only") || strings.Contains(msg, "insert-once")
}
