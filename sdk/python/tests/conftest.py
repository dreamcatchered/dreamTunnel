"""Shared pytest fixtures: a fake ``dt`` binary that speaks the real CLI."""

from __future__ import annotations

import os
import shutil
import stat
from pathlib import Path

import pytest

FAKE_SRC = Path(__file__).parent / "fake_dt.py"


@pytest.fixture()
def fake_dt(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    """Copy ``tests/fake_dt.py`` to an executable ``dt`` and return its path."""
    path = tmp_path / "dt"
    shutil.copy(FAKE_SRC, path)
    path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    # keep session files out of the real ~/.dreamtunnel
    monkeypatch.setenv("DREAMTUNNEL_HOME", str(tmp_path / "home"))
    return path


@pytest.fixture()
def clean_env(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    """Isolate tests from a real dt installation on the machine."""
    monkeypatch.delenv("DT_BINARY", raising=False)
    monkeypatch.delenv("DT_TOKEN", raising=False)
    monkeypatch.delenv("DT_SERVER", raising=False)
    monkeypatch.delenv("DT_CONFIG", raising=False)
    monkeypatch.setenv("DREAMTUNNEL_HOME", str(tmp_path / "home"))
    monkeypatch.setenv("PATH", str(tmp_path / "empty"))
    assert not os.path.exists(tmp_path / "empty")
