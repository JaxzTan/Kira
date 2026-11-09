#!/usr/bin/env bash
# Prints the litestream config path to use. Never prints secret values.
# Reads only whether LITESTREAM_* are non-empty, from the environment or .env.
set -eu
cd "$(dirname "$0")/.."
get() { # value of $1 from env, else from .env (not echoed by callers)
  local v="${!1:-}"
  if [ -z "$v" ] && [ -f .env ]; then
    v="$(grep -E "^$1=" .env | tail -n1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' || true)"
  fi
  printf '%s' "$v"
}
if [ -n "$(get LITESTREAM_REPLICA_URL)" ] && [ -n "$(get LITESTREAM_ACCESS_KEY_ID)" ] && [ -n "$(get LITESTREAM_SECRET_ACCESS_KEY)" ]; then
  echo "$PWD/deploy/litestream.yml"
else
  echo "$PWD/deploy/litestream.local.yml"
fi
