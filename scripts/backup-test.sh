#!/usr/bin/env bash
# Restore drill (Phase 5). Uses a throwaway env: temp DATA_DIR and freshly generated keys, never the real ones.
# STUBBED until Phase 2: needs `kira serve`, `kira verify`, and a way to seed entries
# (TODO: `kira seed --n N` helper or API calls with the e2e build's /auth/test-login).
set -eu
cd "$(dirname "$0")/.."
command -v litestream >/dev/null || { echo "litestream not installed (brew install litestream)"; exit 1; }
[ -x bin/kira ] || { echo "bin/kira missing: run make build"; exit 1; }

tmp="$(mktemp -d)"; trap 'kill ${kpid:-} ${lpid:-} 2>/dev/null || true; rm -rf "$tmp"' EXIT
export DATA_DIR="$tmp/data" LEDGER_KEY="$(openssl rand -hex 32)" SESSION_SECRET="$(openssl rand -hex 32)"
export PORT=18080 PUBLIC_URL=http://localhost:18080
mkdir -m 700 "$DATA_DIR"

cat > "$tmp/ls.yml" <<YML
dbs:
  - path: $DATA_DIR/kira.db
    replicas:
      - type: file
        path: $tmp/replica
        sync-interval: 1s
YML

# TODO(phase2): seed N entries (bin/kira seed) so the DB exists and has rows.
N=5
bin/kira seed --n "$N" || { echo "TODO: 'kira seed' not implemented yet (Phase 2)"; exit 2; }
litestream replicate -config "$tmp/ls.yml" & lpid=$!
sleep 3; kill "$lpid"; lpid=

litestream restore -config "$tmp/ls.yml" -o "$tmp/restored/kira.db" "$DATA_DIR/kira.db"
# TODO(phase2): verify must accept a data dir / db path; assumed DATA_DIR override here.
DATA_DIR="$tmp/restored" bin/kira verify
# TODO(phase2): count entries from restored DB (sqlite3 optional) and compare with $N.
got="$(sqlite3 "$tmp/restored/kira.db" 'select count(*) from entries')"
[ "$got" = "$N" ] && echo "backup-test OK: $got entries restored and verified" || { echo "count mismatch: want $N got $got"; exit 1; }
