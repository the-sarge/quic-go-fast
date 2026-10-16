#!/usr/bin/env python3
"""Figures for each raised cell of the port's readiness record, from its JSON files.

Reads summary.json, attribution.json and r9-context.json of the readiness record
(default: ../2026-10-16-bbr-port-readiness) and writes raised-cells.json beside
this script. For every raised flag it gives the registered ratio (median of five
per-block ratios), blocks crossing, the per-block range, the A/A range, the arm
medians against frozen Reno, and r9's value from #739. Memory cells add the
per-block excess in MiB (arm minus the same block's frozen Reno, as #747's
memory_excess.py computed it); control p95 cells add the difference of arm
medians in ms. Descriptive only: it recomputes no flag.
"""
import json
import pathlib
import statistics
import sys

HERE = pathlib.Path(__file__).resolve().parent
DEFAULT = HERE.parent / "2026-10-16-bbr-port-readiness"
ARMS = {"cand-bbrv3": ("cand", "bbrv3", ""), "cand-reno": ("cand", "reno", ""), "reno-reno-aa": ("reno", "reno", "aa")}
FROZEN = ("reno", "reno", "")
MEASURES = {"send_rss": ("send", "peak_rss_mib"), "receive_rss": ("receive", "peak_rss_mib"), "control_p95": (None, "control_p95_ms")}


def value(obs, measure):
    side, key = MEASURES[measure]
    return obs[key] if side is None else obs[side][key]


def main(argv):
    src = pathlib.Path(argv[1]) if len(argv) > 1 else DEFAULT
    summary = json.loads((src / "summary.json").read_text())
    attribution = json.loads((src / "attribution.json").read_text())
    r9 = json.loads((src / "r9-context.json").read_text())
    blocks = {}
    for obs in summary["observations"]:
        if obs["phase"] != "main":
            continue
        arm = (obs["variant"], obs["controller"], obs["tag"])
        blocks.setdefault((obs["path"], obs["workload"]), {}).setdefault(obs["pair"], {})[arm] = obs
    r9_by_cell = {(e["path"], e["workload"], e["arm"]): e["measures"] for e in r9["readiness"]}
    cells = []
    for entry in summary["readiness"]:
        if entry["arm"] not in ("cand-bbrv3", "cand-reno"):
            continue
        for measure, flag in entry["flags"].items():
            if not isinstance(flag, dict) or not flag.get("raised"):
                continue
            arm = ARMS[entry["arm"]]
            pairs = sorted(blocks[(entry["path"], entry["workload"])].items())
            per_block = [(p, value(b[arm], measure), value(b[FROZEN], measure)) for p, b in pairs if arm in b and FROZEN in b]
            ratios = entry["ratios"][measure]
            arm_median = statistics.median(v for _, v, _ in per_block)
            frozen_median = statistics.median(f for _, _, f in per_block)
            cell = {
                "path": entry["path"], "workload": entry["workload"], "arm": entry["arm"], "measure": measure,
                "ratio": flag["median"], "blocks_crossing": flag["blocks_crossing"], "blocks": ratios["n"],
                "per_block_ratio": [ratios["minimum"], ratios["maximum"]],
                "aa": flag["aa"], "frozen_median": frozen_median, "arm_median": arm_median,
                "r9": r9_by_cell[(entry["path"], entry["workload"], entry["arm"])][measure]["r9"],
            }
            excess = [v - f for _, v, f in per_block]
            if measure.endswith("rss"):
                cell["unit"] = "MiB"
                cell["excess_median_mib"] = statistics.median(excess)
                cell["excess_max_mib"] = max(excess)
            else:
                cell["unit"] = "ms"
                cell["difference_of_arm_medians_ms"] = arm_median - frozen_median
                cell["per_block_difference_ms"] = [min(excess), max(excess)]
            cells.append(cell)
    out = {
        "source": str(src.relative_to(HERE.parent)) if src.is_relative_to(HERE.parent) else str(src),
        "instrument": "readiness: peak_rss_mib (ru_maxrss) and control_p95_ms at 20 (loopback) or 30 (WAN) replies per run",
        "cells": cells,
        "preslat": attribution["preslat"],
        "candidate_over_reno_on_candidate": r9["cand_over_reno_on_cand"],
    }
    (HERE / "raised-cells.json").write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
    for c in cells:
        extra = (f"excess median {c['excess_median_mib']:6.2f} max {c['excess_max_mib']:6.2f} MiB" if c["unit"] == "MiB"
                 else f"diff {c['difference_of_arm_medians_ms']:+.3f} ms")
        print(f"{c['path']:8} {c['workload']:8} {c['arm']:10} {c['measure']:11} {c['ratio']:.3f} ({c['blocks_crossing']}) "
              f"blocks {c['per_block_ratio'][0]:.2f}-{c['per_block_ratio'][1]:.2f} A/A {c['aa']['minimum']:.2f}-{c['aa']['maximum']:.2f} "
              f"frozen {c['frozen_median']:.3f} -> {c['arm_median']:.3f} {c['unit']}  {extra}  r9 {c['r9']['median']:.3f}{'*' if c['r9']['raised'] else ''}")


if __name__ == "__main__":
    main(sys.argv)
