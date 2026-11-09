package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type ctxKey struct{}

// UserFrom returns the user the Middleware attached to ctx.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// WithUser returns ctx carrying u (for handler tests in other packages).
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// ---- login ----

func (a *Auth) handleLogin(w http.ResponseWriter, r *http.Request) {
	oc, _, err := a.oidcClient(r.Context())
	if err != nil {
		a.log.Error("oidc discovery failed", "err", err)
		writePage(w, http.StatusServiceUnavailable, "Sign-in is temporarily unavailable", "Kira could not reach Google. Try again in a moment.")
		return
	}
	state, err1 := randString(32)
	nonce, err2 := randString(32)
	if err1 != nil || err2 != nil {
		writePage(w, http.StatusInternalServerError, "Something went wrong", "Try again.")
		return
	}
	st := oauthState{State: state, Nonce: nonce, Verifier: oauth2.GenerateVerifier(),
		Expires: a.now().Add(OAuthStateTTL).Unix()}
	val, err := a.encodeOAuthState(st)
	if err != nil {
		writePage(w, http.StatusInternalServerError, "Something went wrong", "Try again.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: OAuthCookie, Value: val, Path: oauthCookiePath,
		MaxAge: int(OAuthStateTTL / time.Second), HttpOnly: true,
		Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode,
	})
	url := oc.AuthCodeURL(state,
		oauth2.S256ChallengeOption(st.Verifier),
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

// ---- callback ----

type idClaims struct {
	Email         string          `json:"email"`
	EmailVerified json.RawMessage `json:"email_verified"`
}

// verified accepts JSON true or the string "true" (some IdPs send strings).
func (c idClaims) verified() bool {
	s := strings.TrimSpace(string(c.EmailVerified))
	return s == "true" || s == `"true"`
}

func (a *Auth) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	// The state cookie is single-use: clear it whatever happens next.
	a.clearCookie(w, OAuthCookie, oauthCookiePath)

	var st oauthState
	c, err := r.Cookie(OAuthCookie)
	if err == nil {
		st, err = a.decodeOAuthState(c.Value)
	}
	q := r.URL.Query()
	if err != nil || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(st.State)) != 1 {
		a.rejectState("bad state or oauth cookie")
		writePage(w, http.StatusBadRequest, "Sign-in expired", "The sign-in request was invalid or expired. Please start again.")
		return
	}

	code := q.Get("code")
	if code == "" {
		a.rejectState("missing code")
		writePage(w, http.StatusForbidden, "Sign-in failed", "Google did not complete the sign-in.")
		return
	}
	oc, verifier, err := a.oidcClient(ctx)
	if err != nil {
		a.log.Error("oidc discovery failed", "err", err)
		writePage(w, http.StatusServiceUnavailable, "Sign-in is temporarily unavailable", "Kira could not reach Google. Try again in a moment.")
		return
	}
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) { // log the error code only, never the body
			err = errors.New("token endpoint error: " + re.ErrorCode)
		}
		// No token came back from Google, so nothing authenticated happened:
		// WARN only, no auth_log row (same reasoning as reject_state).
		a.log.Warn("code exchange failed", "result", ResultRejectState, "err", err)
		writePage(w, http.StatusForbidden, "Sign-in failed", "Google's sign-in response could not be verified. Please try again.")
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		a.rejectToken(w, r, "no id_token in token response", nil)
		return
	}
	idt, err := verifier.Verify(ctx, rawID) // sig, iss, aud, exp
	if err != nil {
		a.rejectToken(w, r, "id token verification failed", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		a.rejectToken(w, r, "nonce mismatch", nil)
		return
	}
	var cl idClaims
	if err := idt.Claims(&cl); err != nil {
		a.rejectToken(w, r, "bad id token claims", err)
		return
	}
	email, sub := strings.TrimSpace(cl.Email), idt.Subject

	if !cl.verified() {
		a.reject(w, r, email, sub, ResultRejectUnverified)
		return
	}
	if !a.emailAllowed(email) {
		a.reject(w, r, email, sub, ResultRejectEmail)
		return
	}
	result, err := a.checkOrPinSub(ctx, email, sub)
	if err != nil {
		a.log.Error("owner lookup failed", "err", err)
		writePage(w, http.StatusInternalServerError, "Something went wrong", "Try again.")
		return
	}
	if result == ResultRejectSub {
		a.reject(w, r, email, sub, ResultRejectSub)
		return
	}

	token, _, err := a.newSession(ctx, email, sub, r.UserAgent())
	if err != nil {
		a.log.Error("create session failed", "err", err)
		writePage(w, http.StatusInternalServerError, "Something went wrong", "Try again.")
		return
	}
	a.logAuth(ctx, email, sub, result)
	a.setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusFound)
}

// checkOrPinSub returns ResultOK, ResultOwnerPinned or ResultRejectSub.
func (a *Auth) checkOrPinSub(ctx context.Context, email, sub string) (string, error) {
	pinned, found, err := a.pinnedSub(ctx)
	if err != nil {
		return "", err
	}
	if found {
		if subtle.ConstantTimeCompare([]byte(pinned), []byte(sub)) == 1 && sub != "" {
			return ResultOK, nil
		}
		return ResultRejectSub, nil
	}
	if sub == "" {
		return ResultRejectSub, nil
	}
	if err := a.store.PinOwner(ctx, sub, email, a.now()); err != nil {
		// Lost an insert-once race? Re-read and compare.
		pinned, found, err2 := a.store.OwnerSub(ctx)
		if err2 != nil || !found {
			return "", err
		}
		if pinned == sub {
			return ResultOK, nil
		}
		return ResultRejectSub, nil
	}
	return ResultOwnerPinned, nil
}

// rejectState logs an unauthenticated callback failure via slog only.
func (a *Auth) rejectState(why string) {
	a.log.Warn("login", "result", ResultRejectState, "why", why)
}

func (a *Auth) rejectToken(w http.ResponseWriter, r *http.Request, why string, err error) {
	if err != nil {
		a.log.Warn(why, "err", err)
	} else {
		a.log.Warn(why)
	}
	a.logAuth(r.Context(), "", "", ResultRejectToken)
	writePage(w, http.StatusForbidden, "Sign-in failed", "Google's sign-in response could not be verified. Please try again.")
}

func (a *Auth) reject(w http.ResponseWriter, r *http.Request, email, sub, result string) {
	a.logAuth(r.Context(), email, sub, result)
	writePage(w, http.StatusForbidden, "Not allowed", "This Google account is not allowed into Kira.")
}

// ---- logout ----

func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !SameOrigin(r, a.cfg.PublicOrigin) {
		writeJSONError(w, http.StatusForbidden, "forbidden", "cross-site request")
		return
	}
	hash, ok := a.sessionHash(r)
	a.clearCookie(w, a.SessionCookieName(), "/")
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	_, _, _, found, err := a.store.GetSession(r.Context(), hash)
	if err != nil {
		a.log.Error("session lookup failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	if !found {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	if err := a.store.DeleteSession(r.Context(), hash); err != nil {
		a.log.Error("delete session failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- middleware ----

// Middleware guards /api/*: 401 without a valid unexpired session, 403 if
// the session's email/sub no longer pass the current allow-list.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hash, ok := a.sessionHash(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
			return
		}
		email, sub, exp, found, err := a.store.GetSession(ctx, hash)
		if err != nil {
			a.log.Error("session lookup failed", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if !found {
			a.clearCookie(w, a.SessionCookieName(), "/")
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "session expired")
			return
		}
		if !a.now().Before(exp) {
			if err := a.store.DeleteSession(ctx, hash); err != nil {
				a.log.Error("delete expired session failed", "err", err)
			}
			a.clearCookie(w, a.SessionCookieName(), "/")
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "session expired")
			return
		}
		pinned, pfound, err := a.pinnedSub(ctx)
		if err != nil {
			a.log.Error("owner lookup failed", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if !a.emailAllowed(email) || !pfound || sub == "" ||
			subtle.ConstantTimeCompare([]byte(pinned), []byte(sub)) != 1 {
			a.log.Warn("session no longer passes allow-list", "email", email, "sub", sub)
			writeJSONError(w, http.StatusForbidden, "forbidden", "this account is no longer allowed")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(ctx, User{Email: email, Sub: sub})))
	})
}

// ---- cookies & responses ----

// sessionHash reads only SessionCookieName(); on https a plain
// kira_session cookie (which a sibling subdomain could plant) is ignored.
func (a *Auth) sessionHash(r *http.Request) (string, bool) {
	c, err := r.Cookie(a.SessionCookieName())
	if err != nil {
		return "", false
	}
	tok, ok := parseToken(c.Value)
	if !ok {
		return "", false
	}
	return hashToken(tok), true
}

func (a *Auth) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: a.SessionCookieName(), Value: token, Path: "/", // no Domain (__Host- rule)
		MaxAge: int(SessionTTL / time.Second), HttpOnly: true,
		Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: path, MaxAge: -1, HttpOnly: true,
		Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode,
	})
}

type errorBody struct {
	Error  string            `json:"error"`
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields"`
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: msg, Code: code})
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Kira · {{.Title}}</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem;color:#1f2328}a{color:inherit}</style>
</head><body><h1>{{.Title}}</h1><p>{{.Message}}</p><p><a href="/login">Back to sign-in</a></p></body></html>
`))

func writePage(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, struct{ Title, Message string }{title, msg})
}
