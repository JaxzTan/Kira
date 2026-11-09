#!/usr/bin/env bash
# Litestream wrapper (launchd + systemd). Least privilege: parses .env with a safe KEY=VALUE
# reader and exports ONLY DATA_DIR and LITESTREAM_*; LEDGER_KEY/SESSION_SECRET/Google secrets
# are never loaded. Nothing is echoed. The effective config is written to a 0600 temp file
# (it holds ${VAR} references only, no secret values) and removed on exit.
set -eu
cd "$(dirname "$0")/.."
umask 077
if [ -f .env ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    [[ "$line" =~ ^(DATA_DIR|LITESTREAM_[A-Z0-9_]+)=(.*)$ ]] || continue
    k="${BASH_REMATCH[1]}"; v="${BASH_REMATCH[2]}"
    v="${v%\"}"; v="${v#\"}"
    export "$k=$v"
  done < .env
fi
export DATA_DIR="${DATA_DIR:-./data}"
case "$DATA_DIR" in /*) ;; *) DATA_DIR="$PWD/${DATA_DIR#./}"; export DATA_DIR;; esac
command -v litestream >/dev/null || { echo "litestream not installed (brew install litestream)" >&2; exit 2; }

cfg="$(mktemp "${TMPDIR:-/tmp}/kira-ls.XXXXXX")"; chmod 600 "$cfg"
trap 'rm -f "$cfg"; [ -z "${pid:-}" ] || kill "$pid" 2>/dev/null || true' EXIT
trap 'exit 0' INT TERM

{
  if [ -n "${LITESTREAM_REPLICA_URL:-}" ] && [ -n "${LITESTREAM_ACCESS_KEY_ID:-}" ] && [ -n "${LITESTREAM_SECRET_ACCESS_KEY:-}" ]; then
    echo 'access-key-id: ${LITESTREAM_ACCESS_KEY_ID}'
    echo 'secret-access-key: ${LITESTREAM_SECRET_ACCESS_KEY}'
    echo 'dbs:'
    echo '  - path: ${DATA_DIR}/kira.db'
    echo '    replicas:'
    echo '      - url: ${LITESTREAM_REPLICA_URL}'
    [ -z "${LITESTREAM_ENDPOINT:-}" ] || echo '        endpoint: ${LITESTREAM_ENDPOINT}'
  else
    mkdir -p backup
    echo 'dbs:'
    echo '  - path: ${DATA_DIR}/kira.db'
    echo '    replicas:'
    echo '      - type: file'
    echo "        path: $PWD/backup/kira.db"
  fi
  echo '        sync-interval: 10s'
  if [ -n "${LITESTREAM_AGE_RECIPIENT:-}" ]; then
    # Restore requires the matching age identity (kept offline).
    echo '        age:'
    echo '          recipients:'
    echo '            - ${LITESTREAM_AGE_RECIPIENT}'
  fi
} > "$cfg"

litestream replicate -config "$cfg" &
pid=$!
wait "$pid"
