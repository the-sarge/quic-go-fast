#!/usr/bin/env python3
"""Apply the #824 registration's rules to counted.jsonl.

usage: analyze.py counted.jsonl [rerun.jsonl] > summary.json
A round with any contaminated run is replaced by its rerun (same round
number) only when every rerun run is clean; otherwise it is excluded.
A verification aid for #824's measurement gate; not a maintained deliverable.
"""
import json, statistics, sys

GIB = 2**30
ARMS = ["R-main", "R-cand", "R-128"]
RATIOS = {"R-cand/R-main": ("R-cand", "R-main"), "R-128/R-main": ("R-128", "R-main")}
RATIO_KEYS = ("mbps", "recv_cpu_s_per_gib", "send_cpu_s_per_gib", "recv_maxrss_kb", "min_rtt_us", "srtt_us")
LINE = 1e-4  # 0.01%
FIXTURE_SHARE = 0.005  # R-main must discard at least 0.5% at its queue
RESIDUAL_LIMIT = 2  # as #743: DATAGRAMs per run the drop accounting may leave unexplained


def load(path):
    rounds = {}
    for line in open(path):
        r = json.loads(line)
        rounds.setdefault(r["round"], {})[r["arm"]] = r
    return rounds


def metrics(r):
    recv, send = r["recv"], r["send"]
    gib = recv["bytes"] / GIB
    sent, got = send["sent_datagrams"], recv["datagrams"]
    queue, unproc, sock = recv["datagram_queue_drops"], recv["unprocessed_drops"], max(0, recv["socket_drops"])
    m = {
        "mbps": recv["mbps"],
        "send_cpu_s_per_gib": (send["cpu_user_s"] + send["cpu_sys_s"]) / gib,
        "recv_cpu_s_per_gib": (recv["cpu_user_s"] + recv["cpu_sys_s"]) / gib,
        "recv_maxrss_kb": r["recv_maxrss_kb"],
        "recv_vm_hwm_kb": recv["vm_hwm_kb"],
        "segments_per_send": send["sendmsg_segments"] / send["sendmsg_calls"],
        "sent_datagrams": sent,
        "datagram_loss": 1 - got / sent,
        # The measurement-only counter at the queue's refusal site, in every arm.
        "queue_drop_share": queue / sent,
        "unprocessed_drops": unproc,
        "unprocessed_drop_share": unproc / sent,
        "socket_drops": recv["socket_drops"],
        # Packer discards plus anything still queued at close, as #743.
        "residual_datagrams": sent - got - queue - unproc - sock,
        "packets_lost": send["packets_lost"],
        "min_rtt_us": send["min_rtt_us"],
        "srtt_us": send["srtt_us"],
        # Unprocessed-queue drops the sender never declared lost: packets
        # that were not ack-eliciting, so carried no DATAGRAM.
        "unprocessed_not_lost": unproc - send["packets_lost"],
        # Packets the receiver never processed; equal to unprocessed drops
        # when the unprocessed queue is the only packet-level drop point.
        "packets_unreceived": send["packets_sent"] - recv["packets_received"],
        "foreign_s": r["foreign_s"],
    }
    if "drops_v1" in recv:
        v1 = recv["drops_v1"]
        m["drops_v1"] = v1
        m["v1_share"] = v1 / sent
        # Rule 2: the counter against the harness's derived queue loss
        # (sent - received - unprocessed - socket drops); zero when exact.
        m["v1_minus_derived"] = v1 - (sent - got - unproc - sock)
        m["v1_minus_measurement_counter"] = v1 - queue
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
    ratios = {name: {k: spread([per_round[n][a][k] / per_round[n][b][k] for n in per_round]) for k in RATIO_KEYS}
              for name, (a, b) in RATIOS.items()}
    arm = lambda a, k: arms[a][k]["median"]
    fixture = arm("R-main", "queue_drop_share") >= FIXTURE_SHARE and arm("R-main", "segments_per_send") >= 2
    share_passes = arm("R-cand", "v1_share") <= LINE
    loss_passes = arm("R-cand", "datagram_loss") <= LINE
    # Decision D2 (2026-10-18): total loss above the line is acceptable only
    # when the excess is entirely unprocessed-queue and socket drops.
    loss_without_other_queues = statistics.median(
        per_round[n]["R-cand"]["datagram_loss"] - per_round[n]["R-cand"]["unprocessed_drop_share"]
        - max(0, per_round[n]["R-cand"]["socket_drops"]) / per_round[n]["R-cand"]["sent_datagrams"]
        for n in per_round)
    exact = [per_round[n]["R-128"]["v1_minus_derived"] for n in per_round]
    # A difference is explained only when it equals the run's unprocessed
    # drops of packets without a DATAGRAM.
    explained = all(per_round[n]["R-128"]["v1_minus_derived"] in (0, per_round[n]["R-128"]["unprocessed_not_lost"])
                    for n in per_round)
    residual = max(abs(per_round[n][a]["residual_datagrams"]) for n in per_round for a in ARMS)
    out = {
        "rounds_used": sorted(per_round), "notes": notes,
        "decisions": {
            "0_fixture_reproduces": fixture,
            "1_discard_share_passes": share_passes,
            "1_loss_passes": loss_passes,
            "1_loss_without_other_queues": loss_without_other_queues,
            "1_d2_applies": share_passes and not loss_passes and loss_without_other_queues <= LINE,
            "1_passes": share_passes and (loss_passes or loss_without_other_queues <= LINE),
            "2_r128_v1_minus_derived": exact,
            "2_counter_exact": all(d == 0 for d in exact),
            "2_counter_exact_or_explained": explained,
            "3_residual_max": residual,
            "3_correct": residual <= RESIDUAL_LIMIT,
        },
        # Not registered rules: added after the stage to diagnose rule 2.
        "outside_rules": {
            # With #743's packer residual (computed from the measurement-only
            # queue counter) as its last term, the brief's formula reduces to
            # this comparison plus socket drops, which are zero in every run.
            "v1_equals_measurement_counter_all_candidate_runs": all(
                per_round[n][a]["v1_minus_measurement_counter"] == 0 for n in per_round for a in ("R-cand", "R-128")),
            "unprocessed_drops_equal_unreceived_packets_all_runs": all(
                per_round[n][a]["unprocessed_drops"] == per_round[n][a]["packets_unreceived"] for n in per_round for a in ARMS),
            "runs_with_nonzero_residual": sorted(f"{n} {a} {per_round[n][a]['residual_datagrams']}"
                                                 for n in per_round for a in ARMS if per_round[n][a]["residual_datagrams"]),
        },
        "ratios": ratios, "arms": arms,
        "per_round": {str(n): v for n, v in per_round.items()},
    }
    json.dump(out, sys.stdout, indent=1, sort_keys=True)
    print()


if __name__ == "__main__":
    main()
