"""Exceptions raised by the dreamTunnel SDK."""

from __future__ import annotations


class DreamTunnelError(Exception):
    """Base class for every dreamTunnel error."""


class BinaryNotFound(DreamTunnelError):
    """The ``dt`` executable could not be found and auto-install is off."""


class TunnelTimeout(DreamTunnelError):
    """The relay did not report the tunnel as online in time."""


class TunnelExited(DreamTunnelError):
    """The ``dt`` process exited before the tunnel came online."""

    def __init__(self, message: str, *, code: int | None = None, output: str = ""):
        super().__init__(message)
        self.code = code
        self.output = output


class TunnelError(DreamTunnelError):
    """The relay refused the tunnel (bad token, rejected subdomain, ...)."""

    def __init__(self, message: str, *, raw: str = ""):
        super().__init__(message)
        self.raw = raw
