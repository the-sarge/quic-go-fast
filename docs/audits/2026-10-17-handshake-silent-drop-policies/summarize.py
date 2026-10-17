#!/usr/bin/env python3
"""Derive summary.json and tables.md from the archived #759 captures without running tests.

Usage: python3 -I summarize.py [--check]

The boundary rule, scenario summary and run projection are #758's own functions,
loaded from its frozen summarize.py after a hash check. --check regenerates both
outputs in memory and fails unless they are byte-identical to the committed files
and every generated table appears verbatim in README.md.
"""

import collections
import hashlib
import importlib.util
import io
import json
import os
import sys
import tarfile

# Loading #758's summarizer must not write a bytecode cache into either record.
sys.dont_write_bytecode = True

HERE = os.path.dirname(os.path.abspath(__file__))
RAW = os.path.join(HERE, "raw")
BASE_REC = os.path.join(os.path.dirname(HERE), "2026-10-17-handshake-silent-drop-baseline")
BASE_SUMMARIZE_SHA256 = "2aa91fd0cea35c3469ba9a61437fc0e03f46f468423153cab22c95c1abed6adb"  # #758's SHA256SUMS

VARIANTS = ["baseline", "p1", "p2", "p1-guarded"]
CANDIDATES = ["p1", "p2", "p1-guarded"]
MAIN_ROWS = [
    "client-oversized-dropped",
    "server-oversized-dropped",
    "both-oversized-dropped",
    "loss-after-client-receipt",
    "healthy",
    "lossless-high-delay",
    "one-lost-round",
    "two-lost-rounds",
    "total-loss",
]
REAL_UDP = "real-udp-client-oversized-dropped"
PRESERVATION_ROWS = [
    "explicit-1280-client-oversized-dropped",
    "explicit-1280-server-oversized-dropped",
    "explicit-1280-one-lost-round",
    "explicit-1200-one-lost-round",
    "explicit-1200-total-loss",
    "explicit-1280-client-local-msgsize",
    "unset-client-local-msgsize",
]
BBR = "bbrv3-client-oversized-dropped"
BBR_VARIANTS = ["baseline", "p1"]

# Which endpoint's reduction a size-restricted direction needs. Every other reduction
# in a size-drop row is collateral; every reduction in a non-size row is unnecessary.
NEEDED = {
    "client-oversized-dropped": {"client"},
    "server-oversized-dropped": {"server"},
    "both-oversized-dropped": {"client", "server"},
    REAL_UDP: {"client"},
    BBR: {"client"},
    "unset-client-local-msgsize": {"client"},
}
EXPLICIT = {r for r in PRESERVATION_ROWS if r.startswith("explicit-")}

# Run-to-run projection differences that are explained, pinned exactly: any other
# difference, or a different one in this cell, fails the summary.
EXPLAINED_REPEAT = {
    "p2.server-oversized-dropped:handshake_datagrams": {
        "run1_only": [],
        "run2_only": [[410000000, "s2c", False, "deliver", ""]],
        "reason": "At the server's P2 trigger (410 ms) its Handshake probes are re-packed at 1200 bytes. The "
                  "certificate flight's length varies by a few bytes between runs (as in #758), so run1 sends two "
                  "1197-byte datagrams and run2 sends 1200, 1200 and 45. Timer calls, receipts, phases and "
                  "outcomes are identical.",
    },
}


def load_base():
    path = os.path.join(BASE_REC, "summarize.py")
    with open(path, "rb") as f:
        if hashlib.sha256(f.read()).hexdigest() != BASE_SUMMARIZE_SHA256:
            sys.exit("#758's summarize.py differs from its archived hash")
    spec = importlib.util.spec_from_file_location("base758", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


base = load_base()
ms, jsonl = base.ms, base.jsonl


def load_tar(path):
    files = {}
    with tarfile.open(path) as tf:
        for m in tf.getmembers():
            if m.isfile():
                files[os.path.basename(m.name)] = tf.extractfile(m).read().decode()
    return files


def cells():
    out = [(v, r) for v in VARIANTS for r in MAIN_ROWS + PRESERVATION_ROWS]
    out += [(v, BBR) for v in BBR_VARIANTS]
    out += [(v, REAL_UDP) for v in VARIANTS]
    return out


def phase_of(state):
    if not state["received_first_packet"]:
        return "before receipt"
    if not state["handshake_complete"]:
        return "after receipt, before completion"
    return "after completion, before confirmation"


def endpoint_policy(rows, name, row):
    """Variant decisions, the first reduction and sizes for one endpoint."""
    calls = [r for r in rows if r["kind"] == "timeout_call" and r["endpoint"] == name]
    decisions = []
    for p in (r for r in rows if r["kind"] == "policy" and r["endpoint"] == name):
        call = max((c for c in calls if c["seq"] < p["seq"]), key=lambda c: c["seq"])
        d = {
            "t_ms": ms(p["t_ns"]),
            "eligible_count": p["eligible_count"],
            "class": call["class"],
            "level": call.get("level"),
            "phase": phase_of(p),
            "decision": p["decision"],
        }
        if p["decision"] == "trigger":
            d["reduced"] = p["reduced"]
            d["effective_after"] = p["effective_initial_packet_size_after"]
            d["max_packet_size_after"] = p["max_packet_size_after"]
            d["max_payload_size_estimate_after"] = p["max_payload_size_estimate_after"]
        decisions.append(d)
    first = next((d for d in decisions if d.get("reduced")), None)
    first_rec = next(
        (p for p in rows if p["kind"] == "policy" and p["endpoint"] == name and p.get("reduced")), None)
    out = {"decisions": decisions, "reduction": None}
    if first:
        kind = "necessary" if name in NEEDED.get(row, set()) else (
            "collateral" if row in NEEDED else "unnecessary")
        if row in EXPLICIT:
            kind = "explicit-size violation"
        out["reduction"] = {k: first[k] for k in ("t_ms", "eligible_count", "class", "level", "phase")}
        out["reduction"]["kind"] = kind
        direction = "c2s" if name == "client" else "s2c"
        later = [r for r in rows if r["kind"] == "datagram" and r["dir"] == direction and r["seq"] > first_rec["seq"]]
        out["reduction"]["datagrams_after"] = len(later)
        out["reduction"]["max_datagram_after"] = max((r["size"] for r in later), default=None)
    out["guard_abstentions"] = sum(1 for d in decisions if d["decision"] == "guard_abstains")
    out["below_threshold"] = sum(1 for d in decisions if d["decision"] == "below_threshold")
    out["owner_no_ops"] = sum(1 for d in decisions if d["decision"] == "trigger" and not d["reduced"])
    return out


def summarize_cell(variant, row, rows):
    s = base.summarize_scenario(rows)
    fx = s["fixture"]
    checks = dict(s["checks"])
    checks["fixture records this variant and scenario"] = fx["variant"] == variant and fx["scenario"] == row
    cell = {
        "fixture": {k: fx[k] for k in ("initial_packet_size", "congestion_control", "client_local_msg_size", "network")},
        "dial": s["dial"],
        "server_outcome": s["server_outcome"],
        "transfer_verified": s["transfer_verified"],
        "cleanup": s["cleanup"],
        "datagrams": s["datagrams"],
        "server_amplification_limited_sends": s["server_amplification"]["server_sends_at_or_over_3x_budget"],
        "endpoints": {},
    }
    if "release" in s:
        cell["release"] = {k: s["release"][k] for k in ("t_ms", "at_client_eligible_count")}
    for name in ("client", "server"):
        e = s["endpoints"][name]
        out = {
            "connection": e["connection"],
            "receipt_ms": e["receipt_ms"],
            "eligible": [
                {k: c[k] for k in ("t_ms", "class", "level", "eligible_count", "received_first_packet", "handshake_complete")}
                for c in e["loss_timer_calls"] if c["eligible"]
            ],
            "indeterminate_calls": e["indeterminate_calls"],
        }
        for key in ("completion", "handshake_keys_discarded"):
            if key in e:
                out[key] = {k: e[key][k] for k in ("t_ms", "effective_initial_packet_size", "max_packet_size")}
        out["eligible_before_completion"] = sum(1 for c in out["eligible"] if not c["handshake_complete"])
        out.update(endpoint_policy(rows, name, row))
        r = out["reduction"]
        if r:
            checks[f"{name}: no datagram over 1250 bytes after its reduction"] = (
                r["max_datagram_after"] is None or r["max_datagram_after"] <= base.FILTER)
        cell["endpoints"][name] = out
    if variant == "baseline":
        cell["witnesses_758"] = base.scenario_witness(row, s, rows) if row in MAIN_ROWS + [REAL_UDP] else {}
    cell["checks"] = checks
    return cell


def strip_times(p):
    return {
        "timeout_calls": [x[1:] for x in p["timeout_calls"]],
        "handshake_datagrams": sorted(x[1:] for x in p["handshake_datagrams"]),
        "phases": [x[1:] for x in p["phases"]],
        "receipts": [x[1:] for x in p["receipts"]],
    }


def projections(a, b, wall_clock):
    pa, pb = base.projection(a), base.projection(b)
    if wall_clock:
        pa, pb = strip_times(pa), strip_times(pb)
    return pa, pb


def compare(a, b, wall_clock):
    pa, pb = projections(a, b, wall_clock)
    return {k: pa[k] == pb[k] for k in pa}


def difference(a, b, wall_clock, key):
    """Multiset difference of one projection key, as JSON-ready lists."""
    pa, pb = projections(a, b, wall_clock)
    ca = collections.Counter(tuple(x) for x in pa[key])
    cb = collections.Counter(tuple(x) for x in pb[key])
    return {
        "run1_only": sorted(list(x) for x in (ca - cb).elements()),
        "run2_only": sorted(list(x) for x in (cb - ca).elements()),
    }


def outcome_text(c):
    d = c["dial"]
    if d["error_class"] == "none":
        return f"success, Dial {d['t_ms']:g}"
    label = {"idle_timeout": "idle timeout", "handshake_timeout": "handshake timeout"}.get(d["error_class"], d["error_class"])
    return f"{label} {d['t_ms']:g}"


def reduction_text(c, name):
    e = c["endpoints"][name]
    r = e["reduction"]
    if r:
        return f"{r['t_ms']:g} (#{r['eligible_count']}, {r['phase']}; {r['kind']})"
    if not e["connection"]:
        return "no connection"
    if e["guard_abstentions"]:
        return f"none (guard abstained {e['guard_abstentions']}×)"
    if e["below_threshold"]:
        return f"none ({e['below_threshold']} below threshold)"
    if e["owner_no_ops"]:
        return f"none (owner no-op {e['owner_no_ops']}×)"
    return "none"


def md_table(header, rows):
    lines = ["| " + " | ".join(header) + " |", "| " + " | ".join("---" for _ in header) + " |"]
    lines += ["| " + " | ".join(str(x) for x in r) + " |" for r in rows]
    return "\n".join(lines)


def tables(summary):
    cs = summary["cells"]
    key = lambda v, r: f"{v}.{r}"
    rows = MAIN_ROWS + [REAL_UDP]
    blocks = {}
    blocks["outcomes"] = md_table(
        ["Scenario"] + VARIANTS,
        [[r] + [outcome_text(cs[key(v, r)]) for v in VARIANTS] for r in rows])
    for name in ("client", "server"):
        blocks[f"{name}-reductions"] = md_table(
            ["Scenario"] + CANDIDATES,
            [[r] + [reduction_text(cs[key(v, r)], name) for v in CANDIDATES] for r in rows])
    def sizes(c):
        out = []
        for name in ("client", "server"):
            e = c["endpoints"][name]
            if not e["connection"]:
                continue
            comp = e.get("completion", {}).get("effective_initial_packet_size", "–")
            conf = e.get("handshake_keys_discarded", {}).get("effective_initial_packet_size", "–")
            out.append(f"{name[0].upper()} {comp}/{conf}")
        return ", ".join(out) or "–"
    blocks["sizes"] = md_table(
        ["Scenario"] + VARIANTS,
        [[r] + [sizes(cs[key(v, r)]) for v in VARIANTS] for r in rows])
    pres = []
    for r in PRESERVATION_ROWS:
        line = [r, outcome_text(cs[key("baseline", r)])]
        for v in CANDIDATES:
            eq = all(summary["preservation"][v][r]["equal_to_baseline"].values())
            red = [n for n in ("client", "server") if cs[key(v, r)]["endpoints"][n]["reduction"]]
            line.append(("same as baseline" if eq else "differs") + (f"; reduced: {', '.join(red)}" if red else "; no reduction"))
        pres.append(line)
    blocks["preservation"] = md_table(["Row", "Baseline outcome"] + CANDIDATES, pres)
    blocks["bbr"] = md_table(
        ["Variant", "Outcome", "Client reduction"],
        [[v, outcome_text(cs[key(v, BBR)]), reduction_text(cs[key(v, BBR)], "client")] for v in BBR_VARIANTS])
    return blocks


def render_tables(blocks):
    parts = ["# Generated tables (#759)", "",
             "Generated by `summarize.py` from the archived captures; README.md quotes each block verbatim.", ""]
    for name, text in blocks.items():
        parts += [f"## {name}", "", text, ""]
    return "\n".join(parts)


def mapping_key(text):
    """#758's comparison key: mapping alarms carry wall-clock offsets, so semantic fields only."""
    return [
        (r["case"], r["action"].split("@")[0], r["pto_count_before"], r["pto_count_after"],
         r["send_mode_before"], r["send_mode_after"], r.get("boundary_class"), [e["name"] for e in r["events"]])
        for r in jsonl(text)
    ]


def test_log(text):
    lines = [l.strip() for l in text.splitlines()]
    return {
        "passed": any(l.startswith("ok ") for l in lines) and not any(
            l.startswith("--- FAIL") or l.startswith("FAIL") or l.startswith("panic:") for l in lines),
        "data_race_reports": text.count("WARNING: DATA RACE"),
    }


def build():
    run1 = load_tar(os.path.join(RAW, "run1.tar.gz"))
    run2 = load_tar(os.path.join(RAW, "run2.tar.gz"))
    race = load_tar(os.path.join(RAW, "race.tar.gz"))
    base_run1 = load_tar(os.path.join(BASE_REC, "raw", "run1.tar.gz"))
    summary = {
        "baseline_identity": open(os.path.join(RAW, "E0-identity.log")).read().splitlines(),
        "receipts": {n: t["receipt.txt"].splitlines() for n, t in (("run1", run1), ("run2", run2), ("race", race))},
        "test_logs": {
            "run1": {log: test_log(run1[log]) for log in ("mapping-test.log", "scenario-test.log")},
            "run2": {log: test_log(run2[log]) for log in ("mapping-test.log", "scenario-test.log")},
            "race": {log: test_log(race[log]) for log in ("mapping-race.log", "scenario-race.log")},
        },
        "mapping": {
            "rule_violations": base.summarize_mapping(run1["mapping.jsonl"])["rule_violations"],
            "records": len(jsonl(run1["mapping.jsonl"])),
            "identical_to_758_run1": mapping_key(run1["mapping.jsonl"]) == mapping_key(base_run1["mapping.jsonl"]),
            "runs_identical": mapping_key(run1["mapping.jsonl"]) == mapping_key(run2["mapping.jsonl"]),
        },
        "cells": {},
        "repeat_run": {},
        "repeat_differences": {},
        "baseline_control_vs_758": {},
        "preservation": {v: {} for v in CANDIDATES},
    }
    for v, r in cells():
        name = f"{v}.{r}"
        rows = jsonl(run1[name + ".jsonl"])
        summary["cells"][name] = summarize_cell(v, r, rows)
        rows2 = jsonl(run2[name + ".jsonl"])
        summary["repeat_run"][name] = compare(rows, rows2, r == REAL_UDP)
        for k, ok in summary["repeat_run"][name].items():
            if not ok:
                diff = difference(rows, rows2, r == REAL_UDP, k)
                explained = EXPLAINED_REPEAT.get(f"{name}:{k}")
                diff["explained"] = explained is not None and all(diff[s] == explained[s] for s in ("run1_only", "run2_only"))
                if diff["explained"]:
                    diff["reason"] = explained["reason"]
                summary["repeat_differences"][f"{name}:{k}"] = diff
        if v == "baseline" and r in MAIN_ROWS + [REAL_UDP]:
            summary["baseline_control_vs_758"][r] = compare(rows, jsonl(base_run1[r + ".jsonl"]), r == REAL_UDP)
    for v in CANDIDATES:
        for r in PRESERVATION_ROWS:
            summary["preservation"][v][r] = {
                "equal_to_baseline": compare(jsonl(run1[f"{v}.{r}.jsonl"]), jsonl(run1[f"baseline.{r}.jsonl"]), False),
            }
    failed = []
    for name, c in summary["cells"].items():
        failed += [f"{name}: {k}" for k, ok in c["checks"].items() if not ok]
        failed += [f"{name}: 758 witness {k}" for k, ok in c.get("witnesses_758", {}).items()
                   if ok is False]
        failed += [f"{name}: repeat {k}" for k, ok in summary["repeat_run"][name].items()
                   if not ok and not summary["repeat_differences"][f"{name}:{k}"]["explained"]]
    unused = set(EXPLAINED_REPEAT) - set(summary["repeat_differences"])
    failed += [f"explained repeat difference not observed: {k}" for k in sorted(unused)]
    for r, res in summary["baseline_control_vs_758"].items():
        failed += [f"baseline control {r}: {k} differs from #758 run1" for k, ok in res.items() if not ok]
    for run, logs in summary["test_logs"].items():
        failed += [f"{run} {log} did not pass cleanly" for log, t in logs.items()
                   if not t["passed"] or t["data_race_reports"]]
    m = summary["mapping"]
    if m["rule_violations"] or not m["identical_to_758_run1"] or not m["runs_identical"]:
        failed.append("mapping evidence differs from #758 or between runs")
    summary["failed_checks"] = failed
    blocks = tables(summary)
    return json.dumps(summary, indent=1, sort_keys=True) + "\n", render_tables(blocks), blocks


def main():
    text, tables_md, blocks = build()
    paths = {os.path.join(HERE, "summary.json"): text, os.path.join(HERE, "tables.md"): tables_md}
    if "--check" in sys.argv[1:]:
        for path, want in paths.items():
            with open(path) as f:
                if f.read() != want:
                    sys.exit(f"{os.path.basename(path)} differs from the regenerated output")
        with open(os.path.join(HERE, "README.md")) as f:
            readme = f.read()
        missing = [n for n, b in blocks.items() if b not in readme]
        if missing:
            sys.exit("README.md does not quote these generated tables verbatim: " + ", ".join(missing))
        print("summary.json and tables.md are up to date; README.md quotes every table")
        return
    for path, want in paths.items():
        with open(path, "w") as f:
            f.write(want)
        print("wrote", path)


if __name__ == "__main__":
    main()
