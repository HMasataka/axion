#!/usr/bin/env bash
set -euo pipefail

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

PSK="$TMPDIR/psk"
echo "smoke-test-secret" > "$PSK"
mkdir -p "$TMPDIR/server-data" "$TMPDIR/clientA" "$TMPDIR/clientB"

# Build
CGO_ENABLED=0 go build -o "$TMPDIR/axion" ./cmd/axion/

# Start server
"$TMPDIR/axion" server \
    --bind 127.0.0.1:18999 \
    --data-dir "$TMPDIR/server-data" \
    --psk-file "$PSK" \
    > "$TMPDIR/server.log" 2>&1 &
SERVER_PID=$!
sleep 2

# Start clients
"$TMPDIR/axion" client \
    --server ws://127.0.0.1:18999 \
    --root "$TMPDIR/clientA" \
    --id-file "$TMPDIR/clientA-id" \
    --psk-file "$PSK" \
    > "$TMPDIR/clientA.log" 2>&1 &
CA_PID=$!

"$TMPDIR/axion" client \
    --server ws://127.0.0.1:18999 \
    --root "$TMPDIR/clientB" \
    --id-file "$TMPDIR/clientB-id" \
    --psk-file "$PSK" \
    > "$TMPDIR/clientB.log" 2>&1 &
CB_PID=$!

sleep 3

# Verify clients registered
CLIENTS=$(sqlite3 "$TMPDIR/server-data/axion.db" "SELECT COUNT(*) FROM clients WHERE status = 'online';")
if [ "$CLIENTS" != "2" ]; then
    echo "FAIL: expected 2 online clients, got $CLIENTS"
    cat "$TMPDIR/server.log"
    cat "$TMPDIR/clientA.log"
    cat "$TMPDIR/clientB.log"
    kill $SERVER_PID $CA_PID $CB_PID 2>/dev/null
    exit 1
fi

echo "Smoke test passed: server + 2 clients connected"

# Cleanup
kill -TERM $SERVER_PID $CA_PID $CB_PID 2>/dev/null
wait 2>/dev/null
