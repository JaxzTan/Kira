//go:build e2e

package config

import (
	"os"
	"testing"
)

// E2E builds never read the repo .env: tests pass every variable.
func TestE2EBuildIgnoresDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("DATA_DIR=./fromfile\nLEDGER_KEY="+goodLedger+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", "")
	t.Setenv("LEDGER_KEY", "")
	os.Unsetenv("DATA_DIR")
	os.Unsetenv("LEDGER_KEY")
	_, err := Load(ModeLedger)
	if err == nil {
		t.Fatal(".env was loaded in an e2e build")
	}
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("LEDGER_KEY", goodLedger)
	c, err := Load(ModeLedger)
	if err != nil || c.EnvFile != "" {
		t.Fatalf("%+v %v", c.EnvFile, err)
	}
}
