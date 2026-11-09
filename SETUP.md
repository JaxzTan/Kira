# Kira starter — setup (delete this file once built)

```
kira/
├── docs/            PRD-kira.md, TRD-kira.md            (given)
├── design/          screens/*.png|html, tokens.css, README.md, source/  (given)
├── .env             created by bootstrap, you add Google keys at Gate A (given)
├── CLAUDE.md        project rules every agent reads
├── PLAN.md          live build board (phases, checkboxes, gates)
├── .claude/
│   ├── commands/kickoff.md   /kickoff orchestration prompt
│   ├── agents/kira-*.md      7 lane subagents
│   └── settings.json         allowed commands; blocks reading .env and git push
└── scripts/         bootstrap.sh, check-env.sh
```
Everything else (Go backend, web/, e2e/, deploy/, Makefile, README) is built by Claude Code.

## Steps
1. Unzip into an empty folder (or your repo root).
2. `bash scripts/bootstrap.sh` — add `--agency` to also install agency-agents as extra reviewers.
3. Copy `LEDGER_KEY` from `.env` into your password manager.
4. `claude` then `/kickoff` (or `/kickoff step` to approve every phase).
5. Type `go` after it shows the plan. It stops at **Gate A** for Google credentials and **Gate B** for backups.
6. Session died? `claude` then `/kickoff resume`.
