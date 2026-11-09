//go:build e2e

package api

import (
	"net/http"
	"testing"
)

// In -tags e2e builds the router exposes /auth/test-login (behind the /auth
// rate limit). It is 404 unless KIRA_E2E=1 and the TCP peer is loopback;
// the harness owner is pinned to a different sub, so the real attempt is 403.
func TestRouterTestLoginE2E(t *testing.T) {
	h := newHarness(t)
	h.remote = "127.0.0.1:4000"
	t.Setenv("KIRA_E2E", "")
	wantStatus(t, h.serve(h.req("POST", "/auth/test-login", nil)), http.StatusNotFound)

	t.Setenv("KIRA_E2E", "1")
	wantError(t, h.serve(h.req("POST", "/auth/test-login", nil)), http.StatusForbidden, CodeForbidden)

	h.remote = "192.0.2.10:5555"
	wantStatus(t, h.serve(h.req("POST", "/auth/test-login", nil)), http.StatusNotFound)
}
