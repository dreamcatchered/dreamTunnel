#!/usr/bin/env bash
# dt-e2e.sh <control_addr> <edge_addr> <public_token> <tag> <master_secret>
# End-to-end check: local HTTP app -> dt client -> relay -> public URL.
set -u

CTRL="${1:?control addr}"; EDGE="${2:?edge addr}"; PUB="${3:?public token}"
TAG="${4:-test}"; MASTER="${5:?master secret}"
EPORT="${EDGE##*:}"

WORK="/tmp/dte2e-$TAG"
rm -rf "$WORK"; mkdir -p "$WORK/home"
BIN=/tmp/dt-e2e
LPORT=18080
FAIL=0

ok()   { echo "  [PASS] $1"; }
bad()  { echo "  [FAIL] $1"; FAIL=1; }

cleanup() {
  [ -n "${P1:-}" ] && kill "$P1" 2>/dev/null
  [ -n "${P2:-}" ] && kill "$P2" 2>/dev/null
  [ -n "${P3:-}" ] && kill "$P3" 2>/dev/null
  [ -f "$WORK/py.pid" ] && kill "$(cat "$WORK/py.pid")" 2>/dev/null
  return 0
}
trap cleanup EXIT

echo "== e2e [$TAG] ctrl=$CTRL edge=$EDGE =="

echo "HELLO-$TAG" > "$WORK/index.html"
( cd "$WORK" && exec python3 -m http.server "$LPORT" --bind 127.0.0.1 >/dev/null 2>&1 ) &
echo $! > "$WORK/py.pid"
sleep 1

# --- 1. zero-config client: no token, no config file ---------------------
echo "-- 1) zero-config (baked-in public token)"
HOME="$WORK/home" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out1.json" 2>"$WORK/err1.txt" &
P1=$!
for _ in $(seq 1 60); do grep -q '"event":"ready"' "$WORK/out1.json" 2>/dev/null && break; sleep 0.25; done

if grep -q '"event":"ready"' "$WORK/out1.json"; then
  ok "client reported ready without any configuration"
else
  bad "client never reported ready"; sed -n '1,10p' "$WORK/out1.json" "$WORK/err1.txt"
fi

URL=$(sed -n 's/.*"url":"\([^"]*\)".*/\1/p' "$WORK/out1.json" | head -1)
HOST="${URL#https://}"
echo "   url=$URL host=$HOST"

if [ -n "$HOST" ] && printf '%s' "$HOST" | grep -q '\.maiuni\.online$'; then
  ok "public URL issued: $URL"
else
  bad "no usable public URL (got '$URL')"
fi

# --- 2. the public URL actually serves the local app ---------------------
echo "-- 2) fetch through the tunnel"
BODY=$(curl -sS --max-time 15 --resolve "$HOST:$EPORT:127.0.0.1" "https://$HOST:$EPORT/" 2>"$WORK/curl.err" | tr -d '\r\n')
if [ "$BODY" = "HELLO-$TAG" ]; then
  ok "tunnel end-to-end works (got '$BODY')"
else
  bad "unexpected body '$BODY'"; sed -n '1,5p' "$WORK/curl.err"
fi

# --- 3. reconnect produces the same host --------------------------------
echo "-- 3) reconnect keeps the host"
kill "$P1" 2>/dev/null; wait "$P1" 2>/dev/null; P1=""
HOME="$WORK/home" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out3.json" 2>/dev/null &
P1=$!
for _ in $(seq 1 60); do grep -q '"event":"ready"' "$WORK/out3.json" 2>/dev/null && break; sleep 0.25; done
HOST3=$(sed -n 's/.*"host":"\([^"]*\)".*/\1/p' "$WORK/out3.json" | head -1)
if [ "$HOST3" = "$HOST" ]; then
  ok "same host after restart ($HOST3)"
else
  bad "host changed: '$HOST' -> '$HOST3'"
fi
kill "$P1" 2>/dev/null; wait "$P1" 2>/dev/null; P1=""

# --- 4. garbage token is rejected ---------------------------------------
echo "-- 4) garbage token rejected"
HOME="$WORK/home" DT_TOKEN=garbage-token-not-valid "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out4.json" 2>/dev/null &
P2=$!
for _ in $(seq 1 40); do grep -q '"event":"error"' "$WORK/out4.json" 2>/dev/null && break; sleep 0.25; done
if grep -q '"event":"error"' "$WORK/out4.json" && grep -qi 'auth' "$WORK/out4.json"; then
  ok "bad token denied by server"
else
  bad "bad token was not denied"; sed -n '1,5p' "$WORK/out4.json"
fi
kill "$P2" 2>/dev/null; wait "$P2" 2>/dev/null; P2=""

# --- 5. master secret still works ---------------------------------------
echo "-- 5) master secret still authenticates"
HOME="$WORK/home2" DT_TOKEN="$MASTER" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out5.json" 2>/dev/null &
P3=$!
for _ in $(seq 1 60); do grep -q '"event":"ready"' "$WORK/out5.json" 2>/dev/null && break; sleep 0.25; done
if grep -q '"event":"ready"' "$WORK/out5.json"; then
  ok "master token accepted (no regression)"
else
  bad "master token broke"; sed -n '1,5p' "$WORK/out5.json"
fi
kill "$P3" 2>/dev/null; wait "$P3" 2>/dev/null; P3=""

echo "== result: $([ $FAIL -eq 0 ] && echo ALL-PASS || echo SOME-FAILED) =="
exit $FAIL
