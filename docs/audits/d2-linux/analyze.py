#!/usr/bin/env python3
"""Summarize the fixed D2 sample set with prespecified paired uncertainty."""

import argparse
import json
import math
from pathlib import Path
import random
import statistics


def percentile(values, fraction):
    position = (len(values) - 1) * fraction
    low = int(position)
    high = min(low + 1, len(values) - 1)
    return values[low] + (values[high] - values[low]) * (position - low)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    records = [json.loads(line) for line in args.manifest.read_text().splitlines()]
    assert len(records) == 160, len(records)
    cases = ("SteadyDrain", "BurstDrain", "Overflow", "Concurrent")
    data = {case: {variant: {} for variant in ("base", "ring")} for case in cases}
    for record in records:
        assert record["returncode"] == 0
        lines = [line for line in record["stdout"].splitlines() if line.startswith("Benchmark")]
        assert len(lines) == 1, record
        tokens = lines[0].split()
        metrics = {tokens[i + 1]: float(tokens[i]) for i in range(2, len(tokens), 2)}
        metrics["offered"] = int(tokens[1])
        if record["case"] == "Concurrent":
            assert metrics["delivered"] + metrics["drops"] == metrics["offered"], metrics
        group = data[record["case"]][record["variant"]]
        assert record["round"] not in group
        group[record["round"]] = metrics
    output = {}
    for case in cases:
        group = data[case]
        for variant in group:
            assert sorted(group[variant]) == list(range(1, 21))
        primary = "ns/delivered" if case == "Concurrent" else "ns/op"
        ratios = [group["ring"][i][primary] / group["base"][i][primary] for i in range(1, 21)]
        logs = [math.log(ratio) for ratio in ratios]
        randomizer = random.Random(20260906)
        bootstrap = sorted(
            math.exp(statistics.mean(randomizer.choices(logs, k=20)))
            for _ in range(10000)
        )
        bounds = [percentile(bootstrap, 0.025), percentile(bootstrap, 0.975)]
        medians = {
            variant: {
                metric: statistics.median(group[variant][i][metric] for i in range(1, 21))
                for metric in group[variant][1]
            }
            for variant in group
        }
        output[case] = {
            "primary_metric": primary,
            "medians": medians,
            "paired_geomean_ratio": math.exp(statistics.mean(logs)),
            "paired_bootstrap_ci95": bounds,
            "within_5pct_budget": bounds[1] < 1.05,
            "sample_ratios": ratios,
        }
    print(json.dumps(output, indent=2))


if __name__ == "__main__":
    main()
