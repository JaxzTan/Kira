# Kira build plan (live board)

| Phase | Status | Runs in parallel | Gate |
|---|---|---|---|
| 0 · Scaffold + API contract | ✅ done | main only | — |
| 1 · Core build | ⬜ todo | kira-ledger · kira-auth · kira-frontend · kira-ops | — |
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
- [ ] migrations + triggers (entries, auth_log append-only; owner insert-once; categories; sessions)
- [ ] store: open DB with PRAGMAs, migrate, seed categories
- [ ] ledger.Service: Append(create/amend/void), Current view, History, Verify
- [ ] tamper tests: raw-SQL edit, dropped trigger, deleted tail → Verify fails at right id
- [ ] durability test: kill mid-write, reopen, all committed rows present
- [ ] receipts store (sha256 names, type/size limits, EXIF GPS strip) — FR-6
- [ ] export CSV + zip — FR-12

**kira-auth** — FR-1, FR-2, NFR-1
- [ ] OIDC login/callback with state + PKCE + nonce
- [ ] allow-list (email_verified, email, pinned sub) + auth_log
- [ ] sessions (hashed token, cookie flags, 30-day expiry, logout)
- [ ] middleware re-checking allow-list per request
- [ ] tests with fake OIDC provider (httptest JWKS): wrong email, unverified, wrong sub, expired, bad sig, bad state
- [ ] `/auth/test-login` behind `//go:build e2e` + test proving normal build lacks it

**kira-frontend** — FR-7, FR-10, FR-11 UI, NFR-7 (against mock API)
- [ ] shared components from design/README.md
- [ ] Login, Dashboard, Entries + QuickAdd, Entry detail (match design/screens/*.png)
- [ ] Export + Settings pages (categories, ledger status) in same style
- [ ] keyboard: `N` shortcut, quick-add Tab order, Enter saves
- [ ] component tests (Vitest) for QuickAdd and money formatting

**kira-ops**
- [ ] Makefile targets (CLAUDE.md list)
- [ ] `deploy/kira.service`, `deploy/litestream.yml` (env-driven; local file replica fallback)

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

## Log
_(one line per phase completion)_
- Phase 0 ✅ scaffold, Tailwind theme, API contract (md + dto.go + types.ts), make check green
