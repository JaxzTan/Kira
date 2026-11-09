package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JaxzTan/kira/internal/store"
)

// ReceiptChecker reports whether a receipt has been uploaded
// (*receipts.Store satisfies it).
type ReceiptChecker interface {
	Exists(sha256 string) bool
}

// Config configures a Service.
type Config struct {
	Store    *store.Store
	Key      []byte         // LEDGER_KEY bytes (decoded from hex by config); >= 32 bytes
	Receipts ReceiptChecker // optional; nil = receipt_sha256 is only format-checked
	Now      func() time.Time
	Location *time.Location // "today" for the ±365-day rule; default time.Local
}

// Service is the single writer of the ledger.
type Service struct {
	st       *store.Store
	db       *sql.DB
	key      []byte
	receipts ReceiptChecker
	now      func() time.Time
	loc      *time.Location

	mu sync.Mutex // serialises Append (single writer, TR-4)

	stateMu sync.Mutex
	last    *VerifyResult
	ackKey  string
}

// New builds a Service. It does not verify; call Verify at startup (TR-7).
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("ledger: nil store")
	}
	if len(cfg.Key) < 32 {
		return nil, errors.New("ledger: LEDGER_KEY must be at least 32 bytes")
	}
	s := &Service{
		st:       cfg.Store,
		db:       cfg.Store.DB(),
		key:      append([]byte(nil), cfg.Key...),
		receipts: cfg.Receipts,
		now:      cfg.Now,
		loc:      cfg.Location,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.loc == nil {
		s.loc = time.Local
	}
	return s, nil
}

func (s *Service) today() time.Time { return s.now().In(s.loc) }

// Create appends a create row.
func (s *Service) Create(ctx context.Context, in Input) (AppendResult, error) {
	return s.Append(ctx, Request{Kind: KindCreate, Input: in})
}

// Amend appends a full corrected version of original create id.
func (s *Service) Amend(ctx context.Context, id int64, in Input) (AppendResult, error) {
	return s.Append(ctx, Request{Kind: KindAmend, RefID: id, Input: in})
}

// Void appends a void row for original create id.
func (s *Service) Void(ctx context.Context, id int64, reason string) (AppendResult, error) {
	return s.Append(ctx, Request{Kind: KindVoid, RefID: id, Reason: reason})
}

// Append is the only write path for entries. It returns only after COMMIT.
//
// Errors: ErrLedgerBroken, *ValidationError, ErrDateConfirmRequired,
// ErrNotFound, ErrNotCreate, ErrVoided, or a wrapped DB error.
func (s *Service) Append(ctx context.Context, req Request) (AppendResult, error) {
	if s.WritesBlocked() {
		return AppendResult{}, ErrLedgerBroken
	}
	var in Input
	var reason string
	var err error
	switch req.Kind {
	case KindCreate, KindAmend:
		in, err = ValidateInput(req.Input, s.today())
		if err != nil {
			return AppendResult{}, err
		}
		if in.ReceiptSHA256 != nil && s.receipts != nil && !s.receipts.Exists(*in.ReceiptSHA256) {
			return AppendResult{}, &ValidationError{Fields: map[string]string{"receipt_sha256": "unknown receipt; upload it first"}}
		}
	case KindVoid:
		reason, err = ValidateReason(req.Reason)
		if err != nil {
			return AppendResult{}, err
		}
	default:
		return AppendResult{}, fmt.Errorf("ledger: unknown kind %q", req.Kind)
	}
	if req.Kind != KindCreate && req.RefID <= 0 {
		return AppendResult{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.WritesBlocked() {
		return AppendResult{}, ErrLedgerBroken
	}

	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE via DSN _txlock
	if err != nil {
		return AppendResult{}, fmt.Errorf("ledger: begin: %w", err)
	}
	defer tx.Rollback()

	row := Row{Kind: req.Kind, Currency: CurrencyMYR, Direction: DirectionExpense}
	var target *Entry
	if req.Kind != KindCreate {
		var kind string
		err := tx.QueryRowContext(ctx, `SELECT kind FROM entries WHERE id = ?`, req.RefID).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			return AppendResult{}, ErrNotFound
		}
		if err != nil {
			return AppendResult{}, fmt.Errorf("ledger: load target: %w", err)
		}
		if kind != KindCreate {
			return AppendResult{}, ErrNotCreate
		}
		e, err := getEntry(ctx, tx, req.RefID)
		if err != nil {
			return AppendResult{}, err
		}
		if e.Status == StatusVoided {
			return AppendResult{}, ErrVoided
		}
		target = &e
		ref := req.RefID
		row.RefID = &ref
	}

	if req.Kind == KindVoid {
		row.Direction = target.Direction
		row.VoidReason = &reason
	} else {
		var archived sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT archived_at FROM categories WHERE id = ?`, in.CategoryID).Scan(&archived)
		if errors.Is(err, sql.ErrNoRows) {
			return AppendResult{}, &ValidationError{Fields: map[string]string{"category_id": "unknown category"}}
		}
		if err != nil {
			return AppendResult{}, fmt.Errorf("ledger: load category: %w", err)
		}
		// An amend may keep an entry's (now archived) category; new choices must be active.
		if archived.Valid && (target == nil || target.CategoryID != in.CategoryID) {
			return AppendResult{}, &ValidationError{Fields: map[string]string{"category_id": "category is archived"}}
		}
		amt, cat, date, payee, method := in.AmountSen, in.CategoryID, in.OccurredOn, in.Payee, in.Method
		row.Direction = in.Direction
		row.OccurredOn, row.AmountSen, row.CategoryID = &date, &amt, &cat
		row.Payee, row.Method, row.Note, row.ReceiptSHA256 = &payee, &method, in.Note, in.ReceiptSHA256
	}

	head, err := headOf(ctx, tx)
	if err != nil {
		return AppendResult{}, err
	}
	row.ID = head.ID + 1
	row.PrevHash = head.Hash
	row.CreatedAt = store.FormatTime(s.now())
	if row.Hash, err = RowHash(s.key, row); err != nil {
		return AppendResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entries
		(id, kind, ref_id, direction, occurred_on, amount_sen, currency, category_id, payee, method, note, void_reason, receipt_sha256, created_at, prev_hash, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Kind, ptrVal(row.RefID), row.Direction, ptrVal(row.OccurredOn), ptrVal(row.AmountSen), row.Currency,
		ptrVal(row.CategoryID), ptrVal(row.Payee), ptrVal(row.Method), ptrVal(row.Note), ptrVal(row.VoidReason),
		ptrVal(row.ReceiptSHA256), row.CreatedAt, row.PrevHash, row.Hash); err != nil {
		return AppendResult{}, fmt.Errorf("ledger: insert: %w", err)
	}
	entryID := row.ID
	if row.RefID != nil {
		entryID = *row.RefID
	}
	entry, err := getEntry(ctx, tx, entryID)
	if err != nil {
		return AppendResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AppendResult{}, fmt.Errorf("ledger: commit: %w", err)
	}
	return AppendResult{Row: row, Entry: entry}, nil
}

type queryer interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func headOf(ctx context.Context, q queryer) (Head, error) {
	var h Head
	err := q.QueryRowContext(ctx, `SELECT id, hash FROM entries ORDER BY id DESC LIMIT 1`).Scan(&h.ID, &h.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return Head{ID: 0, Hash: GenesisHash}, nil
	}
	if err != nil {
		return Head{}, fmt.Errorf("ledger: head: %w", err)
	}
	return h, nil
}

// Head returns the chain tip (0 and GenesisHash when empty), for the startup log.
func (s *Service) Head(ctx context.Context) (Head, error) { return headOf(ctx, s.db) }

// WritesBlocked reports whether the last Verify failed and was not acknowledged.
func (s *Service) WritesBlocked() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.last != nil && !s.last.OK && !s.last.Acknowledged
}

// LastVerify returns the most recent Verify result, if any.
func (s *Service) LastVerify() (VerifyResult, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.last == nil {
		return VerifyResult{}, false
	}
	return *s.last, true
}

// Acknowledge re-enables writes after a failed Verify (TR-7). The
// acknowledgement covers that exact set of problems: a later Verify that
// finds a different set (new tampering) blocks writes again. Returns the
// (updated) last result; ok=false if Verify has never run.
func (s *Service) Acknowledge() (VerifyResult, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.last == nil {
		return VerifyResult{}, false
	}
	if !s.last.OK {
		s.ackKey = problemsKey(s.last.Problems)
		s.last.Acknowledged = true
	}
	return *s.last, true
}

func (s *Service) record(res *VerifyResult) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if res.OK {
		s.ackKey = ""
	} else if s.ackKey != "" && s.ackKey == problemsKey(res.Problems) {
		res.Acknowledged = true
	}
	cp := *res
	s.last = &cp
}
