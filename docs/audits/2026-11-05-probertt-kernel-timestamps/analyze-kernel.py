"""Offline kernel-boundary analysis for issue #1210; no qualification verdict.

All differences are computed within one endpoint's clock. Linux software RX/TX
stamps are not wire timestamps. Darwin RX is UDP socket delivery, not NIC ingress.
Only the first GSO segment can identify a timestamped Linux submission. Ambiguous
identity/PN/event joins and coalesced RX aggregates are excluded explicitly.
"""
import argparse
import bisect
import collections
import gzip
import importlib.util
import json
from pathlib import Path
import statistics
import sys
sys.dont_write_bytecode = True
import struct
import tarfile
import tempfile


def stats(values):
    values = sorted(values)
    if not values:
        return {"n": 0}
    return dict(n=len(values), mean=statistics.fmean(values), p50=statistics.median(values),
                p95=values[int(.95*(len(values)-1))], p99=values[int(.99*(len(values)-1))],
                min=values[0], max=values[-1], negative=sum(v < 0 for v in values))


def grouped_stats(items):
    keys = sorted({key for item in items for key in item if key.endswith("_us")})
    return {key: stats(item[key] for item in items if key in item) for key in keys}


def unique_index(rows, key):
    """Never select one of multiple candidates, even byte-identical duplicates."""
    groups = collections.defaultdict(list)
    for row in rows:
        groups[key(row)].append(row)
    return ({k: v[0] for k, v in groups.items() if len(v) == 1},
            {k: len(v) for k, v in groups.items() if len(v) != 1})


def record_iter(path):
    opener = gzip.open if path.suffix == ".gz" else open
    with opener(path, "rb") as stream:
        while block := stream.read(128*8192):
            if len(block) % 128:
                raise ValueError("truncated diagnostic records: " + str(path))
            yield from struct.iter_unpack("<16q", block)


def load_base_module():
    path = Path(__file__).resolve().parent.parent / "2026-11-05-probertt-investigation/analyze.py"
    spec = importlib.util.spec_from_file_location("probertt_original_analysis", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_rows(root, directory, scratch, base_module):
    headers = {role: base_module.header(directory/(role+"-stderr.log")) for role in ("send", "receive")}
    paths = {}
    for role, header in headers.items():
        name = Path(header["diag1210"]).name
        candidates = ([root/name, root/(name+".gz")] if role == "send" else
                      [root/"local"/name, root/"local"/(name+".gz")])
        path = next((p for p in candidates if p.is_file()), None)
        if path is None and role == "send":
            with tarfile.open(root/"remote-worker-files.tar.gz") as archive:
                members = [m for m in archive.getmembers() if m.isfile() and Path(m.name).name == name]
                if len(members) != 1:
                    raise ValueError(f"expected one sender trace {name}, found {len(members)}")
                path = scratch/name
                with archive.extractfile(members[0]) as source, path.open("wb") as target:
                    while data := source.read(1024*1024):
                        target.write(data)
        if path is None:
            raise FileNotFoundError(name)
        paths[role] = path
    data = {}
    for role, path in paths.items():
        kept = collections.defaultdict(list)
        counts = collections.Counter()
        for row in record_iter(path):
            counts[row[0]] += 1
            if row[0] in (1, 8, 30, 31, 32, 33, 34, 38, 39, 50, 51, 52):
                kept[row[0]].append(row)
        if sum(counts.values()) != headers[role]["records"]:
            raise ValueError("header record count mismatch: " + role)
        for rows in kept.values():
            rows.sort(key=lambda r: r[1])
        data[role] = {"rows": kept, "counts": dict(counts), "header": headers[role]}
    return data


class Endpoint:
    def __init__(self, role, data):
        self.role, self.rows, self.header = role, data["rows"], data["header"]
        self.record_counts = data["counts"]
        self.ambiguities = {}
        def index(name, kind, key):
            found, duplicates = unique_index(self.rows[kind], key)
            self.ambiguities[name] = {"ambiguous_keys": len(duplicates), "ambiguous_rows": sum(duplicates.values())}
            return found
        self.tx_identity = index("tx_identity", 50, lambda r: r[3])
        self.tx_pn = index("tx_packet_number", 50, lambda r: r[2])
        self.rx_identity = index("rx_identity", 51, lambda r: r[3])
        self.rx_pn = index("rx_packet_number", 51, lambda r: r[2])
        self.kernel_rx = index("kernel_rx_identity", 31, lambda r: r[2])
        self.tx_submission_identity = index("tx_submission_identity", 32, lambda r: r[3])
        self.tx_submission_id = index("tx_submission_id", 32, lambda r: (r[11], r[2]))
        self.tx_event = index("tx_timestamp_id_type", 33, lambda r: (r[12], r[2], r[3]))
        self.write_pn = index("write_packet_number", 8, lambda r: r[2])
        self.ack_frames = index("ack_frame_identity_largest", 52, lambda r: (r[3], r[4]))
        self.setup = index("socket_setup", 30, lambda r: r[2])
        self.platform = next(iter(self.setup.values()))[3] if self.setup else 0
        self.rejections = collections.Counter()

    def rx(self, identity):
        r = self.kernel_rx.get(identity)
        if r is None:
            self.rejections["rx_missing_or_ambiguous_identity"] += 1
            return None
        setup = self.setup.get(r[12])
        if setup is None or not setup[4] or setup[5] or setup[7] != r[11]:
            self.rejections["rx_missing_or_inconsistent_clock_setup"] += 1
            return None
        platform = setup[3]
        truncated = (r[9] & (0x28 if platform == 1 else 0x30)) != 0
        # Row31 position15 is GRO segment size in schema v1 final observer.
        if not r[13] or r[14] or not r[3] or not r[4] or truncated:
            self.rejections["rx_invalid_or_truncated_timestamp"] += 1
            return None
        if r[15] > 0 and r[10] > r[15]:
            self.rejections["rx_coalesced_aggregate"] += 1
            return None
        if r[11] == 1:
            converted = r[3]+r[5]-r[6]
        elif r[11] == 2 and setup[8] > 0 and setup[9] > 0:
            converted = r[3]*setup[8]//setup[9]+setup[10]
        else:
            self.rejections["rx_unsupported_clock_or_timebase"] += 1
            return None
        if converted != r[4]:
            self.rejections["rx_stored_clock_conversion_mismatch"] += 1
            return None
        packet = self.rx_identity.get(identity)
        if packet is not None and (self.rx_pn.get(packet[2]) != packet or packet[4] != r[10]):
            self.rejections["rx_ambiguous_packet_or_payload_length_mismatch"] += 1
            return None
        return r

    def tx(self, identity):
        s = self.tx_submission_identity.get(identity)
        if s is None:
            self.rejections["tx_missing_or_ambiguous_submission"] += 1
            return None
        if self.tx_submission_id.get((s[11], s[2])) != s:
            self.rejections["tx_ambiguous_submission_identifier"] += 1
            return None
        setup = self.setup.get(s[11])
        if setup is None or not setup[4] or setup[5] or setup[3] != 1 or setup[7] != 1:
            self.rejections["tx_missing_or_inconsistent_linux_clock_setup"] += 1
            return None
        if s[10] or s[9] != s[7]:
            self.rejections["tx_unsuccessful_or_partial_submission"] += 1
            return None
        events = {}
        for kind, label in ((1, "schedule"), (0, "software")):
            r = self.tx_event.get((s[11], s[2], kind))
            if r is None:
                self.rejections["tx_missing_or_ambiguous_"+label] += 1
                return None
            if not r[13] or r[14] or not r[4] or r[7] != 42 or r[8] != 4 or r[9] & 0x8:
                self.rejections["tx_invalid_"+label] += 1
                return None
            events[label] = {"row": r, "mono": r[4]+r[5]-r[6]}
        return s, events

    def quality(self):
        # Periodic counters are lower bounds unless the final close emitted one.
        final = {}
        for r in self.rows[39]:
            final[str(r[2])] = list(r)
        offsets = [r[5]-r[6] for r in self.rows[31]] + [r[5]-r[6] for r in self.rows[33]]
        zero = offsets[0] if offsets else 0
        return dict(record_counts=self.rows_count(), dropped_records=self.header.get("dropped"),
                    setup=[list(r) for r in self.rows[30]], errors=[list(r) for r in self.rows[38]],
                    last_periodic_counters=final, counter_note="last recorded snapshot; may precede final packet",
                    clock_calibrations=[list(r) for r in self.rows[34]],
                    same_endpoint_wall_to_monotonic_offset_change_us=stats((v-zero)/1000 for v in offsets),
                    darwin_calibration_offset_change_us=stats((r[6]-r[8])/1000 for r in self.rows[34]),
                    darwin_calibration_interval_disjoint_count=sum(abs(r[6]-r[8]) > r[7]+self.setup.get(r[2], (0,)*16)[11] for r in self.rows[34]),
                    ambiguous_joins=self.ambiguities, lookup_rejections=dict(self.rejections))

    def rows_count(self):
        return {str(kind): count for kind, count in self.record_counts.items()}


def probe_selector(base, measurement_s):
    selected = []
    for probe in base.get("probes", []):
        end_s = probe["start_schedule_s"] + probe["duration_ms"]/1000
        if probe["complete"] and probe["start_schedule_s"] >= 0 and end_s <= measurement_s:
            selected.append(dict(index=probe["index"], lo=probe["start_mono_ns"]+2_000_000,
                                 hi=probe["end_mono_ns"]-2_000_000, baseline=probe))
    def find(mono):
        for probe in selected:
            if probe["lo"] <= mono < probe["hi"]:
                return probe["index"]
        return None
    return selected, find


def rx_values(endpoint, kernel, authenticated):
    result = dict(kernel_to_read_batch_return_us=(kernel[5]-kernel[4])/1000,
                  batch_return_to_protocol_receive_time_us=(kernel[1]-kernel[5])/1000,
                  receive_batch_count=kernel[8], gro_segment_size=kernel[15])
    if authenticated is not None:
        result.update(batch_return_to_protocol_receive_time_us=(authenticated[5]-kernel[5])/1000,
                      protocol_receive_to_authenticated_processing_us=(authenticated[1]-authenticated[5])/1000,
                      kernel_to_protocol_receive_time_us=(authenticated[5]-kernel[4])/1000)
    return result


def tx_values(submission, events):
    schedule, software = events["schedule"]["mono"], events["software"]["mono"]
    start_offset = submission[4]-submission[5]
    offsets = [event["row"][5]-event["row"][6] for event in events.values()]
    return dict(write_start_to_scheduler_us=(schedule-submission[4])/1000,
                scheduler_to_software_tx_us=(software-schedule)/1000,
                write_start_to_software_tx_us=(software-submission[4])/1000,
                write_call_us=(submission[1]-submission[4])/1000,
                timestamp_read_after_software_tx_us=(events["software"]["row"][1]-software)/1000,
                wall_clock_offset_change_start_to_event_us=max(abs(v-start_offset) for v in offsets)/1000,
                submission_bytes=submission[7], gso_size=submission[8], segment_count=submission[12])


def analyze_rows(data, base, measurement_s):
    sender, receiver = Endpoint("send", data["send"]), Endpoint("receive", data["receive"])
    probes, phase = probe_selector(base, measurement_s)
    sender_rx, receiver_rx, sender_tx, cycles, partial_cycles = [], [], [], [], []
    exclusions = collections.Counter()
    for identity, r in sender.kernel_rx.items():
        index = phase(r[5])
        if index is None:
            continue
        k = sender.rx(identity)
        if k is not None:
            sender_rx.append(dict(probe=index, identity=identity, **rx_values(sender, k, sender.rx_identity.get(identity))))
    for identity, r in receiver.kernel_rx.items():
        tx = sender.tx_identity.get(identity)
        rx = receiver.rx_identity.get(identity)
        if (tx is None or rx is None or tx[2] != rx[2] or tx[4] != rx[4]
                or sender.tx_pn.get(tx[2]) != tx or receiver.rx_pn.get(rx[2]) != rx):
            exclusions["receiver_rx_missing_or_inconsistent_sender_packet"] += 1
            continue
        index = phase(tx[1])
        if index is None:
            continue
        k = receiver.rx(identity)
        if k is not None:
            receiver_rx.append(dict(probe=index, identity=identity, packet_number=tx[2], **rx_values(receiver, k, rx)))
    for identity, s in sender.tx_submission_identity.items():
        index = phase(s[4])
        if index is None:
            continue
        found = sender.tx(identity)
        if found is not None:
            submission, events = found
            sender_tx.append(dict(probe=index, identity=identity, **tx_values(submission, events)))

    # Complete cycles use both packet identities, receiver ACK->largest mapping,
    # the sender's original RTT timestamps, and local kernel/write boundaries.
    for ack in sender.ack_frames.values():
        index = phase(ack[5])
        if index is None:
            continue
        ack_identity, largest = ack[3], ack[4]
        sent = sender.tx_pn.get(largest)
        peer_ack = receiver.tx_identity.get(ack_identity)
        received_ack = sender.rx_identity.get(ack_identity)
        if (sent is None or peer_ack is None or peer_ack[2] != ack[2] or peer_ack[6] != largest
                or sender.tx_identity.get(sent[3]) != sent or receiver.tx_pn.get(peer_ack[2]) != peer_ack
                or received_ack is None or received_ack[2] != ack[2]
                or sender.rx_pn.get(ack[2]) != received_ack):
            exclusions["cycle_missing_or_inconsistent_identity_link"] += 1
            continue
        data_identity = sent[3]
        peer_data = receiver.rx_identity.get(data_identity)
        if (peer_data is None or peer_data[2] != largest or peer_data[4] != sent[4]
                or receiver.rx_pn.get(largest) != peer_data):
            exclusions["cycle_missing_or_inconsistent_data_receive"] += 1
            continue
        # A packet launched outside the trimmed probe is not a steady cycle.
        if phase(sent[1]) != index:
            exclusions["cycle_data_outside_same_steady_probe"] += 1
            continue
        sr, rr, st = sender.rx(ack_identity), receiver.rx(data_identity), sender.tx(data_identity)
        write = receiver.write_pn.get(peer_ack[2])
        if sr is not None and rr is not None and write is not None:
            # This cohort does not require a Linux TX timestamp. Its remainder
            # deliberately retains all sender logical-send-to-TX residence.
            # Never subtract a typical TX cost from a different sample cohort.
            partial_raw = ack[5]-sent[5]
            partial_peer = write[3]-rr[4]
            partial_sender_rx = ack[5]-sr[4]
            partial_cycles.append(dict(probe=index, largest_acked=largest, ack_carrier=ack[2],
                                       data_identity=data_identity, ack_identity=ack_identity,
                                       raw_rtt_us=partial_raw/1000,
                                       receiver_socket_delivery_to_ack_write_start_us=partial_peer/1000,
                                       sender_kernel_rx_to_protocol_receive_us=partial_sender_rx/1000,
                                       remaining_including_sender_logical_send_to_tx_us=(partial_raw-partial_peer-partial_sender_rx)/1000))
        if sr is None or rr is None or st is None or write is None:
            exclusions["cycle_missing_kernel_or_ack_write_boundary"] += 1
            continue
        submission, events = st
        if submission[7] < sent[4] or (submission[8] and submission[8] != sent[4]):
            exclusions["cycle_inconsistent_first_gso_segment"] += 1
            continue
        # These terms partition raw RTT by each endpoint's local durations. They
        # do not subtract absolute times across machines or infer one-way delay.
        raw = ack[5]-sent[5]
        sender_before_software = events["software"]["mono"]-sent[5]
        receiver_socket_to_ack_write = write[3]-rr[4]
        sender_kernel_to_rcv = ack[5]-sr[4]
        residual = raw-sender_before_software-receiver_socket_to_ack_write-sender_kernel_to_rcv
        cycles.append(dict(probe=index, largest_acked=largest, ack_carrier=ack[2],
                           data_identity=data_identity, ack_identity=ack_identity,
                           data_gso_size=submission[8], data_submission_segment_count=submission[12],
                           ack_write_batch_count_marker=write[5], raw_rtt_us=raw/1000,
                           sender_logical_send_to_software_tx_us=sender_before_software/1000,
                           receiver_socket_delivery_to_ack_write_start_us=receiver_socket_to_ack_write/1000,
                           sender_kernel_rx_to_protocol_receive_us=sender_kernel_to_rcv/1000,
                           residual_after_measured_local_durations_us=residual/1000,
                           receiver_socket_to_batch_return_us=(rr[5]-rr[4])/1000,
                           receiver_batch_return_to_protocol_receive_us=(peer_data[5]-rr[5])/1000,
                           receiver_protocol_receive_to_ack_write_us=(write[3]-peer_data[5])/1000,
                           sender_kernel_rx_to_batch_return_us=(sr[5]-sr[4])/1000,
                           sender_batch_return_to_protocol_receive_us=(ack[5]-sr[5])/1000,
                           sender_write_start_to_scheduler_us=(events["schedule"]["mono"]-submission[4])/1000,
                           sender_scheduler_to_software_tx_us=(events["software"]["mono"]-events["schedule"]["mono"])/1000))
    per_probe = []
    for p in probes:
        selected = lambda values: [v for v in values if v["probe"] == p["index"]]
        per_probe.append(dict(index=p["index"], start_mono_ns=p["lo"], end_mono_ns=p["hi"],
                              sender_rx=grouped_stats(selected(sender_rx)), receiver_rx=grouped_stats(selected(receiver_rx)),
                              sender_tx=grouped_stats(selected(sender_tx)), complete_cycles=grouped_stats(selected(cycles)),
                              partial_cycles=grouped_stats(selected(partial_cycles)),
                              sample_counts=dict(sender_rx=len(selected(sender_rx)), receiver_rx=len(selected(receiver_rx)),
                                                 sender_tx=len(selected(sender_tx)), complete_cycles=len(selected(cycles)),
                                                 partial_cycles=len(selected(partial_cycles)))))
    return dict(probes=per_probe, aggregate=dict(sender_rx=grouped_stats(sender_rx), receiver_rx=grouped_stats(receiver_rx),
                                               sender_tx=grouped_stats(sender_tx), complete_cycles=grouped_stats(cycles),
                                               partial_cycles=grouped_stats(partial_cycles)),
                sample_counts=dict(sender_rx=len(sender_rx), receiver_rx=len(receiver_rx), sender_tx=len(sender_tx),
                                   complete_cycles=len(cycles), partial_cycles=len(partial_cycles)),
                exclusions=dict(exclusions), endpoint_quality={"send": sender.quality(), "receive": receiver.quality()},
                correlated_cycles=cycles, correlated_partial_cycles=partial_cycles,
                interpretation={"selection": "Complete measurement ProbeRTT intervals; first/last 2 ms excluded. Receiver phases use correlated sender packet identities, not clock-offset subtraction.",
                                "linux_software_tx": "Software scheduler/driver boundary, shared with the first segment of a potentially segmented submission; not wire departure.",
                                "darwin_rx": "UDP delivery timestamp before socket append; excludes earlier NIC/driver/IP processing.",
                                "residual": "Combined Linux post-software-TX through Darwin socket delivery and Darwin pre-write-call through Linux software RX. Includes unavailable Darwin TX, kernel/driver/NIC/network work; not one-way or pure propagation.",
                                "partial_cycles": "Raw RTT minus receiver socket-delivery-to-ACK-write-start and sender kernel-RX-to-protocol-receive, each measured on the same correlated cycle. The remainder retains sender logical-send-to-TX residence; no typical TX cost or GSO internal-packet mapping is substituted. Complete cycles are a subset, not an independent cohort.",
                                "sampling": "Identity selection about 1/16 in each direction; complete cycles require both independent identities plus the first Linux GSO segment. No confidence interval or representative tail claim.",
                                "ack_write_batch_count_marker": "Event8 field5 is accepted entries on the batch path and constant1 on the single-write path; it is not universal proof of successful submission.",
                                "negative_values": "Retained and counted; inspect clock uncertainty, boundary semantics and capture validity before interpretation."})


def analyze(root, arm=None, base_path=None):
    root = Path(root)
    module = load_base_module()
    result_path = root/"controller"/arm/"result.json" if arm else next(root.glob("controller/*/result.json"))
    result = json.loads(result_path.read_text())
    base = json.loads(Path(base_path).read_text()) if base_path else module.analyze(root, arm)
    with tempfile.TemporaryDirectory(prefix="q1210-kernel-analysis-") as tmp:
        data = load_rows(root, result_path.parent, Path(tmp), module)
        output = analyze_rows(data, base, result["config"]["measurement_ms"]/1000)
    output.update(root=str(root), arm=result_path.parent.name, status=result["status"], summary=result["summary"],
                  baseline_analysis={k: base.get(k) for k in ("headers", "clock_anchor_drift_ns", "probes", "window_alignment_qualified")})
    return output


def self_test():
    # Endpoint offsets intentionally differ by many days. All local terms remain
    # invariant; repeated identity and timestamp rows must never choose a winner.
    rows = [(50, 1, 7, 100), (50, 2, 8, 200), (50, 3, 9, 100)]
    unique, ambiguous = unique_index(rows, lambda r: r[3])
    assert set(unique) == {200} and ambiguous == {100: 2}
    s = [0]*16
    s[1], s[4], s[5], s[7], s[9], s[12] = 180, 100, 900000100, 1126, 1126, 1
    events = {}
    for name, raw, read_mono in (("schedule", 900000120, 200), ("software", 900000150, 210)):
        r = [0]*16
        r[1], r[4], r[5], r[6] = read_mono, raw, read_mono, read_mono+900000000
        events[name] = {"row": r, "mono": r[4]+r[5]-r[6]}
    v = tx_values(s, events)
    assert v["write_start_to_scheduler_us"] == .020
    assert v["scheduler_to_software_tx_us"] == .030
    assert v["wall_clock_offset_change_start_to_event_us"] == 0
    for event in events.values():
        event["row"][4] += 123456789000000
        event["row"][6] += 123456789000000
        event["mono"] = event["row"][4]+event["row"][5]-event["row"][6]
    s[5] += 123456789000000
    assert tx_values(s, events) == v
    assert stats([-1, 0, 1])["negative"] == 1
    print("Kernel analysis self-test passed: clock-offset invariance, ambiguity rejection, negative retention.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path, nargs="?")
    parser.add_argument("output", type=Path, nargs="?")
    parser.add_argument("--arm")
    parser.add_argument("--base-analysis", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
    else:
        if args.root is None or args.output is None:
            parser.error("root and output are required")
        output = analyze(args.root, args.arm, args.base_analysis)
        args.output.write_text(json.dumps(output, indent=2)+"\n")
        print(json.dumps({k: output[k] for k in ("arm", "status", "sample_counts", "aggregate", "exclusions")}, indent=2))
