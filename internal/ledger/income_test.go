package ledger

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/store"
	"github.com/JaxzTan/kira/migrations"
)

// Income category ids on a fresh DB (migration 0004 seeds them after the 10 expense seeds).
const (
	catClientRevenue int64 = 11
	catProductSales  int64 = 12
	catRefunds       int64 = 13
	catOtherIncome   int64 = 14
)

func incomeInput(date string, sen, cat int64, payee string) Input {
	return Input{Direction: DirectionIncome, OccurredOn: date, AmountSen: sen, CategoryID: cat, Payee: payee, Method: MethodBankTransfer}
}

// TestUpgradeFromV3 builds a schema-v3 database that already holds a
// hash-chained ledger (creates, amend, void) plus owner categories, then
// opens it with the current Store: migration 0004 must apply, keep every
// existing row expense, seed income categories without clobbering a
// same-named owner category, and leave the chain verifiable.
func TestUpgradeFromV3(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kira.db")
	db, err := sql.Open("sqlite", store.DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[:3] {
		if _, err := db.Exec(m.SQL); err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?, ?, ?)`, m.Version, m.Name, "2026-11-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	// Owner categories created under v3: one unrelated, one colliding (case-
	// insensitively) with an income seed, one archived.
	for _, q := range []string{
		`INSERT INTO categories (id, name, color) VALUES (11, 'Courses', '#0F766E')`,
		`INSERT INTO categories (id, name, color) VALUES (12, 'OTHER INCOME', '#2F6FB5')`,
		`UPDATE categories SET archived_at = '2026-10-01T00:00:00Z' WHERE id = 3`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// A v3 ledger written exactly like Service.Append does.
	type spec struct {
		kind       string
		ref        int64
		date       string
		sen, cat   int64
		voidReason string
	}
	specs := []spec{
		{kind: KindCreate, date: "2026-10-01", sen: 4590, cat: 1},
		{kind: KindCreate, date: "2026-10-02", sen: 12000, cat: 3},
		{kind: KindAmend, ref: 1, date: "2026-10-01", sen: 4600, cat: 1},
		{kind: KindCreate, date: "2026-10-03", sen: 999, cat: 12},
		{kind: KindVoid, ref: 2, voidReason: "dup"},
	}
	prev := GenesisHash
	for i, sp := range specs {
		r := Row{ID: int64(i + 1), Kind: sp.kind, Direction: DirectionExpense, Currency: CurrencyMYR,
			CreatedAt: store.FormatTime(testNow.Add(time.Duration(i) * time.Minute)), PrevHash: prev}
		if sp.ref != 0 {
			ref := sp.ref
			r.RefID = &ref
		}
		if sp.kind == KindVoid {
			reason := sp.voidReason
			r.VoidReason = &reason
		} else {
			date, sen, cat, payee, method := sp.date, sp.sen, sp.cat, "payee", MethodCard
			r.OccurredOn, r.AmountSen, r.CategoryID, r.Payee, r.Method = &date, &sen, &cat, &payee, &method
		}
		if r.Hash, err = RowHash(testKey, r); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO entries
			(id, kind, ref_id, direction, occurred_on, amount_sen, currency, category_id, payee, method, note, void_reason, receipt_sha256, created_at, prev_hash, hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.Kind, ptrVal(r.RefID), r.Direction, ptrVal(r.OccurredOn), ptrVal(r.AmountSen), r.Currency,
			ptrVal(r.CategoryID), ptrVal(r.Payee), ptrVal(r.Method), nil, ptrVal(r.VoidReason), nil, r.CreatedAt, r.PrevHash, r.Hash); err != nil {
			t.Fatal(err)
		}
		prev = r.Hash
	}
	db.Close() // no clean shutdown of anything else; just release the file

	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("upgrade open: %v", err)
	}
	defer st.Close()
	if v, err := st.SchemaVersion(ctx); err != nil || v != 4 {
		t.Fatalf("schema version = %d %v", v, err)
	}
	svc, err := New(Config{Store: st, Key: testKey, Now: func() time.Time { return testNow }, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := svc.Verify(ctx); err != nil || !res.OK || res.Entries != 5 || res.HeadHash != prev {
		t.Fatalf("verify after upgrade = %+v %v", res, err)
	}

	cats, err := st.ListCategories(ctx, true, "")
	if err != nil {
		t.Fatal(err)
	}
	type wantCat struct {
		id       int64
		name     string
		dir      string
		archived bool
	}
	wants := []wantCat{
		{1, "Cloud & hosting", DirectionExpense, false},
		{3, "Hardware", DirectionExpense, true},
		{10, "Other", DirectionExpense, false},
		{11, "Courses", DirectionExpense, false},
		{12, "OTHER INCOME", DirectionExpense, false}, // owner's row kept as-is
		{13, "Client revenue", DirectionIncome, false},
		{14, "Product sales", DirectionIncome, false},
		{15, "Refunds & rebates", DirectionIncome, false},
	}
	if len(cats) != 15 {
		t.Fatalf("got %d categories, want 15 (12 existing + 3 new income; 'Other income' skipped)", len(cats))
	}
	byID := map[int64]store.Category{}
	for _, c := range cats {
		byID[c.ID] = c
	}
	for _, w := range wants {
		c, ok := byID[w.id]
		if !ok || c.Name != w.name || c.Direction != w.dir || (c.ArchivedAt != nil) != w.archived {
			t.Errorf("category %d = %+v, want %+v", w.id, c, w)
		}
	}
	if c := byID[15]; c.Color == nil || *c.Color != "#C2701E" {
		t.Errorf("refunds color = %v", c.Color)
	}

	// Existing entries are untouched and still expense.
	list, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if list.TotalSen != 4600+999 || list.IncomeSen != 0 || list.Count != 2 || list.VoidedCount != 1 || len(list.Entries) != 3 {
		t.Fatalf("list after upgrade = %+v", list)
	}
	for _, e := range list.Entries {
		if e.Direction != DirectionExpense {
			t.Fatalf("entry %d direction %q", e.ID, e.Direction)
		}
	}
	// The ledger keeps working: amend the old entry keeping its archived
	// category is fine; new income goes on top of the old chain.
	if _, err := svc.Create(ctx, incomeInput("2026-11-01", 500000, 13, "Acme Sdn Bhd")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, incomeInput("2026-11-01", 1, 12, "x")); !isFieldErr(err, "category_id", "category is for expenses") {
		t.Fatalf("income on owner's expense 'OTHER INCOME' category: %v", err)
	}
	if res, _ := svc.Verify(ctx); !res.OK || res.Entries != 6 {
		t.Fatalf("verify after income append = %+v", res)
	}
	// Re-opening does not re-apply 0004 (would fail on duplicate column).
	st.Close()
	st2, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
}

func isFieldErr(err error, field, msg string) bool {
	var ve *ValidationError
	return errors.As(err, &ve) && ve.Fields[field] == msg
}

func TestIncomeCreateAmendVoid(t *testing.T) {
	ctx := context.Background()
	svc, st := newTestService(t)
	if _, err := st.ArchiveCategory(ctx, catRefunds, testNow); err != nil {
		t.Fatal(err)
	}
	exp := mustCreate(t, svc, input("2026-11-01", 4590, "Cloudflare")).Row.ID              // 1
	inc := mustCreate(t, svc, incomeInput("2026-11-02", 250000, catClientRevenue, "Acme")) // 2
	if inc.Row.Direction != DirectionIncome || inc.Entry.Direction != DirectionIncome || inc.Entry.CategoryName != "Client revenue" {
		t.Fatalf("income create = %+v", inc)
	}
	incID := inc.Row.ID
	// An income entry whose category is then archived (raw: bypass ordering issue).
	keep := mustCreate(t, svc, incomeInput("2026-11-03", 700, catOtherIncome, "Rebate")).Row.ID // 3
	if _, err := st.ArchiveCategory(ctx, catOtherIncome, testNow); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		req       Request
		wantField string // category_id message, "" = success
		wantErr   error
		check     func(t *testing.T, r AppendResult)
	}{
		{"expense with income category", Request{Kind: KindCreate, Input: input("2026-11-01", 100, "x").withCat(catClientRevenue)}, "category is for income", nil, nil},
		{"default direction with income category", Request{Kind: KindCreate, Input: Input{OccurredOn: "2026-11-01", AmountSen: 1, CategoryID: catProductSales, Payee: "x", Method: MethodCash}}, "category is for income", nil, nil},
		{"income with expense category", Request{Kind: KindCreate, Input: incomeInput("2026-11-01", 100, 1, "x")}, "category is for expenses", nil, nil},
		{"income with archived income category", Request{Kind: KindCreate, Input: incomeInput("2026-11-01", 100, catRefunds, "x")}, "category is archived", nil, nil},
		{"income with unknown category", Request{Kind: KindCreate, Input: incomeInput("2026-11-01", 100, 999, "x")}, "unknown category", nil, nil},
		{"income amount zero", Request{Kind: KindCreate, Input: incomeInput("2026-11-01", 0, catClientRevenue, "x")}, "", nil, nil}, // checked below via amount_sen
		{"amend income to expense keeping income category", Request{Kind: KindAmend, RefID: incID, Input: input("2026-11-02", 250000, "Acme").withCat(catClientRevenue)}, "category is for income", nil, nil},
		{"amend income amount", Request{Kind: KindAmend, RefID: incID, Input: incomeInput("2026-11-02", 260050, catProductSales, "Acme")}, "", nil, func(t *testing.T, r AppendResult) {
			if r.Row.Direction != DirectionIncome || r.Entry.AmountSen != 260050 || r.Entry.Status != StatusAmended || r.Entry.CategoryName != "Product sales" {
				t.Fatalf("amend = %+v", r)
			}
		}},
		{"amend keeps archived income category", Request{Kind: KindAmend, RefID: keep, Input: incomeInput("2026-11-03", 800, catOtherIncome, "Rebate")}, "", nil, func(t *testing.T, r AppendResult) {
			if r.Entry.AmountSen != 800 || r.Entry.CategoryID != catOtherIncome {
				t.Fatalf("amend archived = %+v", r)
			}
		}},
		{"amend expense to income with income category", Request{Kind: KindAmend, RefID: exp, Input: incomeInput("2026-11-01", 4590, catClientRevenue, "Refund from Cloudflare")}, "", nil, func(t *testing.T, r AppendResult) {
			if r.Row.Direction != DirectionIncome || r.Entry.Direction != DirectionIncome {
				t.Fatalf("direction change = %+v", r)
			}
		}},
		{"void income", Request{Kind: KindVoid, RefID: keep, Reason: "not a rebate"}, "", nil, func(t *testing.T, r AppendResult) {
			if r.Row.Direction != DirectionIncome || r.Entry.Status != StatusVoided || r.Entry.Direction != DirectionIncome {
				t.Fatalf("void = %+v", r)
			}
		}},
		{"amend voided income", Request{Kind: KindAmend, RefID: keep, Input: incomeInput("2026-11-03", 900, catOtherIncome, "Rebate")}, "", ErrVoided, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := svc.Append(ctx, tc.req)
			switch {
			case tc.name == "income amount zero":
				if !isFieldErr(err, "amount_sen", "must be greater than 0") {
					t.Fatalf("err = %v", err)
				}
			case tc.wantField != "":
				if !isFieldErr(err, "category_id", tc.wantField) {
					t.Fatalf("err = %v, want category_id %q", err, tc.wantField)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if tc.check != nil {
					tc.check(t, r)
				}
			}
		})
	}
	// History of the direction-changed entry: create (expense) then amend (income).
	e, rows, err := svc.History(ctx, exp)
	if err != nil || len(rows) != 2 || rows[0].Direction != DirectionExpense || rows[1].Direction != DirectionIncome || e.Direction != DirectionIncome {
		t.Fatalf("history = %+v %+v %v", e, rows, err)
	}
	if res, _ := svc.Verify(ctx); !res.OK {
		t.Fatalf("verify = %+v", res)
	}
}

func (in Input) withCat(id int64) Input { in.CategoryID = id; return in }

// TestIncomeListAndSummary checks money math with both directions,
// including voided income and an income entry amended to a new amount.
func TestIncomeListAndSummary(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	sha := strings.Repeat("d", 64)
	mk := func(in Input) int64 { return mustCreate(t, svc, in).Row.ID }
	e1 := mk(input("2026-11-01", 10000, "AWS")) // expense, no receipt
	_ = e1
	e2 := mk(Input{OccurredOn: "2026-11-03", AmountSen: 5000, CategoryID: 2, Payee: "GitHub", Method: MethodCard, ReceiptSHA256: &sha})
	i1 := mk(incomeInput("2026-11-02", 200000, catClientRevenue, "Acme"))  // income, no receipt: not "missing"
	i2 := mk(incomeInput("2026-11-04", 30000, catProductSales, "Gumroad")) // amended to 45000
	i3 := mk(incomeInput("2026-11-05", 99999, catRefunds, "Bank"))         // voided
	mk(incomeInput("2026-10-15", 50000, catClientRevenue, "Acme"))         // prev month income
	mk(input("2026-10-10", 20000, "Maxis"))                                // prev month expense
	i5 := mk(input("2026-11-06", 7000, "Shopee"))                          // expense → amended to income 8000
	mk(incomeInput("2026-06-30", 1, catOtherIncome, "interest"))           // trend start month
	if _, err := svc.Amend(ctx, i2, incomeInput("2026-11-04", 45000, catProductSales, "Gumroad")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Void(ctx, i3, "bounced"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Amend(ctx, i5, incomeInput("2026-11-06", 8000, catRefunds, "Shopee refund")); err != nil {
		t.Fatal(err)
	}

	listCases := []struct {
		name               string
		f                  Filter
		entries            int
		total, income, net int64
		count, voided      int
	}{
		{"all", Filter{}, 9, 35000, 303001, 268001, 8, 1},
		{"all explicit", Filter{Direction: "all"}, 9, 35000, 303001, 268001, 8, 1},
		{"expense", Filter{Direction: DirectionExpense}, 3, 35000, 0, -35000, 3, 0},
		{"income", Filter{Direction: DirectionIncome}, 6, 0, 303001, 303001, 5, 1},
		{"income voided", Filter{Direction: DirectionIncome, Status: StatusVoided}, 1, 0, 0, 0, 0, 1},
		{"november", Filter{From: "2026-11-01", To: "2026-11-30"}, 6, 15000, 253000, 238000, 5, 1},
		{"income category", Filter{CategoryID: catRefunds}, 2, 0, 8000, 8000, 1, 1},
	}
	for _, tc := range listCases {
		t.Run("list "+tc.name, func(t *testing.T) {
			r, err := svc.List(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Entries) != tc.entries || r.TotalSen != tc.total || r.IncomeSen != tc.income || r.NetSen != tc.net || r.Count != tc.count || r.VoidedCount != tc.voided {
				t.Fatalf("got entries=%d total=%d income=%d net=%d count=%d voided=%d", len(r.Entries), r.TotalSen, r.IncomeSen, r.NetSen, r.Count, r.VoidedCount)
			}
			if d := tc.f.Direction; d == DirectionExpense || d == DirectionIncome {
				for _, e := range r.Entries {
					if e.Direction != d {
						t.Fatalf("filter %s returned %+v", d, e)
					}
				}
			}
		})
	}
	if _, err := svc.List(ctx, Filter{Direction: "both"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bad direction err = %v", err)
	}

	type trend struct{ exp, inc int64 }
	sumCases := []struct {
		month                  string
		total, income, net     int64
		prevTotal, prevIncome  int64
		count, voided, missing int
		changePct              *float64
		byCat                  []CategoryTotal
		trend                  []trend
	}{
		{
			month: "2026-11", total: 15000, income: 253000, net: 238000, prevTotal: 20000, prevIncome: 50000,
			count: 5, voided: 1, missing: 1, changePct: fp(-25.0),
			byCat: []CategoryTotal{{CategoryID: 1, TotalSen: 10000, Pct: 66.7}, {CategoryID: 2, TotalSen: 5000, Pct: 33.3}},
			trend: []trend{{0, 1}, {0, 0}, {0, 0}, {0, 0}, {20000, 50000}, {15000, 253000}},
		},
		{
			month: "2026-10", total: 20000, income: 50000, net: 30000, count: 2, missing: 1, changePct: nil,
			byCat: []CategoryTotal{{CategoryID: 1, TotalSen: 20000, Pct: 100}},
			trend: []trend{{0, 0}, {0, 1}, {0, 0}, {0, 0}, {0, 0}, {20000, 50000}},
		},
		{
			month: "2026-06", income: 1, net: 1, count: 1, byCat: []CategoryTotal{}, // income only: no bars, no missing
			trend: []trend{{0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 0}, {0, 1}},
		},
	}
	for _, tc := range sumCases {
		t.Run("summary "+tc.month, func(t *testing.T) {
			s, err := svc.Summary(ctx, tc.month)
			if err != nil {
				t.Fatal(err)
			}
			if s.TotalSen != tc.total || s.IncomeSen != tc.income || s.NetSen != tc.net || s.PrevMonthTotalSen != tc.prevTotal ||
				s.PrevMonthIncomeSen != tc.prevIncome || s.Count != tc.count || s.VoidedCount != tc.voided || s.MissingReceipts != tc.missing {
				t.Fatalf("summary = %+v", s)
			}
			if (s.ChangePct == nil) != (tc.changePct == nil) || (s.ChangePct != nil && *s.ChangePct != *tc.changePct) {
				t.Fatalf("change pct = %v", s.ChangePct)
			}
			if len(s.ByCategory) != len(tc.byCat) {
				t.Fatalf("by category = %+v", s.ByCategory)
			}
			for i, w := range tc.byCat {
				if g := s.ByCategory[i]; g.CategoryID != w.CategoryID || g.TotalSen != w.TotalSen || g.Pct != w.Pct {
					t.Fatalf("by category[%d] = %+v, want %+v", i, g, w)
				}
			}
			if len(s.Trend) != 6 {
				t.Fatalf("trend = %+v", s.Trend)
			}
			for i, w := range tc.trend {
				if s.Trend[i].TotalSen != w.exp || s.Trend[i].IncomeSen != w.inc {
					t.Fatalf("trend[%d] = %+v, want %+v", i, s.Trend[i], w)
				}
			}
		})
	}
	// Recent covers both directions (5 newest by date, voided included).
	s, _ := svc.Summary(ctx, "2026-11")
	wantRecent := []int64{i5, i3, i2, e2, i1}
	for i, id := range wantRecent {
		if s.Recent[i].ID != id {
			t.Fatalf("recent = %+v", s.Recent)
		}
	}
	if s.Recent[0].Direction != DirectionIncome || s.Recent[3].Direction != DirectionExpense || s.Recent[1].Status != StatusVoided {
		t.Fatalf("recent directions = %+v", s.Recent)
	}
	if res, _ := svc.Verify(ctx); !res.OK {
		t.Fatalf("verify = %+v", res)
	}
}

// TestIncomeMoneyLimits: income uses the same amount bounds and int64 sums.
func TestIncomeMoneyLimits(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	cases := []struct {
		sen int64
		ok  bool
	}{{1, true}, {MaxAmountSen - 1, true}, {MaxAmountSen, false}, {0, false}, {-1, false}}
	for _, tc := range cases {
		_, err := svc.Create(ctx, incomeInput("2026-11-09", tc.sen, catClientRevenue, "x"))
		if (err == nil) != tc.ok {
			t.Fatalf("income %d: err = %v", tc.sen, err)
		}
	}
	for i := 0; i < 20; i++ {
		mustCreate(t, svc, incomeInput("2026-11-09", MaxAmountSen-1, catClientRevenue, "big"))
	}
	mustCreate(t, svc, input("2026-11-09", MaxAmountSen-1, "big expense"))
	r, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	wantInc := 21*(MaxAmountSen-1) + 1 // + the 1-sen row from the bounds table
	if r.IncomeSen != wantInc || r.TotalSen != MaxAmountSen-1 || r.NetSen != wantInc-(MaxAmountSen-1) {
		t.Fatalf("list = income %d total %d net %d", r.IncomeSen, r.TotalSen, r.NetSen)
	}
	s, _ := svc.Summary(ctx, "2026-11")
	if s.IncomeSen != r.IncomeSen || s.NetSen != r.NetSen || s.Trend[5].IncomeSen != r.IncomeSen {
		t.Fatalf("summary = %+v", s)
	}
}
