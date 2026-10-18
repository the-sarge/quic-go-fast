#!/usr/bin/env python3
"""Apply the #742 registration's decision rules to counted.jsonl.

usage: analyze.py counted.jsonl [rerun.jsonl] > summary.json
A round with any contaminated run is replaced by its rerun (same round
number) only when every rerun run is clean; otherwise it is excluded.
"""
import json, statistics, sys

GIB = 2**30
ARMS = ["D-pre", "D-post", "D-off", "S-pre", "S-post"]


def load(path):
    rounds = {}
    for line in open(path):
        r = json.loads(line)
        rounds.setdefault(r["round"], {})[r["arm"]] = r
    return rounds


def metrics(r):
    recv, send = r["recv"], r["send"]
    gib = recv["bytes"] / GIB
    m = {
        "mbps": recv["mbps"],
        "send_cpu_s_per_gib": (send["cpu_user_s"] + send["cpu_sys_s"]) / gib,
        "recv_cpu_s_per_gib": (recv["cpu_user_s"] + recv["cpu_sys_s"]) / gib,
        "foreign_s": r["foreign_s"],
    }
    if r["arm"].startswith("D-"):
        m["loss"] = 1 - recv["datagrams"] / send["sent_datagrams"]
    return m


def spread(values):
    return {"median": statistics.median(values), "min": min(values), "max": max(values), "n": len(values)}


def main():
    rounds = load(sys.argv[1])
    reruns = load(sys.argv[2]) if len(sys.argv) > 2 else {}
    used, notes = {}, []
    for n, runs in sorted(rounds.items()):
        if not any(r["contaminated"] for r in runs.values()):
            used[n] = runs
        elif n in reruns and not any(r["contaminated"] for r in reruns[n].values()):
            used[n] = reruns[n]
            notes.append(f"round {n} replaced by its clean rerun")
        else:
            notes.append(f"round {n} excluded: contaminated and no clean rerun")
    per_round = {n: {a: metrics(runs[a]) for a in ARMS} for n, runs in used.items()}
    arms = {a: {k: spread([per_round[n][a][k] for n in per_round]) for k in per_round[next(iter(per_round))][a]} for a in ARMS}
    pairs = {"D-pre/D-off": ("D-pre", "D-off"), "D-post/D-off": ("D-post", "D-off"),
             "D-post/D-pre": ("D-post", "D-pre"), "S-post/S-pre": ("S-post", "S-pre")}
    ratios = {}
    for name, (a, b) in pairs.items():
        ratios[name] = {k: spread([per_round[n][a][k] / per_round[n][b][k] for n in per_round])
                        for k in ("mbps", "send_cpu_s_per_gib", "recv_cpu_s_per_gib")}
    med = lambda name, k: ratios[name][k]["median"]
    gap = med("D-pre/D-off", "mbps") <= 0.985 or med("D-pre/D-off", "send_cpu_s_per_gib") >= 1.015
    closed = med("D-post/D-off", "mbps") >= 0.985 and med("D-post/D-off", "send_cpu_s_per_gib") <= 1.015
    favorable = med("D-post/D-off", "mbps") > 1.015 or med("D-post/D-off", "send_cpu_s_per_gib") < 0.985
    guard = not (med("S-post/S-pre", "mbps") < 0.97 or med("S-post/S-pre", "send_cpu_s_per_gib") > 1.03)
    out = {
        "rounds_used": sorted(per_round), "notes": notes,
        "decisions": {"gap_reproduces": gap, "gap_closes": closed, "favorable_deviation": favorable, "stream_guard_passes": guard},
        "ratios": ratios, "arms": arms,
        "per_round": {str(n): v for n, v in per_round.items()},
    }
    json.dump(out, sys.stdout, indent=1, sort_keys=True)
    print()


if __name__ == "__main__":
    main()
