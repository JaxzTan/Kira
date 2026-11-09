package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// All reads below use v_current (TR-9) except History, which reads the raw chain.

const entryCols = `id, version_id, direction, occurred_on, amount_sen, currency, category_id, category_name,
	payee, method, note, receipt_sha256, status, amend_count, void_reason, created_at, updated_at, hash`

func scanEntry(sc interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	var date, payee, method sql.NullString
	var amt, cat sql.NullInt64
	var note, receipt, voidReason sql.NullString
	err := sc.Scan(&e.ID, &e.VersionID, &e.Direction, &date, &amt, &e.Currency, &cat, &e.CategoryName,
		&payee, &method, &note, &receipt, &e.Status, &e.AmendCount, &voidReason, &e.CreatedAt, &e.UpdatedAt, &e.Hash)
	if err != nil {
		return e, err
	}
	e.OccurredOn, e.AmountSen, e.CategoryID = date.String, amt.Int64, cat.Int64
	e.Payee, e.Method = payee.String, method.String
	e.Note, e.ReceiptSHA256, e.VoidReason = nstr(note), nstr(receipt), nstr(voidReason)
	return e, nil
}

func nstr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func nint(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func getEntry(ctx context.Context, q queryer, id int64) (Entry, error) {
	e, err := scanEntry(q.QueryRowContext(ctx, `SELECT `+entryCols+` FROM v_current WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("ledger: get entry: %w", err)
	}
	return e, nil
}

func queryEntries(ctx context.Context, q queryer, where string, args ...any) ([]Entry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+entryCols+` FROM v_current `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: query entries: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("ledger: scan entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns the current view of original create id, or ErrNotFound
// (also for ids of amend/void rows).
func (s *Service) Get(ctx context.Context, id int64) (Entry, error) { return getEntry(ctx, s.db, id) }

// History returns the entry and its full chain (create + amends/voids), oldest first.
func (s *Service) History(ctx context.Context, id int64) (Entry, []Row, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Entry{}, nil, err
	}
	defer tx.Rollback()
	e, err := getEntry(ctx, tx, id)
	if err != nil {
		return Entry{}, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, kind, ref_id, direction, occurred_on, amount_sen, currency, category_id,
		payee, method, note, void_reason, receipt_sha256, created_at, prev_hash, hash
		FROM entries WHERE id = ? OR ref_id = ? ORDER BY id`, id, id)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("ledger: history: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var ref, amt, cat sql.NullInt64
		var date, payee, method, note, vr, receipt sql.NullString
		if err := rows.Scan(&r.ID, &r.Kind, &ref, &r.Direction, &date, &amt, &r.Currency, &cat,
			&payee, &method, &note, &vr, &receipt, &r.CreatedAt, &r.PrevHash, &r.Hash); err != nil {
			return Entry{}, nil, fmt.Errorf("ledger: history scan: %w", err)
		}
		r.RefID, r.AmountSen, r.CategoryID = nint(ref), nint(amt), nint(cat)
		r.OccurredOn, r.Payee, r.Method, r.Note, r.VoidReason, r.ReceiptSHA256 =
			nstr(date), nstr(payee), nstr(method), nstr(note), nstr(vr), nstr(receipt)
		out = append(out, r)
	}
	return e, out, rows.Err()
}

// List returns current entries matching f, ordered by occurred_on desc, id desc.
// Voided entries are included (unless filtered) but excluded from TotalSen/Count.
// Filter errors wrap ErrBadRequest.
func (s *Service) List(ctx context.Context, f Filter) (ListResult, error) {
	f, err := ValidateFilter(f)
	if err != nil {
		return ListResult{}, err
	}
	var conds []string
	var args []any
	if f.From != "" {
		conds, args = append(conds, "occurred_on >= ?"), append(args, f.From)
	}
	if f.To != "" {
		conds, args = append(conds, "occurred_on <= ?"), append(args, f.To)
	}
	if f.CategoryID > 0 {
		conds, args = append(conds, "category_id = ?"), append(args, f.CategoryID)
	}
	if f.Method != "" {
		conds, args = append(conds, "method = ?"), append(args, f.Method)
	}
	switch f.Status {
	case StatusActive:
		conds = append(conds, "status <> 'voided'")
	case StatusVoided:
		conds = append(conds, "status = 'voided'")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	all, err := queryEntries(ctx, s.db, where+" ORDER BY occurred_on DESC, id DESC", args...)
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{Entries: all}
	if f.Q != "" {
		// Done in Go: SQLite lower()/LIKE only fold ASCII.
		q := strings.ToLower(f.Q)
		res.Entries = res.Entries[:0:0]
		for _, e := range all {
			if strings.Contains(strings.ToLower(e.Payee), q) || (e.Note != nil && strings.Contains(strings.ToLower(*e.Note), q)) {
				res.Entries = append(res.Entries, e)
			}
		}
	}
	for _, e := range res.Entries {
		if e.Status == StatusVoided {
			res.VoidedCount++
			continue
		}
		res.TotalSen += e.AmountSen
		res.Count++
	}
	return res, nil
}

// ExportEntries returns current entries with from <= occurred_on <= to,
// INCLUDING voided, ordered by occurred_on asc, id asc (TR-10).
func (s *Service) ExportEntries(ctx context.Context, from, to string) ([]Entry, error) {
	if _, err := ParseDate(from); err != nil {
		return nil, fmt.Errorf("%w: from must be YYYY-MM-DD", ErrBadRequest)
	}
	if _, err := ParseDate(to); err != nil {
		return nil, fmt.Errorf("%w: to must be YYYY-MM-DD", ErrBadRequest)
	}
	return queryEntries(ctx, s.db, `WHERE occurred_on >= ? AND occurred_on <= ? ORDER BY occurred_on ASC, id ASC`, from, to)
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// Summary computes the dashboard for month "YYYY-MM" ("" = current month,
// server local time). Errors on a bad month wrap ErrBadRequest.
func (s *Service) Summary(ctx context.Context, month string) (Summary, error) {
	var m time.Time
	if month == "" {
		now := s.today()
		m = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		var err error
		if m, err = ParseMonth(month); err != nil {
			return Summary{}, err
		}
	}
	day := func(t time.Time) string { return t.Format(time.DateOnly) }
	start, end := m, m.AddDate(0, 1, 0)
	trendStart := m.AddDate(0, -5, 0)
	out := Summary{Month: m.Format("2006-01"), ByCategory: []CategoryTotal{}}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Summary{}, err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status <> 'voided' THEN amount_sen END), 0),
		COUNT(CASE WHEN status <> 'voided' THEN 1 END),
		COUNT(CASE WHEN status = 'voided' THEN 1 END),
		COUNT(CASE WHEN status <> 'voided' AND receipt_sha256 IS NULL THEN 1 END)
		FROM v_current WHERE occurred_on >= ? AND occurred_on < ?`, day(start), day(end)).
		Scan(&out.TotalSen, &out.Count, &out.VoidedCount, &out.MissingReceipts)
	if err != nil {
		return Summary{}, fmt.Errorf("ledger: summary totals: %w", err)
	}

	// Trend (includes the previous month).
	byMonth := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT substr(occurred_on, 1, 7), SUM(amount_sen) FROM v_current
		WHERE status <> 'voided' AND occurred_on >= ? AND occurred_on < ? GROUP BY 1`, day(trendStart), day(end))
	if err != nil {
		return Summary{}, fmt.Errorf("ledger: summary trend: %w", err)
	}
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return Summary{}, err
		}
		byMonth[k] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}
	for i := 0; i < 6; i++ {
		k := trendStart.AddDate(0, i, 0).Format("2006-01")
		out.Trend = append(out.Trend, MonthTotal{Month: k, TotalSen: byMonth[k]})
	}
	out.PrevMonthTotalSen = byMonth[m.AddDate(0, -1, 0).Format("2006-01")]
	if out.PrevMonthTotalSen != 0 {
		pct := round1(float64(out.TotalSen-out.PrevMonthTotalSen) * 100 / float64(out.PrevMonthTotalSen))
		out.ChangePct = &pct
	}

	rows, err = tx.QueryContext(ctx, `SELECT v.category_id, COALESCE(c.name, ''), c.color, SUM(v.amount_sen)
		FROM v_current v LEFT JOIN categories c ON c.id = v.category_id
		WHERE v.status <> 'voided' AND v.occurred_on >= ? AND v.occurred_on < ?
		GROUP BY v.category_id`, day(start), day(end))
	if err != nil {
		return Summary{}, fmt.Errorf("ledger: summary categories: %w", err)
	}
	for rows.Next() {
		var ct CategoryTotal
		var color sql.NullString
		if err := rows.Scan(&ct.CategoryID, &ct.Name, &color, &ct.TotalSen); err != nil {
			rows.Close()
			return Summary{}, err
		}
		if ct.TotalSen == 0 {
			continue
		}
		ct.Color = nstr(color)
		if out.TotalSen > 0 {
			ct.Pct = round1(float64(ct.TotalSen) * 100 / float64(out.TotalSen))
		}
		out.ByCategory = append(out.ByCategory, ct)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}
	sort.SliceStable(out.ByCategory, func(i, j int) bool {
		a, b := out.ByCategory[i], out.ByCategory[j]
		if a.TotalSen != b.TotalSen {
			return a.TotalSen > b.TotalSen
		}
		return a.CategoryID < b.CategoryID
	})

	out.Recent, err = queryEntries(ctx, tx, `ORDER BY occurred_on DESC, id DESC LIMIT 5`)
	if err != nil {
		return Summary{}, err
	}
	return out, nil
}
