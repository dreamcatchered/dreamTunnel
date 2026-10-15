"""Supervise a ``dt`` tunnel process from Python."""

from __future__ import annotations

import itertools
import json
import os
import re
import subprocess
import threading
import time
from collections import deque
from pathlib import Path
from typing import Any, Callable, Iterable, Sequence

from .binary import DEFAULT_BASE_URL, cache_root, ensure_dt
from .errors import (
    BinaryNotFound,
    DreamTunnelError,
    TunnelError,
    TunnelExited,
    TunnelTimeout,
)

DEFAULT_SERVER = "tunnel.swag.best:444"

# Each Tunnel gets its own dt config file (and therefore its own session id),
# otherwise two tunnels started from one machine would fight over the single
# session stored in ~/.dreamtunnel/config.env.
_SEQ = itertools.count(1)
_config_lock = threading.Lock()

# "  Forwarding   https://abc.maiuni.online -> http://127.0.0.1:8080"
_FORWARD_RE = re.compile(r"Forwarding\s+(\S+)\s+->\s+(\S+)")
# "[dt] connection lost (server denied: auth failed), retrying in 2s"
_LOST_RE = re.compile(r"connection lost \((?P<err>[^)]*)\)")
# "[dt] error: no token: ..."
_ERR_RE = re.compile(r"\[dt\] error: (?P<err>.+)")

EventCallback = Callable[[dict[str, Any]], None]


class Tunnel:
    """A running dreamTunnel tunnel.

    Args:
        port: local port to expose.
        subdomain: requested subdomain label (``my-api`` ->
            ``https://my-api.maiuni.online``). Random when omitted.
        mode: ``"http"`` (default) or ``"tcp"``.
        server: control endpoint, ``host:port``. Defaults to the public relay.
        token: access token. When omitted the built-in enrollment token is used
            and the device registers itself on the first run, so a fresh
            install works with no configuration.
        dt_binary: explicit path to ``dt``.
        auto_install: download ``dt`` when it cannot be found.
        ready_timeout: seconds to wait for the relay to report the tunnel online.
        on_event: called for every event (``enrolled``, ``ready``,
            ``reconnected``, ``disconnected``, ``error``, ...).
        session: give this tunnel its own dt config file (default ``True``) so
            several tunnels can run from one machine without stealing each
            other's session; ``False`` shares ``~/.dreamtunnel/config.env``.
        config_path: explicit dt config file instead of the private one.

    Example:
        >>> with Tunnel(8080) as tun:
        ...     print(tun.url)
    """

    def __init__(
        self,
        port: int,
        subdomain: str | None = None,
        *,
        mode: str = "http",
        server: str | None = None,
        token: str | None = None,
        dt_binary: str | os.PathLike[str] | None = None,
        auto_install: bool = True,
        ready_timeout: float = 30.0,
        on_event: EventCallback | None = None,
        base_url: str = DEFAULT_BASE_URL,
        session: bool = True,
        config_path: str | os.PathLike[str] | None = None,
    ) -> None:
        if mode not in {"http", "tcp"}:
            raise ValueError("mode must be 'http' or 'tcp'")
        self.port = int(port)
        self.subdomain = subdomain
        self.mode = mode
        self.server = server
        self.token = token
        self.dt_binary = dt_binary
        self.auto_install = auto_install
        self.ready_timeout = ready_timeout
        self.on_event = on_event
        self.base_url = base_url
        self.session = session
        self._config_path = Path(config_path).expanduser() if config_path else None
        self._own_config = False

        self._proc: subprocess.Popen[str] | None = None
        self._lock = threading.RLock()
        self._ready = threading.Event()
        self._events: deque[dict[str, Any]] = deque(maxlen=500)
        self._stderr: deque[str] = deque(maxlen=500)
        self._url: str | None = None
        self._host: str | None = None
        self._local_url: str | None = None
        self._device_id: str | None = None
        self._last_error: str | None = None
        self._exit_code: int | None = None
        self._threads: list[threading.Thread] = []

    # ------------------------------------------------------------------ utils

    def __repr__(self) -> str:  # pragma: no cover - cosmetic
        state = "running" if self.is_running else "stopped"
        return f"<Tunnel {self.mode} {self.port} {state} url={self.url!r}>"

    @property
    def url(self) -> str | None:
        """Public URL, or ``None`` until the tunnel is online."""
        return self._url

    @property
    def host(self) -> str | None:
        """Public hostname without scheme."""
        return self._host

    @property
    def local_url(self) -> str | None:
        """The local endpoint being exposed."""
        return self._local_url

    @property
    def device_id(self) -> str | None:
        """Id the relay assigned to this device (``enrolled``/``ready`` event).

        Every device is registered on its first run and gets a token of its
        own; the id makes it recognisable in the server's admin panel. Older
        ``dt`` builds never report it and this stays ``None``.
        """
        return self._device_id

    @property
    def pid(self) -> int | None:
        """PID of the ``dt`` process."""
        return self._proc.pid if self._proc else None

    @property
    def exit_code(self) -> int | None:
        """Exit code once the process has finished."""
        return self._exit_code

    @property
    def is_running(self) -> bool:
        """True while the ``dt`` process is alive."""
        return self._proc is not None and self._proc.poll() is None

    @property
    def connected(self) -> bool:
        """True once the relay has accepted the tunnel."""
        return self._ready.is_set() and self.is_running

    @property
    def events(self) -> tuple[dict[str, Any], ...]:
        """Events seen so far, oldest first."""
        with self._lock:
            return tuple(self._events)

    @property
    def stderr(self) -> str:
        """Everything the process wrote to stderr."""
        with self._lock:
            return "".join(self._stderr)

    # ------------------------------------------------------------------ build

    def _command(self, binary: str) -> list[str]:
        cmd = [binary, self.mode, str(self.port)]
        if self.subdomain:
            cmd.append(self.subdomain)
        if self.server:
            cmd += ["--server", self.server]
        cmd.append("--json")
        return cmd

    def _environment(self) -> dict[str, str]:
        env = dict(os.environ)
        if self.token:
            env["DT_TOKEN"] = self.token
        if self.server:
            env["DT_SERVER"] = self.server
        if self.subdomain:
            env["DT_SUB"] = self.subdomain
        if self.session:
            env["DT_CONFIG"] = str(self._session_config())
        return env

    def _session_config(self) -> Path:
        """Path of this tunnel's own dt config (it carries the session id)."""
        if self._config_path is None:
            with _config_lock:
                n = next(_SEQ)
            self._config_path = cache_root() / "sessions" / f"tun-{os.getpid()}-{n}.env"
            self._own_config = True
        return self._config_path

    # ------------------------------------------------------------------ state

    def _record(self, event: dict[str, Any]) -> None:
        kind = str(event.get("event", ""))

        if kind == "ready":
            with self._lock:
                self._url = event.get("url") or self._url
                self._host = event.get("host") or self._host
                self._local_url = event.get("local") or self._local_url
                self._device_id = event.get("device") or self._device_id
                if event.get("reconnect"):
                    event = {**event, "event": "reconnected"}
            self._ready.set()
        elif kind == "enrolled":
            # the relay issued this device a token of its own on this run
            with self._lock:
                self._device_id = event.get("device") or self._device_id
        elif kind in {"error", "disconnected"}:
            with self._lock:
                self._last_error = str(event.get("error", "")) or self._last_error
            if kind == "disconnected":
                self._ready.clear()

        with self._lock:
            self._events.append(event)

        if self.on_event is not None:
            try:
                self.on_event(event)
            except Exception:  # pragma: no cover - user callback must not kill us
                pass

    # ------------------------------------------------------------------ io

    def _read_stdout(self) -> None:
        proc = self._proc
        assert proc is not None and proc.stdout is not None
        for raw in proc.stdout:
            line = raw.strip()
            if not line:
                continue
            if line.startswith("{"):
                try:
                    self._record(json.loads(line))
                    continue
                except json.JSONDecodeError:
                    pass
            m = _FORWARD_RE.search(line)
            if m:
                url, local = m.group(1), m.group(2)
                self._record(
                    {
                        "event": "ready",
                        "url": url,
                        "host": url.split("://", 1)[-1],
                        "local": local,
                        "mode": self.mode,
                    }
                )
        with self._lock:
            self._exit_code = proc.poll()

    def _read_stderr(self) -> None:
        proc = self._proc
        assert proc is not None and proc.stderr is not None
        for raw in proc.stderr:
            line = raw.rstrip("\n")
            with self._lock:
                self._stderr.append(line + "\n")
            m = _LOST_RE.search(line)
            if m:
                self._record({"event": "disconnected", "error": m.group("err")})
                continue
            m = _ERR_RE.search(line)
            if m:
                self._record({"event": "error", "error": m.group("err")})

    # ------------------------------------------------------------------ lifecycle

    def start(self, timeout: float | None = None) -> "Tunnel":
        """Start the tunnel and block until it is online.

        Returns:
            self, so ``Tunnel(...).start().url`` works.

        Raises:
            BinaryNotFound: ``dt`` is missing and auto-install is off.
            TunnelExited: the process died before the tunnel came online.
            TunnelError: the relay refused the tunnel.
            TunnelTimeout: the relay did not answer in time.
        """
        if self.is_running:
            self.wait_ready(timeout)
            return self

        binary = ensure_dt(self.dt_binary, auto_install=self.auto_install, base_url=self.base_url)

        with self._lock:
            self._ready.clear()
            self._url = None
            self._host = None
            self._local_url = None
            self._device_id = None
            self._last_error = None
            self._exit_code = None
            self._events.clear()
            self._stderr.clear()

        self._proc = subprocess.Popen(  # noqa: S603
            self._command(binary),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=self._environment(),
            text=True,
            bufsize=1,
        )
        self._threads = [
            threading.Thread(target=self._read_stdout, name="dt-stdout", daemon=True),
            threading.Thread(target=self._read_stderr, name="dt-stderr", daemon=True),
        ]
        for t in self._threads:
            t.start()

        try:
            self.wait_ready(timeout)
        except DreamTunnelError:
            self.stop()
            raise
        return self

    def stop(self, timeout: float = 5.0) -> None:
        """Terminate the tunnel (SIGTERM, then SIGKILL after ``timeout``)."""
        proc = self._proc
        self._ready.clear()
        if proc is None:
            return
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=timeout)
        self._exit_code = proc.poll()
        for t in self._threads:
            t.join(timeout=1.0)
        self._proc = None

    # connect/disconnect are friendlier names for the same two operations
    def connect(self, timeout: float | None = None) -> "Tunnel":
        """Alias for :meth:`start`."""
        return self.start(timeout)

    def disconnect(self, timeout: float = 5.0) -> None:
        """Alias for :meth:`stop`."""
        self.stop(timeout)

    def restart(self, timeout: float | None = None) -> "Tunnel":
        """Stop then start the tunnel; the same subdomain is reused."""
        self.stop()
        return self.start(timeout)

    def close(self, timeout: float = 5.0) -> None:
        """Stop the tunnel and delete the session file it created.

        Unlike :meth:`stop`, a later :meth:`start` will get a fresh session,
        so an auto-assigned subdomain may change.
        """
        self.stop(timeout)
        if self._own_config and self._config_path is not None:
            try:
                self._config_path.unlink()
            except FileNotFoundError:
                pass

    def wait(self, timeout: float | None = None) -> int | None:
        """Block until the ``dt`` process exits (or ``timeout`` elapses)."""
        proc = self._proc
        if proc is None:
            return self._exit_code
        try:
            self._exit_code = proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            pass
        return self._exit_code

    def wait_ready(self, timeout: float | None = None) -> str:
        """Wait for the tunnel to come online and return the public URL."""
        limit = self.ready_timeout if timeout is None else timeout
        deadline = time.monotonic() + limit
        while True:
            if self._ready.is_set() and self._url:
                return self._url
            if self._proc is not None and self._proc.poll() is not None:
                with self._lock:
                    self._exit_code = self._proc.poll()
                # let the reader threads drain stdout/stderr so .output is complete
                for t in self._threads:
                    t.join(timeout=1.0)
                raise TunnelExited(
                    f"dt exited with code {self._exit_code} before the tunnel was online",
                    code=self._exit_code,
                    output=self.stderr,
                )
            if time.monotonic() > deadline:
                with self._lock:
                    err = self._last_error
                if err:
                    raise TunnelError(err)
                raise TunnelTimeout(f"tunnel was not online within {limit:.0f}s")
            time.sleep(0.05)

    # ------------------------------------------------------------------ context

    def __enter__(self) -> "Tunnel":
        return self.start()

    def __exit__(self, exc_type, exc, tb) -> None:
        self.close()
