package ledger

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JaxzTan/kira/internal/receipts"
	"github.com/JaxzTan/kira/internal/store"
)

// countingVerifier wraps a receipts.Store and counts VerifyFile calls per sha.
type countingVerifier struct {
	*receipts.Store
	mu    sync.Mutex
	calls map[string]int
}

func (c *countingVerifier) VerifyFile(sha string) error {
	c.mu.Lock()
	c.calls[sha]++
	c.mu.Unlock()
	return c.Store.VerifyFile(sha)
}

func pdf(n int) []byte { return []byte("%PDF-1.4\n% receipt " + strings.Repeat("x", n) + "\n%%EOF\n") }

// receiptLedger: rows 1 create (A), 2 create (no receipt), 3 create (B),
// 4 amend of 1 (still A), 5 create (A again).
func receiptLedger(t *testing.T) (*Service, *countingVerifier, receipts.Info, receipts.Info) {
	t.Helper()
	rs, err := receipts.New(filepath.Join(t.TempDir(), "receipts"))
	if err != nil {
		t.Fatal(err)
	}
	cv := &countingVerifier{Store: rs, calls: map[string]int{}}
	svc, _ := newTestService(t, func(c *Config) { c.Receipts = cv })
	a, err := rs.Save(bytes.NewReader(pdf(1)), -1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rs.Save(bytes.NewReader(pdf(2)), -1)
	if err != nil {
		t.Fatal(err)
	}
	withR := func(sha string) Input { in := input("2026-11-01", 100, "p"); in.ReceiptSHA256 = &sha; return in }
	mustCreate(t, svc, withR(a.SHA256))
	mustCreate(t, svc, input("2026-11-02", 200, "q"))
	mustCreate(t, svc, withR(b.SHA256))
	if _, err := svc.Amend(context.Background(), 1, withR(a.SHA256)); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, svc, withR(a.SHA256))
	return svc, cv, a, b
}

func TestVerifyReceipts(t *testing.T) {
	cases := []struct {
		name       string
		tamper     func(t *testing.T, dir string, a, b receipts.Info)
		wantAt     []int64
		wantReason []string
	}{
		{"intact", nil, nil, nil},
		{"overwrite A", func(t *testing.T, dir string, a, _ receipts.Info) {
			os.WriteFile(filepath.Join(dir, a.FileName()), []byte("%PDF-1.4 forged"), 0o600)
		}, []int64{1}, []string{ReasonReceiptMismatch}},
		{"delete B", func(t *testing.T, dir string, _, b receipts.Info) {
			os.Remove(filepath.Join(dir, b.FileName()))
		}, []int64{3}, []string{ReasonReceiptMissing}},
		{"both", func(t *testing.T, dir string, a, b receipts.Info) {
			os.Remove(filepath.Join(dir, a.FileName()))
			os.WriteFile(filepath.Join(dir, b.FileName()), []byte("x"), 0o600)
		}, []int64{1, 3}, []string{ReasonReceiptMissing, ReasonReceiptMismatch}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, cv, a, b := receiptLedger(t)
			if tc.tamper != nil {
				tc.tamper(t, cv.Dir(), a, b)
			}
			res, err := svc.Verify(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if cv.calls[a.SHA256] != 1 || cv.calls[b.SHA256] != 1 || len(cv.calls) != 2 {
				t.Fatalf("each receipt must be hashed exactly once: %v", cv.calls)
			}
			if res.Entries != 5 {
				t.Fatalf("entries = %d", res.Entries)
			}
			if len(tc.wantAt) == 0 {
				if !res.OK {
					t.Fatalf("intact ledger failed: %+v", res)
				}
				return
			}
			if res.OK || *res.BrokenAt != tc.wantAt[0] || *res.Reason != tc.wantReason[0] || len(res.Problems) != len(tc.wantAt) {
				t.Fatalf("res = %+v", res)
			}
			for i, p := range res.Problems {
				if p.ID != tc.wantAt[i] || p.Reason != tc.wantReason[i] {
					t.Fatalf("problem %d = %+v", i, p)
				}
			}
			if !svc.WritesBlocked() {
				t.Fatal("receipt tamper must block writes")
			}
		})
	}
}

func TestValidationReportsAllFields(t *testing.T) {
	ctx := context.Background()
	svc, st := newTestService(t, func(c *Config) { c.Receipts = fakeReceipts{} })
	if _, err := st.ArchiveCategory(ctx, 3, testNow); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, svc, input("2026-11-01", 100, "p"))
	cases := []struct {
		name string
		req  Request
		want []string
	}{
		{"everything bad incl unknown category", Request{Kind: KindCreate, Input: Input{Direction: "refund", OccurredOn: "x", AmountSen: 0, CategoryID: 99, Payee: " ", Method: "paypal", ReceiptSHA256: sp("")}},
			[]string{"amount_sen", "category_id", "direction", "method", "occurred_on", "payee", "receipt_sha256"}},
		{"archived category + amount", Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-01", AmountSen: -1, CategoryID: 3, Payee: "x", Method: MethodCard}},
			[]string{"amount_sen", "category_id"}},
		{"unknown receipt + unknown category", Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-01", AmountSen: 1, CategoryID: 42, Payee: "x", Method: MethodCard, ReceiptSHA256: sp(strings.Repeat("c", 64))}},
			[]string{"category_id", "receipt_sha256"}},
		{"income on amend with expense category", Request{Kind: KindAmend, RefID: 1, Input: Input{Direction: DirectionIncome, OccurredOn: "2026-11-01", AmountSen: 1, CategoryID: 1, Payee: "x", Method: MethodCard}},
			[]string{"category_id"}},
		{"income category mismatch + amount", Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-01", AmountSen: 0, CategoryID: 11, Payee: "x", Method: MethodCard}},
			[]string{"amount_sen", "category_id"}},
		{"empty receipt string", Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-01", AmountSen: 1, CategoryID: 1, Payee: "x", Method: MethodCard, ReceiptSHA256: sp("")}},
			[]string{"receipt_sha256"}},
		{"field errors beat date confirm", Request{Kind: KindCreate, Input: Input{OccurredOn: "2020-01-01", AmountSen: 1, CategoryID: 99, Payee: "x", Method: MethodCard}},
			[]string{"category_id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Append(ctx, tc.req)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v", err)
			}
			if len(ve.Fields) != len(tc.want) {
				t.Fatalf("fields = %v, want %v", ve.Fields, tc.want)
			}
			for _, f := range tc.want {
				if ve.Fields[f] == "" {
					t.Fatalf("missing field %s in %v", f, ve.Fields)
				}
			}
		})
	}
	// Whitespace-only note is stored as NULL; note is trimmed.
	r := mustCreate(t, svc, Input{OccurredOn: "2026-11-01", AmountSen: 1, CategoryID: 1, Payee: "x", Method: MethodCard, Note: sp(" \t ")})
	if r.Row.Note != nil || r.Entry.Note != nil {
		t.Fatal("whitespace note not nil")
	}
	r = mustCreate(t, svc, Input{OccurredOn: "2026-11-01", AmountSen: 1, CategoryID: 1, Payee: "x", Method: MethodCard, Note: sp("  hi  ")})
	if *r.Entry.Note != "hi" {
		t.Fatalf("note = %q", *r.Entry.Note)
	}
	if res, _ := svc.Verify(ctx); !res.OK {
		t.Fatalf("rejected appends must not touch the chain: %+v", res)
	}
}

func TestAcknowledgeSubsetSemantics(t *testing.T) {
	ctx := context.Background()
	svc, st, head := seedChain(t) // head = 5
	dropTriggers(t, st.DB())
	if _, err := st.DB().Exec(`DELETE FROM entries WHERE id = 5`); err != nil {
		t.Fatal(err)
	}
	opts := VerifyOptions{Expected: &head} // head file NOT rewritten after ack
	res, _ := svc.VerifyWith(ctx, opts)
	if res.OK || res.Acknowledged || *res.Reason != ReasonHeadMismatch || *res.BrokenAt != 5 {
		t.Fatalf("tail deletion: %+v", res)
	}
	rec := FormatHeadID(head)
	ack, ok, err := svc.Acknowledge(ctx, rec, func(r VerifyResult) error {
		if !r.Acknowledged {
			return errors.New("onAck saw unacknowledged result")
		}
		return nil
	})
	if err != nil || !ok || !ack.Acknowledged {
		t.Fatalf("ack: %+v %v %v", ack, ok, err)
	}
	steps := []struct {
		name      string
		do        func(t *testing.T)
		wantAcked bool
		wantOK    bool
	}{
		{"one valid append keeps ack (new row 5 differs from recorded head, same key)", func(t *testing.T) {
			mustCreate(t, svc, input("2026-11-06", 1, "a"))
		}, true, false},
		{"second append keeps ack", func(t *testing.T) {
			mustCreate(t, svc, input("2026-11-06", 2, "b"))
		}, true, false},
		{"new tamper blocks", func(t *testing.T) {
			if _, err := st.DB().Exec(`UPDATE entries SET payee = 'evil' WHERE id = 2`); err != nil {
				t.Fatal(err)
			}
		}, false, false},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			s.do(t)
			r, err := svc.VerifyWith(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			if r.OK != s.wantOK || r.Acknowledged != s.wantAcked || svc.WritesBlocked() == s.wantAcked {
				t.Fatalf("res = %+v blocked=%v", r, svc.WritesBlocked())
			}
		})
	}

	// Ack state round-trips into a fresh Service (restart).
	if _, _, err := svc.Acknowledge(ctx, rec, nil); err != nil {
		t.Fatal(err)
	}
	data, err := svc.ExportAck()
	if err != nil || len(data) == 0 {
		t.Fatalf("export: %s %v", data, err)
	}
	var as AckState
	if err := json.Unmarshal(data, &as); err != nil || len(as.Keys) != 2 || as.At == "" {
		t.Fatalf("ack state %s", data)
	}
	fresh, err := New(Config{Store: st, Key: testKey, Now: svc.now, Location: svc.loc})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := fresh.VerifyWith(ctx, opts); r.Acknowledged || !fresh.WritesBlocked() {
		t.Fatal("fresh service without ack state must block")
	}
	if err := fresh.ImportAck(data); err != nil {
		t.Fatal(err)
	}
	if r, _ := fresh.VerifyWith(ctx, opts); !r.Acknowledged || fresh.WritesBlocked() {
		t.Fatalf("restored ack not applied: %+v", r)
	}
	for _, bad := range [][]byte{[]byte("{"), []byte(`{"v":9,"keys":[]}`), []byte(`{"v":1,"keys":[""]}`)} {
		if err := fresh.ImportAck(bad); err == nil {
			t.Fatalf("ImportAck(%s) accepted", bad)
		}
	}

	// Integrity events: one per acknowledge, newest first.
	evs, err := svc.IntegrityEvents(ctx, 10)
	if err != nil || len(evs) != 2 {
		t.Fatalf("events %+v %v", evs, err)
	}
	e := evs[1]
	var ps []ProblemJSON
	if e.Kind != store.IntegrityAcknowledge || *e.RecordedHead != rec || *e.ActualHead != FormatHeadID(Head{ID: ack.HeadID, Hash: ack.HeadHash}) || ack.HeadID != 4 ||
		json.Unmarshal([]byte(e.Problems), &ps) != nil || len(ps) != 1 || ps[0].Reason != ReasonHeadMismatch || ps[0].Detail != rec {
		t.Fatalf("event %+v", e)
	}

	// A clean verify clears the ack set.
	svc2, _, _ := seedChain(t)
	if _, err := svc2.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ := svc2.ExportAck(); d != nil {
		t.Fatal("clean ledger has ack state")
	}
	if _, ok, _ := svc2.Acknowledge(ctx, "", nil); !ok {
		t.Fatal("ok expected")
	}
	if evs, _ := svc2.IntegrityEvents(ctx, 10); len(evs) != 0 {
		t.Fatal("acknowledging a clean ledger must not write an event")
	}
}

func TestHeadReasons(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		seed    bool
		reason  string
		wantOK  bool
		wantKey string
	}{
		{"missing, empty ledger", false, ReasonHeadMissing, true, ""},
		{"missing, entries", true, ReasonHeadMissing, false, ReasonHeadMissing},
		{"invalid, entries", true, ReasonHeadInvalid, false, ReasonHeadInvalid},
		{"invalid, empty ledger", false, ReasonHeadInvalid, false, ReasonHeadInvalid},
		{"none", true, "", true, ""},
		{"none, empty ledger", false, "", true, ""},
		{"unknown reason ignored", true, "bogus", true, ""},
		{"unknown reason ignored, empty ledger", false, "bogus", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var svc *Service
			if tc.seed {
				svc, _, _ = seedChain(t)
			} else {
				svc, _ = newTestService(t)
			}
			r, err := svc.VerifyWith(ctx, VerifyOptions{HeadReason: tc.reason})
			if err != nil || r.OK != tc.wantOK {
				t.Fatalf("%+v %v", r, err)
			}
			if !tc.wantOK {
				if *r.BrokenAt != r.HeadID || *r.Reason != tc.reason || r.Problems[0].Key() != tc.wantKey {
					t.Fatalf("%+v", r)
				}
				if !tc.seed && (*r.BrokenAt != 0 || r.HeadID != 0 || r.Entries != 0) {
					t.Fatalf("empty ledger: want broken_at 0, got %+v", r)
				}
				// Ack, then an append: same key, still acknowledged.
				if _, _, err := svc.Acknowledge(ctx, "", nil); err != nil {
					t.Fatal(err)
				}
				mustCreate(t, svc, input("2026-11-09", 1, "x"))
				if r, _ := svc.VerifyWith(ctx, VerifyOptions{HeadReason: tc.reason}); !r.Acknowledged {
					t.Fatalf("after append: %+v", r)
				}
			}
		})
	}
}

func TestHeadMAC(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	// Independent derivation of the documented construction.
	hk, err := hkdf.Key(sha256.New, testKey, nil, "kira-head", 32)
	if err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, hk)
	m.Write([]byte("head|7|" + hash))
	want := hex.EncodeToString(m.Sum(nil))
	if got := HeadMAC(testKey, 7, hash); got != want {
		t.Fatalf("HeadMAC = %s, want %s", got, want)
	}
	good := FormatHeadFile(testKey, Head{ID: 7, Hash: hash})
	if good != "7:"+hash+":"+want+"\n" {
		t.Fatalf("file = %q", good)
	}
	otherKey := bytes.Repeat([]byte{1}, 32)
	cases := []struct {
		name    string
		key     []byte
		content string
		ok      bool
	}{
		{"good", testKey, good, true},
		{"good no newline", testKey, strings.TrimSpace(good), true},
		{"wrong key", otherKey, good, false},
		{"id changed", testKey, "8:" + hash + ":" + want, false},
		{"hash changed", testKey, "7:" + strings.Repeat("cd", 32) + ":" + want, false},
		{"mac flipped", testKey, "7:" + hash + ":" + strings.Repeat("0", 64), false},
		{"legacy id:hash", testKey, "7:" + hash, false},
		{"leading zero id", testKey, "07:" + hash + ":" + want, false},
		{"negative id", testKey, "-1:" + hash + ":" + HeadMAC(testKey, -1, hash), false},
		{"upper mac", testKey, "7:" + hash + ":" + strings.ToUpper(want), false},
		{"empty", testKey, "", false},
		{"extra field", testKey, strings.TrimSpace(good) + ":x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := ParseHeadFile(tc.key, tc.content)
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if tc.ok && (h.ID != 7 || h.Hash != hash) {
				t.Fatalf("head %+v", h)
			}
			if !tc.ok && !errors.Is(err, ErrHeadInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if !CheckHeadMAC(testKey, 7, hash, want) || CheckHeadMAC(testKey, 7, hash, want[:63]) {
		t.Fatal("CheckHeadMAC")
	}
}

func TestAppendHookAndWriteLock(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	var mu sync.Mutex
	var seen []Row
	svc.SetAppendHook(func(r Row) {
		mu.Lock()
		seen = append(seen, r)
		mu.Unlock()
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := svc.Create(ctx, input("2026-11-09", int64(i+1), "c")); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(seen) != 20 {
		t.Fatalf("hook calls = %d", len(seen))
	}
	prev := GenesisHash
	for i, r := range seen {
		if r.ID != int64(i+1) || r.PrevHash != prev {
			t.Fatalf("hook row %d out of chain order: %+v", i, r)
		}
		prev = r.Hash
	}

	// VerifyLocked: fn sees the tip, and no append can interleave.
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := svc.VerifyLocked(ctx, VerifyOptions{}, func(r VerifyResult) error {
			if r.HeadID != 20 {
				return errors.New("wrong head")
			}
			close(started)
			<-release
			return nil
		})
		done <- err
	}()
	<-started
	appended := make(chan struct{})
	go func() {
		mustCreate(t, svc, input("2026-11-09", 99, "late"))
		close(appended)
	}()
	select {
	case <-appended:
		t.Fatal("append ran while VerifyLocked held the lock")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-appended
	if len(seen) != 21 {
		t.Fatalf("hook calls = %d", len(seen))
	}
	// fn error is surfaced; WithWriteLock passes errors through.
	sentinel := errors.New("boom")
	if _, err := svc.VerifyLocked(ctx, VerifyOptions{}, func(VerifyResult) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
	if err := svc.WithWriteLock(func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
	svc.SetAppendHook(nil)
	mustCreate(t, svc, input("2026-11-09", 1, "nohook"))
	if len(seen) != 21 {
		t.Fatal("hook not removed")
	}
}
