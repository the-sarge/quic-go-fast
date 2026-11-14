#!/usr/bin/env python3
"""B5 aggregation benefit and shared-bottleneck cost: a missing or misaccounted case is a failed claim, not a product result."""

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import resource
import shutil
import signal
import statistics
import subprocess
import sys
import time

GO_VERSION = "go1.27.4"
ROUNDS = 3
WARMUP_S = 10
WINDOW_S = 60
SETUP_S = 5
DRAIN_S = 2
BASE_SEED = 1044
TOPOLOGIES = ("balanced", "unequal", "shared")
ARMS = ("A", "B", "C", "D", "E", "F")
ARM_CLAIMS = {
    "A": {"controller": "bbrv3", "multipath": False, "paths": "a"},
    "B": {"controller": "bbrv3", "multipath": False, "paths": "b"},
    "C": {"controller": "bbrv3", "multipath": False, "paths": "ab"},
    "D": {"controller": "reno", "multipath": True, "paths": "ab", "scheduler": "aggregate"},
    "E": {"controller": "bbrv3", "multipath": True, "paths": "ab", "scheduler": "aggregate"},
    "F": {"controller": "bbrv3", "multipath": True, "paths": "ab", "scheduler": "round-robin"},
}
FOCAL = ("focal-a", "focal-b")
COMPETITOR = "competitor"
# Accepted aggregation thresholds, section "Qualification and adoption policy" of the BBR plan.
GAIN_FLOOR = 1.20
INDEPENDENT_FLOOR = 0.95
COMPETITOR_FLOOR = 0.90
# Foreign CPU, as a share of one core, that flags an observation: R18's rule, decided before data.
FOREIGN_SHARE_LIMIT = 0.05
MAX_RERUNS = 3
QUIET_WAIT_LIMIT_S = 7200
RELAY_CPUS, SERVER_CPUS, CLIENT_CPUS = "4-5", "8-11", "12-15"
FIXTURE_CPUS = [4, 5] + list(range(8, 16))
FIXTURE_CPUS += [cpu + 16 for cpu in FIXTURE_CPUS]
HOST_BUSY_PATTERN = "[R]unner.Worker|[t]ransfer.test|qemu.*guest=[i]nfra-garm|[n]ativerig"
QUIET_BUSY_SHARE = 0.05
SERVER_ADDRESS = "127.0.0.1:47460"
FRONTS = {"front_a": "127.0.0.1:47461", "front_b": "127.0.0.1:47462", "front_competitor": "127.0.0.1:47463"}
METRIC_BOUNDARIES = {
    "useful_bytes": "receiver-counted in-order STREAM bytes read within the window, plus unique DATAGRAM payload bytes whose send time is within the window",
    "object_completion": "1 MiB STREAM object, sender's first header byte to the receiver's FIN, for objects finishing within the window",
    "datagram_useful_delivery": "unique DATAGRAMs offered within the window and received, over those offered",
    "competitor": "the ordinary Reno flow's receiver-counted STREAM bytes within the window",
}


def schedule():
    """The fixed (topology, round, arm, seed) instances. Each round rotates the arm order."""
    instances = []
    for index, topology in enumerate(TOPOLOGIES):
        for round_ in range(ROUNDS):
            seed = BASE_SEED + 100 * index + round_
            shift = 2 * round_
            order = ARMS[shift:] + ARMS[:shift]
            instances += [(topology, round_, arm, seed) for arm in order]
    return instances


def server_flows(record, labels):
    flows = record["server"]["result"]["flows"] or {}
    return [flows[label] for label in labels if label in flows]


def client_flows(record, labels):
    return [flow for flow in record["client"]["result"]["flows"] if flow["label"] in labels]


def link(record, name):
    links = [item for item in record["relay"]["result"]["links"] if item["name"] == name]
    if len(links) != 1:
        raise ValueError(f"{record['topology']}/{record['arm']}: relay link {name} missing")
    return links[0]


def check_observation(record, go_version, window_s=WINDOW_S):
    topology, arm = record["topology"], record["arm"]
    where = f"{topology}/{arm}"
    claim = ARM_CLAIMS[arm]
    for role in ("relay", "server", "client"):
        if record[role]["result"]["go"] != go_version:
            raise ValueError(f"{where}: {role} toolchain {record[role]['result']['go']}")
        if record[role]["spec"]["arm"] != arm or record[role]["spec"]["topology"] != topology:
            raise ValueError(f"{where}: {role} ran another case")
    server, client = record["server"]["result"], record["client"]["result"]
    if not server["granted"] or not server["joined"]:
        raise ValueError(f"{where}: server grant did not join")
    if server["stream_errors"]:
        raise ValueError(f"{where}: {server['stream_errors']} stream errors")
    competing = topology == "shared"
    names = ["a", "b"] + (["competitor"] if competing else [])
    if sorted(item["name"] for item in record["relay"]["result"]["links"]) != sorted(names):
        raise ValueError(f"{where}: relay links do not match the topology")
    for name in names:
        if link(record, name)["forward"]["overflow"] or link(record, name)["reverse"]["overflow"]:
            raise ValueError(f"{where}: relay backlog overflowed on {name}")
    used = {name: link(record, name)["forward"]["window_bytes"] > 0 for name in ("a", "b")}
    if used != {"a": "a" in claim["paths"], "b": "b" in claim["paths"]}:
        raise ValueError(f"{where}: relay path use {used} does not match the arm")
    focal_labels = ["focal-a", "focal-b"] if arm == "C" else ["focal-a"]
    labels = focal_labels + ([COMPETITOR] if competing else [])
    if sorted(flow["label"] for flow in client["flows"]) != sorted(labels):
        raise ValueError(f"{where}: client flows do not match the arm")
    for flow in client["flows"]:
        if flow["timeout_close"] or flow["close_cause"]:
            raise ValueError(f"{where}: {flow['label']} closed before the end: {flow['close_cause']}")
        expected = "reno" if flow["label"] == COMPETITOR else claim["controller"]
        if flow["controller"] != expected:
            raise ValueError(f"{where}: {flow['label']} ran {flow['controller']}")
        multipath = claim["multipath"] and flow["label"] != COMPETITOR
        if flow["multipath_negotiated"] != multipath:
            raise ValueError(f"{where}: {flow['label']} multipath negotiation does not match the arm")
        if multipath:
            if flow["scheduler"] != claim["scheduler"]:
                raise ValueError(f"{where}: scheduler {flow['scheduler']}")
            states = [(path["lifecycle"], path["local_use"]) for path in flow["paths"] or []]
            if states != [("live", "active"), ("live", "active")]:
                raise ValueError(f"{where}: paths {states} are not two live active paths")
        if flow["label"] != COMPETITOR and not flow["datagrams_offered"]:
            raise ValueError(f"{where}: {flow['label']} offered no DATAGRAMs")
    if any(item["timeout_close"] for item in server["connections"]):
        raise ValueError(f"{where}: a server connection timed out")
    flows = server["flows"] or {}
    if sorted(flows) != sorted(labels):
        raise ValueError(f"{where}: server flows {sorted(flows)} do not match the arm")
    for label, flow in flows.items():
        series = flow["stream_series"]
        if len(series) != window_s or any(type(value) is not int or value < 0 for value in series):
            raise ValueError(f"{where}: {label} has an invalid useful-byte series")
        if sum(series) != flow["stream_useful_bytes"]:
            raise ValueError(f"{where}: {label} useful bytes do not match its series")
        if flow["stream_useful_bytes"] <= 0:
            raise ValueError(f"{where}: {label} delivered no STREAM bytes")


def check_accounting(records, go_version):
    if any(record.get("mode") != "campaign" for record in records):
        raise ValueError("diagnostic/smoke records cannot satisfy campaign inventory")
    expected = sorted(schedule())
    actual = sorted((record["topology"], record["round"], record["arm"], record["seed"]) for record in records)
    if actual != expected:
        raise ValueError(f"{len(actual)} instances do not match the {len(expected)} scheduled")
    for record in records:
        check_observation(record, go_version)


def spread(values):
    return {"median": statistics.median(values), "range": [min(values), max(values)], "rounds": values}


def useful_bytes(record):
    return sum(flow["stream_useful_bytes"] + flow["datagram_useful_bytes"] for flow in server_flows(record, FOCAL))


def competitor_bytes(record):
    return server_flows(record, [COMPETITOR])[0]["stream_useful_bytes"]


def observation_metrics(record):
    """The per-observation values reported separately, never folded into a threshold."""
    focal = server_flows(record, FOCAL)
    offered = sum(flow["datagrams_offered"] for flow in client_flows(record, FOCAL))
    delivered = sum(flow["datagrams_useful"] for flow in focal)
    return {
        "useful_bytes": useful_bytes(record),
        "stream_bytes": sum(flow["stream_useful_bytes"] for flow in focal),
        "datagram_bytes": sum(flow["datagram_useful_bytes"] for flow in focal),
        "datagram_useful_delivery": delivered / offered,
        "datagram_duplicates": sum(flow["datagram_duplicates"] for flow in focal),
        "objects_completed": sum(flow["objects_completed"] for flow in focal),
        "object_completion_p50_ms": [flow["object_completion"]["p50_ms"] for flow in focal],
        "object_completion_p99_ms": [flow["object_completion"]["p99_ms"] for flow in focal],
        "datagram_latency_p99_ms": [flow["datagram_latency"]["p99_ms"] for flow in focal],
        "empty_seconds": sum(1 for flow in focal for value in flow["stream_series"] if value == 0),
        "path_window_bytes": {name: link(record, name)["forward"]["window_bytes"] for name in ("a", "b")},
        # Diagnostic only: the receiver's feedback bytes on each path, never a criterion.
        "path_reverse_window_bytes": {name: link(record, name)["reverse"]["window_bytes"] for name in ("a", "b")},
        "packets_lost": sum(flow["packets_lost"] for flow in client_flows(record, FOCAL)),
        "competitor_bytes": competitor_bytes(record) if record["topology"] == "shared" else None,
    }


def ratios(keyed, numerator, denominator, value):
    return [value(keyed[(numerator, round_)]) / value(keyed[(denominator, round_)]) for round_ in range(ROUNDS)]


def judge_topology(topology, keyed):
    """Judge one topology from its {(arm, round): record}, on medians of round-paired ratios."""
    metrics = {arm: [observation_metrics(keyed[(arm, round_)]) for round_ in range(ROUNDS)] for arm in ARMS}
    medians = {arm: statistics.median(m["useful_bytes"] for m in metrics[arm]) for arm in ARMS}
    # The better single path is chosen on measured medians before any ratio is formed.
    best = "A" if medians["A"] >= medians["B"] else "B"
    verdict = {"topology": topology, "metrics": metrics, "useful_bytes_median": medians, "better_single_path": best}
    verdict["informational"] = {
        arm: {"vs_better_single_path": spread(ratios(keyed, arm, best, useful_bytes)),
              "vs_independent": spread(ratios(keyed, arm, "C", useful_bytes))}
        for arm in ("D", "E", "F")
    }
    verdict["informational"]["E_vs_F"] = spread(ratios(keyed, "E", "F", useful_bytes))
    failures = []
    if topology == "balanced":
        gain = spread(ratios(keyed, "E", best, useful_bytes))
        independent = spread(ratios(keyed, "E", "C", useful_bytes))
        verdict.update(gain_over_better_single_path=gain, share_of_independent=independent)
        if gain["median"] < GAIN_FLOOR:
            failures.append(f"E gain over {best} {gain['median']:.3f} below {GAIN_FLOOR}")
        if independent["median"] < INDEPENDENT_FLOOR:
            failures.append(f"E share of C {independent['median']:.3f} below {INDEPENDENT_FLOOR}")
    elif topology == "shared":
        competitor = spread(ratios(keyed, "E", "C", competitor_bytes))
        verdict["competitor_share_beside_E_vs_C"] = competitor
        verdict["informational"]["competitor_beside"] = {
            arm: spread(ratios(keyed, arm, "C", competitor_bytes)) for arm in ("A", "B", "D", "F")}
        if competitor["median"] < COMPETITOR_FLOOR:
            failures.append(f"competitor beside E keeps {competitor['median']:.3f} of its goodput beside C, below {COMPETITOR_FLOOR}")
    verdict["thresholded"] = topology in ("balanced", "shared")
    verdict["passed"] = not failures
    verdict["failures"] = failures
    return verdict


def summarize(records, go_version):
    check_accounting(records, go_version)
    topologies = {}
    for topology in TOPOLOGIES:
        keyed = {(record["arm"], record["round"]): record for record in records if record["topology"] == topology}
        topologies[topology] = judge_topology(topology, keyed)
    failed = [name for name, verdict in topologies.items() if not verdict["passed"]]
    contaminated = sum(1 for record in records if record["contaminated"])
    if contaminated:
        raise ValueError(f"{contaminated} contaminated observations remain in the inventory")
    return {
        "support": "positive" if not failed else "negative",
        "failed_topologies": failed,
        "topologies": topologies,
        "contaminated_observations": contaminated,
    }


def foreign_limit_s(duration_s):
    return FOREIGN_SHARE_LIMIT * duration_s


def rerun_due(record, attempt, reruns_used):
    """One recorded rerun of a contaminated observation. The rule never sees a result."""
    return attempt == 0 and reruns_used < MAX_RERUNS and record["contaminated"]


SCHEDSTAT = Path("/proc/schedstat")


def schedstat_running_ns():
    running = {}
    if not SCHEDSTAT.exists():
        # Only a smoke runs without it; a campaign requires it as a prerequisite.
        return running
    for line in SCHEDSTAT.read_text().splitlines():
        if line.startswith("cpu"):
            parts = line.split()
            running[int(parts[0][3:])] = int(parts[7])
    return running


def fixture_cpus():
    present = schedstat_running_ns()
    return [cpu for cpu in FIXTURE_CPUS if cpu in present]


def softirqd_ns(cpus):
    ticks = 0
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            stat = (entry / "stat").read_text()
        except OSError:
            continue
        name = stat[stat.index("(") + 1:stat.rindex(")")]
        if name.startswith("ksoftirqd/") and int(name.split("/")[1]) in cpus:
            fields = stat[stat.rindex(")") + 2:].split()
            ticks += int(fields[11]) + int(fields[12])
    return ticks * 1e9 / os.sysconf("SC_CLK_TCK")


def fixture_busy_share(sample_s=2.0):
    cpus = fixture_cpus()
    if not cpus:
        return 0.0
    before = schedstat_running_ns()
    time.sleep(sample_s)
    after = schedstat_running_ns()
    return max((after[cpu] - before[cpu]) / (sample_s * 1e9) for cpu in cpus)


def wait_quiet(log):
    waited = 0.0
    while True:
        busy = subprocess.run(["pgrep", "-f", HOST_BUSY_PATTERN], capture_output=True, check=False).returncode == 0
        share = fixture_busy_share()
        # Background processes on other cores do not contaminate the fixture.
        if share < QUIET_BUSY_SHARE:
            return {"waited_s": waited, "fixture_busy_share": share, "named_process_present": busy}
        log(f"host busy: named process {busy}, fixture share {share:.3f}")
        time.sleep(13)
        waited += 15
        if waited > QUIET_WAIT_LIMIT_S:
            raise RuntimeError(f"host stayed busy for {waited:.0f} s")


def role_command(binary, timeout_s):
    return [binary, "-test.run=^TestAggregationRole$", "-test.count=1", "-test.v", f"-test.timeout={math.ceil(timeout_s)}s"]


def check_role_log(role, output):
    if output.count("--- PASS: TestAggregationRole") != 1:
        raise ValueError(f"{role} must pass exactly once:\n{output[-2000:]}")


def run_observation(binary, directory, topology, round_, arm, seed, go_version, warmup_s=WARMUP_S,
                    window_s=WINDOW_S, timeout_s=SETUP_S + WARMUP_S + WINDOW_S + 30, record=None):
    """One instance; every launched child is reaped, including a partial launch."""
    record = record if record is not None else {}
    directory.mkdir(parents=True, exist_ok=False)
    spec = dict(FRONTS, topology=topology, arm=arm, seed=seed, server=SERVER_ADDRESS,
                start_at_unix_ns=time.time_ns() + SETUP_S * 10**9,
                warmup_ns=warmup_s * 10**9, window_ns=window_s * 10**9)
    env = {key: value for key, value in os.environ.items() if not key.startswith("QUIC_GO_")}
    cpus = fixture_cpus()
    processes, usage = {}, {}
    before = schedstat_running_ns()
    softirq_before = softirqd_ns(cpus)
    begun = time.monotonic()
    deadline = begun + timeout_s

    def reap(role, process, timeout=None):
        # Only this script reaps children: the RUSAGE_CHILDREN delta belongs to this role.
        previous = resource.getrusage(resource.RUSAGE_CHILDREN)
        try:
            process.wait(timeout=timeout)
        finally:
            current = resource.getrusage(resource.RUSAGE_CHILDREN)
            usage[role] = usage.get(role, 0) + (current.ru_utime + current.ru_stime
                                               - previous.ru_utime - previous.ru_stime)

    try:
        for role, role_cpus, procs in (("relay", RELAY_CPUS, "2"), ("server", SERVER_CPUS, "4"), ("client", CLIENT_CPUS, "4")):
            role_spec = dict(spec, role=role, output=str(directory / f"{role}.json"))
            with (directory / f"{role}.log").open("w") as log:
                processes[role] = subprocess.Popen(
                    ["taskset", "-c", role_cpus, *role_command(binary, timeout_s)],
                    env=dict(env, GOMAXPROCS=procs, AGGREGATION_SPEC=json.dumps(role_spec)),
                    stdout=log, stderr=subprocess.STDOUT,
                )
        for role, process in processes.items():
            reap(role, process, timeout=max(0, deadline - time.monotonic()))
            if process.returncode != 0:
                raise ValueError(f"{topology}/{arm} {role} exit {process.returncode}")
    finally:
        # Terminate every role before waiting on any. No failed launch leaves a peer alive.
        for process in processes.values():
            if process.returncode is None:
                process.kill()
        for role, process in processes.items():
            if process.returncode is None:
                reap(role, process)
        occupied = time.monotonic() - begun
        record.update(topology=topology, round=round_, arm=arm, seed=seed,
                      occupied_s=occupied, process_cpu_s=usage,
                      exit={role: process.returncode for role, process in processes.items()})
    running = schedstat_running_ns()
    foreign = (sum(running[cpu] - before[cpu] for cpu in cpus)
               - (softirqd_ns(cpus) - softirq_before)) / 1e9 - sum(usage.values())
    if not cpus:
        foreign = None
    record.update(foreign_cpu_s=foreign, contaminated=foreign is not None and foreign > foreign_limit_s(occupied))
    for role in processes:
        check_role_log(role, (directory / f"{role}.log").read_text())

        def reject_nonfinite(value):
            raise ValueError(f"{role}: nonfinite JSON number {value}")
        record[role] = json.loads((directory / f"{role}.json").read_text(), parse_constant=reject_nonfinite)
    check_observation(record, go_version, window_s=window_s)
    return record


class AttemptLedger:
    """The single owner of supplied capacity and durable, pre-launch attempt charges."""

    def __init__(self, output, remaining_instances, remaining_host_seconds, timeout_s):
        if (remaining_instances < 0 or not math.isfinite(remaining_host_seconds)
                or remaining_host_seconds < 0 or not math.isfinite(timeout_s) or timeout_s <= 0):
            raise ValueError("invalid remaining allowance or timeout")
        self.path = output / "ledger.json"
        self.remaining_instances = remaining_instances
        self.remaining_host_seconds = remaining_host_seconds
        self.timeout_s = timeout_s
        self.attempts = []
        self.save()

    def snapshot(self):
        return {
            "instances_charged": len(self.attempts),
            "host_seconds_charged": sum(record["occupied_s"] for record in self.attempts),
            "remaining_instances_supplied": self.remaining_instances,
            "remaining_host_seconds_supplied": self.remaining_host_seconds,
            "per_invocation_timeout_s": self.timeout_s,
            "attempts": self.attempts,
        }

    def save(self):
        temporary = self.path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.snapshot(), indent=2, allow_nan=False) + "\n")
        temporary.replace(self.path)

    def require_capacity(self, instances=1):
        state = self.snapshot()
        if (state["instances_charged"] + instances > self.remaining_instances
                or state["host_seconds_charged"] + instances * self.timeout_s > self.remaining_host_seconds):
            raise ValueError("insufficient remaining instance/host-time allowance")

    def observe(self, binary, directory, topology, round_, arm, seed, candidate, mode, go_version,
                attempt=0, warmup_s=WARMUP_S, window_s=WINDOW_S):
        self.require_capacity()
        record = dict(topology=topology, round=round_, arm=arm, seed=seed, candidate=candidate,
                      mode=mode, attempt=attempt, status="started", occupied_s=0,
                      started_at_unix_ns=time.time_ns())
        self.attempts.append(record)
        self.save()  # A charge exists before any role can start.
        begun = time.monotonic()
        try:
            run_observation(binary, directory, topology, round_, arm, seed, go_version,
                            warmup_s=warmup_s, window_s=window_s, timeout_s=self.timeout_s, record=record)
            record["status"] = "succeeded"
            return record
        except BaseException as error:
            record.update(status="failed", error=f"{type(error).__name__}: {error}")
            raise
        finally:
            record["occupied_s"] = time.monotonic() - begun
            self.save()


def check_prerequisites(system, machine, which, campaign=True):
    if system != "Linux" or machine != "x86_64":
        raise RuntimeError(f"needs Linux amd64, not {system} {machine}")
    if campaign and not SCHEDSTAT.exists():
        raise RuntimeError("contamination accounting needs /proc/schedstat")
    if not which("taskset"):
        raise RuntimeError("taskset is required")


def run_campaign(binary, output, candidate, go_version, log, ledger):
    """Every fixed case once. A failed attempt stops the campaign; only contamination reruns."""
    ledger.require_capacity(len(schedule()))
    results = output / "observations.jsonl"
    superseded = output / "superseded.jsonl"
    records, reruns = [], 0
    for topology, round_, arm, seed in schedule():
        attempt = 0
        while True:
            quiet = wait_quiet(log)
            load_before = os.getloadavg()[0]
            record = ledger.observe(binary, output / topology / f"round{round_}" / f"attempt{attempt}" / arm,
                                    topology, round_, arm, seed, candidate, "campaign", go_version, attempt=attempt)
            record.update(quiet=quiet, load_before=load_before, load_after=os.getloadavg()[0])
            log(f"{topology} round {round_} {arm} attempt {attempt}: foreign {record['foreign_cpu_s']} s, contaminated {record['contaminated']}")
            if not rerun_due(record, attempt, reruns):
                break
            record["rerun_cause"] = "foreign CPU over the limit"
            with superseded.open("a") as file:
                file.write(json.dumps(record) + "\n")
            ledger.save()
            ledger.require_capacity()
            reruns += 1
            attempt += 1
        records.append(record)
        with results.open("a") as file:
            file.write(json.dumps(record) + "\n")
        if record["contaminated"]:
            raise ValueError("contaminated observation has no admitted replacement")
    return records, reruns


def binary_identity(binary):
    path = Path(binary).resolve(strict=True)
    build = subprocess.run(["go", "version", "-m", str(path)], capture_output=True, text=True, check=True).stdout
    first = build.splitlines()[0] if build.splitlines() else ""
    return {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "go": first.rsplit(": ", 1)[-1], "build_info": build}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", required=True, help="the exact 40-character product commit under measurement")
    parser.add_argument("--binary", required=True, help="the test binary built with -tags aggregation")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--remaining-instances", required=True, type=int)
    parser.add_argument("--remaining-host-seconds", required=True, type=float)
    parser.add_argument("--timeout-seconds", required=True, type=float, help="maximum reserved duration per observation, including setup/cleanup")
    parser.add_argument("--smoke-topology", choices=TOPOLOGIES, help="one short observation per arm; never counted campaign evidence")
    parser.add_argument("--smoke-window", type=int, default=5, help="smoke window seconds")
    args = parser.parse_args(argv)
    check_prerequisites(platform.system(), platform.machine(), shutil.which, campaign=not args.smoke_topology)
    if len(args.candidate) != 40 or any(c not in "0123456789abcdef" for c in args.candidate):
        parser.error("--candidate must be a full lowercase commit SHA")
    binary = binary_identity(args.binary)
    mode = "smoke" if args.smoke_topology else "campaign"
    if mode == "campaign" and binary["go"] != GO_VERSION:
        parser.error(f"a campaign binary must be built with {GO_VERSION}, not {binary['go']}")
    args.output.mkdir(parents=True, exist_ok=False)
    warmup, window = (2, args.smoke_window) if mode == "smoke" else (WARMUP_S, WINDOW_S)
    identity = {
        "candidate": args.candidate, "binary": binary, "kernel": platform.platform(), "uname": " ".join(platform.uname()),
        "go": binary["go"], "relay_cpus": RELAY_CPUS, "server_cpus": SERVER_CPUS, "client_cpus": CLIENT_CPUS,
        "fixture_cpus_and_siblings": FIXTURE_CPUS, "server": SERVER_ADDRESS, "fronts": FRONTS,
        "warmup_s": warmup, "window_s": window, "base_seed": BASE_SEED, "mode": mode,
        "remaining_instances": args.remaining_instances, "remaining_host_seconds": args.remaining_host_seconds,
        "per_invocation_timeout_s": args.timeout_seconds, "metric_boundaries": METRIC_BOUNDARIES,
        "thresholds": {"gain_floor": GAIN_FLOOR, "independent_floor": INDEPENDENT_FLOOR, "competitor_floor": COMPETITOR_FLOOR},
    }
    (args.output / "identity.json").write_text(json.dumps(identity, indent=2) + "\n")

    def log(message):
        line = f"{time.strftime('%H:%M:%S')} {message}"
        print(line, flush=True)
        with (args.output / "campaign.log").open("a") as file:
            file.write(line + "\n")

    def interrupt_on_sigterm(signum, frame):
        # Use the same unwind path as an operator interrupt: reap roles, then persist charges.
        raise KeyboardInterrupt("SIGTERM")

    previous_sigterm = signal.signal(signal.SIGTERM, interrupt_on_sigterm)
    ledger = None
    summary = {"support": "invalid", "mode": mode, "campaign_complete": False, "metric_boundaries": METRIC_BOUNDARIES}
    try:
        ledger = AttemptLedger(args.output, args.remaining_instances, args.remaining_host_seconds, args.timeout_seconds)
        if args.timeout_seconds < SETUP_S + warmup + window + DRAIN_S + 10:
            raise ValueError("timeout does not cover setup, warmup, window, drain and fixture cleanup")
        if mode == "campaign":
            records, reruns = run_campaign(binary["path"], args.output, args.candidate, binary["go"], log, ledger)
            summary.update(summarize(records, binary["go"]), reruns=reruns, campaign_complete=True)
        else:
            ledger.require_capacity(len(ARMS))
            for arm in ARMS:
                quiet = wait_quiet(log)
                record = ledger.observe(binary["path"], args.output / args.smoke_topology / arm, args.smoke_topology, 0,
                                        arm, BASE_SEED, args.candidate, mode, binary["go"], warmup_s=warmup, window_s=window)
                record["quiet"] = quiet
                record["metrics"] = observation_metrics(record)
                log(f"smoke {args.smoke_topology} {arm}: useful {record['metrics']['useful_bytes']} B, contaminated {record['contaminated']}")
                with (args.output / "smoke.jsonl").open("a") as file:
                    file.write(json.dumps(record) + "\n")
            summary.update(support="not-evidence", smoke_complete=True)
    except (Exception, KeyboardInterrupt) as error:
        summary.update(error=f"{type(error).__name__}: {error}")
    finally:
        signal.signal(signal.SIGTERM, previous_sigterm)
        if ledger:
            ledger.save()
            summary.update(ledger.snapshot())
        (args.output / "summary.json").write_text(json.dumps(summary, indent=2, allow_nan=False) + "\n")
    log(f"done: mode {mode}, support {summary['support']}, charged {summary.get('instances_charged', 0)}")
    return 0 if summary.get("campaign_complete") or summary.get("smoke_complete") else 1


if __name__ == "__main__":
    sys.exit(main())
