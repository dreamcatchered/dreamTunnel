#!/usr/bin/env bash
# dt-e2e.sh <control_addr> <edge_addr> <enroll_token> <tag> <master_secret> [server_conf]
#
# End-to-end check: local HTTP app -> dt client -> relay -> public URL, plus
# per-device token enrollment. The optional server_conf enables the device
# management checks (revoke/restore/list), which need the admin panel of the
# instance under test.
set -u

CTRL="${1:?control addr}"; EDGE="${2:?edge addr}"; PUB="${3:?enroll token}"
TAG="${4:-test}"; MASTER="${5:?master secret}"; SCONF="${6:-}"
EPORT="${EDGE##*:}"

WORK="/tmp/dte2e-$TAG"
rm -rf "$WORK"; mkdir -p "$WORK/home"
BIN="${DT_BIN:-/tmp/dt-e2e}"
LPORT=18080
FAIL=0

ok()   { echo "  [PASS] $1"; }
bad()  { echo "  [FAIL] $1"; FAIL=1; }

cleanup() {
  [ -n "${P1:-}" ] && kill "$P1" 2>/dev/null
  [ -n "${P2:-}" ] && kill "$P2" 2>/dev/null
  [ -n "${P3:-}" ] && kill "$P3" 2>/dev/null
  [ -n "${P4:-}" ] && kill "$P4" 2>/dev/null
  [ -f "$WORK/py.pid" ] && kill "$(cat "$WORK/py.pid")" 2>/dev/null
  return 0
}
trap cleanup EXIT

# admin panel of the instance under test (for the device checks)
ADMIN=""; AUSER="admin"; APASS=""
if [ -n "$SCONF" ] && [ -r "$SCONF" ]; then
  ADMIN="$(sed -n 's/^DT_ADMIN_ADDR=//p' "$SCONF" | head -1)"; ADMIN="${ADMIN:-127.0.0.1:8484}"
  AUSER="$(sed -n 's/^DT_ADMIN_USER=//p' "$SCONF" | head -1)"; AUSER="${AUSER:-admin}"
  APASS="$(sed -n 's/^DT_ADMIN_PASS=//p' "$SCONF" | head -1)"
fi

echo "== e2e [$TAG] ctrl=$CTRL edge=$EDGE bin=$BIN admin=${ADMIN:-none} =="

echo "HELLO-$TAG" > "$WORK/index.html"
( cd "$WORK" && exec python3 -m http.server "$LPORT" --bind 127.0.0.1 >/dev/null 2>&1 ) &
echo $! > "$WORK/py.pid"
sleep 1

wait_ready() { # wait_ready <file> <seconds*4>
  for _ in $(seq 1 "$2"); do grep -q '"event":"ready"' "$1" 2>/dev/null && return 0; sleep 0.25; done
  return 1
}

# --- 1. zero-config client: no token, no config file ---------------------
echo "-- 1) zero-config (baked-in enrollment token)"
HOME="$WORK/home" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out1.json" 2>"$WORK/err1.txt" &
P1=$!
if wait_ready "$WORK/out1.json" 80; then
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

# --- 3. enrollment: the device got its own token -------------------------
echo "-- 3) the device was enrolled and its token stored"
CFG="$WORK/home/.dreamtunnel/config.env"
DEV1=$(sed -n 's/.*"device":"\([^"]*\)".*/\1/p' "$WORK/out1.json" | head -1)
if grep -q '"event":"enrolled"' "$WORK/out1.json"; then
  ok "server issued a device token on the first run"
else
  bad "no enrolled event"; sed -n '1,10p' "$WORK/out1.json" "$WORK/err1.txt"
fi
if [ -n "$DEV1" ]; then
  ok "device id reported: $DEV1"
else
  bad "no device id in the client events"
fi
if [ -f "$CFG" ] && grep -q '^DT_TOKEN=..*' "$CFG"; then
  ok "device token stored in $CFG"
else
  bad "the device token was not stored"; sed -n '1,10p' "$CFG" 2>/dev/null
fi

# --- 4. reconnect keeps the host and does not re-enroll ------------------
echo "-- 4) reconnect keeps the host, reuses the token"
kill "$P1" 2>/dev/null; wait "$P1" 2>/dev/null; P1=""
HOME="$WORK/home" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out4.json" 2>/dev/null &
P1=$!
if wait_ready "$WORK/out4.json" 80; then
  HOST4=$(sed -n 's/.*"host":"\([^"]*\)".*/\1/p' "$WORK/out4.json" | head -1)
  [ "$HOST4" = "$HOST" ] && ok "same host after restart ($HOST4)" || bad "host changed: '$HOST' -> '$HOST4'"
  if grep -q '"event":"enrolled"' "$WORK/out4.json"; then
    bad "the client enrolled again instead of reusing its token"
  else
    ok "no second enrollment — the stored token was reused"
  fi
  DEV4=$(sed -n 's/.*"device":"\([^"]*\)".*/\1/p' "$WORK/out4.json" | head -1)
  [ -n "$DEV1" ] && [ "$DEV4" = "$DEV1" ] && ok "same device id ($DEV4)" || bad "device id changed: '$DEV1' -> '$DEV4'"
else
  bad "client never came back up"
fi
kill "$P1" 2>/dev/null; wait "$P1" 2>/dev/null; P1=""

# --- 5. garbage token is rejected ---------------------------------------
echo "-- 5) garbage token rejected"
HOME="$WORK/home" DT_TOKEN=garbage-token-not-valid "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out5.json" 2>/dev/null &
P2=$!
for _ in $(seq 1 40); do grep -q '"event":"error"' "$WORK/out5.json" 2>/dev/null && break; sleep 0.25; done
if grep -q '"event":"error"' "$WORK/out5.json" && grep -qi 'auth' "$WORK/out5.json"; then
  ok "bad token denied by server"
else
  bad "bad token was not denied"; sed -n '1,5p' "$WORK/out5.json"
fi
kill "$P2" 2>/dev/null; wait "$P2" 2>/dev/null; P2=""

# --- 6. a forged device token is rejected -------------------------------
echo "-- 6) forged device token rejected"
HOME="$WORK/home3" DT_TOKEN="dtd_deadbeef_00000000000000000000000000000000" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out6.json" 2>/dev/null &
P2=$!
for _ in $(seq 1 40); do grep -q '"event":"error"' "$WORK/out6.json" 2>/dev/null && break; sleep 0.25; done
if grep -q '"event":"error"' "$WORK/out6.json"; then
  ok "unknown device id refused"
else
  bad "a made-up device token was accepted"; sed -n '1,5p' "$WORK/out6.json"
fi
kill "$P2" 2>/dev/null; wait "$P2" 2>/dev/null; P2=""

# --- 7. master secret still works ---------------------------------------
echo "-- 7) master secret still authenticates"
HOME="$WORK/home2" DT_TOKEN="$MASTER" "$BIN" http "$LPORT" --server "$CTRL" --json \
  > "$WORK/out7.json" 2>/dev/null &
P3=$!
if wait_ready "$WORK/out7.json" 80; then
  ok "master token accepted (no regression)"
else
  bad "master token broke"; sed -n '1,5p' "$WORK/out7.json"
fi
kill "$P3" 2>/dev/null; wait "$P3" 2>/dev/null; P3=""

# --- 8..10. device management through the admin panel -------------------
if [ -z "$ADMIN" ] || [ -z "$APASS" ]; then
  echo "-- device management checks skipped (no server_conf with admin credentials)"
else
  COOKIE="$WORK/cookie"
  curl -sS -c "$COOKIE" -H 'Content-Type: application/json' \
    -d "{\"user\":\"$AUSER\",\"pass\":\"$APASS\"}" "http://$ADMIN/api/login" > "$WORK/login.json" 2>/dev/null
  if grep -q '"ok"' "$WORK/login.json"; then
    ok "admin login on $ADMIN"
  else
    bad "admin login failed: $(cat "$WORK/login.json" 2>/dev/null)"
  fi

  echo "-- 8) the device list shows the enrolled device"
  curl -sS -b "$COOKIE" "http://$ADMIN/api/state" > "$WORK/state.json" 2>/dev/null
  if grep -q "\"id\":\"$DEV1\"" "$WORK/state.json"; then
    ok "device $DEV1 is listed in the admin panel"
  else
    bad "device $DEV1 is missing from the admin panel"
  fi

  echo "-- 9) revoking a device closes it out"
  HOME="$WORK/home" "$BIN" http "$LPORT" --server "$CTRL" --json > "$WORK/out9.json" 2>/dev/null &
  P4=$!
  if wait_ready "$WORK/out9.json" 80; then
    ok "device tunnel is up before the revoke"
  else
    bad "device tunnel did not come up"; sed -n '1,5p' "$WORK/out9.json"
  fi
  REV=$(curl -sS -b "$COOKIE" -H 'Content-Type: application/json' \
    -d "{\"id\":\"$DEV1\"}" "http://$ADMIN/api/device/revoke" 2>/dev/null)
  if printf '%s' "$REV" | grep -q '"ok":"1"'; then
    ok "device revoked ($REV)"
  else
    bad "revoke failed: $REV"
  fi
  for _ in $(seq 1 40); do grep -q '"event":"error"' "$WORK/out9.json" 2>/dev/null && break; sleep 0.25; done
  if grep -q '"event":"error"' "$WORK/out9.json"; then
    ok "the revoked device is refused and its tunnel dropped"
  else
    bad "the revoked device kept working"; sed -n '1,5p' "$WORK/out9.json"
  fi
  kill "$P4" 2>/dev/null; wait "$P4" 2>/dev/null; P4=""

  echo "-- 10) restoring the device gives it access back"
  RES=$(curl -sS -b "$COOKIE" -H 'Content-Type: application/json' \
    -d "{\"id\":\"$DEV1\"}" "http://$ADMIN/api/device/restore" 2>/dev/null)
  printf '%s' "$RES" | grep -q '"ok":"1"' || bad "restore failed: $RES"
  HOME="$WORK/home" "$BIN" http "$LPORT" --server "$CTRL" --json > "$WORK/out10.json" 2>/dev/null &
  P4=$!
  if wait_ready "$WORK/out10.json" 80; then
    ok "the restored device works again"
  else
    bad "the restored device still cannot connect"; sed -n '1,5p' "$WORK/out10.json"
  fi
  kill "$P4" 2>/dev/null; wait "$P4" 2>/dev/null; P4=""

  echo "-- 11) dt devices list / dt enroll"
  if "$BIN" devices list --config "$SCONF" 2>/dev/null | grep -q "$DEV1"; then
    ok "dt devices list shows the device"
  else
    bad "dt devices list did not show $DEV1"
  fi
  if HOME="$WORK/home4" "$BIN" enroll --server "$CTRL" 2>/dev/null | grep -q 'зарегистрировано'; then
    ok "dt enroll registers a device on demand"
  else
    bad "dt enroll did not register a device"
  fi
fi

echo "== result: $([ $FAIL -eq 0 ] && echo ALL-PASS || echo SOME-FAILED) =="
exit $FAIL
