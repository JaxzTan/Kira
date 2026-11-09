#!/usr/bin/env bash
# launchd/systemd wrapper: cd to the install root and exec the server. The Go binary loads .env itself.
# Layout: repo checkout, or a runtime copy (KIRA_HOME, see scripts/install-macos.sh) holding bin/ scripts/ .env data/.
set -eu
if [ -n "${KIRA_HOME:-}" ] && [ -x "$KIRA_HOME/bin/kira" ]; then
  cd "$KIRA_HOME"
else
  cd "$(dirname "$0")/.."
fi
[ -x bin/kira ] || { echo "bin/kira missing: run make build" >&2; exit 1; }
umask 077
exec bin/kira serve
