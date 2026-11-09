package api

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JaxzTan/kira/internal/ledger"
)

// Integrity owns the two files that make ledger checks survive restarts
// (docs/api-contract.md "Phase 4 hardening"):
//
// DATA_DIR/head ("id:hash:mac", ledger.FormatHeadFile, mode 0600) is the
// last trusted chain tip, so deleted tail rows are detected.
//
//   - It is written only under the ledger write mutex: from the append hook
//     and from the Verify/Acknowledge callbacks.
//   - It only moves forward: a head with an id <= the recorded one is never
//     written over a trusted file, and an unchanged head is not rewritten.
//   - A missing file (while entries exist) or one that is malformed / has a
//     bad MAC makes verify fail with "head missing" / "head invalid". Such a
//     file is never overwritten until the owner acknowledges (fail closed);
//     the append hook does not "heal" it either.
//   - Legacy "id:hash" files (pre-Phase 4, no MAC) are treated as invalid
//     and need one acknowledgement. Upgrading them silently would let anyone
//     who can truncate the DB also write a matching legacy file and bypass
//     the MAC, so the safer choice is to ask the owner once.
//
// DATA_DIR/ack (mode 0600) persists the acknowledged-problem set
// (ledger.Service.ExportAck) so an acknowledgement survives restarts. The
// file is "<hmac>\n<json>\n" with an HMAC keyed by HKDF(LEDGER_KEY,
// "kira-ack"), so a forged ack file cannot silently re-enable writes; a bad
// or unreadable file is ignored with a WARN (writes stay blocked).
type Integrity struct {
	svc      *ledger.Service
	key      []byte
	headPath string
	ackPath  string
	log      *slog.Logger
}

// AckMACInfo is the HKDF info string for the ack-file key.
const AckMACInfo = "kira-ack"

// NewIntegrity wires head/ack persistence for svc and installs the append
// hook that advances DATA_DIR/head after every committed append.
func NewIntegrity(svc *ledger.Service, ledgerKey []byte, headPath, ackPath string, log *slog.Logger) *Integrity {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	in := &Integrity{svc: svc, key: append([]byte(nil), ledgerKey...), headPath: headPath, ackPath: ackPath, log: log}
	svc.SetAppendHook(in.onAppend)
	return in
}

// HeadPath returns DATA_DIR/head.
func (in *Integrity) HeadPath() string { return in.headPath }

// AckPath returns DATA_DIR/ack.
func (in *Integrity) AckPath() string { return in.ackPath }

// headState is what DATA_DIR/head currently says.
type headState struct {
	head   *ledger.Head // trusted recorded head (nil unless reason == "")
	reason string       // "", ledger.ReasonHeadMissing or ledger.ReasonHeadInvalid
}

func (st headState) opts() ledger.VerifyOptions {
	if st.reason != "" {
		return ledger.VerifyOptions{HeadReason: st.reason}
	}
	return ledger.VerifyOptions{Expected: st.head}
}

// ReadHead returns the trusted recorded head, or nil and the reason it
// cannot be trusted (ledger.ReasonHeadMissing / ReasonHeadInvalid).
func (in *Integrity) ReadHead() (*ledger.Head, string) {
	st := in.readHead()
	return st.head, st.reason
}

func (in *Integrity) readHead() headState {
	b, err := os.ReadFile(in.headPath)
	if errors.Is(err, os.ErrNotExist) {
		return headState{reason: ledger.ReasonHeadMissing}
	}
	if err != nil {
		in.log.Error("ledger head file unreadable; treating as invalid", "path", in.headPath, "err", err)
		return headState{reason: ledger.ReasonHeadInvalid}
	}
	hd, err := ledger.ParseHeadFile(in.key, string(b))
	if err != nil {
		if isLegacyHead(string(b)) {
			in.log.Warn("ledger head file has the old id:hash format (no MAC); treated as invalid until acknowledged once in Settings", "path", in.headPath)
		}
		return headState{reason: ledger.ReasonHeadInvalid}
	}
	return headState{head: &hd}
}

// isLegacyHead reports whether s is a pre-Phase 4 "id:hash" head file.
func isLegacyHead(s string) bool {
	idS, hash, ok := strings.Cut(strings.TrimSpace(s), ":")
	id, err := strconv.ParseInt(idS, 10, 64)
	return ok && err == nil && id >= 0 && ledger.IsSHA256Hex(hash)
}

// recordLocked writes hd to the head file if the rules allow it. Must run
// under the ledger write mutex. replaceUntrusted allows overwriting a
// missing/invalid file (only after an ok or acknowledged verify).
func (in *Integrity) recordLocked(st headState, hd ledger.Head, replaceUntrusted bool) bool {
	if st.reason != "" {
		if !replaceUntrusted {
			return false
		}
	} else if st.head != nil && hd.ID <= st.head.ID {
		return false // unchanged, or would move backwards
	}
	if err := writeFileAtomic(in.headPath, []byte(ledger.FormatHeadFile(in.key, hd))); err != nil {
		in.log.Error("could not record ledger head", "path", in.headPath, "err", err)
		return false
	}
	return true
}

// onAppend is the ledger append hook (write mutex held, chain order). It
// re-reads the file so a head file deleted or edited while Kira runs is
// left as evidence for the next verify instead of being overwritten.
func (in *Integrity) onAppend(row ledger.Row) {
	hd := ledger.Head{ID: row.ID, Hash: row.Hash}
	st := in.readHead()
	if st.reason == ledger.ReasonHeadMissing && row.ID == 1 && row.PrevHash == ledger.GenesisHash {
		st.reason = "" // very first row of an empty ledger: nothing could have been deleted
	}
	if st.reason != "" {
		in.log.Warn("ledger head file is "+strings.TrimPrefix(st.reason, "head ")+"; not advancing it (the next verify reports this)", "id", row.ID)
		return
	}
	in.recordLocked(st, hd, false)
}

// Verify runs a full verify under the write mutex against DATA_DIR/head,
// logs the outcome (head id:hash at INFO, failures at WARN; TR-7), advances
// the head file when the result is ok or acknowledged, and persists the
// acknowledgement state. Used by `kira serve` at startup, `kira seed` and
// GET /api/ledger/verify.
func (in *Integrity) Verify(ctx context.Context) (ledger.VerifyResult, error) {
	var res ledger.VerifyResult
	err := in.svc.WithWriteLock(func() error {
		st := in.readHead()
		r, err := in.svc.VerifyWith(ctx, st.opts())
		if err != nil {
			return err
		}
		res = r
		in.logResult(st, r)
		if r.OK || r.Acknowledged {
			in.recordLocked(st, ledger.Head{ID: r.HeadID, Hash: r.HeadHash}, true)
		}
		in.persistAckLocked()
		return nil
	})
	return res, err
}

// VerifyReadOnly verifies against DATA_DIR/head (and the imported ack
// state) without writing either file. Used by `kira verify`.
func (in *Integrity) VerifyReadOnly(ctx context.Context) (ledger.VerifyResult, error) {
	st := in.readHead()
	res, err := in.svc.VerifyWith(ctx, st.opts())
	if err != nil {
		return res, err
	}
	in.logResult(st, res)
	return res, nil
}

func (in *Integrity) logResult(st headState, res ledger.VerifyResult) {
	head := ledger.FormatHeadID(ledger.Head{ID: res.HeadID, Hash: res.HeadHash})
	if !res.OK {
		attrs := []any{"head", head, "entries", res.Entries, "acknowledged", res.Acknowledged, "problems", len(res.Problems)}
		if res.BrokenAt != nil {
			attrs = append(attrs, "broken_at", *res.BrokenAt, "reason", *res.Reason)
		}
		if st.head != nil {
			attrs = append(attrs, "recorded_head", ledger.FormatHeadID(*st.head))
		}
		in.log.Warn("ledger verify FAILED", attrs...)
		return
	}
	in.log.Info("ledger verify ok", "head", head, "entries", res.Entries)
}

// Acknowledge acknowledges the last failed verify (running one first if
// none ran yet). Inside the ledger's write mutex it records the current
// head (an untrusted head file is replaced; a trusted one only moves
// forward) and persists DATA_DIR/ack.
func (in *Integrity) Acknowledge(ctx context.Context) (ledger.VerifyResult, error) {
	if _, ran := in.svc.LastVerify(); !ran {
		if _, err := in.Verify(ctx); err != nil {
			return ledger.VerifyResult{}, err
		}
	}
	recorded := ""
	if st := in.readHead(); st.head != nil {
		recorded = ledger.FormatHeadID(*st.head)
	}
	res, _, err := in.svc.Acknowledge(ctx, recorded, func(r ledger.VerifyResult) error {
		st := in.readHead()
		in.recordLocked(st, ledger.Head{ID: r.HeadID, Hash: r.HeadHash}, r.Acknowledged)
		in.persistAckLocked()
		return nil
	})
	return res, err
}

// ---- ack file ----

func (in *Integrity) ackMAC(data []byte) string {
	k, err := hkdf.Key(sha256.New, in.key, nil, AckMACInfo, 32)
	if err != nil {
		panic("api: hkdf: " + err.Error())
	}
	m := hmac.New(sha256.New, k)
	m.Write(data)
	return hex.EncodeToString(m.Sum(nil))
}

func (in *Integrity) formatAck(data []byte) []byte {
	return []byte(in.ackMAC(data) + "\n" + string(data) + "\n")
}

// LoadAck imports DATA_DIR/ack into the ledger service. Call it at startup
// BEFORE the first verify. A missing file is fine; an unreadable,
// malformed or unauthenticated one is ignored with a WARN (fail closed:
// problems stay unacknowledged). Returns whether an ack state was imported.
func (in *Integrity) LoadAck() bool {
	b, err := os.ReadFile(in.ackPath)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		in.log.Warn("ledger ack file unreadable; ignoring it", "path", in.ackPath, "err", err)
		return false
	}
	macHex, data, ok := strings.Cut(strings.TrimRight(string(b), "\n"), "\n")
	if !ok || !hmac.Equal([]byte(macHex), []byte(in.ackMAC([]byte(data)))) {
		in.log.Warn("ledger ack file is not authentic (bad MAC); ignoring it", "path", in.ackPath)
		return false
	}
	if err := in.svc.ImportAck([]byte(data)); err != nil {
		in.log.Warn("ledger ack file malformed; ignoring it", "path", in.ackPath, "err", err)
		return false
	}
	st := in.svc.AckState()
	in.log.Info("ledger acknowledgement restored", "problems", len(st.Keys), "at", st.At)
	return true
}

// persistAckLocked writes (or removes) DATA_DIR/ack to match the service's
// acknowledged set. Errors are logged.
func (in *Integrity) persistAckLocked() {
	data, err := in.svc.ExportAck()
	if err != nil {
		in.log.Error("could not export ledger ack state", "err", err)
		return
	}
	if data == nil {
		if err := os.Remove(in.ackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			in.log.Error("could not remove ledger ack file", "path", in.ackPath, "err", err)
		}
		return
	}
	want := in.formatAck(data)
	if cur, err := os.ReadFile(in.ackPath); err == nil && bytes.Equal(cur, want) {
		return
	}
	if err := writeFileAtomic(in.ackPath, want); err != nil {
		in.log.Error("could not persist ledger acknowledgement", "path", in.ackPath, "err", err)
	}
}

// writeFileAtomic writes data to path with mode 0600 via temp file + fsync
// + rename (+ dir fsync).
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	fail := func(err error) error {
		f.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := f.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
