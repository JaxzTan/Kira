//go:build e2e

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTestLoginE2E(t *testing.T) {
	cases := []struct {
		name       string
		prePin     string
		wantStatus int
		wantResult string
	}{
		{"pins owner on empty db", "", 204, ResultOwnerPinned},
		{"already pinned to e2e sub", E2ETestSub, 204, ResultOK},
		{"pinned to someone else", "real-sub", 403, ResultRejectSub},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if c.prePin != "" {
				_ = h.store.PinOwner(t.Context(), c.prePin, testEmail, h.clock.Now())
			}
			rec := h.do(httptest.NewRequest("POST", "/auth/test-login", nil))
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if h.lastResult() != c.wantResult {
				t.Fatalf("result = %q", h.lastResult())
			}
			sc := findCookie(rec, SessionCookie)
			if c.wantStatus != 204 {
				if sc != nil {
					t.Fatal("cookie set on reject")
				}
				return
			}
			if sc == nil || !sc.HttpOnly || sc.SameSite != http.SameSiteLaxMode {
				t.Fatalf("bad cookie %+v", sc)
			}
			if me := h.api(sc.Value); me.Code != 200 {
				t.Fatalf("/api/me = %d", me.Code)
			}
		})
	}
}
