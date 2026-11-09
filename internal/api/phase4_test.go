package api

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/ledger"
)

// ---- CSRF (Phase 4) ----

func TestCSRFSameOrigin(t *testing.T) {
	const origin = "http://localhost:5173"
	h := newHarness(t, func(o *Options) { o.PublicOrigin = origin + "/" })
	h.create(validInput())

	type hdrs map[string]string
	variants := []struct {
		name    string
		headers hdrs // replaces the harness default Sec-Fetch-Site
		ok      bool
	}{
		{"sec-fetch-site same-origin", hdrs{"Sec-Fetch-Site": "same-origin"}, true},
		{"origin matches PUBLIC_URL", hdrs{"Origin": origin}, true},
		{"cross-site", hdrs{"Sec-Fetch-Site": "cross-site"}, false},
		{"same-site (other localhost port)", hdrs{"Sec-Fetch-Site": "same-site"}, false},
		{"none (typed URL / bookmark)", hdrs{"Sec-Fetch-Site": "none"}, false},
		{"cross-site wins over matching origin", hdrs{"Sec-Fetch-Site": "cross-site", "Origin": origin}, false},
		{"cross-site origin", hdrs{"Origin": "https://evil.example"}, false},
		{"origin null", hdrs{"Origin": "null"}, false},
		{"missing both", hdrs{}, false},
	}
	type target struct {
		name string
		mk   func() *http.Request
	}
	targets := []target{
		{"POST /api/entries", func() *http.Request { return h.authed(h.req("POST", "/api/entries", validInput())) }},
		{"POST amend", func() *http.Request { return h.authed(h.req("POST", "/api/entries/1/amend", validInput())) }},
		{"POST void", func() *http.Request {
			return h.authed(h.req("POST", "/api/entries/999/void", map[string]string{"reason": "x"}))
		}},
		{"POST categories", func() *http.Request {
			return h.authed(h.req("POST", "/api/categories", map[string]string{"name": fmt.Sprint("C", time.Now().UnixNano())}))
		}},
		{"PATCH category", func() *http.Request {
			return h.authed(h.req("PATCH", "/api/categories/10", map[string]any{"archived": false}))
		}},
		{"POST acknowledge (bodyless)", func() *http.Request { return h.authed(h.req("POST", "/api/ledger/acknowledge", nil)) }},
		{"POST receipts (multipart)", func() *http.Request { return multipartReq(t, h, "file", pngBytes(t)) }},
	}
	for _, tg := range targets {
		for _, v := range variants {
			t.Run(tg.name+"/"+v.name, func(t *testing.T) {
				r := tg.mk()
				r.Header.Del("Sec-Fetch-Site")
				for k, val := range v.headers {
					r.Header.Set(k, val)
				}
				before := h.head()
				rec := h.serve(r)
				if v.ok {
					if rec.Code == http.StatusForbidden {
						t.Fatalf("same-origin request rejected: %s", rec.Body.String())
					}
					return
				}
				e := wantError(t, rec, http.StatusForbidden, CodeForbidden)
				if e.Error != "cross-site request" {
					t.Fatalf("error = %q", e.Error)
				}
				if after := h.head(); after != before {
					t.Fatal("rejected request still appended")
				}
			})
		}
	}
	// Reads need no CSRF headers.
	r := h.authed(h.req("GET", "/api/entries", nil))
	r.Header.Del("Sec-Fetch-Site")
	wantStatus(t, h.serve(r), 200)
	// Without a session the answer is still 401 (auth runs first).
	r = h.req("POST", "/api/entries", validInput())
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	wantError(t, h.serve(r), 401, CodeUnauthorized)
	// Logout goes through the auth package's own check.
	r = h.authed(h.req("POST", "/auth/logout", nil))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if rec := h.serve(r); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site logout = %d", rec.Code)
	}
}

func TestCSRFNoPublicOriginNeedsSecFetchSite(t *testing.T) {
	h := newHarness(t) // PublicOrigin "" disables the Origin fallback
	r := h.authed(h.req("POST", "/api/entries", validInput()))
	r.Header.Del("Sec-Fetch-Site")
	r.Header.Set("Origin", "http://localhost")
	wantError(t, h.serve(r), http.StatusForbidden, CodeForbidden)
}

// ---- receipts tamper (P1-1) ----

func TestReceiptTampered(t *testing.T) {
	h := newHarness(t)
	rc := decode[Receipt](t, h.serve(multipartReq(t, h, "file", pngBytes(t))))
	in := validInput()
	in["receipt_sha256"] = rc.SHA256
	h.create(in)

	_, path, err := h.rs.Stat(rc.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	swapped := append(pngBytes(t), 0) // still a PNG prefix, different hash
	if err := os.WriteFile(path, swapped, 0o600); err != nil {
		t.Fatal(err)
	}

	wantError(t, h.do("GET", "/api/receipts/"+rc.SHA256, nil), http.StatusConflict, CodeConflict)

	v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil))
	if v.OK || v.Reason == nil || *v.Reason != ledger.ReasonReceiptMismatch || v.BrokenAt == nil || *v.BrokenAt != 1 {
		t.Fatalf("verify = %+v", v)
	}

	day := today()
	rec := h.do("GET", "/api/export?format=zip&from="+day+"&to="+day, nil)
	wantStatus(t, rec, 200)
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var list string
	for _, f := range zr.File {
		if f.Name == "TAMPERED_RECEIPTS.txt" {
			rd, _ := f.Open()
			b, _ := io.ReadAll(rd)
			list = string(b)
		}
	}
	if !strings.Contains(list, rc.SHA256) {
		t.Fatalf("TAMPERED_RECEIPTS.txt = %q", list)
	}
}

// ---- head file ----

func TestHeadFileStates(t *testing.T) {
	legacy := func(h *harness) []byte { return []byte(ledger.FormatHeadID(h.head()) + "\n") }
	tests := []struct {
		name    string
		content func(h *harness) []byte // nil = delete the file
		reason  string
	}{
		{"missing", nil, ledger.ReasonHeadMissing},
		{"garbage", func(*harness) []byte { return []byte("garbage") }, ledger.ReasonHeadInvalid},
		{"legacy id:hash without mac", legacy, ledger.ReasonHeadInvalid},
		{"bad mac", func(h *harness) []byte {
			return []byte(ledger.FormatHeadID(h.head()) + ":" + strings.Repeat("0", 64) + "\n")
		}, ledger.ReasonHeadInvalid},
		{"mac from another key", func(h *harness) []byte {
			return []byte(ledger.FormatHeadFile(bytes.Repeat([]byte{1}, 32), h.head()))
		}, ledger.ReasonHeadInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			cr := h.create(validInput())
			var want []byte
			if tt.content == nil {
				if err := os.Remove(h.integ.HeadPath()); err != nil {
					t.Fatal(err)
				}
			} else {
				want = tt.content(h)
				if err := os.WriteFile(h.integ.HeadPath(), want, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil))
			if v.OK || v.Reason == nil || *v.Reason != tt.reason || *v.BrokenAt != cr.ID || v.Acknowledged {
				t.Fatalf("verify = %+v", v)
			}
			wantError(t, h.do("POST", "/api/entries", validInput()), 423, CodeLedgerBroken)
			// fail closed: verify did not touch the file
			got, err := os.ReadFile(h.integ.HeadPath())
			if tt.content == nil && !os.IsNotExist(err) {
				t.Fatalf("missing head file was recreated: %q %v", got, err)
			}
			if tt.content != nil && !bytes.Equal(got, want) {
				t.Fatalf("invalid head file overwritten: %q", got)
			}

			// acknowledge → current head recorded with a valid MAC
			ack := decode[VerifyResult](t, h.do("POST", "/api/ledger/acknowledge", nil))
			if !ack.Acknowledged {
				t.Fatalf("ack = %+v", ack)
			}
			if hd := h.head(); hd.ID != cr.ID || hd.Hash != cr.Hash {
				t.Fatalf("head after ack = %+v", hd)
			}
			assertAckFile(t, h)
			h.create(validInput())
			// the head is trusted again: next verify is clean, ack cleared and file removed
			v = decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil))
			if !v.OK {
				t.Fatalf("verify after ack = %+v", v)
			}
			if _, err := os.Stat(h.integ.AckPath()); !os.IsNotExist(err) {
				t.Fatalf("ack file not removed after clean verify: %v", err)
			}
		})
	}
}

func TestAppendHookDoesNotHealHeadFile(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	if err := os.Remove(h.integ.HeadPath()); err != nil {
		t.Fatal(err)
	}
	h.create(validInput()) // writes still allowed until the next verify...
	if _, err := os.Stat(h.integ.HeadPath()); !os.IsNotExist(err) {
		t.Fatal("append hook recreated a deleted head file")
	}
	v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil)) // ...which catches it
	if v.OK || *v.Reason != ledger.ReasonHeadMissing {
		t.Fatalf("verify = %+v", v)
	}
	if !strings.Contains(h.logs.String(), "not advancing it") {
		t.Fatal("hook did not warn")
	}
}

func TestHeadFileUnchangedIsNotRewritten(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	fi1, err := os.Stat(h.integ.HeadPath())
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, h.do("GET", "/api/ledger/verify", nil), 200)
	fi2, _ := os.Stat(h.integ.HeadPath())
	if !os.SameFile(fi1, fi2) {
		t.Fatal("unchanged head was rewritten")
	}
	// An older (smaller) recorded head that matches the chain is advanced by verify.
	first := h.head()
	h.create(validInput())
	h.writeHead(first)
	v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil))
	if !v.OK || h.head().ID != 2 {
		t.Fatalf("verify = %+v head = %+v", v, h.head())
	}
}

func TestHeadFollowsConcurrentAppends(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := h.do("POST", "/api/entries", validInput())
			if rec.Code != 201 {
				t.Errorf("create = %d %s", rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	if hd := h.head(); hd.ID != 16 {
		t.Fatalf("head = %+v", hd)
	}
	if v := decode[VerifyResult](t, h.do("GET", "/api/ledger/verify", nil)); !v.OK {
		t.Fatalf("verify = %+v", v)
	}
}

// ---- acknowledgement persistence ----

// restart builds a fresh ledger.Service + Integrity on the same DATA_DIR,
// the way `kira serve` starts: LoadAck before the first verify.
func (h *harness) restart(t *testing.T) (*ledger.Service, *Integrity, ledger.VerifyResult) {
	t.Helper()
	svc, err := ledger.New(ledger.Config{Store: h.st, Key: h.key, Receipts: h.rs})
	if err != nil {
		t.Fatal(err)
	}
	in := NewIntegrity(svc, h.key, h.integ.HeadPath(), h.integ.AckPath(), h.log)
	in.LoadAck()
	res, err := in.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return svc, in, res
}

func TestAcknowledgeSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	h.writeHead(ledger.Head{ID: 9, Hash: strings.Repeat("d", 64)}) // tail deletion
	wantStatus(t, h.do("GET", "/api/ledger/verify", nil), 200)
	if _, _, res := h.restart(t); res.Acknowledged {
		t.Fatal("acknowledged before acknowledging")
	}
	wantStatus(t, h.do("POST", "/api/ledger/acknowledge", nil), 200)

	svc, _, res := h.restart(t)
	if res.OK || !res.Acknowledged || svc.WritesBlocked() {
		t.Fatalf("after restart: %+v blocked=%v", res, svc.WritesBlocked())
	}

	// a forged ack file (no valid MAC) is ignored: writes stay blocked
	if err := os.WriteFile(h.integ.AckPath(), []byte(strings.Repeat("0", 64)+"\n"+`{"v":1,"keys":["head missing"]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, _, res = h.restart(t)
	if res.Acknowledged || !svc.WritesBlocked() {
		t.Fatalf("forged ack accepted: %+v", res)
	}
	if !strings.Contains(h.logs.String(), "not authentic") {
		t.Fatal("forged ack not logged")
	}
}

func TestVerifyReadOnlyWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.create(validInput())
	if err := os.Remove(h.integ.HeadPath()); err != nil {
		t.Fatal(err)
	}
	res, err := h.integ.VerifyReadOnly(t.Context())
	if err != nil || res.OK || *res.Reason != ledger.ReasonHeadMissing {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(h.integ.HeadPath()); !os.IsNotExist(err) {
		t.Fatal("read-only verify wrote the head file")
	}
}

// ---- categories / list ----

func TestCategoryPatchIsAtomic(t *testing.T) {
	h := newHarness(t)
	wantStatus(t, h.do("POST", "/api/categories", map[string]any{"name": "Courses"}), 201)
	// rename to a duplicate + archive: neither applies
	wantError(t, h.do("PATCH", "/api/categories/15", map[string]any{"name": "Office", "archived": true}), 409, CodeConflict)
	l := decode[CategoryList](t, h.do("GET", "/api/categories", nil))
	if len(l.Categories) != 15 {
		t.Fatalf("category archived by a failed PATCH: %d", len(l.Categories))
	}
	rec := h.do("PATCH", "/api/categories/15", map[string]any{"name": "Training", "archived": true})
	wantStatus(t, rec, 200)
	if c := decode[Category](t, rec); c.Name != "Training" || c.ArchivedAt == nil {
		t.Fatalf("patch both = %+v", c)
	}
}

func TestListFromAfterTo(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do("GET", "/api/entries?from=2026-02-01&to=2026-01-01", nil), 400, CodeBadRequest)
	wantStatus(t, h.do("GET", "/api/entries?from=2026-01-01&to=2026-01-01", nil), 200)
}

// ---- rate limiter ----

func TestClientIPWithoutTrustProxy(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := clientIP(r, false); got != "127.0.0.1" {
		t.Fatalf("XFF trusted without TRUST_PROXY: %s", got)
	}
}

func TestRateLimitTrustProxy(t *testing.T) {
	for _, trust := range []bool{false, true} {
		t.Run(fmt.Sprint("trust=", trust), func(t *testing.T) {
			h := newHarness(t, func(o *Options) { o.TrustProxy = trust })
			h.remote = "127.0.0.1:4000"
			send := func(xff string) int {
				r := h.req("GET", "/auth/google/login", nil)
				r.Header.Set("X-Forwarded-For", xff)
				return h.serve(r).Code
			}
			for i := 0; i < 10; i++ {
				send("100.64.0.1")
			}
			if c := send("100.64.0.1"); c != http.StatusTooManyRequests {
				t.Fatalf("11th = %d", c)
			}
			c := send("100.64.0.2")
			if trust && c == http.StatusTooManyRequests {
				t.Fatal("distinct forwarded client shares a bucket with TRUST_PROXY=1")
			}
			if !trust && c != http.StatusTooManyRequests {
				t.Fatal("X-Forwarded-For honoured without TRUST_PROXY")
			}
		})
	}
}

func TestRateLimiterCap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(10, func() time.Time { return now })
	rl.max = 3
	for _, ip := range []string{"a", "b", "c"} {
		rl.allow(ip)
		now = now.Add(time.Second)
	}
	for i := 0; i < 10; i++ {
		rl.allow("b") // b and c are newer than a
	}
	rl.allow("d")
	if len(rl.clients) != 3 {
		t.Fatalf("clients = %d", len(rl.clients))
	}
	if _, ok := rl.clients["a"]; ok {
		t.Fatal("oldest client not evicted")
	}
	if rl.allow("b") {
		t.Fatal("recent client's bucket was reset by eviction")
	}
	if rl.max != 3 || maxRateClients != 10_000 {
		t.Fatal("cap")
	}
}
