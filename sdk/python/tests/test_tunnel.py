"""Tunnel lifecycle tests, driven by the fake ``dt`` binary."""

from __future__ import annotations

import json
import time
from pathlib import Path

import pytest

from dreamtunnel import BinaryNotFound, Tunnel, TunnelError, TunnelExited, TunnelTimeout
from dreamtunnel.binary import ensure_dt


def make(fake_dt: Path, **kwargs) -> Tunnel:
    kwargs.setdefault("port", 8080)
    return Tunnel(dt_binary=str(fake_dt), auto_install=False, **kwargs)


def child_env(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> list[dict]:
    """Point the fake dt at a dump dir and return the DT_* envs it saw."""
    dumps = tmp_path / "dumps"
    dumps.mkdir(exist_ok=True)
    monkeypatch.setenv("FAKE_DT_ENVDUMP_DIR", str(dumps))
    return dumps


def read_envs(dumps: Path) -> list[dict]:
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        found = sorted(dumps.glob("env-*.json"))
        if found:
            return [json.loads(p.read_text()) for p in found]
        time.sleep(0.05)
    raise AssertionError("the fake dt never dumped its environment")


# --------------------------------------------------------------------- basics


def test_start_exposes_url_and_stops(fake_dt: Path) -> None:
    tun = make(fake_dt, subdomain="my-api")
    tun.start()
    try:
        assert tun.url == "https://my-api.maiuni.online"
        assert tun.host == "my-api.maiuni.online"
        assert tun.local_url == "http://127.0.0.1:8080"
        assert tun.is_running and tun.connected
        assert tun.pid is not None
    finally:
        tun.stop()

    assert not tun.is_running
    assert not tun.connected
    tun.stop()  # idempotent


def test_context_manager(fake_dt: Path) -> None:
    with make(fake_dt, subdomain="ctx") as tun:
        assert tun.url == "https://ctx.maiuni.online"
    assert not tun.is_running


def test_connect_disconnect_aliases(fake_dt: Path) -> None:
    tun = make(fake_dt, subdomain="alias")
    tun.connect()
    assert tun.connected
    tun.disconnect()
    assert not tun.is_running


def test_random_subdomain_is_returned(fake_dt: Path) -> None:
    # no subdomain requested -> the relay picks one, the SDK reports it verbatim
    with make(fake_dt) as tun:
        assert tun.url == "https://fake.maiuni.online"


def test_restart_keeps_the_same_url(fake_dt: Path) -> None:
    tun = make(fake_dt, subdomain="sticky")
    tun.start()
    url = tun.url
    tun.restart()
    try:
        assert tun.url == url
        assert tun.connected
    finally:
        tun.stop()


def test_events_and_callback_reach_the_caller(fake_dt: Path) -> None:
    seen: list[dict] = []
    with make(fake_dt, subdomain="cb", on_event=seen.append) as tun:
        tun.wait_ready()
    assert seen, "on_event was never called"
    assert seen[0]["event"] == "ready"
    assert any(e["event"] == "ready" for e in tun.events)


def test_device_id_is_reported_and_survives_restart(
    fake_dt: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A fresh device is registered by the relay and keeps its id."""
    monkeypatch.setenv("FAKE_DT_ENROLL", "1")
    seen: list[dict] = []
    tun = make(fake_dt, subdomain="dev", on_event=seen.append)
    tun.start()
    try:
        assert tun.device_id == "f00dbabe"
        tun.restart()
        assert tun.device_id == "f00dbabe", "the device id must survive a restart"
    finally:
        tun.stop()
    assert seen[0]["event"] == "enrolled"
    assert any(e["event"] == "ready" and e.get("device") == "f00dbabe" for e in seen)


def test_device_id_is_none_for_old_binary(fake_dt: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """dt builds without the enrollment protocol report no device id."""
    monkeypatch.setenv("FAKE_DT_IGNORE_JSON", "1")
    with make(fake_dt, subdomain="old-dev") as tun:
        assert tun.device_id is None


def test_reconnect_event_is_reported(fake_dt: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("FAKE_DT_BEHAVIOUR", "flap")
    seen: list[dict] = []
    with make(fake_dt, subdomain="flap", on_event=seen.append):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if any(e["event"] in {"disconnected", "reconnected"} for e in seen):
                break
            time.sleep(0.05)
        kinds = {e["event"] for e in seen}
    assert "disconnected" in kinds
    assert "reconnected" in kinds


# -------------------------------------------------------------------- failure


def test_timeout_when_relay_never_answers(fake_dt: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("FAKE_DT_BEHAVIOUR", "slow")
    tun = make(fake_dt, ready_timeout=0.5)
    with pytest.raises(TunnelTimeout):
        tun.start()
    assert not tun.is_running, "a failed start must not leave a stray process"


def test_process_exit_before_ready_is_reported(fake_dt: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("FAKE_DT_BEHAVIOUR", "fail")
    with pytest.raises(TunnelExited) as exc:
        make(fake_dt).start()
    assert "no token" in exc.value.output


def test_relay_refusal_raises_tunnel_error(fake_dt: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("FAKE_DT_BEHAVIOUR", "denied")
    with pytest.raises(TunnelError):
        make(fake_dt, ready_timeout=2).start()


def test_missing_binary_raises(fake_dt: Path, tmp_path: Path) -> None:
    tun = Tunnel(port=1, dt_binary=str(tmp_path / "nope"), auto_install=False)
    with pytest.raises(BinaryNotFound):
        tun.start()


def test_human_output_fallback(fake_dt: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """An older dt without --json is understood via the Forwarding line."""
    monkeypatch.setenv("FAKE_DT_IGNORE_JSON", "1")
    with make(fake_dt, subdomain="old") as tun:
        assert tun.url == "https://old.maiuni.online"
        assert tun.local_url == "http://127.0.0.1:8080"


# -------------------------------------------------------------------- sessions


def test_token_and_server_are_passed_through(
    fake_dt: Path, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """DT_TOKEN/DT_SERVER must reach the child process."""
    dumps = child_env(monkeypatch, tmp_path)
    with make(fake_dt, subdomain="env", token="tok-123", server="example.test:444"):
        pass
    env = read_envs(dumps)[0]
    assert env["DT_TOKEN"] == "tok-123"
    assert env["DT_SERVER"] == "example.test:444"
    assert env["DT_SUB"] == "env"


def test_each_tunnel_gets_its_own_config(
    fake_dt: Path, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """Two tunnels from one machine must not share a dt session."""
    dumps = child_env(monkeypatch, tmp_path)
    with make(fake_dt, subdomain="s1") as first:
        with make(fake_dt, subdomain="s2") as second:
            assert first.url == "https://s1.maiuni.online"
            assert second.url == "https://s2.maiuni.online"
            assert first.is_running and second.is_running
    envs = read_envs(dumps)
    assert len(envs) == 2
    assert len({e["DT_CONFIG"] for e in envs}) == 2, "tunnels share one dt config"


def test_session_can_be_disabled(
    fake_dt: Path, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """session=False falls back to the shared ~/.dreamtunnel/config.env."""
    dumps = child_env(monkeypatch, tmp_path)
    with make(fake_dt, session=False):
        pass
    assert "DT_CONFIG" not in read_envs(dumps)[0]


def test_explicit_config_path_is_used_and_kept(
    fake_dt: Path, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    dumps = child_env(monkeypatch, tmp_path)
    cfg = tmp_path / "mine" / "dt.env"
    with make(fake_dt, config_path=cfg):
        pass
    assert read_envs(dumps)[0]["DT_CONFIG"] == str(cfg)
    assert cfg.exists(), "an explicit config file must not be deleted"


def test_restart_reuses_the_same_config_file(
    fake_dt: Path, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """The session file is what makes a restart land on the same host."""
    dumps = child_env(monkeypatch, tmp_path)
    tun = make(fake_dt, subdomain="r")
    tun.start()
    tun.restart()
    try:
        envs = read_envs(dumps)
        assert len({e["DT_CONFIG"] for e in envs}) == 1
    finally:
        tun.close()


def test_close_deletes_the_session_file(fake_dt: Path) -> None:
    tun = make(fake_dt, subdomain="bye")
    tun.start()
    cfg = tun._session_config()
    assert cfg.exists()
    tun.close()
    assert not cfg.exists()
    assert not tun.is_running


def test_ensure_dt_finds_explicit_binary(fake_dt: Path) -> None:
    assert ensure_dt(str(fake_dt), auto_install=False) == str(fake_dt)
