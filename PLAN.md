# Kira build plan (live board)

| Phase | Status | Runs in parallel | Gate |
|---|---|---|---|
| 0 · Scaffold + API contract | ✅ done | main only | — |
| 1 · Core build | ✅ done | kira-ledger · kira-auth · kira-frontend · kira-ops | — |
| 2 · Wire API + real frontend | ⬜ todo | kira-api → kira-frontend | — |
| 🛑 A · Google OAuth credentials | ⬜ waiting | — | **Jaxz** |
| 3 · Real login + E2E | ⬜ todo | kira-tester (+ fixes) | after A |
| 4 · Review + hardening | ⬜ todo | kira-reviewer (+ agency reviewers if installed) | — |
| 🛑 B · Backup target (optional) | ⬜ waiting | — | **Jaxz** |
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
- [ ] kira-api: config loading + validation, router, all handlers per contract, rate limit on /auth
- [ ] kira-api: `bin/kira serve` and `bin/kira verify` subcommands; startup verify + head log
- [ ] kira-frontend: switch from mock to real API, error states, 401 → /login
- [ ] `make check` green; commit `feat: core app`

## 🛑 Gate A · Google OAuth credentials (Jaxz)
Claude posts the exact Google Cloud Console steps, then waits for **"creds ready"**.
- [ ] Jaxz filled GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ALLOWED_EMAIL in `.env`
- [ ] `make check-env` ✓

## Phase 3 · Real login + E2E
- [ ] Jaxz logs in once with real Google (Claude asks him to, then reads server logs) → owner sub pinned
- [ ] Jaxz tries a second Google account → 403 confirmed
- [ ] kira-tester: Playwright (`-tags e2e` build): login → add → amend → void → filter → export → verify
- [ ] fix failures; commit `test: e2e`

## Phase 4 · Review + hardening
- [ ] kira-reviewer: security review vs TRD §7, traceability FR → TR → test
- [ ] apply fixes; re-run `make check` + `make e2e`; commit `fix: review findings`

## 🛑 Gate B · Backup target (Jaxz, optional)
- [ ] Jaxz fills LITESTREAM_* in `.env`, or replies "local backup" to use `./backup`

## Phase 5 · Ops + restore drill + README
- [ ] `make backup-test`: write entries → restore to temp → `kira verify` passes
- [ ] install instructions for systemd (and macOS launchd note)
- [ ] README.md (setup, Google console steps, run, backup/restore, LEDGER_KEY warning)
- [ ] final report: FR coverage table + what's deferred; commit `docs: readme`

## Cross-lane requests
_(subagents add notes here)_
- **kira-auth → kira-ledger**: implement `auth.Store` (internal/auth/auth.go) in internal/store with exactly those signatures. Needs `sessions` to have `email` and `google_sub` columns (TRD §4 lacks them). `PinOwner` must be insert-once (return an error if an owner already exists). `GetSession` returns found=false (nil err) for a missing row. `DeleteSession` on `sessions` is allowed (not append-only).
- **kira-auth → kira-api**: build `auth.Config` from env (IssuerURL empty = Google; RedirectURL = PUBLIC_URL+"/auth/google/callback"; Secure = PUBLIC_URL is https; SessionSecret >= 32 bytes). Mount `a.Routes(r)` under the /auth rate limiter, wrap /api/* with `a.Middleware`, and use `auth.UserFrom(ctx)` for /api/me. Use `internal/auth/memstore` + `auth.WithUser` in handler tests. Logout/test-login are bodyless POSTs, so exempt them from the JSON Content-Type rule (SameSite=Lax still applies).
- **kira-ledger → kira-auth**: done. `*store.Store` matches `auth.Store` exactly. `PinOwner` is a no-op for the same sub and returns `store.ErrOwnerMismatch` for a different sub.
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

## Log
_(one line per phase completion)_
- Phase 1 ✅ ledger (195 tests, REPLACE-bypass triggers added), auth (50/53 tests, fake OIDC), frontend (6 pages, 43 tests, mock API), ops (Makefile + deploy units)
- Phase 0 ✅ scaffold, Tailwind theme, API contract (md + dto.go + types.ts), make check green
