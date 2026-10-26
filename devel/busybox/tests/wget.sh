#!/bin/sh
set -eu
work=$(mktemp -d)
server=
trap 'test -z "$server" || kill "$server" 2>/dev/null || true; rm -rf "$work"' EXIT
printf '%s\n' 'wget test content' > "$work/expected"
# Serve one real HTTP response with BusyBox itself. No unrelated Python
# runtime or threaded server is needed to test the wget applet.
# Bubblewrap shares the host network, so a fixed port can belong to another job.
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    port=$((20000 + $(/usr/bin/busybox od -An -N2 -tu2 /dev/urandom) % 40000))
    {
        printf 'HTTP/1.0 200 OK\r\nContent-Length: %s\r\nConnection: close\r\n\r\n' "$(wc -c < "$work/expected")"
        cat "$work/expected"
    } | /usr/bin/busybox nc -l -p "$port" -w 10 > "$work/request" 2> "$work/server.log" &
    server=$!
    sleep 1
    if kill -0 "$server" 2>/dev/null; then
        break
    fi
    wait "$server" || true
    server=
    if ! grep -F 'Address already in use' "$work/server.log"; then
        cat "$work/server.log"
        exit 1
    fi
    echo "Port $port was occupied on attempt $attempt; retrying" >&2
done
if [ -z "$server" ]; then
    echo 'Could not bind the local HTTP fixture' >&2
    exit 1
fi
if ! /usr/bin/busybox wget -T 10 -O "$work/downloaded" "http://127.0.0.1:$port/test.txt"; then
    cat "$work/server.log"
    exit 1
fi
wait "$server"
server=
cmp "$work/expected" "$work/downloaded"
grep -F 'GET /test.txt ' "$work/request"
echo 'PASS: wget successfully downloaded the local HTTP response'
