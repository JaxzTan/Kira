#!/usr/bin/env bash
# Kira bootstrap: checks tools, inits git, prepares .env (keeps your values), optionally installs agency-agents.
# Usage: bash scripts/bootstrap.sh [--agency]
set -euo pipefail
cd "$(dirname "$0")/.."

ok()   { printf "  \033[32m✓\033[0m %s\n" "$1"; }
warn() { printf "  \033[33m!\033[0m %s\n" "$1"; }
die()  { printf "  \033[31m✗\033[0m %s\n" "$1"; exit 1; }

echo "1/5 Checking tools"
command -v go >/dev/null || die "Go not found (need 1.23+): https://go.dev/dl/"
gov=$(go env GOVERSION | sed 's/go//'); ok "go $gov"
[ "$(printf '%s\n1.23\n' "$gov" | sort -V | head -n1)" = "1.23" ] || die "Go $gov is older than 1.23"
command -v node >/dev/null || die "Node not found (need 20+): https://nodejs.org"
nv=$(node -v | sed 's/v//' | cut -d. -f1); [ "$nv" -ge 20 ] || die "Node $nv < 20"; ok "node $(node -v)"
command -v git >/dev/null || die "git not found"; ok "git"
command -v openssl >/dev/null || die "openssl not found"; ok "openssl"
command -v claude >/dev/null && ok "claude (Claude Code)" || warn "claude CLI not found: npm i -g @anthropic-ai/claude-code"
command -v sqlite3 >/dev/null && ok "sqlite3 (optional)" || warn "sqlite3 not found (optional, handy for inspecting the DB)"

echo "2/5 Checking given folders"
for f in docs/PRD-kira.md docs/TRD-kira.md design/README.md design/tokens.css CLAUDE.md PLAN.md .claude/commands/kickoff.md; do
  [ -f "$f" ] && ok "$f" || die "missing $f"
done

echo "3/5 Git"
if [ -d .git ]; then ok "repo exists"; else git init -q && ok "git init"; fi
grep -qxF ".env" .gitignore 2>/dev/null && ok ".env is git-ignored" || die ".gitignore must contain .env"

echo "4/5 .env"
if [ ! -f .env ]; then cp .env.example .env && chmod 600 .env && ok "created .env from .env.example"; else ok ".env exists — keeping your values"; fi
chmod 600 .env
# append any keys from .env.example missing in .env (never overwrite)
while IFS= read -r line; do
  key="${line%%=*}"; [[ "$line" =~ ^[A-Z_]+= ]] || continue
  grep -qE "^$key=" .env || { echo "$line" >> .env; ok "added missing key $key"; }
done < .env.example
gen() { # fill an empty secret with 32 random bytes hex
  if grep -qE "^$1=$" .env; then
    v=$(openssl rand -hex 32)
    tmp=$(mktemp); sed "s|^$1=$|$1=$v|" .env > "$tmp" && mv "$tmp" .env && chmod 600 .env
    ok "generated $1"
  else ok "$1 already set"; fi
}
gen SESSION_SECRET
gen LEDGER_KEY
warn "Save LEDGER_KEY from .env into your password manager now. Lose it = can't verify the ledger."
mkdir -p data && chmod 700 data && ok "data/ (700)"

echo "5/5 Agents"
ok "project subagents: $(ls .claude/agents | sed 's/\.md//' | tr '\n' ' ')"
if [ "${1:-}" = "--agency" ]; then
  tmp=$(mktemp -d)
  git clone -q --depth 1 https://github.com/msitarzewski/agency-agents.git "$tmp/agency" \
    && (cd "$tmp/agency" && ./scripts/install.sh --tool claude-code --division engineering,security,testing) \
    && ok "agency-agents installed to ~/.claude/agents (extra reviewers)" \
    || warn "agency-agents install failed — build still works with project subagents"
  rm -rf "$tmp"
else
  warn "agency-agents not installed (optional): re-run with --agency"
fi

echo
bash scripts/check-env.sh || true
cat << 'NEXT'

Next:
  1. claude                      # start Claude Code in this folder
  2. /kickoff                    # auto mode (stops only at gates)
     /kickoff step               # stop after every phase
     /kickoff resume             # continue a previous session
  Google keys can stay empty until Claude stops at Gate A.
NEXT
