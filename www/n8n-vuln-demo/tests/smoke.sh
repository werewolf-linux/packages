#!/bin/sh
set -eu

root=${N8N_PACKAGE_ROOT:-$(pwd)}
probe=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/smoke.cjs
home=$(mktemp -d)
export HOME="$home" N8N_USER_FOLDER="$home" TMPDIR="$home"
export N8N_LISTEN_ADDRESS=127.0.0.1
export N8N_DIAGNOSTICS_ENABLED=false N8N_VERSION_NOTIFICATIONS_ENABLED=false
export N8N_TEMPLATES_ENABLED=false N8N_COMMUNITY_PACKAGES_ENABLED=false
export N8N_RUNNERS_ENABLED=false N8N_ENFORCE_SETTINGS_FILE_PERMISSIONS=true
N8N_PORT=$(node --no-node-snapshot "$probe" --port)
export N8N_PORT

cd "$root"
# The harness launches processes; the application does not need node:exec.
node --no-node-snapshot node_modules/n8n/bin/n8n start >"$home/server.log" 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null || :; wait "$server" 2>/dev/null || :; rm -rf "$home"' EXIT
if ! node --no-node-snapshot "$probe"; then
  cat "$home/server.log"
  exit 1
fi
