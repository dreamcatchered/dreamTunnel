"""Python SDK for dreamTunnel — instant public HTTPS tunnels.

Typical use:

    from dreamtunnel import Tunnel

    with Tunnel(port=8080) as tun:
        print(tun.url)          # https://k7f3m9ab.maiuni.online
        ...

The SDK supervises the existing ``dt`` CLI process: it starts the tunnel,
waits until the relay reports it online, exposes the public URL, forwards the
relay's events (including reconnects) to your code, and shuts the process down
cleanly on exit.
"""

from .binary import ensure_dt, install_dt
from .errors import (
    BinaryNotFound,
    DreamTunnelError,
    TunnelError,
    TunnelExited,
    TunnelTimeout,
)
from .tunnel import DEFAULT_SERVER, Tunnel

__version__ = "0.1.1"

__all__ = [
    "Tunnel",
    "DEFAULT_SERVER",
    "ensure_dt",
    "install_dt",
    "DreamTunnelError",
    "BinaryNotFound",
    "TunnelTimeout",
    "TunnelExited",
    "TunnelError",
    "__version__",
]
