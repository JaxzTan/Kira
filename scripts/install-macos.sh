#!/usr/bin/env bash
# Install a runtime copy of Kira OUTSIDE iCloud Drive and render the launchd agents.
#   scripts/install-macos.sh [--load]
# KIRA_HOME defaults to "$HOME/Library/Application Support/Kira". Never prints .env values.
# Run `make build` first. Without --load only the launchctl commands are printed.
set -eu
repo="$(cd "$(dirname "$0")/.." && pwd)"
KIRA_HOME="${KIRA_HOME:-$HOME/Library/Application Support/Kira}"
LA="$HOME/Library/LaunchAgents"
load=0; [ "${1:-}" = "--load" ] && load=1

[ -x "$repo/bin/kira" ] || { echo "bin/kira missing: run make build" >&2; exit 1; }
case "$KIRA_HOME" in *"|"*) echo "KIRA_HOME must not contain '|'" >&2; exit 1;; esac
umask 077
mkdir -p "$KIRA_HOME/bin" "$KIRA_HOME/scripts" "$LA"
chmod 700 "$KIRA_HOME"
cp "$repo/bin/kira" "$KIRA_HOME/bin/kira"; chmod 755 "$KIRA_HOME/bin/kira"
cp "$repo"/scripts/*.sh "$KIRA_HOME/scripts/"; chmod 755 "$KIRA_HOME"/scripts/*.sh

if [ ! -f "$KIRA_HOME/.env" ]; then
  [ -f "$repo/.env" ] || { echo ".env missing in repo: run scripts/bootstrap.sh" >&2; exit 1; }
  cp "$repo/.env" "$KIRA_HOME/.env"; chmod 600 "$KIRA_HOME/.env"
  echo "created $KIRA_HOME/.env (copied from repo, mode 600)"
else
  echo "kept existing $KIRA_HOME/.env"
fi

# If DATA_DIR is the relative ./data, point it at KIRA_HOME/data (in the copy only; values not printed).
if grep -Eq '^DATA_DIR="?(\./)?data/?"?[[:space:]]*$' "$KIRA_HOME/.env"; then
  tmpf="$(mktemp "$KIRA_HOME/.env.XXXXXX")"
  while IFS= read -r line || [ -n "$line" ]; do
    if [[ "$line" =~ ^DATA_DIR= ]]; then printf 'DATA_DIR=%s\n' "$KIRA_HOME/data"; else printf '%s\n' "$line"; fi
  done < "$KIRA_HOME/.env" > "$tmpf"
  chmod 600 "$tmpf"; mv "$tmpf" "$KIRA_HOME/.env"
  echo "set DATA_DIR in the runtime .env to $KIRA_HOME/data"
fi
mkdir -p "$KIRA_HOME/data"; chmod 700 "$KIRA_HOME/data"

for n in server litestream; do
  sed "s|@REPO@|$KIRA_HOME|g" "$repo/deploy/com.kira.$n.plist" > "$LA/com.kira.$n.plist"
  chmod 644 "$LA/com.kira.$n.plist"
done
echo "wrote $LA/com.kira.server.plist and com.kira.litestream.plist"

cmds=("launchctl bootstrap gui/$(id -u) \"$LA/com.kira.server.plist\""
      "launchctl bootstrap gui/$(id -u) \"$LA/com.kira.litestream.plist\"")
if [ "$load" = 1 ]; then
  for n in server litestream; do launchctl bootstrap "gui/$(id -u)" "$LA/com.kira.$n.plist"; done
  echo "loaded."
else
  echo "To load, run:"; printf '  %s\n' "${cmds[@]}"
fi
echo "Note: copy a new bin/kira here (re-run this script) after each make build. Back up LEDGER_KEY."
