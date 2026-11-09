// Package config loads Kira's settings from `.env` (if present) and the
// process environment. Real environment variables always win over `.env`.
//
// Errors name the offending key but never include its value, so they are
// safe to print and log (CLAUDE.md rule 7).
//
// Optional keys:
//
//	HOST          listen address (default 127.0.0.1). A non-loopback HOST is
//	              refused while PUBLIC_URL is http:// (session cookies would
//	              cross the network in clear text).
//	TRUST_PROXY   "1" trusts X-Forwarded-For from a loopback peer for the
//	              /auth rate limit. Set it when a local reverse proxy such as
//	              `tailscale serve` fronts Kira; otherwise every client behind
//	              the proxy shares one bucket. Default off.
//	OIDC_ISSUER_URL  honoured only in -tags e2e builds (fake IdP); a normal
//	              build ignores it with a warning so Google is always used.
//
// E2E builds (-tags e2e) never read .env: the tests pass every variable.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	Host                 string // listen host; default 127.0.0.1
	TrustProxy           bool   // TRUST_PROXY=1: honour X-Forwarded-For from a loopback peer

	// EnvFile is the absolute path of the .env file that was loaded ("" if none).
	EnvFile string
	// Warnings are non-fatal problems to log at startup (never contain values).
	Warnings []string
}

// iCloudWarning is logged when data or .env live in iCloud Drive.
const iCloudWarning = "data is in iCloud Drive; run scripts/install-macos.sh"

// DBPath is DATA_DIR/kira.db.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "kira.db") }

// ReceiptsDir is DATA_DIR/receipts.
func (c Config) ReceiptsDir() string { return filepath.Join(c.DataDir, "receipts") }

// HeadPath is DATA_DIR/head (last recorded ledger head, "id:hash:mac").
func (c Config) HeadPath() string { return filepath.Join(c.DataDir, "head") }

// AckPath is DATA_DIR/ack (persisted ledger acknowledgement, mode 0600).
func (c Config) AckPath() string { return filepath.Join(c.DataDir, "ack") }

// PublicOrigin is scheme://host[:port] of PUBLIC_URL (lower-case, no path),
// the value browsers send in the Origin header. "" if PUBLIC_URL is unset.
func (c Config) PublicOrigin() string {
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

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
		slog.String("host", c.Host),
		slog.Bool("trust_proxy", c.TrustProxy),
		slog.String("env_file", c.EnvFile),
	)
}

// Load reads `.env` from the working directory (if it exists, without
// overriding variables already set in the environment), then builds the
// Config from the environment. E2E builds skip .env entirely.
func Load(mode Mode) (Config, error) {
	envFile := ""
	if !e2eBuild {
		err := godotenv.Load(".env")
		switch {
		case err == nil:
			if abs, aerr := filepath.Abs(".env"); aerr == nil {
				envFile = abs
			} else {
				envFile = ".env"
			}
		case !errors.Is(err, os.ErrNotExist):
			// Do not include err: a parse error can quote the offending line.
			return Config{}, errors.New("config: .env could not be parsed (check its syntax with `make check-env`)")
		}
	}
	c, err := FromLookup(mode, os.LookupEnv)
	if err != nil {
		return c, err
	}
	c.EnvFile = envFile
	if envFile != "" && InICloud(envFile) {
		c.Warnings = append(c.Warnings, iCloudWarning+" (.env is under ~/Library/Mobile Documents)")
	}
	return c, nil
}

// InICloud reports whether path (or the directory it resolves to) is inside
// iCloud Drive (~/Library/Mobile Documents). SQLite WAL files and the
// ledger head must not be synced by iCloud.
func InICloud(path string) bool {
	const marker = "/Library/Mobile Documents/"
	candidates := []string{path}
	if abs, err := filepath.Abs(path); err == nil {
		candidates = append(candidates, abs)
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			candidates = append(candidates, real)
		}
	}
	for _, p := range candidates {
		if strings.Contains(filepath.ToSlash(p)+"/", marker) {
			return true
		}
	}
	return false
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
		if iss := get("OIDC_ISSUER_URL"); iss != "" {
			if e2eBuild {
				c.OIDCIssuerURL = iss
			} else {
				c.Warnings = append(c.Warnings, "OIDC_ISSUER_URL is set but ignored: only e2e test builds use a custom issuer; Google is used")
			}
		}
		c.LitestreamReplicaURL = get("LITESTREAM_REPLICA_URL")

		c.Host = get("HOST")
		if c.Host == "" {
			c.Host = "127.0.0.1"
		}
		if strings.ContainsAny(c.Host, "/ ") {
			problems = append(problems, "HOST must be a host name or IP address")
		} else if !isLoopbackHost(c.Host) && strings.HasPrefix(strings.ToLower(c.PublicURL), "http://") {
			problems = append(problems, "HOST is not a loopback address while PUBLIC_URL is http://; use https (e.g. tailscale serve) or bind HOST=127.0.0.1")
		}
		switch strings.ToLower(get("TRUST_PROXY")) {
		case "", "0", "false", "no":
		case "1", "true", "yes":
			c.TrustProxy = true
		default:
			problems = append(problems, "TRUST_PROXY must be 1 or 0")
		}
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	if InICloud(c.DataDir) {
		c.Warnings = append(c.Warnings, iCloudWarning+" (DATA_DIR is under ~/Library/Mobile Documents)")
	}
	return c, nil
}

// isLoopbackHost: "localhost" or a loopback IP literal (brackets allowed).
func isLoopbackHost(h string) bool {
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
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
