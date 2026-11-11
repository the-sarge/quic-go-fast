#!/usr/bin/env python3
"""E6 native qualification: a missing, skipped or unobserved class is a failed claim."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import time

GO_VERSIONS = ["go1.26.11", "go1.27.4"]
TOLERANCE_MS = 20.0
EAGAIN_RULE = "inject=sendmsg:error=EAGAIN"
# Injected classes run under strace, so their timing is attributed, never qualified.
CLASSES = {
    "expired-before-dispatch": {"rule": None, "timing": {"expiry", "successor_publication"}},
    "eagain-to-deadline": {"rule": EAGAIN_RULE, "timing": {"injected_native_return", "injected_publication"}},
    "retry-fallback": {"rule": EAGAIN_RULE + ":when=2+", "drop_udp": True, "timing": {"injected_retry_return", "injected_fallback_return"}},
    "queued-expiry": {"rule": EAGAIN_RULE, "timing": {"injected_queued_expiry", "injected_current_return"}},
    "shutdown-join": {"rule": None, "timing": {"public_write_return"}},
    "request-contention": {"rule": None, "timing": {"native_return_overrun", "publication", "pressure_native_return_overrun", "pressure_publication"}},
}
NAMESPACE_SETUP = (
    "ip link set lo up && nft add table inet qual && "
    "nft add chain inet qual out '{ type filter hook output priority 0; }' && "
    "nft add rule inet qual out meta l4proto udp drop"
)


def command_for(name, binary, trace):
    selector = f"-test.run=^TestNativeQualification$/^{name}$"
    test = [binary, selector, "-test.count=1", "-test.timeout=30s", "-test.v"]
    rule = CLASSES[name]["rule"]
    if rule is None:
        return test
    traced = ["strace", "-f", "-qq", "--seccomp-bpf", "-o", trace, "-e", "trace=sendmsg", "-e", rule, *test]
    if not CLASSES[name].get("drop_udp"):
        return traced
    # exec keeps the disposable namespace's lifetime equal to the traced process.
    quoted = " ".join(f"'{part}'" for part in traced)
    return ["sudo", "-n", "unshare", "-n", "sh", "-c", f"{NAMESPACE_SETUP} && exec {quoted}"]


def with_evidence(command, evidence):
    # sudo clears the environment, so a namespaced class receives it explicitly.
    if command[0] == "sudo":
        return [*command[:2], "env", f"NATIVE_QUAL_EVIDENCE={evidence}", *command[2:]]
    return command


def check_output(name, output):
    for word in ("SKIP", "FAIL"):
        if re.search(rf"^\s*(--- )?{word}\b", output, re.MULTILINE):
            raise ValueError(f"{word}: {name}")
    if output.count(f"--- PASS: TestNativeQualification/{name} ") != 1:
        raise ValueError(f"{name} must pass exactly once")


def check_trace(name, trace):
    # strace splits a call when another thread's event interleaves. The result is
    # on its resumed line.
    calls = [line for line in trace.splitlines() if "sendmsg resumed>" in line or (" sendmsg(" in line and not line.endswith("<unfinished ...>"))]
    injected = [line for line in calls if "EAGAIN" in line and line.endswith("(INJECTED)")]
    rule = CLASSES[name]["rule"]
    if rule is None:
        if "INJECTED" in trace:
            raise ValueError(f"{name} is uninstrumented: no injection may appear")
        return
    if CLASSES[name].get("drop_udp"):
        real_eperm = bool(calls) and "EPERM" in calls[0] and "INJECTED" not in calls[0]
        if not real_eperm or len(injected) < 2 or len(injected) != len(calls) - 1:
            raise ValueError(f"{name}: retry needs one real EPERM, then at least two injected EAGAIN calls")
        return
    if not injected:
        raise ValueError(f"{name}: no injected sendmsg in the trace")


def check_evidence(name, go, evidence):
    if evidence.get("class") != name:
        raise ValueError(f"evidence class {evidence.get('class')} is not {name}")
    if evidence.get("go") != go:
        raise ValueError(f"evidence toolchain {evidence.get('go')} is not {go}")
    timing = evidence.get("timing_ms") or {}
    missing = CLASSES[name]["timing"] - timing.keys()
    if missing:
        raise ValueError(f"{name}: timing not recorded: {sorted(missing)}")
    if CLASSES[name]["rule"] is not None:
        if evidence.get("traversal") is not True:
            raise ValueError(f"{name}: native poll traversal was not observed")
        return
    late = {key: value for key, value in timing.items() if value > TOLERANCE_MS}
    if late:
        raise ValueError(f"{name}: over the {TOLERANCE_MS} ms tolerance: {late}")


def check_accounting(instances):
    expected = sorted((go, name) for go in GO_VERSIONS for name in CLASSES)
    if sorted(instances) != expected:
        raise ValueError(f"expected the 12 fixed instances exactly once, got {len(instances)}")


def check_prerequisites(system, machine, which):
    if system.lower() != "linux" or machine not in ("x86_64", "amd64"):
        raise ValueError("native qualification requires Linux amd64")
    for tool in ("go", "strace", "sudo", "unshare", "ip", "nft"):
        if which(tool) is None:
            raise ValueError(f"missing prerequisite: {tool}")


def capture(*command, env=None):
    return subprocess.run(command, env=env, check=True, capture_output=True, text=True).stdout.strip()


def build(go, output):
    env = os.environ | {"GOTOOLCHAIN": go, "CGO_ENABLED": "0", "GOFLAGS": "-buildvcs=false"}
    reported = capture("go", "env", "GOVERSION", env=env)
    if reported != go:
        raise ValueError(f"requested {go}, got {reported}")
    binary = output / f"{go}.test"
    capture("go", "test", "-c", "-tags=nativequal", "-o", str(binary), ".", env=env)
    source = Path(capture("go", "env", "GOROOT", env=env)) / "src/internal/poll/fd_unix.go"
    return binary, hashlib.sha256(source.read_bytes()).hexdigest()


def run_instance(go, name, binary, output):
    directory = output / go / name
    directory.mkdir(parents=True)
    evidence_path = directory / "evidence.json"
    trace_path = directory / "strace.log"
    env = os.environ | {"NATIVE_QUAL_EVIDENCE": str(evidence_path)}
    command = with_evidence(command_for(name, str(binary.resolve()), str(trace_path)), evidence_path)
    load_before = Path("/proc/loadavg").read_text().strip()
    started = time.monotonic()
    done = subprocess.run(command, env=env, capture_output=True, text=True, timeout=60)
    elapsed = time.monotonic() - started
    if CLASSES[name].get("drop_udp"):
        capture("sudo", "-n", "chown", "-R", f"{os.getuid()}:{os.getgid()}", str(directory))
    (directory / "test.log").write_text(done.stdout + done.stderr)
    record = {"go": go, "class": name, "command": command, "exit": done.returncode, "seconds": round(elapsed, 3), "load_before": load_before, "load_after": Path("/proc/loadavg").read_text().strip()}
    if done.returncode != 0:
        return record, f"exit {done.returncode}"
    try:
        check_output(name, done.stdout)
        check_trace(name, trace_path.read_text() if trace_path.exists() else "")
        check_evidence(name, go, json.loads(evidence_path.read_text()))
    except (ValueError, OSError) as error:
        return record, str(error)
    return record, None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--candidate", required=True)
    args = parser.parse_args()
    check_prerequisites(platform.system(), platform.machine(), shutil.which)
    args.output.mkdir(parents=True, exist_ok=False)
    results = {
        "candidate": args.candidate,
        "kernel": capture("uname", "-a"),
        "os_release": Path("/etc/os-release").read_text(),
        "strace": capture("strace", "-V").splitlines()[0],
        "toolchains": {},
        "instances": [],
    }
    binaries = {}
    # Stop at the first unresolved failure. There is no repeat-until-pass.
    failure = None
    for go in GO_VERSIONS:
        binaries[go], poll_hash = build(go, args.output)
        results["toolchains"][go] = {"internal/poll/fd_unix.go": poll_hash}
    for go in GO_VERSIONS:
        for name in CLASSES:
            if failure is None:
                record, failure = run_instance(go, name, binaries[go], args.output)
                record["failure"] = failure
                results["instances"].append(record)
    (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    if failure is not None:
        raise SystemExit(f"stopped: {failure}")
    check_accounting([(record["go"], record["class"]) for record in results["instances"]])
    print(json.dumps({"instances": len(results["instances"]), "status": "pass"}))


if __name__ == "__main__":
    main()
