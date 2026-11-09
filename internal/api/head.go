package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/JaxzTan/kira/internal/ledger"
)

// HeadFile persists the last trusted ledger head as "id:hash\n" (mode 0600)
// so deleted tail rows are detected across restarts (TRD §4, TR-7).
//
// Rules:
//   - written (atomically: temp file + fsync + rename) after every verify that
//     returns ok, after every successful append that extends the recorded head
//     by exactly one row, and after the owner acknowledges a broken ledger;
//   - never written by a failed, unacknowledged verify, so the evidence of a
//     tail deletion survives restarts;
//   - a missing file means "first run": verify runs without a head check.
type HeadFile struct {
	path string
	mu   sync.Mutex
	cur  *ledger.Head // last value read or written
}

// NewHeadFile returns a HeadFile at path (DATA_DIR/head).
func NewHeadFile(path string) *HeadFile { return &HeadFile{path: path} }

// Path returns the file path.
func (h *HeadFile) Path() string { return h.path }

// Read returns the recorded head, or nil if the file does not exist.
func (h *HeadFile) Read() (*ledger.Head, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.readLocked()
}

func (h *HeadFile) readLocked() (*ledger.Head, error) {
	b, err := os.ReadFile(h.path)
	if errors.Is(err, os.ErrNotExist) {
		h.cur = nil
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("head file: %w", err)
	}
	hd, err := ParseHead(string(b))
	if err != nil {
		return nil, err
	}
	h.cur = &hd
	return &hd, nil
}

// ParseHead parses "id:hash" (surrounding whitespace ignored).
func ParseHead(s string) (ledger.Head, error) {
	idS, hash, ok := strings.Cut(strings.TrimSpace(s), ":")
	id, err := strconv.ParseInt(idS, 10, 64)
	if !ok || err != nil || id < 0 || !ledger.IsSHA256Hex(hash) {
		return ledger.Head{}, errors.New("head file: malformed (want id:hash)")
	}
	return ledger.Head{ID: id, Hash: hash}, nil
}

// FormatHead renders "id:hash".
func FormatHead(hd ledger.Head) string { return strconv.FormatInt(hd.ID, 10) + ":" + hd.Hash }

// Write records hd unconditionally.
func (h *HeadFile) Write(hd ledger.Head) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writeLocked(hd)
}

// Advance records the head after a successful append of row, but only if row
// directly follows the recorded head (same prev_hash). A row that does not
// (concurrent appends finishing out of order, or the chain tip changed outside
// the app) leaves the file alone; the next ok verify records the real head.
// Returns whether the file was written.
func (h *HeadFile) Advance(row ledger.Row) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cur := h.cur
	if cur == nil {
		var err error
		if cur, err = h.readLocked(); err != nil {
			return false, err
		}
	}
	if cur != nil && (row.ID != cur.ID+1 || row.PrevHash != cur.Hash) {
		return false, nil
	}
	if cur == nil && (row.ID != 1 || row.PrevHash != ledger.GenesisHash) {
		return false, nil
	}
	return true, h.writeLocked(ledger.Head{ID: row.ID, Hash: row.Hash})
}

func (h *HeadFile) writeLocked(hd ledger.Head) error {
	dir := filepath.Dir(h.path)
	f, err := os.CreateTemp(dir, ".head-*")
	if err != nil {
		return fmt.Errorf("head file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("head file: %w", err)
	}
	if _, err := f.WriteString(FormatHead(hd) + "\n"); err != nil {
		f.Close()
		return fmt.Errorf("head file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("head file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("head file: %w", err)
	}
	if err := os.Rename(tmp, h.path); err != nil {
		return fmt.Errorf("head file: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	cp := hd
	h.cur = &cp
	return nil
}

// VerifyRecorded runs a full verify against the recorded head (if any),
// records the new head when the chain is ok, and logs the outcome: head
// id:hash at INFO, failures at WARN (TR-7). Used by `kira serve` at
// startup, `kira verify`, and GET /api/ledger/verify.
func VerifyRecorded(ctx context.Context, svc *ledger.Service, hf *HeadFile, log *slog.Logger) (ledger.VerifyResult, error) {
	expected, herr := hf.Read()
	if herr != nil {
		log.Warn("recorded ledger head unreadable; verifying without tail check", "path", hf.Path(), "err", herr)
	}
	res, err := svc.VerifyAgainst(ctx, expected)
	if err != nil {
		return res, err
	}
	head := FormatHead(ledger.Head{ID: res.HeadID, Hash: res.HeadHash})
	if !res.OK {
		attrs := []any{"head", head, "entries", res.Entries, "acknowledged", res.Acknowledged, "problems", len(res.Problems)}
		if res.BrokenAt != nil {
			attrs = append(attrs, "broken_at", *res.BrokenAt, "reason", *res.Reason)
		}
		if expected != nil {
			attrs = append(attrs, "recorded_head", FormatHead(*expected))
		}
		log.Warn("ledger verify FAILED", attrs...)
		return res, nil
	}
	if expected == nil && res.Entries > 0 {
		log.Warn("no recorded ledger head; tail deletion could not be checked this time", "path", hf.Path())
	}
	if err := hf.Write(ledger.Head{ID: res.HeadID, Hash: res.HeadHash}); err != nil {
		log.Error("could not record ledger head", "err", err)
	}
	log.Info("ledger verify ok", "head", head, "entries", res.Entries)
	return res, nil
}
