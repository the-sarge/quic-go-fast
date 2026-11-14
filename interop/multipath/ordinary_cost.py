#!/usr/bin/env python3
"""R18 ordinary-peer cost: a missing or misaccounted case is a failed claim, not a product result."""

import argparse
import json
import hashlib
import math
import resource
import signal
import sys
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import time

GO_VERSION = "go1.27.4"
ROUNDS = 3
WARMUP_S = 10
WINDOW_S = 60
SETUP_S = 5
BASE_SEED = 1038
DISABLE_GSO = {"QUIC_GO_DISABLE_GSO": "true"}
CELLS = {
    "stream-gso-on": {"env": {}},
    "stream-gso-off": {"env": DISABLE_GSO},
    "datagram-batch-on": {"env": {}},
    "datagram-batch-off": {"env": DISABLE_GSO},
    "mixed-bulk": {"env": {}},
    "stateless-pressure": {"env": {}},
}
CONTROL, OPTED_IN = "unregistered", "opted-in"
# B5's BBR primary/backup revalidation reruns two cells with every connection on BBR (#1044).
CONTROLLERS = ("reno", "bbrv3")
BBR_CELLS = ("mixed-bulk", "stateless-pressure")
# Accepted product thresholds, section "Accepted product thresholds" of the Reno plan.
GOODPUT_FLOOR = 0.95
CPU_CEILING = 1.10
LATENCY_FACTOR = 1.10
LATENCY_ALLOWANCE_US = 1000.0
# "Bounded memory" operationalized before data: the high-water mark may not keep climbing.
MEMORY_GROWTH_CEILING = 1.10
# Foreign CPU, as a share of one core, that flags an observation. Decided before data
# from the #742 rule of 0.3 CPU-seconds per 10 s, at a looser share for a longer run.
FOREIGN_SHARE_LIMIT = 0.05
MAX_RERUN_PAIRS = 3
QUIET_WAIT_LIMIT_S = 7200
SERVER_CPUS, CLIENT_CPUS = "8-11", "12-15"
FIXTURE_CPUS = list(range(8, 16)) + list(range(24, 32))
HOST_BUSY_PATTERN = "[R]unner.Worker|[t]ransfer.test|qemu.*guest=[i]nfra-garm|[n]ativerig"
QUIET_BUSY_SHARE = 0.05
SERVER_ADDRESS = "127.0.0.1:47438"
METRIC_BOUNDARIES = {
    "application_send_call": "elapsed Stream.Write / SendDatagram call time; neither native return nor peer delivery",
    "control": "application echo round trip",
    "useful_bytes": "receiver-counted application content within the measurement window",
}


def schedule_cells(controller="reno", cells=None):
    """The campaign's cells: the controller's inventory, or the named subset of it that an
    affected-cell revalidation reruns."""
    inventory = list(CELLS) if controller == "reno" else list(BBR_CELLS)
    if cells is None:
        return inventory
    if not cells or any(cell not in inventory for cell in cells):
        raise ValueError(f"revalidation cells {cells} are not in the {controller} inventory")
    return [cell for cell in inventory if cell in cells]


def schedule(controller="reno", cells=None):
    """The fixed (cell, round, profile, seed) instances. A pair shares its seed, and a cell
    keeps its seeds under either controller."""
    instances = []
    for index, cell in enumerate(CELLS):
        if cell not in schedule_cells(controller, cells):
            continue
        for round_ in range(ROUNDS):
            order = (CONTROL, OPTED_IN) if round_ % 2 == 0 else (OPTED_IN, CONTROL)
            seed = BASE_SEED + 100 * index + round_
            instances += [(cell, round_, profile, seed) for profile in order]
    return instances


def flows(record, label):
    return [flow for flow in record["client"]["flows"] if flow["label"] == label]


def check_observation(record):
    cell, profile = record["cell"], record["profile"]
    server, client = record["server"], record["client"]
    if server["application_send_call_boundary"] != METRIC_BOUNDARIES["application_send_call"]:
        raise ValueError(f"{cell}: application send-call metric boundary mismatch")
    if client["go"] != GO_VERSION:
        raise ValueError(f"{cell}: client toolchain {client['go']}")
    if server["go"] != GO_VERSION:
        raise ValueError(f"{cell}: toolchain {server['go']}")
    if server["granted"] != (profile == OPTED_IN):
        raise ValueError(f"{cell}/{profile}: grant does not match the profile")
    if profile == OPTED_IN and not server["joined"]:
        raise ValueError(f"{cell}: write authority did not join")
    if server["gso_capable"] == bool(CELLS[cell]["env"]):
        raise ValueError(f"{cell}: acceleration does not match the cell")
    if profile not in (CONTROL, OPTED_IN):
        raise ValueError(f"unknown profile {profile}")
    closes = client["flows"] + server["connections"]
    if profile == CONTROL and (server["write_errors"] or any(
            item["timeout_close"] or item["close_cause"] for item in closes)):
        raise ValueError(f"{cell}: failed control observation is invalid comparator evidence")
    for field in ("window_cpu_s", "rss_kb_at_20s", "rss_kb_end"):
        if not math.isfinite(server[field]) or server[field] <= 0:
            raise ValueError(f"{cell}: invalid {field}")
    for flow in client["flows"]:
        if len(flow["series"]) != WINDOW_S:
            raise ValueError(f"{cell}: {flow['label']} has {len(flow['series'])} seconds")
        if any(type(value) is not int or value < 0 for value in flow["series"]):
            raise ValueError(f"{cell}: invalid useful-byte series")
        if sum(flow["series"]) != flow["useful_bytes"]:
            raise ValueError(f"{cell}: {flow['label']} useful bytes do not match its series")
    ordinary = flows(record, "ordinary")
    if len(ordinary) != 1:
        raise ValueError(f"{cell}: {len(ordinary)} ordinary flows")
    if not ordinary[0]["control"] or not ordinary[0]["control"]["count"]:
        raise ValueError(f"{cell}: no control samples")
    if not server["application_send_call"].get("ordinary", {}).get("count"):
        raise ValueError(f"{cell}: no application send-call samples")
    for metric in (ordinary[0]["control"], server["application_send_call"]["ordinary"]):
        if not math.isfinite(metric["p99_us"]) or metric["p99_us"] < 0:
            raise ValueError(f"{cell}: invalid latency")
    if ordinary[0]["useful_bytes"] <= 0:
        raise ValueError(f"{cell}: no useful delivery")
    if cell == "mixed-bulk":
        check_mixed(record)
    elif len(client["flows"]) != 1:
        raise ValueError(f"{cell}: unexpected background flow")
    if cell == "stateless-pressure" and not client["flood"]["sent"]:
        raise ValueError(f"{cell}: no pressure applied")
    controller = record.get("controller", "reno")
    for item in client["flows"] + server["connections"]:
        if item.get("controller", "reno") != controller:
            raise ValueError(f"{cell}: {item['label']} ran {item.get('controller', 'reno')}, not {controller}")


def check_mixed(record):
    background = flows(record, "background")
    if record["profile"] == CONTROL:
        if len(background) != 2:
            raise ValueError("mixed-bulk control needs two independent connections")
        return
    if len(background) != 1 or not background[0].get("multipath_negotiated") or background[0].get("paths") != ["live", "live"]:
        raise ValueError("mixed-bulk opted-in needs one negotiated two-path connection")


def check_accounting(records, controller="reno", cells=None):
    if any(record.get("mode") != "campaign" for record in records):
        raise ValueError("diagnostic/smoke records cannot satisfy campaign inventory")
    if any(record.get("controller", "reno") != controller for record in records):
        raise ValueError(f"records do not all run {controller}")
    expected = sorted(schedule(controller, cells))
    actual = sorted((record["cell"], record["round"], record["profile"], record["seed"]) for record in records)
    if actual != expected:
        raise ValueError(f"{len(actual)} instances do not match the {len(expected)} scheduled")
    for record in records:
        check_observation(record)


def spread(values):
    return {"median": statistics.median(values), "range": [min(values), max(values)], "rounds": values}


def useful_bytes(record):
    return sum(flow["useful_bytes"] for flow in record["client"]["flows"])


def latency_margin(base, treated):
    """Microseconds above the limit: the larger of 110% of baseline or baseline plus 1 ms."""
    return treated - max(LATENCY_FACTOR * base, base + LATENCY_ALLOWANCE_US)


def judge_cell(cell, rounds):
    """Judge one cell from its (control, opted-in) rounds, on medians of paired values."""
    goodput, cpu, control_margin, send_margin = [], [], [], []
    control_p99, send_p99 = {CONTROL: [], OPTED_IN: []}, {CONTROL: [], OPTED_IN: []}
    empty = {CONTROL: [], OPTED_IN: []}
    background = {CONTROL: [], OPTED_IN: []}
    resets = {CONTROL: [], OPTED_IN: []}
    offered = {CONTROL: [], OPTED_IN: []}
    returned = {CONTROL: [], OPTED_IN: []}
    peak = {CONTROL: [], OPTED_IN: []}
    lost = {CONTROL: [], OPTED_IN: []}
    drops = {CONTROL: [], OPTED_IN: []}
    write_errors = {CONTROL: [], OPTED_IN: []}
    timeouts, failed_sends, unbounded, starved = [], [], [], []
    for round_, (base, treated) in enumerate(rounds):
        base_flow, treated_flow = flows(base, "ordinary")[0], flows(treated, "ordinary")[0]
        goodput.append(treated_flow["useful_bytes"] / base_flow["useful_bytes"])
        cpu.append((treated["server"]["window_cpu_s"] / useful_bytes(treated)) / (base["server"]["window_cpu_s"] / useful_bytes(base)))
        base_control = base_flow["control"]["p99_us"]
        treated_control = treated_flow["control"]["p99_us"]
        control_margin.append(latency_margin(base_control, treated_control))
        base_send = base["server"]["application_send_call"]["ordinary"]["p99_us"]
        treated_send = treated["server"]["application_send_call"]["ordinary"]["p99_us"]
        send_margin.append(latency_margin(base_send, treated_send))
        for name, record in ((CONTROL, base), (OPTED_IN, treated)):
            flow = flows(record, "ordinary")[0]
            control_p99[name].append(flow["control"]["p99_us"])
            send_p99[name].append(record["server"]["application_send_call"]["ordinary"]["p99_us"])
            empty[name].append(sum(1 for value in flow["series"] if value == 0))
            background[name].append(sum(f["useful_bytes"] for f in flows(record, "background")))
            peak[name].append(record["server"]["rss_kb_end"])
            lost[name].append(sum(c.get("packets_lost", 0) for c in record["server"]["connections"]))
            drops[name].append(sum(f.get("datagram_drops", 0) for f in record["client"]["flows"]))
            write_errors[name].append(record["server"]["write_errors"])
            flood = record["client"].get("flood")
            offered[name].append(flood["sent"] if flood else 0)
            returned[name].append(flood["resets"] if flood else 0)
            if flood and flood["sent"]:
                resets[name].append(flood["resets"] / flood["sent"])
        closes = treated["client"]["flows"] + treated["server"]["connections"]
        if any(item["timeout_close"] for item in closes):
            timeouts.append(round_)
        if treated["server"]["write_errors"] or any(item["close_cause"] and not item["timeout_close"] for item in closes):
            failed_sends.append(round_)
        if treated["server"]["rss_kb_end"] > MEMORY_GROWTH_CEILING * treated["server"]["rss_kb_at_20s"]:
            unbounded.append(round_)
        if any(value == 0 for flow in treated["client"]["flows"] for value in flow["series"]):
            starved.append(round_)
    failures = []
    if statistics.median(goodput) < GOODPUT_FLOOR:
        failures.append(f"goodput ratio {statistics.median(goodput):.3f} below {GOODPUT_FLOOR}")
    if statistics.median(cpu) > CPU_CEILING:
        failures.append(f"CPU per useful byte ratio {statistics.median(cpu):.3f} above {CPU_CEILING}")
    if statistics.median(control_margin) > 0:
        failures.append(f"control p99 {statistics.median(control_margin):.0f} us over its limit")
    if statistics.median(send_margin) > 0:
        failures.append(f"application send-call p99 {statistics.median(send_margin):.0f} us over its limit")
    if timeouts:
        failures.append(f"timeout-only close in rounds {timeouts}")
    if failed_sends:
        failures.append(f"write error or non-timeout close in rounds {failed_sends}")
    if unbounded:
        failures.append(f"memory kept growing in rounds {unbounded}")
    if starved:
        failures.append(f"fair progress: an empty second in rounds {starved}")
    return {
        "cell": cell,
        "passed": not failures,
        "failures": failures,
        "goodput_ratio": spread(goodput),
        "cpu_per_useful_byte_ratio": spread(cpu),
        "control_p99_margin_us": spread(control_margin),
        "application_send_call_p99_margin_us": spread(send_margin),
        "control_p99_us": control_p99,
        "application_send_call_p99_us": send_p99,
        "empty_seconds": empty,
        "background_bytes": background,
        "stateless_reset_share": resets,
        "stateless_offered_packets": offered,
        "stateless_resets_received": returned,
        "peak_rss_kb": peak,
        "packets_lost": lost,
        "datagram_drops": drops,
        "write_errors": write_errors,
    }


def summarize(records, controller="reno", cells=None):
    check_accounting(records, controller, cells)
    keyed = {(record["cell"], record["round"], record["profile"]): record for record in records}
    verdicts = {}
    for cell in schedule_cells(controller, cells):
        rounds = [(keyed[(cell, round_, CONTROL)], keyed[(cell, round_, OPTED_IN)]) for round_ in range(ROUNDS)]
        verdicts[cell] = judge_cell(cell, rounds)
    failed = [cell for cell, verdict in verdicts.items() if not verdict["passed"]]
    contaminated = sum(1 for record in records if record["contaminated"])
    return {
        "passed": not failed and not contaminated,
        "failed_cells": failed,
        "cells": verdicts,
        "contaminated_observations": contaminated,
    }


def foreign_limit_s(duration_s):
    return FOREIGN_SHARE_LIMIT * duration_s


def rerun_due(pair, attempt, reruns_used):
    """One recorded rerun of a contaminated pair. The rule never sees a result."""
    return attempt == 0 and reruns_used < MAX_RERUN_PAIRS and any(record["contaminated"] for record in pair)


def schedstat_running_ns():
    running = {}
    for line in Path("/proc/schedstat").read_text().splitlines():
        if line.startswith("cpu"):
            parts = line.split()
            running[int(parts[0][3:])] = int(parts[7])
    return running


def softirqd_ns():
    ticks = 0
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            stat = (entry / "stat").read_text()
        except OSError:
            continue
        name = stat[stat.index("(") + 1:stat.rindex(")")]
        if name.startswith("ksoftirqd/") and int(name.split("/")[1]) in FIXTURE_CPUS:
            fields = stat[stat.rindex(")") + 2:].split()
            ticks += int(fields[11]) + int(fields[12])
    return ticks * 1e9 / os.sysconf("SC_CLK_TCK")


def fixture_busy_share(sample_s=2.0):
    before = schedstat_running_ns()
    time.sleep(sample_s)
    after = schedstat_running_ns()
    return max((after[cpu] - before[cpu]) / (sample_s * 1e9) for cpu in FIXTURE_CPUS)


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


def role_command(binary):
    return [binary, "-test.run=^TestOrdinaryCostRole$", "-test.count=1", "-test.v", f"-test.timeout={SETUP_S + WARMUP_S + WINDOW_S + 60}s"]


def check_role_log(role, output):
    if output.count("--- PASS: TestOrdinaryCostRole") != 1:
        raise ValueError(f"{role} must pass exactly once:\n{output[-2000:]}")


def run_observation(binary, directory, cell, round_, profile, seed, warmup_s=WARMUP_S,
                    window_s=WINDOW_S, timeout_s=135, record=None, controller="reno"):
    """One instance; every launched child is reaped, including a partial launch."""
    record = record if record is not None else {}
    directory.mkdir(parents=True, exist_ok=False)
    spec = {
        "cell": cell, "profile": profile, "seed": seed, "address": SERVER_ADDRESS,
        "start_at_unix_ns": time.time_ns() + SETUP_S * 10**9,
        "warmup_ns": warmup_s * 10**9, "window_ns": window_s * 10**9, "controller": controller,
    }
    env = {key: value for key, value in os.environ.items() if not key.startswith("QUIC_GO_")}
    env["GOMAXPROCS"] = "4"
    processes, usage = {}, {}
    before = schedstat_running_ns()
    softirq_before = softirqd_ns()
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
        for role, cpus, process_env in (("server", SERVER_CPUS, dict(env, **CELLS[cell]["env"])),
                                        ("client", CLIENT_CPUS, env)):
            role_spec = dict(spec, role=role, output=str(directory / f"{role}.json"))
            with (directory / f"{role}.log").open("w") as log:
                processes[role] = subprocess.Popen(
                    ["taskset", "-c", cpus, *role_command(binary)],
                    env=dict(process_env, ORDINARY_COST_SPEC=json.dumps(role_spec)),
                    stdout=log, stderr=subprocess.STDOUT,
                )
        for role, process in processes.items():
            reap(role, process, timeout=max(0, deadline - time.monotonic()))
            if process.returncode != 0:
                raise ValueError(f"{cell}/{profile} {role} exit {process.returncode}")
    finally:
        # Terminate both roles before waiting on either. No failed launch leaves a peer alive.
        for process in processes.values():
            if process.returncode is None:
                process.kill()
        for role, process in processes.items():
            if process.returncode is None:
                reap(role, process)
        occupied = time.monotonic() - begun
        record.update(cell=cell, round=round_, profile=profile, seed=seed, controller=controller,
                      occupied_s=occupied, process_cpu_s=usage,
                      exit={role: process.returncode for role, process in processes.items()})
    running = schedstat_running_ns()
    foreign = (sum(running[cpu] - before[cpu] for cpu in FIXTURE_CPUS)
               - (softirqd_ns() - softirq_before)) / 1e9 - sum(usage.values())
    record.update(foreign_cpu_s=foreign, contaminated=foreign > foreign_limit_s(occupied))
    for role in processes:
        check_role_log(role, (directory / f"{role}.log").read_text())
        def reject_nonfinite(value):
            raise ValueError(f"{role}: nonfinite JSON number {value}")
        record[role] = json.loads((directory / f"{role}.json").read_text(), parse_constant=reject_nonfinite)
    check_observation(record)
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

    def observe(self, binary, directory, cell, round_, profile, seed, candidate, mode,
                attempt=0, warmup_s=WARMUP_S, controller="reno"):
        self.require_capacity()
        record = dict(cell=cell, round=round_, profile=profile, seed=seed, candidate=candidate, controller=controller,
                      mode=mode, attempt=attempt, status="started", occupied_s=0,
                      started_at_unix_ns=time.time_ns())
        self.attempts.append(record)
        self.save()  # A charge exists before either role can start.
        begun = time.monotonic()
        try:
            run_observation(binary, directory, cell, round_, profile, seed,
                            warmup_s=warmup_s, timeout_s=self.timeout_s, record=record, controller=controller)
            record["status"] = "succeeded"
            return record
        except BaseException as error:
            record.update(status="failed", error=f"{type(error).__name__}: {error}")
            raise
        finally:
            record["occupied_s"] = time.monotonic() - begun
            self.save()


def check_prerequisites(system, machine, which):
    if system != "Linux" or machine != "x86_64":
        raise RuntimeError(f"needs Linux amd64, not {system} {machine}")
    if not which("taskset"):
        raise RuntimeError("taskset is required")


def run_campaign(binary, output, candidate, log, ledger, controller="reno", cells=None):
    ledger.require_capacity(len(schedule(controller, cells)))
    results = output / "observations.jsonl"
    superseded = output / "superseded.jsonl"
    keyed = {}
    for instance in schedule(controller, cells):
        keyed.setdefault(instance[:2], []).append(instance)
    records, reruns = [], 0
    for (cell, round_), pair in keyed.items():
        attempt = 0
        while True:
            observed = []
            for _, _, profile, seed in pair:
                quiet = wait_quiet(log)
                load_before = os.getloadavg()[0]
                record = ledger.observe(binary, output / cell / f"round{round_}" / f"attempt{attempt}" / profile,
                                        cell, round_, profile, seed, candidate, "campaign", attempt=attempt,
                                        controller=controller)
                record.update(attempt=attempt, candidate=candidate, quiet=quiet, load_before=load_before, load_after=os.getloadavg()[0])
                observed.append(record)
                log(f"{cell} round {round_} {profile} attempt {attempt}: foreign {record['foreign_cpu_s']:.2f} s, contaminated {record['contaminated']}")
            if not rerun_due(observed, attempt, reruns):
                break
            with superseded.open("a") as file:
                for record in observed:
                    record["rerun_cause"] = "foreign CPU over the limit"
                    file.write(json.dumps(record) + "\n")
            ledger.save()
            ledger.require_capacity(len(pair))
            reruns += 1
            attempt += 1
        records += observed
        with results.open("a") as file:
            for record in observed:
                file.write(json.dumps(record) + "\n")
        if any(record["contaminated"] for record in observed):
            raise ValueError("contaminated pair has no admitted replacement")
    return records, reruns


def binary_identity(binary):
    path = Path(binary).resolve(strict=True)
    build = subprocess.run(["go", "version", "-m", str(path)], capture_output=True, text=True, check=True).stdout
    if not build.splitlines() or not build.splitlines()[0].endswith(": " + GO_VERSION):
        raise ValueError(f"binary must be built with {GO_VERSION}")
    return {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "build_info": build}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", required=True, help="the exact 40-character product commit under measurement")
    parser.add_argument("--binary", required=True, help="the test binary built with -tags ordinarycost")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--remaining-instances", required=True, type=int)
    parser.add_argument("--remaining-host-seconds", required=True, type=float)
    parser.add_argument("--timeout-seconds", required=True, type=float, help="maximum reserved duration per observation, including setup/cleanup")
    parser.add_argument("--controller", choices=CONTROLLERS, default="reno",
                        help="every connection's controller; bbrv3 runs B5's revalidation cells")
    parser.add_argument("--revalidate-cell", action="append", choices=sorted(CELLS),
                        help="campaign only: rerun just this affected cell of the Reno inventory; repeatable")
    parser.add_argument("--cause", help="the concrete source change that affects each --revalidate-cell")
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--smoke-cell", choices=sorted(CELLS), help="one short diagnostic pair; never counted campaign evidence")
    modes.add_argument("--diagnostic-cell", choices=sorted(CELLS), help="one 10s/60s diagnostic pair; never counted campaign evidence")
    args = parser.parse_args(argv)
    check_prerequisites(platform.system(), platform.machine(), shutil.which)
    if len(args.candidate) != 40 or any(c not in "0123456789abcdef" for c in args.candidate):
        parser.error("--candidate must be a full lowercase commit SHA")
    cell_choice = args.smoke_cell or args.diagnostic_cell
    if args.revalidate_cell and cell_choice:
        parser.error("--revalidate-cell selects campaign cells")
    if args.revalidate_cell and args.controller != "reno":
        parser.error("--revalidate-cell reruns Reno cells; the BBR revalidation is its fixed inventory")
    if bool(args.revalidate_cell) != bool(args.cause):
        parser.error("--revalidate-cell and --cause go together")
    if cell_choice and cell_choice not in schedule_cells(args.controller):
        parser.error(f"--controller {args.controller} runs only {', '.join(schedule_cells(args.controller))}")
    binary = binary_identity(args.binary)
    args.output.mkdir(parents=True, exist_ok=False)
    mode = "smoke" if args.smoke_cell else "diagnostic" if args.diagnostic_cell else "campaign"
    warmup = 2 if mode == "smoke" else WARMUP_S
    identity = {
        "candidate": args.candidate, "binary": binary,
        "comparator": {"candidate": args.candidate, "binary_sha256": binary["sha256"], "profile": CONTROL},
        "kernel": platform.platform(), "uname": " ".join(platform.uname()), "go": GO_VERSION,
        "server_cpus": SERVER_CPUS, "client_cpus": CLIENT_CPUS, "fixture_cpus_and_siblings": FIXTURE_CPUS,
        "warmup_s": warmup, "window_s": WINDOW_S, "base_seed": BASE_SEED, "mode": mode, "controller": args.controller,
        "cells": schedule_cells(args.controller, args.revalidate_cell), "revalidation_cause": args.cause,
        "remaining_instances": args.remaining_instances, "remaining_host_seconds": args.remaining_host_seconds,
        "per_invocation_timeout_s": args.timeout_seconds, "metric_boundaries": METRIC_BOUNDARIES,
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
    summary = {"passed": False, "mode": mode, "campaign_complete": False, "metric_boundaries": METRIC_BOUNDARIES}
    try:
        ledger = AttemptLedger(args.output, args.remaining_instances, args.remaining_host_seconds, args.timeout_seconds)
        if args.timeout_seconds < SETUP_S + warmup + WINDOW_S + 10:
            raise ValueError("timeout does not cover setup, warmup, window and fixture cleanup")
        if mode == "campaign":
            records, reruns = run_campaign(binary["path"], args.output, args.candidate, log, ledger,
                                           args.controller, args.revalidate_cell)
            summary.update(summarize(records, args.controller, args.revalidate_cell), reruns=reruns,
                           campaign_complete=True, controller=args.controller,
                           selected_cells=schedule_cells(args.controller, args.revalidate_cell))
        else:
            cell = args.smoke_cell or args.diagnostic_cell
            ledger.require_capacity(2)
            records = []
            for profile in (CONTROL, OPTED_IN):
                quiet = wait_quiet(log)
                record = ledger.observe(binary["path"], args.output / cell / profile, cell, 0,
                                        profile, BASE_SEED, args.candidate, mode, warmup_s=warmup,
                                        controller=args.controller)
                record["quiet"] = quiet
                records.append(record)
                with (args.output / f"{mode}.jsonl").open("a") as file:
                    file.write(json.dumps(record) + "\n")
            verdict = judge_cell(cell, [tuple(records)])
            summary.update(passed=verdict["passed"] and not any(r["contaminated"] for r in records),
                           diagnostic=verdict, contaminated_observations=sum(r["contaminated"] for r in records))
    except (Exception, KeyboardInterrupt) as error:
        summary.update(passed=False, error=f"{type(error).__name__}: {error}")
    finally:
        signal.signal(signal.SIGTERM, previous_sigterm)
        if ledger:
            ledger.save()
            summary.update(ledger.snapshot())
        (args.output / "summary.json").write_text(json.dumps(summary, indent=2, allow_nan=False) + "\n")
    log(f"done: mode {mode}, passed {summary['passed']}, charged {summary.get('instances_charged', 0)}")
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
