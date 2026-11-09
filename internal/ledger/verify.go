package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/JaxzTan/kira/internal/receipts"
)

// Verify reasons.
const (
	ReasonIDGap           = "id gap"
	ReasonPrevMismatch    = "prev_hash mismatch"
	ReasonHashMismatch    = "hash mismatch"
	ReasonColumnType      = "invalid column type"
	ReasonHeadMismatch    = "head mismatch"
	ReasonHeadMissing     = "head missing"
	ReasonHeadInvalid     = "head invalid"
	ReasonReceiptMismatch = "receipt mismatch"
	ReasonReceiptMissing  = "receipt missing"
)

// VerifyOptions configures one verification.
type VerifyOptions struct {
	// Expected is the previously recorded head (DATA_DIR/head, backup). It
	// catches deleted tail rows, which leave a valid but shorter chain:
	//   - Expected.ID > current head: broken_at = current head id + 1;
	//   - row Expected.ID exists but its hash differs: broken_at = Expected.ID.
	// nil = no tail check.
	Expected *Head
	// HeadReason, if ReasonHeadMissing or ReasonHeadInvalid, reports that the
	// recorded head could not be trusted (file missing, malformed, bad MAC).
	// broken_at is the current head id (0 on an empty ledger).
	//   - ReasonHeadMissing is only a problem when the ledger has entries
	//     (zero entries + no head file = fresh install).
	//   - ReasonHeadInvalid is always a problem, even with zero entries: a
	//     garbled or forged head file on an empty ledger suggests every row
	//     was deleted.
	// Leave Expected nil in either case.
	HeadReason string
}

// Verify recomputes the whole chain (and re-hashes receipts) and records
// the result for the read-only/acknowledge state (TR-7).
func (s *Service) Verify(ctx context.Context) (VerifyResult, error) {
	return s.VerifyWith(ctx, VerifyOptions{})
}

// VerifyAgainst is Verify plus a check against a previously recorded head.
// After acknowledging a head mismatch, callers should record the new head.
func (s *Service) VerifyAgainst(ctx context.Context, expected *Head) (VerifyResult, error) {
	return s.VerifyWith(ctx, VerifyOptions{Expected: expected})
}

// VerifyWith runs a full verification with opts and records the result.
// It does not take the write mutex; see VerifyLocked.
func (s *Service) VerifyWith(ctx context.Context, opts VerifyOptions) (VerifyResult, error) {
	res, err := s.verify(ctx, opts)
	if err != nil {
		return VerifyResult{}, err
	}
	s.record(&res)
	return res, nil
}

// VerifyLocked is VerifyWith run while holding the ledger write mutex, so
// no append can land between the verification and fn. fn (may be nil) runs
// under the mutex after the result is recorded; use it to persist the head
// file / ack state. fn must not call Append/Create/Amend/Void, VerifyLocked,
// Acknowledge or WithWriteLock (deadlock). An fn error is returned together
// with the (already recorded) result.
func (s *Service) VerifyLocked(ctx context.Context, opts VerifyOptions, fn func(VerifyResult) error) (VerifyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.VerifyWith(ctx, opts)
	if err != nil {
		return VerifyResult{}, err
	}
	if fn != nil {
		if err := fn(res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Service) verify(ctx context.Context, opts VerifyOptions) (VerifyResult, error) {
	res, firstRef, err := verifyChain(ctx, s.db, s.key, opts.Expected)
	if err != nil {
		return VerifyResult{}, err
	}
	switch {
	case opts.HeadReason == ReasonHeadInvalid,
		opts.HeadReason == ReasonHeadMissing && res.Entries > 0:
		res.Problems = append(res.Problems, Problem{ID: res.HeadID, Reason: opts.HeadReason})
	}
	if s.rverify != nil {
		res.Problems = append(res.Problems, checkReceipts(s.rverify, firstRef)...)
	}
	finish(&res)
	res.CheckedAt = s.now().UTC()
	return res, nil
}

// checkReceipts re-hashes each distinct referenced receipt once.
// firstRef maps sha -> first ledger row id referencing it.
func checkReceipts(rv ReceiptVerifier, firstRef map[string]int64) []Problem {
	shas := make([]string, 0, len(firstRef))
	for sha := range firstRef {
		shas = append(shas, sha)
	}
	sort.Slice(shas, func(i, j int) bool { return firstRef[shas[i]] < firstRef[shas[j]] })
	var out []Problem
	for _, sha := range shas {
		err := rv.VerifyFile(sha)
		switch {
		case err == nil:
			continue
		case errors.Is(err, receipts.ErrTampered):
			out = append(out, Problem{ID: firstRef[sha], Reason: ReasonReceiptMismatch, Detail: sha})
		default: // not found, invalid sha, unreadable
			out = append(out, Problem{ID: firstRef[sha], Reason: ReasonReceiptMissing, Detail: sha})
		}
	}
	return out
}

// finish sorts, caps and summarises res.Problems.
func finish(res *VerifyResult) {
	sort.SliceStable(res.Problems, func(i, j int) bool { return res.Problems[i].ID < res.Problems[j].ID })
	if len(res.Problems) > MaxProblems {
		res.Problems = res.Problems[:MaxProblems]
	}
	res.OK = len(res.Problems) == 0
	res.BrokenAt, res.Reason = nil, nil
	if !res.OK {
		id, reason := res.Problems[0].ID, res.Problems[0].Reason
		res.BrokenAt, res.Reason = &id, &reason
	}
}

// MatchesHead reports whether the verified chain ends exactly at h.
func (r VerifyResult) MatchesHead(h Head) bool { return r.HeadID == h.ID && r.HeadHash == h.Hash }

func verifyChain(ctx context.Context, db *sql.DB, key []byte, expected *Head) (VerifyResult, map[string]int64, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}) // consistent snapshot
	if err != nil {
		return VerifyResult{}, nil, fmt.Errorf("ledger: verify begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT `+strings.Join(hashedColumns, ", ")+`, hash FROM entries ORDER BY id`)
	if err != nil {
		return VerifyResult{}, nil, fmt.Errorf("ledger: verify query: %w", err)
	}
	defer rows.Close()

	res := VerifyResult{HeadHash: GenesisHash}
	var problems []Problem
	add := func(p Problem) {
		if len(problems) < MaxProblems {
			problems = append(problems, p)
		}
	}
	firstRef := map[string]int64{}
	var expDetail string
	if expected != nil {
		expDetail = strconv.FormatInt(expected.ID, 10) + ":" + expected.Hash
	}
	idIdx, prevIdx, rcptIdx := indexOf("id"), indexOf("prev_hash"), indexOf("receipt_sha256")
	vals := make([]any, len(hashedColumns)+1)
	ptrs := make([]any, len(vals))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	prevID, prevHash := int64(0), GenesisHash
	var expectedSeen bool
	for rows.Next() {
		for i := range vals {
			vals[i] = nil
		}
		if err := rows.Scan(ptrs...); err != nil {
			return VerifyResult{}, nil, fmt.Errorf("ledger: verify scan: %w", err)
		}
		res.Entries++
		id, idOK := vals[idIdx].(int64)
		storedPrev, prevOK := vals[prevIdx].(string)
		storedHash, hashOK := vals[len(vals)-1].(string)
		if !idOK {
			// INTEGER PRIMARY KEY is always an integer; defensive only.
			add(Problem{ID: prevID + 1, Reason: ReasonColumnType})
			continue
		}
		if sha, ok := vals[rcptIdx].(string); ok && sha != "" {
			if _, seen := firstRef[sha]; !seen {
				firstRef[sha] = id
			}
		}
		fields := make(map[string]any, len(hashedColumns))
		typeOK := prevOK && hashOK
		for i, k := range hashedColumns {
			switch v := vals[i].(type) {
			case nil, int64, string:
				fields[k] = v
			default:
				typeOK = false
			}
		}
		switch {
		case id != prevID+1:
			add(Problem{ID: id, Reason: ReasonIDGap})
		case !typeOK:
			add(Problem{ID: id, Reason: ReasonColumnType})
		case storedPrev != prevHash:
			add(Problem{ID: id, Reason: ReasonPrevMismatch})
		default:
			c, err := canonicalJSON(fields)
			if err != nil {
				add(Problem{ID: id, Reason: ReasonColumnType})
			} else if computeHash(key, storedPrev, c) != storedHash {
				add(Problem{ID: id, Reason: ReasonHashMismatch})
			}
		}
		if expected != nil && id == expected.ID {
			expectedSeen = true
			if storedHash != expected.Hash {
				add(Problem{ID: id, Reason: ReasonHeadMismatch, Detail: expDetail})
			}
		}
		prevID, prevHash = id, storedHash
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, nil, fmt.Errorf("ledger: verify rows: %w", err)
	}
	res.HeadID, res.HeadHash = prevID, prevHash
	if expected != nil && expected.ID > 0 && !expectedSeen {
		if expected.ID > prevID {
			add(Problem{ID: prevID + 1, Reason: ReasonHeadMismatch, Detail: expDetail})
		} else {
			add(Problem{ID: expected.ID, Reason: ReasonHeadMismatch, Detail: expDetail}) // expected row itself is missing (gap)
		}
	}
	res.Problems = problems
	return res, firstRef, nil
}

func indexOf(col string) int {
	for i, c := range hashedColumns {
		if c == col {
			return i
		}
	}
	panic("ledger: unknown column " + col)
}
