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
			if c.AllowedSub != "123" || c.OIDCIssuerURL == "" || c.Secure() {
				t.Fatalf("%+v", c)
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

func TestLoadDotEnvDoesNotOverrideEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	env := "DATA_DIR=./fromfile\nLEDGER_KEY=" + goodLedger + "\n"
	if err := os.WriteFile(".env", []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", filepath.Join(dir, "fromenv"))
	t.Setenv("LEDGER_KEY", "") // registers cleanup; then unset so .env supplies it
	os.Unsetenv("LEDGER_KEY")
	c, err := Load(ModeLedger)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(c.DataDir) != "fromenv" {
		t.Fatalf("env should win over .env, got %s", c.DataDir)
	}
	if len(c.LedgerKey) != 32 {
		t.Fatal(".env value not loaded")
	}
}

func TestLoadBadDotEnvDoesNotQuote(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("LEDGER_KEY='"+secretVal+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(ModeLedger)
	if err == nil {
		t.Skip("godotenv accepted the line; nothing to check")
	}
	if strings.Contains(err.Error(), secretVal) {
		t.Fatalf("error quotes .env content: %v", err)
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
