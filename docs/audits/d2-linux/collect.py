#!/usr/bin/env python3
"""One-shot D2 Linux collection; consumes the two prebuilt binaries for either fixed phase."""

import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import time


def frequency_snapshot():
    result = {}
    for cpu in (12, 13, 28, 29):
        root = Path(f"/sys/devices/system/cpu/cpu{cpu}/cpufreq")
        result[str(cpu)] = {
            name: (root / name).read_text().strip()
            for name in ("scaling_cur_freq", "scaling_governor", "energy_performance_preference")
            if (root / name).exists()
        }
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("--candidate", choices=("ring", "head-index"), default="ring")
    parser.add_argument("--partial-refill", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve()
    results = root / "results"
    manifest = results / "samples.jsonl"
    if manifest.exists():
        raise SystemExit("Refusing to overwrite or resume an existing collection")
    env = dict(os.environ, GOMAXPROCS="2", GOTOOLCHAIN="local", LC_ALL="C", QUEUE_EXPERIMENT="1")
    cases = [
        ("SteadyDrain", "^BenchmarkDatagramReceive$/^SteadyDrain$"),
        ("BurstDrain", "^BenchmarkDatagramReceive$/^BurstDrain$"),
        ("Overflow", "^BenchmarkDatagramReceive$/^Overflow$"),
        ("Concurrent", "^BenchmarkDatagramReceiveConcurrent$"),
    ]
    if args.partial_refill:
        cases.append(("PartialRefill", "^BenchmarkDatagramReceivePartialRefill$"))
    with (results / "cpu-activity.txt").open("x") as cpu_log:
        monitor = subprocess.Popen(
            ["mpstat", "-P", "12,13,28,29", "1"], stdout=cpu_log, env=env
        )
        try:
            with manifest.open("x") as records:
                for round_index in range(20):
                    variants = ("base", args.candidate) if round_index % 2 == 0 else (args.candidate, "base")
                    offset = round_index % len(cases)
                    for case, pattern in cases[offset:] + cases[:offset]:
                        for variant in variants:
                            command = [
                                "taskset", "-c", "12,13", str(root / f"{variant}.test"),
                                "-test.run", "^$", "-test.bench", pattern,
                                "-test.benchmem", "-test.benchtime=1s", "-test.count=1",
                                "-test.timeout=30s",
                            ]
                            record = {
                                "round": round_index + 1,
                                "case": case,
                                "variant": variant,
                                "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                                "command": command,
                                "frequency_before": frequency_snapshot(),
                            }
                            start = time.monotonic()
                            run = subprocess.run(command, env=env, text=True, capture_output=True)
                            record.update(
                                wall_seconds=time.monotonic() - start,
                                frequency_after=frequency_snapshot(),
                                returncode=run.returncode,
                                stdout=run.stdout,
                                stderr=run.stderr,
                            )
                            records.write(json.dumps(record) + "\n")
                            records.flush()
                            with (results / f"{variant}.txt").open("a") as output:
                                output.write(f"# round {round_index + 1} {case}\n" + run.stdout)
                            if run.returncode:
                                raise SystemExit(f"Sample failed: {record}")
                    print(f"Completed paired round {round_index + 1}/20", flush=True)
        finally:
            monitor.terminate()
            monitor.wait()


if __name__ == "__main__":
    main()
