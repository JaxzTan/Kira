package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/JaxzTan/kira/internal/store"
)

// Acknowledgement semantics (api-contract "Phase 4 hardening"):
//
//   - Acknowledge covers every problem of the last failed Verify (by
//     Problem.Key), added to any problems acknowledged before.
//   - A later Verify whose problems are a SUBSET of the acknowledged set is
//     reported Acknowledged and writes stay enabled (e.g. appends after an
//     acknowledged tail deletion). Any problem outside the set blocks writes.
//   - A Verify with no problems clears the acknowledged set.
//
// The set survives restarts via ExportAck/ImportAck (kira-api stores it in
// DATA_DIR/ack, mode 0600).

// AckState is the serialisable acknowledged-problem set.
type AckState struct {
	Version int      `json:"v"`
	At      string   `json:"at,omitempty"` // last acknowledgement, store.TimeFormat
	Keys    []string `json:"keys"`         // Problem.Key values, sorted
}

const ackStateVersion = 1

// Acknowledge re-enables writes after a failed Verify (TR-7) and records an
// integrity_events row (kind "acknowledge") with recordedHead (the head the
// caller had on file, "id:hash"; "" = unknown), the actual chain tip and the
// problems as JSON. It holds the write mutex; onAck (may be nil) runs under
// it after the acknowledgement took effect, so the caller can write the head
// file and ack file before any append can land. onAck must not call Service
// write methods.
//
// Returns the updated last result; ok=false (and no event) if Verify has
// never run. If the last result is OK there is nothing to acknowledge: no
// event is written and onAck is not called. If the event cannot be written
// the acknowledgement is NOT applied and the error is returned. An onAck
// error is returned after the acknowledgement was applied.
func (s *Service) Acknowledge(ctx context.Context, recordedHead string, onAck func(VerifyResult) error) (VerifyResult, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.LastVerify()
	if !ok {
		return VerifyResult{}, false, nil
	}
	if last.OK {
		return last, true, nil
	}
	pj, err := ProblemsJSON(last.Problems)
	if err != nil {
		return last, true, err
	}
	actual := FormatHeadID(Head{ID: last.HeadID, Hash: last.HeadHash})
	var rec *string
	if recordedHead != "" {
		rec = &recordedHead
	}
	now := s.now()
	if _, err := s.st.InsertIntegrityEvent(ctx, now, store.IntegrityAcknowledge, rec, &actual, pj); err != nil {
		return last, true, fmt.Errorf("ledger: acknowledge: %w", err)
	}

	s.stateMu.Lock()
	if s.acked == nil {
		s.acked = map[string]struct{}{}
	}
	for _, p := range last.Problems {
		s.acked[p.Key()] = struct{}{}
	}
	s.ackedAt = store.FormatTime(now)
	// s.last may have been replaced by a concurrent unlocked verify.
	s.last.Acknowledged = s.coveredLocked(s.last.Problems)
	res := *s.last
	s.stateMu.Unlock()

	if onAck != nil {
		if err := onAck(res); err != nil {
			return res, true, err
		}
	}
	return res, true, nil
}

func (s *Service) record(res *VerifyResult) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if res.OK {
		s.acked, s.ackedAt = nil, ""
	} else {
		res.Acknowledged = s.coveredLocked(res.Problems)
	}
	cp := *res
	cp.Problems = append([]Problem(nil), res.Problems...)
	s.last = &cp
}

// coveredLocked reports whether every problem is acknowledged (stateMu held).
func (s *Service) coveredLocked(ps []Problem) bool {
	if len(s.acked) == 0 || len(ps) == 0 {
		return false
	}
	for _, p := range ps {
		if _, ok := s.acked[p.Key()]; !ok {
			return false
		}
	}
	return true
}

// ExportAck returns the acknowledged set as JSON, or nil when nothing is
// acknowledged (callers should then remove the ack file). Persist it after
// Acknowledge and after every verify (a clean verify clears it).
func (s *Service) ExportAck() ([]byte, error) {
	st := s.AckState()
	if len(st.Keys) == 0 {
		return nil, nil
	}
	return json.Marshal(st)
}

// AckState returns a copy of the acknowledged set.
func (s *Service) AckState() AckState {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	st := AckState{Version: ackStateVersion, At: s.ackedAt, Keys: make([]string, 0, len(s.acked))}
	for k := range s.acked {
		st.Keys = append(st.Keys, k)
	}
	sort.Strings(st.Keys)
	return st
}

// ImportAck restores an acknowledged set saved by ExportAck. Call it at
// startup BEFORE the first Verify. Empty/nil data clears the set. It
// replaces the current set and does not change the last result.
func (s *Service) ImportAck(data []byte) error {
	if len(data) == 0 {
		s.stateMu.Lock()
		s.acked, s.ackedAt = nil, ""
		s.stateMu.Unlock()
		return nil
	}
	var st AckState
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("ledger: ack state: %w", err)
	}
	if st.Version != ackStateVersion {
		return fmt.Errorf("ledger: ack state: unsupported version %d", st.Version)
	}
	set := make(map[string]struct{}, len(st.Keys))
	for _, k := range st.Keys {
		if k == "" {
			return errors.New("ledger: ack state: empty key")
		}
		set[k] = struct{}{}
	}
	s.stateMu.Lock()
	s.acked, s.ackedAt = set, st.At
	s.stateMu.Unlock()
	return nil
}

// ProblemJSON is the integrity_events.problems element.
type ProblemJSON struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// ProblemsJSON encodes problems as a JSON array (never null).
func ProblemsJSON(ps []Problem) (string, error) {
	out := make([]ProblemJSON, len(ps))
	for i, p := range ps {
		out[i] = ProblemJSON{ID: p.ID, Reason: p.Reason, Detail: p.Detail}
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// FormatHeadID renders "id:hash" (the head file format without the MAC).
func FormatHeadID(h Head) string { return strconv.FormatInt(h.ID, 10) + ":" + h.Hash }

// IntegrityEvents returns up to limit integrity_events rows, newest first.
func (s *Service) IntegrityEvents(ctx context.Context, limit int) ([]store.IntegrityEvent, error) {
	return s.st.RecentIntegrityEvents(ctx, limit)
}
