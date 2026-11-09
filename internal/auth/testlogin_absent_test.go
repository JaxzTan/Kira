//go:build !e2e

package auth

import (
	"net/http/httptest"
	"testing"
)

// TestTestLoginAbsentInNormalBuild proves CLAUDE.md rule 6: without
// -tags e2e there is no /auth/test-login route and no way to mint a session.
func TestTestLoginAbsentInNormalBuild(t *testing.T) {
	h := newHarness(t, nil)
	for _, method := range []string{"POST", "GET"} {
		rec := h.do(httptest.NewRequest(method, "/auth/test-login", nil))
		if rec.Code != 404 && rec.Code != 405 {
			t.Fatalf("%s /auth/test-login = %d, want 404", method, rec.Code)
		}
		if rec.Code == 405 {
			t.Fatalf("%s /auth/test-login = 405: some method is registered", method)
		}
		if findCookie(rec, SessionCookie) != nil || len(h.store.Sessions()) != 0 {
			t.Fatal("session created")
		}
	}
}
