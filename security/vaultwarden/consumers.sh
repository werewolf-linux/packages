#!/bin/sh
set -eu
work=$(mktemp -d)
export DATA_FOLDER=$work WEB_VAULT_ENABLED=false ROCKET_ADDRESS=127.0.0.1 ROCKET_PORT=18080
export SIGNUPS_ALLOWED=false DISABLE_ICON_DOWNLOAD=true
./target/release/vaultwarden > "$work/server.log" 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; rm -rf "$work"' EXIT
for attempt in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:18080/alive > "$work/alive"; then break; fi
    if ! kill -0 "$pid"; then cat "$work/server.log"; exit 1; fi
    sleep 1
done
test -s "$work/alive"
curl -fsS http://127.0.0.1:18080/api/config > "$work/config"
grep '"object":"config"' "$work/config"
test -s "$work/db.sqlite3"
echo 'PASS: Vaultwarden starts, migrates SQLite and serves client configuration'
