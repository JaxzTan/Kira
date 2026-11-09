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
		env        string
		remote     string
		xff        string
		prePin     string
		wantStatus int
		wantResult string
	}{
		{"pins owner on empty db", "1", "127.0.0.1:40000", "", "", 204, ResultOwnerPinned},
		{"ipv6 loopback", "1", "[::1]:40000", "", "", 204, ResultOwnerPinned},
		{"already pinned to e2e sub", "1", "127.0.0.1:40000", "", E2ETestSub, 204, ResultOK},
		{"pinned to someone else", "1", "127.0.0.1:40000", "", "real-sub", 403, ResultRejectSub},
		{"non-loopback peer", "1", "192.0.2.10:40000", "", "", 404, ""},
		{"non-loopback peer with loopback XFF", "1", "192.0.2.10:40000", "127.0.0.1", "", 404, ""},
		{"private LAN peer", "1", "10.0.0.5:40000", "", "", 404, ""},
		{"malformed RemoteAddr", "1", "garbage", "", "", 404, ""},
		{"KIRA_E2E unset", "", "127.0.0.1:40000", "", "", 404, ""},
		{"KIRA_E2E=0", "0", "127.0.0.1:40000", "", "", 404, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(E2EEnv, c.env)
			h := newHarness(t, nil)
			if c.prePin != "" {
				_ = h.store.PinOwner(t.Context(), c.prePin, testEmail, h.clock.Now())
			}
			req := httptest.NewRequest("POST", "/auth/test-login", nil)
			req.RemoteAddr = c.remote
			if c.xff != "" {
				req.Header.Set("X-Forwarded-For", c.xff)
			}
			rec := h.do(req)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if h.lastResult() != c.wantResult {
				t.Fatalf("result = %q", h.lastResult())
			}
			sc := findCookie(rec, SessionCookie)
			if c.wantStatus != 204 {
				if sc != nil || len(h.store.Sessions()) != 0 {
					t.Fatal("session created on reject")
				}
				if c.wantStatus == 404 {
					if len(h.store.AuthLog()) != 0 {
						t.Fatal("auth_log row on 404")
					}
					if pinned, found, _ := h.store.OwnerSub(t.Context()); found && pinned != c.prePin {
						t.Fatal("owner pinned by a refused test-login")
					}
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

func TestE2EGuardE2EBuild(t *testing.T) {
	cases := []struct {
		env    string
		wantOK bool
	}{{"1", true}, {"", false}, {"0", false}, {"true", false}, {"yes", false}}
	for _, c := range cases {
		t.Run("KIRA_E2E="+c.env, func(t *testing.T) {
			t.Setenv(E2EEnv, c.env)
			if err := E2EGuard(); (err == nil) != c.wantOK {
				t.Fatalf("E2EGuard() = %v, want ok=%v", err, c.wantOK)
			}
		})
	}
}
