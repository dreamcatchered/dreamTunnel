#!/usr/bin/env python3
"""Fake ``dt`` used by the test suite: same CLI surface, scripted behaviour.

Behaviour is driven by environment variables so a single file can cover the
happy path, failures, reconnects and the session handling the SDK relies on:

    FAKE_DT_BEHAVIOUR   ready (default) | fail | denied | slow | flap
    FAKE_DT_LIFETIME    seconds to stay alive after reporting ready
    FAKE_DT_IGNORE_JSON makes it behave like an older binary (no --json)
    FAKE_DT_ENVDUMP_DIR directory to dump the DT_* environment into
    FAKE_DT_ENROLL      emit the enrolled event and a device id (new dt)
"""

from __future__ import annotations

import json
import os
import sys
import time

args = sys.argv[1:]
mode = args[0]
port = args[1]
sub = args[2] if len(args) > 2 and not args[2].startswith("-") else "fake"
json_mode = "--json" in args
if os.environ.get("FAKE_DT_IGNORE_JSON"):
    json_mode = False  # simulate an old binary without --json

behaviour = os.environ.get("FAKE_DT_BEHAVIOUR", "ready")
host = f"{sub}.maiuni.online"
local = f"http://127.0.0.1:{port}"

dump_dir = os.environ.get("FAKE_DT_ENVDUMP_DIR")
if dump_dir:
    os.makedirs(dump_dir, exist_ok=True)
    with open(os.path.join(dump_dir, f"env-{os.getpid()}.json"), "w") as fh:
        json.dump({k: v for k, v in os.environ.items() if k.startswith("DT_")}, fh)


def emit(event: dict) -> None:
    print(json.dumps(event), flush=True)


# Mirror dt: create the config file (with a session id) when it is missing.
cfg_path = os.environ.get("DT_CONFIG")
if cfg_path and not os.path.exists(cfg_path):
    parent = os.path.dirname(cfg_path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(cfg_path, "w") as fh:
        fh.write(f"DT_SERVER={os.environ.get('DT_SERVER', '')}\n")
        fh.write(f"DT_SUB={sub}\n")
        fh.write(f"DT_SESS={os.urandom(8).hex()}\n")


if behaviour == "fail":
    print("[dt] error: no token: run `dt login` first (or set DT_TOKEN)", file=sys.stderr)
    sys.exit(2)

if behaviour == "denied":
    if json_mode:
        emit({"event": "error", "error": "server denied: auth failed", "retry_in": "1s"})
    else:
        print("[dt] connection lost (server denied: auth failed), retrying in 1s",
              file=sys.stderr)
    time.sleep(60)
    sys.exit(0)

if behaviour == "slow":
    time.sleep(60)
    sys.exit(0)

# The relay issues a per-device token on the first run; dt reports it as an
# `enrolled` event and carries the device id in `ready` from then on.
device = "f00dbabe" if os.environ.get("FAKE_DT_ENROLL") and json_mode else None
if device:
    emit({"event": "enrolled", "device": device, "label": "test-host",
          "config": cfg_path or ""})


def ready_event(reconnect: bool) -> dict:
    ev = {"event": "ready", "url": f"https://{host}", "host": host,
          "local": local, "mode": mode, "reconnect": reconnect}
    if device:
        ev["device"] = device
    return ev


if json_mode:
    emit(ready_event(False))
else:
    print(f"  Forwarding   https://{host} -> {local}", flush=True)
    print("  Press Ctrl+C to stop", flush=True)

if behaviour == "flap":
    time.sleep(0.3)
    if json_mode:
        emit({"event": "disconnected", "error": "connection lost", "retry_in": "1s"})
        emit(ready_event(True))

time.sleep(float(os.environ.get("FAKE_DT_LIFETIME", "30")))
sys.exit(0)
