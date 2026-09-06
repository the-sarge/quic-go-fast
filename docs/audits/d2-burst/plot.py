#!/usr/bin/env python3
"""Render descriptive medians for the two pacing modes; requires Matplotlib."""

import json
from pathlib import Path
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.ticker import FixedLocator, FuncFormatter

root = Path(sys.argv[1])
x = json.loads((root/"analysis.json").read_text())
cases = [f"{rate}/{mode}" for rate in (10000, 100000, 500000) for mode in ("internal", "external")]
labels = []
for case in cases:
    rate, mode = case.split("/")
    star = " *" if any(v["unqualified_rounds"] for v in x["cases"][case]["variants"].values()) else ""
    labels.append(f"{int(rate)//1000}k/s · {mode} pacer{star}")
fig, axes = plt.subplots(1, 3, figsize=(13, 5.7), sharey=True)
for ax, metric, title in zip(axes, ("p99_us", "bytes_per_delivered", "cpu_us_per_delivered"),
                           ("p99 queue latency (µs)", "Allocated bytes / delivery", "Process CPU / delivery (µs)")):
    for variant, offset, color, name in (("base", -.10, "#777777", "Current slice"), ("head-index", .10, "#2166ac", "Head-index")):
        values = [x["cases"][case]["variants"][variant]["medians"][metric] for case in cases]
        ax.scatter(values, [i+offset for i in range(len(cases))], color=color, s=35, label=name)
    ax.set_title(title, fontsize=11, pad=15)
    if metric != "bytes_per_delivered":
        ax.set_xscale("log")
        ax.xaxis.set_major_locator(FixedLocator([40, 60, 100, 200] if metric == "p99_us" else [.4, 1, 10, 100]))
        ax.xaxis.set_major_formatter(FuncFormatter(lambda value, _: f"{value:g}"))
        ax.xaxis.set_minor_formatter(FuncFormatter(lambda value, _: ""))
    ax.grid(axis="x", alpha=.2)
    ax.tick_params(axis="y", length=0)
    for spine in ("top", "right", "left"):
        ax.spines[spine].set_visible(False)
    ax.axhspan(1.5, 3.5, color="#000000", alpha=.03)
axes[0].set_yticks(range(6), labels, fontsize=10)
axes[0].invert_yaxis()
handles, names = axes[2].get_legend_handles_labels()
fig.legend(handles, names, frameon=False, loc="upper right", bbox_to_anchor=(.99, .95), fontsize=9, ncol=2)
fig.suptitle("Burst diagnosis on reserved physical cores", fontsize=16, x=.04, ha="left")
fig.text(.04, .90, "Median of 20 samples per point · bursts of 32 · 1,071-byte payloads · minimax/Linux", fontsize=10, color="#555555")
qualification = "All samples met the 98% offered-rate qualification." if not any("*" in label for label in labels) else "* At least one sample missed 98% of target rate; interpret at achieved rates."
fig.text(.04, .03, "Internal process CPU includes busy pacing; external process CPU excludes the separate pacer but includes local IPC.\n" + qualification + " Paired intervals are in the report.\nTiming samples exclude profiler overhead. This is a queue scheduling intervention, not a full QUIC transfer.", fontsize=9, color="#555555")
fig.tight_layout(rect=(.03, .15, 1, .89), w_pad=2)
fig.savefig(root/"comparison.png", dpi=180)
