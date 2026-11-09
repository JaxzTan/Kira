# Kira build plan (live board)

| Phase | Status | Runs in parallel | Gate |
|---|---|---|---|
| 0 · Scaffold + API contract | ✅ done | main only | — |
| 1 · Core build | ✅ done | kira-ledger · kira-auth · kira-frontend · kira-ops | — |
| 2 · Wire API + real frontend | ✅ done | kira-api → kira-frontend | — |
| 🛑 A · Google OAuth credentials | 🟦 waiting on Jaxz | — | **Jaxz** |
| 3 · Real login + E2E | ⬜ todo | kira-tester (+ fixes) | after A |
| 4 · Review + hardening | ✅ done (early, during Gate A) | kira-reviewer (+ agency reviewers if installed) | — |
| 🛑 B · Backup target (optional) | ✅ local backup | — | **Jaxz** |
| 5 · Ops + restore drill + README | ⬜ todo | kira-ops | after B or local fallback |

Legend: ⬜ todo · 🟦 in progress · ✅ done · ⛔ blocked

---

## Phase 0 · Scaffold + API contract (main session)
- [x] Read docs/, design/, CLAUDE.md; post a 10-line summary of what will be built
- [x] `go mod init github.com/JaxzTan/kira`; folder skeleton per CLAUDE.md layout
- [x] Vite React TS + Tailwind in `web/`; Tailwind theme from `design/tokens.css`
  - Tailwind v4 `@theme` in `web/src/index.css`; fonts self-hosted via @fontsource (NFR-8: no third-party requests). `web/embed.go` exposes `web.Dist`.
- [x] Write `docs/api-contract.md` (every endpoint in TRD §5: request, response, errors)
  - Additions derived from TRD: `GET /api/entries/:id`, `POST /api/ledger/acknowledge` (TR-7), `PATCH /api/categories/:id`. Lists include voided rows (design shows them struck through); totals exclude them.
- [x] Write matching `internal/api/dto.go` and `web/src/api/types.ts`
- [x] `make check` skeleton passes; commit `chore: scaffold`

## Phase 1 · Core build (4 subagents in parallel)
**kira-ledger** — FR-3, FR-4, FR-5, FR-13, NFR-2, NFR-3
- [x] migrations + triggers (entries, auth_log append-only; owner insert-once; categories; sessions)
  - `migrations/` embeds `0001_init.sql` (schema, triggers, `v_current`) + `0002_seed_categories.sql`. Found and fixed: `INSERT OR REPLACE` gets past a BEFORE DELETE trigger, so there are also `*_no_replace` BEFORE INSERT triggers on entries/owner/auth_log.
- [x] store: open DB with PRAGMAs, migrate, seed categories
  - `store.Open(ctx, path)`: WAL, synchronous=FULL, foreign_keys, busy_timeout=5000, fullfsync (macOS), `_txlock=immediate`; dir 0700 / db 0600. Has the `auth.Store` method set and the category CRUD (case-insensitive unique names).
- [x] ledger.Service: Append(create/amend/void), Current view, History, Verify
  - Also List/Summary/ExportEntries/Get over `v_current`, validation helpers + sentinel errors, `Head()`, TR-7 block/`Acknowledge()`, `VerifyAgainst(head)`. Canonical hash form documented in `internal/ledger/doc.go`, locked by a golden vector checked against an independent Python HMAC.
- [x] tamper tests: raw-SQL edit, dropped trigger, deleted tail → Verify fails at right id
  - 16 cases (amount, receipt swap, void reason, kind, prev_hash, forged row, middle delete → id gap, tail delete → head mismatch, wrong key, …).
- [x] durability test: kill mid-write, reopen, all committed rows present
  - 3 rounds of re-exec + SIGKILL on the same DB; every reported id/hash is present and Verify is ok.
- [x] receipts store (sha256 names, type/size limits, EXIF GPS strip) — FR-6
  - JPEG/PNG re-encoded (EXIF orientation applied first); PDF stored unchanged. Atomic write + fsync; `VerifyFile` re-hashes the file to catch a swapped receipt.
- [x] export CSV + zip — FR-12

**kira-auth** — FR-1, FR-2, NFR-1
- [x] OIDC login/callback with state + PKCE + nonce
  - `auth.New(cfg, store)`; OIDC discovery is lazy (first login) so the server starts even if Google is unreachable. `kira_oauth` HMAC-signed, Path=/auth/google, 10 min, single-use.
- [x] allow-list (email_verified, email, pinned sub) + auth_log
- [x] sessions (hashed token, cookie flags, 30-day expiry, logout)
  - /api/me name/picture: always null (sessions table has no profile columns).
- [x] middleware re-checking allow-list per request
- [x] tests with fake OIDC provider (httptest JWKS): wrong email, unverified, wrong sub, expired, bad sig, bad state
  - Also: wrong aud/iss, nonce missing/mismatch, tampered/expired state cookie, PKCE enforced by fake token endpoint, no secrets in logs. 50 pass (default), 53 (`-tags e2e`).
- [x] `/auth/test-login` behind `//go:build e2e` + test proving normal build lacks it

**kira-frontend** — FR-7, FR-10, FR-11 UI, NFR-7 (against mock API)
- [x] shared components from design/README.md
- [x] Login, Dashboard, Entries + QuickAdd, Entry detail (match design/screens/*.png)
- [x] Export + Settings pages (categories, ledger status) in same style
- [x] keyboard: `N` shortcut, quick-add Tab order, Enter saves
- [x] component tests (Vitest) for QuickAdd and money formatting
  - Built against `VITE_MOCK_API=1` (`npm run dev` in web/ with that env). 43 tests pass; `npm run typecheck && npm run build` pass. Real client in `web/src/api/client.ts` is ready for Phase 2 wiring.

**kira-ops**
- [x] Makefile targets (CLAUDE.md list)
- [x] `deploy/kira.service`, `deploy/litestream.yml` (env-driven; local file replica fallback)

## Phase 2 · Wire API + real frontend
- [x] kira-api: config loading + validation, router, all handlers per contract, rate limit on /auth
  - `internal/config`: `.env` via godotenv (never overrides real env) + env; `ModeServe` needs all keys, `ModeLedger` (verify/seed) only DATA_DIR + LEDGER_KEY. Errors list every bad key, never values; `.env` parse errors are not quoted. DATA_DIR forced to 0700.
  - `internal/api`: chi router. `/auth/*` = `auth.Routes` + 404 catch-all behind a 10/min/IP token bucket (429 `rate_limited`); IP = RemoteAddr host, X-Forwarded-For (right-most) trusted only from a loopback peer. `/api/*` = `auth.Middleware` → body limits (64 KB JSON, 11 MB multipart) + Content-Type rule (415; `/api/ledger/acknowledge` exempt) → read-only guard (423 for every POST/PATCH except acknowledge while broken+unacked). Strict JSON (unknown fields / trailing data → 400; wrong JSON type → 422 with the field). Everything else = embedded web/dist with SPA fallback (`/assets/*` immutable 1 y, rest no-cache; missing files with an extension 404). CSP/nosniff/Referrer-Policy/X-Frame-Options on all responses; receipts get a stricter CSP that still lets Chrome render PDFs.
  - `DATA_DIR/head` (`internal/api/head.go`): written atomically (0600) after each ok verify, each append that directly extends it, and on acknowledge; never by a failed verify, so a tail deletion stays reported across restarts. `last_backup_at` = newest mtime under `./backup/kira.db` or `DATA_DIR/../backup/kira.db` (Litestream local replica), else null (e.g. remote-only replica).
  - Tests: 34 top-level / 126 with subtests (api, config, cmd), all httptest against real store + ledger + receipts and the real auth middleware with memstore sessions.
- [x] kira-api: `bin/kira serve` and `bin/kira verify` subcommands; startup verify + head log
  - `kira serve`: startup `VerifyRecorded` (INFO `head=id:hash`, WARN on failure, read-only until acknowledged), listens on `127.0.0.1:PORT` (optional `HOST` override), slog JSON (text on a TTY), graceful SIGINT/SIGTERM shutdown, Read/Write/Idle/Header timeouts. `kira verify`: `ok entries=N head=id:hash` / `BROKEN at id=N reason=...` exit 1 (2 = config/DB error). `kira seed --n N [--force]` for backup-test.sh (refuses a non-empty or broken ledger).
  - Smoke (temp env, fake Google keys): `/api/me` 401 JSON, `/` 200 index.html, `POST /auth/test-login` 404, `/entries` 200 index.html, `/assets/*.js` immutable cache, `kira verify` → `ok entries=0 head=0:000…` exit 0.
- [x] kira-frontend: switch from mock to real API, error states, 401 → /login
  - Client: no Content-Type on bodyless POSTs, multipart for receipts, ApiError parsing, 401 → /login (no loop on /login), 403 → "This account is not allowed" screen, 423 from any write → re-verify (red banner), 429 friendly msg, network failure → inline error with Retry. Null arrays normalized. Vite `assetsInlineLimit: 0` (inlined data: fonts violated the CSP). 57 tests. Real-server walkthrough (bin/kira-e2e, headless Chromium) passed with zero CSP violations.
- [x] `make check` green; commit `feat: core app`

## 🛑 Gate A · Google OAuth credentials (Jaxz)
Claude posts the exact Google Cloud Console steps, then waits for **"creds ready"**.
- [ ] Jaxz filled GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ALLOWED_EMAIL in `.env`
- [ ] `make check-env` ✓

## Phase 3 · Real login + E2E
- [ ] Jaxz logs in once with real Google (Claude asks him to, then reads server logs) → owner sub pinned
- [ ] Jaxz tries a second Google account → 403 confirmed
- [x] kira-tester: Playwright (`-tags e2e` build): login → add → amend → void → filter → export → verify
  - `e2e/` (config, `global-setup.ts` starts `bin/kira-e2e` on :18090 in a temp DATA_DIR/cwd with throwaway env + random keys, test-login storageState; teardown deletes it). 10 spec files / 14 tests: keyboard add (<10 s, ~0.2 s measured), amend, void, filter+search, CSV+ZIP export, ledger status, receipt, logout/401, normal build 404 on /auth/test-login (:18091), SIGKILL durability (:18092). 14/14 pass in ~6 s via `make e2e`. No product bugs found. Run: `make build && make build-e2e && make e2e` (needs a one-time `npx playwright install chromium`).
- [ ] fix failures; commit `test: e2e`

## Phase 4 · Review + hardening
- [x] kira-reviewer: security review vs TRD §7, traceability FR → TR → test
  - + agency reviewers in parallel: Code Reviewer, AppSec Engineer, API Tester, Reality Checker. 0 P0, 4 P1 (receipt swap undetected, e2e binary unguarded, 45,90 parsed as RM 4,590, receipt upload race), ~15 P2 — all P1 + most P2 fixed.
- [x] apply fixes; re-run `make check` + `make e2e`; commit `fix: review findings`
  - Open (Jaxz decision): live data in iCloud Drive (AppSec HIGH) → scripts/install-macos.sh ready, not run.
  - [x] kira-api Phase 4 fixes (2026-11-09): new ledger APIs adopted (`api.Integrity`: MAC'd `DATA_DIR/head` written only from the append hook / verify / acknowledge callbacks under the write lock, forward-only, fail closed on missing/invalid; legacy `id:hash` files are treated as **invalid** (one acknowledgement), not silently upgraded, because an unauthenticated legacy file would bypass the MAC). `DATA_DIR/ack` persisted (0600, atomic, HMAC'd with HKDF(LEDGER_KEY,"kira-ack") so a forged file is ignored), imported before the first verify; `kira verify` reads both files and writes neither. CSRF (`auth.SameOrigin`) on every non-GET /api request → 403 `forbidden` "cross-site request"; CORP header. `GET /api/receipts/:sha` → 409 on tamper. e2e guard first in `kira serve`; e2e builds skip `.env`. Config: OIDC_ISSUER_URL e2e-only (WARN otherwise), HOST in config (non-loopback + http PUBLIC_URL refused), TRUST_PROXY, rate-limit map capped at 10k, iCloud WARN. PATCH category via `UpdateCategory`; from>to → 400 before querying (list + export). Go: 52 tests / 226 incl. subtests in api+config+cmd; `go test ./...` and `-tags e2e` green; `make check` green; binary smoke passed. Deferred: exposing integrity events in Settings (no DTO change).

## 🛑 Gate B · Backup target (Jaxz, optional)
- [x] Jaxz fills LITESTREAM_* in `.env`, or replies "local backup" to use `./backup`
  - Decision 2026-11-09: **local backup** (./backup) for now, chosen by Claude at Jaxz's request. Upgrade to Cloudflare R2 later for off-machine copies (NFR-4).

## Phase 5 · Ops + restore drill + README
- [ ] `make backup-test`: write entries → restore to temp → `kira verify` passes
- [ ] install instructions for systemd (and macOS launchd note)
- [x] README.md (setup, Google console steps, run, backup/restore, LEDGER_KEY warning)
- [ ] final report: FR coverage table + what's deferred; commit `docs: readme`

## Cross-lane requests
_(subagents add notes here)_
- **kira-auth → kira-ledger**: implement `auth.Store` (internal/auth/auth.go) in internal/store with exactly those signatures. Needs `sessions` to have `email` and `google_sub` columns (TRD §4 lacks them). `PinOwner` must be insert-once (return an error if an owner already exists). `GetSession` returns found=false (nil err) for a missing row. `DeleteSession` on `sessions` is allowed (not append-only).
- **kira-auth → kira-api**: build `auth.Config` from env (IssuerURL empty = Google; RedirectURL = PUBLIC_URL+"/auth/google/callback"; Secure = PUBLIC_URL is https; SessionSecret >= 32 bytes). Mount `a.Routes(r)` under the /auth rate limiter, wrap /api/* with `a.Middleware`, and use `auth.UserFrom(ctx)` for /api/me. Use `internal/auth/memstore` + `auth.WithUser` in handler tests. Logout/test-login are bodyless POSTs, so exempt them from the JSON Content-Type rule (SameSite=Lax still applies).
- **kira-ledger → kira-auth**: done. `*store.Store` matches `auth.Store` exactly. `PinOwner` is a no-op for the same sub and returns `store.ErrOwnerMismatch` for a different sub.
- **kira-auth → kira-api (Phase 4)**: (1) call `auth.E2EGuard()` at startup and exit on error (e2e builds require `KIRA_E2E=1`; normal builds always nil). (2) Set `auth.Config.PublicOrigin` = scheme://host[:port] of PUBLIC_URL. (3) Use `auth.SameOrigin(r, publicOrigin)` for non-GET /api/* (403 `forbidden` "cross-site request"); logout already enforces it. (4) Session cookie is `__Host-kira_session` when `Secure`, else `kira_session`; use `a.SessionCookieName()` instead of `auth.SessionCookie`. `internal/api` `TestLogoutThroughRouter` (api_test.go:596) now needs `Sec-Fetch-Site: same-origin` on the logout request. /auth/test-login answers 404 unless the TCP peer is loopback (X-Forwarded-For ignored).
- **kira-auth → kira-tester (Phase 4)**: start `bin/kira-e2e` with `KIRA_E2E=1` and call /auth/test-login via 127.0.0.1/::1. Logout from the browser sends Sec-Fetch-Site, so it passes; direct API calls need `Sec-Fetch-Site: same-origin` or a matching `Origin`.
- **kira-ledger → kira-api** (wiring): `st := store.Open(ctx, DB_PATH)`; `rs := receipts.New(dataDir+"/receipts")`; `svc := ledger.New(ledger.Config{Store: st, Key: key, Receipts: rs})`. At startup call `svc.Verify` (or `svc.VerifyAgainst(ctx, &lastHead)` when a previous head is known), then log `svc.Head()`. Error mapping:
  - 422 `validation`: `*ledger.ValidationError` (`.Fields`) and `*store.NameError` (`.Field`)
  - 422 `date_confirm_required`: `ledger.ErrDateConfirmRequired`
  - 404: `ledger.ErrNotFound`, `store.ErrNotFound`, `receipts.ErrNotFound`
  - 409: `ledger.ErrNotCreate`, `ledger.ErrVoided`, `store.ErrDuplicateName`
  - 423: `ledger.ErrLedgerBroken`
  - 400: `ledger.ErrBadRequest`, `receipts.ErrInvalidSHA`
  - 413: `receipts.ErrTooLarge`
  - 415: `receipts.ErrUnsupported`, `receipts.ErrInvalidImage`
  - Convert `VerifyResult.CheckedAt` (`time.Time`) with `.Format(time.RFC3339)`. `Problems` is internal and is not in the DTO.
- **kira-ledger → main (contract notes)**: ✅ folded into docs/api-contract.md (notes 1–5); go mod tidy done. Head persistence (`data/head`) assigned to kira-api in Phase 2.
  1. Acknowledge covers the *set of problems* found, not the head. After an ack, new appends stay allowed; a verify that finds any new or different breakage blocks writes again. Read literally, "different head" would re-lock after every write.
  2. `export.WriteCSV/WriteZip(w, rows, receipts)` take a receipt resolver (needed for `<sha>.<ext>`). If a receipt file is missing, `receipt_file` is the bare sha, and the zip lists it in `MISSING_RECEIPTS.txt`.
  3. The CSV injection guard also covers cells starting with TAB and CR (OWASP). Lines are CRLF (RFC 4180).
  4. An amend may keep an entry's now-archived category. Switching to an archived category is still a 422.
  5. A recorded head needs to persist somewhere for tail-deletion detection (TRD §4). Suggest kira-api/kira-ops write `data/head` (id:hash) after each verify/startup and pass it to `VerifyAgainst`.
  6. `go.mod` still marks modernc.org/sqlite `// indirect`. I did not run `go mod tidy`; orchestrator please do so at phase end.
- **kira-api → kira-ledger**: `Acknowledge` matches the *exact* problem set. After acking a head mismatch the API records the new head (as `VerifyAgainst` docs advise), so if the same verify also found other problems (e.g. a hash mismatch at row 5), the next verify has a smaller set and re-locks writes. Suggest: treat a verify whose problems are a subset of the acknowledged set as still acknowledged. Edge case only; no workaround in the API beyond re-acknowledging.
- **kira-api → kira-ops**: `bin/kira seed --n N [--force]` now exists and `DATA_DIR=<dir> bin/kira verify` works with only DATA_DIR + LEDGER_KEY, so the TODOs in `scripts/backup-test.sh` can be removed (the restored dir has no `head` file, which is treated as a first run). Server binds `127.0.0.1:PORT`; optional `HOST` env (e.g. `0.0.0.0`) could be documented in `.env.example`.
- **kira-api → main (contract notes)**: (1) a JSON value of the wrong type (e.g. `"amount_sen":"45.90"`) returns 422 `validation` with that field, not 400; unknown fields and malformed/multiple JSON values are 400. (2) While the ledger is broken and unacknowledged *every* POST/PATCH under /api (categories and receipt uploads too) is 423, per TR-7 "app stays read-only". (3) Unknown `/api/*` paths are 401 without a session, 404 JSON with one (auth runs first, so routes aren't probeable). 405 under /api uses code `bad_request`. (4) `PATCH /api/categories/:id` with neither `name` nor `archived` → 400. `GET /api/export` with `from > to` → 400.
- **kira-api → kira-frontend**: 423 `ledger_broken` can come from category create/rename/archive and receipt upload, not only entry writes; show the banner for those too.

- **kira-tester → kira-ops**: `make e2e` assumes the Playwright Chromium browser is installed. Please add `cd e2e && npx playwright install chromium` (idempotent) after `npm --prefix e2e ci` in the `e2e` target, otherwise a fresh machine fails with "Executable doesn't exist".
- **kira-ledger → kira-api (Phase 4 ledger fixes)**. `internal/api` does NOT compile until handlers.go:367 adopts the new `Acknowledge` signature. New/changed exported APIs:
  - `ledger.(*Service).Acknowledge(ctx, recordedHead string, onAck func(VerifyResult) error) (VerifyResult, bool, error)` (CHANGED). Holds the write mutex, inserts an `integrity_events` row (kind `acknowledge`, recorded_head = the arg, `""` = NULL, actual_head, problems JSON), then runs `onAck` under the mutex: write the head file and the ack file there. No event and no `onAck` when the last verify was OK.
  - `ledger.(*Service).VerifyLocked(ctx, VerifyOptions, fn func(VerifyResult) error) (VerifyResult, error)`: verify + record under the write mutex, then `fn` (write head/ack files). Also available: `VerifyWith(ctx, VerifyOptions)` (no lock), `WithWriteLock(fn func() error) error`.
  - `ledger.VerifyOptions{Expected *Head; HeadReason string}`. HeadReason is `ledger.ReasonHeadMissing` or `ledger.ReasonHeadInvalid` when DATA_DIR/head is absent, malformed or has a bad MAC. It only counts when entries exist, and `broken_at` is the current head id. New reasons: `ReasonHeadMissing`, `ReasonHeadInvalid`, `ReasonReceiptMismatch`, `ReasonReceiptMissing`. `Problem` gained `Detail` (receipt sha or recorded head) and `Key()`.
  - `ledger.(*Service).SetAppendHook(fn func(Row))`: runs after every commit while the mutex is held, rows strictly in chain order. Use it to write the head file. It cannot fail the append, so log any error inside it.
  - `ledger.(*Service).ExportAck() ([]byte, error)` (nil = nothing acknowledged, so remove DATA_DIR/ack) and `ImportAck([]byte) error` (call before the first verify at startup). `AckState()` returns the struct.
  - `ledger.(*Service).IntegrityEvents(ctx, limit int) ([]store.IntegrityEvent, error)`, newest first. Store-level: `store.(*Store).InsertIntegrityEvent`, `RecentIntegrityEvents`.
  - Head MAC: `ledger.HeadMAC(key []byte, id int64, hash string) string`, `ledger.CheckHeadMAC(key, id, hash, mac) bool`, `ledger.FormatHeadFile(key, Head) string` ("id:hash:mac\n"), `ledger.ParseHeadFile(key, content string) (Head, error)` (returns `ledger.ErrHeadInvalid`), `ledger.FormatHeadID(Head) string`. HKDF is stdlib `crypto/hkdf`: SHA-256, no salt, info "kira-head", 32 bytes.
  - Receipts: `receipts.(*Store).OpenVerified(sha) (*os.File, Info, error)` returns `receipts.ErrTampered` (map to 409 `conflict`). `ErrCorrupt` remains as an alias. Verify re-hashes receipts automatically because `*receipts.Store` implements `ledger.ReceiptVerifier`.
  - `store.(*Store).UpdateCategory(ctx, id int64, name *string, archived *bool, at time.Time) (Category, error)`: rename and archive in one transaction. Use it for PATCH instead of Rename+Archive.
  - Validation: `income` is a 422 on `direction`. `receipt_sha256: ""` is a 422. All failing fields come back in one `*ValidationError`, including unknown or archived `category_id`.
  - Export: `WriteZip` adds `TAMPERED_RECEIPTS.txt`. Signature unchanged.

- **kira-api → kira-ledger (Phase 4)**: with an *invalid* (garbled / bad-MAC) head file and **zero** entries, verify is OK because `HeadReason` only counts when entries exist, so "delete every row + garble the head file" verifies clean (the API then overwrites the file). Suggest: `ReasonHeadInvalid` counts even on an empty ledger (`ReasonHeadMissing` must stay exempt for fresh installs).
  - **kira-ledger**: done. `ReasonHeadInvalid` now fails verify even with zero entries (`broken_at` = 0, `head_id` = 0); `ReasonHeadMissing` stays exempt on an empty ledger. Documented on `VerifyOptions.HeadReason`; covered by `TestHeadReasons`. No internal/api changes needed, all api tests pass.
- **kira-api → kira-ops (Phase 4)**: document in `.env.example`: `TRUST_PROXY=` (set `1` behind `tailscale serve` so the /auth rate limit sees client IPs; default off) and that `HOST` other than loopback now requires an https `PUBLIC_URL` (startup refuses otherwise). `OIDC_ISSUER_URL` is ignored (WARN) in normal builds. `kira serve` logs WARN "data is in iCloud Drive; run scripts/install-macos.sh" when DATA_DIR or the loaded .env is under ~/Library/Mobile Documents (true for `make dev` from this repo). `backup-test.sh` already copies `head`; a real Litestream restore has no `head`/`ack`, so the first verify reports "head missing" and needs one acknowledgement (document in README restore steps). `kira verify` output gained ` acknowledged=true|false`.
- **kira-api → kira-tester (Phase 4)**: every non-GET /api call needs `Sec-Fetch-Site: same-origin` (or `Origin: <PUBLIC_URL origin>`); Playwright `request` fixtures do not send it, so set `extraHTTPHeaders: {'Sec-Fetch-Site': 'same-origin'}` (or Origin) in playwright.config / `request.newContext`. bin/kira-e2e ignores `.env` and exits 2 without `KIRA_E2E=1`.
- **kira-api → kira-frontend (Phase 4)**: `GET /api/receipts/:sha` can now return 409 `conflict` (file tampered); show "receipt file changed since upload" instead of a broken image. Integrity events are not exposed yet (deferred, no DTO).

## Deferred
- **Integrity events in Settings** (kira-api, Phase 4): `ledger.Service.IntegrityEvents` exists; exposing it needs a DTO/contract change (main). Not done.
- **Category names are outside the hash chain** (kira-ledger, Phase 4): entries hash `category_id` only, so renaming a category relabels historic entries and CSV exports. A v1.1 migration should snapshot `category_name` per entries row (hashed) and export that.

## Log
_(one line per phase completion)_
- Phase 4 ✅ 5 reviewers; fixed receipt integrity, e2e guard, CSRF, MAC'd head + persisted ack + integrity_events, money/upload races; make check green, e2e 19/19
- Phase 2 ✅ API wired (126 Go api/config/cli tests), frontend on real API (57 web tests), real-server walkthrough pass, CSP data:-font bug fixed
- Phase 1 ✅ ledger (195 tests, REPLACE-bypass triggers added), auth (50/53 tests, fake OIDC), frontend (6 pages, 43 tests, mock API), ops (Makefile + deploy units)
- Phase 0 ✅ scaffold, Tailwind theme, API contract (md + dto.go + types.ts), make check green

## Review findings (kira-reviewer)
_Phase 4 early pass, 2026-11-09, during Gate A. `go vet` and `go test ./...` pass (also with `-tags e2e`). The normal binary has no test-login symbols or strings; the e2e binary does. No UPDATE/DELETE on entries/auth_log outside tests. The only INSERT INTO entries is ledger.Service.Append. Every query is parameterised. No float money._
- **P0:** none.
- **P1-1 (kira-ledger + kira-api):** receipt swap is not detected. `receipts.VerifyFile` (internal/receipts/receipts.go:251) is never called. Fix: re-hash every referenced receipt during verify, on GET /api/receipts/:sha and in the zip export. Report a mismatch or missing file as a problem.
- **P1-2 (kira-auth/kira-api):** `bin/kira-e2e` has no guard (internal/auth/testlogin_e2e.go:14). If it runs from the repo root, it uses the real .env and data/. Then an unauthenticated POST mints a session, and while no owner is pinned (now, Gate A) it pins `e2e-test-sub` in the insert-once owner table, which locks out real login. Fix: refuse to start unless `KIRA_E2E=1`, refuse if the owner is pinned to another sub, and log a WARN banner at startup.
- **P2:** Origin/Sec-Fetch-Site check on /api POST/PATCH and /auth/logout. Bodyless acknowledge and multipart uploads count as "simple" requests, and in dev other localhost ports are same-site (internal/api/server.go:166,182). · Reject `direction=income` in v1, because totals ignore direction (internal/ledger/validate.go:111, query.go:170,215). · Lower MaxPixels or limit decodes to one at a time (receipts.go:33). · Allow OIDC_ISSUER_URL only in test builds (config.go:138). · Refuse a non-loopback HOST while PUBLIC_URL is http (cmd/kira/main.go:167). · Add `.env.*` (except `!.env.example`) to .gitignore. · Add an NFR-6 benchmark. · Categories are outside the chain, so a rename changes old exports (document this, or snapshot the name in a later migration).
- **Untested Must/Should:** FR-1/FR-2 with real Google (Gate A / Phase 3), NFR-4/TR-11 restore (Phase 5), NFR-6 performance, the NFR-7 phone layout, and receipt-swap detection (P1-1).
- **kira-tester (Phase 4 follow-up)**: e2e suite now 19 tests green x2 (~9 s). Launcher sets KIRA_E2E=1; API contexts send `Sec-Fetch-Site: same-origin` (e2e/tests/fixtures.ts). New specs 11-15: e2e guard, CSRF, "45,90", receipt race, receipt tamper + ack across restart. No product bugs found.
