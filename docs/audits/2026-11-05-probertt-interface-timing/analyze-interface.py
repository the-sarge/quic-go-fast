"""Offline interface timing for issue #1210; does not run captures or workloads.

BPF timestamps are host software observations, not wire times. Each subtraction
uses one endpoint's clock; no cross-host epoch comparison or cohort correction.
"""
import argparse
import bisect
import collections
import contextlib
import gzip
import importlib.util
import ipaddress
import json
from pathlib import Path
import struct
import sys
import tempfile

sys.dont_write_bytecode = True


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


KERNEL = load_module("q1210_kernel_analysis", Path(__file__).resolve().parent.parent /
                     "2026-11-05-probertt-kernel-timestamps/analyze-kernel.py")


def identity(length, prefix, mode="size-class-v2"):
    """Mirror Go Identity on a complete length and the required protected prefix."""
    if length == 0:
        return 0
    if length < 0 or len(prefix) < min(32, length):
        raise ValueError("identity needs nonempty payload and complete prefix")
    value = 14695981039346656037
    for byte in length.to_bytes(8, "little") + prefix[:min(32, length)]:
        value = ((value ^ byte) * 1099511628211) & ((1 << 64) - 1)
    if mode == "size-class-v2":
        value &= (1 << 63) - 1
        value = value or 1
        if length < 256:
            value |= 1 << 63
    elif mode != "legacy":
        raise ValueError("unknown identity mode: " + mode)
    value = value or 1
    return value if value < 1 << 63 else value - (1 << 64)


class PacketRejected(ValueError):
    pass


def decode_packet(frame, original_length, sender_ip, receiver_ip, mode):
    """Return only validated IPv4 UDP metadata; a snaplen-truncated body is fine."""
    def require(condition, reason):
        if not condition:
            raise PacketRejected(reason)
    require(len(frame) >= 14, "truncated_ethernet")
    offset, ether_type, vlans = 14, int.from_bytes(frame[12:14], "big"), []
    while ether_type in (0x8100, 0x88a8, 0x9100):
        require(len(vlans) < 2, "more_than_two_vlan_tags")
        require(len(frame) >= offset + 4, "truncated_vlan")
        vlans.append(int.from_bytes(frame[offset:offset+2], "big"))
        ether_type = int.from_bytes(frame[offset+2:offset+4], "big")
        offset += 4
    require(ether_type == 0x0800, "not_ipv4")
    require(len(frame) >= offset + 20, "truncated_ipv4")
    version_ihl = frame[offset]
    ihl = (version_ihl & 15) * 4
    require(version_ihl >> 4 == 4 and ihl >= 20, "malformed_ipv4_header")
    require(len(frame) >= offset + ihl, "truncated_ipv4_options")
    total = int.from_bytes(frame[offset+2:offset+4], "big")
    require(total >= ihl, "malformed_ipv4_total_length")
    require(original_length >= offset + total, "original_frame_shorter_than_ip_packet")
    require(frame[offset+9] == 17, "not_udp")
    source, dest = frame[offset+12:offset+16], frame[offset+16:offset+20]
    if (source, dest) == (sender_ip, receiver_ip):
        direction = "receive"
    elif (source, dest) == (receiver_ip, sender_ip):
        direction = "send"
    else:
        raise PacketRejected("other_address_pair")
    # Reject even a first fragment: its UDP length is not a complete datagram.
    fragment = int.from_bytes(frame[offset+6:offset+8], "big")
    require(fragment & 0x3fff == 0, "fragmented_ipv4")
    require(fragment & 0x8000 == 0, "reserved_ipv4_fragment_flag")
    offset += ihl
    require(total >= ihl + 8, "malformed_udp_ip_length")
    require(len(frame) >= offset + 8, "truncated_udp_header")
    source_port, dest_port, udp_length, _ = struct.unpack_from("!HHHH", frame, offset)
    require(udp_length >= 8 and udp_length == total - ihl, "malformed_udp_length")
    payload_length = udp_length - 8
    require(payload_length > 0, "empty_udp_payload")
    prefix_length = min(32, payload_length)
    require(len(frame) >= offset + 8 + prefix_length, "truncated_identity_prefix")
    prefix = frame[offset+8:offset+8+prefix_length]
    return dict(direction=direction, identity=identity(payload_length, prefix, mode),
                payload_length=payload_length, source_port=source_port, dest_port=dest_port,
                vlan_tags=vlans, ipv4_header_length=ihl)


class Capture:
    """Stream classic pcap and retain only needed identity keys, rejecting duplicates."""
    MAGIC = {b"\xd4\xc3\xb2\xa1": ("<", 1000), b"\xa1\xb2\xc3\xd4": (">", 1000),
             b"\x4d\x3c\xb2\xa1": ("<", 1), b"\xa1\xb2\x3c\x4d": (">", 1)}

    def __init__(self, sender_ip="192.168.1.50", receiver_ip="192.168.1.110",
                 mode="size-class-v2", wanted=None):
        self.sender_ip = ipaddress.IPv4Address(sender_ip).packed
        self.receiver_ip = ipaddress.IPv4Address(receiver_ip).packed
        self.mode, self.wanted = mode, wanted
        self.packets, self.duplicates = {}, collections.Counter()
        self.rejected, self.counts, self.files = collections.Counter(), collections.Counter(), []
        self.retained_outputs = []

    def read(self, stream, name="<stream>", retained_stream=None, retained_name="<stream>"):
        header = stream.read(24)
        if len(header) != 24 or header[:4] not in self.MAGIC:
            raise ValueError("expected complete classic pcap header: " + name)
        endian, quantum = self.MAGIC[header[:4]]
        major, minor, _, _, snaplen, linktype = struct.unpack(endian + "HHIIII", header[4:])
        if (major, minor) != (2, 4) or linktype != 1 or snaplen < 14:
            raise ValueError("expected pcap 2.4 Ethernet linktype 1 and snaplen >= 14: " + name)
        info = dict(path=name, timestamp_quantum_ns=quantum, snaplen=snaplen, records=0,
                    timestamp_regressions=0)
        retained = None
        def retain(raw):
            if retained_stream.write(raw) != len(raw):
                raise OSError("short write to retained pcap: " + retained_name)
            retained["bytes"] += len(raw)
        if retained_stream is not None:
            retained = dict(source=name, path=retained_name, packet_records=0, bytes=0,
                            selection="Every valid packet with a wanted direction/identity, including all duplicate records. Original global and packet headers and captured bytes are preserved.")
            retain(header)
        previous = None
        while raw_header := stream.read(16):
            if len(raw_header) != 16:
                raise ValueError("truncated pcap record header: " + name)
            seconds, fraction, captured, original = struct.unpack(endian + "IIII", raw_header)
            if captured > snaplen or captured > original or fraction >= 1_000_000_000 // quantum:
                raise ValueError("invalid pcap record header: " + name)
            # IPv4's maximum length plus link headers is < 64 KiB. Larger records
            # cannot be this workload; fail before allowing an unbounded allocation.
            if captured > 1_048_576:
                raise ValueError("pcap record exceeds 1 MiB: " + name)
            frame = stream.read(captured)
            if len(frame) != captured:
                raise ValueError("truncated pcap record data: " + name)
            info["records"] += 1
            self.counts["records"] += 1
            timestamp = seconds * 1_000_000_000 + fraction * quantum
            if previous is not None and timestamp < previous:
                info["timestamp_regressions"] += 1
            previous = timestamp
            try:
                packet = decode_packet(frame, original, self.sender_ip, self.receiver_ip, self.mode)
            except PacketRejected as error:
                self.rejected[str(error)] += 1
                continue
            self.counts["valid_workload_address_udp"] += 1
            key = (packet["direction"], packet["identity"])
            if self.wanted is not None and key not in self.wanted:
                self.counts["identity_not_needed"] += 1
                continue
            self.counts["needed_packet_records"] += 1
            if retained is not None:
                retain(raw_header)
                retain(frame)
                retained["packet_records"] += 1
            if key in self.duplicates:
                self.duplicates[key] += 1
            elif key in self.packets:
                del self.packets[key]
                self.duplicates[key] = 2
            else:
                packet.update(timestamp_ns=timestamp, timestamp_quantum_ns=quantum,
                              record=info["records"], capture=name)
                self.packets[key] = packet
        self.files.append(info)
        if retained is not None:
            self.retained_outputs.append(retained)
        return self

    def quality(self):
        return dict(files=self.files, counts=dict(self.counts), packet_rejections=dict(self.rejected),
                    unique_needed_identities=len(self.packets),
                    ambiguous_identities=[dict(direction=k[0], identity=k[1], records=n)
                                          for k, n in sorted(self.duplicates.items())],
                    capture_drop_counts="not stored in pcap; inspect collector stderr separately",
                    retained_outputs=self.retained_outputs)


def duration_fields(**values):
    result = {}
    for name, value in values.items():
        result[name + "_ns"] = value
        result[name + "_us"] = value / 1000
    return result


def analyze_rows(data, base, measurement_s, capture):
    sender, receiver = KERNEL.Endpoint("send", data["send"]), KERNEL.Endpoint("receive", data["receive"])
    probes, phase = KERNEL.probe_selector(base, measurement_s)
    turnaround, partial, complete, excluded = [], [], [], []
    # The row31 fields come from one time.Now() value, but Darwin's Go runtime
    # reads walltime() and then nanotime(): these clocks are not sampled atomically.
    # Scheduling between reads biases mono-wall and can make the optional split
    # negative. Neighboring observations show sensitivity, not an error bound.
    # BPF turnaround and the combined pre-socket/post-write term cancel this offset.
    anchor_rows = sorted((r for r in receiver.rows[31] if r[5] and r[6]), key=lambda r: r[6])
    anchor_walls = [r[6] for r in anchor_rows]
    def anchor_observations(packet, socket_id):
        position = bisect.bisect_left(anchor_walls, packet["timestamp_ns"])
        neighbors = anchor_rows[max(0, position-1):position+1]
        return [dict(wall_ns=r[6], mono_ns=r[5], offset_ns=r[5]-r[6], socket_id=r[12])
                for r in neighbors if r[12] == socket_id]
    def reject(reason, ack, index, stage="turnaround"):
        excluded.append(dict(probe=index, ack_carrier=ack[2], ack_identity=ack[3],
                             largest_acked=ack[4], stage=stage, reason=reason))
    for ack in sender.ack_frames.values():
        index = phase(ack[5])
        if index is None:
            continue
        ack_identity, largest = ack[3], ack[4]
        sent, peer_ack = sender.tx_pn.get(largest), receiver.tx_identity.get(ack_identity)
        received_ack = sender.rx_identity.get(ack_identity)
        if (sent is None or peer_ack is None or received_ack is None or peer_ack[2] != ack[2]
                or peer_ack[6] != largest or received_ack[2] != ack[2]
                or peer_ack[4] != received_ack[4]
                or sender.tx_identity.get(sent[3]) != sent
                or receiver.tx_pn.get(peer_ack[2]) != peer_ack
                or sender.rx_pn.get(ack[2]) != received_ack):
            reject("missing_or_inconsistent_packet_identity_link", ack, index)
            continue
        peer_data = receiver.rx_identity.get(sent[3])
        if (peer_data is None or peer_data[2] != largest or peer_data[4] != sent[4]
                or receiver.rx_pn.get(largest) != peer_data):
            reject("missing_or_inconsistent_data_receive", ack, index)
            continue
        if phase(sent[1]) != index:
            reject("data_outside_same_steady_probe", ack, index)
            continue
        keys = (("receive", sent[3]), ("send", ack_identity))
        if any(key in capture.duplicates for key in keys):
            reject("ambiguous_bpf_identity", ack, index)
            continue
        data_packet, ack_packet = (capture.packets.get(key) for key in keys)
        if data_packet is None or ack_packet is None:
            reject("missing_bpf_data" if data_packet is None else "missing_bpf_ack", ack, index)
            continue
        if data_packet["payload_length"] != sent[4] or ack_packet["payload_length"] != peer_ack[4]:
            reject("bpf_payload_length_mismatch", ack, index)
            continue
        if (data_packet["source_port"], data_packet["dest_port"]) != (ack_packet["dest_port"], ack_packet["source_port"]):
            reject("bpf_data_ack_flow_mismatch", ack, index)
            continue
        raw = ack[5] - sent[5]
        peer = ack_packet["timestamp_ns"] - data_packet["timestamp_ns"]
        row = dict(probe=index, largest_acked=largest, ack_carrier=ack[2], data_identity=sent[3],
                   ack_identity=ack_identity, bpf_data=data_packet, bpf_ack=ack_packet,
                   sender_logical_send_ns=sent[5], sender_protocol_receive_ns=ack[5],
                   **duration_fields(raw_rtt=raw, receiver_interface_turnaround=peer,
                                     remaining_including_sender_tx_and_rx=raw-peer))
        turnaround.append(row)
        sr = sender.rx(ack_identity)
        if sr is None:
            reject("missing_or_invalid_linux_rx_boundary", ack, index, "partial")
            continue
        sender_rx = ack[5] - sr[4]
        row = dict(row, sender_software_rx_ns=sr[4],
                   **duration_fields(sender_kernel_rx_to_protocol_receive=sender_rx,
                                     remaining_including_sender_logical_send_to_tx=raw-peer-sender_rx))
        partial.append(row)
        st = sender.tx(sent[3])
        if st is None:
            reject("missing_or_invalid_linux_tx_boundary", ack, index, "complete")
            continue
        submission, events = st
        if submission[7] < sent[4] or (submission[8] and submission[8] != sent[4]):
            reject("inconsistent_first_gso_segment", ack, index, "complete")
            continue
        sender_tx = events["software"]["mono"] - sent[5]
        row = dict(row, data_gso_size=submission[8], data_submission_segment_count=submission[12],
                   sender_software_tx_ns=events["software"]["mono"],
                   **duration_fields(sender_logical_send_to_software_tx=sender_tx,
                                     residual_outside_receiver_interface=raw-peer-sender_rx-sender_tx))
        # Optional additional receiver-local duration needs no BPF/monotime mapping.
        rr, write = receiver.rx(sent[3]), receiver.write_pn.get(peer_ack[2])
        if rr is not None and write is not None:
            socket_to_write = write[3] - rr[4]
            row.update(duration_fields(receiver_socket_delivery_to_ack_write_start=socket_to_write,
                                       receiver_combined_pre_socket_and_post_write=peer-socket_to_write))
            offset = rr[5] - rr[6]
            observations = anchor_observations(data_packet, rr[12]) + anchor_observations(ack_packet, rr[12])
            offsets = [offset] + [r["offset_ns"] for r in observations]
            row.update(duration_fields(
                receiver_interface_rx_to_socket_delivery=rr[4]-(data_packet["timestamp_ns"]+offset),
                receiver_ack_write_start_to_interface_tx=ack_packet["timestamp_ns"]+offset-write[3]))
            row["receiver_wall_mapping"] = dict(
                data_batch_wall_ns=rr[6], data_batch_mono_ns=rr[5], offset_ns=offset,
                neighboring_anchors=observations,
                observed_neighbor_offset_span_ns=max(offsets)-min(offsets),
                maximum_observed_neighbor_offset_delta_ns=max(abs(v-offset) for v in offsets),
                note="Both BPF endpoints use the matched data batch anchor. On Darwin, Go time.Now reads walltime then nanotime nonatomically; scheduling between reads can bias this split, including negative pre-socket time. Neighbor offset changes show sensitivity, not an error bound. Capture timestamp quantum is reported on each packet. Prefer the calibration-independent turnaround and combined pre-socket/post-write term; clock steps between BPF timestamps remain possible.")
        complete.append(row)
    cohorts = dict(turnaround_cycles=turnaround, partial_cycles=partial, complete_cycles=complete)
    per_probe = []
    for p in probes:
        selected = {name: [r for r in rows if r["probe"] == p["index"]] for name, rows in cohorts.items()}
        per_probe.append(dict(index=p["index"], start_mono_ns=p["lo"], end_mono_ns=p["hi"],
                              sample_counts={name: len(rows) for name, rows in selected.items()},
                              **{name: KERNEL.grouped_stats(rows) for name, rows in selected.items()}))
    return dict(probes=per_probe, aggregate={name: KERNEL.grouped_stats(rows) for name, rows in cohorts.items()},
                sample_counts={name: len(rows) for name, rows in cohorts.items()},
                correlated_turnaround_cycles=turnaround, correlated_partial_cycles=partial,
                correlated_cycles=complete, excluded_cycles=excluded,
                exclusions=dict(collections.Counter(row["stage"] + ":" + row["reason"] for row in excluded)),
                capture_quality=capture.quality(),
                endpoint_quality=dict(send=sender.quality(), receive=receiver.quality()),
                interpretation=dict(
                    residual="Sum of Linux software TX to macOS BPF RX, and macOS BPF TX to Linux software RX. Includes NIC/driver/network work; not wire-only or one-way latency.",
                    clock="BPF turnaround uses two macOS host-clock timestamps; all other terms use Linux-local differences. Constant host epoch offsets cancel. Clock steps within a cycle do not cancel; inspect calibration and negative values.",
                    cohorts="Complete is a subset of partial, which is a subset of turnaround. Every correction comes from the same identified cycle. Missing TX stamps are never replaced by other samples or cohort medians.",
                    selection="Complete measurement ProbeRTT intervals, with first/last 2 ms excluded. Data and ACK must match the same steady probe. Identity ambiguity rejects all candidates.",
                    identity_mode=capture.mode,
                    sampling=("size-class-v2 selects all payloads shorter than 256 bytes and about 1/16 larger payloads. This is size-based sampling, not proof that every short packet contains an ACK. Only parsed ACK-frame links enter cycles." if capture.mode == "size-class-v2" else "Legacy observer selects about 1/16 identities in each direction; complete cycles additionally require the first Linux GSO segment."),
                    negative_values="Retained and counted in distributions; inspect timing semantics and clocks before interpretation."))


def analyze(root, pcaps, arm=None, base_path=None, mode="size-class-v2", sender_ip="192.168.1.50", receiver_ip="192.168.1.110", retained_pcap=None):
    if retained_pcap is not None and len(pcaps) != 1:
        raise ValueError("--retained-pcap requires exactly one input pcap")
    root = Path(root)
    original = KERNEL.load_base_module()
    candidates = [root/"controller"/arm/"result.json"] if arm else list(root.glob("controller/*/result.json"))
    if len(candidates) != 1:
        raise ValueError("specify --arm when root does not contain exactly one result")
    result_path = candidates[0]
    result = json.loads(result_path.read_text())
    base = json.loads(Path(base_path).read_text()) if base_path else original.analyze(root, arm)
    with tempfile.TemporaryDirectory(prefix="q1210-interface-analysis-") as scratch:
        data = KERNEL.load_rows(root, result_path.parent, Path(scratch), original)
        wanted = {("receive", r[3]) for r in data["send"]["rows"][50]}
        wanted.update(("send", r[3]) for r in data["receive"]["rows"][50])
        capture = Capture(sender_ip, receiver_ip, mode, wanted)
        for path in pcaps:
            path = Path(path)
            opener = gzip.open if path.suffix == ".gz" else open
            with contextlib.ExitStack() as stack:
                stream = stack.enter_context(opener(path, "rb"))
                retained_stream = (stack.enter_context(open(retained_pcap, "xb"))
                                   if retained_pcap is not None else None)
                capture.read(stream, str(path), retained_stream, str(retained_pcap))
        output = analyze_rows(data, base, result["config"]["measurement_ms"]/1000, capture)
    output.update(root=str(root), arm=result_path.parent.name, status=result["status"], summary=result["summary"],
                  baseline_analysis={key: base.get(key) for key in ("headers", "clock_anchor_drift_ns", "probes", "window_alignment_qualified")})
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--pcap", type=Path, action="append", required=True)
    parser.add_argument("--retained-pcap", type=Path,
                        help="Write a new uncompressed classic pcap of all wanted valid packet records, including duplicates; requires one input")
    parser.add_argument("--arm")
    parser.add_argument("--base-analysis", type=Path)
    parser.add_argument("--identity-mode", choices=("size-class-v2", "legacy"), default="size-class-v2")
    parser.add_argument("--sender-ip", default="192.168.1.50")
    parser.add_argument("--receiver-ip", default="192.168.1.110")
    args = parser.parse_args()
    output = analyze(args.root, args.pcap, args.arm, args.base_analysis, args.identity_mode, args.sender_ip, args.receiver_ip, args.retained_pcap)
    args.output.write_text(json.dumps(output, indent=2) + "\n")
    print(json.dumps({key: output[key] for key in ("arm", "status", "sample_counts", "aggregate", "exclusions")}, indent=2))
