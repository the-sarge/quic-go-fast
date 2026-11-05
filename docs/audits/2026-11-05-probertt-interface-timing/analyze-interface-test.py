"""Synthetic packet-to-cycle witnesses for the issue #1210 offline analyzer."""
import collections
import importlib.util
import io
import ipaddress
from pathlib import Path
import struct
import sys
import unittest

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("interface_analysis", Path(__file__).with_name("analyze-interface.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
A, B = (ipaddress.IPv4Address(s).packed for s in ("192.168.1.50", "192.168.1.110"))
DATA, ACK = bytes(range(256)) * 4 + bytes(range(102)), bytes(range(38))


def frame(payload=DATA, reverse=False, tags=(), ihl=20, fragment=0, ports=(1234, 443)):
    source, dest = (B, A) if reverse else (A, B)
    ethernet = bytes.fromhex("644bf0601482644bf0601483")
    for kind, value in tags:
        ethernet += struct.pack("!HH", kind, value)
    ethernet += struct.pack("!H", 0x0800)
    header = struct.pack("!BBHHHBBH4s4s", 0x40 | (ihl // 4), 0, ihl + 8 + len(payload),
                         0, fragment, 64, 17, 0, source, dest) + bytes(ihl - 20)
    return ethernet + header + struct.pack("!HHHH", *ports, len(payload) + 8, 0) + payload


def pcap(records, endian="<", nano=False, snaplen=128):
    result = struct.pack(endian + "IHHIIII", 0xa1b23c4d if nano else 0xa1b2c3d4, 2, 4, 0, 0, snaplen, 1)
    for timestamp, packet in records:
        seconds, fraction = divmod(timestamp, 1_000_000_000)
        fraction = fraction if nano else fraction // 1000
        captured = packet[:snaplen]
        result += struct.pack(endian + "IIII", seconds, fraction, len(captured), len(packet)) + captured
    return io.BytesIO(result)


def row(*values):
    return tuple(values) + (0,) * (16 - len(values))


def endpoint(rows):
    by_kind = collections.defaultdict(list)
    for record in rows:
        by_kind[record[0]].append(record)
    return dict(rows=by_kind, counts=dict(collections.Counter(r[0] for r in rows)), header={"dropped": 0})


def witness(linux_epoch=9_000_000_000):
    data_id, ack_id = m.identity(len(DATA), DATA), m.identity(len(ACK), ACK)
    s, r, w = 3_000_000, 100_000_000, linux_epoch
    send = [row(30, 1, 1, 1, 1, 0, 0, 1),
            row(50, s+2000, 7, data_id, len(DATA), s, -1, 0),
            row(51, s+260000, 99, ack_id, len(ACK), s+255000),
            row(52, s+261000, 99, ack_id, 7, s+255000, 0),
            row(31, s+255000, ack_id, w+s+220000, s+220000, s+250000, w+s+250000,
                1, 1, 0, len(ACK), 1, 1, 1, 0, 0),
            row(32, s+25000, 1, data_id, s+5000, w+s+5000, w+s+25000,
                len(DATA), 0, len(DATA), 0, 1, 1),
            row(33, s+30000, 1, 1, w+s+10000, s+30000, w+s+30000, 42, 4, 0, 0, 0, 1, 1, 0),
            row(33, s+31000, 1, 0, w+s+20000, s+31000, w+s+31000, 42, 4, 0, 0, 0, 1, 1, 0)]
    receive = [row(30, 1, 1, 2, 1, 0, 0, 2, 1, 1, 0, 1),
               row(31, r+85000, data_id, r, r, r+80000, 1_000_000_000_000+r+80000, 1, 1, 0, len(DATA), 2, 1, 1, 0, 0),
               row(51, r+90000, 7, data_id, len(DATA), r+85000),
               row(50, r+95000, 99, ack_id, len(ACK), r+94000, 7, 0),
               row(8, r+105000, 99, r+100000, len(ACK), 1)]
    base = {"probes": [dict(index=0, start_mono_ns=0, end_mono_ns=200_000_000,
                               start_schedule_s=0, duration_ms=200, complete=True)]}
    return dict(send=endpoint(send), receive=endpoint(receive)), base


def capture(epoch=1_000_000_000_000, extra=()):
    return m.Capture().read(pcap([(epoch+99_990_000, frame()),
                                 (epoch+100_120_000, frame(ACK, True, ports=(443, 1234))), *extra]))


class PacketTests(unittest.TestCase):
    def test_timestamp_formats_and_maximum_headers(self):
        packet = frame(tags=((0x88a8, 2), (0x8100, 1)), ihl=60)
        for endian in ("<", ">"):
            for nano in (False, True):
                value = 123_456_789_123 if nano else 123_456_789_000
                c = m.Capture().read(pcap([(value, packet)], endian, nano))
                result = next(iter(c.packets.values()))
                self.assertEqual(result["timestamp_ns"], value)
                self.assertEqual(result["payload_length"], len(DATA))
                self.assertEqual(result["identity"], m.identity(len(DATA), DATA))
                self.assertEqual(result["vlan_tags"], [2, 1])

    def test_short_datagram_and_identity_modes(self):
        packet = frame(b"abc")
        c = m.Capture().read(pcap([(0, packet)]))
        result = next(iter(c.packets.values()))
        self.assertEqual(result["payload_length"], 3)
        self.assertLess(result["identity"], 0)
        self.assertEqual(m.identity(0, b""), 0)
        self.assertGreater(m.identity(len(DATA), DATA), 0)
        for payload in (ACK, DATA):
            legacy = m.identity(len(payload), payload, "legacy") & ((1 << 64) - 1)
            v2 = m.identity(len(payload), payload) & ((1 << 64) - 1)
            self.assertEqual(legacy & ((1 << 63) - 1), v2 & ((1 << 63) - 1))

    def test_fragment_and_prefix_rejections(self):
        for flag in (0x2000, 1, 0x8000):
            c = m.Capture().read(pcap([(0, frame(fragment=flag))]))
            self.assertFalse(c.packets)
            self.assertEqual(sum(c.rejected.values()), 1)
        c = m.Capture().read(pcap([(0, frame())], snaplen=73))
        self.assertEqual(c.rejected["truncated_identity_prefix"], 1)
        packet = bytearray(frame())
        packet[38:40] = struct.pack("!H", len(DATA) + 7)
        c = m.Capture().read(pcap([(0, packet)]))
        self.assertEqual(c.rejected["malformed_udp_length"], 1)

    def test_malformed_record_is_fatal(self):
        raw = pcap([(0, frame())]).getvalue()
        for shortened in (raw[:12], raw[:30], raw[:-1]):
            with self.assertRaises(ValueError):
                m.Capture().read(io.BytesIO(shortened))
        raw = bytearray(raw)
        raw[32:36] = struct.pack("<I", 129)
        with self.assertRaises(ValueError):
            m.Capture().read(io.BytesIO(raw))

    def test_retained_subset_preserves_bytes_duplicates_and_analysis(self):
        wanted = {("receive", m.identity(len(DATA), DATA)), ("send", m.identity(len(ACK), ACK))}
        packets = [(1_000_099_990_000, frame()),
                   (1_000_100_120_000, frame(ACK, True, ports=(443, 1234))),
                   (1_000_100_121_000, frame(b"other valid unselected payload"))]
        for duplicate in (False, True):
            records = packets + ([packets[0]] if duplicate else [])
            for endian, nano in (("<", False), (">", True)):
                original = pcap(records, endian=endian, nano=nano)
                retained = io.BytesIO()
                full = m.Capture(wanted=wanted).read(original, "original", retained, "subset")
                expected = pcap(records[:2] + ([packets[0]] if duplicate else []), endian=endian, nano=nano)
                self.assertEqual(retained.getvalue(), expected.getvalue())
                retained.seek(0)
                subset = m.Capture(wanted=wanted).read(retained, "subset")
                self.assertEqual(full.duplicates, subset.duplicates)
                self.assertEqual(set(full.packets), set(subset.packets))
                self.assertEqual(full.counts["needed_packet_records"], subset.counts["needed_packet_records"])
                self.assertEqual(full.retained_outputs[0]["packet_records"], 3 if duplicate else 2)
                self.assertEqual(full.retained_outputs[0]["bytes"], len(retained.getvalue()))
                data, base = witness()
                full_result = m.analyze_rows(data, base, 1, full)
                subset_result = m.analyze_rows(data, base, 1, subset)
                for key in ("sample_counts", "aggregate", "exclusions"):
                    self.assertEqual(full_result[key], subset_result[key], key)
                self.assertEqual(full.counts["records"], len(records))
                self.assertEqual(full.counts["identity_not_needed"], 1)

    def test_duplicate_identity_has_no_winner(self):
        c = m.Capture().read(pcap([(0, frame()), (1000, frame()), (2000, frame())]))
        key = ("receive", m.identity(len(DATA), DATA))
        self.assertNotIn(key, c.packets)
        self.assertEqual(c.duplicates[key], 3)


class CycleTests(unittest.TestCase):
    def test_exact_partition_and_probe_summary(self):
        data, base = witness()
        result = m.analyze_rows(data, base, 1, capture())
        self.assertEqual(result["sample_counts"], dict(turnaround_cycles=1, partial_cycles=1, complete_cycles=1))
        c = result["correlated_cycles"][0]
        self.assertEqual(c["raw_rtt_ns"], 255000)
        self.assertEqual(c["receiver_interface_turnaround_ns"], 130000)
        self.assertEqual(c["sender_logical_send_to_software_tx_ns"], 20000)
        self.assertEqual(c["sender_kernel_rx_to_protocol_receive_ns"], 35000)
        self.assertEqual(c["residual_outside_receiver_interface_ns"], 70000)
        self.assertEqual(c["receiver_combined_pre_socket_and_post_write_ns"], 30000)
        self.assertEqual(c["receiver_interface_rx_to_socket_delivery_ns"], 10000)
        self.assertEqual(c["receiver_ack_write_start_to_interface_tx_ns"], 20000)
        self.assertEqual(result["probes"][0]["complete_cycles"]["residual_outside_receiver_interface_us"]["p50"], 70)

    def test_independent_endpoint_clock_shift_invariance(self):
        data, base = witness()
        first = m.analyze_rows(data, base, 1, capture())["correlated_cycles"][0]
        data, base = witness(linux_epoch=432_009_000_000_000)
        for kind, rows in data["receive"]["rows"].items():
            if kind == 31:
                changed = list(rows[0])
                changed[6] += 345_600_000_000_000
                rows[0] = tuple(changed)
        second = m.analyze_rows(data, base, 1, capture(epoch=346_600_000_000_000))["correlated_cycles"][0]
        for key in first:
            if key.endswith("_us"):
                self.assertEqual(first[key], second[key], key)

    def test_missing_tx_is_partial_without_estimation(self):
        data, base = witness()
        for kind in (32, 33):
            data["send"]["rows"][kind] = []
        result = m.analyze_rows(data, base, 1, capture())
        self.assertEqual(result["sample_counts"]["complete_cycles"], 0)
        self.assertEqual(result["sample_counts"]["partial_cycles"], 1)
        self.assertEqual(result["correlated_partial_cycles"][0]["remaining_including_sender_logical_send_to_tx_ns"], 90000)
        self.assertEqual(result["excluded_cycles"][0]["stage"], "complete")

    def test_no_mac_clock_calibration_or_socket_stamp_required(self):
        data, base = witness()
        data["receive"]["rows"][31] = []
        result = m.analyze_rows(data, base, 1, capture())
        self.assertEqual(result["sample_counts"]["complete_cycles"], 1)
        self.assertNotIn("receiver_combined_pre_socket_and_post_write_ns", result["correlated_cycles"][0])

    def test_capture_ambiguity_and_wrong_flow_exclude_cycle(self):
        data, base = witness()
        result = m.analyze_rows(data, base, 1, capture(extra=[(1_000_100_000_000, frame())]))
        self.assertEqual(result["sample_counts"]["turnaround_cycles"], 0)
        self.assertEqual(result["excluded_cycles"][0]["reason"], "ambiguous_bpf_identity")
        c = m.Capture().read(pcap([(0, frame()), (130000, frame(ACK, True, ports=(444, 1234)))]))
        result = m.analyze_rows(data, base, 1, c)
        self.assertEqual(result["excluded_cycles"][0]["reason"], "bpf_data_ack_flow_mismatch")

    def test_invalid_linux_timestamp_and_packet_ambiguity(self):
        for kind in (31, 33):
            data, base = witness()
            changed = list(data["send"]["rows"][kind][0])
            changed[14] = 1
            data["send"]["rows"][kind][0] = tuple(changed)
            result = m.analyze_rows(data, base, 1, capture())
            self.assertEqual(result["sample_counts"]["complete_cycles"], 0)
        data, base = witness()
        data["receive"]["rows"][51] *= 2
        result = m.analyze_rows(data, base, 1, capture())
        self.assertEqual(result["sample_counts"]["turnaround_cycles"], 0)

    def test_neighbor_clock_change_is_reported(self):
        data, base = witness()
        previous = list(data["receive"]["rows"][31][0])
        previous[2] += 1
        previous[5] -= 100000
        previous[6] -= 104000
        data["receive"]["rows"][31].append(tuple(previous))
        result = m.analyze_rows(data, base, 1, capture())
        cycle = result["correlated_cycles"][0]
        self.assertEqual(cycle["residual_outside_receiver_interface_us"], 70)
        self.assertEqual(cycle["receiver_wall_mapping"]["observed_neighbor_offset_span_ns"], 4000)

    def test_negative_residual_is_visible(self):
        data, base = witness()
        c = m.Capture().read(pcap([(0, frame()), (400000, frame(ACK, True, ports=(443, 1234)))]))
        result = m.analyze_rows(data, base, 1, c)
        self.assertEqual(result["aggregate"]["complete_cycles"]["residual_outside_receiver_interface_us"]["negative"], 1)


if __name__ == "__main__":
    unittest.main()
