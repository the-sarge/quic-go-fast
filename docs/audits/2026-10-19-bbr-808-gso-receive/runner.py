#!/usr/bin/env python3
"""#808 side-stage runner for minimax (loopback), adapted from #824's (2f550727).

Recorded changes from #824's runner: the arms differ in the sender build
(pre-#743 `send-pre` or the revision's `send`), the mode, the controller
(`-cc` at both endpoints) and the sender's environment (#743's kill switch);
the receiver is always `bench808-recv` (the revision plus #824's counters);
the binaries, output, done marker and yield file are this record's. Each
round runs every arm once, in an order rotated by round. #824's description
follows.
Per run: wait for quiet fixture cores, start the arm's receiver (cores
12-15) under GNU time, which forks it, so its ru_maxrss is the receiver's
own rather than this runner's (Linux ru_maxrss survives execve), then the
sender (cores 8-11). Record both JSON lines, whole-process CPU (wait4), the
receiver's peak RSS, and a foreign-load check on the fixture cores and their
SMT siblings.

usage: runner.py OUTDIR ROUNDS [ARM ...] [--duration S] [--tag TAG] [--first N]
A verification aid for #808's side stage; not a maintained deliverable.
"""
import json, os, subprocess, sys, time

BIN = os.environ.get("BENCH808_BIN", os.path.expanduser("~/agents/quic-808/.local/bbr-808-gso/bin"))
SENDER_CPUS, RECEIVER_CPUS = "8-11", "12-15"
FIXTURE = list(range(8, 16)) + list(range(24, 32))
HZ = os.sysconf("SC_CLK_TCK")
TIME = "/usr/bin/time"  # GNU time: forks the receiver and reports its peak RSS
KILL = {"QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS": "true"}
ARMS = {
    # name: (sender build, mode, controller at both endpoints, sender environment); receiver: bench808-recv
    "S-pre": ("send-pre", "stream", "reno", {}),
    "S-rev": ("send", "stream", "reno", {}),
    "D-reno": ("send", "datagram", "reno", {}),
    "M-reno": ("send", "mixed", "reno", {}),
    "D-bbr": ("send", "datagram", "bbrv3", {}),
    "Dk-bbr": ("send", "datagram", "bbrv3", KILL),
    "S-bbr": ("send", "stream", "bbrv3", {}),
    "M-bbr": ("send", "mixed", "bbrv3", {}),
}
DEFAULT_ARMS = list(ARMS)
# Another campaign asks for the host by creating this file; runs pause
# between runs until it is removed.
YIELD = os.path.expanduser("~/agents/quic-808/YIELD-808.request")
QUIET_BUSY = 0.05  # per fixture CPU over a 2 s sample
FOREIGN_LIMIT_S = 0.3  # foreign CPU-seconds on fixture CPUs per run


def cpu_times():
    out = {}
    with open("/proc/stat") as f:
        for line in f:
            if line.startswith("cpu") and line[3].isdigit():
                p = line.split()
                v = list(map(int, p[1:9]))
                idle = v[3] + v[4]
                out[int(p[0][3:])] = (sum(v) - idle, sum(v))
    return out


def run_ns():
    """Per-CPU task running time (ns) from /proc/schedstat."""
    out = {}
    with open("/proc/schedstat") as f:
        for line in f:
            if line.startswith("cpu"):
                p = line.split()
                out[int(p[0][3:])] = int(p[7])
    return out


def busy_seconds(a, b, cpus):
    return sum(b[c][0] - a[c][0] for c in cpus) / HZ


def softirqd_ticks():
    total = 0
    for pid in os.listdir("/proc"):
        if not pid.isdigit():
            continue
        try:
            with open(f"/proc/{pid}/stat") as f:
                s = f.read()
        except OSError:
            continue
        name = s[s.index("(") + 1:s.rindex(")")]
        if name.startswith("ksoftirqd/") and int(name.split("/")[1]) in FIXTURE:
            f = s[s.rindex(")") + 2:].split()
            total += int(f[11]) + int(f[12])
    return total


def wait_quiet(log):
    waited = 0.0
    while os.path.exists(YIELD):
        log(f"yield requested ({YIELD}); paused")
        time.sleep(15)
        waited += 15
    while True:
        a = cpu_times()
        time.sleep(2)
        b = cpu_times()
        worst = max((b[c][0] - a[c][0]) / max(1, b[c][1] - a[c][1]) for c in FIXTURE)
        if worst < QUIET_BUSY:
            return waited, worst
        log(f"fixture busy {worst:.3f}; waiting")
        time.sleep(13)
        waited += 15
        if waited > 1800:
            return waited, worst


def run_one(arm, duration, workdir):
    build, mode, cc, extra = ARMS[arm]
    env = dict(os.environ, GOMAXPROCS="4")
    cert = os.path.join(workdir, "cert.pem")
    rss_file = os.path.join(workdir, "recv-maxrss.txt")
    if os.path.exists(rss_file):
        os.remove(rss_file)
    # taskset pins GNU time, which forks the receiver onto the same cores.
    recv = subprocess.Popen(
        ["taskset", "-c", RECEIVER_CPUS, TIME, "-f", "%M", "-o", rss_file,
         f"{BIN}/bench808-recv", "-role", "recv", "-mode", mode, "-cc", cc, "-cert", cert],
        env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if recv.stderr.readline().strip() != "ready":
        recv.kill()
        raise RuntimeError("receiver not ready")
    send = subprocess.Popen(
        ["taskset", "-c", SENDER_CPUS, f"{BIN}/bench808-{build}", "-role", "send", "-mode", mode, "-cc", cc,
         "-payload", "1190", "-duration", f"{duration}s", "-cert", cert],
        env=dict(env, **extra), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    _, sstatus, sru = os.wait4(send.pid, 0)
    _, rstatus, rru = os.wait4(recv.pid, 0)
    sout, serr = send.stdout.read(), send.stderr.read()
    rout, rerr = recv.stdout.read(), recv.stderr.read()
    if sstatus or rstatus:
        raise RuntimeError(f"{arm}: sender {sstatus} {serr!r} receiver {rstatus} {rerr!r}")
    with open(rss_file) as f:
        recv_maxrss_kb = int(f.read().split()[-1])
    return {
        "send": json.loads(sout), "recv": json.loads(rout),
        "send_process_cpu_s": sru.ru_utime + sru.ru_stime,
        # wait4 on GNU time covers time and the receiver it waited for.
        "recv_process_cpu_s": rru.ru_utime + rru.ru_stime,
        "recv_maxrss_kb": recv_maxrss_kb,
        "recv_wait4_maxrss_kb": rru.ru_maxrss,
    }


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    opts = dict(a[2:].split("=", 1) for a in sys.argv[1:] if a.startswith("--"))
    outdir, rounds, arms = args[0], int(args[1]), args[2:] or DEFAULT_ARMS
    duration = float(opts.get("duration", "10"))
    tag = opts.get("tag", "counted")
    os.makedirs(outdir, exist_ok=True)
    logf = open(os.path.join(outdir, f"{tag}.log"), "a")

    def log(msg):
        line = f"{time.strftime('%Y-%m-%dT%H:%M:%S%z')} {msg}"
        print(line, flush=True)
        logf.write(line + "\n")
        logf.flush()

    first = int(opts.get("first", "1"))
    with open(os.path.join(outdir, f"{tag}.jsonl"), "a") as out:
        for r in range(first, first + rounds):
            order = arms[(r - 1) % len(arms):] + arms[:(r - 1) % len(arms)]
            log(f"round {r} order {' '.join(order)}")
            for pos, arm in enumerate(order):
                waited, quiet = wait_quiet(log)
                load_before = open("/proc/loadavg").read().split()[:3]
                a, sa, na = cpu_times(), softirqd_ticks(), run_ns()
                t0 = time.time()
                res = run_one(arm, duration, outdir)
                b, sb, nb = cpu_times(), softirqd_ticks(), run_ns()
                busy = busy_seconds(a, b, FIXTURE)
                running = sum(nb[c] - na[c] for c in FIXTURE) / 1e9
                softirqd = (sb - sa) / HZ
                # Task running time on fixture CPUs not accounted to the harness.
                foreign = running - res["send_process_cpu_s"] - res["recv_process_cpu_s"] - softirqd
                rec = dict(res, tag=tag, round=r, position=pos, arm=arm, start=t0, wall_s=time.time() - t0,
                           quiet_wait_s=waited, quiet_worst=quiet, load_before=load_before,
                           load_after=open("/proc/loadavg").read().split()[:3],
                           fixture_busy_s=busy, fixture_running_s=running, fixture_softirqd_s=softirqd, foreign_s=foreign,
                           contaminated=foreign > FOREIGN_LIMIT_S)
                out.write(json.dumps(rec) + "\n")
                out.flush()
                sends = res["send"].get("sendmsg_calls") or 1
                sent = res["send"].get("sent_datagrams") or 1
                log(f"round {r} {arm} [{res['send'].get('controller')}/{res['recv'].get('controller')}]: "
                    f"{res['recv'].get('mbps', 0):.0f} Mbit/s "
                    f"segs/send {res['send'].get('sendmsg_segments', 0) / sends:.2f} "
                    f"loss {1 - res['recv'].get('datagrams', 0) / sent:.6f} "
                    f"v1 {res['recv'].get('drops_v1', '-')} queue {res['recv'].get('datagram_queue_drops')} "
                    f"unproc {res['recv'].get('unprocessed_drops')} rss {res['recv_maxrss_kb']} KiB "
                    f"foreign {foreign:.3f}s softirqd {softirqd:.3f}s")
    log("done")
    open(os.path.join(outdir, f"{tag}.DONE"), "w").close()


if __name__ == "__main__":
    main()
