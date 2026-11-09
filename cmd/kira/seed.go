package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/JaxzTan/kira/internal/config"
	"github.com/JaxzTan/kira/internal/ledger"
)

// cmdSeed appends N sample entries through ledger.Service (the only write
// path). Used by scripts/backup-test.sh and for local demos.
func cmdSeed(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 20, "number of entries to append")
	force := fs.Bool("force", false, "append even if the ledger already has entries")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *n < 1 || *n > 100000 {
		fmt.Fprintln(stderr, "kira seed: --n must be between 1 and 100000")
		return 2
	}
	cfg, err := config.Load(config.ModeLedger)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return seed(ctx, cfg, *n, *force, stdout, stderr)
}

type sample struct {
	payee  string
	cat    string
	method string
	minSen int64
	maxSen int64
	note   string
}

var samples = []sample{
	{"Cloudflare", "Cloud & hosting", ledger.MethodCard, 2000, 9000, "R2 + domain"},
	{"DigitalOcean", "Cloud & hosting", ledger.MethodCard, 2500, 12000, "Droplet"},
	{"GitHub Copilot", "Software & subscriptions", ledger.MethodCard, 4400, 4600, "Monthly plan"},
	{"JetBrains", "Software & subscriptions", ledger.MethodCard, 3000, 9000, ""},
	{"Lazada", "Hardware", ledger.MethodCard, 5000, 80000, "USB-C hub"},
	{"Unifi Fibre", "Internet & phone", ledger.MethodBankTransfer, 12900, 14900, "Home fibre"},
	{"Grab", "Travel & transport", ledger.MethodGrabPay, 800, 4500, "Client meeting"},
	{"Touch 'n Go RFID", "Travel & transport", ledger.MethodTNGEWallet, 500, 3000, "Tolls"},
	{"Village Park", "Meals & entertainment", ledger.MethodCash, 1200, 6000, "Lunch with client"},
	{"SSM", "Professional fees", ledger.MethodBankTransfer, 5000, 30000, "Annual return"},
	{"Canva", "Marketing", ledger.MethodCard, 5000, 6000, ""},
	{"Popular Bookstore", "Office", ledger.MethodCash, 500, 5000, "Stationery"},
}

func seed(ctx context.Context, cfg config.Config, n int, force bool, stdout, stderr io.Writer) int {
	log := newLogger(stderr, slog.LevelWarn)
	d, err := openLedger(ctx, cfg, log)
	if err != nil {
		fmt.Fprintln(stderr, "kira seed:", err)
		return 2
	}
	defer d.st.Close()
	head, err := d.svc.Head(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "kira seed:", err)
		return 2
	}
	if head.ID > 0 && !force {
		fmt.Fprintf(stderr, "kira seed: ledger already has %d rows; refusing (use --force to append anyway)\n", head.ID)
		return 1
	}
	// Writes are only allowed on a verified ledger, same as the server. The
	// append hook (api.Integrity) advances DATA_DIR/head per entry.
	if res, err := d.integ.Verify(ctx); err != nil || !res.OK {
		fmt.Fprintln(stderr, "kira seed: ledger does not verify; refusing to write")
		return 1
	}
	cats, err := d.st.ListCategories(ctx, false)
	if err != nil || len(cats) == 0 {
		fmt.Fprintln(stderr, "kira seed: no categories")
		return 2
	}
	catID := map[string]int64{}
	for _, c := range cats {
		catID[c.Name] = c.ID
	}
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 42))
	today := time.Now()
	var last ledger.AppendResult
	for i := 0; i < n; i++ {
		s := samples[rng.IntN(len(samples))]
		id, ok := catID[s.cat]
		if !ok {
			id = cats[rng.IntN(len(cats))].ID
		}
		in := ledger.Input{
			OccurredOn: today.AddDate(0, 0, -rng.IntN(180)).Format("2006-01-02"),
			AmountSen:  s.minSen + rng.Int64N(s.maxSen-s.minSen+1),
			CategoryID: id,
			Payee:      s.payee,
			Method:     s.method,
		}
		if s.note != "" {
			note := s.note
			in.Note = &note
		}
		if last, err = d.svc.Create(ctx, in); err != nil {
			fmt.Fprintf(stderr, "kira seed: entry %d: %v\n", i+1, err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "seeded %d entries head=%s\n", n, ledger.FormatHeadID(ledger.Head{ID: last.Row.ID, Hash: last.Row.Hash}))
	return 0
}
