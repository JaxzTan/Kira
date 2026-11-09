//go:build !e2e

package api

import (
	"net/http"
	"testing"
)

// CLAUDE.md rule 6: the full router of a normal build has no
// /auth/test-login and no other way to mint a session.
func TestRouterHasNoTestLogin(t *testing.T) {
	h := newHarness(t)
	before := len(h.mem.Sessions())
	for _, m := range []string{"POST", "GET"} {
		rec := h.serve(h.req(m, "/auth/test-login", nil))
		wantError(t, rec, http.StatusNotFound, CodeNotFound)
		for _, c := range rec.Result().Cookies() {
			if c.Name == "kira_session" && c.Value != "" {
				t.Fatal("session cookie set")
			}
		}
	}
	if len(h.mem.Sessions()) != before {
		t.Fatal("session created")
	}
}
