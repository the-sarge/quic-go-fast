#!/usr/bin/env python3
"""Apply the #743 registration's rules to counted.jsonl.

usage: analyze.py counted.jsonl [rerun.jsonl] > summary.json
A round with any contaminated run is replaced by its rerun (same round
number) only when every rerun run is clean; otherwise it is excluded.
A verification aid for #743's measurement gate; not a maintained deliverable.
"""
import json, statistics, sys

GIB = 2**30
ARMS = ["D-main", "D-proto", "D-kill", "S-main", "S-proto", "M-main", "M-proto"]
RATIOS = {
    "D-proto/D-main": ("D-proto", "D-main"),
    "D-kill/D-main": ("D-kill", "D-main"),
    "D-proto/D-kill": ("D-proto", "D-kill"),
    "S-proto/S-main": ("S-proto", "S-main"),
    "M-proto/M-main": ("M-proto", "M-main"),
}
RATIO_KEYS = ("mbps", "datagram_mbps", "stream_mbps", "send_cpu_s_per_gib", "recv_cpu_s_per_gib")
RESIDUAL_LIMIT = 2  # DATAGRAMs per run the drop accounting may leave unexplained


def load(path):
    rounds = {}
    for line in open(path):
        r = json.loads(line)
        rounds.setdefault(r["round"], {})[r["arm"]] = r
    return rounds


def metrics(r):
    recv, send = r["recv"], r["send"]
    gib = recv["bytes"] / GIB
    calls = send["sendmsg_calls"]
    m = {
        "mbps": recv["mbps"],
        "send_cpu_s_per_gib": (send["cpu_user_s"] + send["cpu_sys_s"]) / gib,
        "recv_cpu_s_per_gib": (recv["cpu_user_s"] + recv["cpu_sys_s"]) / gib,
        "segments_per_send": send["sendmsg_segments"] / calls,
        "multi_segment_share": send["sendmsg_multi"] / calls,
        "max_segments": send["sendmsg_max_segments"],
        "packets_lost": send["packets_lost"],
        "unprocessed_drops": recv["unprocessed_drops"],
        "socket_drops": recv["socket_drops"],
        "min_rtt_us": send["min_rtt_us"],
        "srtt_us": send["srtt_us"],
        "foreign_s": r["foreign_s"],
    }
    if "stream_mbps" in recv:
        m["stream_mbps"] = recv["stream_mbps"]
    if "datagram_mbps" in recv:
        sent, got = send["sent_datagrams"], recv["datagrams"]
        m["datagram_mbps"] = recv["datagram_mbps"]
        m["datagram_loss"] = 1 - got / sent
        m["datagram_queue_drop_share"] = recv["datagram_queue_drops"] / sent
        m["unprocessed_drop_share"] = recv["unprocessed_drops"] / sent
        if r["arm"].startswith("D-"):
            # DATAGRAM-only packets carry one DATAGRAM each, so every
            # unexplained DATAGRAM is a packer discard (or still queued at close).
            m["residual_datagrams"] = sent - got - recv["datagram_queue_drops"] - recv["unprocessed_drops"] - max(0, recv["socket_drops"])
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
    first = per_round[next(iter(per_round))]
    arms = {a: {k: spread([per_round[n][a][k] for n in per_round]) for k in first[a]} for a in ARMS}
    ratios = {}
    for name, (a, b) in RATIOS.items():
        ratios[name] = {k: spread([per_round[n][a][k] / per_round[n][b][k] for n in per_round])
                        for k in RATIO_KEYS if k in first[a] and k in first[b]}
    med = lambda name, k: ratios[name][k]["median"]
    arm = lambda a, k: arms[a][k]["median"]
    engaged = (arm("D-proto", "segments_per_send") >= 2 and arm("D-proto", "multi_segment_share") >= 0.5
               and arm("D-main", "multi_segment_share") <= 0.01 and arm("D-kill", "multi_segment_share") <= 0.01)
    kill = all(0.985 <= med("D-kill/D-main", k) <= 1.015 for k in ("mbps", "send_cpu_s_per_gib"))
    benefit = med("D-proto/D-main", "mbps") >= 1.10 or med("D-proto/D-main", "send_cpu_s_per_gib") <= 0.90
    stream_guard = med("S-proto/S-main", "mbps") >= 0.97 and med("S-proto/S-main", "send_cpu_s_per_gib") <= 1.03
    mixed_guard = med("M-proto/M-main", "stream_mbps") >= 0.97 and med("M-proto/M-main", "datagram_mbps") >= 0.97
    residual = max(abs(per_round[n][a]["residual_datagrams"]) for n in per_round for a in ("D-main", "D-proto", "D-kill"))
    out = {
        "rounds_used": sorted(per_round), "notes": notes,
        "decisions": {
            "1_engaged": engaged,
            "2_kill_switch_equivalent": kill,
            "3_material_benefit": benefit,
            "4_stream_guard_passes": stream_guard,
            "4_consumer_stream_line_passes": med("S-proto/S-main", "mbps") >= 0.99,
            "5_mixed_guard_passes": mixed_guard,
            "6_packer_residual_max": residual,
            "6_correct": residual <= RESIDUAL_LIMIT,
        },
        "ratios": ratios, "arms": arms,
        "per_round": {str(n): v for n, v in per_round.items()},
    }
    json.dump(out, sys.stdout, indent=1, sort_keys=True)
    print()


if __name__ == "__main__":
    main()
