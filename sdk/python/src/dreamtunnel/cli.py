"""Command line interface: ``dreamtunnel http 8080``.

Mirrors the ``dt`` CLI but uses the SDK, so anything the SDK can do
(events, JSON output, clean shutdown) is available from the shell too.
"""

from __future__ import annotations

import argparse
import json
import signal
import sys
import time
from typing import Any

from . import __version__
from .binary import DEFAULT_BASE_URL
from .errors import DreamTunnelError
from .tunnel import DEFAULT_SERVER, Tunnel


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="dreamtunnel",
        description="Expose a local port through dreamTunnel.",
    )
    parser.add_argument("--version", action="version", version=f"dreamtunnel {__version__}")

    sub = parser.add_subparsers(dest="mode", required=True)
    for name, help_text in (("http", "expose a local HTTP service"),
                            ("tcp", "expose a raw TCP service")):
        p = sub.add_parser(name, help=help_text)
        p.add_argument("port", type=int, help="local port to expose")
        p.add_argument("subdomain", nargs="?", default=None, help="requested subdomain label")
        p.add_argument("--sub", dest="sub_flag", default=None, help="same as the positional name")
        p.add_argument("--server", default=None, help=f"control endpoint (default {DEFAULT_SERVER})")
        p.add_argument("--token", default=None, help="access token (defaults to the built-in one)")
        p.add_argument("--dt", dest="dt_binary", default=None, help="path to the dt binary")
        p.add_argument("--base-url", default=DEFAULT_BASE_URL, help="download base URL")
        p.add_argument("--no-install", action="store_true", help="never download dt")
        p.add_argument("--json", action="store_true", help="print NDJSON events")
        p.add_argument("--timeout", type=float, default=30.0, help="ready timeout in seconds")
    return parser


def _print_event(event: dict[str, Any], as_json: bool) -> None:
    if as_json:
        print(json.dumps(event, ensure_ascii=False), flush=True)
    elif event.get("event") == "ready" and not event.get("reconnect"):
        print(f"\n  dreamTunnel — tunnel online\n", flush=True)
        print(f"  Forwarding   {event.get('url')} -> {event.get('local')}\n", flush=True)
        print("  Press Ctrl+C to stop\n", flush=True)
    elif event.get("event") in {"error", "disconnected"}:
        print(f"[dt] {event.get('event')}: {event.get('error')}", file=sys.stderr, flush=True)


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    subdomain = args.sub_flag or args.subdomain

    tunnel = Tunnel(
        port=args.port,
        subdomain=subdomain,
        mode=args.mode,
        server=args.server,
        token=args.token,
        dt_binary=args.dt_binary,
        auto_install=not args.no_install,
        ready_timeout=args.timeout,
        on_event=lambda e: _print_event(e, args.json),
        base_url=args.base_url,
    )

    stopping = False

    def _stop(signum: int, frame: object) -> None:  # noqa: ARG001
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGINT, _stop)
    signal.signal(signal.SIGTERM, _stop)

    try:
        tunnel.start()
    except DreamTunnelError as exc:
        print(f"[dt] error: {exc}", file=sys.stderr)
        return 1

    try:
        while not stopping and tunnel.is_running:
            time.sleep(0.2)
    finally:
        tunnel.stop()
    print("\n[dt] shutting down", flush=True)
    return 0
