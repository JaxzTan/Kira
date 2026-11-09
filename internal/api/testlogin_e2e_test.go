//go:build e2e

package api

import (
	"net/http"
	"testing"
)

// In -tags e2e builds the router exposes /auth/test-login (behind the /auth
// rate limit). The harness owner is pinned to a different sub, so it is 403.
func TestRouterTestLoginE2E(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.serve(h.req("POST", "/auth/test-login", nil)), http.StatusForbidden, CodeForbidden)
}
