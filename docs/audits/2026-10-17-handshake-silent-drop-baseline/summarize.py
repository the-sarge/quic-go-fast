#!/usr/bin/env python3
"""Derive summary.json from the archived #758 captures without running tests.

Usage: python3 -I summarize.py [--check]

--check regenerates the summary in memory and fails unless it is byte-identical
to the committed summary.json.
"""

import io
import json
import os
import sys
import tarfile

HERE = os.path.dirname(os.path.abspath(__file__))
RAW = os.path.join(HERE, "raw")
FILTER = 1250
SCENARIOS = [
    "client-oversized-dropped",
    "server-oversized-dropped",
    "both-oversized-dropped",
    "loss-after-client-receipt",
    "healthy",
    "lossless-high-delay",
    "one-lost-round",
    "two-lost-rounds",
    "total-loss",
    "real-udp-client-oversized-dropped",
]
PROBE_MODES = {"pto (Initial)", "pto (Handshake)", "pto (Application Data)"}
LONG_TYPES = {"initial", "handshake", "0RTT"}


def ms(ns):
    return round(ns / 1e6, 3)


def load_run(name):
    files = {}
    with tarfile.open(os.path.join(RAW, name + ".tar.gz")) as tf:
        for m in tf.getmembers():
            if m.isfile():
                files[os.path.basename(m.name)] = tf.extractfile(m).read().decode()
    return files


def jsonl(text):
    return [json.loads(line) for line in io.StringIO(text) if line.strip()]


def classify(events, before, after):
    """Python statement of the boundary rule; must agree with the Go fixture."""
    for e in events:
        d = e.get("data") or {}
        if e["name"] == "recovery:loss_timer_updated" and d.get("event_type") == "expired":
            if d.get("timer_type") == "pto":
                return "pto", d.get("packet_number_space")
            if d.get("timer_type") == "ack":
                return "ack_loss", d.get("packet_number_space")
    if before in PROBE_MODES or before == "none" or after == "none":
        return "indeterminate", None
    if after == "pto (Initial)":
        return "pto_silent", "initial"
    if after == "pto (Handshake)":
        return "pto_silent", "handshake"
    if after == "pto (Application Data)":
        return "pto_silent", "application_data"
    return "no_op", None


def summarize_mapping(text):
    rows = []
    violations = []
    for r in jsonl(text):
        delta = r["pto_count_after"] - r["pto_count_before"]
        row = {
            "case": r["case"].split("/", 1)[1],
            "kind": r["kind"],
            "action": r["action"],
            "perspective": r["perspective"],
            "pto_count": [r["pto_count_before"], r["pto_count_after"]],
            "probes": [r["probes_before"], r["probes_after"]],
            "send_mode": [r["send_mode_before"], r["send_mode_after"]],
            "events": [
                e["name"] + ("" if not isinstance(e.get("data"), dict) else _event_tag(e["data"]))
                for e in r["events"]
            ],
        }
        if r["kind"] == "timeout_call":
            py_class, _ = classify(r["events"], r["send_mode_before"], r["send_mode_after"])
            row["boundary_class"] = r["boundary_class"]
            row["python_class_agrees"] = py_class == r["boundary_class"]
            if py_class != r["boundary_class"]:
                violations.append(f"{row['case']}: Go {r['boundary_class']} vs Python {py_class}")
            counted = r["boundary_class"] in ("pto", "pto_silent")
            if counted and delta != 1:
                violations.append(f"{row['case']}: counted without a recovery increment")
            if delta == 1 and r["boundary_class"] in ("no_op", "ack_loss"):
                violations.append(f"{row['case']}: recovery increment classified {r['boundary_class']}")
            if delta not in (0, 1):
                violations.append(f"{row['case']}: recovery count changed by {delta}")
        rows.append(row)
    return {"records": rows, "rule_violations": violations}


def _event_tag(d):
    if "event_type" in d:
        return f"[{d['event_type']} {d.get('timer_type', '')} {d.get('packet_number_space', '')}]".replace("  ", " ")
    if "pto_count" in d:
        return f"[pto_count={d['pto_count']}]"
    return ""


def summarize_scenario(rows):
    out = {}
    fixture = next(r for r in rows if r["kind"] == "fixture")
    out["fixture"] = {k: v for k, v in fixture.items() if k not in ("seq", "t_ns", "kind")}
    qlog = {"client": [], "server": []}
    for r in rows:
        if r["kind"] == "qlog":
            qlog[r["endpoint"]].append(r)
    checks = {}

    # Datagrams.
    dgrams = [r for r in rows if r["kind"] == "datagram"]
    by_dir = {}
    for d in dgrams:
        s = by_dir.setdefault(d["dir"], {"attempted": 0, "bytes": 0, "sizes": {}, "delivered": 0, "dropped": {}})
        s["attempted"] += 1
        s["bytes"] += d["size"]
        s["sizes"][str(d["size"])] = s["sizes"].get(str(d["size"]), 0) + 1
        if d["decision"] == "deliver":
            s["delivered"] += 1
        else:
            s["dropped"][d["cause"]] = s["dropped"].get(d["cause"], 0) + 1
    for s in by_dir.values():
        s["max_size"] = max(int(k) for k in s["sizes"])
        s["sizes"] = dict(sorted(s["sizes"].items(), key=lambda kv: int(kv[0])))
    out["datagrams"] = dict(sorted(by_dir.items()))
    link_mtu = out["fixture"]["link_mtu"]
    checks["every attempted datagram fits the link MTU"] = all(d["size"] <= link_mtu for d in dgrams)
    checks["size filters drop only datagrams over 1250 bytes"] = all(
        d["size"] > FILTER for d in dgrams if d["cause"] == "size_filter"
    )
    checks["no unknown datagram direction"] = all(d["dir"] in ("c2s", "s2c") for d in dgrams)
    # The client's first flight: its datagrams before any peer datagram or loss-timer call.
    stop = min(
        [r["seq"] for r in rows if (r["kind"] == "datagram" and r["dir"] == "s2c")
         or (r["kind"] == "timeout_call" and r["endpoint"] == "client")] or [float("inf")]
    )
    out["client_first_flight_datagrams"] = [d["size"] for d in dgrams if d["dir"] == "c2s" and d["seq"] < stop]

    # Connections, receipt, completion and confirmation.
    created = {r["endpoint"]: r["count"] for r in rows if r["kind"] == "conn_created"}
    out["connections_created"] = created
    checks["at most one connection per endpoint"] = all(v == 1 for v in created.values())
    ep = {}
    for name in ("client", "server"):
        e = {"connection": name in created}
        rec = next((r for r in rows if r["kind"] == "receipt" and r["endpoint"] == name), None)
        e["receipt_ms"] = ms(rec["t_ns"]) if rec else None
        for r in rows:
            if r["kind"] == "conn_state" and r["endpoint"] == name:
                key = "completion" if r["what"] == "handshake complete" else "handshake_keys_discarded"
                e[key] = {
                    "t_ms": ms(r["t_ns"]),
                    "handshake_confirmed": r["handshake_confirmed"],
                    "effective_initial_packet_size": r["effective_initial_packet_size"],
                    "max_packet_size": r["max_packet_size"],
                    "df": r["df"],
                }
        buffered = [q for q in qlog[name] if q["name"] == "transport:packet_buffered"]
        e["packets_buffered_undecryptable"] = len(buffered)
        e["packets_buffered_before_receipt"] = sum(
            1 for q in buffered if rec is None or q["seq"] < rec["seq"]
        )

        calls = [r for r in rows if r["kind"] == "timeout_call" and r["endpoint"] == name]
        timeline = []
        disagreements = 0
        for c in calls:
            evs = [q for q in qlog[name] if c["event_index_start"] <= q["index"] < c["event_index_end"]]
            py_class, py_level = classify(evs, c["send_mode_before"], c["send_mode_after"])
            if py_class != c["class"]:
                disagreements += 1
            item = {
                "t_ms": ms(c["t_ns"]),
                "class": c["class"],
                "level": c.get("level"),
                "eligible": c["eligible"],
                "eligible_count": c["eligible_count"],
                "comparison_count": c["comparison_count"],
                "received_first_packet": c["received_first_packet"],
                "handshake_complete": c["handshake_complete"],
                "handshake_confirmed": c["handshake_confirmed"],
                "effective_initial_packet_size": c["effective_initial_packet_size"],
            }
            if c["epoch_reset"]:
                item["epoch_reset"] = True
            timeline.append(item)
        e["loss_timer_calls"] = timeline
        eligible = [c for c in timeline if c["eligible"]]
        e["eligible_expirations"] = len(eligible)
        e["eligible_by_branch"] = {
            "ordinary": sum(1 for c in eligible if c["class"] == "pto"),
            "silent_one_probe": sum(1 for c in eligible if c["class"] == "pto_silent"),
        }
        e["eligible_before_receipt"] = sum(1 for c in eligible if not c["received_first_packet"])
        e["eligible_after_completion"] = sum(1 for c in eligible if c["handshake_complete"])
        e["final_comparison_count"] = min(len(eligible), 2)
        e["qlog_pto_expired_events_before_confirmation"] = sum(
            1
            for q in qlog[name]
            if q["name"] == "recovery:loss_timer_updated"
            and (q.get("data") or {}).get("event_type") == "expired"
            and (q.get("data") or {}).get("timer_type") == "pto"
            and (q.get("data") or {}).get("packet_number_space") in ("initial", "handshake")
        )
        e["indeterminate_calls"] = sum(1 for c in timeline if c["class"] == "indeterminate")
        e["python_go_class_disagreements"] = disagreements
        e["effective_size_changes"] = sorted({c["effective_initial_packet_size"] for c in timeline} | (
            {e["completion"]["effective_initial_packet_size"]} if "completion" in e else set()))
        ep[name] = e
    out["endpoints"] = ep
    checks["no indeterminate loss-timer call"] = all(e["indeterminate_calls"] == 0 for e in ep.values())
    checks["Go and Python classifications agree"] = all(e["python_go_class_disagreements"] == 0 for e in ep.values())

    # Server anti-amplification budget, from delivered client bytes and server sends.
    received = sent = 0
    limited = []
    for d in dgrams:
        if d["dir"] == "c2s" and d["decision"] == "deliver":
            received += d["size"]
        elif d["dir"] == "s2c":
            if sent >= 3 * received:
                limited.append(ms(d["t_ns"]))
            sent += d["size"]
    out["server_amplification"] = {
        "server_sends_at_or_over_3x_budget": limited,
        "first_flight_note": "budget counts delivered client datagrams; server bytes count every write",
    }

    # Release ordering.
    release = next((r for r in rows if r["kind"] == "release"), None)
    if release:
        after = [d for d in dgrams if d["dir"] == "c2s" and d["seq"] > release["seq"]]
        before_same_instant = [
            d for d in dgrams if d["dir"] == "c2s" and d["seq"] < release["seq"] and d["t_ns"] == release["t_ns"]
        ]
        probes = [
            q for q in qlog["client"]
            if q["name"] == "transport:packet_sent" and q["seq"] > release["seq"] and q["t_ns"] == release["t_ns"]
        ]
        out["release"] = {
            "t_ms": ms(release["t_ns"]),
            "at_client_eligible_count": release["at_eligible_count"],
            "client_had_processed_peer_packet": release["received_first_packet"],
            "client_datagrams_routed_at_the_release_instant_before_it": len(before_same_instant),
            "client_packets_sent_at_the_release_instant_after_it": [
                f"{q['data']['header']['packet_type']}#{q['data']['header'].get('packet_number')}" for q in probes
            ],
            "first_client_datagram_after_release": {
                "t_ms": ms(after[0]["t_ns"]), "size": after[0]["size"], "decision": after[0]["decision"],
            } if after else None,
        }
        checks["release precedes the probes it releases"] = bool(probes) and bool(after) and after[0]["decision"] == "deliver"
        checks["no loss-window drop after release"] = not any(
            d["cause"].startswith("loss_window") for d in after
        )
        checks["no client datagram at the release instant was routed before it"] = not before_same_instant

    # Phases and outcome.
    phases = [r for r in rows if r["kind"] == "phase"]
    dial = next(r for r in phases if r["what"] == "dial returned")
    out["dial"] = {"t_ms": ms(dial["t_ns"]), "error_class": dial["error_class"], "error": dial["error"]}
    srv = next((r for r in phases if r["what"] == "server connection ended"), None)
    out["server_outcome"] = (
        {"t_ms": ms(srv["t_ns"]), "error_class": srv["error_class"], "error": srv["error"]} if srv
        else ("no server connection" if any(r["what"] == "no server connection" for r in phases) else "handshake succeeded")
    )
    acc = next((r for r in phases if r["what"] == "accepted"), None)
    if acc:
        out["curves"] = {"client": acc["client_curve"], "server": acc["server_curve"]}
    out["transfer_verified"] = any(r["what"] == "bidirectional transfer verified" for r in phases)
    cleanup = next((r for r in rows if r["kind"] == "cleanup"), None)
    out["cleanup"] = cleanup["what"] if cleanup else None
    checks["cleanup recorded"] = cleanup is not None
    checks["outcome is a transport outcome, not the fixture deadline"] = dial["error_class"] in (
        "none", "idle_timeout", "handshake_timeout")
    checks["transfer verified after every successful dial"] = dial["error_class"] != "none" or out["transfer_verified"]
    out["checks"] = checks
    return out


def scenario_witness(name, s, rows):
    """Scenario-specific sequence witnesses required by #758."""
    w = {}
    c, sv = s["endpoints"]["client"], s["endpoints"]["server"]
    dgrams = [r for r in rows if r["kind"] == "datagram"]
    if name in ("client-oversized-dropped", "both-oversized-dropped", "real-udp-client-oversized-dropped"):
        w["every client datagram is over 1250 bytes and dropped"] = all(
            d["size"] > FILTER and d["decision"] == "drop" for d in dgrams if d["dir"] == "c2s")
        w["client never processes a peer packet"] = c["receipt_ms"] is None
    if name == "server-oversized-dropped":
        first_drop = next(d for d in dgrams if d["dir"] == "s2c" and d["decision"] == "drop")
        first_server_pto = next(x for x in sv["loss_timer_calls"] if x["eligible"])
        rec = next(r for r in rows if r["kind"] == "receipt" and r["endpoint"] == "server")
        w["server processes the client's Initial before its first dropped write"] = rec["seq"] < first_drop["seq"]
        w["server processes the client's Initial before its first eligible PTO"] = first_server_pto["received_first_packet"]
    if name == "loss-after-client-receipt":
        rec = next(r for r in rows if r["kind"] == "receipt" and r["endpoint"] == "client")
        drops = [d for d in dgrams if d["cause"] == "loss_window_after_receipt"]
        sent = [r for r in rows if r["kind"] == "qlog" and r["endpoint"] == "client" and r["name"] == "transport:packet_sent"]
        dropped_types = sorted({
            q["data"]["header"]["packet_type"] for q in sent
            for d in drops if q["data"].get("datagram_payload_checksum") == d["checksum"]
        })
        w["client receipt precedes the first loss-window drop"] = bool(drops) and rec["seq"] < drops[0]["seq"]
        w["no eligible client PTO before receipt"] = c["eligible_before_receipt"] == 0
        w["dropped client datagrams carried handshake packets"] = dropped_types
        w["release happens at the client's first eligible PTO"] = s.get("release", {}).get("at_client_eligible_count") == 1
    if name == "lossless-high-delay":
        el = [x for x in c["loss_timer_calls"] if x["eligible"]]
        w["first eligible client PTO precedes client receipt"] = bool(el) and not el[0]["received_first_packet"]
        w["client receipt precedes any second eligible client PTO"] = len(el) < 2 or el[1]["received_first_packet"]
        w["no datagram dropped"] = all(d["decision"] == "deliver" for d in dgrams)
    if name in ("one-lost-round", "two-lost-rounds"):
        k = 1 if name == "one-lost-round" else 2
        rel = next(r for r in rows if r["kind"] == "release")
        w[f"every client datagram before the release is dropped"] = all(
            d["decision"] == "drop" for d in dgrams if d["dir"] == "c2s" and d["seq"] < rel["seq"])
        w[f"release happens at eligible client PTO #{k}"] = rel["at_eligible_count"] == k
        w["server-to-client delivery stays enabled"] = all(d["decision"] == "deliver" for d in dgrams if d["dir"] == "s2c")
    if name == "healthy":
        w["no datagram dropped"] = all(d["decision"] == "deliver" for d in dgrams)
    if name == "total-loss":
        w["every datagram dropped"] = all(d["decision"] == "drop" for d in dgrams)
    return w


def projection(rows):
    """Run-to-run comparison: timer classifications, datagram decisions, outcomes."""
    long_header = {
        r["data"].get("datagram_payload_checksum")
        for r in rows
        if r["kind"] == "qlog" and r["name"] == "transport:packet_sent"
        and r["data"]["header"]["packet_type"] in LONG_TYPES
    }
    return {
        "timeout_calls": [
            (r["t_ns"], r["endpoint"], r["class"], r.get("level"), r["eligible_count"])
            for r in rows if r["kind"] == "timeout_call"
        ],
        # Datagrams carrying an Initial or Handshake packet: when, which way, whether
        # over the filter, and what happened. Exact sizes and 1-RTT-only
        # packetization are reported separately.
        "handshake_datagrams": sorted(
            (r["t_ns"], r["dir"], r["size"] > FILTER, r["decision"], r["cause"])
            for r in rows if r["kind"] == "datagram" and r["checksum"] in long_header
        ),
        "phases": [(r["t_ns"], r["what"], r.get("error_class")) for r in rows if r["kind"] == "phase"],
        "receipts": [(r["t_ns"], r["endpoint"]) for r in rows if r["kind"] == "receipt"],
    }


def build():
    run1, run2 = load_run("run1"), load_run("run2")
    summary = {
        "baseline": open(os.path.join(RAW, "E0-baseline.log")).read().splitlines(),
        "receipts": {"run1": run1["receipt.txt"].splitlines(), "run2": run2["receipt.txt"].splitlines()},
        "mapping": summarize_mapping(run1["mapping.jsonl"]),
        "scenarios": {},
        "repeat_run": {},
    }
    # Mapping alarms carry wall-clock offsets; compare the semantic fields only.
    def mapping_key(text):
        return [
            (r["case"], r["action"].split("@")[0], r["pto_count_before"], r["pto_count_after"],
             r["send_mode_before"], r["send_mode_after"], r.get("boundary_class"), [e["name"] for e in r["events"]])
            for r in jsonl(text)
        ]
    summary["mapping_runs_identical"] = mapping_key(run1["mapping.jsonl"]) == mapping_key(run2["mapping.jsonl"])
    for name in SCENARIOS:
        rows = jsonl(run1[name + ".jsonl"])
        s = summarize_scenario(rows)
        s["witnesses"] = scenario_witness(name, s, rows)
        summary["scenarios"][name] = s
        p1, p2 = projection(rows), projection(jsonl(run2[name + ".jsonl"]))
        if name.startswith("real-udp"):
            # Wall-clock time: compare classifications and decisions, not timestamps.
            strip = lambda p: {
                "timeout_calls": [x[1:] for x in p["timeout_calls"]],
                "handshake_datagrams": sorted(x[1:] for x in p["handshake_datagrams"]),
                "phases": [x[1:] for x in p["phases"]],
                "receipts": [x[1:] for x in p["receipts"]],
            }
            p1, p2 = strip(p1), strip(p2)
        result = {k: p1[k] == p2[k] for k in p1}
        sizes = lambda text: sorted(
            (r["dir"], r["size"]) for r in jsonl(text) if r["kind"] == "datagram" and r["size"] <= FILTER and r["size"] > 1000
        )
        result["sub_filter_large_datagram_sizes"] = {
            "run1": sorted({sz for _, sz in sizes(run1[name + ".jsonl"])}),
            "run2": sorted({sz for _, sz in sizes(run2[name + ".jsonl"])}),
        }
        result["datagram_counts"] = {
            "run1": sum(1 for r in rows if r["kind"] == "datagram"),
            "run2": sum(1 for r in jsonl(run2[name + ".jsonl"]) if r["kind"] == "datagram"),
        }
        summary["repeat_run"][name] = result
    return json.dumps(summary, indent=1, sort_keys=True) + "\n"


def main():
    text = build()
    path = os.path.join(HERE, "summary.json")
    if "--check" in sys.argv[1:]:
        with open(path) as f:
            if f.read() != text:
                sys.exit("summary.json differs from the regenerated summary")
        print("summary.json is up to date")
        return
    with open(path, "w") as f:
        f.write(text)
    print("wrote", path)


if __name__ == "__main__":
    main()
