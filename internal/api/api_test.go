package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/ledger"
)

func TestUnauthorizedWithoutSession(t *testing.T) {
	h := newHarness(t)
	sha := strings.Repeat("a", 64)
	tests := []struct{ method, path string }{
		{"GET", "/api/me"},
		{"GET", "/api/entries"},
		{"POST", "/api/entries"},
		{"GET", "/api/entries/1"},
		{"GET", "/api/entries/1/history"},
		{"POST", "/api/entries/1/amend"},
		{"POST", "/api/entries/1/void"},
		{"POST", "/api/receipts"},
		{"GET", "/api/receipts/" + sha},
		{"GET", "/api/categories"},
		{"POST", "/api/categories"},
		{"PATCH", "/api/categories/1"},
		{"GET", "/api/summary"},
		{"GET", "/api/export?from=2026-01-01&to=2026-12-31"},
		{"GET", "/api/ledger/verify"},
		{"POST", "/api/ledger/acknowledge"},
		{"GET", "/api/unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var body any
			if tt.method != "GET" {
				body = "{}"
			}
			wantError(t, h.serve(h.req(tt.method, tt.path, body)), http.StatusUnauthorized, CodeUnauthorized)
		})
	}
	// A forged / unknown token is also 401.
	r := h.req("GET", "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: "kira_session", Value: strings.Repeat("A", 43)})
	wantError(t, h.serve(r), http.StatusUnauthorized, CodeUnauthorized)
}

func TestMe(t *testing.T) {
	h := newHarness(t)
	rec := h.do("GET", "/api/me", nil)
	wantStatus(t, rec, 200)
	if got := rec.Body.String(); !strings.Contains(got, `"email":"owner@example.com"`) || !strings.Contains(got, `"name":null`) || !strings.Contains(got, `"picture":null`) {
		t.Fatalf("me = %s", got)
	}
}

func TestEntryLifecycle(t *testing.T) {
	h := newHarness(t)
	// create
	cr := h.create(validInput())
	if cr.Kind != KindCreate || cr.RefID != nil || cr.ID != 1 || cr.Entry.ID != 1 || cr.Entry.Status != StatusActive ||
		cr.Entry.CategoryName != "Cloud & hosting" || cr.PrevHash != ledger.GenesisHash || len(cr.Hash) != 64 {
		t.Fatalf("create = %+v", cr)
	}
	if hd, _ := h.head.Read(); hd == nil || hd.ID != 1 || hd.Hash != cr.Hash {
		t.Fatalf("head file not advanced: %+v", hd)
	}
	second := validInput()
	second["payee"], second["amount_sen"], second["method"] = "Grab", 1200, "grabpay"
	h.create(second)

	// get + list
	rec := h.do("GET", "/api/entries/1", nil)
	wantStatus(t, rec, 200)
	if e := decode[Entry](t, rec); e.Payee != "Cloudflare" || e.VersionID != 1 || e.Note == nil {
		t.Fatalf("get = %+v", e)
	}
	list := decode[EntryList](t, h.do("GET", "/api/entries", nil))
	if list.Count != 2 || list.TotalSen != 5790 || len(list.Entries) != 2 || list.Entries[0].ID != 2 {
		t.Fatalf("list = %+v", list)
	}

	// amend
	am := validInput()
	am["amount_sen"], am["note"] = 4480, "corrected"
	rec = h.do("POST", "/api/entries/1/amend", am)
	wantStatus(t, rec, 201)
	ar := decode[AppendResult](t, rec)
	if ar.Kind != KindAmend || ar.RefID == nil || *ar.RefID != 1 || ar.Entry.Status != StatusAmended ||
		ar.Entry.AmendCount != 1 || ar.Entry.VersionID != ar.ID || ar.Entry.AmountSen != 4480 {
		t.Fatalf("amend = %+v", ar)
	}
	// amending the amend row itself is not allowed
	wantError(t, h.do("POST", fmt.Sprintf("/api/entries/%d/amend", ar.ID), am), 409, CodeConflict)

	// void
	rec = h.do("POST", "/api/entries/1/void", map[string]string{"reason": "Duplicate of #2"})
	wantStatus(t, rec, 201)
	vr := decode[AppendResult](t, rec)
	if vr.Kind != KindVoid || vr.AmountSen != nil || vr.VoidReason == nil || vr.Entry.Status != StatusVoided || vr.Entry.AmountSen != 4480 {
		t.Fatalf("void = %+v", vr)
	}
	// amend/void after void → 409
	wantError(t, h.do("POST", "/api/entries/1/amend", am), 409, CodeConflict)
	wantError(t, h.do("POST", "/api/entries/1/void", map[string]string{"reason": "again"}), 409, CodeConflict)

	// history: create, amend, void ascending
	hist := decode[EntryHistory](t, h.do("GET", "/api/entries/1/history", nil))
	if len(hist.Rows) != 3 || hist.Rows[0].Kind != KindCreate || hist.Rows[1].Kind != KindAmend || hist.Rows[2].Kind != KindVoid ||
		hist.Entry.Status != StatusVoided {
		t.Fatalf("history = %+v", hist)
	}
	// list totals exclude voided; status filter
	list = decode[EntryList](t, h.do("GET", "/api/entries", nil))
	if list.Count != 1 || list.VoidedCount != 1 || list.TotalSen != 1200 || len(list.Entries) != 2 {
		t.Fatalf("list after void = %+v", list)
	}
	list = decode[EntryList](t, h.do("GET", "/api/entries?status=voided", nil))
	if len(list.Entries) != 1 || list.Entries[0].ID != 1 {
		t.Fatalf("voided filter = %+v", list)
	}
	list = decode[EntryList](t, h.do("GET", "/api/entries?q=grab&method=grabpay&category=1&from=2000-01-01&to=2999-12-31", nil))
	if len(list.Entries) != 1 || list.Entries[0].Payee != "Grab" {
		t.Fatalf("filtered = %+v", list)
	}

	// head file tracks every append
	if hd, _ := h.head.Read(); hd == nil || hd.ID != vr.ID || hd.Hash != vr.Hash {
		t.Fatalf("head = %+v, want %d", hd, vr.ID)
	}
	// session token never logged
	if strings.Contains(h.logs.String(), h.token) {
		t.Fatal("session token in logs")
	}
}

func TestEntryErrors(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	old := time.Now().AddDate(-2, 0, 0).Format("2006-01-02")
	with := func(k string, v any) map[string]any { m := validInput(); m[k] = v; return m }

	tests := []struct {
		name       string
		method     string
		path       string
		body       any
		ctype      string // override Content-Type ("" = keep)
		status     int
		code       string
		wantFields []string
	}{
		{name: "validation fields", method: "POST", path: "/api/entries",
			body: func() map[string]any {
				m := validInput()
				m["amount_sen"] = 0
				m["payee"] = "  "
				m["method"] = "cheque"
				return m
			}(),
			status: 422, code: CodeValidation, wantFields: []string{"amount_sen", "payee", "method"}},
		{name: "amount too large", method: "POST", path: "/api/entries", body: with("amount_sen", 1_000_000_000), status: 422, code: CodeValidation, wantFields: []string{"amount_sen"}},
		{name: "bad date", method: "POST", path: "/api/entries", body: with("occurred_on", "2026-13-40"), status: 422, code: CodeValidation, wantFields: []string{"occurred_on"}},
		{name: "unknown category", method: "POST", path: "/api/entries", body: with("category_id", 999), status: 422, code: CodeValidation, wantFields: []string{"category_id"}},
		{name: "amount as string", method: "POST", path: "/api/entries", body: with("amount_sen", "45.90"), status: 422, code: CodeValidation, wantFields: []string{"amount_sen"}},
		{name: "date confirm required", method: "POST", path: "/api/entries", body: with("occurred_on", old), status: 422, code: CodeDateConfirmRequired},
		{name: "unknown field", method: "POST", path: "/api/entries", body: with("amount_myr", "45.90"), status: 400, code: CodeBadRequest},
		{name: "malformed json", method: "POST", path: "/api/entries", body: `{"amount_sen":`, status: 400, code: CodeBadRequest},
		{name: "two objects", method: "POST", path: "/api/entries", body: `{} {}`, status: 400, code: CodeBadRequest},
		{name: "empty body", method: "POST", path: "/api/entries", body: ``, status: 400, code: CodeBadRequest},
		{name: "wrong content type", method: "POST", path: "/api/entries", body: `{}`, ctype: "text/plain", status: 415, code: CodeUnsupportedMedia},
		{name: "form post (CSRF)", method: "POST", path: "/api/entries", body: `a=b`, ctype: "application/x-www-form-urlencoded", status: 415, code: CodeUnsupportedMedia},
		{name: "body too large", method: "POST", path: "/api/entries", body: `{"payee":"` + strings.Repeat("x", 70<<10) + `"}`, status: 413, code: CodePayloadTooLarge},
		{name: "get non-numeric id", method: "GET", path: "/api/entries/abc", status: 400, code: CodeBadRequest},
		{name: "get missing", method: "GET", path: "/api/entries/999", status: 404, code: CodeNotFound},
		{name: "history missing", method: "GET", path: "/api/entries/999/history", status: 404, code: CodeNotFound},
		{name: "amend missing", method: "POST", path: "/api/entries/999/amend", body: validInput(), status: 404, code: CodeNotFound},
		{name: "void empty reason", method: "POST", path: "/api/entries/1/void", body: map[string]string{"reason": " "}, status: 422, code: CodeValidation, wantFields: []string{"reason"}},
		{name: "list bad from", method: "GET", path: "/api/entries?from=yesterday", status: 400, code: CodeBadRequest},
		{name: "list bad category", method: "GET", path: "/api/entries?category=x", status: 400, code: CodeBadRequest},
		{name: "list bad status", method: "GET", path: "/api/entries?status=deleted", status: 400, code: CodeBadRequest},
		{name: "unknown api route", method: "GET", path: "/api/nope", status: 404, code: CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := h.authed(h.req(tt.method, tt.path, tt.body))
			if tt.ctype != "" {
				r.Header.Set("Content-Type", tt.ctype)
			}
			e := wantError(t, h.serve(r), tt.status, tt.code)
			for _, f := range tt.wantFields {
				if e.Fields[f] == "" {
					t.Errorf("missing field %s in %+v", f, e.Fields)
				}
			}
		})
	}
	// confirm_date lets the old date through
	m := with("occurred_on", old)
	m["confirm_date"] = true
	h.create(m)
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 8; i++ {
		img.Set(i, i, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func multipartReq(t *testing.T, h *harness, field string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("note", "ignored")
	fw, err := mw.CreateFormFile(field, "receipt.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/receipts", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.RemoteAddr = h.remote
	return h.authed(r)
}

func TestReceipts(t *testing.T) {
	h := newHarness(t)
	rec := h.serve(multipartReq(t, h, "file", pngBytes(t)))
	wantStatus(t, rec, 201)
	rc := decode[Receipt](t, rec)
	if rc.Ext != "png" || rc.ContentType != "image/png" || len(rc.SHA256) != 64 || rc.Size == 0 {
		t.Fatalf("receipt = %+v", rc)
	}
	// idempotent
	if again := decode[Receipt](t, h.serve(multipartReq(t, h, "file", pngBytes(t)))); again.SHA256 != rc.SHA256 {
		t.Fatal("upload not idempotent")
	}

	rec = h.do("GET", "/api/receipts/"+rc.SHA256, nil)
	wantStatus(t, rec, 200)
	hd := rec.Header()
	if hd.Get("Content-Type") != "image/png" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Content-Disposition") != `inline; filename="`+rc.SHA256+`.png"` ||
		hd.Get("Cache-Control") != "private, max-age=31536000, immutable" ||
		!strings.Contains(hd.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("receipt headers = %v", hd)
	}
	if _, err := png.Decode(rec.Body); err != nil {
		t.Fatalf("served file is not a PNG: %v", err)
	}

	// attach to an entry
	in := validInput()
	in["receipt_sha256"] = rc.SHA256
	if cr := h.create(in); cr.Entry.ReceiptSHA256 == nil || *cr.Entry.ReceiptSHA256 != rc.SHA256 {
		t.Fatalf("entry receipt = %+v", cr.Entry)
	}
	// unknown receipt sha on an entry → 422
	in["receipt_sha256"] = strings.Repeat("b", 64)
	wantError(t, h.do("POST", "/api/entries", in), 422, CodeValidation)

	tests := []struct {
		name   string
		req    *http.Request
		status int
		code   string
	}{
		{"text file", multipartReq(t, h, "file", []byte("hello, not an image")), 415, CodeUnsupportedMedia},
		{"broken png", multipartReq(t, h, "file", append([]byte("\x89PNG\r\n\x1a\n"), 1, 2, 3)), 415, CodeUnsupportedMedia},
		{"no file field", multipartReq(t, h, "upload", pngBytes(t)), 422, CodeValidation},
		{"json content type", h.authed(h.req("POST", "/api/receipts", `{}`)), 415, CodeUnsupportedMedia},
		{"too large", multipartReq(t, h, "file", bytes.Repeat([]byte{0}, 12<<20)), 413, CodePayloadTooLarge},
		{"just over 10MB", multipartReq(t, h, "file", append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{' '}, 10<<20)...)), 413, CodePayloadTooLarge},
		{"bad sha", h.authed(h.req("GET", "/api/receipts/XYZ", nil)), 400, CodeBadRequest},
		{"unknown sha", h.authed(h.req("GET", "/api/receipts/"+strings.Repeat("c", 64), nil)), 404, CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantError(t, h.serve(tt.req), tt.status, tt.code) })
	}
}

func TestCategories(t *testing.T) {
	h := newHarness(t)
	list := decode[CategoryList](t, h.do("GET", "/api/categories", nil))
	if len(list.Categories) != 10 || list.Categories[0].Name != "Cloud & hosting" || list.Categories[9].Name != "Other" {
		t.Fatalf("seed = %+v", list)
	}
	rec := h.do("POST", "/api/categories", map[string]any{"name": " Courses ", "color": "#2F6FB5"})
	wantStatus(t, rec, 201)
	c := decode[Category](t, rec)
	if c.Name != "Courses" || c.ID != 11 || c.ArchivedAt != nil {
		t.Fatalf("created = %+v", c)
	}
	rec = h.do("PATCH", "/api/categories/11", map[string]any{"name": "Training"})
	wantStatus(t, rec, 200)
	if c = decode[Category](t, rec); c.Name != "Training" {
		t.Fatalf("renamed = %+v", c)
	}
	rec = h.do("PATCH", "/api/categories/11", map[string]any{"archived": true})
	wantStatus(t, rec, 200)
	if c = decode[Category](t, rec); c.ArchivedAt == nil {
		t.Fatal("not archived")
	}
	if l := decode[CategoryList](t, h.do("GET", "/api/categories", nil)); len(l.Categories) != 10 {
		t.Fatalf("archived listed: %d", len(l.Categories))
	}
	if l := decode[CategoryList](t, h.do("GET", "/api/categories?include_archived=1", nil)); len(l.Categories) != 11 {
		t.Fatalf("include_archived: %d", len(l.Categories))
	}
	rec = h.do("PATCH", "/api/categories/11", map[string]any{"archived": false})
	if c = decode[Category](t, rec); c.ArchivedAt != nil {
		t.Fatal("not unarchived")
	}

	tests := []struct {
		name, method, path string
		body               any
		status             int
		code               string
	}{
		{"duplicate (case-insensitive)", "POST", "/api/categories", map[string]any{"name": "cloud & HOSTING"}, 409, CodeConflict},
		{"empty name", "POST", "/api/categories", map[string]any{"name": "  "}, 422, CodeValidation},
		{"long name", "POST", "/api/categories", map[string]any{"name": strings.Repeat("n", 61)}, 422, CodeValidation},
		{"bad color", "POST", "/api/categories", map[string]any{"name": "X", "color": "red"}, 422, CodeValidation},
		{"rename to duplicate", "PATCH", "/api/categories/11", map[string]any{"name": "Office"}, 409, CodeConflict},
		{"patch nothing", "PATCH", "/api/categories/11", map[string]any{}, 400, CodeBadRequest},
		{"patch missing", "PATCH", "/api/categories/999", map[string]any{"name": "Y"}, 404, CodeNotFound},
		{"patch bad id", "PATCH", "/api/categories/x", map[string]any{"name": "Y"}, 400, CodeBadRequest},
		{"bad include flag", "GET", "/api/categories?include_archived=maybe", nil, 400, CodeBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantError(t, h.do(tt.method, tt.path, tt.body), tt.status, tt.code) })
	}
}

func TestSummary(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	month := time.Now().Format("2006-01")
	for _, path := range []string{"/api/summary", "/api/summary?month=" + month} {
		rec := h.do("GET", path, nil)
		wantStatus(t, rec, 200)
		s := decode[Summary](t, rec)
		if s.Month != month || s.TotalSen != 4590 || s.Count != 1 || len(s.Trend) != 6 || len(s.ByCategory) != 1 ||
			len(s.Recent) != 1 || s.MissingReceipts != 1 || s.ChangePct != nil {
			t.Fatalf("summary = %+v", s)
		}
	}
	empty := decode[Summary](t, h.do("GET", "/api/summary?month=1999-01", nil))
	if empty.ByCategory == nil || empty.Recent == nil || !strings.Contains(h.do("GET", "/api/summary?month=1999-01", nil).Body.String(), `"by_category":[]`) {
		t.Fatalf("empty arrays must be [] not null: %+v", empty)
	}
	wantError(t, h.do("GET", "/api/summary?month=2026-13", nil), 400, CodeBadRequest)
	wantError(t, h.do("GET", "/api/summary?month=nov", nil), 400, CodeBadRequest)
}

func TestExport(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	from, to := time.Now().AddDate(0, -1, 0).Format("2006-01-02"), today()

	rec := h.do("GET", "/api/export?from="+from+"&to="+to, nil)
	wantStatus(t, rec, 200)
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="kira-`+from+`_`+to+`.csv"` {
		t.Fatalf("disposition %q", cd)
	}
	lines := strings.Split(rec.Body.String(), "\r\n")
	if lines[0] != "date,category,payee,method,amount_myr,note,status,entry_id,receipt_file" {
		t.Fatalf("csv header %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], today()+",Cloud & hosting,Cloudflare,Card,45.90,R2 + domain,active,1,") {
		t.Fatalf("csv row %q", lines[1])
	}

	rec = h.do("GET", "/api/export?from="+from+"&to="+to+"&format=zip", nil)
	wantStatus(t, rec, 200)
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("zip content-type %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasSuffix(cd, `.zip"`) {
		t.Fatalf("zip disposition %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range zr.File {
		found = found || f.Name == "expenses.csv"
	}
	if !found {
		t.Fatal("zip has no expenses.csv")
	}

	for _, q := range []string{"from=" + from, "to=" + to, "from=x&to=" + to, "from=" + to + "&to=" + from, "from=" + from + "&to=" + to + "&format=xlsx"} {
		t.Run(q, func(t *testing.T) { wantError(t, h.do("GET", "/api/export?"+q, nil), 400, CodeBadRequest) })
	}
}

func TestVerifyAndBackupTime(t *testing.T) {
	backup := t.TempDir()
	if err := os.WriteFile(filepath.Join(backup, "wal"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(o *Options) { o.BackupDirs = []string{filepath.Join(backup, "missing"), backup} })
	cr := h.create(validInput())
	rec := h.do("GET", "/api/ledger/verify", nil)
	wantStatus(t, rec, 200)
	v := decode[VerifyResult](t, rec)
	if !v.OK || v.Entries != 1 || v.HeadID != 1 || v.HeadHash != cr.Hash || v.BrokenAt != nil || v.LastBackupAt == nil || v.Acknowledged {
		t.Fatalf("verify = %+v", v)
	}
	if _, err := time.Parse(time.RFC3339, v.CheckedAt); err != nil {
		t.Fatal(err)
	}
	// no backup dirs → null
	h2 := newHarness(t)
	if !strings.Contains(h2.do("GET", "/api/ledger/verify", nil).Body.String(), `"last_backup_at":null`) {
		t.Fatal("last_backup_at should be null")
	}
	// head file is 0600
	fi, err := os.Stat(h.head.Path())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("head file mode %v err %v", fi.Mode().Perm(), err)
	}
}

// TR-7: a broken ledger (here: tail deletion detected via DATA_DIR/head)
// makes every write 423 until acknowledged.
func TestBrokenLedgerReadOnlyUntilAcknowledged(t *testing.T) {
	h := newHarness(t)
	cr := h.create(validInput())
	// Pretend rows 2..5 existed and were deleted: recorded head is ahead of the chain.
	if err := h.head.Write(ledger.Head{ID: 5, Hash: strings.Repeat("d", 64)}); err != nil {
		t.Fatal(err)
	}
	v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil))
	if v.OK || v.BrokenAt == nil || *v.BrokenAt != 2 || v.Reason == nil || *v.Reason != ledger.ReasonHeadMismatch || v.Acknowledged {
		t.Fatalf("verify = %+v", v)
	}
	// failed verify must not overwrite the recorded head (evidence survives restarts)
	if hd, _ := h.head.Read(); hd.ID != 5 {
		t.Fatalf("head overwritten by failed verify: %+v", hd)
	}

	writes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/entries", validInput()},
		{"POST", "/api/entries/1/amend", validInput()},
		{"POST", "/api/entries/1/void", map[string]string{"reason": "x"}},
		{"POST", "/api/categories", map[string]string{"name": "New"}},
		{"PATCH", "/api/categories/1", map[string]string{"name": "New"}},
	}
	for _, w := range writes {
		wantError(t, h.do(w.method, w.path, w.body), 423, CodeLedgerBroken)
	}
	wantError(t, h.serve(multipartReq(t, h, "file", pngBytes(t))), 423, CodeLedgerBroken)
	// reads still work
	wantStatus(t, h.do("GET", "/api/entries", nil), 200)

	// acknowledge (bodyless POST: no Content-Type needed)
	rec := h.do("POST", "/api/ledger/acknowledge", nil)
	wantStatus(t, rec, 200)
	ack := decode[VerifyResult](t, rec)
	if ack.OK || !ack.Acknowledged {
		t.Fatalf("ack = %+v", ack)
	}
	if hd, _ := h.head.Read(); hd.ID != cr.ID || hd.Hash != cr.Hash {
		t.Fatalf("ack should record the current head, got %+v", hd)
	}
	h.create(validInput())
	// acknowledged tail deletion is not reported again
	if v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil)); !v.OK {
		t.Fatalf("verify after ack = %+v", v)
	}
}

func TestAcknowledgeWhenOK(t *testing.T) {
	h := newHarness(t)
	rec := h.do("POST", "/api/ledger/acknowledge", nil)
	wantStatus(t, rec, 200)
	v := decode[VerifyResult](t, rec)
	if !v.OK || v.HeadID != 0 || v.HeadHash != ledger.GenesisHash || v.Acknowledged {
		t.Fatalf("ack on ok ledger = %+v", v)
	}
}

func TestVerifyRecordedStartup(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	if err := os.Remove(h.head.Path()); err != nil {
		t.Fatal(err)
	}
	res, err := VerifyRecorded(t.Context(), h.svc, h.head, h.srv.log)
	if err != nil || !res.OK {
		t.Fatalf("first-run verify: %+v %v", res, err)
	}
	if !strings.Contains(h.logs.String(), "no recorded ledger head") || !strings.Contains(h.logs.String(), `"head":"1:`) {
		t.Fatalf("expected head log and first-run warning, logs: %s", h.logs.String())
	}
	if hd, _ := h.head.Read(); hd == nil || hd.ID != 1 {
		t.Fatal("head not recorded after ok verify")
	}
	// corrupt head file: verify still runs, warns, then rewrites it
	if err := os.WriteFile(h.head.Path(), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, err := VerifyRecorded(t.Context(), h.svc, h.head, h.srv.log); err != nil || !res.OK {
		t.Fatalf("verify with corrupt head: %+v %v", res, err)
	}
	if hd, err := h.head.Read(); err != nil || hd.ID != 1 {
		t.Fatalf("head not repaired: %+v %v", hd, err)
	}
}

func TestSecurityHeadersAndStatic(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name, method, path string
		authed             bool
		status             int
		bodyHas            string
		cache              string
	}{
		{"root", "GET", "/", false, 200, "kira-index", "no-cache"},
		{"index.html", "GET", "/index.html", false, 200, "kira-index", "no-cache"},
		{"spa route", "GET", "/entries", false, 200, "kira-index", "no-cache"},
		{"spa nested", "GET", "/entries/12", false, 200, "kira-index", "no-cache"},
		{"spa login", "GET", "/login", false, 200, "kira-index", "no-cache"},
		{"asset", "GET", "/assets/app-abc.js", false, 200, "console.log", "public, max-age=31536000, immutable"},
		{"favicon", "GET", "/favicon.svg", false, 200, "<svg", "no-cache"},
		{"missing asset", "GET", "/assets/nope.js", false, 404, "", ""},
		{"missing file", "GET", "/robots.txt", false, 404, "", ""},
		{"traversal is cleaned, never leaves dist", "GET", "/../../etc/passwd", false, 200, "kira-index", "no-cache"},
		{"traversal to file", "GET", "/assets/../../go.mod", false, 404, "", ""},
		{"post to spa", "POST", "/entries", false, 405, "", ""},
		{"unknown api authed", "GET", "/api/unknown", true, 404, `"code":"not_found"`, ""},
		{"unknown auth", "GET", "/auth/unknown", false, 404, `"code":"not_found"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := h.req(tt.method, tt.path, nil)
			if tt.authed {
				h.authed(r)
			}
			rec := h.serve(r)
			wantStatus(t, rec, tt.status)
			if tt.bodyHas != "" && !strings.Contains(rec.Body.String(), tt.bodyHas) {
				t.Fatalf("body %q missing %q", rec.Body.String(), tt.bodyHas)
			}
			if tt.cache != "" && rec.Header().Get("Cache-Control") != tt.cache {
				t.Fatalf("cache-control %q, want %q", rec.Header().Get("Cache-Control"), tt.cache)
			}
			hd := rec.Header()
			if hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Referrer-Policy") != "same-origin" || hd.Get("X-Frame-Options") != "DENY" {
				t.Fatalf("security headers missing: %v", hd)
			}
			if !strings.HasPrefix(tt.path, "/api/receipts") && hd.Get("Content-Security-Policy") != ContentSecurityPolicy {
				t.Fatalf("csp = %q", hd.Get("Content-Security-Policy"))
			}
		})
	}
}

func TestStaticNotBuilt(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Static = nil })
	wantStatus(t, h.serve(h.req("GET", "/", nil)), http.StatusServiceUnavailable)
}

func TestLogoutThroughRouter(t *testing.T) {
	h := newHarness(t)
	rec := h.serve(h.authed(h.req("POST", "/auth/logout", nil)))
	wantStatus(t, rec, http.StatusNoContent)
	wantError(t, h.do("GET", "/api/me", nil), 401, CodeUnauthorized)
}

func TestRateLimitAuth(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 10; i++ {
		rec := h.serve(h.req("POST", "/auth/logout", nil))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d rate limited too early", i+1)
		}
	}
	rec := h.serve(h.req("POST", "/auth/logout", nil))
	wantError(t, rec, http.StatusTooManyRequests, CodeRateLimited)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// login endpoint shares the limit
	wantError(t, h.serve(h.req("GET", "/auth/google/login", nil)), 429, CodeRateLimited)
	// another IP is unaffected; /api is not rate limited
	r := h.req("POST", "/auth/logout", nil)
	r.RemoteAddr = "198.51.100.7:1234"
	if h.serve(r).Code == http.StatusTooManyRequests {
		t.Fatal("other IP limited")
	}
	for i := 0; i < 15; i++ {
		wantStatus(t, h.do("GET", "/api/me", nil), 200)
	}
	// spoofed X-Forwarded-For from a remote peer does not reset the bucket
	r = h.req("POST", "/auth/logout", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	wantError(t, h.serve(r), 429, CodeRateLimited)
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		remote, xff, want string
	}{
		{"192.0.2.1:1234", "", "192.0.2.1"},
		{"192.0.2.1:1234", "203.0.113.5", "192.0.2.1"},           // remote peer: XFF ignored
		{"127.0.0.1:5000", "203.0.113.5", "203.0.113.5"},         // local proxy: XFF used
		{"[::1]:5000", "10.0.0.1, 100.64.0.7", "100.64.0.7"},     // right-most entry
		{"127.0.0.1:5000", "garbage, 100.64.0.8 ", "100.64.0.8"}, // whitespace trimmed
		{"127.0.0.1:5000", "not-an-ip", "127.0.0.1"},             // nothing valid
		{"127.0.0.1:5000", "", "127.0.0.1"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		if tt.xff != "" {
			r.Header.Set("X-Forwarded-For", tt.xff)
		}
		if got := clientIP(r); got != tt.want {
			t.Errorf("clientIP(%s, %q) = %s, want %s", tt.remote, tt.xff, got, tt.want)
		}
	}
}

func TestRateLimiterRefills(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(10, func() time.Time { return now })
	for i := 0; i < 10; i++ {
		if !rl.allow("a") {
			t.Fatalf("denied at %d", i)
		}
	}
	if rl.allow("a") {
		t.Fatal("11th allowed")
	}
	now = now.Add(6 * time.Second)
	if !rl.allow("a") {
		t.Fatal("token not refilled after 6s")
	}
	if rl.allow("a") {
		t.Fatal("only one token should refill")
	}
}

func TestDTOResponsesUseNulls(t *testing.T) {
	h := newHarness(t)
	in := validInput()
	in["note"] = nil
	h.create(in)
	body := h.do("GET", "/api/entries/1", nil).Body.String()
	for _, want := range []string{`"note":null`, `"receipt_sha256":null`, `"void_reason":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing in %s", want, body)
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	if len(m) != 18 {
		t.Fatalf("entry has %d fields, want 18: %s", len(m), body)
	}
}
