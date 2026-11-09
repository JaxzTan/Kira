package ledger

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/JaxzTan/kira/internal/store"
)

// seedChain builds: 1 create, 2 create (receipt), 3 amend of 1, 4 void of 2, 5 create.
func seedChain(t *testing.T) (*Service, *store.Store, Head) {
	t.Helper()
	ctx := context.Background()
	shaA := strings.Repeat("a", 64)
	svc, st := newTestService(t)
	mustCreate(t, svc, input("2026-11-01", 4590, "Cloudflare"))
	mustCreate(t, svc, Input{OccurredOn: "2026-11-02", AmountSen: 4480, CategoryID: 2, Payee: "GitHub", Method: MethodCard, ReceiptSHA256: &shaA})
	if _, err := svc.Amend(ctx, 1, input("2026-11-01", 4600, "Cloudflare")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Void(ctx, 2, "dup"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, svc, input("2026-11-05", 1200, "Grab"))
	head, err := svc.Head(ctx)
	if err != nil || head.ID != 5 {
		t.Fatalf("head %+v %v", head, err)
	}
	return svc, st, head
}

func dropTriggers(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tr := range []string{"entries_no_update", "entries_no_delete", "entries_no_replace"} {
		if _, err := db.Exec(`DROP TRIGGER ` + tr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpdateWithTriggersPresent(t *testing.T) {
	svc, st, _ := seedChain(t)
	for _, q := range []string{
		`UPDATE entries SET amount_sen = 1 WHERE id = 3`,
		`DELETE FROM entries WHERE id = 5`,
		`INSERT OR REPLACE INTO entries (id, kind, occurred_on, amount_sen, created_at, prev_hash, hash) VALUES (5,'create','2026-01-01',5,'t','p','x')`,
	} {
		_, err := st.DB().Exec(q)
		if err == nil || !strings.Contains(err.Error(), "ledger is append-only") {
			t.Fatalf("%s: err = %v", q, err)
		}
	}
	if res, _ := svc.Verify(context.Background()); !res.OK {
		t.Fatalf("verify after refused edits = %+v", res)
	}
}

func TestTamperDetection(t *testing.T) {
	type tc struct {
		name       string
		tamper     func(t *testing.T, db *sql.DB, svc *Service)
		key        []byte // verify with this key (nil = testKey)
		useHead    bool   // pass the pre-tamper head to VerifyAgainst
		wantPlain  bool   // expected OK from plain Verify (no head)
		wantAt     int64
		wantReason string
	}
	exec := func(q string, args ...any) func(t *testing.T, db *sql.DB, _ *Service) {
		return func(t *testing.T, db *sql.DB, _ *Service) {
			t.Helper()
			if _, err := db.Exec(q, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	cases := []tc{
		{name: "edit amount of create", tamper: exec(`UPDATE entries SET amount_sen = 459 WHERE id = 1`), wantAt: 1, wantReason: ReasonHashMismatch},
		{name: "edit amount of amend", tamper: exec(`UPDATE entries SET amount_sen = 46 WHERE id = 3`), wantAt: 3, wantReason: ReasonHashMismatch},
		{name: "edit amount, re-hash with wrong key", tamper: func(t *testing.T, db *sql.DB, svc *Service) {
			_, rows, err := svc.History(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			r := rows[1] // id 3
			amt := int64(1)
			r.AmountSen = &amt
			forged, _ := RowHash(bytes.Repeat([]byte{0x99}, 32), r)
			exec(`UPDATE entries SET amount_sen = 1, hash = ? WHERE id = 3`, forged)(t, db, svc)
		}, wantAt: 3, wantReason: ReasonHashMismatch},
		{name: "swap receipt hash", tamper: exec(`UPDATE entries SET receipt_sha256 = ? WHERE id = 2`, strings.Repeat("b", 64)), wantAt: 2, wantReason: ReasonHashMismatch},
		{name: "remove receipt hash", tamper: exec(`UPDATE entries SET receipt_sha256 = NULL WHERE id = 2`), wantAt: 2, wantReason: ReasonHashMismatch},
		{name: "edit void reason", tamper: exec(`UPDATE entries SET void_reason = 'typo' WHERE id = 4`), wantAt: 4, wantReason: ReasonHashMismatch},
		{name: "un-void by changing kind", tamper: exec(`UPDATE entries SET kind = 'amend', amount_sen = 1 WHERE id = 4`), wantAt: 4, wantReason: ReasonHashMismatch},
		{name: "edit created_at", tamper: exec(`UPDATE entries SET created_at = '2020-01-01T00:00:00Z' WHERE id = 5`), wantAt: 5, wantReason: ReasonHashMismatch},
		{name: "edit prev_hash", tamper: exec(`UPDATE entries SET prev_hash = ? WHERE id = 4`, GenesisHash), wantAt: 4, wantReason: ReasonPrevMismatch},
		{name: "type change note to blob", tamper: exec(`UPDATE entries SET payee = X'41' WHERE id = 5`), wantAt: 5, wantReason: ReasonColumnType},
		{name: "delete middle row", tamper: exec(`DELETE FROM entries WHERE id = 3`), wantAt: 4, wantReason: ReasonIDGap},
		{name: "append forged row", tamper: exec(`INSERT INTO entries (id, kind, occurred_on, amount_sen, category_id, payee, method, created_at, prev_hash, hash)
			SELECT 6, 'create', '2026-11-09', 100, 1, 'x', 'card', '2026-11-09T00:00:00Z', hash, 'forged' FROM entries WHERE id = 5`), wantAt: 6, wantReason: ReasonHashMismatch},
		{name: "delete tail row: chain valid, head mismatch", tamper: exec(`DELETE FROM entries WHERE id = 5`),
			useHead: true, wantPlain: true, wantAt: 5, wantReason: ReasonHeadMismatch},
		{name: "delete two tail rows", tamper: exec(`DELETE FROM entries WHERE id >= 4`),
			useHead: true, wantPlain: true, wantAt: 4, wantReason: ReasonHeadMismatch},
		{name: "delete tail then app appends: head row differs", tamper: func(t *testing.T, db *sql.DB, svc *Service) {
			exec(`DELETE FROM entries WHERE id = 5`)(t, db, svc)
			if _, err := svc.Create(context.Background(), input("2026-11-06", 999, "other")); err != nil {
				t.Fatal(err)
			}
		}, useHead: true, wantPlain: true, wantAt: 5, wantReason: ReasonHeadMismatch},
		{name: "wrong LEDGER_KEY", tamper: func(*testing.T, *sql.DB, *Service) {}, key: bytes.Repeat([]byte{1}, 32), wantAt: 1, wantReason: ReasonHashMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			svc, st, head := seedChain(t)
			dropTriggers(t, st.DB())
			c.tamper(t, st.DB(), svc)
			v := svc
			if c.key != nil {
				var err error
				if v, err = New(Config{Store: st, Key: c.key, Now: svc.now}); err != nil {
					t.Fatal(err)
				}
			}
			plain, err := v.Verify(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if plain.OK != c.wantPlain {
				t.Fatalf("plain verify OK = %v, want %v (%+v)", plain.OK, c.wantPlain, plain)
			}
			res := plain
			if c.useHead {
				if plain.MatchesHead(head) {
					t.Fatal("head unexpectedly matches")
				}
				if res, err = v.VerifyAgainst(ctx, &head); err != nil {
					t.Fatal(err)
				}
			}
			if res.OK || res.BrokenAt == nil || res.Reason == nil {
				t.Fatalf("tamper not detected: %+v", res)
			}
			if *res.BrokenAt != c.wantAt || *res.Reason != c.wantReason {
				t.Fatalf("broken_at=%d reason=%q, want %d %q (problems %+v)", *res.BrokenAt, *res.Reason, c.wantAt, c.wantReason, res.Problems)
			}
			if !v.WritesBlocked() {
				t.Fatal("writes not blocked after failed verify")
			}
		})
	}
}

func TestVerifyAgainstMatchingHead(t *testing.T) {
	ctx := context.Background()
	svc, _, head := seedChain(t)
	cases := []struct {
		name string
		h    *Head
	}{
		{"nil", nil},
		{"current", &head},
		{"zero", &Head{}},
	}
	for _, tc := range cases {
		if res, err := svc.VerifyAgainst(ctx, tc.h); err != nil || !res.OK {
			t.Fatalf("%s: %+v %v", tc.name, res, err)
		}
	}
	// Legit growth after the recorded head is fine.
	mustCreate(t, svc, input("2026-11-09", 1, "more"))
	if res, _ := svc.VerifyAgainst(ctx, &head); !res.OK || res.HeadID != 6 {
		t.Fatalf("after growth: %+v", res)
	}
	// Recorded head with a hash that never existed.
	bad := Head{ID: 3, Hash: strings.Repeat("f", 64)}
	if res, _ := svc.VerifyAgainst(ctx, &bad); res.OK || *res.BrokenAt != 3 || *res.Reason != ReasonHeadMismatch {
		t.Fatalf("bad head: %+v", res)
	}
}

func TestBrokenLedgerBlocksWritesUntilAcknowledged(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := seedChain(t)
	if _, ok, _ := svc.Acknowledge(ctx, "", nil); ok {
		t.Fatal("acknowledge before any verify reported ok")
	}
	if res, _ := svc.Verify(ctx); !res.OK || svc.WritesBlocked() {
		t.Fatal("clean ledger blocked")
	}
	dropTriggers(t, st.DB())
	if _, err := st.DB().Exec(`UPDATE entries SET amount_sen = 1 WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	res, _ := svc.Verify(ctx)
	if res.OK || res.Acknowledged {
		t.Fatalf("verify = %+v", res)
	}
	steps := []struct {
		name    string
		do      func() error
		wantErr error
	}{
		{"create blocked", func() error { _, err := svc.Create(ctx, input("2026-11-09", 1, "x")); return err }, ErrLedgerBroken},
		{"amend blocked", func() error { _, err := svc.Amend(ctx, 1, input("2026-11-09", 1, "x")); return err }, ErrLedgerBroken},
		{"void blocked", func() error { _, err := svc.Void(ctx, 5, "x"); return err }, ErrLedgerBroken},
		{"acknowledge", func() error {
			r, ok, err := svc.Acknowledge(ctx, "", nil)
			if err != nil {
				return err
			}
			if !ok || !r.Acknowledged || r.OK {
				return errors.New("acknowledge result wrong")
			}
			return nil
		}, nil},
		{"create allowed after ack", func() error { _, err := svc.Create(ctx, input("2026-11-09", 1, "x")); return err }, nil},
		{"re-verify same breakage stays acknowledged", func() error {
			r, _ := svc.Verify(ctx)
			if r.OK || !r.Acknowledged || *r.BrokenAt != 2 {
				return errors.New("ack lost on same breakage")
			}
			_, err := svc.Create(ctx, input("2026-11-09", 1, "y"))
			return err
		}, nil},
		{"new tamper re-blocks", func() error {
			if _, err := st.DB().Exec(`UPDATE entries SET payee = 'evil' WHERE id = 5`); err != nil {
				return err
			}
			r, _ := svc.Verify(ctx)
			if r.Acknowledged || len(r.Problems) != 2 {
				return errors.New("new tampering was auto-acknowledged")
			}
			_, err := svc.Create(ctx, input("2026-11-09", 1, "z"))
			return err
		}, ErrLedgerBroken},
	}
	for _, s := range steps {
		if err := s.do(); !errors.Is(err, s.wantErr) {
			t.Fatalf("%s: err = %v, want %v", s.name, err, s.wantErr)
		}
	}
	last, ok := svc.LastVerify()
	if !ok || last.OK {
		t.Fatal("LastVerify")
	}
}
