#!/usr/bin/env python3
"""Fixed, one-shot paired collection for the paced queue experiment."""

import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import time


def frequencies():
    return {
        str(cpu): {
            name: (Path(f"/sys/devices/system/cpu/cpu{cpu}/cpufreq") / name).read_text().strip()
            for name in ("scaling_cur_freq", "scaling_governor")
        }
        for cpu in (12, 13, 28, 29)
    }


def main():
    root = Path(sys.argv[1]).resolve()
    results = root / "results"
    results.mkdir(exist_ok=True)
    cases = [(rate, shape, burst, stall) for rate in (10000, 100000, 500000)
             for shape, burst, stall in (("single", 1, "0"), ("burst32", 32, "0"), ("pause", 32, "1"))]
    env = dict(os.environ, GOMAXPROCS="2", GOTOOLCHAIN="local", LC_ALL="C", QUEUE_EXPERIMENT="1", QUEUE_DURATION_MS="2000")
    # Open exclusively before starting the monitor or any sample.
    with (results / "samples.jsonl").open("x") as manifest, (results / "cpu-activity.log").open("x") as cpu_log:
        monitor = subprocess.Popen(["mpstat", "-P", "12,13,28,29", "1"], stdout=cpu_log, env=env)
        try:
            for round_index in range(20):
                variants = ("base", "head-index") if round_index % 2 == 0 else ("head-index", "base")
                offset = round_index % len(cases)
                for rate, shape, burst, stall in cases[offset:] + cases[:offset]:
                    sample_env = dict(env, QUEUE_RATE=str(rate), QUEUE_BURST=str(burst), QUEUE_STALL=stall)
                    for variant in variants:
                        command = ["taskset", "-c", "12,13", str(root / f"{variant}.test"),
                                   "-test.run", "^TestDatagramReceivePacedExperiment$", "-test.v", "-test.count=1", "-test.timeout=15s"]
                        record = {"round": round_index + 1, "case": f"{rate}/{shape}", "variant": variant,
                                  "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "command": command,
                                  "environment": {k: v for k, v in sample_env.items() if k.startswith("QUEUE_") or k in ("GOMAXPROCS", "GOTOOLCHAIN", "LC_ALL")},
                                  "frequency_before": frequencies()}
                        start = time.monotonic()
                        run = subprocess.run(command, env=sample_env, capture_output=True, text=True)
                        record.update(returncode=run.returncode, stdout=run.stdout, stderr=run.stderr,
                                      wall_seconds=time.monotonic() - start, frequency_after=frequencies())
                        manifest.write(json.dumps(record) + "\n")
                        manifest.flush()
                        if run.returncode:
                            raise SystemExit(f"Failed sample: {record['case']} {variant}, round {round_index + 1}")
                print(f"Completed paired round {round_index + 1}/20", flush=True)
        finally:
            monitor.terminate()
            monitor.wait()


if __name__ == "__main__":
    main()
