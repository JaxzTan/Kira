//go:build !e2e

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestLoadRecordsEnvFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(".env", []byte("DATA_DIR=./data\nLEDGER_KEY="+goodLedger+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", "")
	t.Setenv("LEDGER_KEY", "")
	os.Unsetenv("DATA_DIR")
	os.Unsetenv("LEDGER_KEY")
	c, err := Load(ModeLedger)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(c.EnvFile) != ".env" || !filepath.IsAbs(c.EnvFile) {
		t.Fatalf("EnvFile = %q", c.EnvFile)
	}
	if strings.Contains(c.LogValue().String(), goodLedger) {
		t.Fatal("ledger key in LogValue")
	}
}
