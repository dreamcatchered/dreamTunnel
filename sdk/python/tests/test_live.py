"""End-to-end test against the real relay.

Skipped by default; enable with::

    DREAMTUNNEL_LIVE=1 pytest tests/test_live.py -v

The test starts a local HTTP server, tunnels it, fetches the public URL and
checks the body, then repeats after a restart to prove the subdomain sticks.
"""

from __future__ import annotations

import http.server
import os
import threading
import urllib.request
from functools import partial
from pathlib import Path

import pytest

from dreamtunnel import Tunnel

pytestmark = pytest.mark.skipif(
    os.environ.get("DREAMTUNNEL_LIVE") != "1",
    reason="set DREAMTUNNEL_LIVE=1 to run against the real relay",
)

BODY = b"dreamtunnel-sdk-live-test"


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

    def log_message(self, *args: object) -> None:  # silence
        return


@pytest.fixture()
def local_server(tmp_path: Path):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), partial(Handler))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield server.server_port
    server.shutdown()


def fetch(url: str, timeout: float = 20.0) -> bytes:
    with urllib.request.urlopen(url, timeout=timeout) as resp:  # noqa: S310
        return resp.read()


def test_tunnel_serves_the_local_app(local_server: int) -> None:
    with Tunnel(port=local_server, subdomain=os.environ.get("DREAMTUNNEL_SUB")) as tun:
        assert tun.url and tun.url.startswith("https://")
        assert fetch(tun.url + "/") == BODY


def test_subdomain_survives_a_restart(local_server: int) -> None:
    tun = Tunnel(port=local_server, subdomain=os.environ.get("DREAMTUNNEL_SUB"))
    tun.start()
    try:
        first = tun.url
        assert fetch(first + "/") == BODY
    finally:
        tun.stop()

    tun.start()
    try:
        assert tun.url == first
        assert fetch(tun.url + "/") == BODY
    finally:
        tun.stop()
