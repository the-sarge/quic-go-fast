#!/usr/bin/env python3
"""Required peer scenarios: a missing or skipped role is a failed claim."""

import argparse
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess

PACKAGE = "github.com/quic-go/quic-go/integrationtests/self"
EXPECTED = ["TestPicoquicOrdinaryControl/client", "TestPicoquicOrdinaryControl/server"]
# R14's two-path smoke reads private owners, so it runs in the root package.
SMOKE_PACKAGE = "github.com/quic-go/quic-go"
SMOKE_EXPECTED = ["TestPicoquicMultipathSmoke/client", "TestPicoquicMultipathSmoke/server"]


def check_cases(lines, package=PACKAGE, expected=EXPECTED):
    passed = []
    for line in lines:
        event = json.loads(line)
        if event.get("Package") != package:
            continue
        action = event.get("Action")
        if action in ("skip", "fail"):
            raise ValueError(f"{action}: {event.get('Test', package)}")
        if action == "pass" and event.get("Test") in expected:
            passed.append(event["Test"])
    if sorted(passed) != sorted(expected):
        raise ValueError(f"missing or duplicate expected cases: {passed}; required {expected}")


def check_prerequisites(binary, system, machine):
    if system.lower() != "linux" or machine not in ("x86_64", "amd64"):
        raise ValueError("the peer scenarios require Linux amd64")
    if not Path(binary).is_file() or not os.access(binary, os.X_OK):
        raise ValueError(f"missing executable peer binary: {binary}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--smoke", action="store_true", help="run R14's two-path smoke instead of the ordinary control")
    args = parser.parse_args()
    check_prerequisites(args.binary, platform.system(), platform.machine())
    for executable in ("go", "stdbuf"):
        if shutil.which(executable) is None:
            raise ValueError(f"missing prerequisite: {executable}")
    args.output.mkdir(parents=True, exist_ok=True)
    env = os.environ | {"PICOQUIC_BINARY": str(Path(args.binary).resolve()), "PICOQUIC_LOG_DIR": str(args.output.resolve())}
    command = ["go", "test", "-json", "-tags=picoquic", "-run=^TestPicoquicOrdinaryControl$", "-count=1", "-timeout=90s", "./integrationtests/self", "-version=1"]
    package, expected = PACKAGE, EXPECTED
    if args.smoke:
        command = ["go", "test", "-json", "-tags=picoquic", "-run=^TestPicoquicMultipathSmoke$", "-count=1", "-timeout=90s", "."]
        package, expected = SMOKE_PACKAGE, SMOKE_EXPECTED
    with (args.output / "go-test.jsonl").open("w") as log:
        process = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            status = process.wait(timeout=120)
        except BaseException:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            raise
    if status != 0:
        raise ValueError(f"peer control exited {status}; see {args.output / 'go-test.jsonl'}")
    check_cases((args.output / "go-test.jsonl").read_text().splitlines(), package, expected)
    print(json.dumps({"executed": expected, "status": "pass"}))


if __name__ == "__main__":
    main()
