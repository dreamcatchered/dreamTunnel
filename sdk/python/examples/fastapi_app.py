"""FastAPI + uvicorn exposed with dreamTunnel.

    pip install fastapi uvicorn dreamtunnel
    python fastapi_app.py
"""

from __future__ import annotations

import threading
import time

import uvicorn
from fastapi import FastAPI

from dreamtunnel import Tunnel

app = FastAPI()


@app.get("/")
def index() -> dict:
    return {"hello": "from dreamTunnel"}


def serve(port: int = 8000) -> None:
    uvicorn.run(app, host="127.0.0.1", port=port, log_level="warning")


if __name__ == "__main__":
    port = 8000
    threading.Thread(target=serve, args=(port,), daemon=True).start()
    time.sleep(1.0)

    with Tunnel(port=port) as tun:
        print(f"public URL: {tun.url}")
        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            pass
