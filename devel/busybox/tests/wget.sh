#!/bin/sh
set -eu
work=$(mktemp -d)
server=
trap 'test -z "$server" || kill "$server" 2>/dev/null || true; rm -rf "$work"' EXIT
printf '%s\n' 'wget test content' > "$work/expected"
# Serve one real HTTP response with BusyBox itself. No unrelated Python
# runtime or threaded server is needed to test the wget applet.
{
    printf 'HTTP/1.0 200 OK\r\nContent-Length: %s\r\nConnection: close\r\n\r\n' "$(wc -c < "$work/expected")"
    cat "$work/expected"
} | /usr/bin/busybox nc -l -p 8888 -w 10 > "$work/request" 2> "$work/server.log" &
server=$!
sleep 1
if ! /usr/bin/busybox wget -T 10 -O "$work/downloaded" http://127.0.0.1:8888/test.txt; then
    cat "$work/server.log"
    exit 1
fi
wait "$server"
server=
cmp "$work/expected" "$work/downloaded"
grep -F 'GET /test.txt ' "$work/request"
echo 'PASS: wget successfully downloaded the local HTTP response'
