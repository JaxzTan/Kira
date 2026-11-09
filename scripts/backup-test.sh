#!/usr/bin/env bash
# Restore drill. Throwaway env: temp DATA_DIR and freshly generated keys; real secrets never touched.
# seed 25 -> litestream replicate (local file replica) -> restore -> kira verify -> compare row counts.
# If age-keygen is installed the drill also exercises age encryption (restore needs the identity).
set -eu
cd "$(dirname "$0")/.."
command -v litestream >/dev/null || { echo "litestream not installed: brew install litestream" >&2; exit 2; }
command -v sqlite3 >/dev/null || { echo "sqlite3 not found" >&2; exit 2; }
[ -x bin/kira ] || { echo "bin/kira missing: run make build" >&2; exit 2; }
KIRA="$PWD/bin/kira"

tmp="$(mktemp -d)"; lpid=""
trap '[ -z "$lpid" ] || kill "$lpid" 2>/dev/null || true; rm -rf "$tmp"' EXIT
unset GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET ALLOWED_EMAIL
export DATA_DIR="$tmp/data" LEDGER_KEY="$(openssl rand -hex 32)" SESSION_SECRET="$(openssl rand -hex 32)"
mkdir -m 700 "$DATA_DIR"
N=25

age_rec=""; age_id=""
if command -v age-keygen >/dev/null; then
  age-keygen -o "$tmp/age.key" >/dev/null 2>&1; chmod 600 "$tmp/age.key"
  age_rec="$(age-keygen -y "$tmp/age.key")"
  age_id="$(grep '^AGE-SECRET-KEY' "$tmp/age.key")"
fi

{
  echo "dbs:"
  echo "  - path: $DATA_DIR/kira.db"
  echo "    replicas:"
  echo "      - type: file"
  echo "        path: $tmp/replica"
  echo "        sync-interval: 1s"
  if [ -n "$age_rec" ]; then
    echo "        age:"
    echo "          recipients: [\"$age_rec\"]"
    echo "          identities: [\"$age_id\"]"
  fi
} > "$tmp/ls.yml"
chmod 600 "$tmp/ls.yml"

"$KIRA" seed --n "$N" >/dev/null
src_count="$(sqlite3 "$DATA_DIR/kira.db" 'select count(*) from entries')"

litestream replicate -config "$tmp/ls.yml" >"$tmp/ls.log" 2>&1 & lpid=$!
for _ in $(seq 1 30); do
  [ -d "$tmp/replica" ] && [ -n "$(find "$tmp/replica" -type f 2>/dev/null | head -n1)" ] && break
  sleep 1
done
sleep 3
kill "$lpid" 2>/dev/null || true; wait "$lpid" 2>/dev/null || true; lpid=""
[ -d "$tmp/replica" ] || { echo "FAIL: no replica produced"; exit 1; }

mkdir -m 700 "$tmp/restored"
litestream restore -config "$tmp/ls.yml" -o "$tmp/restored/kira.db" "$DATA_DIR/kira.db" >/dev/null 2>&1 \
  || { echo "FAIL: litestream restore failed"; exit 1; }
cp "$DATA_DIR/head" "$tmp/restored/head"   # needed for the tail check

if ! DATA_DIR="$tmp/restored" "$KIRA" verify >/dev/null; then echo "FAIL: kira verify on restored db"; exit 1; fi
got="$(sqlite3 "$tmp/restored/kira.db" 'select count(*) from entries')"
if [ "$got" = "$src_count" ] && [ "$got" -ge "$N" ]; then
  echo "PASS: $got entries replicated, restored and verified${age_rec:+ (age-encrypted)}"
else
  echo "FAIL: count mismatch (source $src_count, restored $got)"; exit 1
fi
