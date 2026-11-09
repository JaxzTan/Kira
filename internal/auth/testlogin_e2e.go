//go:build e2e

package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// registerTestLogin adds POST /auth/test-login (e2e builds only). It creates
// a session for AllowedEmail with sub E2ETestSub, pinning the owner to that
// sub if none is pinned (e2e runs use a temp DB).
func (a *Auth) registerTestLogin(r chi.Router) {
	r.Post("/auth/test-login", a.handleTestLogin)
}

func (a *Auth) handleTestLogin(w http.ResponseWriter, r *http.Request) {
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
