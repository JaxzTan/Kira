#!/usr/bin/env bash
# Validates .env WITHOUT printing any secret values.
set -u
ENV_FILE="${1:-.env}"
[ -f "$ENV_FILE" ] || { echo "✗ $ENV_FILE not found — run scripts/bootstrap.sh"; exit 1; }

val() { grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//'; }
fail=0
req() { # key, check-description, regex
  local v; v="$(val "$1")"
  if [ -z "$v" ]; then echo "✗ $1 is empty"; fail=1
  elif [ -n "${3:-}" ] && ! [[ "$v" =~ $3 ]]; then echo "✗ $1 doesn't look right ($2)"; fail=1
  else echo "✓ $1"; fi
}
opt() { [ -n "$(val "$1")" ] && echo "✓ $1 (set)" || echo "· $1 (empty — $2)"; }

echo "Checking $ENV_FILE"
req PORT "number" '^[0-9]+$'
req PUBLIC_URL "http(s)://..." '^https?://'
req DATA_DIR "path"
req GOOGLE_CLIENT_ID "ends with .apps.googleusercontent.com" '\.apps\.googleusercontent\.com$'
req GOOGLE_CLIENT_SECRET "non-empty"
req ALLOWED_EMAIL "an email" '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
req SESSION_SECRET "64 hex chars" '^[0-9a-f]{64}$'
req LEDGER_KEY "64 hex chars" '^[0-9a-f]{64}$'
opt ALLOWED_SUB "auto-pinned on first login"
opt LITESTREAM_REPLICA_URL "local ./backup replica will be used"

if [ "$fail" = 0 ]; then echo "All required keys OK."; else echo "Fix the ✗ keys above."; fi
exit $fail
