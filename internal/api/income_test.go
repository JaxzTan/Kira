package api

import (
	"strings"
	"testing"
	"time"
)

// FR-9 Income through the HTTP API. Seeded income categories are ids 11–14
// (Client revenue, Product sales, Refunds & rebates, Other income).

func incomeInput() map[string]any {
	return map[string]any{
		"direction": "income", "occurred_on": today(), "amount_sen": 500000, "category_id": 11,
		"payee": "Acme Sdn Bhd", "method": "bank_transfer", "note": "Invoice 42",
		"receipt_sha256": nil, "confirm_date": false,
	}
}

func TestIncomeCreateAmendVoid(t *testing.T) {
	h := newHarness(t)
	cr := h.create(incomeInput())
	if cr.Direction != "income" || cr.Entry.Direction != "income" || cr.Entry.CategoryName != "Client revenue" || cr.Entry.AmountSen != 500000 {
		t.Fatalf("create = %+v", cr)
	}
	h.create(validInput()) // expense 4590, id 2

	// amend income: change amount and category (still income)
	am := incomeInput()
	am["amount_sen"], am["category_id"] = 450000, 12
	rec := h.do("POST", "/api/entries/1/amend", am)
	wantStatus(t, rec, 201)
	ar := decode[AppendResult](t, rec)
	if ar.Kind != KindAmend || ar.Entry.Direction != "income" || ar.Entry.AmountSen != 450000 || ar.Entry.CategoryName != "Product sales" {
		t.Fatalf("amend = %+v", ar)
	}

	l := decode[EntryList](t, h.do("GET", "/api/entries", nil))
	if l.TotalSen != 4590 || l.IncomeSen != 450000 || l.NetSen != 450000-4590 || l.Count != 2 {
		t.Fatalf("list = %+v", l)
	}

	// amend an expense into income: allowed only together with an income category
	flip := validInput()
	flip["direction"] = "income"
	e := wantError(t, h.do("POST", "/api/entries/2/amend", flip), 422, CodeValidation)
	if e.Fields["category_id"] == "" {
		t.Fatalf("flip without category: %+v", e)
	}
	flip["category_id"] = 13
	wantStatus(t, h.do("POST", "/api/entries/2/amend", flip), 201)
	l = decode[EntryList](t, h.do("GET", "/api/entries", nil))
	if l.TotalSen != 0 || l.IncomeSen != 450000+4590 || l.NetSen != 450000+4590 {
		t.Fatalf("after flip = %+v", l)
	}

	// void the income entry
	rec = h.do("POST", "/api/entries/1/void", map[string]string{"reason": "sent twice"})
	wantStatus(t, rec, 201)
	if vr := decode[AppendResult](t, rec); vr.Entry.Status != StatusVoided || vr.Entry.Direction != "income" {
		t.Fatalf("void = %+v", vr)
	}
	l = decode[EntryList](t, h.do("GET", "/api/entries", nil))
	if l.IncomeSen != 4590 || l.NetSen != 4590 || l.Count != 1 || l.VoidedCount != 1 {
		t.Fatalf("after void = %+v", l)
	}
	if v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil)); !v.OK {
		t.Fatalf("verify = %+v", v)
	}
}

func TestIncomeCategoryMismatch(t *testing.T) {
	h := newHarness(t)
	in := incomeInput()
	in["category_id"] = 1 // expense category
	e := wantError(t, h.do("POST", "/api/entries", in), 422, CodeValidation)
	if !strings.Contains(e.Fields["category_id"], "expenses") {
		t.Fatalf("income on expense category: %+v", e)
	}
	ex := validInput()
	ex["category_id"] = 11 // income category
	e = wantError(t, h.do("POST", "/api/entries", ex), 422, CodeValidation)
	if !strings.Contains(e.Fields["category_id"], "income") {
		t.Fatalf("expense on income category: %+v", e)
	}
	bad := validInput()
	bad["direction"] = "refund"
	e = wantError(t, h.do("POST", "/api/entries", bad), 422, CodeValidation)
	if e.Fields["direction"] == "" {
		t.Fatalf("bad direction: %+v", e)
	}
	if l := decode[EntryList](t, h.do("GET", "/api/entries", nil)); l.Count != 0 {
		t.Fatalf("rejected entries were written: %+v", l)
	}
}

func TestEntriesDirectionFilter(t *testing.T) {
	h := newHarness(t)
	h.create(incomeInput())
	h.create(validInput())
	for _, tt := range []struct {
		q          string
		n          int
		dir        string
		total, inc int64
	}{
		{"", 2, "", 4590, 500000},
		{"?direction=all", 2, "", 4590, 500000},
		{"?direction=expense", 1, "expense", 4590, 0},
		{"?direction=income", 1, "income", 0, 500000},
	} {
		l := decode[EntryList](t, h.do("GET", "/api/entries"+tt.q, nil))
		if len(l.Entries) != tt.n || l.TotalSen != tt.total || l.IncomeSen != tt.inc || l.NetSen != tt.inc-tt.total {
			t.Fatalf("%q: %+v", tt.q, l)
		}
		if tt.dir != "" && l.Entries[0].Direction != tt.dir {
			t.Fatalf("%q: wrong direction %+v", tt.q, l.Entries[0])
		}
	}
	wantError(t, h.do("GET", "/api/entries?direction=refund", nil), 400, CodeBadRequest)
	wantError(t, h.do("GET", "/api/entries?direction=INCOME", nil), 400, CodeBadRequest)
}

func TestSummaryIncome(t *testing.T) {
	h := newHarness(t)
	h.create(incomeInput())
	h.create(validInput())
	month := time.Now().Format("2006-01")
	s := decode[Summary](t, h.do("GET", "/api/summary?month="+month, nil))
	if s.TotalSen != 4590 || s.IncomeSen != 500000 || s.NetSen != 500000-4590 || s.PrevMonthIncomeSen != 0 ||
		s.Count != 2 || s.MissingReceipts != 1 || len(s.Recent) != 2 {
		t.Fatalf("summary = %+v", s)
	}
	for _, c := range s.ByCategory {
		if c.CategoryID >= 11 && c.CategoryID <= 14 {
			t.Fatalf("by_category includes income: %+v", s.ByCategory)
		}
	}
	last := s.Trend[len(s.Trend)-1]
	if last.Month != month || last.TotalSen != 4590 || last.IncomeSen != 500000 {
		t.Fatalf("trend = %+v", s.Trend)
	}
	// JSON field names per contract
	body := h.do("GET", "/api/summary?month="+month, nil).Body.String()
	for _, k := range []string{`"income_sen":500000`, `"net_sen":495410`, `"prev_month_income_sen":0`} {
		if !strings.Contains(body, k) {
			t.Fatalf("summary json missing %s: %s", k, body)
		}
	}
	body = h.do("GET", "/api/entries", nil).Body.String()
	if !strings.Contains(body, `"income_sen":500000`) || !strings.Contains(body, `"net_sen":495410`) {
		t.Fatalf("list json: %s", body)
	}
}

func TestCategoriesDirection(t *testing.T) {
	h := newHarness(t)
	inc := decode[CategoryList](t, h.do("GET", "/api/categories?direction=income", nil))
	if len(inc.Categories) != 4 || inc.Categories[0].Name != "Client revenue" {
		t.Fatalf("income cats = %+v", inc)
	}
	for _, c := range inc.Categories {
		if c.Direction != "income" {
			t.Fatalf("not income: %+v", c)
		}
	}
	if ex := decode[CategoryList](t, h.do("GET", "/api/categories?direction=expense", nil)); len(ex.Categories) != 10 {
		t.Fatalf("expense cats = %d", len(ex.Categories))
	}
	if all := decode[CategoryList](t, h.do("GET", "/api/categories?direction=all", nil)); len(all.Categories) != 14 {
		t.Fatalf("all cats = %d", len(all.Categories))
	}
	wantError(t, h.do("GET", "/api/categories?direction=both", nil), 400, CodeBadRequest)

	rec := h.do("POST", "/api/categories", map[string]any{"name": "Grants", "direction": "income"})
	wantStatus(t, rec, 201)
	if c := decode[Category](t, rec); c.Direction != "income" || c.Name != "Grants" {
		t.Fatalf("created = %+v", c)
	}
	rec = h.do("POST", "/api/categories", map[string]any{"name": "Courses"})
	wantStatus(t, rec, 201)
	if c := decode[Category](t, rec); c.Direction != "expense" {
		t.Fatalf("default direction = %+v", c)
	}
	e := wantError(t, h.do("POST", "/api/categories", map[string]any{"name": "Gifts", "direction": "refund"}), 422, CodeValidation)
	if e.Fields["direction"] == "" {
		t.Fatalf("bad direction fields = %+v", e)
	}
	if inc = decode[CategoryList](t, h.do("GET", "/api/categories?direction=income", nil)); len(inc.Categories) != 5 {
		t.Fatalf("income after create = %d", len(inc.Categories))
	}
	// direction is immutable: PATCH has no direction field (strict decode → 400)
	wantError(t, h.do("PATCH", "/api/categories/11", map[string]any{"direction": "expense"}), 400, CodeBadRequest)
	// new income category is usable for income entries
	in := incomeInput()
	in["category_id"] = 15
	h.create(in)
}

func TestExportTypeColumn(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	h.create(incomeInput())
	from, to := time.Now().AddDate(0, -1, 0).Format("2006-01-02"), today()
	rec := h.do("GET", "/api/export?from="+from+"&to="+to, nil)
	wantStatus(t, rec, 200)
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	body := rec.Body.String()
	lines := strings.Split(strings.TrimRight(body, "\r\n"), "\r\n")
	if lines[0] != strings.Join(CSVHeader, ",") || !strings.HasSuffix(lines[0], ",type") {
		t.Fatalf("header %q", lines[0])
	}
	var sawInc, sawExp bool
	for _, ln := range lines[1:] {
		switch {
		case strings.Contains(ln, ",Client revenue,Acme Sdn Bhd,") && strings.HasSuffix(ln, ",income") && strings.Contains(ln, ",5000.00,"):
			sawInc = true
		case strings.Contains(ln, ",Cloudflare,") && strings.HasSuffix(ln, ",expense"):
			sawExp = true
		}
	}
	if !sawInc || !sawExp {
		t.Fatalf("csv rows:\n%s", body)
	}
}
