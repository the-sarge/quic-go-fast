#!/usr/bin/env python3
"""Post-process all diagnostic profiles after measurement and isolation cleanup."""

import json
from pathlib import Path
import re
import subprocess
import sys


root = Path(sys.argv[1]).resolve()
results = root / "results"
records = [json.loads(line) for line in (results/"profiles.jsonl").read_text().splitlines()]
assert len(records) == 36
summary = {}
for record in records:
    assert record["returncode"] == record.get("pacer_returncode", 0) == 0
    rate, mode = record["case"].split("/")
    variant, phase = record["variant"], record["phase"]
    key = f"{rate}-{mode}-{variant}"
    prefix = results / "profiles" / key
    profile_types = ["cpu"] if phase == "cpu" else ["block", "mutex"] if phase == "wait" else ["sched", "sync", "syscall", "net"]
    for kind in profile_types:
        path = Path(str(prefix) + f".{kind}.pprof")
        if phase == "trace":
            profile = subprocess.check_output(["go", "tool", "trace", f"-pprof={kind}", str(prefix)+".trace"])
            path.write_bytes(profile)
        base = ["go", "tool", "pprof"]
        binary = str(root / f"{variant}.test")
        top = subprocess.check_output(base + ["-top", "-nodecount=40", "-unit=ms", binary, str(path)], text=True, stderr=subprocess.STDOUT)
        Path(str(prefix)+f".{kind}.top.log").write_text(top)
        raw = subprocess.check_output(base + ["-raw", binary, str(path)], text=True)
        section = raw.split("Samples:\n", 1)[1].split("Locations\n", 1)[0]
        headers = section.splitlines()[0].split()
        samples = [list(map(int, match.groups())) for line in section.splitlines() if (match := re.match(r"\s*(\d+)\s+(\d+):", line))]
        if samples:
            assert len(headers) == 2
        totals = [sum(sample[i] for sample in samples) for i in range(2)]
        measured = json.loads(next(line[len("QUEUE_RESULT "):] for line in record["stdout"].splitlines() if line.startswith("QUEUE_RESULT ")))
        summary.setdefault(key, {})[kind] = {"sample_types": headers, "totals": totals,
            "delivered_in_profile_run": measured["delivered"], "offered_in_profile_run": measured["offered"],
            "miss_pct_in_profile_run": 100*measured["generator_skipped"]/measured["planned"],
            "duration_seconds": measured["duration_seconds"]}
    print(f"Processed {key} {phase}", flush=True)
(results/"profile-summary.json").write_text(json.dumps(summary, indent=2, sort_keys=True)+"\n")
