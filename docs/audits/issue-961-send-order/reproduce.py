#!/usr/bin/env python3
"""Activate the issue 961 fixture without changing a checkout's Go sources."""

import argparse
import json
from pathlib import Path
import subprocess
import tempfile


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[3])
    parser.add_argument("--upstream", action="store_true", help="adapt the packet-number API for upstream d05f943b")
    args = parser.parse_args()
    root = args.root.resolve()
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    print(f"root={root}\nrevision={revision}\nupstream_api={args.upstream}", flush=True)
    source = Path(__file__).with_name("reproduction_test.go.txt").read_text()
    if args.upstream:
        source = source.replace("h.PopPacketNumber(level)", "h.PopPacketNumber(level.PacketNumberSpace())")
    with tempfile.TemporaryDirectory(prefix="issue-961-overlay-") as directory:
        backing = Path(directory) / "issue961_reproduction_test.go"
        backing.write_text(source)
        overlay = Path(directory) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(root / "internal/ackhandler/issue961_reproduction_test.go"): str(backing)}}))
        return subprocess.run(
            ["go", "test", "-overlay", str(overlay), "./internal/ackhandler", "-run", "^TestIssue961RecoverySendOrder$", "-count=1", "-v"],
            cwd=root,
            check=False,
        ).returncode


if __name__ == "__main__":
    raise SystemExit(main())
