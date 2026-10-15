"""Flask + dreamTunnel in one script.

    pip install flask dreamtunnel
    python flask_app.py

Starts the local Flask server in a background thread and exposes it through
dreamTunnel. The public URL is printed as soon as the relay accepts it, and
the tunnel is shut down cleanly when you press Ctrl+C.
"""

from __future__ import annotations

import threading
import time

from flask import Flask

from dreamtunnel import Tunnel

app = Flask(__name__)


@app.route("/")
def index() -> str:
    return "<h1>работает через dreamTunnel!</h1>"


@app.route("/health")
def health() -> dict:
    return {"ok": True}


def serve(port: int = 8080) -> None:
    app.run(host="127.0.0.1", port=port, use_reloader=False, threaded=True)


if __name__ == "__main__":
    port = 8080
    threading.Thread(target=serve, args=(port,), daemon=True).start()
    time.sleep(0.5)  # let Flask bind the port before the tunnel dials it

    with Tunnel(port=port, subdomain="my-api") as tun:
        print(f"\n  Открыто: {tun.url}\n  Ctrl+C — остановить\n")
        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            pass
