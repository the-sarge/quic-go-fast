#!/usr/bin/env python3
"""Analyze all 360 fixed paced samples; never filter on rate or outcome."""

import datetime
import gzip
import json
import math
from pathlib import Path
import random
import statistics
import sys


def percentile(values, fraction):
    values = sorted(values)
    index = (len(values) - 1) * fraction
    low = int(index)
    high = min(low + 1, len(values) - 1)
    return values[low] + (values[high] - values[low]) * (index - low)


def interval(values, ratio=False):
    values = [math.log(value) for value in values] if ratio else values
    rng = random.Random(20260906)
    boot = [statistics.fmean(rng.choices(values, k=len(values))) for _ in range(10000)]
    transform = math.exp if ratio else lambda value: value
    return {"estimate": transform(statistics.fmean(values)),
            "ci95": [transform(percentile(boot, 0.025)), transform(percentile(boot, 0.975))]}


def metrics(result):
    return {
        "offered_per_second": result["offered_per_second"],
        "delivered_per_second": result["delivered_per_second"],
        "generator_miss_pct": 100 * result["generator_skipped"] / result["planned"],
        "queue_drop_pct": 100 * result["queue_drops"] / result["offered"],
        "bytes_per_delivered": result["allocated_bytes"] / result["delivered"],
        "allocations_per_delivered": result["allocations"] / result["delivered"],
        "p50_us": result["queue_latency"]["p50_us_upper"],
        "p99_us": result["queue_latency"]["p99_us_upper"],
        "mean_latency_us": result["queue_latency"]["mean_us"],
        "max_latency_us": result["queue_latency"]["max_us"],
        "p99_lateness_us": result["arrival_lateness"]["p99_us_upper"],
        "pause_mean_us": result["receiver_pause"].get("mean_us", 0),
        "pause_count": result["receiver_pause"]["count"],
        "gc_cycles": result["gc_cycles"],
        "gc_pause_ms": result["gc_pause_ns"] / 1e6,
        "cpu_us_per_delivered_including_pacer": 1e6 * result["process_cpu_seconds_including_pacer"] / result["delivered"],
    }


def host_summary(records, root):
    cpus = {cpu: [] for cpu in ("12", "13", "28", "29")}
    intervals = []
    date = records[0]["started_utc"].split("T")[0]
    assert all(record["started_utc"].startswith(date) for record in records)
    for line in (root / "cpu-activity.log").read_text().splitlines():
        fields = line.split()
        if len(fields) == 12 and fields[0][0].isdigit() and fields[1] in cpus:
            cpus[fields[1]].append([float(value) for value in fields[2:]])
            end = datetime.datetime.fromisoformat(date + "T" + fields[0] + "+00:00").timestamp()
            intervals.append((end, float(fields[9])))
    activity = {}
    for cpu, rows in cpus.items():
        assert rows, cpu
        activity[cpu] = {"samples": len(rows), "mean_idle_pct": statistics.fmean(row[-1] for row in rows),
                         "min_idle_pct": min(row[-1] for row in rows), "max_steal_pct": max(row[6] for row in rows),
                         "max_guest_pct": max(row[7] for row in rows)}
    frequency = {}
    for variant in ("base", "head-index"):
        frequency[variant] = {}
        for cpu in cpus:
            values = [int(record[boundary][cpu]["scaling_cur_freq"]) for record in records if record["variant"] == variant
                      for boundary in ("frequency_before", "frequency_after")]
            frequency[variant][cpu] = {"median_khz": statistics.median(values), "min_khz": min(values), "max_khz": max(values)}
    overlap = {}
    for record in records:
        start = datetime.datetime.fromisoformat(record["started_utc"]).timestamp()
        end = start + record["wall_seconds"]
        rounds = overlap.setdefault(record["case"], {}).setdefault(record["variant"], [])
        if any(tick > start and tick - 1 < end and guest > 0 for tick, guest in intervals):
            rounds.append(record["round"])
    return {"activity": activity, "boundary_frequency": frequency,
            "rounds_overlapping_guest_activity_approx_1s": overlap}


def main():
    path = Path(sys.argv[1])
    opener = gzip.open if path.suffix == ".gz" else open
    with opener(path, "rt") as source:
        records = [json.loads(line) for line in source]
    assert len(records) == 360, len(records)
    cases = [f"{rate}/{shape}" for rate in (10000, 100000, 500000) for shape in ("single", "burst32", "pause")]
    expected_order = []
    for round_index in range(20):
        offset = round_index % len(cases)
        variants = ("base", "head-index") if round_index % 2 == 0 else ("head-index", "base")
        expected_order.extend((round_index + 1, case, variant) for case in cases[offset:] + cases[:offset] for variant in variants)
    assert [(record["round"], record["case"], record["variant"]) for record in records] == expected_order
    groups = {}
    for record in records:
        assert record["returncode"] == 0, record
        lines = [line.removeprefix("QUEUE_RESULT ") for line in record["stdout"].splitlines() if line.startswith("QUEUE_RESULT ")]
        assert len(lines) == 1, record
        result = json.loads(lines[0])
        assert result["delivered"] + result["queue_drops"] == result["offered"]
        assert result["offered"] + result["generator_skipped"] == result["planned"]
        assert result["queue_latency"]["count"] == result["delivered"]
        assert result["arrival_lateness"]["count"] * result["burst"] == result["offered"]
        assert result["queue_latency"]["overflow_count"] == 0
        assert result["duration_seconds"] == 2
        shape = "pause" if result["stall"] else "single" if result["burst"] == 1 else "burst32"
        assert record["case"] == f"{result['rate']}/{shape}"
        key = (record["round"], record["variant"])
        case = groups.setdefault(record["case"], {})
        assert key not in case
        case[key] = result
    assert len(groups) == 9
    output = {"sample_count": len(records), "first_started_utc": records[0]["started_utc"],
              "last_started_utc": records[-1]["started_utc"], "host": host_summary(records, path.parent), "cases": {}}
    ratios = ("delivered_per_second", "bytes_per_delivered", "allocations_per_delivered", "p50_us", "p99_us", "mean_latency_us", "cpu_us_per_delivered_including_pacer")
    for name, samples in groups.items():
        assert set(samples) == {(round_index, variant) for round_index in range(1, 21) for variant in ("base", "head-index")}
        measured = {key: metrics(value) for key, value in samples.items()}
        summary = {"variants": {}, "paired_ratios": {}, "paired_differences": {}}
        for variant in ("base", "head-index"):
            rows = [measured[(index, variant)] for index in range(1, 21)]
            unqualified = [index for index in range(1, 21) if samples[(index, variant)]["offered_per_second"] < 0.98 * samples[(index, variant)]["rate"]]
            summary["variants"][variant] = {
                "medians": {metric: statistics.median(row[metric] for row in rows) for metric in rows[0]},
                "unqualified_rounds": unqualified,
                "min_achieved_rate_fraction": min(samples[(index, variant)]["offered_per_second"] / samples[(index, variant)]["rate"] for index in range(1, 21)),
            }
        for metric in ratios:
            summary["paired_ratios"][metric] = interval([measured[(index, "head-index")][metric] / measured[(index, "base")][metric] for index in range(1, 21)], ratio=True)
        for metric in ("queue_drop_pct", "generator_miss_pct", "gc_cycles", "gc_pause_ms"):
            summary["paired_differences"][metric] = interval([measured[(index, "head-index")][metric] - measured[(index, "base")][metric] for index in range(1, 21)])
        output["cases"][name] = summary
    print(json.dumps(output, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
