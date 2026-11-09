#!/usr/bin/env bash
# launchd/systemd wrapper: cd to repo and exec the server. The Go binary loads .env itself.
set -eu
cd "$(dirname "$0")/.."
[ -x bin/kira ] || { echo "bin/kira missing: run make build" >&2; exit 1; }
umask 077
exec bin/kira serve
