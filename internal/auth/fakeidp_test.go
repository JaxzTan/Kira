package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"

	"github.com/JaxzTan/kira/internal/auth/memstore"
)

const (
	testClientID     = "kira-client"
	testClientSecret = "client-secret-DO-NOT-LOG"
	testEmail        = "owner@example.com"
	testSub          = "owner-sub-1"
	testKID          = "k1"
)

var (
	keysOnce          sync.Once
	goodKey, otherKey *rsa.PrivateKey
)

func keys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keysOnce.Do(func() {
		var err error
		if goodKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if otherKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return goodKey, otherKey
}

type issuedCode struct {
	idToken   string
	challenge string
}

// fakeIdP is an httptest OIDC provider: discovery, JWKS and a token endpoint
// that enforces client auth and PKCE (S256).
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]issuedCode
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	k, _ := keys(t)
	f := &fakeIdP{t: t, key: k, codes: map[string]issuedCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &f.key.PublicKey, KeyID: testKID, Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", f.handleToken)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != testClientID || secret != testClientSecret {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
		return
	}
	f.mu.Lock()
	ic, found := f.codes[r.PostForm.Get("code")]
	delete(f.codes, r.PostForm.Get("code"))
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || r.PostForm.Get("grant_type") != "authorization_code" ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != ic.challenge {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at-DO-NOT-LOG", "token_type": "Bearer", "expires_in": 3600,
		"id_token": ic.idToken,
	})
}

func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256,
		Key: jose.JSONWebKey{Key: key, KeyID: testKID}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	obj, err := s.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	out, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *fakeIdP) issue(code, idToken, challenge string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code] = issuedCode{idToken: idToken, challenge: challenge}
}

// ---- harness ----

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

type harness struct {
	t      *testing.T
	idp    *fakeIdP
	store  *memstore.Store
	clock  *clock
	auth   *Auth
	router chi.Router
	logs   *bytes.Buffer
}

func newHarness(t *testing.T, mut func(*Config)) *harness {
	t.Helper()
	h := &harness{t: t, idp: newFakeIdP(t), store: memstore.New(),
		clock: &clock{t: time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}}
	h.rebuild(mut)
	return h
}

// rebuild constructs a fresh Auth over the same store (simulates a restart
// with changed config).
func (h *harness) rebuild(mut func(*Config)) {
	h.t.Helper()
	cfg := Config{
		ClientID: testClientID, ClientSecret: testClientSecret,
		IssuerURL: h.idp.srv.URL, RedirectURL: "http://kira.test/auth/google/callback",
		AllowedEmail: testEmail, SessionSecret: bytes.Repeat([]byte("s"), 32),
		Now:    h.clock.Now,
		Logger: slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if mut != nil {
		mut(&cfg)
	}
	a, err := New(cfg, h.store)
	if err != nil {
		h.t.Fatal(err)
	}
	h.auth = a
	r := chi.NewRouter()
	a.Routes(r)
	r.With(a.Middleware).Get("/api/me", func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r.Context())
		if !ok {
			http.Error(w, "no user", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"email": u.Email, "name": u.Name, "picture": u.Picture})
	})
	h.router = r
}

func (h *harness) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

type loginStart struct {
	state, nonce, challenge string
	cookie                  *http.Cookie
	location                *url.URL
}

func (h *harness) startLogin() loginStart {
	h.t.Helper()
	rec := h.do(httptest.NewRequest("GET", "/auth/google/login", nil))
	if rec.Code != http.StatusFound {
		h.t.Fatalf("login: status %d", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	ls := loginStart{location: loc, state: loc.Query().Get("state"),
		nonce: loc.Query().Get("nonce"), challenge: loc.Query().Get("code_challenge"),
		cookie: findCookie(rec, OAuthCookie)}
	if ls.cookie == nil {
		h.t.Fatal("login: no kira_oauth cookie")
	}
	return ls
}

func (h *harness) defaultClaims(ls loginStart) map[string]any {
	now := h.clock.Now()
	return map[string]any{
		"iss": h.idp.srv.URL, "aud": testClientID, "sub": testSub,
		"email": testEmail, "email_verified": true, "nonce": ls.nonce,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "name": "Owner",
	}
}

func (h *harness) callback(state, code string, c *http.Cookie) *httptest.ResponseRecorder {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	if code != "" {
		q.Set("code", code)
	}
	req := httptest.NewRequest("GET", "/auth/google/callback?"+q.Encode(), nil)
	if c != nil {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return h.do(req)
}

// login runs a full successful-shape flow with the given claims mutation
// and returns the callback response.
func (h *harness) login(mut func(claims map[string]any)) *httptest.ResponseRecorder {
	h.t.Helper()
	ls := h.startLogin()
	claims := h.defaultClaims(ls)
	if mut != nil {
		mut(claims)
	}
	h.idp.issue("code-1", sign(h.t, h.idp.key, claims), ls.challenge)
	return h.callback(ls.state, "code-1", ls.cookie)
}

func (h *harness) api(token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/me", nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: h.auth.SessionCookieName(), Value: token})
	}
	return h.do(req)
}

func (h *harness) lastResult() string {
	rows := h.store.AuthLog()
	if len(rows) == 0 {
		return ""
	}
	return rows[len(rows)-1].Result
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func newMem() *memstore.Store { return memstore.New() }
