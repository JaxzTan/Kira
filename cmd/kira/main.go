// Command kira runs the Kira server.
//
//	kira serve            start the HTTP server (verifies the ledger first)
//	kira verify           recompute the ledger hash chain; exit 1 if broken
//	kira seed --n N       append N realistic entries (dev/backup drill helper)
//
// Configuration comes from .env in the working directory plus the
// environment (internal/config); e2e builds ignore .env. verify and seed
// need only DATA_DIR and LEDGER_KEY. The server listens on 127.0.0.1:PORT;
// set the optional HOST (e.g. 0.0.0.0) only if a non-local proxy must reach
// it, which requires an https PUBLIC_URL. Set TRUST_PROXY=1 behind
// `tailscale serve` so the /auth rate limit sees real client addresses.
//
// Ledger integrity files: DATA_DIR/head (last trusted chain tip, MAC'd) and
// DATA_DIR/ack (persisted acknowledgement). See api.Integrity.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/JaxzTan/kira/internal/api"
	"github.com/JaxzTan/kira/internal/auth"
	"github.com/JaxzTan/kira/internal/config"
	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
	"github.com/JaxzTan/kira/internal/store"
	"github.com/JaxzTan/kira/web"
)

const usage = `usage:
  kira serve            start the HTTP server
  kira verify           verify the ledger hash chain (exit 1 if broken)
  kira seed --n N       append N sample entries (refuses a non-empty ledger unless --force)
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		return cmdServe(ctx, stdout, stderr)
	case "verify":
		return cmdVerify(ctx, stdout, stderr)
	case "seed":
		return cmdSeed(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "kira: unknown command %q\n%s", args[0], usage)
	return 2
}

// newLogger writes JSON to w, or human-readable text when w is a terminal.
func newLogger(w io.Writer, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if f, ok := w.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return slog.New(slog.NewTextHandler(w, opts))
		}
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// ledgerDeps opens the DB, receipts store and ledger service for cfg.
type ledgerDeps struct {
	st    *store.Store
	rs    *receipts.Store
	svc   *ledger.Service
	integ *api.Integrity
}

// openLedger opens everything and restores the persisted acknowledgement
// (DATA_DIR/ack) before any verify can run.
func openLedger(ctx context.Context, cfg config.Config, log *slog.Logger) (*ledgerDeps, error) {
	if err := config.EnsureDataDir(cfg.DataDir); err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return nil, err
	}
	rs, err := receipts.New(cfg.ReceiptsDir())
	if err != nil {
		st.Close()
		return nil, err
	}
	svc, err := ledger.New(ledger.Config{Store: st, Key: cfg.LedgerKey, Receipts: rs})
	if err != nil {
		st.Close()
		return nil, err
	}
	integ := api.NewIntegrity(svc, cfg.LedgerKey, cfg.HeadPath(), cfg.AckPath(), log)
	integ.LoadAck()
	return &ledgerDeps{st: st, rs: rs, svc: svc, integ: integ}, nil
}

// ---- serve ----

func cmdServe(ctx context.Context, stdout, stderr io.Writer) int {
	// First: an e2e test build refuses to run unless KIRA_E2E=1, before it
	// reads any config or data (normal builds: always nil).
	if err := auth.E2EGuard(); err != nil {
		fmt.Fprintln(stderr, "kira:", err)
		return 2
	}
	log := newLogger(stdout, slog.LevelInfo)
	cfg, err := config.Load(config.ModeServe)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := serve(ctx, cfg, log); err != nil {
		log.Error("server stopped with error", "err", err)
		return 1
	}
	return 0
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	log.Info("starting kira", "config", cfg)
	for _, w := range cfg.Warnings {
		log.Warn(w)
	}
	d, err := openLedger(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer d.st.Close()

	// TR-7: verify on startup against the recorded head (and the restored
	// acknowledgement); a failure leaves the app read-only until the owner
	// acknowledges it.
	res, err := d.integ.Verify(ctx)
	if err != nil {
		return fmt.Errorf("startup verify: %w", err)
	}
	log.Info("ledger head", "head", ledger.FormatHeadID(ledger.Head{ID: res.HeadID, Hash: res.HeadHash}),
		"ok", res.OK, "acknowledged", res.Acknowledged)
	switch {
	case !res.OK && res.Acknowledged:
		log.Warn("ledger has acknowledged problems; writes stay enabled until a new problem appears")
	case !res.OK:
		log.Warn("ledger is BROKEN: writes are disabled until acknowledged in Settings")
	}

	a, err := auth.New(auth.Config{
		ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret,
		IssuerURL: cfg.OIDCIssuerURL, RedirectURL: cfg.RedirectURL(),
		AllowedEmail: cfg.AllowedEmail, AllowedSub: cfg.AllowedSub,
		SessionSecret: cfg.SessionSecret, Secure: cfg.Secure(), Logger: log,
		PublicOrigin: cfg.PublicOrigin(),
	}, d.st)
	if err != nil {
		return err
	}
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}
	srv, err := api.New(api.Options{
		Auth: a, Store: d.st, Ledger: d.svc, Receipts: d.rs, Integrity: d.integ, Static: dist, Logger: log,
		BackupDirs: backupDirs(cfg.DataDir), PublicOrigin: cfg.PublicOrigin(), TrustProxy: cfg.TrustProxy,
	})
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	hs := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // 10 MB receipt on a slow link
		WriteTimeout:      5 * time.Minute, // zip export with receipts
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "public_url", cfg.PublicURL)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("stopped")
	return nil
}

// backupDirs are the Litestream local file replica locations
// (deploy/litestream.local.yml writes ./backup/kira.db relative to the repo).
// last_backup_at = newest mtime inside the first that exists, else null.
func backupDirs(dataDir string) []string {
	return []string{
		filepath.Join("backup", "kira.db"),
		filepath.Join(filepath.Dir(dataDir), "backup", "kira.db"),
	}
}

// ---- verify ----

func cmdVerify(ctx context.Context, stdout, stderr io.Writer) int {
	cfg, err := config.Load(config.ModeLedger)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return verify(ctx, cfg, stdout, newLogger(stderr, slog.LevelWarn))
}

// verify prints "ok entries=N head=id:hash" (exit 0) or
// "BROKEN at id=N reason=... acknowledged=bool" (exit 1). Other failures
// exit 2. It reads DATA_DIR/head and DATA_DIR/ack but never writes them
// (safe to run next to a live server).
func verify(ctx context.Context, cfg config.Config, stdout io.Writer, log *slog.Logger) int {
	if _, err := os.Stat(cfg.DBPath()); err != nil {
		fmt.Fprintf(stdout, "error: no ledger database at %s\n", cfg.DBPath())
		return 2
	}
	d, err := openLedger(ctx, cfg, log)
	if err != nil {
		fmt.Fprintln(stdout, "error:", err)
		return 2
	}
	defer d.st.Close()
	res, err := d.integ.VerifyReadOnly(ctx)
	if err != nil {
		fmt.Fprintln(stdout, "error:", err)
		return 2
	}
	head := ledger.FormatHeadID(ledger.Head{ID: res.HeadID, Hash: res.HeadHash})
	if res.OK {
		fmt.Fprintf(stdout, "ok entries=%d head=%s\n", res.Entries, head)
		return 0
	}
	var id int64
	reason := "unknown"
	if res.BrokenAt != nil {
		id = *res.BrokenAt
	}
	if res.Reason != nil {
		reason = *res.Reason
	}
	fmt.Fprintf(stdout, "BROKEN at id=%d reason=%s entries=%d head=%s problems=%d acknowledged=%t\n",
		id, reason, res.Entries, head, len(res.Problems), res.Acknowledged)
	return 1
}
