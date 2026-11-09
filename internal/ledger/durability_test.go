package ledger

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JaxzTan/kira/internal/store"
)

const (
	envDurChild = "KIRA_LEDGER_DURABILITY_CHILD"
	envDurDB    = "KIRA_LEDGER_DURABILITY_DB"
	envDurKey   = "KIRA_LEDGER_DURABILITY_KEY" // test key only, never LEDGER_KEY
)

// TestDurabilityChild is the subprocess body: append forever, printing each
// id only after Append (i.e. COMMIT) returned. Skipped unless re-executed.
func TestDurabilityChild(t *testing.T) {
	if os.Getenv(envDurChild) != "1" {
		t.Skip("subprocess helper for TestDurabilityKill9")
	}
	key, err := hex.DecodeString(os.Getenv(envDurKey))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad key:", err)
		os.Exit(2)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, os.Getenv(envDurDB))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(2)
	}
	svc, err := New(Config{Store: st, Key: key, Location: time.UTC})
	if err != nil {
		fmt.Fprintln(os.Stderr, "new:", err)
		os.Exit(2)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	for i := 1; ; i++ {
		var r AppendResult
		if i%5 == 0 {
			r, err = svc.Amend(ctx, 1, input(today, int64(i), "amend"))
		} else {
			r, err = svc.Create(ctx, Input{OccurredOn: today, AmountSen: int64(i), CategoryID: 1, Payee: "durable", Method: MethodCash,
				Note: sp(strings.Repeat("n", 400))})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "append:", err)
			os.Exit(3)
		}
		// Unbuffered write straight to the pipe.
		fmt.Fprintf(os.Stdout, "committed %d %s\n", r.Row.ID, r.Row.Hash)
	}
}

// TestDurabilityKill9 re-execs the test binary as a writer, SIGKILLs it
// mid-stream (no clean shutdown, no Close), reopens the DB and checks every
// id the child reported as committed is present with the same hash and the
// chain verifies. Repeated so later rounds also exercise WAL recovery.
func TestDurabilityKill9(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGKILL semantics")
	}
	if testing.Short() {
		t.Skip("short")
	}
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "kira.db")
	committed := map[int64]string{}
	rounds := []struct {
		minCommits int
		extraDelay time.Duration
	}{{50, 0}, {200, 3 * time.Millisecond}, {25, 7 * time.Millisecond}}
	for ri, round := range rounds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDurabilityChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), envDurChild+"=1", envDurDB+"="+dbPath, envDurKey+"="+hex.EncodeToString(testKey))
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(out)
		n, killed := 0, false
		deadline := time.After(20 * time.Second)
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) != 3 || f[0] != "committed" {
				continue // go test chatter
			}
			id, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			committed[id] = f[2]
			n++
			if !killed {
				select {
				case <-deadline:
					t.Fatal("child too slow")
				default:
				}
				if n >= round.minCommits {
					time.Sleep(round.extraDelay)
					if err := cmd.Process.Kill(); err != nil { // SIGKILL
						t.Fatal(err)
					}
					killed = true
				}
			}
		}
		werr := cmd.Wait()
		if !killed {
			t.Fatalf("round %d: child exited before kill: %v", ri, werr)
		}
		if ws, ok := cmd.ProcessState.Sys().(interface{ Signaled() bool }); ok && !ws.Signaled() {
			t.Fatalf("round %d: child not killed by signal: %v", ri, werr)
		}

		st, err := store.Open(ctx, dbPath)
		if err != nil {
			t.Fatal(err)
		}
		svc, err := New(Config{Store: st, Key: testKey})
		if err != nil {
			t.Fatal(err)
		}
		res, err := svc.Verify(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK {
			t.Fatalf("round %d: verify after kill: %+v", ri, res)
		}
		var missing []int64
		for id, h := range committed {
			var got string
			if err := st.DB().QueryRow(`SELECT hash FROM entries WHERE id = ?`, id).Scan(&got); err != nil || got != h {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			t.Fatalf("round %d: %d committed rows lost/changed, e.g. %v", ri, len(missing), missing[:min(5, len(missing))])
		}
		if res.Entries < int64(len(committed)) {
			t.Fatalf("round %d: %d rows < %d committed", ri, res.Entries, len(committed))
		}
		t.Logf("round %d: %d reported committed, %d rows on disk, head %d", ri, len(committed), res.Entries, res.HeadID)
		st.Close()
	}
}
