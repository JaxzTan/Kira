#!/usr/bin/env bash
# Nightly receipts sync (TR-11). Target: RECEIPTS_SYNC_TARGET env (rsync dest, e.g. user@host:/path/)
# or the same key in .env; otherwise copies to ./backup/receipts. Prints no secret values.
#
# systemd timer example (~/.config/systemd/user/kira-receipts.{service,timer}):
#   [Service] Type=oneshot / ExecStart=@REPO@/scripts/receipts-sync.sh
#   [Timer]   OnCalendar=daily / Persistent=true   [Install] WantedBy=timers.target
# launchd: a plist with ProgramArguments=[scripts/receipts-sync.sh] and
#   StartCalendarInterval {Hour=3, Minute=15}, loaded like the other agents.
set -eu
cd "$(dirname "$0")/.."
target="${RECEIPTS_SYNC_TARGET:-}"
data="${DATA_DIR:-}"
if [ -f .env ]; then
  [ -n "$target" ] || target="$(grep -E '^RECEIPTS_SYNC_TARGET=' .env | tail -n1 | cut -d= -f2- || true)"
  [ -n "$data" ] || data="$(grep -E '^DATA_DIR=' .env | tail -n1 | cut -d= -f2- || true)"
fi
data="${data:-./data}"
src="$data/receipts/"
[ -d "$src" ] || { echo "no receipts dir at $src, nothing to sync"; exit 0; }
target="${target:-./backup/receipts/}"
case "$target" in *:*) ;; *) mkdir -p "$target"; chmod 700 "$target" ;; esac
# no --delete: backups never remove receipts
rsync -a "$src" "$target"
echo "receipts synced"
