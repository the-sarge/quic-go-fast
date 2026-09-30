#!/usr/bin/env python3
"""Finite read-only extraction from the frozen local-reproduction tar stream."""

import collections
import hashlib
import json
import re
import sys
import tarfile

EVIDENCE = "2383659ce0781b358fe098f32637d41767b7e9d0"
EXPECTED = {
    f"frozen-{workload}-p{pair}-bbrv3/send.json"
    for workload in ("stream", "datagram")
    for pair in (1, 2, 3)
}
RECEIVERS = {path.replace("/send.json", "/receive.json") for path in EXPECTED}

records = []
receivers = {}
with tarfile.open(fileobj=sys.stdin.buffer, mode="r|gz") as archive:
    for member in archive:
        if member.name not in EXPECTED | RECEIVERS:
            continue
        with archive.extractfile(member) as source:
            raw = source.read()
        endpoint = json.loads(raw)
        trace = endpoint["trace"]
        if member.name in RECEIVERS:
            receivers[member.name] = {
                "member": member.name,
                "bytes": len(raw),
                "sha256": hashlib.sha256(raw).hexdigest(),
                "received_ecn": trace["received_ecn"],
                "lost_packets": trace["lost_packets"],
                "flow_control_blocked_frames": trace["flow_control_blocked_frames"],
            }
            continue
        snapshots = []
        for event in trace["events"]:
            if event["kind"] != "transport:congestion_control":
                continue
            fields = dict(re.findall(r"(\w+)=([^ ]+)", event["event"]["Message"]))
            snapshots.append({"unix_ns": event["unix_ns"], **fields})
        post_startup = [s for s in snapshots if s["phase"] != "Startup"]
        rates = [int(s["pacing"]) for s in post_startup]
        records.append({
            "member": member.name,
            "bytes": len(raw),
            "sha256": hashlib.sha256(raw).hexdigest(),
            "source": endpoint["source"],
            "snapshot_count": len(snapshots),
            "phase_counts": dict(collections.Counter(s["phase"] for s in snapshots)),
            "post_startup_pacing_bytes_per_second": {"minimum": min(rates), "maximum": max(rates)},
            "post_startup_quanta": sorted({int(s["quantum"]) for s in post_startup}),
            "ce_active_snapshot_count": sum(s["ce_active"] == "true" for s in snapshots),
            "lost_packets": trace["lost_packets"],
            "flow_control_blocked_frames": trace["flow_control_blocked_frames"],
            "sent_ecn": trace["sent_ecn"],
            "received_ecn": trace["received_ecn"],
            "snapshots": snapshots,
        })

assert {r["member"] for r in records} == EXPECTED
assert len(records) == len(EXPECTED)
assert set(receivers) == RECEIVERS
for record in records:
    record["paired_receiver"] = receivers[record["member"].replace("/send.json", "/receive.json")]
print(json.dumps({"evidence_commit": EVIDENCE, "records": records}, indent=2) + "\n", end="")
