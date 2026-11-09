#!/usr/bin/env python3
"""Required peer scenarios: a missing or skipped role is a failed claim."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import time

PACKAGE = "github.com/quic-go/quic-go/integrationtests/self"
EXPECTED = ["TestPicoquicOrdinaryControl/client", "TestPicoquicOrdinaryControl/server"]
# R14's two-path smoke reads private owners, so it runs in the root package.
SMOKE_PACKAGE = "github.com/quic-go/quic-go"
SMOKE_EXPECTED = ["TestPicoquicMultipathSmoke/client", "TestPicoquicMultipathSmoke/server"]
MATRIX_SCENARIOS = (
    "useful-transfer", "absent-negotiation", "path-zero-ack",
    "key-update-silent-path", "status-backup-failover",
    "abandon-reopen-late-feedback", "same-id-migration-rebinding",
    "unequal-mtu-invalid-frame",
)
MATRIX_EXPECTED = [f"TestPicoquicMultipathMatrix/{scenario}/{role}"
                   for scenario in MATRIX_SCENARIOS for role in ("client", "server")]
ZERO_EXPECTED = ["TestMultipathNegotiatedZeroVectors", "TestMultipathNegotiatedZeroControlledPeer"]


def matrix_invocation(scenario, role, test_binary=None):
    if scenario not in MATRIX_SCENARIOS or role not in ("client", "server"):
        raise ValueError("the matrix requires one fixed scenario and endpoint role")
    expression = f"^TestPicoquicMultipathMatrix$/^{scenario}$/^{role}$"
    command = ["go", "test", "-json", "-tags=picoquic", f"-run={expression}", "-count=1", "-timeout=90s", "."]
    if test_binary:
        command = ["go", "tool", "test2json", "-t", "-p", SMOKE_PACKAGE, str(test_binary),
                   "-test.v=test2json", f"-test.run={expression}", "-test.count=1", "-test.timeout=90s"]
    return command, [f"TestPicoquicMultipathMatrix/{scenario}/{role}"]


def check_matrix(root):
    identity = None
    expected_paths = set()
    # Require every log before comparing metadata, so missing evidence wins.
    for case in MATRIX_EXPECTED:
        _, scenario, role = case.split("/")
        expected_paths.add(root / scenario / role / "go-test.jsonl")
    actual_paths = set(root.glob("*/*/go-test.jsonl"))
    if actual_paths != expected_paths:
        raise ValueError(f"missing or unexpected matrix case logs: {actual_paths ^ expected_paths}")
    for case in MATRIX_EXPECTED:
        _, scenario, role = case.split("/")
        directory = root / scenario / role
        check_cases((directory / "go-test.jsonl").read_text().splitlines(), SMOKE_PACKAGE, [case])
        result = json.loads((directory / "result.json").read_text())
        if result.get("status") != "pass" or result.get("executed") != [case] or not result.get("identity"):
            raise ValueError(f"invalid case receipt: {case}")
        if identity is None:
            identity = result["identity"]
        elif result["identity"] != identity:
            raise ValueError(f"matrix candidate/build/kernel identity changed: {case}")
    zero = root / "negotiated-zero"
    if not (zero / "go-test.jsonl").is_file() or not (zero / "result.json").is_file():
        raise ValueError("missing negotiated-zero normative/controlled evidence")
    check_cases((zero / "go-test.jsonl").read_text().splitlines(), SMOKE_PACKAGE, ZERO_EXPECTED)
    result = json.loads((zero / "result.json").read_text())
    if result.get("status") != "pass" or result.get("executed") != ZERO_EXPECTED or result.get("identity") != identity:
        raise ValueError("negotiated-zero receipt identity/accounting mismatch")
    return {"status": "pass", "executed": MATRIX_EXPECTED, "identity": identity,
            "negotiated_zero": {"executed": ZERO_EXPECTED, "independent_peer": False},
            "guarantee": "example-level; fixed draft-21 QUIC v1 scenarios only"}


def execution_identity(binary, test_binary):
    def digest(path):
        return hashlib.sha256(Path(path).read_bytes()).hexdigest()
    candidate = os.environ.get("QGF_SOURCE_COMMIT", "")
    tree = os.environ.get("QGF_SOURCE_TREE", "")
    image = os.environ.get("QGF_IMAGE_ID", "")
    if (any(len(value) != 40 or any(c not in "0123456789abcdef" for c in value) for value in (candidate, tree))
            or len(image) != 71 or not image.startswith("sha256:") or any(c not in "0123456789abcdef" for c in image[7:])):
        raise ValueError("matrix execution requires exact QGF_SOURCE_COMMIT, QGF_SOURCE_TREE and QGF_IMAGE_ID")
    version = subprocess.check_output(["go", "version"], text=True, timeout=10).strip()
    if version != "go version go1.27.3 linux/amd64":
        raise ValueError(f"unqualified primary toolchain: {version}")
    return {"candidate": candidate, "tree": tree, "image": image, "go": version,
            "kernel": {"system": platform.system(), "release": platform.release(), "version": platform.version(), "machine": platform.machine()},
            "picoquic": "01e124ebdf9fc50df307e01e71f2744471ddc470", "picotls": "bfa67875982afc4c24f21e146cef4747fa189c2f",
            "peer_binary_sha256": digest(binary), "test_binary_sha256": digest(test_binary) if test_binary else None,
            "runner_sha256": digest(__file__), "peer_driver_sha256": digest(Path(__file__).with_name("peer.c"))}


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
    parser.add_argument("--binary")
    parser.add_argument("--output", required=True, type=Path)
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--smoke", action="store_true", help="run R14's two-path smoke instead of the ordinary control")
    modes.add_argument("--matrix", action="store_true", help="run exactly one R17 scenario/role")
    modes.add_argument("--zero-vectors", action="store_true", help="run the separate negotiated-zero normative/controlled evidence")
    modes.add_argument("--validate-matrix", action="store_true", help="validate the 16 retained independent case receipts")
    parser.add_argument("--scenario", choices=MATRIX_SCENARIOS)
    parser.add_argument("--role", choices=("client", "server"))
    parser.add_argument("--test-binary", type=Path)
    args = parser.parse_args()
    if args.validate_matrix:
        result = check_matrix(args.output)
        with (args.output / "matrix-result.json").open("x") as output:
            json.dump(result, output, indent=2)
            output.write("\n")
        print(json.dumps(result))
        return
    if not args.binary:
        parser.error("--binary is required for execution")
    if not args.matrix and (args.scenario or args.role):
        parser.error("case selection requires --matrix")
    if args.test_binary and not (args.matrix or args.zero_vectors):
        parser.error("--test-binary requires --matrix or --zero-vectors")
    if args.matrix and (not args.scenario or not args.role):
        parser.error("--matrix requires --scenario and --role")
    check_prerequisites(args.binary, platform.system(), platform.machine())
    for executable in ("go", "stdbuf"):
        if shutil.which(executable) is None:
            raise ValueError(f"missing prerequisite: {executable}")
    if args.test_binary and (not args.test_binary.is_file() or not os.access(args.test_binary, os.X_OK)):
        raise ValueError(f"missing executable test binary: {args.test_binary}")
    identity = execution_identity(args.binary, args.test_binary) if args.matrix or args.zero_vectors else None
    args.output.mkdir(parents=True, exist_ok=False)
    env = os.environ | {"PICOQUIC_BINARY": str(Path(args.binary).resolve()), "PICOQUIC_LOG_DIR": str(args.output.resolve())}
    command = ["go", "test", "-json", "-tags=picoquic", "-run=^TestPicoquicOrdinaryControl$", "-count=1", "-timeout=90s", "./integrationtests/self", "-version=1"]
    package, expected = PACKAGE, EXPECTED
    if args.smoke:
        command = ["go", "test", "-json", "-tags=picoquic", "-run=^TestPicoquicMultipathSmoke$", "-count=1", "-timeout=90s", "."]
        package, expected = SMOKE_PACKAGE, SMOKE_EXPECTED
    if args.matrix:
        command, expected = matrix_invocation(args.scenario, args.role, args.test_binary)
        package = SMOKE_PACKAGE
    if args.zero_vectors:
        expression = "^TestMultipathNegotiatedZero(Vectors|ControlledPeer)$"
        command = ["go", "test", "-json", "-tags=picoquic", f"-run={expression}", "-count=1", "-timeout=90s", "."]
        if args.test_binary:
            command = ["go", "tool", "test2json", "-t", "-p", SMOKE_PACKAGE, str(args.test_binary),
                       "-test.v=test2json", f"-test.run={expression}", "-test.count=1", "-test.timeout=90s"]
        package, expected = SMOKE_PACKAGE, ZERO_EXPECTED
    started = datetime.now(timezone.utc).isoformat()
    before = time.monotonic()
    attempt = {"expected": expected, "identity": identity, "command": command, "started": started}
    (args.output / "attempt.json").write_text(json.dumps(attempt, indent=2) + "\n")
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
    result = {"executed": expected, "status": "pass", "identity": identity,
              "command": command, "started": started, "duration_seconds": time.monotonic() - before}
    (args.output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
