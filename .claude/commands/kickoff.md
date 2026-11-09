---
description: Orchestrate the full Kira build from docs/ and design/ using parallel subagents, with a visible plan and stops at credential gates.
---

You are the **orchestrator** for building Kira. You coordinate; subagents do most of the building.

Mode: $ARGUMENTS
- empty or `auto` → after I approve the plan once, run phase to phase on your own and stop only at 🛑 gates or on a blocker.
- `step` → stop after every phase and wait for me to type `next`.
- `resume` → read PLAN.md, find the first unfinished item, say where you are, continue.

## 0. Orient (always first)
1. Read CLAUDE.md, PLAN.md, docs/PRD-kira.md, docs/TRD-kira.md, design/README.md; look at design/screens/*.png.
2. Run `ls .claude/agents ~/.claude/agents 2>/dev/null` and list which helpers exist:
   - project lane agents: kira-ledger, kira-auth, kira-api, kira-frontend, kira-tester, kira-reviewer, kira-ops
   - any agency-agents reviewers (e.g. a code reviewer, security engineer, API tester, reality checker). They are **extra reviewers only** in Phase 3–4; they never own a lane.
3. Run `make check-env` if it exists, else `bash scripts/check-env.sh`, and report which keys are ✓/✗ (never values). Missing Google keys are expected now.
4. Mirror PLAN.md into your todo list, then show me:
   - a 10-line summary of what you're building,
   - the phase table from PLAN.md,
   - which subagents run in parallel in each phase,
   - any open questions (max 3).
   Then **wait for me to type `go`** (in every mode).

## How to run each phase
- At phase start: set it 🟦 in PLAN.md and post the phase's checklist + who runs it.
- Launch independent subagents **in one message so they run in parallel**. Give each: its lane, the PLAN.md items it owns, and "read CLAUDE.md first; tick your PLAN.md items; return a 5-line summary".
- When subagents return: read their summaries, run `make check` (or the pieces that exist), handle "Cross-lane requests" in PLAN.md yourself or by re-dispatching the owner.
- At phase end: set ✅, add one line to the Log, commit (conventional message, never push), and post:
  `Phase N done · built: … · tests: X passing · issues: … · next: …`

## Phases
**Phase 0 (you, sequential):** scaffold + API contract exactly as PLAN.md. The contract (docs/api-contract.md, internal/api/dto.go, web/src/api/types.ts) must be complete before Phase 1, because four agents build against it at once.

**Phase 1 (parallel):** kira-ledger, kira-auth, kira-frontend (mock API), kira-ops — all four in one message.

**Phase 2:** kira-api first; when it returns green, kira-frontend switches to the real API. Run `make build` and start the server briefly to confirm `/api/me` returns 401 without a session.

**🛑 Gate A — Google OAuth.** Stop all work that needs real login. Post exactly this, then wait for me to type `creds ready`:

> **Your turn: Google OAuth client (≈5 min)**
> 1. https://console.cloud.google.com → create project **Kira**.
> 2. Google Auth Platform → **Branding**: app name Kira, your email as support + developer contact.
> 3. **Audience**: External, keep it in **Testing**, add your Gmail under Test users (only test users can sign in, which is what we want).
> 4. **Clients** → Create client → **Web application**.
>    - Authorized JavaScript origins: `http://localhost:5173`, `http://localhost:8080`
>    - Authorized redirect URIs: `http://localhost:5173/auth/google/callback`, `http://localhost:8080/auth/google/callback`
>    - (Later, for Tailscale: add `https://<machine>.<tailnet>.ts.net/auth/google/callback`)
> 5. Put Client ID, Client secret and your Gmail into `.env` as `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `ALLOWED_EMAIL`.
> 6. Type `creds ready`.
> Scopes needed: only `openid email profile` (no verification required).

While waiting at Gate A you may finish independent work (e.g. kira-tester writing specs, kira-ops docs) if PLAN.md items don't depend on login.
After `creds ready`: run `make check-env`; if anything is ✗, tell me which key and wait again. Never open .env yourself.

**Phase 3:** start `make dev`, ask me to sign in once at http://localhost:5173 and then try a second Google account; confirm from server logs (auth_log reasons, no secrets) that the first succeeded + pinned owner and the second got 403. Then kira-tester runs the e2e suite; fix failures via the owning lane agent.

**Phase 4:** kira-reviewer plus any agency reviewers found in step 0.2, **in parallel, one message**. Merge findings, dedupe, fix all P0/P1 via lane agents, re-run `make check` and `make e2e`.

**🛑 Gate B — Backup (optional).** Ask: fill `LITESTREAM_*` in .env for Cloudflare R2/S3 and type `backup ready`, or type `local backup` to replicate to ./backup. Wait.

**Phase 5:** kira-ops: `make backup-test` must pass, deploy units, README.md. Then post the final report: FR coverage table (FR → status → test), what's deferred (Could items), how to run it daily, and a reminder to save LEDGER_KEY in a password manager.

## Rules
- Follow CLAUDE.md non-negotiables. If a subagent's output breaks one, send it back.
- Don't silently change the PRD/TRD. If something must change, propose it, wait for my yes, then update the doc.
- If blocked >2 attempts on the same error, stop and show me the error + what you tried.
- Keep messages short: plans as checklists, reports as one status line plus bullets.
