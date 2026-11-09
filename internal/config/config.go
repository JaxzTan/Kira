// Package config loads Kira's settings from `.env` (if present) and the
// process environment. Real environment variables always win over `.env`.
//
// Errors name the offending key but never include its value, so they are
// safe to print and log (CLAUDE.md rule 7).
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Mode selects which keys are required.
type Mode int

const (
	// ModeServe needs every key (HTTP server + Google login).
	ModeServe Mode = iota
	// ModeLedger needs only DATA_DIR and LEDGER_KEY (kira verify, kira seed),
	// so it works before Google credentials exist.
	ModeLedger
)

// Config is the validated runtime configuration.
type Config struct {
	Port                 int
	PublicURL            string // no trailing slash
	DataDir              string // absolute
	GoogleClientID       string
	GoogleClientSecret   string
	AllowedEmail         string
	AllowedSub           string // optional
	OIDCIssuerURL        string // optional; empty = Google (tests point it elsewhere)
	LitestreamReplicaURL string // optional
	SessionSecret        []byte // 32 bytes
	LedgerKey            []byte // 32 bytes
}

// DBPath is DATA_DIR/kira.db.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "kira.db") }

// ReceiptsDir is DATA_DIR/receipts.
func (c Config) ReceiptsDir() string { return filepath.Join(c.DataDir, "receipts") }

// HeadPath is DATA_DIR/head (last recorded ledger head, "id:hash").
func (c Config) HeadPath() string { return filepath.Join(c.DataDir, "head") }

// Secure reports whether cookies should carry the Secure flag.
func (c Config) Secure() bool { return strings.HasPrefix(c.PublicURL, "https://") }

// RedirectURL is the OAuth callback registered with Google.
func (c Config) RedirectURL() string { return c.PublicURL + "/auth/google/callback" }

// LogValue keeps secrets out of logs even if a Config is logged whole.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("port", c.Port),
		slog.String("public_url", c.PublicURL),
		slog.String("data_dir", c.DataDir),
		slog.Bool("allowed_sub_set", c.AllowedSub != ""),
		slog.Bool("custom_issuer", c.OIDCIssuerURL != ""),
		slog.Bool("remote_replica", c.LitestreamReplicaURL != ""),
	)
}

// Load reads `.env` from the working directory (if it exists, without
// overriding variables already set in the environment), then builds the
// Config from the environment.
func Load(mode Mode) (Config, error) {
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Do not include err: a parse error can quote the offending line.
		return Config{}, errors.New("config: .env could not be parsed (check its syntax with `make check-env`)")
	}
	return FromLookup(mode, os.LookupEnv)
}

// FromLookup builds and validates a Config using lookup (os.LookupEnv in
// production; a map in tests). All problems are reported at once.
func FromLookup(mode Mode, lookup func(string) (string, bool)) (Config, error) {
	get := func(k string) string {
		v, _ := lookup(k)
		return strings.TrimSpace(v)
	}
	var problems []string
	need := func(k string) string {
		v := get(k)
		if v == "" {
			problems = append(problems, k+" is not set")
		}
		return v
	}
	var c Config

	if dir := need("DATA_DIR"); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			problems = append(problems, "DATA_DIR is not a usable path")
		}
		c.DataDir = abs
	}
	c.LedgerKey = hexKey("LEDGER_KEY", need("LEDGER_KEY"), &problems)

	if mode == ModeServe {
		if p := need("PORT"); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				problems = append(problems, "PORT must be a number between 1 and 65535")
			}
			c.Port = n
		}
		if u := need("PUBLIC_URL"); u != "" {
			pu, err := url.Parse(u)
			if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
				problems = append(problems, "PUBLIC_URL must be an http:// or https:// URL")
			}
			c.PublicURL = strings.TrimRight(u, "/")
		}
		c.GoogleClientID = need("GOOGLE_CLIENT_ID")
		c.GoogleClientSecret = need("GOOGLE_CLIENT_SECRET")
		if e := need("ALLOWED_EMAIL"); e != "" {
			if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
				problems = append(problems, "ALLOWED_EMAIL must be a plain email address")
			}
			c.AllowedEmail = e
		}
		c.SessionSecret = hexKey("SESSION_SECRET", need("SESSION_SECRET"), &problems)
		c.AllowedSub = get("ALLOWED_SUB")
		c.OIDCIssuerURL = get("OIDC_ISSUER_URL")
		c.LitestreamReplicaURL = get("LITESTREAM_REPLICA_URL")
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return c, nil
}

// hexKey decodes a 64-hex-char secret. The value never appears in errors.
func hexKey(name, v string, problems *[]string) []byte {
	if v == "" {
		return nil
	}
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != 32 {
		*problems = append(*problems, name+" must be 64 hex characters (32 bytes); generate one with `openssl rand -hex 32`")
		return nil
	}
	return b
}

// EnsureDataDir creates dir (and parents) and forces mode 0700 (TR-12).
func EnsureDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: create DATA_DIR: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("config: chmod DATA_DIR: %w", err)
	}
	return nil
}
