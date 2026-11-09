# Kira — project rules for Claude Code

Personal, single-user business expense recorder. Desktop-first web app.
**Read before any work:** `docs/PRD-kira.md`, `docs/TRD-kira.md`, `design/README.md`, `PLAN.md`.

## Given vs to-build
- Given by Jaxz (do not restructure): `docs/`, `design/`, `.env`.
- Everything else is built by you, following `PLAN.md`.

## Stack (from TRD §3)
- Go 1.23+, chi, `modernc.org/sqlite` (pure Go, no cgo), `coreos/go-oidc/v3` + `golang.org/x/oauth2`, `log/slog`.
- Module path: `github.com/JaxzTan/kira`.
- Frontend: React + Vite + TypeScript + Tailwind in `web/`, built into `web/dist` and embedded with `go:embed`.
- E2E: Playwright (TypeScript) in `e2e/`.
- No Docker.

## Repo layout & ownership (parallel subagents must stay in their lane)
| Path | Owner subagent |
|---|---|
| `migrations/`, `internal/store/`, `internal/ledger/`, `internal/receipts/`, `internal/export/` | `kira-ledger` |
| `internal/auth/` | `kira-auth` |
| `internal/api/`, `cmd/kira/`, `internal/config/` | `kira-api` |
| `web/` | `kira-frontend` |
| `e2e/` | `kira-tester` |
| `deploy/`, `scripts/`, `Makefile` | `kira-ops` |
| `docs/api-contract.md`, `web/src/api/types.ts`, `internal/api/dto.go` | main session (Phase 0 contract; changes only via main) |

A subagent that needs something outside its lane writes a note in `PLAN.md` under "Cross-lane requests" instead of editing the file.

## Non-negotiable rules
1. **Money is integer sen** (`amount_sen int64`). Never float. Format to `RM 1,234.56` only at the edge.
2. **Ledger is append-only.** No `UPDATE`/`DELETE` on `entries` or `auth_log`, ever. Corrections = `amend` rows, removals = `void` rows. All writes go through `ledger.Service.Append`.
3. **Hash chain:** `hash = HMAC_SHA256(LEDGER_KEY, prev_hash || canonical_json(row_without_hash))`, genesis prev_hash = 64 zeros. Canonical JSON = sorted keys, no whitespace.
4. **Durability:** SQLite `journal_mode=WAL`, `synchronous=FULL`, `foreign_keys=ON`, single writer. Return 201 only after commit.
5. **Auth:** verify ID token via go-oidc; require `email_verified`, `email == ALLOWED_EMAIL`, and pinned `sub`. Re-check allow-list on every `/api/*` request. Reject = 403 + `auth_log` row, no session.
6. **No auth bypass in normal builds.** The only test login is `/auth/test-login`, compiled in **only** with `-tags e2e`. A normal `go build` must not contain it (there is a test for this).
7. **Secrets:** never read, print, or log `.env` contents or secret values. Use `make check-env` to validate. Never commit `.env`.
8. Every Go package has table-driven tests. `make check` (fmt, vet, test, web typecheck+build) must pass before a phase is ticked done.
9. Git: commit at the end of every phase with a conventional commit message. Never `git push`.

## Build decisions (fill gaps in TRD)
- `ALLOWED_SUB`: if empty, the first successful login with `ALLOWED_EMAIL` stores the `sub` in table `owner` (single row, insert-once trigger). After that, a different `sub` with the same email is rejected.
- Dev: Vite on :5173 proxies `/api` and `/auth` to Go on :8080; `PUBLIC_URL=http://localhost:5173`. Cookie `Secure` flag on when `PUBLIC_URL` is https.
- Frontend uses a typed `api` client; during Phase 1 it runs against an in-memory mock implementing the same interface (`VITE_MOCK_API=1`).

## Make targets (kira-ops creates)
`make dev` (Go + Vite), `make build` (single binary at `bin/kira`), `make test`, `make check`, `make check-env`, `make verify` (runs `bin/kira verify` on the ledger), `make e2e`, `make backup-test`.

## Progress visibility
- `PLAN.md` is the live board. Tick `[x]` as tasks finish, add notes under the task, update the status table at the top.
- Start of every phase: post the phase's task list and which subagents run in parallel. End of every phase: post a short report (done / tests / issues / next).
- At 🛑 gates: stop, tell Jaxz exactly what to do, and wait for his reply. Do not continue work that depends on the gate.
