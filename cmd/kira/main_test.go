package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/JaxzTan/kira/internal/config"
)

const testLedgerKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func ledgerEnv(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir()) // no stray .env
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv("DATA_DIR", dir)
	t.Setenv("LEDGER_KEY", testLedgerKey)
	return dir
}

func runCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunUsage(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{nil, 2},
		{[]string{"bogus"}, 2},
		{[]string{"help"}, 0},
		{[]string{"seed", "--n", "0"}, 2},
		{[]string{"seed", "--bad-flag"}, 2},
	}
	for _, tt := range tests {
		if code, _, _ := runCmd(t, tt.args...); code != tt.code {
			t.Errorf("run(%v) = %d, want %d", tt.args, code, tt.code)
		}
	}
}

func TestSeedAndVerify(t *testing.T) {
	dir := ledgerEnv(t)

	// verify before any DB exists: error exit 2
	if code, out, _ := runCmd(t, "verify"); code != 2 || !strings.Contains(out, "no ledger database") {
		t.Fatalf("verify without db = %d %q", code, out)
	}

	code, out, errOut := runCmd(t, "seed", "--n", "25")
	if code != 0 || !strings.HasPrefix(out, "seeded 25 entries head=25:") {
		t.Fatalf("seed = %d %q %q", code, out, errOut)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("DATA_DIR mode: %v %v", fi.Mode().Perm(), err)
	}
	// non-empty ledger: refuse without --force
	if code, _, errOut := runCmd(t, "seed", "--n", "1"); code != 1 || !strings.Contains(errOut, "--force") {
		t.Fatalf("seed refuse = %d %q", code, errOut)
	}
	if code, out, _ := runCmd(t, "seed", "--n", "2", "--force"); code != 0 || !strings.Contains(out, "head=27:") {
		t.Fatalf("seed --force = %d %q", code, out)
	}

	code, out, _ = runCmd(t, "verify")
	if code != 0 || !regexp.MustCompile(`^ok entries=27 head=27:[0-9a-f]{64}\n$`).MatchString(out) {
		t.Fatalf("verify ok = %d %q", code, out)
	}
	head, err := os.ReadFile(filepath.Join(dir, "head"))
	if err != nil || !strings.HasPrefix(string(head), "27:") {
		t.Fatalf("head file %q %v", head, err)
	}

	// Simulate deleted tail rows: recorded head is ahead of the DB.
	if err := os.WriteFile(filepath.Join(dir, "head"), []byte("30:"+strings.Repeat("e", 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runCmd(t, "verify")
	if code != 1 || !strings.HasPrefix(out, "BROKEN at id=28 reason=head mismatch") {
		t.Fatalf("verify broken = %d %q", code, out)
	}
	// still broken on the next run: a failed verify never overwrites the head
	if code, _, _ := runCmd(t, "verify"); code != 1 {
		t.Fatal("failed verify overwrote the recorded head")
	}
	// seed refuses to write to a broken ledger even with --force
	if code, _, _ := runCmd(t, "seed", "--n", "1", "--force"); code != 1 {
		t.Fatal("seed wrote to a broken ledger")
	}
}

func TestVerifyNeedsOnlyLedgerKeys(t *testing.T) {
	ledgerEnv(t)
	for _, k := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "SESSION_SECRET", "PORT", "PUBLIC_URL", "ALLOWED_EMAIL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	runCmd(t, "seed", "--n", "1")
	if code, out, errOut := runCmd(t, "verify"); code != 0 {
		t.Fatalf("verify = %d %q %q", code, out, errOut)
	}
	// serve needs everything and names the missing keys without values
	code, _, errOut := runCmd(t, "serve")
	if code != 2 || !strings.Contains(errOut, "GOOGLE_CLIENT_ID is not set") || strings.Contains(errOut, testLedgerKey) {
		t.Fatalf("serve config error = %d %q", code, errOut)
	}
}

func TestVerifyMissingKey(t *testing.T) {
	ledgerEnv(t)
	t.Setenv("LEDGER_KEY", "")
	code, _, errOut := runCmd(t, "verify")
	if code != 2 || !strings.Contains(errOut, "LEDGER_KEY is not set") {
		t.Fatalf("= %d %q", code, errOut)
	}
}

func TestBackupDirs(t *testing.T) {
	cfg := config.Config{DataDir: "/srv/kira/data"}
	got := backupDirs(cfg.DataDir)
	if got[0] != filepath.Join("backup", "kira.db") || got[1] != "/srv/kira/backup/kira.db" {
		t.Fatalf("%v", got)
	}
}
