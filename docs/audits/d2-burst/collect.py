#!/usr/bin/env python3
"""Fixed burst timing and separate profiles, with temporary cpuset ownership."""

import argparse
import datetime
import json
import os
from pathlib import Path
import signal
import subprocess
import time


def stop_process(process, label, errors):
    """Bound each wait and keep trying after individual subprocess errors."""
    try:
        if process.poll() is not None:
            return
    except BaseException as exc:
        errors.append((f"{label} poll", exc))
    try:
        process.terminate()
    except BaseException as exc:
        errors.append((f"{label} terminate", exc))
    try:
        process.wait(timeout=10)
        return
    except BaseException as exc:
        errors.append((f"{label} wait after terminate", exc))
    try:
        process.kill()
    except BaseException as exc:
        errors.append((f"{label} kill", exc))
    try:
        process.wait(timeout=10)
    except BaseException as exc:
        errors.append((f"{label} wait after kill", exc))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("--smoke", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve()
    results = root / ("smoke" if args.smoke else "results")
    results.mkdir(exist_ok=True)
    helper = root / "base/docs/audits/d2-burst/isolate.py"
    sudo = ["sudo", "-n", "python3", str(helper)]
    socket = root / "run.sock"
    children = []

    def interrupted(signum, frame):
        raise RuntimeError(f"Interrupted by signal {signum}")

    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupted)

    def isolation(action):
        return subprocess.check_output(sudo + [action], text=True).strip()

    def frequency():
        return {str(cpu): (Path(f"/sys/devices/system/cpu/cpu{cpu}/cpufreq/scaling_cur_freq").read_text().strip())
                for cpu in (8, 9, 10, 24, 25, 26)}

    def sample(rate, mode, variant, duration_ms, phase, round_index):
        assert not socket.exists(), "Stale socket; refusing reuse"
        receipt = isolation("check")
        env = ["GOTOOLCHAIN=local", "GOMAXPROCS=2", "LC_ALL=C", "QUEUE_EXPERIMENT=1",
               f"QUEUE_RATE={rate}", "QUEUE_BURST=32", "QUEUE_STALL=0", f"QUEUE_DURATION_MS={duration_ms}",
               f"QUEUE_PACING={mode}", f"QUEUE_SOCKET={socket}"]
        binary = root / (f"{variant}.race.test" if args.smoke else f"{variant}.test")
        command = sudo + ["enter", "taskset", "-c", "8,9", "env"] + env + [str(binary),
                  "-test.run", "^TestDatagramReceivePacedExperiment$", "-test.v", "-test.count=1", "-test.timeout=15s"]
        prefix = results / "profiles" / f"{rate}-{mode}-{variant}"
        if phase == "cpu":
            command += ["-test.cpuprofile", str(prefix) + ".cpu.pprof"]
        elif phase == "wait":
            command += ["-test.blockprofile", str(prefix) + ".block.pprof", "-test.blockprofilerate=1",
                        "-test.mutexprofile", str(prefix) + ".mutex.pprof", "-test.mutexprofilefraction=1"]
        elif phase == "trace":
            command += ["-test.trace", str(prefix) + ".trace"]
        record = {"round": round_index, "case": f"{rate}/{mode}", "variant": variant, "phase": phase,
                  "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "command": command,
                  "isolation_before": json.loads(receipt), "frequency_before": frequency()}
        start = time.monotonic()
        receiver = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        children.append((receiver, f"receiver {rate}/{mode}/{variant}/{phase}/{round_index}"))
        pacer = None
        if mode == "external":
            ready_deadline = time.monotonic() + 10
            while not socket.exists():
                if receiver.poll() is not None or time.monotonic() > ready_deadline:
                    raise RuntimeError(f"Receiver readiness failure: {receiver.communicate(timeout=2)}")
                time.sleep(0.01)
            pacer_command = sudo + ["enter", "taskset", "-c", "10", "env"] + env + ["GOMAXPROCS=1", "QUEUE_PACER=1",
                            str(root / "base.test"), "-test.run", "^TestDatagramExternalPacer$", "-test.v", "-test.count=1", "-test.timeout=15s"]
            record["pacer_command"] = pacer_command
            pacer = subprocess.Popen(pacer_command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            children.append((pacer, f"pacer {rate}/{mode}/{variant}/{phase}/{round_index}"))
        stdout, stderr = receiver.communicate(timeout=20)
        record.update(returncode=receiver.returncode, stdout=stdout, stderr=stderr, wall_seconds=time.monotonic()-start,
                      frequency_after=frequency(), isolation_after=json.loads(isolation("check")))
        if pacer:
            out, err = pacer.communicate(timeout=5)
            record.update(pacer_stdout=out, pacer_stderr=err, pacer_returncode=pacer.returncode)
        return record

    manifest = results / "samples.jsonl"
    with manifest.open("x") as output, (results / "cpu-activity.log").open("x") as cpu_log:
        monitor = None
        failure = None
        setup_receipt = isolation("setup")
        try:
            (results / "isolation-before.json").write_text(setup_receipt + "\n")
            monitor = subprocess.Popen(["mpstat", "-P", "ALL", "1"], stdout=cpu_log, env=dict(os.environ, LC_ALL="C", TZ="UTC"))
            precheck = subprocess.check_output(["mpstat", "-P", "ALL", "1", "3"], text=True)
            (results / "precheck.log").write_text(precheck)
            cases = [(rate, mode) for rate in (10000, 100000, 500000) for mode in ("internal", "external")]
            rounds = 1 if args.smoke else 20
            for index in range(rounds):
                offset = index % len(cases)
                variants = ("base", "head-index") if index % 2 == 0 else ("head-index", "base")
                for rate, mode in cases[offset:] + cases[:offset]:
                    for variant in variants:
                        rec = sample(rate, mode, variant, 160 if args.smoke else 2000, "timing", index+1)
                        output.write(json.dumps(rec) + "\n")
                        output.flush()
                        if rec["returncode"] or rec.get("pacer_returncode", 0):
                            raise RuntimeError(f"Failed sample: {rec}")
                print(f"Completed paired round {index+1}/{rounds}", flush=True)
            if not args.smoke:
                (results / "profiles").mkdir()
                with (results / "profiles.jsonl").open("x") as profiles:
                    for phase, duration in (("cpu", 4000), ("wait", 2000), ("trace", 2000)):
                        for rate, mode in cases:
                            for variant in ("base", "head-index"):
                                rec = sample(rate, mode, variant, duration, phase, 1)
                                profiles.write(json.dumps(rec) + "\n")
                                profiles.flush()
                                if rec["returncode"] or rec.get("pacer_returncode", 0):
                                    raise RuntimeError(f"Failed profile: {rec}")
                        print(f"Completed separate {phase} profiles", flush=True)
        except BaseException as exc:
            failure = exc
            raise
        finally:
            errors = []
            try:
                for child, label in children:
                    stop_process(child, label, errors)
                if monitor is not None:
                    stop_process(monitor, "monitor", errors)
            finally:
                # A successful setup owns exactly one cleanup attempt, even if
                # teardown fails. Only the helper can confirm CPU restoration.
                try:
                    cleanup_receipt = isolation("cleanup")
                except BaseException as exc:
                    errors.append(("isolation cleanup (restoration unconfirmed)", exc))
                else:
                    try:
                        (results / "isolation-after.json").write_text(cleanup_receipt + "\n")
                    except BaseException as exc:
                        errors.append(("persist isolation-after.json (cleanup confirmed)", exc))
            if errors:
                diagnostics = [("collection", failure)] if failure is not None else []
                diagnostics += errors
                message = "\n".join(f"{label}: {type(exc).__name__}: {exc}" for label, exc in diagnostics)
                raise RuntimeError(f"Collector failed:\n{message}") from (failure or errors[0][1])


if __name__ == "__main__":
    main()
