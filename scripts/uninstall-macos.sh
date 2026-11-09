#!/usr/bin/env bash
# Stop and remove the Kira launchd agents. Data in KIRA_HOME is KEPT unless --purge is given.
#   scripts/uninstall-macos.sh [--purge]
set -eu
KIRA_HOME="${KIRA_HOME:-$HOME/Library/Application Support/Kira}"
LA="$HOME/Library/LaunchAgents"
for n in server litestream; do
  launchctl bootout "gui/$(id -u)/com.kira.$n" 2>/dev/null || true
  rm -f "$LA/com.kira.$n.plist"
done
echo "agents removed."
if [ "${1:-}" = "--purge" ]; then
  echo "--purge: this deletes the ledger at $KIRA_HOME. Type 'delete' to confirm:"
  read -r ans
  [ "$ans" = "delete" ] && rm -rf "$KIRA_HOME" && echo "removed $KIRA_HOME" || echo "kept $KIRA_HOME"
else
  echo "kept $KIRA_HOME (data, .env, backups). Use --purge to delete."
fi
