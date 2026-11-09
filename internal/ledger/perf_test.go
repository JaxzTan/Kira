package ledger

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/store"
)

// seedFast inserts n hash-chained rows in ONE transaction (bypassing the
// per-append fsync, which would take minutes): ~90% creates spread over
// two years, ~8% amends, ~2% voids. Every 7th create is income (income
// category, FR-9); amends keep their entry's direction. The chain is valid
// (Verify passes).
func seedFast(t testing.TB, svc *Service, n int) {
	t.Helper()
	ctx := context.Background()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO entries
		(id, kind, ref_id, direction, occurred_on, amount_sen, currency, category_id, payee, method, note, void_reason, receipt_sha256, created_at, prev_hash, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	payees := []string{"Cloudflare", "GitHub", "Grab", "Shopee", "Maxis", "Starbucks", "AWS", "Figma", "Notion", "Café Ölçü"}
	methods := []string{MethodCard, MethodCash, MethodGrabPay, MethodTNGEWallet}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	created := []int64{}
	voided := map[int64]bool{}
	income := map[int64]bool{}
	prev := GenesisHash
	for i := 1; i <= n; i++ {
		id := int64(i)
		r := Row{ID: id, Currency: CurrencyMYR, Direction: DirectionExpense, PrevHash: prev,
			CreatedAt: store.FormatTime(base.Add(time.Duration(i) * time.Minute))}
		switch {
		case i%50 == 0 && len(created) > 10:
			ref := created[(i*7)%len(created)]
			if voided[ref] {
				ref = created[len(created)-1]
			}
			if !voided[ref] {
				voided[ref] = true
				reason := "dup"
				r.Kind, r.RefID, r.VoidReason = KindVoid, &ref, &reason
				if income[ref] {
					r.Direction = DirectionIncome // void rows carry the entry's direction
				}
				break
			}
			fallthrough
		case i%12 == 0 && len(created) > 10:
			ref := created[(i*13)%len(created)]
			if !voided[ref] {
				r.Kind, r.RefID = KindAmend, &ref
			} else {
				r.Kind = KindCreate
			}
		default:
			r.Kind = KindCreate
		}
		if r.Kind != KindVoid {
			date := base.AddDate(0, 0, i%700).Format(time.DateOnly)
			amt, cat := int64(100+i%50000), int64(1+i%10)
			isIncome := (r.Kind == KindCreate && i%7 == 0) || (r.Kind == KindAmend && income[*r.RefID])
			if isIncome {
				r.Direction, cat = DirectionIncome, int64(11+i%4)
				if r.Kind == KindCreate {
					income[id] = true
				}
			}
			payee, method := payees[i%len(payees)], methods[i%len(methods)]
			r.OccurredOn, r.AmountSen, r.CategoryID, r.Payee, r.Method = &date, &amt, &cat, &payee, &method
			if i%3 == 0 {
				note := fmt.Sprintf("invoice %d for project", i)
				r.Note = &note
			}
		}
		if r.Kind == KindCreate {
			created = append(created, id)
		}
		if r.Hash, err = RowHash(svc.key, r); err != nil {
			t.Fatal(err)
		}
		if _, err := stmt.ExecContext(ctx, r.ID, r.Kind, ptrVal(r.RefID), r.Direction, ptrVal(r.OccurredOn), ptrVal(r.AmountSen), r.Currency,
			ptrVal(r.CategoryID), ptrVal(r.Payee), ptrVal(r.Method), ptrVal(r.Note), ptrVal(r.VoidReason),
			ptrVal(r.ReceiptSHA256), r.CreatedAt, r.PrevHash, r.Hash); err != nil {
			t.Fatal(err)
		}
		prev = r.Hash
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestNFR6Performance: with ~20k entries, List (no filter, and with q) and
// Summary each complete in < 200 ms (NFR-6).
func TestNFR6Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("NFR-6 timing test skipped under -short")
	}
	if raceEnabled {
		t.Skip("NFR-6 timing test skipped under -race")
	}
	ctx := context.Background()
	svc, _ := newTestService(t)
	seedFast(t, svc, 20_000)
	if res, err := svc.Verify(ctx); err != nil || !res.OK || res.Entries != 20_000 {
		t.Fatalf("seed verify: %+v %v", res, err)
	}
	const limit = 200 * time.Millisecond
	cases := []struct {
		name string
		run  func() (int, error)
	}{
		{"List no filter", func() (int, error) { r, err := svc.List(ctx, Filter{}); return len(r.Entries), err }},
		{"List q", func() (int, error) { r, err := svc.List(ctx, Filter{Q: "flare"}); return len(r.Entries), err }},
		{"List q non-ascii", func() (int, error) { r, err := svc.List(ctx, Filter{Q: "ÖLÇÜ"}); return len(r.Entries), err }},
		{"List q note", func() (int, error) { r, err := svc.List(ctx, Filter{Q: "invoice 1"}); return len(r.Entries), err }},
		{"List month + category", func() (int, error) {
			r, err := svc.List(ctx, Filter{From: "2025-03-01", To: "2025-03-31", CategoryID: 3})
			return len(r.Entries), err
		}},
		{"List income", func() (int, error) {
			r, err := svc.List(ctx, Filter{Direction: DirectionIncome})
			return len(r.Entries), err
		}},
		{"Summary", func() (int, error) { s, err := svc.Summary(ctx, "2025-06"); return s.Count, err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			best := time.Hour
			var n int
			for i := 0; i < 3; i++ { // best of 3 (first run warms the page cache)
				start := time.Now()
				var err error
				if n, err = tc.run(); err != nil {
					t.Fatal(err)
				}
				if d := time.Since(start); d < best {
					best = d
				}
			}
			t.Logf("%s: %v (%d rows)", tc.name, best, n)
			if n == 0 {
				t.Fatal("query returned nothing; seed broken")
			}
			if best > limit {
				t.Fatalf("%s took %v, want < %v", tc.name, best, limit)
			}
		})
	}
}
