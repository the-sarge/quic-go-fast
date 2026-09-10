"""Reconstruct the two retained failures; never execute network traffic."""
import hashlib
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parent
expected = {
    "i1": "1ed6e68e39353bd346f65063b5f77820e4fc3f56839725bd41cbf55cf97a19a8",
    "i2": "53b032d4075edbc51367079de91e44de3f00500082b1307725a1afcba770c7fc",
}
summary = {}
for name, sha in expected.items():
    raw = (root / "raw" / (name + ".jsonl")).read_bytes()
    assert hashlib.sha256(raw).hexdigest() == sha
    records = [dict(json.loads(line), line=i) for i, line in enumerate(raw.splitlines(), 1)]
    assert records[0]["source"] == "capture_start"
    assert records[-1]["source"] == "dial_finished"
    assert records[-1]["data"] == "Dial returned: context deadline exceeded dropped_or_truncated=0"
    for r in records:
        data = r["data"]
        if "event=" in data and " data=" in data:
            assert "encode_error=<nil>" in data
            r["event"] = data.split("event=", 1)[1].split()[0]
            r["q"] = json.loads(data.split(" data=", 1)[1])
    attempts = []
    for sent in records:
        q = sent.get("q", {})
        if not (sent.get("event") == "transport:packet_sent" and "client=false" in sent["source"]
                and q["header"]["packet_type"] == "initial"
                and any(f["frame_type"] == "crypto" for f in q.get("frames", []))):
            continue
        crc = str(q["datagram_payload_checksum"])
        mutation = next(r for r in records if r["source"] == "corruption proxy"
                        and "crc32c_before=" + crc + " " in r["data"])
        data = mutation["data"]
        before = next(r for r in records if r["source"] == "before_mutation"
                      and re.search(r"crc32c=" + crc + r"\b", r["data"]))
        payload = bytearray.fromhex(re.search(r"payload=([0-9a-f]+)", before["data"])[1])
        offset = int(re.search(r"offset=(\d+)", data)[1])
        old, new = (int(re.search(r" " + key + r"=(\d+)", data)[1]) for key in ("before", "after"))
        assert old != new and payload[offset] == old
        payload[offset] = new
        after_crc = re.search(r"crc32c_after=(\d+)", data)[1]
        sockets = [r for r in records if r["source"].startswith("socket")
                   and re.search(r"crc32c=" + after_crc + r"\b", r["data"])]
        assert any("client=false" in r["source"] and "operation=write " in r["data"] for r in sockets)
        assert any("client=true" in r["source"] and "operation=read " in r["data"] for r in sockets)
        for r in sockets:
            assert payload == bytes.fromhex(re.search(r"payload=([0-9a-f]+)", r["data"])[1])
            assert "error=<nil>" in r["data"]
            n = re.search(r"submitted_bytes=(\d+) result_bytes=(\d+)", r["data"])
            assert int(n[1]) == int(n[2]) == len(payload)
        drops = [r for r in records if r.get("event") == "transport:packet_dropped"
                 and str(r["q"].get("datagram_payload_checksum")) == after_crc]
        assert len(drops) == 1 and drops[0]["q"]["trigger"] == "payload_decrypt_error"
        attempts.append({"packet_number": q["header"]["packet_number"], "sent_line": sent["line"],
                         "mutation_line": mutation["line"], "socket_lines": [r["line"] for r in sockets],
                         "drop_line": drops[0]["line"], "offset": offset, "old": old, "new": new})
    received = [r["q"] for r in records if r.get("event") == "transport:packet_received" and "client=true" in r["source"]]
    assert received and all(q["header"]["packet_type"] == "initial" and q["frames"]
                            and all(f["frame_type"] == "ack" for f in q["frames"]) for q in received)
    keys = [r["q"]["key_type"] for r in records if r.get("event") == "security:key_updated" and "client=true" in r["source"]]
    assert keys == ["client_initial_secret", "server_initial_secret"]
    timers = [{"time": r["time"], "line": r["line"], **r["q"]} for r in records
              if r.get("event") == "recovery:loss_timer_updated" and "client=false" in r["source"]]
    summary[name] = {"sha256": sha, "record_count": len(records), "metadata": records[0]["data"],
                     "Initial_CRYPTO_attempts": attempts, "accepted_ACK_only_Initials": len(received),
                     "client_keys": keys, "server_timers": timers,
                     "deadline": next(r["data"] for r in records if r["source"] == "dial_context")}
print(json.dumps(summary, indent=2))
