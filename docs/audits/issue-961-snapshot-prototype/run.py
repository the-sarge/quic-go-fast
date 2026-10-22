#!/usr/bin/env python3
"""Replay the frozen issue-961 experiment through temporary Go source overlays."""

import json
from pathlib import Path
import subprocess
import tempfile


def main() -> int:
    evidence = Path(__file__).resolve().parent
    repo = evidence.parents[2]
    source = "78096061e13bdac5cbfad66ce40db9289ca92959"
    modified = [
        "internal/ackhandler/sent_packet_handler.go",
        "internal/congestion/cubic_sender.go",
        "internal/congestion/hybrid_slow_start.go",
    ]
    added = [
        "internal/ackhandler/snapshot_prototype_test.go",
        "internal/congestion/snapshot_prototype.go",
        "internal/congestion/snapshot_prototype_test.go",
    ]
    print("THROWAWAY snapshot-only experiment; eligible-send prefix policy", flush=True)
    print("production source=" + source, flush=True)
    print(subprocess.check_output(["go", "version"], cwd=repo, text=True).strip(), flush=True)
    with tempfile.TemporaryDirectory(prefix="issue961-snapshot-replay-") as directory:
        scratch = Path(directory)
        for name in modified:
            baseline = subprocess.check_output(["git", "show", source + ":" + name], cwd=repo)
            if (repo / name).read_bytes() != baseline:
                raise SystemExit("Replay requires unchanged pinned production source: " + name)
            target = scratch / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(baseline)
        subprocess.run(["git", "apply", "--unsafe-paths", str(evidence / "candidate.patch")], cwd=scratch, check=True)
        replacements = {str(repo / name): str(scratch / name) for name in modified + added}
        replacements[str(repo / "internal/ackhandler/issue961_reproduction_test.go")] = str(evidence.parent / "issue-961-send-order/reproduction_test.go.txt")
        overlay = scratch / "overlay.json"
        overlay.write_text(json.dumps({"Replace": replacements}))
        return subprocess.run(
            ["go", "test", "-overlay", str(overlay), "./internal/ackhandler", "./internal/congestion", "-run", "^TestIssue961", "-count=1", "-v"],
            cwd=repo,
            check=False,
        ).returncode


if __name__ == "__main__":
    raise SystemExit(main())
