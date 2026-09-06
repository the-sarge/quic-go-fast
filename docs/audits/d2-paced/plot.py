#!/usr/bin/env python3
"""Render the fixed paired estimates; requires matplotlib."""

import json
from pathlib import Path
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt


root = Path(sys.argv[1])
analysis = json.loads((root / "analysis.json").read_text())
cases = [f"{rate}/{shape}" for rate in (10000, 100000, 500000) for shape in ("single", "burst32", "pause")]
labels = []
for case in cases:
    rate, shape = case.split("/")
    unqualified = any(v["unqualified_rounds"] for v in analysis["cases"][case]["variants"].values())
    shape = {"single": "individual", "burst32": "burst 32", "pause": "burst 32 + pauses"}[shape]
    labels.append(f"{int(rate)//1000}k/s · {shape}" + (" *" if unqualified else ""))

fig, axes = plt.subplots(1, 3, figsize=(13, 6), sharey=True)
for axis, metric, title, ratio in zip(
    axes,
    ("bytes_per_delivered", "p99_us", "queue_drop_pct"),
    ("Allocated bytes / delivery\nchange (%)", "p99 queue latency\nchange (%)", "Queue drop rate\nchange (percentage points)"),
    (True, True, False),
):
    for index, case in enumerate(cases):
        group = "paired_ratios" if ratio else "paired_differences"
        result = analysis["cases"][case][group][metric]
        convert = (lambda value: (value - 1) * 100) if ratio else (lambda value: value)
        point = convert(result["estimate"])
        lower, upper = map(convert, result["ci95"])
        axis.errorbar(point, index, xerr=[[point - lower], [upper - point]], fmt="o", markersize=5,
                      capsize=3, color="#2166ac", linewidth=1.5)
    axis.axvline(0, color="#777777", linewidth=1, linestyle="--")
    axis.set_title(title, fontsize=11, pad=12)
    axis.grid(axis="x", alpha=0.18)
    for spine in ("top", "right", "left"):
        axis.spines[spine].set_visible(False)
    axis.tick_params(axis="y", length=0)
    axis.axhspan(2.5, 5.5, color="#000000", alpha=0.025)
axes[0].set_yticks(range(len(cases)), labels, fontsize=10)
axes[0].invert_yaxis()
fig.suptitle("Paced receive queue: head-index versus current slice", fontsize=16, x=0.04, ha="left")
fig.text(0.04, 0.90, "20 interleaved pairs per case · 2 seconds · 1,071-byte payloads · minimax/Linux", fontsize=10, color="#555555")
fig.text(0.04, 0.045, "Points: paired estimates; bars: bootstrap 95% intervals. Lower favors head-index.\n* At least one sample missed 98% of its requested rate; interpret at achieved rates. Synthetic queue workload, not a network transfer.", fontsize=9, color="#555555")
fig.tight_layout(rect=(0.03, 0.11, 1, 0.89), w_pad=2)
fig.savefig(root / "comparison.png", dpi=180)
fig.savefig(root / "comparison.svg")
