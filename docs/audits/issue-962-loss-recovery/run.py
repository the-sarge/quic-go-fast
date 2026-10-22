#!/usr/bin/env python3
"""Replay the frozen issue-962 scenarios on either pinned source tree."""

import argparse
import json
from pathlib import Path
import subprocess
import tempfile

PINS = {
    "fork": "1c4a28b7bc000bbc521332f5d7145e6493b42709",
    "upstream": "2ccf27f2a1883b1a08cbbbe65bd3db168b4f8dd4",
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path)
    parser.add_argument("--source", choices=PINS, default="fork")
    args = parser.parse_args()
    evidence = Path(__file__).resolve().parent
    repo = (args.repo or evidence.parents[2]).resolve()
    pin = PINS[args.source]
    subprocess.run(["git", "diff", "--exit-code", pin, "--", "*.go", "go.mod", "go.sum"], cwd=repo, check=True)
    untracked = subprocess.check_output(["git", "ls-files", "--others", "--exclude-standard"], cwd=repo, text=True).splitlines()
    if any(name.endswith(".go") for name in untracked):
        raise SystemExit("Untracked Go source would contaminate this replay")
    source = (evidence / "reproduction_test.go.txt").read_text()
    if args.source == "upstream":
        source = source.replace("h.PopPacketNumber(level)", "h.PopPacketNumber(level.PacketNumberSpace())")
        source = source.replace("_, err := f.h.OnLossDetectionTimeout(at)", "err := f.h.OnLossDetectionTimeout(at)")
    print("issue962 source=" + args.source + " pin=" + pin, flush=True)
    print(subprocess.check_output(["go", "version"], cwd=repo, text=True).strip(), flush=True)
    with tempfile.TemporaryDirectory(prefix="issue962-replay-") as directory:
        scratch = Path(directory)
        fixture = scratch / "issue962_reproduction_test.go"
        fixture.write_text(source)
        overlay = scratch / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(repo / "internal/ackhandler/issue962_reproduction_test.go"): str(fixture)}}))
        return subprocess.run(["go", "test", "-overlay", str(overlay), "./internal/ackhandler", "-run", "^TestIssue962", "-count=1", "-v"], cwd=repo).returncode


if __name__ == "__main__":
    raise SystemExit(main())
