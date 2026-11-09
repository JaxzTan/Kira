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

// ReceiptVerifier re-hashes a stored receipt (*receipts.Store satisfies it).
// VerifyFile returns nil, receipts.ErrTampered (content swapped) or another
// error (missing/unreadable). If Config.Receipts also implements it, Verify
// re-hashes every distinct referenced receipt (TR-8).
type ReceiptVerifier interface {
	VerifyFile(sha256 string) error
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
	rverify  ReceiptVerifier
	now      func() time.Time
	loc      *time.Location

	mu         sync.Mutex // serialises Append (single writer, TR-4); see WithWriteLock
	appendHook func(Row)  // guarded by mu

	stateMu sync.Mutex
	last    *VerifyResult
	acked   map[string]struct{} // Problem.Key set covered by acknowledgements
	ackedAt string
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
	if rv, ok := cfg.Receipts.(ReceiptVerifier); ok {
		s.rverify = rv
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
	var ve *ValidationError
	var needConfirm bool
	var reason string
	switch req.Kind {
	case KindCreate, KindAmend:
		in, ve, needConfirm = validateInput(req.Input, s.today())
		if in.ReceiptSHA256 != nil && ve.Fields["receipt_sha256"] == "" && s.receipts != nil && !s.receipts.Exists(*in.ReceiptSHA256) {
			ve.add("receipt_sha256", "unknown receipt; upload it first")
		}
	case KindVoid:
		var err error
		if reason, err = ValidateReason(req.Reason); err != nil {
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
	// Load the target (amend/void) first: an amend may keep its (now
	// archived) category, so the category check depends on it.
	var target *Entry
	var targetErr error
	if req.Kind != KindCreate {
		var kind string
		err := tx.QueryRowContext(ctx, `SELECT kind FROM entries WHERE id = ?`, req.RefID).Scan(&kind)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			targetErr = ErrNotFound
		case err != nil:
			return AppendResult{}, fmt.Errorf("ledger: load target: %w", err)
		case kind != KindCreate:
			targetErr = ErrNotCreate
		default:
			e, err := getEntry(ctx, tx, req.RefID)
			if err != nil {
				return AppendResult{}, err
			}
			if e.Status == StatusVoided {
				targetErr = ErrVoided
			}
			target = &e
			ref := req.RefID
			row.RefID = &ref
		}
	}

	if req.Kind != KindVoid {
		// Category existence is checked even when other fields fail, so one
		// 422 lists every bad field.
		if in.CategoryID > 0 {
			var archived sql.NullString
			var catDir string
			err := tx.QueryRowContext(ctx, `SELECT archived_at, direction FROM categories WHERE id = ?`, in.CategoryID).Scan(&archived, &catDir)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				ve.add("category_id", "unknown category")
			case err != nil:
				return AppendResult{}, fmt.Errorf("ledger: load category: %w", err)
			case ValidDirection(in.Direction) && catDir != in.Direction:
				// FR-9: an entry's category must match its direction (also
				// on amend, so a direction change needs a matching category).
				if catDir == DirectionIncome {
					ve.add("category_id", "category is for income")
				} else {
					ve.add("category_id", "category is for expenses")
				}
			case archived.Valid && (target == nil || target.CategoryID != in.CategoryID):
				// An amend may keep an entry's (now archived) category; new choices must be active.
				ve.add("category_id", "category is archived")
			}
		}
		if err := ve.orNil(); err != nil {
			return AppendResult{}, err
		}
	}
	if targetErr != nil {
		return AppendResult{}, targetErr
	}
	if needConfirm {
		return AppendResult{}, ErrDateConfirmRequired
	}

	if req.Kind == KindVoid {
		row.Direction = target.Direction
		row.VoidReason = &reason
	} else {
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
	if s.appendHook != nil {
		s.appendHook(row) // still under s.mu: row is the chain tip
	}
	return AppendResult{Row: row, Entry: entry}, nil
}

// SetAppendHook registers fn to run after every committed append, while the
// write mutex is still held, so fn sees rows strictly in chain order and no
// other append or VerifyLocked can interleave (use it to advance the head
// file). The row is already durable when fn runs; fn cannot fail the append,
// so it should log its own errors. fn must not call back into Service write
// methods (deadlock). nil removes the hook.
func (s *Service) SetAppendHook(fn func(Row)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendHook = fn
}

// WithWriteLock runs fn while holding the ledger write mutex (no append,
// VerifyLocked or Acknowledge can run concurrently). fn must not call
// Append/Create/Amend/Void, VerifyLocked, Acknowledge, SetAppendHook or
// WithWriteLock. Plain reads and VerifyWith are fine.
func (s *Service) WithWriteLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
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
