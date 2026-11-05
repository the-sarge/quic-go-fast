"""Synthetic exact-join and context-boundary witnesses for issue #1210 NAPI."""
import copy
import importlib.util
from pathlib import Path
import sys
import unittest

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("napi_analysis", Path(__file__).with_name("analyze-napi.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def interface():
    rows = []
    for i, size in enumerate((1126, 37), 1):
        rows.append(dict(probe=i, largest_acked=10+i, ack_carrier=20+i, data_identity=100+i,
                         ack_identity=-100-i, bpf_data=dict(payload_length=size, timestamp_ns=8_000_000_000),
                         bpf_ack=dict(identity=-100-i, direction="send", payload_length=38,
                                      source_port=51781, dest_port=33773, timestamp_ns=8_000_100_000)))
    return dict(correlated_turnaround_cycles=rows, correlated_partial_cycles=rows,
                correlated_cycles=rows[:1])


def rx(identity=-101, gro=1_000_000, poll=980_000, irq=970_000, pointer=123, ports=(51781, 33773), length=38):
    return f"RX {gro} 3 {identity} {length} {pointer} {poll} {irq} {ports[0]} {ports[1]}"


def trace(rows=None, ready=True, end=True, selected=None, end_ns=2_000_000):
    if rows is None:
        rows = [rx(), rx(-102, gro=1_100_000, poll=1_080_000, irq=1_070_000)]
    lines = ["CONFIG local=33773 peer=51781 duration=180 identity=fnv1a63-small-v2 clock=monotonic"]
    if ready:
        lines.append(m.READY)
    lines.extend(rows)
    if end:
        lines.append(f"END monotonic_ns={end_ns}")
    lines.extend([f"@selected_udp: {len(rows) if selected is None else selected}",
                  "@poll_duration_ns:", "[16K, 32K) 4 |@@@@|", "@irq_calls: 12"])
    return m.Trace().read(lines)


class NapiTests(unittest.TestCase):
    def test_exact_join_intervals_cohorts_and_maps(self):
        result = m.analyze(interface(), trace())
        self.assertTrue(result["valid"])
        self.assertEqual(result["sample_counts"]["matched_ack_cycles"], 2)
        first = result["correlated_cycles"][0]
        self.assertEqual(first["preceding_irq_to_poll_ns"], 10000)
        self.assertEqual(first["poll_to_gro_ns"], 20000)
        self.assertEqual(first["preceding_irq_to_gro_ns"], 30000)
        self.assertTrue(first["interface_complete_cycle"])
        self.assertFalse(result["correlated_cycles"][1]["interface_complete_cycle"])
        self.assertEqual(result["payload_size_cohorts"]["1126"]["n"], 1)
        self.assertEqual(result["payload_size_cohorts"]["37"]["n"], 1)
        self.assertEqual(result["probes"][0]["aggregate"]["n"], 1)
        self.assertEqual(result["trace_quality"]["scalar_maps"]["@irq_calls"], [12])
        self.assertTrue(any(item["text"] == "[16K, 32K) 4 |@@@@|" for item in result["trace_quality"]["metadata_lines"]))
        self.assertFalse(any("residual" in key or "rtt" in key for key in first))

    def test_duplicate_identity_has_no_winner(self):
        result = m.analyze(interface(), trace([rx(), rx(gro=1_010_000), rx(-102)]))
        self.assertEqual(result["sample_counts"]["matched_ack_cycles"], 1)
        self.assertEqual(result["exclusions"]["duplicate_napi_identity"], 1)
        self.assertEqual(len(result["excluded_cycles"][0]["candidates"]), 2)
        self.assertEqual(result["trace_quality"]["duplicate_wanted_identities"][0]["identity"], -101)

    def test_reversed_ports_and_wrong_length_are_rejected(self):
        for bad in (rx(ports=(33773, 51781)), rx(length=39)):
            result = m.analyze(interface(), trace([bad, rx(-102)]))
            self.assertEqual(result["exclusions"]["napi_ack_length_or_ports_mismatch"], 1)
            self.assertEqual(result["sample_counts"]["matched_ack_cycles"], 1)

    def test_missing_context_is_not_replaced_by_zero_duration(self):
        result = m.analyze(interface(), trace([rx(pointer=0, poll=0, irq=0), rx(-102, irq=0)]))
        first, second = result["correlated_cycles"]
        self.assertEqual(first["napi"]["poll_entry_mono_ns"], 0)
        self.assertIn("missing_napi_pointer", first["context_flags"])
        self.assertNotIn("preceding_irq_to_poll_us", first)
        self.assertNotIn("poll_to_gro_us", first)
        self.assertEqual(second["poll_to_gro_us"], 20)
        self.assertEqual(result["aggregate"]["complete_context"], 0)
        self.assertEqual(result["aggregate"]["intervals"]["poll_to_gro_us"]["n"], 1)

    def test_negative_intervals_and_context_survive(self):
        result = m.analyze(interface(), trace([rx(poll=1_010_000, irq=1_020_000), rx(-102, pointer=-1)]))
        first, second = result["correlated_cycles"]
        self.assertEqual(first["preceding_irq_to_poll_us"], -10)
        self.assertEqual(first["poll_to_gro_us"], -10)
        self.assertEqual(first["preceding_irq_to_gro_us"], -20)
        self.assertIn("negative_napi_pointer", second["context_flags"])
        self.assertFalse(first["ordered_context"])
        self.assertEqual(result["aggregate"]["intervals"]["poll_to_gro_us"]["negative"], 1)

    def test_independent_timestamp_origins_do_not_change_intervals(self):
        original = m.analyze(interface(), trace())
        shifted_input = interface()
        for cycle in shifted_input["correlated_turnaround_cycles"]:
            cycle["bpf_data"]["timestamp_ns"] += 7_000_000_000_000_000
            cycle["bpf_ack"]["timestamp_ns"] += 7_000_000_000_000_000
        shift = 3_000_000_000_000_000
        shifted_trace = trace([rx(gro=shift+1_000_000, poll=shift+980_000, irq=shift+970_000),
                               rx(-102, gro=shift+1_100_000, poll=shift+1_080_000, irq=shift+1_070_000)], end_ns=shift+2_000_000)
        shifted = m.analyze(shifted_input, shifted_trace)
        self.assertEqual(original["aggregate"], shifted["aggregate"])
        self.assertEqual(original["payload_size_cohorts"], shifted["payload_size_cohorts"])

    def test_config_is_not_readiness_and_end_is_required(self):
        for witness in (trace(ready=False), trace(end=False)):
            result = m.analyze(interface(), witness)
            self.assertFalse(result["valid"])
            self.assertEqual(result["sample_counts"]["matched_ack_cycles"], 2)

    def test_warning_and_lost_event_metadata_are_retained(self):
        witness = trace()
        witness.read(["WARNING: Lost 4 events", "helper read failed: error -14"], "stderr")
        result = m.analyze(interface(), witness)
        self.assertFalse(result["valid"])
        self.assertEqual(len(result["trace_quality"]["warning_lines"]), 2)
        self.assertIn("WARNING: Lost 4 events", result["trace_quality"]["stderr_lines"])

    def test_selected_counter_mismatch_is_invalid(self):
        result = m.analyze(interface(), trace(selected=3))
        self.assertFalse(result["valid"])
        self.assertIn("selected_udp_count_mismatch", [item["reason"] for item in result["trace_quality"]["invalid_reasons"]])

    def test_malformed_record_is_not_silently_skipped(self):
        result = m.analyze(interface(), trace([rx(), "RX 1 broken"]))
        self.assertFalse(result["valid"])
        self.assertEqual(result["trace_quality"]["invalid_reasons"][0]["reason"], "malformed_rx")
        self.assertEqual(result["exclusions"]["missing_napi_identity"], 1)

    def test_duplicate_or_inconsistent_interface_identity_rejected(self):
        source = interface()
        source["correlated_turnaround_cycles"].append(copy.deepcopy(source["correlated_turnaround_cycles"][0]))
        result = m.analyze(source, trace())
        self.assertEqual(result["exclusions"]["duplicate_interface_ack_identity"], 2)
        source = interface()
        source["correlated_turnaround_cycles"][0]["bpf_ack"]["identity"] = -999
        result = m.analyze(source, trace())
        self.assertEqual(result["exclusions"]["inconsistent_interface_ack_metadata"], 1)


if __name__ == "__main__":
    unittest.main()
