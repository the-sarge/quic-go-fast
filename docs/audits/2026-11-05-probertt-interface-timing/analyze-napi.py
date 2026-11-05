"""Join issue #1210 NAPI observer rows to identified ACKs; never infer packet arrival.

All reported intervals use Linux monotonic timestamps within one RX row. The last
IRQ is context, not proof of the IRQ that caused the packet or its arrival time.
"""
import argparse
import collections
import gzip
import importlib.util
import json
from pathlib import Path
import re
import sys

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("q1210_interface", Path(__file__).with_name("analyze-interface.py"))
INTERFACE = importlib.util.module_from_spec(spec)
spec.loader.exec_module(INTERFACE)
READY = "__BPFTRACE_NOTIFY_PROBES_ATTACHED"
FIELDS = ("gro_mono_ns", "cpu", "identity", "payload_length", "napi_pointer",
          "poll_entry_mono_ns", "preceding_irq_mono_ns", "source_port", "dest_port")
CONFIG = re.compile(r"CONFIG local=(\d+) peer=(\d+) duration=(\d+) identity=(\S+) clock=(\S+)$")
END = re.compile(r"END monotonic_ns=(\d+)$")
SCALAR = re.compile(r"(@[^:]+):\s*(-?\d+)\s*$")


class Trace:
    def __init__(self, wanted=None):
        self.wanted = wanted
        self.rows = collections.defaultdict(list)
        self.counts = collections.Counter()
        self.configs, self.ends, self.metadata, self.warnings, self.invalid = [], [], [], [], []
        self.maps, self.stderr = {}, []
        self.ready_count = 0
        self.maximum_gro_ns = 0

    def warning(self, line, stream, number):
        if re.search(r"\b(?:warning|error|lost|loss|PERF_RECORD_LOST)\b", line, re.IGNORECASE):
            self.warnings.append(dict(stream=stream, line=number, text=line))

    def read(self, lines, stream="stdout"):
        for number, raw in enumerate(lines, 1):
            line = raw.rstrip("\r\n")
            self.warning(line, stream, number)
            if stream == "stderr":
                self.stderr.append(line)
                continue
            if line.startswith("RX "):
                self.counts["rx_lines"] += 1
                words = line.split()
                try:
                    if len(words) != 10:
                        raise ValueError("expected nine RX fields")
                    values = [int(word) for word in words[1:]]
                    row = dict(zip(FIELDS, values), line=number)
                    if not (-1 << 63) <= row["identity"] < (1 << 63) or row["identity"] == 0:
                        raise ValueError("invalid signed identity")
                    if row["gro_mono_ns"] <= 0 or row["cpu"] < 0 or row["payload_length"] <= 0:
                        raise ValueError("invalid GRO timestamp, CPU or length")
                    if any(not 0 <= row[field] <= 65535 for field in ("source_port", "dest_port")):
                        raise ValueError("invalid UDP port")
                except ValueError as error:
                    self.invalid.append(dict(reason="malformed_rx", line=number, text=line, detail=str(error)))
                    continue
                self.counts["parsed_rx_rows"] += 1
                self.maximum_gro_ns = max(self.maximum_gro_ns, row["gro_mono_ns"])
                if self.wanted is None or row["identity"] in self.wanted:
                    self.rows[row["identity"]].append(row)
                    self.counts["wanted_rx_rows"] += 1
                else:
                    self.counts["rx_identity_not_needed"] += 1
                continue
            self.metadata.append(dict(line=number, text=line))
            if line == READY:
                self.ready_count += 1
            elif found := CONFIG.fullmatch(line):
                local, peer, duration, identity, clock = found.groups()
                self.configs.append(dict(local_port=int(local), peer_port=int(peer), duration_seconds=int(duration),
                                         identity=identity, clock=clock))
            elif found := END.fullmatch(line):
                self.ends.append(int(found.group(1)))
            elif found := SCALAR.fullmatch(line):
                name, value = found.groups()
                self.maps.setdefault(name, []).append(int(value))
        return self

    def quality(self):
        invalid = list(self.invalid)
        for name, count in (("readiness", self.ready_count), ("config", len(self.configs)), ("end", len(self.ends))):
            if count != 1:
                invalid.append(dict(reason="expected_exactly_one_" + name, count=count))
        if len(self.configs) == 1 and (self.configs[0]["identity"] != "fnv1a63-small-v2" or self.configs[0]["clock"] != "monotonic"):
            invalid.append(dict(reason="unsupported_identity_or_clock"))
        if len(self.ends) == 1 and self.maximum_gro_ns > self.ends[0]:
            invalid.append(dict(reason="gro_timestamp_after_end", maximum_gro_mono_ns=self.maximum_gro_ns))
        if self.warnings:
            invalid.append(dict(reason="observer_warnings_require_review", count=len(self.warnings)))
        selected = self.maps.get("@selected_udp")
        if selected is not None and (len(selected) != 1 or selected[0] != self.counts["rx_lines"]):
            invalid.append(dict(reason="selected_udp_count_mismatch", map_values=selected, rx_lines=self.counts["rx_lines"]))
        return dict(valid=not invalid, invalid_reasons=invalid, configs=self.configs,
                    ready_markers=self.ready_count, end_monotonic_ns=self.ends,
                    counts=dict(self.counts), scalar_maps=self.maps,
                    metadata_lines=self.metadata, stderr_lines=self.stderr, warning_lines=self.warnings,
                    duplicate_wanted_identities=[dict(identity=identity, records=rows)
                                                 for identity, rows in sorted(self.rows.items()) if len(rows) != 1],
                    metadata_note="All non-RX stdout lines, including histograms and END/maps, are retained verbatim. Absence of a warning is not proof of zero probe overhead or complete hardware visibility.")


def cycle_key(cycle):
    return tuple(cycle[name] for name in ("probe", "largest_acked", "ack_carrier", "data_identity", "ack_identity"))


def summary(rows):
    flags = collections.Counter(flag for row in rows for flag in row["context_flags"])
    return dict(n=len(rows), complete_context=sum(row["complete_context"] for row in rows),
                ordered_context=sum(row["ordered_context"] for row in rows),
                context_flags=dict(flags), intervals=INTERFACE.KERNEL.grouped_stats(rows),
                cpu_counts=dict(collections.Counter(str(row["napi"]["cpu"]) for row in rows)))


def payload_cohorts(rows):
    return {str(size): summary([row for row in rows if row["data_payload_length"] == size])
            for size in sorted({row["data_payload_length"] for row in rows})}


def analyze(interface, trace):
    source = interface.get("correlated_turnaround_cycles", [])
    targets = collections.Counter(cycle["ack_identity"] for cycle in source)
    partial = {cycle_key(row) for row in interface.get("correlated_partial_cycles", [])}
    complete = {cycle_key(row) for row in interface.get("correlated_cycles", [])}
    rows, excluded = [], []
    for cycle in source:
        key = cycle_key(cycle)
        identity, packet = cycle["ack_identity"], cycle["bpf_ack"]
        base = {name: cycle[name] for name in ("probe", "largest_acked", "ack_carrier", "data_identity", "ack_identity")}
        base["data_payload_length"] = cycle["bpf_data"]["payload_length"]
        base["interface_partial_cycle"] = key in partial
        base["interface_complete_cycle"] = key in complete
        def reject(reason, **details):
            excluded.append(dict(base, reason=reason, **details))
        if targets[identity] != 1:
            reject("duplicate_interface_ack_identity")
            continue
        if packet["identity"] != identity or packet["direction"] != "send":
            reject("inconsistent_interface_ack_metadata")
            continue
        candidates = trace.rows.get(identity, [])
        if len(candidates) != 1:
            reject("missing_napi_identity" if not candidates else "duplicate_napi_identity", candidates=candidates)
            continue
        napi = candidates[0]
        if (napi["payload_length"], napi["source_port"], napi["dest_port"]) != (packet["payload_length"], packet["source_port"], packet["dest_port"]):
            reject("napi_ack_length_or_ports_mismatch", candidate=napi)
            continue
        flags, intervals = [], {}
        for field in ("napi_pointer", "poll_entry_mono_ns", "preceding_irq_mono_ns"):
            if napi[field] == 0:
                flags.append("missing_" + field)
            elif napi[field] < 0:
                flags.append("negative_" + field)
        for name, after, before in (("preceding_irq_to_poll", "poll_entry_mono_ns", "preceding_irq_mono_ns"),
                                    ("poll_to_gro", "gro_mono_ns", "poll_entry_mono_ns"),
                                    ("preceding_irq_to_gro", "gro_mono_ns", "preceding_irq_mono_ns")):
            if napi[after] > 0 and napi[before] > 0:
                value = napi[after] - napi[before]
                intervals[name + "_ns"] = value
                intervals[name + "_us"] = value / 1000
                if value < 0:
                    flags.append("negative_" + name)
        complete_context = all(napi[field] > 0 for field in ("napi_pointer", "poll_entry_mono_ns", "preceding_irq_mono_ns"))
        rows.append(dict(base, bpf_ack=packet, napi=napi, context_flags=flags,
                         complete_context=complete_context, ordered_context=complete_context and not flags, **intervals))
    quality = trace.quality()
    probes = sorted({cycle["probe"] for cycle in source})
    return dict(valid=quality["valid"], trace_quality=quality,
                sample_counts=dict(interface_turnaround_cycles=len(source), matched_ack_cycles=len(rows), excluded_cycles=len(excluded)),
                aggregate=summary(rows), payload_size_cohorts=payload_cohorts(rows),
                probes=[dict(index=probe, aggregate=summary([row for row in rows if row["probe"] == probe]),
                             payload_size_cohorts=payload_cohorts([row for row in rows if row["probe"] == probe])) for probe in probes],
                correlated_cycles=rows, excluded_cycles=excluded,
                exclusions=dict(collections.Counter(row["reason"] for row in excluded)),
                interpretation=dict(
                    preceding_irq="The most recent IRQ66 entry recorded when this NAPI poll began. It is not a packet-arrival timestamp or proven causal IRQ; one IRQ/poll can serve multiple packets and polls may run without a fresh IRQ.",
                    poll_to_gro="Elapsed time inside the recorded poll before this packet's napi_gro_receive_entry, when context exists. It is not pre-interrupt residence or wire delay.",
                    arithmetic="Intervals are differences between Linux monotonic fields in the same RX row. No cross-host timestamp subtraction, IRQ subtraction from RTT, or residual correction is performed.",
                    validity="Missing readiness/END/config, malformed rows, recognized observer warnings, or selected-counter mismatch mark the trace invalid. Matched rows remain inspectable; statistics from an invalid trace do not establish a complete or unbiased capture.",
                    missing_context="Zero or negative context values and negative intervals remain in exact rows with flags. Unavailable intervals are omitted rather than assigned zero.",
                    cohorts="Joins start from all interface turnaround cycles, not only complete Linux TX cycles. Payload-size cohorts describe the largest-acked outgoing carrier; 37-byte carriers are distinct from 1126-byte application-size carriers.",
                    selection="Only unique exact identity, payload length, and same-direction UDP port matches enter results. Duplicate identities choose no winner. These are selected ACK observations, not independent IRQs or representative latency quantiles."))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("interface_analysis", type=Path)
    parser.add_argument("stdout", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--stderr", type=Path)
    args = parser.parse_args()
    interface = json.loads(args.interface_analysis.read_text())
    wanted = {row["ack_identity"] for row in interface.get("correlated_turnaround_cycles", [])}
    trace = Trace(wanted)
    for stream, path in (("stdout", args.stdout), ("stderr", args.stderr)):
        if path is not None:
            opener = gzip.open if path.suffix == ".gz" else open
            with opener(path, "rt") as lines:
                trace.read(lines, stream)
    output = analyze(interface, trace)
    output["inputs"] = dict(interface_analysis=str(args.interface_analysis), stdout=str(args.stdout),
                            stderr=str(args.stderr) if args.stderr else None)
    args.output.write_text(json.dumps(output, indent=2) + "\n")
    print(json.dumps({name: output[name] for name in ("valid", "sample_counts", "aggregate", "payload_size_cohorts", "exclusions")}, indent=2))
    return 0 if output["valid"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
