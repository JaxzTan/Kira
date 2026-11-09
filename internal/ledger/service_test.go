package ledger

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/store"
)

func TestNewRejectsShortKey(t *testing.T) {
	_, st := newTestService(t)
	cases := []struct {
		name string
		cfg  Config
	}{
		{"nil store", Config{Key: testKey}},
		{"short key", Config{Store: st, Key: []byte("short")}},
		{"nil key", Config{Store: st}},
	}
	for _, tc := range cases {
		if _, err := New(tc.cfg); err == nil {
			t.Errorf("%s: New succeeded", tc.name)
		}
	}
}

func TestAppendCreateAmendVoidFlow(t *testing.T) {
	ctx := context.Background()
	shaA := strings.Repeat("a", 64)
	svc, st := newTestService(t, func(c *Config) { c.Receipts = fakeReceipts{shaA: true} })
	if _, err := st.ArchiveCategory(ctx, 3, testNow); err != nil {
		t.Fatal(err)
	}

	type step struct {
		name    string
		req     Request
		wantErr error
		field   string // expected ValidationError field
		check   func(t *testing.T, r AppendResult)
	}
	note := "R2 + domain"
	steps := []step{
		{name: "create 1", req: Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-09", AmountSen: 4590, CategoryID: 1, Payee: "Cloudflare", Method: MethodCard, Note: &note}},
			check: func(t *testing.T, r AppendResult) {
				if r.Row.ID != 1 || r.Row.PrevHash != GenesisHash || r.Row.RefID != nil || len(r.Row.Hash) != 64 {
					t.Fatalf("row = %+v", r.Row)
				}
				e := r.Entry
				if e.ID != 1 || e.VersionID != 1 || e.Status != StatusActive || e.AmendCount != 0 || e.CategoryName != "Cloud & hosting" ||
					e.Hash != r.Row.Hash || e.CreatedAt != "2026-11-09T03:12:44Z" || e.UpdatedAt != e.CreatedAt || *e.Note != note {
					t.Fatalf("entry = %+v", e)
				}
			}},
		{name: "create 2 with receipt", req: Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-08", AmountSen: 4480, CategoryID: 2, Payee: "GitHub", Method: MethodCard, ReceiptSHA256: &shaA}},
			check: func(t *testing.T, r AppendResult) {
				if r.Row.ID != 2 || *r.Entry.ReceiptSHA256 != shaA {
					t.Fatalf("row %+v", r.Row)
				}
			}},
		{name: "amend 1", req: Request{Kind: KindAmend, RefID: 1, Input: Input{OccurredOn: "2026-11-09", AmountSen: 4600, CategoryID: 1, Payee: "Cloudflare Inc", Method: MethodCard}},
			check: func(t *testing.T, r AppendResult) {
				e := r.Entry
				if r.Row.ID != 3 || *r.Row.RefID != 1 || e.ID != 1 || e.VersionID != 3 || e.Status != StatusAmended ||
					e.AmendCount != 1 || e.AmountSen != 4600 || e.Payee != "Cloudflare Inc" || e.Note != nil || e.Hash != r.Row.Hash {
					t.Fatalf("entry = %+v", e)
				}
			}},
		{name: "amend 1 again", req: Request{Kind: KindAmend, RefID: 1, Input: Input{OccurredOn: "2026-11-07", AmountSen: 4700, CategoryID: 2, Payee: "Cloudflare", Method: MethodCash}},
			check: func(t *testing.T, r AppendResult) {
				if r.Entry.VersionID != 4 || r.Entry.AmendCount != 2 || r.Entry.AmountSen != 4700 || r.Entry.Method != MethodCash {
					t.Fatalf("entry = %+v", r.Entry)
				}
			}},
		{name: "amend targets amend row", req: Request{Kind: KindAmend, RefID: 3, Input: input("2026-11-09", 1, "x")}, wantErr: ErrNotCreate},
		{name: "void targets amend row", req: Request{Kind: KindVoid, RefID: 3, Reason: "x"}, wantErr: ErrNotCreate},
		{name: "amend missing", req: Request{Kind: KindAmend, RefID: 99, Input: input("2026-11-09", 1, "x")}, wantErr: ErrNotFound},
		{name: "void missing", req: Request{Kind: KindVoid, RefID: 99, Reason: "x"}, wantErr: ErrNotFound},
		{name: "void zero id", req: Request{Kind: KindVoid, RefID: 0, Reason: "x"}, wantErr: ErrNotFound},
		{name: "void no reason", req: Request{Kind: KindVoid, RefID: 2, Reason: " "}, field: "reason"},
		{name: "unknown category", req: Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-09", AmountSen: 1, CategoryID: 99, Payee: "x", Method: MethodCard}}, field: "category_id"},
		{name: "archived category", req: Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-09", AmountSen: 1, CategoryID: 3, Payee: "x", Method: MethodCard}}, field: "category_id"},
		{name: "unknown receipt", req: Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-09", AmountSen: 1, CategoryID: 1, Payee: "x", Method: MethodCard, ReceiptSHA256: sp(strings.Repeat("b", 64))}}, field: "receipt_sha256"},
		{name: "date confirm", req: Request{Kind: KindCreate, Input: input("2020-01-01", 1, "x")}, wantErr: ErrDateConfirmRequired},
		{name: "bad kind", req: Request{Kind: "delete", RefID: 1}, wantErr: errors.New("any")},
		{name: "void 2", req: Request{Kind: KindVoid, RefID: 2, Reason: " Duplicate of #1 "},
			check: func(t *testing.T, r AppendResult) {
				row, e := r.Row, r.Entry
				if row.ID != 5 || row.Kind != KindVoid || row.AmountSen != nil || row.OccurredOn != nil || row.CategoryID != nil ||
					row.Payee != nil || row.Method != nil || row.ReceiptSHA256 != nil || *row.VoidReason != "Duplicate of #1" {
					t.Fatalf("void row = %+v", row)
				}
				if e.Status != StatusVoided || *e.VoidReason != "Duplicate of #1" || e.AmountSen != 4480 || e.Payee != "GitHub" ||
					e.VersionID != 2 || e.Hash != row.Hash || *e.ReceiptSHA256 != shaA {
					t.Fatalf("voided entry = %+v", e)
				}
			}},
		{name: "void again", req: Request{Kind: KindVoid, RefID: 2, Reason: "again"}, wantErr: ErrVoided},
		{name: "amend voided", req: Request{Kind: KindAmend, RefID: 2, Input: input("2026-11-09", 1, "x")}, wantErr: ErrVoided},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			r, err := svc.Append(ctx, s.req)
			switch {
			case s.field != "":
				var ve *ValidationError
				if !errors.As(err, &ve) || ve.Fields[s.field] == "" {
					t.Fatalf("err = %v, want field %s", err, s.field)
				}
			case s.wantErr != nil:
				if err == nil {
					t.Fatal("want error")
				}
				if s.wantErr.Error() != "any" && !errors.Is(err, s.wantErr) {
					t.Fatalf("err = %v, want %v", err, s.wantErr)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				s.check(t, r)
			}
		})
	}

	// Failed appends must not consume ids or break the chain.
	head, _ := svc.Head(ctx)
	if head.ID != 5 {
		t.Fatalf("head = %d, want 5", head.ID)
	}
	res, err := svc.Verify(ctx)
	if err != nil || !res.OK || res.Entries != 5 || res.HeadID != 5 || res.HeadHash != head.Hash {
		t.Fatalf("verify = %+v %v", res, err)
	}

	// An amend may keep a now-archived category but not switch to one.
	if _, err := st.ArchiveCategory(ctx, 2, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Amend(ctx, 1, Input{OccurredOn: "2026-11-07", AmountSen: 4800, CategoryID: 2, Payee: "Cloudflare", Method: MethodCash}); err != nil {
		t.Fatalf("amend keeping archived category: %v", err)
	}
	var ve *ValidationError
	if _, err := svc.Amend(ctx, 1, Input{OccurredOn: "2026-11-07", AmountSen: 4800, CategoryID: 3, Payee: "Cloudflare", Method: MethodCash}); !errors.As(err, &ve) {
		t.Fatalf("amend to archived category: %v", err)
	}
}

func TestCurrentAndHistory(t *testing.T) {
	ctx := context.Background()
	clock := testNow
	svc, _ := newTestService(t, func(c *Config) { c.Now = func() time.Time { clock = clock.Add(time.Minute); return clock } })
	c := mustCreate(t, svc, input("2026-11-01", 100, "A"))
	mustCreate(t, svc, input("2026-11-02", 200, "B"))
	a1, _ := svc.Amend(ctx, c.Row.ID, input("2026-11-01", 150, "A1"))
	a2, _ := svc.Amend(ctx, c.Row.ID, input("2026-11-01", 175, "A2"))
	v, err := svc.Void(ctx, c.Row.ID, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	e, rows, err := svc.History(ctx, c.Row.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []int64{1, 3, 4, 5}
	wantKinds := []string{KindCreate, KindAmend, KindAmend, KindVoid}
	if len(rows) != len(wantIDs) {
		t.Fatalf("history len %d", len(rows))
	}
	for i := range rows {
		if rows[i].ID != wantIDs[i] || rows[i].Kind != wantKinds[i] {
			t.Fatalf("rows[%d] = %d %s", i, rows[i].ID, rows[i].Kind)
		}
	}
	if *rows[1].AmountSen != 150 || rows[0].RefID != nil || *rows[3].RefID != 1 || rows[1].PrevHash != a1.Row.PrevHash || rows[1].Hash != a1.Row.Hash {
		t.Fatal("history fields")
	}
	if e.Status != StatusVoided || e.AmountSen != 175 || e.Payee != "A2" || e.VersionID != a2.Row.ID || e.AmendCount != 2 ||
		e.Hash != v.Row.Hash || e.UpdatedAt != v.Row.CreatedAt || e.CreatedAt != c.Row.CreatedAt {
		t.Fatalf("entry = %+v", e)
	}
	_ = a1
	cases := []struct {
		id      int64
		wantErr error
	}{{1, nil}, {2, nil}, {3, ErrNotFound}, {5, ErrNotFound}, {42, ErrNotFound}}
	for _, tc := range cases {
		if _, err := svc.Get(ctx, tc.id); !errors.Is(err, tc.wantErr) {
			t.Errorf("Get(%d) err = %v, want %v", tc.id, err, tc.wantErr)
		}
		if _, _, err := svc.History(ctx, tc.id); !errors.Is(err, tc.wantErr) {
			t.Errorf("History(%d) err = %v, want %v", tc.id, err, tc.wantErr)
		}
	}
}

func TestListFilters(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	mk := func(date string, sen int64, payee string, cat int64, method string, note *string) int64 {
		r := mustCreate(t, svc, Input{OccurredOn: date, AmountSen: sen, CategoryID: cat, Payee: payee, Method: method, Note: note})
		return r.Row.ID
	}
	id1 := mk("2026-10-30", 1000, "Cloudflare", 1, MethodCard, sp("R2 bucket"))
	id2 := mk("2026-11-01", 2000, "Kedai Ünï", 6, MethodCash, nil)
	id3 := mk("2026-11-01", 3000, "GitHub", 2, MethodCard, sp("Copilot"))
	id4 := mk("2026-11-05", 4000, "Grab", 5, MethodGrabPay, nil)
	if _, err := svc.Amend(ctx, id3, Input{OccurredOn: "2026-11-02", AmountSen: 3500, CategoryID: 2, Payee: "GitHub", Method: MethodCard, Note: sp("Copilot yearly")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Void(ctx, id4, "dup"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		f         Filter
		wantIDs   []int64
		wantTotal int64
		wantCount int
		wantVoid  int
		wantErr   bool
	}{
		{"all", Filter{}, []int64{id4, id3, id2, id1}, 6500, 3, 1, false},
		{"status all", Filter{Status: "all"}, []int64{id4, id3, id2, id1}, 6500, 3, 1, false},
		{"active incl amended", Filter{Status: StatusActive}, []int64{id3, id2, id1}, 6500, 3, 0, false},
		{"voided", Filter{Status: StatusVoided}, []int64{id4}, 0, 0, 1, false},
		{"from/to inclusive", Filter{From: "2026-11-01", To: "2026-11-02"}, []int64{id3, id2}, 5500, 2, 0, false},
		{"amended date used", Filter{To: "2026-11-01"}, []int64{id2, id1}, 3000, 2, 0, false},
		{"category", Filter{CategoryID: 2}, []int64{id3}, 3500, 1, 0, false},
		{"method", Filter{Method: MethodCard}, []int64{id3, id1}, 4500, 2, 0, false},
		{"q payee case-insensitive", Filter{Q: "cloudFLARE"}, []int64{id1}, 1000, 1, 0, false},
		{"q note", Filter{Q: "yearly"}, []int64{id3}, 3500, 1, 0, false},
		{"q unicode fold", Filter{Q: "ÜNÏ"}, []int64{id2}, 2000, 1, 0, false},
		{"q no match", Filter{Q: "zzz"}, []int64{}, 0, 0, 0, false},
		{"bad filter", Filter{Method: "bitcoin"}, nil, 0, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.List(ctx, tc.f)
			if tc.wantErr {
				if !errors.Is(err, ErrBadRequest) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := []int64{}
			for _, e := range res.Entries {
				got = append(got, e.ID)
			}
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("ids = %v, want %v", got, tc.wantIDs)
			}
			for i := range got {
				if got[i] != tc.wantIDs[i] {
					t.Fatalf("ids = %v, want %v", got, tc.wantIDs)
				}
			}
			if res.TotalSen != tc.wantTotal || res.Count != tc.wantCount || res.VoidedCount != tc.wantVoid {
				t.Fatalf("total=%d count=%d voided=%d", res.TotalSen, res.Count, res.VoidedCount)
			}
		})
	}
	exp, err := svc.ExportEntries(ctx, "2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(exp) != 3 || exp[0].ID != id2 || exp[1].ID != id3 || exp[2].ID != id4 || exp[2].Status != StatusVoided {
		t.Fatalf("export = %+v", exp)
	}
	if _, err := svc.ExportEntries(ctx, "", "2026-11-30"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("export bad from: %v", err)
	}
}

func TestSummary(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	sha := strings.Repeat("c", 64)
	mk := func(date string, sen int64, cat int64, receipt *string) int64 {
		return mustCreate(t, svc, Input{OccurredOn: date, AmountSen: sen, CategoryID: cat, Payee: "p", Method: MethodCard, ReceiptSHA256: receipt}).Row.ID
	}
	mk("2026-05-31", 9999, 1, nil) // outside 6-month window
	mk("2026-06-15", 64000, 1, nil)
	mk("2026-10-01", 100000, 1, &sha) // prev month
	mk("2026-11-01", 60000, 1, &sha)
	b := mk("2026-11-02", 30000, 2, nil)
	c := mk("2026-11-30", 10000, 1, nil)
	v := mk("2026-11-15", 77777, 3, nil)
	if _, err := svc.Amend(ctx, b, Input{OccurredOn: "2026-11-02", AmountSen: 40000, CategoryID: 2, Payee: "p", Method: MethodCard, ReceiptSHA256: &sha}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Void(ctx, v, "dup"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		month       string
		total, prev int64
		count, void int
		missing     int
		changePct   *float64
		trend       []int64
		trendStart  string
		byCat       []CategoryTotal
	}{
		{
			month: "2026-11", total: 110000, prev: 100000, count: 3, void: 1, missing: 1,
			changePct: fp(10.0), trend: []int64{64000, 0, 0, 0, 100000, 110000}, trendStart: "2026-06",
			byCat: []CategoryTotal{{CategoryID: 1, TotalSen: 70000, Pct: 63.6}, {CategoryID: 2, TotalSen: 40000, Pct: 36.4}},
		},
		{
			month: "2026-10", total: 100000, prev: 0, count: 1, missing: 0,
			changePct: nil, trend: []int64{9999, 64000, 0, 0, 0, 100000}, trendStart: "2026-05",
			byCat: []CategoryTotal{{CategoryID: 1, TotalSen: 100000, Pct: 100}},
		},
		{
			month: "2027-01", total: 0, prev: 0, trend: []int64{0, 0, 100000, 110000, 0, 0}, trendStart: "2026-08",
			byCat: []CategoryTotal{},
		},
		{ // "" = current month (testNow is 2026-11-09)
			month: "", total: 110000, prev: 100000, count: 3, void: 1, missing: 1, changePct: fp(10.0),
			trend: []int64{64000, 0, 0, 0, 100000, 110000}, trendStart: "2026-06",
			byCat: []CategoryTotal{{CategoryID: 1, TotalSen: 70000, Pct: 63.6}, {CategoryID: 2, TotalSen: 40000, Pct: 36.4}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.month, func(t *testing.T) {
			s, err := svc.Summary(ctx, tc.month)
			if err != nil {
				t.Fatal(err)
			}
			if s.TotalSen != tc.total || s.PrevMonthTotalSen != tc.prev || s.Count != tc.count || s.VoidedCount != tc.void || s.MissingReceipts != tc.missing {
				t.Fatalf("summary = %+v", s)
			}
			if (s.ChangePct == nil) != (tc.changePct == nil) || (s.ChangePct != nil && *s.ChangePct != *tc.changePct) {
				t.Fatalf("change pct = %v", s.ChangePct)
			}
			if len(s.Trend) != 6 || s.Trend[0].Month != tc.trendStart {
				t.Fatalf("trend = %+v", s.Trend)
			}
			for i, w := range tc.trend {
				if s.Trend[i].TotalSen != w {
					t.Fatalf("trend[%d] = %+v, want %d", i, s.Trend[i], w)
				}
			}
			if len(s.ByCategory) != len(tc.byCat) {
				t.Fatalf("by category = %+v", s.ByCategory)
			}
			for i, w := range tc.byCat {
				g := s.ByCategory[i]
				if g.CategoryID != w.CategoryID || g.TotalSen != w.TotalSen || g.Pct != w.Pct || g.Name == "" || g.Color == nil {
					t.Fatalf("by category[%d] = %+v, want %+v", i, g, w)
				}
			}
			if len(s.Recent) != 5 || s.Recent[0].ID != c || s.Recent[1].ID != v || s.Recent[1].Status != StatusVoided {
				t.Fatalf("recent = %+v", s.Recent)
			}
		})
	}
	if _, err := svc.Summary(ctx, "2026-13"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bad month err = %v", err)
	}
}

func fp(f float64) *float64 { return &f }

func TestMoneyLargeTotals(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	const n = 30
	for i := 0; i < n; i++ {
		mustCreate(t, svc, input("2026-11-09", MaxAmountSen-1, "big"))
	}
	res, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(n) * (MaxAmountSen - 1); res.TotalSen != want {
		t.Fatalf("total = %d, want %d", res.TotalSen, want)
	}
	s, _ := svc.Summary(ctx, "2026-11")
	if s.TotalSen != res.TotalSen || s.ByCategory[0].Pct != 100 {
		t.Fatalf("summary total %d", s.TotalSen)
	}
}

func TestConcurrentAppendsSingleChain(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	const workers, per = 8, 20
	var wg sync.WaitGroup
	errs := make(chan error, workers*per)
	ids := make(chan int64, workers*per)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				r, err := svc.Create(ctx, input("2026-11-09", int64(i+1), "c"))
				if err != nil {
					errs <- err
					return
				}
				ids <- r.Row.ID
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
	}
	res, err := svc.Verify(ctx)
	if err != nil || !res.OK || res.Entries != workers*per {
		t.Fatalf("verify = %+v %v", res, err)
	}
}

func TestHeadEmpty(t *testing.T) {
	svc, _ := newTestService(t)
	h, err := svc.Head(context.Background())
	if err != nil || h.ID != 0 || h.Hash != GenesisHash {
		t.Fatalf("head = %+v %v", h, err)
	}
	res, err := svc.Verify(context.Background())
	if err != nil || !res.OK || res.HeadID != 0 || res.HeadHash != GenesisHash || res.BrokenAt != nil {
		t.Fatalf("verify empty = %+v", res)
	}
	_ = store.FormatTime
}
