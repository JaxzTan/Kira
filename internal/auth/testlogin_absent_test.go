//go:build !e2e

package auth

import (
	"net/http/httptest"
	"testing"
)

// TestTestLoginAbsentInNormalBuild proves CLAUDE.md rule 6: without
// -tags e2e there is no /auth/test-login route and no way to mint a session,
// even from loopback with KIRA_E2E=1.
func TestTestLoginAbsentInNormalBuild(t *testing.T) {
	t.Setenv("KIRA_E2E", "1")
	h := newHarness(t, nil)
	for _, method := range []string{"POST", "GET"} {
		for _, remote := range []string{"127.0.0.1:5555", "[::1]:5555", "192.0.2.1:1234"} {
			req := httptest.NewRequest(method, "/auth/test-login", nil)
			req.RemoteAddr = remote
			rec := h.do(req)
			if rec.Code != 404 {
				t.Fatalf("%s /auth/test-login from %s = %d, want 404", method, remote, rec.Code)
			}
			if findCookie(rec, SessionCookie) != nil || len(h.store.Sessions()) != 0 || len(h.store.AuthLog()) != 0 {
				t.Fatal("session or auth_log row created")
			}
		}
	}
}

func TestE2EGuardNormalBuild(t *testing.T) {
	for _, v := range []string{"", "0", "1"} {
		t.Setenv("KIRA_E2E", v)
		if err := E2EGuard(); err != nil {
			t.Fatalf("KIRA_E2E=%q: E2EGuard() = %v, want nil in normal builds", v, err)
		}
	}
}
