package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/JaxzTan/kira/internal/auth"
	"github.com/JaxzTan/kira/internal/auth/memstore"
	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
	"github.com/JaxzTan/kira/internal/store"
)

const (
	testEmail = "owner@example.com"
	testSub   = "owner-sub-123"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type harness struct {
	t      *testing.T
	dir    string
	st     *store.Store
	svc    *ledger.Service
	rs     *receipts.Store
	mem    *memstore.Store
	head   *HeadFile
	srv    *Server
	h      http.Handler
	token  string
	logs   *syncBuf
	remote string
}

var testStatic = fstest.MapFS{
	"index.html":        {Data: []byte("<!doctype html><title>Kira</title><div id=root>kira-index</div>")},
	"favicon.svg":       {Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
	"assets/app-abc.js": {Data: []byte("console.log('kira')")},
}

func newHarness(t *testing.T, mutate ...func(*Options)) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "kira.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rs, err := receipts.New(filepath.Join(dir, "receipts"))
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	svc, err := ledger.New(ledger.Config{Store: st, Key: key, Receipts: rs})
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuf{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mem := memstore.New()
	a, err := auth.New(auth.Config{
		ClientID: "test.apps.googleusercontent.com", ClientSecret: "client-secret-value",
		IssuerURL: "http://127.0.0.1:1", RedirectURL: "http://localhost/auth/google/callback",
		AllowedEmail: testEmail, SessionSecret: bytes.Repeat([]byte{9}, 32), Logger: log,
	}, mem)
	if err != nil {
		t.Fatal(err)
	}
	head := NewHeadFile(filepath.Join(dir, "head"))
	opts := Options{Auth: a, Store: st, Ledger: svc, Receipts: rs, Head: head, Static: testStatic, Logger: log}
	for _, m := range mutate {
		m(&opts)
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	// A real session in the memstore so requests pass the real auth.Middleware.
	var b [32]byte
	_, _ = rand.Read(b[:])
	token := base64.RawURLEncoding.EncodeToString(b[:])
	sum := sha256.Sum256([]byte(token))
	now := time.Now()
	if err := mem.PinOwner(ctx, testSub, testEmail, now); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateSession(ctx, hex.EncodeToString(sum[:]), testEmail, testSub, "test", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, dir: dir, st: st, svc: svc, rs: rs, mem: mem, head: head, srv: srv,
		h: srv.Handler(), token: token, logs: logs, remote: "192.0.2.10:5555"}
}

// req builds a request; body may be nil, []byte, string, or any JSON value.
func (h *harness) req(method, path string, body any) *http.Request {
	var rd io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ct = bytes.NewReader(b), "application/json"
	case string:
		rd, ct = bytes.NewReader([]byte(b)), "application/json"
	default:
		js, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		rd, ct = bytes.NewReader(js), "application/json"
	}
	r := httptest.NewRequest(method, path, rd)
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	r.RemoteAddr = h.remote
	return r
}

func (h *harness) authed(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: h.token})
	return r
}

func (h *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, r)
	return rec
}

// do sends an authenticated request.
func (h *harness) do(method, path string, body any) *httptest.ResponseRecorder {
	return h.serve(h.authed(h.req(method, path, body)))
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %T: %v (body %s)", v, err, rec.Body.String())
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) ErrorResponse {
	t.Helper()
	wantStatus(t, rec, status)
	if ct := rec.Header().Get("Content-Type"); ct == "" || ct[:16] != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	e := decode[ErrorResponse](t, rec)
	if e.Code != code || e.Error == "" {
		t.Fatalf("error = %+v, want code %s", e, code)
	}
	if code != CodeValidation && e.Fields != nil {
		t.Fatalf("fields must be null for %s: %+v", code, e)
	}
	return e
}

func today() string { return time.Now().Format("2006-01-02") }

func validInput() map[string]any {
	return map[string]any{
		"occurred_on": today(), "amount_sen": 4590, "category_id": 1, "payee": "Cloudflare",
		"method": "card", "note": "R2 + domain", "receipt_sha256": nil, "confirm_date": false,
	}
}

func (h *harness) create(in map[string]any) AppendResult {
	h.t.Helper()
	rec := h.do("POST", "/api/entries", in)
	wantStatus(h.t, rec, http.StatusCreated)
	return decode[AppendResult](h.t, rec)
}
