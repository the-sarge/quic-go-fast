#!/usr/bin/env python3
"""Per-block peak-RSS excess of r9 over frozen Reno, in MiB, from #739's readiness data.

Reads the r9 Linux re-demonstration's summary.json and pairs each block's
candidate (cand, bbrv3) observation with the same block's frozen Reno
(reno, reno, untagged) observation. Writes memory-excess.json beside this script.
Descriptive only: readiness flags use the median of per-block ratios, which this
script does not recompute.
"""
import json
import pathlib
import statistics
import sys

HERE = pathlib.Path(__file__).resolve().parent
DEFAULT = HERE.parent / "2026-10-08-bbr-r9-linux-redemonstration" / "summary.json"


def main(argv):
    src = pathlib.Path(argv[1]) if len(argv) > 1 else DEFAULT
    data = json.loads(src.read_text())
    blocks = {}
    for obs in data["observations"]:
        if obs["phase"] != "main":
            continue
        arm = (obs["variant"], obs["controller"], obs["tag"])
        if arm not in (("cand", "bbrv3", ""), ("reno", "reno", "")):
            continue
        blocks.setdefault((obs["path"], obs["workload"], obs["pair"]), {})[arm[0]] = obs
    cells = {}
    for (path, workload, pair), arms in sorted(blocks.items()):
        if set(arms) != {"cand", "reno"}:
            continue
        for endpoint in ("send", "receive"):
            cell = cells.setdefault(f"{path} {workload} {endpoint}", {"blocks": []})
            reno = arms["reno"][endpoint]["peak_rss_mib"]
            cand = arms["cand"][endpoint]["peak_rss_mib"]
            cell["blocks"].append({"pair": pair, "reno_mib": reno, "candidate_mib": cand, "excess_mib": cand - reno})
    for cell in cells.values():
        excess = [b["excess_mib"] for b in cell["blocks"]]
        cell["median_excess_mib"] = statistics.median(excess)
        cell["max_excess_mib"] = max(excess)
        cell["reno_median_mib"] = statistics.median(b["reno_mib"] for b in cell["blocks"])
        cell["candidate_median_mib"] = statistics.median(b["candidate_mib"] for b in cell["blocks"])
    out = {"source": str(src.relative_to(HERE.parent)) if src.is_relative_to(HERE.parent) else str(src), "instrument": "peak_rss_mib (ru_maxrss), readiness instrument", "cells": cells}
    (HERE / "memory-excess.json").write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
    for name, cell in sorted(cells.items()):
        print(f"{name:28} reno {cell['reno_median_mib']:6.2f}  cand {cell['candidate_median_mib']:6.2f}  excess median {cell['median_excess_mib']:6.2f}  max {cell['max_excess_mib']:6.2f}")


if __name__ == "__main__":
    main(sys.argv)
