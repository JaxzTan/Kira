package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	goodSession = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	goodLedger  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	secretVal   = "super-secret-client-value-XYZ"
)

func fullEnv() map[string]string {
	return map[string]string{
		"PORT":                 "8080",
		"PUBLIC_URL":           "https://kira.example.ts.net/",
		"DATA_DIR":             "./data",
		"GOOGLE_CLIENT_ID":     "x.apps.googleusercontent.com",
		"GOOGLE_CLIENT_SECRET": secretVal,
		"ALLOWED_EMAIL":        "owner@example.com",
		"SESSION_SECRET":       goodSession,
		"LEDGER_KEY":           goodLedger,
	}
}

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestFromLookup(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		mutate   func(m map[string]string)
		wantErr  []string // substrings that must appear
		noLeak   []string // substrings that must NOT appear
		checkCfg func(t *testing.T, c Config)
	}{
		{name: "serve ok", mode: ModeServe, checkCfg: func(t *testing.T, c Config) {
			if c.Port != 8080 || c.PublicURL != "https://kira.example.ts.net" || !c.Secure() {
				t.Fatalf("bad cfg %+v", c)
			}
			if c.RedirectURL() != "https://kira.example.ts.net/auth/google/callback" {
				t.Fatal(c.RedirectURL())
			}
			if len(c.SessionSecret) != 32 || len(c.LedgerKey) != 32 || !filepath.IsAbs(c.DataDir) {
				t.Fatal("keys/dir not decoded")
			}
			if filepath.Base(c.DBPath()) != "kira.db" || filepath.Base(c.HeadPath()) != "head" {
				t.Fatal("paths")
			}
		}},
		{name: "missing client id", mode: ModeServe, mutate: func(m map[string]string) { delete(m, "GOOGLE_CLIENT_ID") },
			wantErr: []string{"GOOGLE_CLIENT_ID is not set"}, noLeak: []string{secretVal, goodSession, goodLedger}},
		{name: "empty secret", mode: ModeServe, mutate: func(m map[string]string) { m["GOOGLE_CLIENT_SECRET"] = "  " },
			wantErr: []string{"GOOGLE_CLIENT_SECRET is not set"}},
		{name: "bad ledger key never echoed", mode: ModeServe, mutate: func(m map[string]string) { m["LEDGER_KEY"] = "zz" + secretVal },
			wantErr: []string{"LEDGER_KEY must be 64 hex"}, noLeak: []string{secretVal, goodSession}},
		{name: "short session secret", mode: ModeServe, mutate: func(m map[string]string) { m["SESSION_SECRET"] = "abcd" },
			wantErr: []string{"SESSION_SECRET must be 64 hex"}, noLeak: []string{"abcd"}},
		{name: "bad port", mode: ModeServe, mutate: func(m map[string]string) { m["PORT"] = "99999" }, wantErr: []string{"PORT"}},
		{name: "bad url", mode: ModeServe, mutate: func(m map[string]string) { m["PUBLIC_URL"] = "ftp://x" }, wantErr: []string{"PUBLIC_URL"}},
		{name: "bad email", mode: ModeServe, mutate: func(m map[string]string) { m["ALLOWED_EMAIL"] = "nope" }, wantErr: []string{"ALLOWED_EMAIL"}},
		{name: "all problems at once", mode: ModeServe, mutate: func(m map[string]string) { clear(m) },
			wantErr: []string{"PORT", "PUBLIC_URL", "DATA_DIR", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "ALLOWED_EMAIL", "SESSION_SECRET", "LEDGER_KEY"}},
		{name: "ledger mode needs no google keys", mode: ModeLedger, mutate: func(m map[string]string) {
			for _, k := range []string{"PORT", "PUBLIC_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "ALLOWED_EMAIL", "SESSION_SECRET"} {
				delete(m, k)
			}
		}, checkCfg: func(t *testing.T, c Config) {
			if len(c.LedgerKey) != 32 || c.DataDir == "" {
				t.Fatal("ledger mode cfg")
			}
		}},
		{name: "ledger mode needs ledger key", mode: ModeLedger, mutate: func(m map[string]string) { delete(m, "LEDGER_KEY") },
			wantErr: []string{"LEDGER_KEY is not set"}},
		{name: "optional keys", mode: ModeServe, mutate: func(m map[string]string) {
			m["ALLOWED_SUB"] = "123"
			m["OIDC_ISSUER_URL"] = "http://127.0.0.1:9/"
			m["PUBLIC_URL"] = "http://localhost:5173"
		}, checkCfg: func(t *testing.T, c Config) {
			if c.AllowedSub != "123" || c.Secure() || c.Host != "127.0.0.1" || c.TrustProxy {
				t.Fatalf("%+v", c)
			}
			if e2eBuild != (c.OIDCIssuerURL != "") {
				t.Fatalf("OIDC_ISSUER_URL honoured=%v in e2eBuild=%v", c.OIDCIssuerURL != "", e2eBuild)
			}
			if !e2eBuild && !hasWarning(c, "OIDC_ISSUER_URL is set but ignored") {
				t.Fatalf("no warning: %v", c.Warnings)
			}
			if c.PublicOrigin() != "http://localhost:5173" {
				t.Fatal(c.PublicOrigin())
			}
		}},
		{name: "non-loopback host with http url", mode: ModeServe, mutate: func(m map[string]string) {
			m["HOST"] = "0.0.0.0"
			m["PUBLIC_URL"] = "http://kira.local:8080"
		}, wantErr: []string{"HOST is not a loopback address while PUBLIC_URL is http://"}},
		{name: "non-loopback host with https url", mode: ModeServe, mutate: func(m map[string]string) { m["HOST"] = "0.0.0.0" },
			checkCfg: func(t *testing.T, c Config) {
				if c.Host != "0.0.0.0" {
					t.Fatal(c.Host)
				}
			}},
		{name: "loopback hosts with http url", mode: ModeServe, mutate: func(m map[string]string) {
			m["HOST"] = "::1"
			m["PUBLIC_URL"] = "http://localhost:5173"
		}, checkCfg: func(t *testing.T, c Config) {}},
		{name: "localhost with http url", mode: ModeServe, mutate: func(m map[string]string) {
			m["HOST"] = "localhost"
			m["PUBLIC_URL"] = "http://localhost:5173"
		}, checkCfg: func(t *testing.T, c Config) {}},
		{name: "bad host", mode: ModeServe, mutate: func(m map[string]string) { m["HOST"] = "a b" }, wantErr: []string{"HOST must be"}},
		{name: "trust proxy on", mode: ModeServe, mutate: func(m map[string]string) { m["TRUST_PROXY"] = "1" },
			checkCfg: func(t *testing.T, c Config) {
				if !c.TrustProxy {
					t.Fatal("TRUST_PROXY")
				}
			}},
		{name: "trust proxy bad", mode: ModeServe, mutate: func(m map[string]string) { m["TRUST_PROXY"] = "maybe" }, wantErr: []string{"TRUST_PROXY must be 1 or 0"}},
		{name: "icloud data dir warns", mode: ModeLedger, mutate: func(m map[string]string) {
			m["DATA_DIR"] = "/Users/x/Library/Mobile Documents/com~apple~CloudDocs/kira/data"
		}, checkCfg: func(t *testing.T, c Config) {
			if !hasWarning(c, "data is in iCloud Drive; run scripts/install-macos.sh") {
				t.Fatalf("warnings = %v", c.Warnings)
			}
		}},
		{name: "normal data dir no warning", mode: ModeLedger, mutate: func(m map[string]string) { m["DATA_DIR"] = "/srv/kira/data" },
			checkCfg: func(t *testing.T, c Config) {
				if len(c.Warnings) != 0 {
					t.Fatalf("warnings = %v", c.Warnings)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fullEnv()
			if tt.mutate != nil {
				tt.mutate(m)
			}
			c, err := FromLookup(tt.mode, lookupFrom(m))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("expected error")
				}
				for _, w := range tt.wantErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q missing %q", err, w)
					}
				}
				for _, s := range tt.noLeak {
					if strings.Contains(err.Error(), s) {
						t.Errorf("error leaks a value: %q", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.checkCfg != nil {
				tt.checkCfg(t, c)
			}
		})
	}
}

func hasWarning(c Config, sub string) bool {
	for _, w := range c.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestPublicOrigin(t *testing.T) {
	tests := map[string]string{
		"https://Kira.Example.ts.net/":   "https://kira.example.ts.net",
		"http://localhost:5173":          "http://localhost:5173",
		"https://kira.example.ts.net/x/": "https://kira.example.ts.net",
		"":                               "",
	}
	for in, want := range tests {
		if got := (Config{PublicURL: strings.TrimRight(in, "/")}).PublicOrigin(); got != want {
			t.Errorf("PublicOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInICloud(t *testing.T) {
	tests := map[string]bool{
		"/Users/a/Library/Mobile Documents/com~apple~CloudDocs/kira/.env": true,
		"/Users/a/Library/Mobile Documents":                               true,
		"/Users/a/kira/data":                                              false,
		"/Users/a/Library/Application Support/kira":                       false,
	}
	for p, want := range tests {
		if got := InICloud(p); got != want {
			t.Errorf("InICloud(%q) = %v", p, got)
		}
	}
}

func TestLogValueHasNoSecrets(t *testing.T) {
	c, err := FromLookup(ModeServe, lookupFrom(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	s := c.LogValue().String()
	for _, bad := range []string{secretVal, "aaaaaaaa", "bbbbbbbb", "googleusercontent"} {
		if strings.Contains(s, bad) {
			t.Fatalf("LogValue leaks %q: %s", bad, s)
		}
	}
}

func TestEnsureDataDir(t *testing.T) {
	d := filepath.Join(t.TempDir(), "a", "data")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDataDir(d); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(d)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v err %v", fi.Mode().Perm(), err)
	}
}
