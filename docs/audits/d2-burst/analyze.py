#!/usr/bin/env python3
"""Analyze the complete isolated 240-sample timing matrix, without exclusions."""

import gzip
import json
import math
from pathlib import Path
import random
import statistics
import sys


def interval(values, ratio=False):
    values = [math.log(v) for v in values] if ratio else values
    rng = random.Random(20260906)
    boot = sorted(statistics.fmean(rng.choices(values, k=len(values))) for _ in range(10000))
    def percentile(q):
        x = q * (len(boot)-1)
        lo = int(x)
        return boot[lo] + (boot[min(lo+1, len(boot)-1)]-boot[lo])*(x-lo)
    convert = math.exp if ratio else lambda v: v
    return {"estimate": convert(statistics.fmean(values)), "ci95": [convert(percentile(.025)), convert(percentile(.975))]}


def result(record, prefix="QUEUE_RESULT ", field="stdout"):
    lines = [line[len(prefix):] for line in record[field].splitlines() if line.startswith(prefix)]
    assert len(lines) == 1
    return json.loads(lines[0])


def metrics(r):
    return {"offered_per_second": r["offered_per_second"], "delivered_per_second": r["delivered_per_second"],
            "miss_pct": 100*r["generator_skipped"]/r["planned"], "queue_drop_pct": 100*r["queue_drops"]/r["offered"],
            "p50_us": r["queue_latency"]["p50_us_upper"], "p99_us": r["queue_latency"]["p99_us_upper"],
            "mean_latency_us": r["queue_latency"]["mean_us"],
            "bytes_per_delivered": r["allocated_bytes"]/r["delivered"], "allocations_per_delivered": r["allocations"]/r["delivered"],
            "cpu_us_per_delivered": r["process_cpu_seconds"]*1e6/r["delivered"],
            "gc_cycles": r["gc_cycles"], "gc_pause_ms": r["gc_pause_ns"]/1e6,
            "external_expired_pct": 100*r.get("external_expired", 0)/r["planned"],
            "external_ipc_miss_pct": 100*r.get("external_ipc_skipped", 0)/r["planned"]}


root = Path(sys.argv[1])
with gzip.open(root/"samples.jsonl.gz", "rt") as source:
    records = [json.loads(line) for line in source]
cases = [(rate, mode) for rate in (10000, 100000, 500000) for mode in ("internal", "external")]
expected = []
for index in range(20):
    offset = index % len(cases)
    variants = ("base", "head-index") if index % 2 == 0 else ("head-index", "base")
    expected.extend((index+1, f"{rate}/{mode}", variant) for rate, mode in cases[offset:]+cases[:offset] for variant in variants)
assert [(r["round"], r["case"], r["variant"]) for r in records] == expected
groups = {}
for record in records:
    assert record["phase"] == "timing" and record["returncode"] == record.get("pacer_returncode", 0) == 0
    for boundary in ("isolation_before", "isolation_after"):
        receipt = record[boundary]
        assert receipt["cpuset.cpus.effective"] == "0-7,11-23,27-31"
        assert receipt["codex-d2-burst-20260906/cpuset.cpus.partition"] == "root"
        assert receipt["codex-d2-burst-20260906/cpuset.cpus.exclusive.effective"] == "8-10,24-26"
    r = result(record)
    assert r["duration_seconds"] == 2 and r["burst"] == 32 and not r["stall"]
    assert record["case"] == f"{r['rate']}/{r['pacing_mode']}"
    assert r["offered"] + r["generator_skipped"] == r["planned"]
    assert r["delivered"] + r["queue_drops"] == r["offered"]
    assert r["queue_latency"]["count"] == r["delivered"] and r["queue_latency"]["overflow_count"] == 0
    assert r["arrival_lateness"]["count"]*32 == r["offered"]
    if r["pacing_mode"] == "external":
        p = result(record, "PACER_RESULT ", "pacer_stdout")
        assert p["sent_ticks"]*32 == r["offered"]+r["external_expired"]
        assert p["generator_skipped_ticks"]*32 == r["external_generator_skipped"]
        assert p["ipc_skipped_ticks"]*32 == r["external_ipc_skipped"]
        assert r["generator_skipped"] == r["external_expired"]+r["external_generator_skipped"]+r["external_ipc_skipped"]
    groups.setdefault(record["case"], {})[(record["round"], record["variant"])] = r

output = {"sample_count": len(records), "first_started_utc": records[0]["started_utc"], "last_started_utc": records[-1]["started_utc"], "cases": {}, "mode_ratios": {}}
ratios = ("p99_us", "p50_us", "bytes_per_delivered", "allocations_per_delivered", "cpu_us_per_delivered", "delivered_per_second")
for case, samples in groups.items():
    m = {key: metrics(r) for key, r in samples.items()}
    summary = {"variants": {}, "paired_ratios": {}, "paired_differences": {}}
    for variant in ("base", "head-index"):
        rows = [m[(index, variant)] for index in range(1, 21)]
        summary["variants"][variant] = {"medians": {key: statistics.median(row[key] for row in rows) for key in rows[0]},
            "unqualified_rounds": [index for index in range(1, 21) if samples[(index, variant)]["offered_per_second"] < .98*samples[(index, variant)]["rate"]],
            "min_rate_fraction": min(samples[(index, variant)]["offered_per_second"]/samples[(index, variant)]["rate"] for index in range(1, 21))}
    for metric in ratios:
        summary["paired_ratios"][metric] = interval([m[(i, "head-index")][metric]/m[(i, "base")][metric] for i in range(1, 21)], True)
    for metric in ("queue_drop_pct", "miss_pct", "gc_cycles", "gc_pause_ms"):
        summary["paired_differences"][metric] = interval([m[(i, "head-index")][metric]-m[(i, "base")][metric] for i in range(1, 21)])
    output["cases"][case] = summary
for rate in (10000, 100000, 500000):
    output["mode_ratios"][str(rate)] = {}
    for variant in ("base", "head-index"):
        output["mode_ratios"][str(rate)][variant] = interval([metrics(groups[f"{rate}/external"][(i, variant)])["p99_us"]/metrics(groups[f"{rate}/internal"][(i, variant)])["p99_us"] for i in range(1, 21)], True)
activity = {str(cpu): [] for cpu in range(32)}
for line in (root/"cpu-activity.log").read_text().splitlines():
    parts = line.split()
    if len(parts) == 12 and parts[0][0].isdigit() and parts[1] in activity:
        activity[parts[1]].append([float(v) for v in parts[2:]])
output["host"] = {cpu: {"mean_idle_pct": statistics.fmean(row[-1] for row in rows), "max_guest_pct": max(row[7] for row in rows),
                         "max_irq_pct": max(row[4]+row[5] for row in rows)} for cpu, rows in activity.items()}
assert json.loads((root/"isolation-after.json").read_text())["cpuset.cpus.effective"] == "0-31"
print(json.dumps(output, indent=2, sort_keys=True))
