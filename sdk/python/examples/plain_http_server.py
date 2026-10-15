"""Expose a plain local HTTP server (no frameworks) and print the URL."""

from __future__ import annotations

import http.server
import threading
import time
from functools import partial
from pathlib import Path

from dreamtunnel import Tunnel

ROOT = Path(__file__).parent


class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(ROOT), **kwargs)


if __name__ == "__main__":
    port = 8099
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", port), partial(Handler))
    threading.Thread(target=httpd.serve_forever, daemon=True).start()

    with Tunnel(port=port, subdomain="files") as tun:
        print(f"local : http://127.0.0.1:{port}/")
        print(f"public: {tun.url}/")
        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            pass
