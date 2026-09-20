#!/usr/bin/env python3
"""Retain bounded test output and provenance without changing its exit code."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import time

SLOT_BYTES = 32 << 20
RESERVE_BYTES = 1 << 20


def reserve(root):
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    for i in range(8):
        path = root / f"slot-{i}"
        try:
            path.mkdir(mode=0o700)
            return path
        except FileExistsError:
            continue
    raise OSError("HTTP capture quota exhausted: 8 slots reserved")


def command_text(*args):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    return {"exit": result.returncode, "output": result.stdout.decode("utf-8", errors="replace")}


def write_json(path, data):
    payload = (json.dumps(data, indent=2) + "\n").encode()
    if len(payload) > RESERVE_BYTES // 4:
        raise OSError("HTTP capture metadata budget exceeded")
    path.write_bytes(payload)
    path.chmod(0o600)


def checksums(root):
    # Include interrupted captures. This hashes available bytes, not completeness.
    for slot in sorted(root.glob("slot-*")):
        if not slot.is_dir():
            continue
        lines = []
        for path in sorted(slot.iterdir()):
            if path.is_file() and path.name != "SHA256SUMS":
                with path.open("rb") as source:
                    hasher = hashlib.sha256()
                    while chunk := source.read(65536):
                        hasher.update(chunk)
                    digest = hasher.hexdigest()
                lines.append(f"{digest}  {path.name}\n")
        (slot / "SHA256SUMS").write_text("".join(lines), encoding="utf-8")


def run(root, label, argv, unit_source=False):
    slot = None
    log = None
    errors = []
    written = 0
    observed = 0
    run_id = f"{label}-{time.time_ns()}"
    try:
        slot = reserve(root)
        source_paths = []
        if unit_source:
            prefix = command_text("git", "rev-parse", "--show-prefix")
            if prefix["exit"] != 0 or prefix["output"].strip():
                raise OSError("unit source capture requires the repository root")
            if os.path.lexists("integrationtests"):
                raise OSError("integrationtests must be absent for unit source capture")
            source_paths = ["--", ".", ":(top,exclude)integrationtests"]
        metadata = {
            "schema": 1, "run_id": run_id, "command": argv,
            "cwd": os.getcwd(), "started_ns": time.time_ns(),
            "platform": platform.platform(), "machine": platform.machine(),
            "python": platform.python_version(),
            "head": command_text("git", "rev-parse", "HEAD"),
            "tree": command_text("git", "rev-parse", "HEAD^{tree}"),
            "status": command_text("git", "status", "--porcelain", *source_paths),
            "source_scope": {
                "excluded_paths": ["integrationtests"] if unit_source else [],
                "reason": "unit workflow removes integrationtests before testing" if unit_source else "whole checkout",
            },
            "go": command_text("go", "version"),
            "environment": {key: os.environ.get(key, "") for key in (
                "TIMESCALE_FACTOR", "GOTOOLCHAIN", "GODEBUG", "GOMAXPROCS",
                "QUIC_GO_DISABLE_GSO", "QUIC_GO_DISABLE_ECN", "QUIC_GO_DIAL_OWNER_UNTIL", "GITHUB_SHA",
                "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "RUNNER_OS", "RUNNER_ARCH")},
        }
        # Preserve a bounded patch for local instrumentation. Clean CI has an
        # empty patch and immutable checked-out HEAD/tree (including merge refs).
        patch = subprocess.check_output(["git", "diff", "--binary", "HEAD", *source_paths], timeout=20)
        if len(patch) > RESERVE_BYTES // 2:
            raise OSError("HTTP capture source patch budget exceeded")
        (slot / "source.patch").write_bytes(patch)
        metadata["patch_sha256"] = hashlib.sha256(patch).hexdigest()
        untracked = command_text("git", "ls-files", "--others", "--exclude-standard", *source_paths)
        metadata["source_complete"] = untracked["exit"] == 0 and not untracked["output"] and all(
            metadata[key]["exit"] == 0 for key in ("head", "tree", "status")
        )
        write_json(slot / "run.json", metadata)
        log = (slot / "output.log").open("wb", buffering=0)
    except (OSError, subprocess.SubprocessError) as exc:
        errors.append(str(exc))
        print(f"HTTP run capture unavailable: {exc}", file=sys.stderr)
    env = dict(os.environ, QUIC_GO_HTTP_RUN_ID=run_id, QUIC_GO_HTTP_CAPTURE_DIR=str(root.resolve()))
    with subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env) as process:
        while chunk := process.stdout.read1(65536):
            observed += len(chunk)
            sys.stdout.buffer.write(chunk)
            sys.stdout.buffer.flush()
            if log is not None:
                try:
                    accepted = chunk[:max(0, SLOT_BYTES - RESERVE_BYTES - written)]
                    n = log.write(accepted)
                    written += n
                    if n != len(accepted):
                        raise OSError("short HTTP output write")
                except OSError as exc:
                    errors.append(str(exc))
                    log.close()
                    log = None
        code = process.wait()
    if log is not None:
        log.close()
    if slot is not None:
        try:
            write_json(slot / "result.json", {
                "exit": code, "finished_ns": time.time_ns(),
                "output_observed_bytes": observed, "output_retained_bytes": written,
                "truncated": written != observed, "errors": errors,
                "seed_source": "verbatim -test.shuffle line in output.log",
            })
            checksums(root)
        except OSError as exc:
            print(f"HTTP capture finalization failed: {exc}", file=sys.stderr)
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["run", "finalize"])
    parser.add_argument("--root", default=os.environ.get("QUIC_GO_HTTP_CAPTURE_DIR"))
    parser.add_argument("--label", default="local")
    parser.add_argument("--unit-source", action="store_true", help="record the unit checkout with integrationtests removed")
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if not args.root:
        parser.error("--root or QUIC_GO_HTTP_CAPTURE_DIR is required")
    root = Path(args.root)
    if args.action == "finalize":
        checksums(root)
        return 0
    argv = args.command
    if argv and argv[0] == "--":
        argv = argv[1:]
    if not argv:
        parser.error("a command after -- is required")
    return run(root, args.label, argv, unit_source=args.unit_source)


if __name__ == "__main__":
    sys.exit(main())
