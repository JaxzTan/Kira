package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/config"
	"github.com/JaxzTan/kira/internal/ledger"
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

	if _, err := ledger.ParseHeadFile(testKey(t), string(head)); err != nil {
		t.Fatalf("head file not MAC'd: %v", err)
	}

	// Simulate deleted tail rows: recorded head is ahead of the DB.
	writeHead(t, dir, ledger.Head{ID: 30, Hash: strings.Repeat("e", 64)})
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
	t.Setenv("KIRA_E2E", "1") // get past the e2e guard in -tags e2e runs
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

func testKey(t *testing.T) []byte {
	t.Helper()
	k, err := hex.DecodeString(testLedgerKey)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func writeHead(t *testing.T, dir string, hd ledger.Head) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "head"), []byte(ledger.FormatHeadFile(testKey(t), hd)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyHeadFileProblems(t *testing.T) {
	dir := ledgerEnv(t)
	if code, _, e := runCmd(t, "seed", "--n", "3"); code != 0 {
		t.Fatal(e)
	}
	headPath := filepath.Join(dir, "head")
	good, _ := os.ReadFile(headPath)
	legacy := strings.Join(strings.Split(strings.TrimSpace(string(good)), ":")[:2], ":") + "\n"
	tests := []struct {
		name    string
		content *string
		want    string
	}{
		{"missing", nil, "BROKEN at id=3 reason=head missing"},
		{"garbage", ptr("nope"), "BROKEN at id=3 reason=head invalid"},
		{"legacy id:hash", &legacy, "BROKEN at id=3 reason=head invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.content == nil {
				os.Remove(headPath)
			} else if err := os.WriteFile(headPath, []byte(*tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			code, out, _ := runCmd(t, "verify")
			if code != 1 || !strings.HasPrefix(out, tt.want) || !strings.Contains(out, "acknowledged=false") {
				t.Fatalf("verify = %d %q", code, out)
			}
			// kira verify never writes the head file
			got, err := os.ReadFile(headPath)
			if tt.content == nil && err == nil {
				t.Fatal("verify recreated the head file")
			}
			if tt.content != nil && string(got) != *tt.content {
				t.Fatalf("verify rewrote the head file: %q", got)
			}
		})
	}
}

func ptr(s string) *string { return &s }

// kira verify reads DATA_DIR/ack (reports acknowledged=true) but never changes it.
func TestVerifyReadsAckWithoutChangingIt(t *testing.T) {
	dir := ledgerEnv(t)
	if code, _, e := runCmd(t, "seed", "--n", "2"); code != 0 {
		t.Fatal(e)
	}
	writeHead(t, dir, ledger.Head{ID: 9, Hash: strings.Repeat("e", 64)})

	cfg, err := config.Load(config.ModeLedger)
	if err != nil {
		t.Fatal(err)
	}
	log := newLogger(io.Discard, slog.LevelWarn)
	d, err := openLedger(context.Background(), cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := d.integ.Verify(context.Background()); err != nil || res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if res, err := d.integ.Acknowledge(context.Background()); err != nil || !res.Acknowledged {
		t.Fatalf("ack %+v %v", res, err)
	}
	d.st.Close()

	ackPath := filepath.Join(dir, "ack")
	before, err := os.Stat(ackPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, _ := os.ReadFile(ackPath)
	code, out, _ := runCmd(t, "verify")
	if code != 1 || !strings.Contains(out, "reason=head mismatch") || !strings.Contains(out, "acknowledged=true") {
		t.Fatalf("verify = %d %q", code, out)
	}
	after, err := os.Stat(ackPath)
	afterBytes, _ := os.ReadFile(ackPath)
	if err != nil || !os.SameFile(before, after) || !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("kira verify changed the ack file")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// serveEnv sets a complete throwaway serve environment (never the repo .env).
func serveEnv(t *testing.T, dataDir string) int {
	t.Helper()
	t.Chdir(t.TempDir())
	port := freePort(t)
	env := map[string]string{
		"PORT": strconv.Itoa(port), "PUBLIC_URL": "http://localhost:" + strconv.Itoa(port), "DATA_DIR": dataDir,
		"GOOGLE_CLIENT_ID": "x.apps.googleusercontent.com", "GOOGLE_CLIENT_SECRET": "dummy",
		"ALLOWED_EMAIL": "owner@example.com", "SESSION_SECRET": strings.Repeat("ab", 32), "LEDGER_KEY": testLedgerKey,
		"KIRA_E2E": "1", "HOST": "", "TRUST_PROXY": "", "OIDC_ISSUER_URL": "",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	return port
}

func TestServeStartsAndStops(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "Library", "Mobile Documents", "data") // trips the iCloud warning
	port := serveEnv(t, dataDir)
	ctx, cancel := context.WithCancel(context.Background())
	var out, errb bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"serve"}, &out, &errb) }()

	url := "http://127.0.0.1:" + strconv.Itoa(port)
	var resp *http.Response
	for i := 0; i < 100; i++ {
		var err error
		if resp, err = http.Get(url + "/api/me"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp == nil {
		cancel()
		t.Fatalf("server did not start: %s", errb.String())
	}
	resp.Body.Close()
	if resp.StatusCode != 401 || resp.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatalf("GET /api/me = %d %v", resp.StatusCode, resp.Header)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("serve exit %d: %s %s", code, out.String(), errb.String())
	}
	logs := out.String()
	for _, want := range []string{`"msg":"ledger head"`, `"head":"0:` + ledger.GenesisHash, "data is in iCloud Drive; run scripts/install-macos.sh"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, testLedgerKey) || strings.Contains(logs, strings.Repeat("ab", 32)) {
		t.Fatal("secret in logs")
	}
	// startup verify recorded the (empty) head with a MAC
	b, err := os.ReadFile(filepath.Join(dataDir, "head"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.ParseHeadFile(testKey(t), string(b)); err != nil {
		t.Fatal(err)
	}
}

func TestServeRefusesNonLoopbackHTTP(t *testing.T) {
	serveEnv(t, filepath.Join(t.TempDir(), "data"))
	t.Setenv("HOST", "0.0.0.0")
	code, _, errOut := runCmd(t, "serve")
	if code != 2 || !strings.Contains(errOut, "HOST is not a loopback address") {
		t.Fatalf("= %d %q", code, errOut)
	}
}
