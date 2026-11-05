#!/usr/bin/env python3
"""R18 ordinary-peer cost: a missing or misaccounted case is a failed claim, not a product result."""

import argparse
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import time

GO_VERSION = "go1.27.3"
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


def schedule():
    """The fixed (cell, round, profile, seed) instances. A pair shares its seed."""
    instances = []
    for index, cell in enumerate(CELLS):
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
    if server["go"] != GO_VERSION:
        raise ValueError(f"{cell}: toolchain {server['go']}")
    if server["granted"] != (profile == OPTED_IN):
        raise ValueError(f"{cell}/{profile}: grant does not match the profile")
    if profile == OPTED_IN and not server["joined"]:
        raise ValueError(f"{cell}: write authority did not join")
    if server["gso_capable"] == bool(CELLS[cell]["env"]):
        raise ValueError(f"{cell}: acceleration does not match the cell")
    for flow in client["flows"]:
        if len(flow["series"]) != WINDOW_S:
            raise ValueError(f"{cell}: {flow['label']} has {len(flow['series'])} seconds")
        if sum(flow["series"]) != flow["useful_bytes"]:
            raise ValueError(f"{cell}: {flow['label']} useful bytes do not match its series")
    ordinary = flows(record, "ordinary")
    if len(ordinary) != 1:
        raise ValueError(f"{cell}: {len(ordinary)} ordinary flows")
    if not ordinary[0]["control"] or not ordinary[0]["control"]["count"]:
        raise ValueError(f"{cell}: no control samples")
    if not server["send_completion"].get("ordinary", {}).get("count"):
        raise ValueError(f"{cell}: no send-completion samples")
    if cell == "mixed-bulk":
        check_mixed(record)
    if cell == "stateless-pressure" and not client["flood"]["sent"]:
        raise ValueError(f"{cell}: no pressure applied")


def check_mixed(record):
    background = flows(record, "background")
    if record["profile"] == CONTROL:
        if len(background) != 2:
            raise ValueError("mixed-bulk control needs two independent connections")
        return
    if len(background) != 1 or not background[0].get("multipath_negotiated") or len(background[0].get("paths", [])) != 2:
        raise ValueError("mixed-bulk opted-in needs one negotiated two-path connection")


def check_accounting(records):
    expected = sorted(instance[:3] for instance in schedule())
    actual = sorted((record["cell"], record["round"], record["profile"]) for record in records)
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
    peak = {CONTROL: [], OPTED_IN: []}
    lost = {CONTROL: [], OPTED_IN: []}
    drops = {CONTROL: [], OPTED_IN: []}
    write_errors = {CONTROL: [], OPTED_IN: []}
    timeouts, unbounded, starved = [], [], []
    for round_, (base, treated) in enumerate(rounds):
        base_flow, treated_flow = flows(base, "ordinary")[0], flows(treated, "ordinary")[0]
        goodput.append(treated_flow["useful_bytes"] / base_flow["useful_bytes"])
        cpu.append((treated["server"]["window_cpu_s"] / useful_bytes(treated)) / (base["server"]["window_cpu_s"] / useful_bytes(base)))
        base_control = base_flow["control"]["p99_us"]
        treated_control = treated_flow["control"]["p99_us"]
        control_margin.append(latency_margin(base_control, treated_control))
        base_send = base["server"]["send_completion"]["ordinary"]["p99_us"]
        treated_send = treated["server"]["send_completion"]["ordinary"]["p99_us"]
        send_margin.append(latency_margin(base_send, treated_send))
        for name, record in ((CONTROL, base), (OPTED_IN, treated)):
            flow = flows(record, "ordinary")[0]
            control_p99[name].append(flow["control"]["p99_us"])
            send_p99[name].append(record["server"]["send_completion"]["ordinary"]["p99_us"])
            empty[name].append(sum(1 for value in flow["series"] if value == 0))
            background[name].append(sum(f["useful_bytes"] for f in flows(record, "background")))
            peak[name].append(record["server"]["rss_kb_end"])
            lost[name].append(sum(c.get("packets_lost", 0) for c in record["server"]["connections"]))
            drops[name].append(sum(f.get("datagram_drops", 0) for f in record["client"]["flows"]))
            write_errors[name].append(record["server"]["write_errors"])
            flood = record["client"].get("flood")
            if flood and flood["sent"]:
                resets[name].append(flood["resets"] / flood["sent"])
        closes = treated["client"]["flows"] + treated["server"]["connections"]
        if any(item["timeout_close"] for item in closes):
            timeouts.append(round_)
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
        failures.append(f"send-completion p99 {statistics.median(send_margin):.0f} us over its limit")
    if timeouts:
        failures.append(f"timeout-only close in rounds {timeouts}")
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
        "send_p99_margin_us": spread(send_margin),
        "control_p99_us": control_p99,
        "send_p99_us": send_p99,
        "empty_seconds": empty,
        "background_bytes": background,
        "stateless_reset_share": resets,
        "peak_rss_kb": peak,
        "packets_lost": lost,
        "datagram_drops": drops,
        "write_errors": write_errors,
    }


def summarize(records):
    check_accounting(records)
    keyed = {(record["cell"], record["round"], record["profile"]): record for record in records}
    cells = {}
    for cell in CELLS:
        rounds = [(keyed[(cell, round_, CONTROL)], keyed[(cell, round_, OPTED_IN)]) for round_ in range(ROUNDS)]
        cells[cell] = judge_cell(cell, rounds)
    failed = [cell for cell, verdict in cells.items() if not verdict["passed"]]
    return {
        "passed": not failed,
        "failed_cells": failed,
        "cells": cells,
        "contaminated_observations": sum(1 for record in records if record["contaminated"]),
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
        if not busy and share < QUIET_BUSY_SHARE:
            return {"waited_s": waited, "fixture_busy_share": share}
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


def run_observation(binary, directory, cell, round_, profile, seed, warmup_s=WARMUP_S, window_s=WINDOW_S):
    """One instance: the server and client processes on their own pinned cores."""
    directory.mkdir(parents=True, exist_ok=False)
    start_at = time.time_ns() + SETUP_S * 10**9
    spec = {
        "cell": cell, "profile": profile, "seed": seed, "address": SERVER_ADDRESS,
        "start_at_unix_ns": start_at, "warmup_ns": warmup_s * 10**9, "window_ns": window_s * 10**9,
    }
    env = {key: value for key, value in os.environ.items() if not key.startswith("QUIC_GO_")}
    env["GOMAXPROCS"] = "4"
    server_env = dict(env, **CELLS[cell]["env"])
    processes = {}
    before = schedstat_running_ns()
    softirq_before = softirqd_ns()
    begun = time.monotonic()
    for role, cpus, process_env in (("server", SERVER_CPUS, server_env), ("client", CLIENT_CPUS, env)):
        output = directory / f"{role}.json"
        role_spec = dict(spec, role=role, output=str(output))
        full_env = dict(process_env, ORDINARY_COST_SPEC=json.dumps(role_spec))
        log = (directory / f"{role}.log").open("w")
        processes[role] = (subprocess.Popen(["taskset", "-c", cpus, *role_command(binary)], env=full_env, stdout=log, stderr=subprocess.STDOUT), log)
    usage = {}
    for role, (process, log) in processes.items():
        _, status, rusage = os.wait4(process.pid, 0)
        process.returncode = os.waitstatus_to_exitcode(status)
        log.close()
        usage[role] = rusage.ru_utime + rusage.ru_stime
    occupied = time.monotonic() - begun
    running = schedstat_running_ns()
    foreign = (sum(running[cpu] - before[cpu] for cpu in FIXTURE_CPUS) - (softirqd_ns() - softirq_before)) / 1e9 - sum(usage.values())
    record = {
        "cell": cell, "round": round_, "profile": profile, "seed": seed,
        "occupied_s": occupied, "process_cpu_s": usage, "foreign_cpu_s": foreign,
        "contaminated": foreign > foreign_limit_s(occupied),
        "exit": {role: process.returncode for role, (process, _) in processes.items()},
    }
    for role in processes:
        text = (directory / f"{role}.log").read_text()
        if record["exit"][role] != 0:
            raise ValueError(f"{cell}/{profile} {role} exit {record['exit'][role]}:\n{text[-2000:]}")
        check_role_log(role, text)
        record[role] = json.loads((directory / f"{role}.json").read_text())
    check_observation(record)
    return record


def check_prerequisites(system, machine, which):
    if system != "Linux" or machine != "x86_64":
        raise RuntimeError(f"needs Linux amd64, not {system} {machine}")
    if not which("taskset"):
        raise RuntimeError("taskset is required")


def run_campaign(binary, output, candidate, log):
    results = output / "observations.jsonl"
    superseded = output / "superseded.jsonl"
    keyed = {}
    for instance in schedule():
        keyed.setdefault(instance[:2], []).append(instance)
    records, reruns, charged, host_seconds = [], 0, 0, 0.0
    for (cell, round_), pair in keyed.items():
        attempt = 0
        while True:
            observed = []
            for _, _, profile, seed in pair:
                quiet = wait_quiet(log)
                load_before = os.getloadavg()[0]
                record = run_observation(binary, output / cell / f"round{round_}" / f"attempt{attempt}" / profile, cell, round_, profile, seed)
                record.update(attempt=attempt, candidate=candidate, quiet=quiet, load_before=load_before, load_after=os.getloadavg()[0])
                charged += 1
                host_seconds += record["occupied_s"]
                observed.append(record)
                log(f"{cell} round {round_} {profile} attempt {attempt}: foreign {record['foreign_cpu_s']:.2f} s, contaminated {record['contaminated']}")
            if not rerun_due(observed, attempt, reruns):
                break
            with superseded.open("a") as file:
                for record in observed:
                    file.write(json.dumps(dict(record, rerun_cause="foreign CPU over the limit")) + "\n")
            reruns += 1
            attempt += 1
        records += observed
        with results.open("a") as file:
            for record in observed:
                file.write(json.dumps(record) + "\n")
    return records, reruns, charged, host_seconds


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate", required=True, help="the product commit under measurement")
    parser.add_argument("--binary", required=True, help="the test binary built with -tags ordinarycost")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--smoke-cell", choices=sorted(CELLS), help="one short pair, charged to the ledger, instead of the campaign")
    args = parser.parse_args()
    check_prerequisites(platform.system(), platform.machine(), shutil.which)
    args.output.mkdir(parents=True, exist_ok=False)
    identity = {
        "candidate": args.candidate,
        "kernel": platform.platform(),
        "uname": " ".join(platform.uname()),
        "go": GO_VERSION,
        "server_cpus": SERVER_CPUS,
        "client_cpus": CLIENT_CPUS,
        "warmup_s": WARMUP_S,
        "window_s": WINDOW_S,
        "base_seed": BASE_SEED,
    }
    (args.output / "identity.json").write_text(json.dumps(identity, indent=2) + "\n")

    def log(message):
        line = f"{time.strftime('%H:%M:%S')} {message}"
        print(line, flush=True)
        with (args.output / "campaign.log").open("a") as file:
            file.write(line + "\n")

    if args.smoke_cell:
        for profile in (CONTROL, OPTED_IN):
            record = run_observation(args.binary, args.output / args.smoke_cell / profile, args.smoke_cell, 0, profile, BASE_SEED, warmup_s=2, window_s=WINDOW_S)
            with (args.output / "smoke.jsonl").open("a") as file:
                file.write(json.dumps(record) + "\n")
            log(f"smoke {args.smoke_cell} {profile}: {record['occupied_s']:.0f} s")
        return

    records, reruns, charged, host_seconds = run_campaign(args.binary, args.output, args.candidate, log)
    summary = summarize(records)
    summary.update(reruns=reruns, instances_charged=charged, host_seconds_charged=host_seconds)
    (args.output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    log(f"done: passed {summary['passed']}, failed cells {summary['failed_cells']}")


if __name__ == "__main__":
    main()
