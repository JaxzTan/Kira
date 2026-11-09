# Kira

Kira is a personal, single-user business expense recorder: a Go server with an embedded React UI, backed by one SQLite file. Sign in with one Google account, record expenses with receipts, export for your accountant.

Guarantees:
- **Append-only.** Entries are never edited or deleted; corrections are `amend` rows, removals are `void` rows.
- **Hash-chained and tamper-evident** (HMAC-SHA256 keyed by `LEDGER_KEY`). It detects tampering; it does not prevent it, and it cannot protect against someone who holds both your data and your key.
- **Money is integer sen**, never floats. `RM 1,234.56` is formatted only in the UI.

## Prerequisites

- Go 1.23+ and Node 20+
- `brew install litestream age` (`age` is optional, only for encrypted backups)
- `sqlite3` optional (handy for inspecting a restored copy)

## First-time setup

1. `bash scripts/bootstrap.sh` creates `.env` and generates `SESSION_SECRET` and `LEDGER_KEY`. Existing values are kept.
2. **Save `LEDGER_KEY` in a password manager now.**

   > **WARNING: back up `LEDGER_KEY`.** If you lose it you can no longer verify the hash chain (the data stays readable, but nothing can prove it is untampered). A backup of the database is not enough on its own. Never commit `.env`.

3. Google Cloud Console (https://console.cloud.google.com). If menu names differ, look for "OAuth consent screen" and "Credentials".
   1. Create a project named `Kira`.
   2. Google Auth Platform, Branding: fill in app name and your email.
   3. Audience: User type **External**, publishing status **Testing**, and add your Gmail under **Test users**.
   4. Clients, Create client, type **Web application**.
      - Authorized JavaScript origins: `http://localhost:5173` and `http://localhost:8080`
      - Authorized redirect URIs: `http://localhost:5173/auth/google/callback` and `http://localhost:8080/auth/google/callback`
      - Later, for Tailscale, also add `https://<machine>.<tailnet>.ts.net/auth/google/callback`
   5. Scopes: `openid`, `email`, `profile` (the defaults are enough).
   6. Copy the client ID and secret.
4. Fill `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` and `ALLOWED_EMAIL` in `.env`.
5. `make check-env` validates the file and never prints values.

## `.env` keys

| Key | Required | Meaning |
|---|---|---|
| `PORT` | yes | Listen port (default 8080). |
| `PUBLIC_URL` | yes | Where your browser reaches the app. Dev `http://localhost:5173`, prod `http://localhost:8080`, Tailscale `https://...ts.net`. https turns on `Secure` cookies. Also used for the CSRF origin check and the OAuth redirect. |
| `DATA_DIR` | yes | Database, receipts, `head` and `ack` files. Keep mode 700. |
| `HOST` | no | Listen host, default `127.0.0.1`. A non-loopback host is refused unless `PUBLIC_URL` is https. |
| `TRUST_PROXY` | no | Set `1` only behind exactly one trusted proxy (e.g. `tailscale serve`) so the `/auth` rate limiter uses `X-Forwarded-For`. |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | yes | From the OAuth client above. |
| `ALLOWED_EMAIL` | yes | The only Google account allowed in. |
| `ALLOWED_SUB` | no | Your Google user ID. If empty, the first successful login pins it automatically in the DB; a different account with the same email is then rejected. |
| `SESSION_SECRET` | yes | Cookie signing secret (generated). |
| `LEDGER_KEY` | yes | Signs the hash chain (generated). Back it up. |
| `LITESTREAM_REPLICA_URL` | no | Replica target, e.g. an `s3://` or R2 URL. Empty means a local replica in `./backup`. |
| `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY`, `LITESTREAM_ENDPOINT` | no | Credentials and endpoint for S3/R2. |
| `LITESTREAM_AGE_RECIPIENT` | no | age public key (`age1...`) to encrypt replicas. Keep the matching identity offline; restore needs it. Empty means unencrypted. |
| `RECEIPTS_SYNC_TARGET` | no | rsync target for `data/receipts`, e.g. `user@nas:/backups/kira-receipts`. Empty means `./backup/receipts`. |

## Run

- **Dev:** `make dev`, then open http://localhost:5173 (Vite on 5173 proxies to Go on 8080; `PUBLIC_URL=http://localhost:5173`).
- **Prod build:** `make build`, then `bin/kira serve` and open http://localhost:8080. Set `PUBLIC_URL=http://localhost:8080`.
- **Keyboard:** `N` starts a new entry, `Tab` moves through fields in order, `Enter` saves.
- `make help` lists all targets.

## Run as a service

### macOS (launchd)

Do not run the service from iCloud Drive. Sync can conflict with SQLite's WAL files and corrupt the database, and it uploads your key alongside your data. The installer copies a runtime to a local folder instead.

1. `make build`
2. `scripts/install-macos.sh` copies the binary, scripts and `.env` (mode 600) to `~/Library/Application Support/Kira` (override with `KIRA_HOME`) and renders the launchd agents from `deploy/`. Without `--load` it only prints the `launchctl` commands.
3. `scripts/install-macos.sh --load` starts the `com.kira.server` and `com.kira.litestream` agents.
4. Uninstall: `scripts/uninstall-macos.sh` (keeps data; `--purge` removes it too).

### Linux (systemd user units)

Install `deploy/kira.service` and `deploy/litestream.service` as described in the comments at the top of each file, then `systemctl --user enable --now kira litestream` and `loginctl enable-linger $USER`.

### Remote access with Tailscale

1. `tailscale serve --bg 8080` (keep `HOST` on loopback).
2. In `.env`: `PUBLIC_URL=https://<machine>.<tailnet>.ts.net` and `TRUST_PROXY=1`.
3. Add `https://<machine>.<tailnet>.ts.net/auth/google/callback` as a redirect URI in Google Cloud Console.
4. Restart Kira.

## Backups and restore

- **Database:** Litestream replicates continuously to R2/S3 (`LITESTREAM_REPLICA_URL`) or to `./backup` when empty. Set `LITESTREAM_AGE_RECIPIENT` to encrypt; store the age identity offline.
- **Receipts:** `scripts/receipts-sync.sh` copies `data/receipts` to `RECEIPTS_SYNC_TARGET` (or `./backup/receipts`). Run it nightly: it is not scheduled automatically; see the timer/launchd examples in the script header.
- **Drill:** `make backup-test` creates temp data, replicates, restores to a new path, runs `kira verify` and compares counts.

Restore:
1. Stop Kira.
2. `litestream restore -o "$DATA_DIR/kira.db" <replica-url>` (use the same URL as `LITESTREAM_REPLICA_URL`; for a local replica use `file://` plus the path of `./backup`). Encrypted replicas need your age identity.
3. Restore receipts from the receipts backup into `DATA_DIR/receipts`.
4. Run `bin/kira verify`.

A restored DB has no `head` or `ack` file, so the first verify reports **"head missing"** and the app shows the red banner. That is expected: the head file is a separate record that detects tail deletion, and it cannot be recreated from a database alone. After you have confirmed the entry count looks right, press **Acknowledge** once in Settings. The head file is then rewritten and normal operation resumes.

Run the drill quarterly (NFR-4). Keep records for at least 7 years for LHDN (NFR-5).

## Integrity

`bin/kira verify` (also `make verify`, and at every startup) checks:
- the hash chain, recomputed with `LEDGER_KEY`
- gaps in entry ids
- the `DATA_DIR/head` file (`id:hash:mac`) which only moves forward, so deleted tail rows are noticed
- the SHA-256 of every referenced receipt file ("receipt mismatch" / "receipt missing")

Output is `ok entries=N head=...` with exit 0, or the first problem with exit 1.

A **red banner** means verify failed and writes are blocked (the API answers 423). **Acknowledge** (Settings) accepts the current state: it is stored permanently in the append-only `integrity_events` table and in `DATA_DIR/ack`, so it survives restarts. A later verify whose problems are a subset of the acknowledged ones stays acknowledged; any new problem blocks writes again.

Honest limits: this is tamper-evident, not tamper-proof. Someone with both `LEDGER_KEY` and write access can rewrite history consistently. Whole-database rollback to an older backup is only caught through the head file.

## Testing

- `make check`: gofmt, vet, Go tests, web typecheck, build and tests.
- `make e2e`: Playwright against `bin/kira-e2e` (built with `-tags e2e`; `KIRA_E2E=1` is set for you). Never run `bin/kira-e2e` against real data; it refuses to start without `KIRA_E2E=1` and ignores `.env`.
- `make backup-test`: the backup drill above.

## Troubleshooting

| Symptom | Cause, fix |
|---|---|
| `make check-env` shows ✗ for keys you filled | Duplicate blank keys were appended (e.g. by a re-run). Keep exactly one line per key. |
| `redirect_uri_mismatch` | The URI in Google Console must exactly match `PUBLIC_URL` + `/auth/google/callback` (scheme, host, port). |
| 403 "not allowed" at login | Wrong Google account, not added as a Test user, or a different `sub` than the pinned one (`ALLOWED_SUB` or the DB `owner` row). |
| 403 "cross-site request" | State-changing requests need `Sec-Fetch-Site: same-origin` or an `Origin` equal to `PUBLIC_URL`. Open the app at exactly `PUBLIC_URL`. |
| 423 ledger broken | Verify failed. Run `bin/kira verify`; if the cause is understood (e.g. fresh restore), Acknowledge in Settings. |
| Odd SQLite errors or conflict copies of `kira.db-wal` | `DATA_DIR` is inside iCloud Drive. Use the macOS install above. |
| `litestream: command not found` | `brew install litestream`. |
| Port already in use | Another Kira or process on 8080/5173; stop it or change `PORT` (and `PUBLIC_URL`, redirect URIs). |

## Layout and docs

- `cmd/kira/` CLI (`serve`, `verify`, `seed`); `internal/` api, auth, config, ledger, store, receipts, export; `migrations/`
- `web/` React + Vite UI (embedded into the binary); `e2e/` Playwright
- `deploy/` systemd and launchd units, Litestream config; `scripts/` bootstrap, check-env, install, backup helpers
- Docs: `docs/PRD-kira.md`, `docs/TRD-kira.md`, `docs/api-contract.md`, `design/README.md`, `PLAN.md`
