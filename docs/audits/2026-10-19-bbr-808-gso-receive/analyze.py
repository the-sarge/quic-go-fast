#!/usr/bin/env python3
"""Apply #808's side-stage registration to counted.jsonl (README, "Registration").

usage: analyze.py counted.jsonl [rerun.jsonl] [perfdiag.jsonl] > summary.json
       analyze.py --contaminated counted.jsonl   (rounds to rerun, one per line)

Adapted from #824's analyze.py (2f550727): the same round rule (a round with
any contaminated run is replaced by its rerun only when every rerun run is
clean; otherwise it is excluded) and the same per-run metrics, over this
record's arms. The readings are the registration's:
  K3   Reno STREAM, the revision's sender over the pre-#743 sender (S-rev/S-pre)
       per round: "not reproduced" when the median throughput ratio is at
       least 0.99, "persists" below 0.99; reported beside #743's 0.979, its
       registered 0.97 guard and the consumer's 0.99 line.
  RD   DATAGRAM and mixed arms: DATAGRAM loss beside DatagramReceiveDropsV1,
       #824's measurement counter at the same refusal site, unprocessed-queue
       drops and the residual (descriptive).
  B743 BBRv3 DATAGRAM with #743 on (D-bbr) and off (Dk-bbr): segments per
       send and the throughput ratio (descriptive).
A run whose endpoints report a controller other than its arm's makes the
stage incorrect (`controller_mismatch`), not a contaminated run.
A verification aid for #808's side stage; not a maintained deliverable.
"""
import json
import statistics
import sys

GIB = 2**30
ARMS = ["S-pre", "S-rev", "D-reno", "M-reno", "D-bbr", "Dk-bbr", "S-bbr", "M-bbr"]
CONTROLLER = {"S-pre": "reno", "S-rev": "reno", "D-reno": "reno", "M-reno": "reno",
              "D-bbr": "bbrv3", "Dk-bbr": "bbrv3", "S-bbr": "bbrv3", "M-bbr": "bbrv3"}
DATAGRAM_ARMS = ["D-reno", "M-reno", "D-bbr", "Dk-bbr", "M-bbr"]
RATIOS = {"S-rev/S-pre": ("S-rev", "S-pre"), "Dk-bbr/D-bbr": ("Dk-bbr", "D-bbr"), "D-bbr/D-reno": ("D-bbr", "D-reno"),
          "S-bbr/S-rev": ("S-bbr", "S-rev"), "M-bbr/M-reno": ("M-bbr", "M-reno")}
RATIO_KEYS = ("mbps", "send_cpu_s_per_gib", "recv_cpu_s_per_gib", "recv_maxrss_kb")
K3_LINE = 0.99      # the downstream consumer's line; #743's K3 re-check reading
K3_GUARD = 0.97     # #743's registered STREAM guard, reported beside it
K3_PRIOR = 0.9793   # #743's S-proto/S-main median


def load(path):
    rounds = {}
    for line in open(path):
        r = json.loads(line)
        rounds.setdefault(r["round"], {})[r["arm"]] = r
    return rounds


def metrics(arm, r):
    recv, send = r["recv"], r["send"]
    gib = recv["bytes"] / GIB
    m = {
        "mbps": recv["mbps"],
        "send_cpu_s_per_gib": (send["cpu_user_s"] + send["cpu_sys_s"]) / gib,
        "recv_cpu_s_per_gib": (recv["cpu_user_s"] + recv["cpu_sys_s"]) / gib,
        "recv_maxrss_kb": r["recv_maxrss_kb"],
        "segments_per_send": send["sendmsg_segments"] / send["sendmsg_calls"],
        "min_rtt_us": send["min_rtt_us"],
        "srtt_us": send["srtt_us"],
        "foreign_s": r["foreign_s"],
        "controller_ok": send.get("controller") == recv.get("controller") == CONTROLLER[arm],
    }
    if "stream_mbps" in recv:
        m["stream_mbps"] = recv["stream_mbps"]
    if "datagram_mbps" in recv:
        m["datagram_mbps"] = recv["datagram_mbps"]
    if arm in DATAGRAM_ARMS:
        sent, got = send["sent_datagrams"], recv["datagrams"]
        queue, unproc, sock = recv["datagram_queue_drops"], recv["unprocessed_drops"], max(0, recv["socket_drops"])
        v1 = recv["drops_v1"]
        m.update({
            "sent_datagrams": sent,
            "datagram_loss": 1 - got / sent,
            "drops_v1": v1,
            "v1_share": v1 / sent,
            "queue_drop_share": queue / sent,
            "v1_minus_measurement_counter": v1 - queue,
            "unprocessed_drops": unproc,
            "unprocessed_drop_share": unproc / sent,
            "socket_drops": recv["socket_drops"],
            # Packer discards plus anything still queued at close (#743); #824
            # found it can be -1 when the unprocessed queue drops a packet
            # that carries no DATAGRAM.
            "residual_datagrams": sent - got - queue - unproc - sock,
        })
    return m


def spread(values):
    return {"median": statistics.median(values), "min": min(values), "max": max(values), "n": len(values)}


def choose(rounds, reruns):
    used, notes = {}, []
    for n, runs in sorted(rounds.items()):
        if not any(r["contaminated"] for r in runs.values()):
            used[n] = runs
        elif n in reruns and not any(r["contaminated"] for r in reruns[n].values()):
            used[n] = reruns[n]
            notes.append(f"round {n} replaced by its clean rerun")
        else:
            notes.append(f"round {n} excluded: contaminated and no clean rerun")
    return used, notes


def perfdiag(path):
    pairs = {}
    for line in open(path):
        r = json.loads(line)
        gib = r["recv"]["bytes"] / GIB
        p = r["perf"]
        pairs.setdefault(r["pair"], {})[r["arm"]] = dict(
            mbps=r["recv"]["mbps"], contaminated=r["contaminated"],
            send_cpu_s_per_gib=r["send_process_cpu_s"] / gib,
            ginstr_u_per_gib=p.get("instructions:u", 0) / gib / 1e9, gcycles_u_per_gib=p.get("cycles:u", 0) / gib / 1e9,
            ginstr_per_gib=(p.get("instructions:u", 0) + p.get("instructions:k", 0)) / gib / 1e9,
            gcycles_per_gib=(p.get("cycles:u", 0) + p.get("cycles:k", 0)) / gib / 1e9,
            task_clock_ms=p.get("task-clock"))
    out = {}
    for i, v in sorted(pairs.items()):
        if "S-pre" in v and "S-rev" in v:
            a, b = v["S-rev"], v["S-pre"]
            out[str(i)] = dict(arms=v, clean=not (a["contaminated"] or b["contaminated"]),
                               rev_over_pre={k: (a[k] / b[k] if b[k] else None) for k in
                                             ("mbps", "send_cpu_s_per_gib", "ginstr_u_per_gib", "gcycles_u_per_gib")})
    return out


def main(argv):
    if argv[1] == "--contaminated":
        for n, runs in sorted(load(argv[2]).items()):
            if any(r["contaminated"] for r in runs.values()):
                print(n)
        return
    rounds = load(argv[1])
    reruns = load(argv[2]) if len(argv) > 2 and argv[2] != "-" else {}
    used, notes = choose(rounds, reruns)
    per_round = {n: {a: metrics(a, runs[a]) for a in ARMS if a in runs} for n, runs in used.items()}
    present = [a for a in ARMS if all(a in per_round[n] for n in per_round)]
    arms = {a: {k: spread([per_round[n][a][k] for n in per_round]) for k in per_round[next(iter(per_round))][a]
                if k != "controller_ok"} for a in present}
    ratios = {name: {k: spread([per_round[n][a][k] / per_round[n][b][k] for n in per_round]) for k in RATIO_KEYS}
              for name, (a, b) in RATIOS.items() if a in present and b in present}
    k3 = ratios["S-rev/S-pre"]["mbps"]["median"]
    out = {
        "rounds_used": sorted(per_round), "notes": notes,
        "controller_mismatch": sorted(f"{n} {a}" for n in per_round for a in per_round[n] if not per_round[n][a]["controller_ok"]),
        "readings": {
            "K3_rev_over_pre_mbps": k3,
            "K3_reading": "not reproduced" if k3 >= K3_LINE else "persists",
            "K3_guard_0_97_passes": k3 >= K3_GUARD,
            "K3_prior_743": K3_PRIOR,
            "K3_rev_over_pre_send_cpu_s_per_gib": ratios["S-rev/S-pre"]["send_cpu_s_per_gib"]["median"],
            "B743_segments_per_send": {a: arms[a]["segments_per_send"]["median"] for a in ("D-bbr", "Dk-bbr") if a in arms},
            "B743_kill_over_on_mbps": ratios.get("Dk-bbr/D-bbr", {}).get("mbps", {}).get("median"),
            "RD": {a: {k: arms[a][k]["median"] for k in ("datagram_loss", "v1_share", "queue_drop_share", "unprocessed_drop_share")}
                   | {"v1_minus_measurement_counter_max": max(abs(per_round[n][a]["v1_minus_measurement_counter"]) for n in per_round),
                      "residual_max": max(abs(per_round[n][a]["residual_datagrams"]) for n in per_round)}
                   for a in DATAGRAM_ARMS if a in arms},
        },
        "ratios": ratios, "arms": arms,
        "per_round": {str(n): v for n, v in per_round.items()},
    }
    if len(argv) > 3:
        out["perfdiag"] = perfdiag(argv[3])
    json.dump(out, sys.stdout, indent=1, sort_keys=True)
    print()


if __name__ == "__main__":
    main(sys.argv)
