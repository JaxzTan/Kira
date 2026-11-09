//go:build e2e

package auth

import (
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// registerTestLogin adds POST /auth/test-login (e2e builds only). It creates
// a session for AllowedEmail with sub E2ETestSub, pinning the owner to that
// sub if none is pinned (e2e runs use a temp DB). It answers 404 unless
// KIRA_E2E=1 and the TCP peer is loopback, and 403 if the owner is pinned
// to another sub.
func (a *Auth) registerTestLogin(r chi.Router) {
	a.log.Warn("E2E TEST BUILD: POST /auth/test-login is enabled (loopback only, KIRA_E2E=1). Never run this binary against real data.")
	r.Post("/auth/test-login", a.handleTestLogin)
}

// isLoopbackPeer looks only at the TCP peer (r.RemoteAddr); X-Forwarded-For
// and similar headers are ignored on purpose.
func isLoopbackPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *Auth) handleTestLogin(w http.ResponseWriter, r *http.Request) {
	if !e2eEnabled() || !isLoopbackPeer(r) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	email := a.cfg.AllowedEmail
	result, err := a.checkOrPinSub(ctx, email, E2ETestSub)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	if result == ResultRejectSub {
		a.logAuth(ctx, email, E2ETestSub, ResultRejectSub)
		writeJSONError(w, http.StatusForbidden, "forbidden", "owner is pinned to a different sub")
		return
	}
	token, _, err := a.newSession(ctx, email, E2ETestSub, r.UserAgent())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	a.logAuth(ctx, email, E2ETestSub, result)
	a.setSessionCookie(w, token)
	w.WriteHeader(http.StatusNoContent)
}
