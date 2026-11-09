//go:build !e2e

package auth

import "github.com/go-chi/chi/v5"

// registerTestLogin registers nothing in normal builds: /auth/test-login
// does not exist unless built with -tags e2e (CLAUDE.md rule 6).
func (a *Auth) registerTestLogin(chi.Router) {}
