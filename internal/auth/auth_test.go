package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewValidatesConfig(t *testing.T) {
	ok := Config{ClientID: "c", RedirectURL: "http://x/cb", AllowedEmail: "a@b.c", SessionSecret: make([]byte, 32)}
	cases := []struct {
		name string
		mut  func(*Config)
		ok   bool
	}{
		{"valid", func(*Config) {}, true},
		{"no client id", func(c *Config) { c.ClientID = "" }, false},
		{"no redirect", func(c *Config) { c.RedirectURL = "" }, false},
		{"no email", func(c *Config) { c.AllowedEmail = "  " }, false},
		{"short secret", func(c *Config) { c.SessionSecret = make([]byte, 31) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ok
			tc.mut(&c)
			_, err := New(c, newMem())
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestLoginRedirect(t *testing.T) {
	for _, secure := range []bool{false, true} {
		h := newHarness(t, func(c *Config) { c.Secure = secure })
		ls := h.startLogin()
		q := ls.location.Query()
		if ls.location.Path != "/authorize" || q.Get("client_id") != testClientID ||
			q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
			ls.challenge == "" || ls.state == "" || ls.nonce == "" ||
			!strings.Contains(q.Get("scope"), "openid") {
			t.Fatalf("bad authorize URL: %s", ls.location)
		}
		if q.Get("code_verifier") != "" {
			t.Fatal("verifier leaked into URL")
		}
		c := ls.cookie
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure != secure ||
			c.MaxAge != 600 || c.Path != "/auth/google" {
			t.Fatalf("bad kira_oauth cookie flags: %+v", c)
		}
	}
}

func TestCallback(t *testing.T) {
	_, other := keys(t)
	type tc struct {
		name       string
		cfg        func(*Config)
		prePin     string // owner sub pinned before the login
		run        func(h *harness) *httptest.ResponseRecorder
		wantStatus int
		wantResult string
		wantPinned string
	}
	login := func(mut func(map[string]any)) func(*harness) *httptest.ResponseRecorder {
		return func(h *harness) *httptest.ResponseRecorder { return h.login(mut) }
	}
	cases := []tc{
		{name: "happy path pins owner", run: login(nil),
			wantStatus: 302, wantResult: ResultOwnerPinned, wantPinned: testSub},
		{name: "happy path existing pin", prePin: testSub, run: login(nil),
			wantStatus: 302, wantResult: ResultOK, wantPinned: testSub},
		{name: "email case-insensitive", run: login(func(c map[string]any) { c["email"] = " Owner@Example.COM" }),
			wantStatus: 302, wantResult: ResultOwnerPinned, wantPinned: testSub},
		{name: "ALLOWED_SUB matches, no pin written", cfg: func(c *Config) { c.AllowedSub = testSub }, run: login(nil),
			wantStatus: 302, wantResult: ResultOK},
		{name: "email_verified as string true", run: login(func(c map[string]any) { c["email_verified"] = "true" }),
			wantStatus: 302, wantResult: ResultOwnerPinned, wantPinned: testSub},
		{name: "wrong email", run: login(func(c map[string]any) { c["email"] = "intruder@example.com" }),
			wantStatus: 403, wantResult: ResultRejectEmail},
		{name: "unverified email", run: login(func(c map[string]any) { c["email_verified"] = false }),
			wantStatus: 403, wantResult: ResultRejectUnverified},
		{name: "missing email_verified", run: login(func(c map[string]any) { delete(c, "email_verified") }),
			wantStatus: 403, wantResult: ResultRejectUnverified},
		{name: "wrong sub after pin", prePin: testSub, run: login(func(c map[string]any) { c["sub"] = "other-sub" }),
			wantStatus: 403, wantResult: ResultRejectSub, wantPinned: testSub},
		{name: "wrong sub vs ALLOWED_SUB", cfg: func(c *Config) { c.AllowedSub = "pinned-env" }, run: login(nil),
			wantStatus: 403, wantResult: ResultRejectSub},
		{name: "expired token", run: login(func(c map[string]any) {
			c["iat"] = time.Date(2026, 11, 9, 1, 0, 0, 0, time.UTC).Unix()
			c["exp"] = time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC).Unix()
		}), wantStatus: 403, wantResult: ResultRejectToken},
		{name: "wrong aud", run: login(func(c map[string]any) { c["aud"] = "someone-else" }),
			wantStatus: 403, wantResult: ResultRejectToken},
		{name: "wrong iss", run: login(func(c map[string]any) { c["iss"] = "https://evil.example" }),
			wantStatus: 403, wantResult: ResultRejectToken},
		{name: "nonce mismatch", run: login(func(c map[string]any) { c["nonce"] = "not-the-nonce" }),
			wantStatus: 403, wantResult: ResultRejectToken},
		{name: "missing nonce", run: login(func(c map[string]any) { delete(c, "nonce") }),
			wantStatus: 403, wantResult: ResultRejectToken},
		{name: "bad signature", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			h.idp.issue("code-1", sign(t, other, h.defaultClaims(ls)), ls.challenge)
			return h.callback(ls.state, "code-1", ls.cookie)
		}, wantStatus: 403, wantResult: ResultRejectToken},
		{name: "unknown code", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			return h.callback(ls.state, "never-issued", ls.cookie)
		}, wantStatus: 403, wantResult: ""},
		{name: "missing code", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			return h.callback(ls.state, "", ls.cookie)
		}, wantStatus: 403, wantResult: ""},
		{name: "bad state", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			h.idp.issue("code-1", sign(t, h.idp.key, h.defaultClaims(ls)), ls.challenge)
			return h.callback("forged-state", "code-1", ls.cookie)
		}, wantStatus: 400, wantResult: ""},
		{name: "missing state", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			return h.callback("", "code-1", ls.cookie)
		}, wantStatus: 400, wantResult: ""},
		{name: "missing oauth cookie", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			return h.callback(ls.state, "code-1", nil)
		}, wantStatus: 400, wantResult: ""},
		{name: "tampered oauth cookie", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			c := *ls.cookie
			c.Value = "x" + c.Value[1:]
			return h.callback(ls.state, "code-1", &c)
		}, wantStatus: 400, wantResult: ""},
		{name: "oauth cookie older than 10 min", run: func(h *harness) *httptest.ResponseRecorder {
			ls := h.startLogin()
			h.clock.Add(11 * time.Minute)
			h.idp.issue("code-1", sign(t, h.idp.key, h.defaultClaims(ls)), ls.challenge)
			return h.callback(ls.state, "code-1", ls.cookie)
		}, wantStatus: 400, wantResult: ""},
		{name: "wrong client secret", cfg: func(c *Config) { c.ClientSecret = "nope" }, run: login(nil),
			wantStatus: 403, wantResult: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.cfg)
			if c.prePin != "" {
				if err := h.store.PinOwner(t.Context(), c.prePin, testEmail, h.clock.Now()); err != nil {
					t.Fatal(err)
				}
			}
			rec := c.run(h)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, c.wantStatus, rec.Body)
			}
			if got := h.lastResult(); got != c.wantResult {
				t.Fatalf("auth_log result = %q, want %q", got, c.wantResult)
			}
			wantRows := 1
			if c.wantResult == "" { // unauthenticated failure: slog WARN only
				wantRows = 0
				if !strings.Contains(h.logs.String(), "level=WARN") ||
					!strings.Contains(h.logs.String(), "result="+ResultRejectState) {
					t.Fatalf("missing WARN reject_state log: %s", h.logs)
				}
			}
			if n := len(h.store.AuthLog()); n != wantRows {
				t.Fatalf("auth_log rows = %d, want %d", n, wantRows)
			}
			pinned, _, _ := h.store.OwnerSub(t.Context())
			if pinned != c.wantPinned {
				t.Fatalf("pinned = %q, want %q", pinned, c.wantPinned)
			}
			if oc := findCookie(rec, OAuthCookie); oc == nil || oc.MaxAge >= 0 {
				t.Fatal("kira_oauth cookie not cleared")
			}
			sc := findCookie(rec, SessionCookie)
			if c.wantStatus != 302 {
				if sc != nil || len(h.store.Sessions()) != 0 {
					t.Fatal("rejected login created a session")
				}
				if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
					t.Fatalf("content-type = %q", ct)
				}
				if c.wantStatus == 403 && c.wantResult != ResultRejectToken && c.wantResult != "" &&
					!strings.Contains(rec.Body.String(), "This Google account is not allowed into Kira") {
					t.Fatalf("403 page text missing: %s", rec.Body)
				}
				return
			}
			if rec.Header().Get("Location") != "/" {
				t.Fatalf("redirect = %q", rec.Header().Get("Location"))
			}
			if sc == nil {
				t.Fatal("no session cookie")
			}
			row := h.store.AuthLog()[0]
			if row.Sub != testSub || !strings.EqualFold(strings.TrimSpace(row.Email), testEmail) {
				t.Fatalf("auth_log row = %+v", row)
			}
			if me := h.api(sc.Value); me.Code != 200 {
				t.Fatalf("/api/me with new session = %d", me.Code)
			}
		})
	}
}

func TestRejectLogsIdentity(t *testing.T) {
	h := newHarness(t, nil)
	h.login(func(c map[string]any) { c["email"] = "intruder@example.com"; c["sub"] = "intruder-sub" })
	row := h.store.AuthLog()[0]
	if row.Email != "intruder@example.com" || row.Sub != "intruder-sub" || row.Result != ResultRejectEmail {
		t.Fatalf("row = %+v", row)
	}
}

func TestStateCookieSingleUse(t *testing.T) {
	h := newHarness(t, nil)
	ls := h.startLogin()
	h.idp.issue("code-1", sign(t, h.idp.key, h.defaultClaims(ls)), ls.challenge)
	if rec := h.callback(ls.state, "code-1", ls.cookie); rec.Code != 302 {
		t.Fatalf("first callback = %d", rec.Code)
	}
	// Replaying the same code is refused by the IdP (code is single use).
	if rec := h.callback(ls.state, "code-1", ls.cookie); rec.Code != 403 {
		t.Fatalf("replay = %d, want 403", rec.Code)
	}
}

func TestSessionCookie(t *testing.T) {
	for _, secure := range []bool{false, true} {
		h := newHarness(t, func(c *Config) { c.Secure = secure })
		rec := h.login(nil)
		name, other := SessionCookie, SecureSessionCookie
		if secure {
			name, other = SecureSessionCookie, SessionCookie
		}
		if h.auth.SessionCookieName() != name || findCookie(rec, other) != nil {
			t.Fatalf("secure=%v: wrong cookie name", secure)
		}
		c := findCookie(rec, name)
		if c == nil {
			t.Fatal("no session cookie")
		}
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure != secure ||
			c.Path != "/" || c.Domain != "" || c.MaxAge != 2592000 {
			t.Fatalf("bad flags: %+v", c)
		}
		if _, ok := parseToken(c.Value); !ok {
			t.Fatalf("token is not 32 bytes base64url")
		}
		sess := h.store.Sessions()
		if len(sess) != 1 {
			t.Fatalf("sessions = %d", len(sess))
		}
		for k, s := range sess {
			if k != hashToken(c.Value) || k == c.Value || len(k) != 64 {
				t.Fatal("store does not hold SHA-256 hex of token")
			}
			if !s.ExpiresAt.Equal(h.clock.Now().Add(30*24*time.Hour)) || s.Email != testEmail || s.Sub != testSub {
				t.Fatalf("session row = %+v", s)
			}
		}
	}
}

func TestMiddleware(t *testing.T) {
	type tc struct {
		name       string
		setup      func(h *harness, token string) string // returns cookie value to send
		wantStatus int
		wantCode   string
	}
	cases := []tc{
		{"valid session", func(h *harness, tok string) string { return tok }, 200, ""},
		{"no cookie", func(h *harness, tok string) string { return "" }, 401, "unauthorized"},
		{"malformed cookie", func(h *harness, tok string) string { return "not-a-token" }, 401, "unauthorized"},
		{"unknown token", func(h *harness, tok string) string {
			return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}, 401, "unauthorized"},
		{"expired session", func(h *harness, tok string) string {
			h.clock.Add(30*24*time.Hour + time.Second)
			return tok
		}, 401, "unauthorized"},
		{"ALLOWED_EMAIL changed revokes session", func(h *harness, tok string) string {
			h.rebuild(func(c *Config) { c.AllowedEmail = "new-owner@example.com" })
			return tok
		}, 403, "forbidden"},
		{"ALLOWED_SUB set to different sub revokes session", func(h *harness, tok string) string {
			h.rebuild(func(c *Config) { c.AllowedSub = "another-sub" })
			return tok
		}, 403, "forbidden"},
		{"ALLOWED_EMAIL case change still ok", func(h *harness, tok string) string {
			h.rebuild(func(c *Config) { c.AllowedEmail = "OWNER@example.com" })
			return tok
		}, 200, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			sc := findCookie(h.login(nil), SessionCookie)
			if sc == nil {
				t.Fatal("login failed")
			}
			rec := h.api(c.setup(h, sc.Value))
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, c.wantStatus, rec.Body)
			}
			if c.wantStatus == 200 {
				var me map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &me)
				if me["email"] != testEmail || me["name"] != nil || me["picture"] != nil {
					t.Fatalf("me = %v", me)
				}
				return
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("not JSON: %s", rec.Body)
			}
			if body["code"] != c.wantCode || body["error"] == "" {
				t.Fatalf("body = %v", body)
			}
			if f, present := body["fields"]; !present || f != nil {
				t.Fatalf("fields must be null: %v", body)
			}
			if c.name == "expired session" && len(h.store.Sessions()) != 0 {
				t.Fatal("expired session not deleted")
			}
		})
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t, nil)
	sc := findCookie(h.login(nil), SessionCookie)
	req := httptest.NewRequest("POST", "/auth/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sc.Value})
	rec := h.do(req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	if c := findCookie(rec, SessionCookie); c == nil || c.MaxAge >= 0 || c.Value != "" {
		t.Fatal("session cookie not cleared")
	}
	if len(h.store.Sessions()) != 0 {
		t.Fatal("session not deleted")
	}
	if rec := h.api(sc.Value); rec.Code != 401 {
		t.Fatalf("api after logout = %d", rec.Code)
	}
	// No session → 401.
	noSess := httptest.NewRequest("POST", "/auth/logout", nil)
	noSess.Header.Set("Sec-Fetch-Site", "same-origin")
	if rec := h.do(noSess); rec.Code != 401 {
		t.Fatalf("logout without session = %d", rec.Code)
	}
	if rec := h.do(httptest.NewRequest("GET", "/auth/logout", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout = %d", rec.Code)
	}
}

func TestNoSecretsInLogs(t *testing.T) {
	h := newHarness(t, nil)
	ls := h.startLogin()
	idt := sign(t, h.idp.key, h.defaultClaims(ls))
	h.idp.issue("secret-code-xyz", idt, ls.challenge)
	rec := h.callback(ls.state, "secret-code-xyz", ls.cookie)
	sc := findCookie(rec, SessionCookie)
	if sc == nil {
		t.Fatal("login failed")
	}
	h.login(func(c map[string]any) { c["aud"] = "x" }) // a rejected path too
	logs := h.logs.String()
	if !strings.Contains(logs, "owner_pinned") {
		t.Fatalf("expected login log line, got: %s", logs)
	}
	var verifier string
	if st, err := h.auth.decodeOAuthState(ls.cookie.Value); err == nil {
		verifier = st.Verifier
	}
	for name, secret := range map[string]string{
		"session token": sc.Value, "session hash": hashToken(sc.Value), "code": "secret-code-xyz",
		"id token": idt, "client secret": testClientSecret, "access token": "at-DO-NOT-LOG",
		"oauth cookie": ls.cookie.Value, "state": ls.state, "nonce": ls.nonce, "verifier": verifier,
		"session secret": string(bytes.Repeat([]byte("s"), 32)),
	} {
		if secret != "" && strings.Contains(logs, secret) {
			t.Errorf("%s leaked into logs", name)
		}
	}
}

func TestEmailVerifiedParsing(t *testing.T) {
	cases := map[string]bool{`true`: true, `"true"`: true, `false`: false, `"false"`: false, ``: false, `1`: false, `null`: false}
	for raw, want := range cases {
		if got := (idClaims{EmailVerified: json.RawMessage(raw)}).verified(); got != want {
			t.Errorf("%q: got %v want %v", raw, got, want)
		}
	}
}

func TestUserFrom(t *testing.T) {
	if _, ok := UserFrom(t.Context()); ok {
		t.Fatal("empty context has a user")
	}
	u, ok := UserFrom(WithUser(t.Context(), User{Email: "a", Sub: "b"}))
	if !ok || u.Email != "a" || u.Sub != "b" || u.Name != nil || u.Picture != nil {
		t.Fatalf("got %+v %v", u, ok)
	}
}

func TestSameOrigin(t *testing.T) {
	const pub = "https://kira.example.com"
	cases := []struct {
		name    string
		headers map[string][]string
		public  string
		want    bool
	}{
		{"sec-fetch-site same-origin", map[string][]string{"Sec-Fetch-Site": {"same-origin"}}, pub, true},
		{"sec-fetch-site same-origin, no public origin", map[string][]string{"Sec-Fetch-Site": {"same-origin"}}, "", true},
		{"sec-fetch-site wins over matching origin", map[string][]string{"Sec-Fetch-Site": {"cross-site"}, "Origin": {pub}}, pub, false},
		{"sec-fetch-site same-site", map[string][]string{"Sec-Fetch-Site": {"same-site"}}, pub, false},
		{"sec-fetch-site none", map[string][]string{"Sec-Fetch-Site": {"none"}}, pub, false},
		{"duplicate sec-fetch-site", map[string][]string{"Sec-Fetch-Site": {"same-origin", "cross-site"}}, pub, false},
		{"matching origin", map[string][]string{"Origin": {pub}}, pub, true},
		{"matching origin, public has trailing slash", map[string][]string{"Origin": {pub}}, pub + "/", true},
		{"matching origin with port", map[string][]string{"Origin": {"http://localhost:5173"}}, "http://localhost:5173", true},
		{"mismatched origin", map[string][]string{"Origin": {"https://evil.example"}}, pub, false},
		{"origin wrong scheme", map[string][]string{"Origin": {"http://kira.example.com"}}, pub, false},
		{"origin wrong port", map[string][]string{"Origin": {"http://localhost:5174"}}, "http://localhost:5173", false},
		{"origin null", map[string][]string{"Origin": {"null"}}, pub, false},
		{"origin but no public origin configured", map[string][]string{"Origin": {pub}}, "", false},
		{"missing both", nil, pub, false},
		{"missing both, no public origin", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/entries", nil)
			for k, vs := range c.headers {
				for _, v := range vs {
					r.Header.Add(k, v)
				}
			}
			if got := SameOrigin(r, c.public); got != c.want {
				t.Fatalf("SameOrigin = %v, want %v", got, c.want)
			}
		})
	}
}

func TestLogoutCSRF(t *testing.T) {
	const pub = "http://kira.test"
	cases := []struct {
		name       string
		public     string
		headers    map[string]string
		wantStatus int
	}{
		{"sec-fetch-site same-origin", pub, map[string]string{"Sec-Fetch-Site": "same-origin"}, 204},
		{"sec-fetch-site same-origin, no PublicOrigin", "", map[string]string{"Sec-Fetch-Site": "same-origin"}, 204},
		{"matching origin", pub, map[string]string{"Origin": pub}, 204},
		{"cross-site fetch", pub, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": pub}, 403},
		{"mismatched origin", pub, map[string]string{"Origin": "http://evil.test"}, 403},
		{"origin with no PublicOrigin", "", map[string]string{"Origin": pub}, 403},
		{"missing both", pub, nil, 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, func(cfg *Config) { cfg.PublicOrigin = c.public })
			sc := findCookie(h.login(nil), SessionCookie)
			req := httptest.NewRequest("POST", "/auth/logout", nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sc.Value})
			rec := h.do(req)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if c.wantStatus == 204 {
				if len(h.store.Sessions()) != 0 {
					t.Fatal("session not deleted")
				}
				return
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["code"] != "forbidden" {
				t.Fatalf("body = %s", rec.Body)
			}
			if findCookie(rec, SessionCookie) != nil || len(h.store.Sessions()) != 1 {
				t.Fatal("cross-site logout touched the session")
			}
			if me := h.api(sc.Value); me.Code != 200 {
				t.Fatalf("session should survive rejected logout, /api/me = %d", me.Code)
			}
		})
	}
}

func TestSecureCookieNameOnly(t *testing.T) {
	cases := []struct {
		name       string
		secure     bool
		sendAs     string
		wantStatus int
	}{
		{"https accepts __Host- name", true, SecureSessionCookie, 200},
		{"https ignores plain name", true, SessionCookie, 401},
		{"http accepts plain name", false, SessionCookie, 200},
		{"http ignores __Host- name", false, SecureSessionCookie, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, func(cfg *Config) { cfg.Secure = c.secure })
			sc := findCookie(h.login(nil), h.auth.SessionCookieName())
			if sc == nil {
				t.Fatal("login failed")
			}
			req := httptest.NewRequest("GET", "/api/me", nil)
			req.AddCookie(&http.Cookie{Name: c.sendAs, Value: sc.Value})
			if rec := h.do(req); rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			// Logout clears the cookie under the configured name.
			lo := httptest.NewRequest("POST", "/auth/logout", nil)
			lo.Header.Set("Sec-Fetch-Site", "same-origin")
			lo.AddCookie(&http.Cookie{Name: h.auth.SessionCookieName(), Value: sc.Value})
			rec := h.do(lo)
			cl := findCookie(rec, h.auth.SessionCookieName())
			if rec.Code != 204 || cl == nil || cl.MaxAge >= 0 || cl.Secure != c.secure || cl.Path != "/" {
				t.Fatalf("logout = %d, clear cookie %+v", rec.Code, cl)
			}
		})
	}
}
