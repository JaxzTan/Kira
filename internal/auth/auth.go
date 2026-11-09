// Package auth implements Kira's single-owner Google OIDC login (TR-1),
// allow-list (TR-2) and server-side sessions (TR-3).
//
// Name/picture decision: the sessions table has no profile columns, so
// UserFrom returns nil Name and Picture (rendered as null by /api/me).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
)

// Cookie names and lifetimes (docs/api-contract.md, Auth section).
const (
	SessionCookie = "kira_session"
	OAuthCookie   = "kira_oauth"

	SessionTTL    = 30 * 24 * time.Hour
	OAuthStateTTL = 10 * time.Minute

	// E2ETestSub is the sub used by POST /auth/test-login (e2e builds only).
	E2ETestSub = "e2e-test-sub"

	DefaultIssuerURL = "https://accounts.google.com"
	oauthCookiePath  = "/auth/google"
)

// auth_log.result values.
const (
	ResultOK               = "ok"
	ResultOwnerPinned      = "owner_pinned"
	ResultRejectEmail      = "reject_email"
	ResultRejectUnverified = "reject_unverified"
	ResultRejectSub        = "reject_sub"
	ResultRejectToken      = "reject_token"
	ResultRejectState      = "reject_state"
)

// Store is the persistence auth needs. kira-ledger implements it in
// internal/store; memstore provides an in-memory version for tests.
type Store interface {
	CreateSession(ctx context.Context, tokenSHA256, email, sub, userAgent string, createdAt, expiresAt time.Time) error
	GetSession(ctx context.Context, tokenSHA256 string) (email, sub string, expiresAt time.Time, found bool, err error)
	DeleteSession(ctx context.Context, tokenSHA256 string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
	OwnerSub(ctx context.Context) (sub string, found bool, err error)
	PinOwner(ctx context.Context, sub, email string, at time.Time) error // insert-once
	LogAuth(ctx context.Context, at time.Time, email, sub, result string) error
}

// Config is built by kira-api from the environment.
type Config struct {
	ClientID      string
	ClientSecret  string
	IssuerURL     string // default DefaultIssuerURL; tests point it at a fake issuer
	RedirectURL   string // PUBLIC_URL + "/auth/google/callback"
	AllowedEmail  string
	AllowedSub    string // optional; else the owner row (pinned on first login)
	SessionSecret []byte // signs the kira_oauth cookie; >= 32 bytes
	Secure        bool   // PUBLIC_URL is https
	Now           func() time.Time
	Logger        *slog.Logger
}

// User is the authenticated identity attached to the request context.
type User struct {
	Email   string
	Sub     string
	Name    *string // always nil (see package doc)
	Picture *string // always nil (see package doc)
}

// Auth holds the login flow, sessions and middleware.
type Auth struct {
	cfg   Config
	store Store
	log   *slog.Logger

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// New validates cfg. OIDC discovery is lazy (first login), so the server
// can start while the issuer is unreachable.
func New(cfg Config, store Store) (*Auth, error) {
	if store == nil {
		return nil, errors.New("auth: store is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("auth: ClientID is required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("auth: RedirectURL is required")
	}
	if strings.TrimSpace(cfg.AllowedEmail) == "" {
		return nil, errors.New("auth: AllowedEmail is required")
	}
	if len(cfg.SessionSecret) < 32 {
		return nil, errors.New("auth: SessionSecret must be at least 32 bytes")
	}
	if cfg.IssuerURL == "" {
		cfg.IssuerURL = DefaultIssuerURL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.AllowedSub = strings.TrimSpace(cfg.AllowedSub)
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Auth{cfg: cfg, store: store, log: log.With("component", "auth")}, nil
}

// Routes registers /auth/google/login, /auth/google/callback, /auth/logout
// and, only in -tags e2e builds, /auth/test-login.
func (a *Auth) Routes(r chi.Router) {
	r.Get("/auth/google/login", a.handleLogin)
	r.Get("/auth/google/callback", a.handleCallback)
	r.Post("/auth/logout", a.handleLogout)
	a.registerTestLogin(r)
}

func (a *Auth) now() time.Time { return a.cfg.Now().UTC() }

// oidcClient performs discovery once; a failure is retried on the next call.
func (a *Auth) oidcClient(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return a.oauth, a.verifier, nil
	}
	p, err := oidc.NewProvider(ctx, a.cfg.IssuerURL)
	if err != nil {
		return nil, nil, err
	}
	a.provider = p
	a.verifier = p.Verifier(&oidc.Config{ClientID: a.cfg.ClientID, Now: a.cfg.Now})
	a.oauth = &oauth2.Config{
		ClientID:     a.cfg.ClientID,
		ClientSecret: a.cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  a.cfg.RedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return a.oauth, a.verifier, nil
}

// pinnedSub returns ALLOWED_SUB if set, else the owner row.
func (a *Auth) pinnedSub(ctx context.Context) (string, bool, error) {
	if a.cfg.AllowedSub != "" {
		return a.cfg.AllowedSub, true, nil
	}
	return a.store.OwnerSub(ctx)
}

func (a *Auth) emailAllowed(email string) bool {
	return strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(a.cfg.AllowedEmail))
}

func (a *Auth) logAuth(ctx context.Context, email, sub, result string) {
	if err := a.store.LogAuth(ctx, a.now(), email, sub, result); err != nil {
		a.log.Error("auth_log write failed", "result", result, "err", err)
	}
	lvl := slog.LevelInfo
	if strings.HasPrefix(result, "reject_") {
		lvl = slog.LevelWarn
	}
	a.log.Log(ctx, lvl, "login", "result", result, "email", email, "sub", sub)
}

// newSession creates a session and returns the raw cookie token.
func (a *Auth) newSession(ctx context.Context, email, sub, userAgent string) (string, time.Time, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	now := a.now()
	exp := now.Add(SessionTTL)
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	if err := a.store.DeleteExpiredSessions(ctx, now); err != nil {
		a.log.Warn("delete expired sessions failed", "err", err)
	}
	if err := a.store.CreateSession(ctx, hashToken(token), email, sub, userAgent, now, exp); err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// hashToken returns the hex SHA-256 stored in the sessions table.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// parseToken rejects anything that is not 32 base64url bytes.
func parseToken(v string) (string, bool) {
	b, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil || len(b) != 32 {
		return "", false
	}
	return v, true
}

func randString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
