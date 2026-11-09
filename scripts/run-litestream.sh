#!/usr/bin/env bash
# launchd wrapper for litestream. Exports ONLY the DATA_DIR and LITESTREAM_* vars from .env
# using a safe KEY=VALUE parser (no sourcing, no eval, nothing printed).
set -eu
cd "$(dirname "$0")/.."
umask 077
if [ -f .env ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    [[ "$line" =~ ^(DATA_DIR|LITESTREAM_[A-Z_]+)=(.*)$ ]] || continue
    k="${BASH_REMATCH[1]}"; v="${BASH_REMATCH[2]}"
    v="${v%\"}"; v="${v#\"}"
    export "$k=$v"
  done < .env
fi
export DATA_DIR="${DATA_DIR:-./data}"
cfg="$(scripts/litestream-config.sh)"
mkdir -p backup
exec litestream replicate -config "$cfg"
