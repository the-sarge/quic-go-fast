#!/usr/bin/env python3
"""#808 STREAM work diagnostic (descriptive): #743's perfdiag.py (2f550727)
with recorded changes: the pair is S-pre / S-rev from this record's runner
(Reno STREAM, pre-#743 sender against the revision's), the binaries are
bench808's, the -cc flag is passed, and foreign time subtracts the fixture
CPUs' ksoftirqd, as the registered measure in runner.py does. #743's description follows.

#743 STREAM work diagnostic (outside the registered rules).

Runs alternating S-main / S-proto pairs on the counted fixture and attaches
`sudo -n perf stat -e instructions,cycles,task-clock` to each sender, to tell
whether a STREAM throughput difference comes from more work per GiB or from
run state and code placement.

usage: perfdiag.py OUTDIR PAIRS [--duration S]
A verification aid for #808's side stage; not a maintained deliverable.
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import runner  # noqa: E402

EVENTS = "instructions:u,instructions:k,cycles:u,cycles:k,task-clock"


def run_pair_member(arm, duration, outdir, tag):
    build, mode, cc, extra = runner.ARMS[arm]
    env = dict(os.environ, GOMAXPROCS="4")
    cert = os.path.join(outdir, "cert.pem")
    recv = subprocess.Popen(
        ["taskset", "-c", runner.RECEIVER_CPUS, f"{runner.BIN}/bench808-recv", "-role", "recv", "-mode", mode, "-cc", cc, "-cert", cert],
        env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if recv.stderr.readline().strip() != "ready":
        recv.kill()
        raise RuntimeError("receiver not ready")
    send = subprocess.Popen(
        ["taskset", "-c", runner.SENDER_CPUS, f"{runner.BIN}/bench808-{build}", "-role", "send", "-mode", mode, "-cc", cc,
         "-payload", "1190", "-duration", f"{duration}s", "-cert", cert],
        env=dict(env, **extra), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    perf_out = os.path.join(outdir, f"{tag}.perf.csv")
    perf = subprocess.Popen(["sudo", "-n", "taskset", "-c", "1-3", "perf", "stat", "-x,", "-e", EVENTS,
                             "-p", str(send.pid), "-o", perf_out])
    _, sstatus, sru = os.wait4(send.pid, 0)
    _, rstatus, rru = os.wait4(recv.pid, 0)
    perf.wait(timeout=30)
    sout, serr = send.stdout.read(), send.stderr.read()
    rout, rerr = recv.stdout.read(), recv.stderr.read()
    if sstatus or rstatus:
        raise RuntimeError(f"{arm}: sender {sstatus} {serr!r} receiver {rstatus} {rerr!r}")
    counters = {}
    for line in open(perf_out):
        p = line.strip().split(",")
        if len(p) > 2 and p[0] and not line.startswith("#"):
            try:
                counters[p[2]] = float(p[0])
            except ValueError:
                pass
    return {"send": json.loads(sout), "recv": json.loads(rout), "perf": counters,
            "send_process_cpu_s": sru.ru_utime + sru.ru_stime, "recv_process_cpu_s": rru.ru_utime + rru.ru_stime}


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    opts = dict(a[2:].split("=", 1) for a in sys.argv[1:] if a.startswith("--"))
    outdir, pairs = args[0], int(args[1])
    duration = float(opts.get("duration", "10"))
    os.makedirs(outdir, exist_ok=True)
    with open(os.path.join(outdir, "perfdiag.jsonl"), "a") as out:
        for i in range(1, pairs + 1):
            order = ["S-pre", "S-rev"] if i % 2 else ["S-rev", "S-pre"]
            for arm in order:
                waited, quiet = runner.wait_quiet(print)
                na, sa = runner.run_ns(), runner.softirqd_ticks()
                res = run_pair_member(arm, duration, outdir, f"pair{i}-{arm}")
                nb, sb = runner.run_ns(), runner.softirqd_ticks()
                running = sum(nb[c] - na[c] for c in runner.FIXTURE) / 1e9
                # The registered measure (runner.py): less the harness's own CPU and the fixture CPUs' ksoftirqd.
                foreign = running - res["send_process_cpu_s"] - res["recv_process_cpu_s"] - (sb - sa) / runner.HZ
                rec = dict(res, pair=i, arm=arm, quiet_wait_s=waited, foreign_s=foreign, contaminated=foreign > runner.FOREIGN_LIMIT_S)
                out.write(json.dumps(rec) + "\n")
                out.flush()
                gib = res["recv"]["bytes"] / 2**30
                ins = res["perf"].get("instructions:u", 0) + res["perf"].get("instructions:k", 0)
                cyc = res["perf"].get("cycles:u", 0) + res["perf"].get("cycles:k", 0)
                print(f"pair {i} {arm}: {res['recv']['mbps']:.0f} Mbit/s  Ginstr/GiB {ins / gib / 1e9:.3f}  "
                      f"Gcycles/GiB {cyc / gib / 1e9:.3f}  foreign {foreign:.3f}s", flush=True)
    open(os.path.join(outdir, "perfdiag.DONE"), "w").close()


if __name__ == "__main__":
    main()
