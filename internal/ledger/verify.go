package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Verify reasons.
const (
	ReasonIDGap        = "id gap"
	ReasonPrevMismatch = "prev_hash mismatch"
	ReasonHashMismatch = "hash mismatch"
	ReasonColumnType   = "invalid column type"
	ReasonHeadMismatch = "head mismatch"
)

// Verify recomputes the whole chain and records the result for the
// read-only/acknowledge state (TR-7).
func (s *Service) Verify(ctx context.Context) (VerifyResult, error) {
	return s.VerifyAgainst(ctx, nil)
}

// VerifyAgainst is Verify plus a check against a previously recorded head
// (startup log, backup). It catches deleted tail rows, which leave a valid
// but shorter chain:
//   - expected.ID > current head: broken_at = current head id + 1 (first missing row);
//   - row expected.ID exists but its hash differs: broken_at = expected.ID.
//
// After acknowledging a head mismatch, callers should record the new head.
func (s *Service) VerifyAgainst(ctx context.Context, expected *Head) (VerifyResult, error) {
	res, err := verifyChain(ctx, s.db, s.key, expected)
	if err != nil {
		return VerifyResult{}, err
	}
	res.CheckedAt = s.now().UTC()
	s.record(&res)
	return res, nil
}

// MatchesHead reports whether the verified chain ends exactly at h.
func (r VerifyResult) MatchesHead(h Head) bool { return r.HeadID == h.ID && r.HeadHash == h.Hash }

func verifyChain(ctx context.Context, db *sql.DB, key []byte, expected *Head) (VerifyResult, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}) // consistent snapshot
	if err != nil {
		return VerifyResult{}, fmt.Errorf("ledger: verify begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT `+strings.Join(hashedColumns, ", ")+`, hash FROM entries ORDER BY id`)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("ledger: verify query: %w", err)
	}
	defer rows.Close()

	res := VerifyResult{HeadHash: GenesisHash}
	var problems []Problem
	add := func(id int64, reason string) {
		if len(problems) < MaxProblems {
			problems = append(problems, Problem{ID: id, Reason: reason})
		}
	}
	idIdx, prevIdx := indexOf("id"), indexOf("prev_hash")
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
			return VerifyResult{}, fmt.Errorf("ledger: verify scan: %w", err)
		}
		res.Entries++
		id, idOK := vals[idIdx].(int64)
		storedPrev, prevOK := vals[prevIdx].(string)
		storedHash, hashOK := vals[len(vals)-1].(string)
		if !idOK {
			// INTEGER PRIMARY KEY is always an integer; defensive only.
			add(prevID+1, ReasonColumnType)
			continue
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
			add(id, ReasonIDGap)
		case !typeOK:
			add(id, ReasonColumnType)
		case storedPrev != prevHash:
			add(id, ReasonPrevMismatch)
		default:
			c, err := canonicalJSON(fields)
			if err != nil {
				add(id, ReasonColumnType)
			} else if computeHash(key, storedPrev, c) != storedHash {
				add(id, ReasonHashMismatch)
			}
		}
		if expected != nil && id == expected.ID {
			expectedSeen = true
			if storedHash != expected.Hash {
				add(id, ReasonHeadMismatch)
			}
		}
		prevID, prevHash = id, storedHash
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, fmt.Errorf("ledger: verify rows: %w", err)
	}
	res.HeadID, res.HeadHash = prevID, prevHash
	if expected != nil && expected.ID > 0 && !expectedSeen {
		if expected.ID > prevID {
			add(prevID+1, ReasonHeadMismatch)
		} else {
			add(expected.ID, ReasonHeadMismatch) // expected row itself is missing (gap)
		}
	}
	sort.SliceStable(problems, func(i, j int) bool { return problems[i].ID < problems[j].ID })
	res.Problems = problems
	res.OK = len(problems) == 0
	if !res.OK {
		id, reason := problems[0].ID, problems[0].Reason
		res.BrokenAt, res.Reason = &id, &reason
	}
	return res, nil
}

func indexOf(col string) int {
	for i, c := range hashedColumns {
		if c == col {
			return i
		}
	}
	panic("ledger: unknown column " + col)
}

func problemsKey(ps []Problem) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(strconv.FormatInt(p.ID, 10))
		b.WriteByte(':')
		b.WriteString(p.Reason)
		b.WriteByte(';')
	}
	return b.String()
}
