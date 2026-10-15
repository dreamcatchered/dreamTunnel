"""Binary discovery / install tests."""

from __future__ import annotations

from pathlib import Path

import pytest

from dreamtunnel import BinaryNotFound
from dreamtunnel.binary import cache_dir, ensure_dt, platform_binary_name


@pytest.mark.parametrize(
    ("system", "machine", "expected"),
    [
        ("Linux", "x86_64", "dt-linux-amd64"),
        ("Linux", "aarch64", "dt-linux-arm64"),
        ("Darwin", "arm64", "dt-darwin-arm64"),
        ("Darwin", "x86_64", "dt-darwin-amd64"),
        ("Windows", "AMD64", "dt-windows-amd64.exe"),
        ("Windows", "x86", "dt-windows-386.exe"),
    ],
)
def test_platform_binary_name(system: str, machine: str, expected: str) -> None:
    assert platform_binary_name(system, machine) == expected


def test_unsupported_platform() -> None:
    with pytest.raises(BinaryNotFound):
        platform_binary_name("Plan9", "mips")
    with pytest.raises(BinaryNotFound):
        platform_binary_name("Linux", "sparc64")


def test_ensure_dt_prefers_explicit_path(fake_dt: Path) -> None:
    assert ensure_dt(str(fake_dt), auto_install=False) == str(fake_dt)


def test_ensure_dt_reports_missing_when_not_installed(clean_env: None, tmp_path: Path) -> None:
    with pytest.raises(BinaryNotFound):
        ensure_dt(None, auto_install=False)


def test_ensure_dt_uses_cache_dir(clean_env: None, tmp_path: Path) -> None:
    cached = cache_dir() / "dt"
    cached.parent.mkdir(parents=True, exist_ok=True)
    cached.write_text("#!/bin/sh\n", encoding="utf-8")
    assert ensure_dt(None, auto_install=False) == str(cached)


def test_cache_dir_honours_env(clean_env: None, tmp_path: Path) -> None:
    assert str(tmp_path) in str(cache_dir())
