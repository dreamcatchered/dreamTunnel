"""Locate or install the ``dt`` binary.

Lookup order:

1. the ``dt_binary`` argument passed to :class:`~dreamtunnel.Tunnel`
2. the ``DT_BINARY`` environment variable
3. ``dt`` on ``PATH``
4. the SDK cache (``~/.dreamtunnel/bin``)
5. an automatic download from the public release endpoint, unless disabled
"""

from __future__ import annotations

import os
import platform
import shutil
import stat
import sys
import urllib.error
import urllib.request
from pathlib import Path

from .errors import BinaryNotFound

DEFAULT_BASE_URL = os.environ.get("DREAMTUNNEL_BASE_URL", "https://tunnel.swag.best")

# machine() spellings we accept
_AMD64 = {"x86_64", "amd64", "x64", "i686", "i386", "x86"}
_ARM64 = {"aarch64", "arm64"}


def cache_root() -> Path:
    """Root directory used by the SDK (``~/.dreamtunnel`` by default)."""
    override = os.environ.get("DREAMTUNNEL_HOME")
    return Path(override).expanduser() if override else Path.home() / ".dreamtunnel"


def cache_dir() -> Path:
    """Directory where downloaded binaries are kept."""
    return cache_root() / "bin"


def platform_binary_name(
    system: str | None = None, machine: str | None = None
) -> str:
    """Return the release file name for the current (or given) platform.

    Raises:
        BinaryNotFound: for platforms dreamTunnel does not build for.
    """
    system = (system or platform.system()).lower()
    machine = (machine or platform.machine()).lower()

    if machine in _AMD64:
        arch = "386" if machine in {"i686", "i386", "x86"} else "amd64"
    elif machine in _ARM64:
        arch = "arm64"
    else:
        raise BinaryNotFound(f"unsupported CPU architecture: {machine!r}")

    if system.startswith("linux"):
        return f"dt-linux-{arch}"
    if system.startswith(("darwin", "mac")):
        return f"dt-darwin-{arch}"
    if system.startswith("win"):
        return f"dt-windows-{arch}.exe"
    raise BinaryNotFound(f"unsupported platform: {system!r}")


def _executable_name() -> str:
    return "dt.exe" if os.name == "nt" else "dt"


def _make_executable(path: Path) -> None:
    if os.name == "nt":
        return
    mode = path.stat().st_mode
    path.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def download_dt(dest: Path | None = None, *, base_url: str = DEFAULT_BASE_URL) -> Path:
    """Download the ``dt`` binary for this platform into the SDK cache."""
    name = platform_binary_name()
    dest = dest or (cache_dir() / _executable_name())
    dest.parent.mkdir(parents=True, exist_ok=True)

    url = f"{base_url.rstrip('/')}/dl/{name}"
    tmp = dest.with_suffix(dest.suffix + ".part")
    try:
        with urllib.request.urlopen(url, timeout=60) as resp:  # noqa: S310
            data = resp.read()
    except urllib.error.URLError as exc:  # pragma: no cover - network path
        raise BinaryNotFound(f"could not download {url}: {exc}") from exc

    if len(data) < 1024:
        raise BinaryNotFound(f"download from {url} looks truncated ({len(data)} bytes)")

    tmp.write_bytes(data)
    tmp.replace(dest)
    _make_executable(dest)
    return dest


def install_dt(
    dest: Path | None = None, *, base_url: str = DEFAULT_BASE_URL, force: bool = False
) -> Path:
    """Install ``dt`` into the cache directory and return its path."""
    dest = dest or (cache_dir() / _executable_name())
    if dest.exists() and not force:
        return dest
    return download_dt(dest, base_url=base_url)


def ensure_dt(
    explicit: str | os.PathLike[str] | None = None,
    *,
    auto_install: bool = True,
    base_url: str = DEFAULT_BASE_URL,
) -> str:
    """Return a usable path to ``dt``, installing it if necessary."""
    if explicit:
        path = Path(explicit).expanduser()
        if path.exists():
            return str(path)
        raise BinaryNotFound(f"dt binary not found at {path}")

    env = os.environ.get("DT_BINARY")
    if env:
        path = Path(env).expanduser()
        if path.exists():
            return str(path)

    found = shutil.which("dt")
    if found:
        return found

    cached = cache_dir() / _executable_name()
    if cached.exists():
        return str(cached)

    if not auto_install:
        raise BinaryNotFound(
            "dt not found on PATH; install it with "
            "`curl -fsSL https://tunnel.swag.best/install.sh | bash` "
            "or pass dt_binary=..."
        )

    return str(download_dt(base_url=base_url))


def python_supported() -> bool:
    return sys.version_info >= (3, 10)
