"""Summarize retained issue-46 evidence; never executes a diagnostic case."""
import collections
import json
from pathlib import Path

root = Path(__file__).resolve().parent
summary = {}
for case in ("M1", "M2", "L1", "L2", "F1", "F2"):
    rows = [json.loads(line) for line in (root / "raw" / f"{case}.jsonl").read_text().splitlines()]
    start = next(r for r in rows if r["kind"] == "dial_start")
    result = next(r for r in rows if r["kind"] == "dial_result")
    decisions = [r["data"] for r in rows if r["kind"] == "decision"]
    selected = [d for d in decisions if d["selected"]]
    events = [dict(r["data"], decoded=json.loads(r["data"]["json"])) for r in rows if r["kind"] == "qlog"]
    assert not any("encode_error" in e for e in events)
    assert sorted(d["order"] for d in decisions) == list(range(1, len(decisions) + 1))
    before_dial = [d for d in selected if d["callback_ns"] < result["ns"]]
    details = []
    for d in before_dial:
        checksum = d["crc_after"]
        matches = [e for e in events if e["decoded"].get("datagram_payload_checksum") == checksum
                   and ("client=true" in e["source"] or e["source"] == "client transport")]
        details.append(dict(d, receiver_events=[{
            "ms": e["event_ns"] / 1e6, "event": e["event"], "data": e["decoded"]
        } for e in matches]))
    server_pto = [e["event_ns"] / 1e6 for e in events if "client=false" in e["source"]
                  and e["event"] == "recovery:loss_timer_updated"
                  and e["decoded"].get("event_type") == "expired"]
    server_timer = [e["decoded"] for e in events if "client=false" in e["source"]
                    and e["event"] == "recovery:loss_timer_updated"
                    and e["decoded"].get("event_type") == "set"]
    summary[case] = {
        "records": len(rows), "datagrams": len(decisions), "selected": len(selected),
        "selected_before_dial_result": len(before_dial),
        "short_or_failed_writes": sum(d["write_n"] != d["length"] or "write_error" in d for d in selected),
        "dial_start_ms": start["ns"] / 1e6,
        "deadline_ms": start["data"]["deadline_ns"] / 1e6,
        "dial_result_ms": result["ns"] / 1e6,
        "dial_elapsed_ms": (result["ns"] - start["ns"]) / 1e6,
        "dial_result": result["data"],
        "events": dict(collections.Counter(e["event"] for e in events)),
        "server_pto_expired_ms": server_pto,
        "last_server_timer_set": server_timer[-1] if server_timer else None,
        "selected_before_dial_details": details,
    }
(root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
for case, s in summary.items():
    print(case, s["dial_result"], f'{s["dial_elapsed_ms"]:.3f} ms Dial',
          f'{s["selected_before_dial_result"]} selected before Dial result',
          f'{s["short_or_failed_writes"]} short/failed writes')
