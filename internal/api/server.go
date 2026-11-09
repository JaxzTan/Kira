package api

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/JaxzTan/kira/internal/auth"
	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
	"github.com/JaxzTan/kira/internal/store"
)

// ContentSecurityPolicy is sent on every response except receipts. The built
// app (web/dist) needs only same-origin scripts/styles/fonts, inline style
// attributes (React), and blob:/data: images for receipt previews.
const ContentSecurityPolicy = "default-src 'self'; img-src 'self' blob: data:; style-src 'self' 'unsafe-inline'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// receiptCSP is used for GET /api/receipts/:sha. Opening a PDF receipt in a
// tab must work in Chrome's built-in viewer, which object-src 'none' can
// block; nothing in a receipt may run scripts or be framed.
const receiptCSP = "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; object-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// Body limits (docs/api-contract.md).
const (
	maxJSONBody    = MaxJSONBodyBytes
	maxReceiptBody = MaxReceiptBytes + 1<<20 // 10 MB file + multipart overhead
)

// Options wires a Server.
type Options struct {
	Auth     *auth.Auth
	Store    *store.Store
	Ledger   *ledger.Service
	Receipts *receipts.Store
	Head     *HeadFile
	Static   fs.FS // web/dist contents (index.html at the root)
	Logger   *slog.Logger
	Now      func() time.Time
	// BackupDirs are Litestream local file replica locations; the newest
	// file mtime inside the first existing one is last_backup_at (null if
	// none exist, e.g. remote-only replication).
	BackupDirs []string
	// AuthRatePerMinute limits /auth/* per client IP (default 10).
	AuthRatePerMinute int
}

// Server is Kira's HTTP layer.
type Server struct {
	auth     *auth.Auth
	store    *store.Store
	ledger   *ledger.Service
	receipts *receipts.Store
	head     *HeadFile
	static   fs.FS
	log      *slog.Logger
	now      func() time.Time
	backups  []string
	limiter  *rateLimiter
}

// New validates opts and returns a Server.
func New(o Options) (*Server, error) {
	if o.Auth == nil || o.Store == nil || o.Ledger == nil || o.Receipts == nil || o.Head == nil {
		return nil, errors.New("api: Auth, Store, Ledger, Receipts and Head are required")
	}
	s := &Server{auth: o.Auth, store: o.Store, ledger: o.Ledger, receipts: o.Receipts, head: o.Head,
		static: o.Static, log: o.Logger, now: o.Now, backups: o.BackupDirs}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s.log = s.log.With("component", "api")
	if s.now == nil {
		s.now = time.Now
	}
	if s.static == nil {
		s.static = emptyFS{}
	}
	rpm := o.AuthRatePerMinute
	if rpm <= 0 {
		rpm = 10
	}
	s.limiter = newRateLimiter(rpm, s.now)
	return s, nil
}

// Handler returns the full router.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, s.accessLog, securityHeaders)

	// /auth/*: auth package routes behind the per-IP rate limit. The
	// catch-all makes unknown /auth paths (including /auth/test-login in
	// normal builds) a 404 instead of the SPA.
	r.Group(func(g chi.Router) {
		g.Use(s.limiter.middleware)
		s.auth.Routes(g)
		g.HandleFunc("/auth/*", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, CodeNotFound, "not found", nil)
		})
	})

	r.Mount("/api", s.apiRouter())
	r.Handle("/*", spaHandler(s.static))
	return r
}

func (s *Server) apiRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(s.auth.Middleware, s.requestBody, s.readOnlyGuard)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, CodeNotFound, "not found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, CodeBadRequest, "method not allowed", nil)
	})

	r.Get("/me", s.handleMe)

	r.Get("/entries", s.handleListEntries)
	r.Post("/entries", s.handleCreateEntry)
	r.Get("/entries/{id}", s.handleGetEntry)
	r.Get("/entries/{id}/history", s.handleHistory)
	r.Post("/entries/{id}/amend", s.handleAmend)
	r.Post("/entries/{id}/void", s.handleVoid)

	r.Post("/receipts", s.handleUploadReceipt)
	r.Get("/receipts/{sha}", s.handleGetReceipt)

	r.Get("/categories", s.handleListCategories)
	r.Post("/categories", s.handleCreateCategory)
	r.Patch("/categories/{id}", s.handlePatchCategory)

	r.Get("/summary", s.handleSummary)
	r.Get("/export", s.handleExport)

	r.Get("/ledger/verify", s.handleVerify)
	r.Post("/ledger/acknowledge", s.handleAcknowledge)
	return r
}

// ---- middleware ----

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", ContentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// bodylessPOST are state-changing requests that carry no body, so they are
// exempt from the Content-Type rule (SameSite=Lax still protects them).
var bodylessPOST = map[string]bool{"/api/ledger/acknowledge": true}

// requestBody enforces body size limits and the CSRF Content-Type rule on
// POST/PUT/PATCH under /api: application/json everywhere except
// multipart/form-data for POST /api/receipts.
func (s *Server) requestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
			next.ServeHTTP(w, r)
			return
		}
		limit := int64(maxJSONBody)
		want := "application/json"
		if r.URL.Path == "/api/receipts" {
			limit, want = maxReceiptBody, "multipart/form-data"
		}
		if r.ContentLength > limit {
			writeError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "request body too large", nil)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		if !bodylessPOST[r.URL.Path] {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != want {
				writeError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "Content-Type must be "+want, nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// readOnlyGuard: while the ledger is broken and unacknowledged the whole app
// is read-only (TR-7), so every state-changing /api request except the
// acknowledgement is 423.
func (s *Server) readOnlyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			r.URL.Path != "/api/ledger/acknowledge" && s.ledger.WritesBlocked() {
			writeError(w, http.StatusLocked, CodeLedgerBroken, ledger.ErrLedgerBroken.Error(), nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// accessLog logs method, path (no query string), status, size and duration.
// Never bodies, cookies or headers.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		lvl := slog.LevelInfo
		if sw.status >= 500 {
			lvl = slog.LevelError
		}
		s.log.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "ms", time.Since(start).Milliseconds(), "ip", clientIP(r))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, CodeInternal, "internal error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// lastBackup returns the newest file mtime in the first existing backup dir.
func (s *Server) lastBackup() *time.Time {
	for _, dir := range s.backups {
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			continue
		}
		newest := fi.ModTime()
		n := 0
		_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if n++; n > 10000 {
				return filepath.SkipAll
			}
			if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			return nil
		})
		return &newest
	}
	return nil
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
