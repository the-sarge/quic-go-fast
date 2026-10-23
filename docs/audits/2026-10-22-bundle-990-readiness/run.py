#!/usr/bin/env python3
"""Finite, local-only #712 Linux diagnostic runner for one Wayfinder ticket.

Copied from the #711 run aid (dc252f8a): the same endpoint roles, fixture
configuration, receiver checks and frozen-model relay path. Linux changes, all
recorded per observation: each endpoint and the relay run as the ordinary
user under a fixed CPU affinity (four physical cores per endpoint, two for the
relay, every SMT sibling left idle); per-CPU host contention is sampled every
second; effective socket buffers, UDP error counters, thread affinity and
credentials are captured; and attribution stages attach `sudo perf` to the
endpoint PIDs for the measured window only. The relay is #711's v3 plus the
Linux ECN adapter. This is an experiment receipt, not a benchmark framework.
"""
import hashlib
import json
import math
import os
import pathlib
import signal
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[3]
ART = ROOT / '.local/bbr-linux-diagnostic'
OBS = ART / 'observations'
RECEIVER, SENDER, FRONT, BACK = '127.0.0.1:24761', '127.0.0.1:24762', '127.0.0.1:24771', '127.0.0.1:24772'
# Physical cores; SMT sibling of CPU n is n + 16 and is never in any affinity set.
AFFINITY = {'send': '8-11', 'receive': '12-15', 'relay': '4,5'}
HARNESS_CPUS = '1-3'  # this runner and perf; matrix.py is launched under it
FIXTURE_CPUS = [4, 5, 8, 9, 10, 11, 12, 13, 14, 15]
SIBLING_CPUS = [c + 16 for c in FIXTURE_CPUS]
PERF_STAT_EVENTS = ['instructions:u', 'instructions:k', 'cycles:u', 'cycles:k', 'context-switches', 'cpu-migrations',
                    'page-faults', 'sched:sched_wakeup', 'syscalls:sys_enter_sendmsg', 'syscalls:sys_enter_sendmmsg',
                    'syscalls:sys_enter_recvmsg', 'syscalls:sys_enter_recvmmsg', 'syscalls:sys_enter_futex',
                    'syscalls:sys_enter_epoll_pwait', 'syscalls:sys_enter_nanosleep', 'net:net_dev_xmit']
PATHS = {'loopback': dict(warmup_ms=5000, measure_ms=20000), 'S6': dict(warmup_ms=10000, measure_ms=30000),
         'S5': dict(warmup_ms=10000, measure_ms=30000)}


def perf_counters(path):
    """Parse `perf stat -x,` output: value,unit,event,run-time,pct-running,..."""
    out = {}
    for line in path.read_text().splitlines():
        parts = line.split(',')
        if len(parts) < 4 or line.startswith('#'):
            continue
        value, event = parts[0], parts[2]
        if event in PERF_STAT_EVENTS:
            out[event] = dict(value=None if value.startswith('<') else float(value),
                              running_pct=float(parts[4]) if len(parts) > 4 and parts[4] else None)
    return out


def proc_stat_cpus():
    """Per-CPU busy and softirq jiffies from /proc/stat."""
    cpus = {}
    for line in pathlib.Path('/proc/stat').read_text().splitlines():
        f = line.split()
        if f[0].startswith('cpu') and f[0] != 'cpu':
            v = list(map(int, f[1:9]))  # user nice system idle iowait irq softirq steal
            cpus[int(f[0][3:])] = dict(busy=v[0] + v[1] + v[2] + v[5] + v[6] + v[7], softirq=v[6] + v[5])
    return cpus


def proc_cpu_ticks(pid):
    try:
        f = pathlib.Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
        return int(f[11]) + int(f[12])  # utime + stime, clock ticks
    except (FileNotFoundError, ProcessLookupError, IndexError):
        return None


def all_proc_ticks():
    out = {}
    for d in pathlib.Path('/proc').iterdir():
        if d.name.isdigit():
            t = proc_cpu_ticks(d.name)
            if t is not None:
                try:
                    out[d.name] = (t, (d / 'comm').read_text().strip())
                except OSError:
                    pass
    return out


def udp_snmp():
    lines = [l.split() for l in pathlib.Path('/proc/net/snmp').read_text().splitlines() if l.startswith('Udp:')]
    return dict(zip(lines[0][1:], map(int, lines[1][1:])))


def vm_hwm_kib(pid):
    try:
        for line in pathlib.Path(f'/proc/{pid}/status').read_text().splitlines():
            if line.startswith('VmHWM:'):
                return int(line.split()[1])
    except (FileNotFoundError, ProcessLookupError):
        pass
    return None


def read_pid(path, launcher, seconds=2.0):
    deadline = time.monotonic() + seconds
    while not path.exists() or not path.read_text().endswith('\n'):
        if launcher.poll() is not None or time.monotonic() > deadline:
            raise RuntimeError(f'{path.name} missing')
        time.sleep(.005)
    return int(path.read_text())


def process_facts(pid):
    """Credentials, process and per-thread affinity, read while the process runs."""
    out = dict(threads={})
    try:
        status = pathlib.Path(f'/proc/{pid}/status').read_text()
        out.update({k: v.strip() for k, v in (l.split(':', 1) for l in status.splitlines())
                    if k in ('Uid', 'Gid', 'Cpus_allowed_list', 'CapEff', 'Threads')})
        for t in pathlib.Path(f'/proc/{pid}/task').iterdir():
            st = (t / 'status').read_text()
            stat = (t / 'stat').read_text().rsplit(')', 1)[1].split()
            out['threads'][t.name] = dict(cpus=[l.split(':', 1)[1].strip() for l in st.splitlines() if l.startswith('Cpus_allowed_list')][0],
                                          last_cpu=int(stat[36]))
    except (FileNotFoundError, ProcessLookupError):
        out['error'] = 'process gone'
    return out


def socket_facts():
    """Effective socket buffers (skmem rb/tb) and drops for this user's UDP sockets."""
    return subprocess.run(['ss', '-uampnH'], capture_output=True, text=True).stdout.splitlines()


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def summarize(directory):
    tx = json.loads((directory / 'send.json').read_text())
    rx = json.loads((directory / 'receive.json').read_text())
    cfg = tx['result']['run']
    delivery = rx['result']['receiver']
    assert delivery == tx['result']['receiver'], 'receiver report differs'
    assert delivery['corrupt'] == 0 and delivery['duplicates'] == 0
    assert not tx.get('error') and not rx.get('error')
    assert not tx['result'].get('errors') and not rx['result'].get('errors')
    assert tx['result']['controller'] == rx['result']['controller'] == cfg['controller']
    assert sum(delivery['per_second_bytes']) == delivery['useful_bytes']
    gib = delivery['useful_bytes'] / 2**30
    control = tx['result']['control']
    lat = sorted(control['latency_ns'])
    meta = json.loads((directory / 'meta.json').read_text())
    row = dict(id=cfg['id'], controller=cfg['controller'], workload=cfg['workload'], **meta,
               source=tx['source'], useful_bytes=delivery['useful_bytes'],
               goodput_mbps=delivery['useful_bytes'] * 8 / (cfg['measure_ms'] / 1000) / 1e6,
               per_second_mbps=[b * 8 / 1e6 for b in delivery['per_second_bytes']],
               control_p95_ms=lat[math.ceil(len(lat) * .95) - 1] / 1e6 if lat else None,
               control_p50_ms=lat[math.ceil(len(lat) * .5) - 1] / 1e6 if lat else None,
               control_replies=control['replies'], control_skipped=control['skipped'],
               control_unresolved=control['unresolved'], control_missed=control['missed_opportunities'])
    for role, rec in [('send', tx), ('receive', rx)]:
        res = rec['resources']
        row[role] = dict(total_cpu_seconds=res['cpu_seconds'], cpu_seconds_per_gib=res['cpu_seconds'] / gib,
                         peak_rss_mib=res['peak_rss_bytes'] / 2**20, allocated_gib=res['total_alloc_bytes'] / 2**30,
                         allocated_gib_per_gib=res['total_alloc_bytes'] / delivery['useful_bytes'],
                         lost_packets=rec['trace']['lost_packets'] if rec.get('trace_enabled') else None)
    for role in ['send', 'receive']:
        perf = directory / f'{role}.perf-stat.csv'
        if perf.exists():
            c = perf_counters(perf)
            row[role]['counters'] = c
            row[role]['counters_per_gib'] = {k: v['value'] / gib for k, v in c.items() if v['value'] is not None}
        work = directory / f'{role}.work.json'
        if work.exists():
            row[role]['work'] = json.loads(work.read_text())
    row['combined_cpu_seconds_per_gib'] = row['send']['cpu_seconds_per_gib'] + row['receive']['cpu_seconds_per_gib']
    if (directory / 'relay.json').exists():
        relay = json.loads((directory / 'relay.json').read_text())
        assert not relay.get('Error'), relay.get('Error')
        for d in ['Forward', 'Reverse']:
            assert relay[d]['SendErrors'] == 0 and relay[d]['TruncatedRead'] == 0, d
            assert relay[d]['Stats']['PropagationOverflow'] == 0, d
        row['relay'] = {d.lower(): dict(stats=relay[d]['Stats'], ecn_in=relay[d]['ECNIn'], ecn_out=relay[d]['ECNOut'],
                                        max_late_ms=relay[d]['MaxLateNS'] / 1e6, late_over_1ms=relay[d]['LateOver1ms'],
                                        max_payload=relay[d]['MaxPayload']) for d in ['Forward', 'Reverse']}
        row['relay']['max_arrival_lag_ms'] = relay['MaxArrivalLagNS'] / 1e6
        row['relay']['forward_batch_histogram'] = relay['ForwardBatchHistogram']
    write(directory / 'summary.json', row)
    return row


def stop(proc):
    if proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def wait_ready(proc, path, token, seconds=2.0):
    deadline = time.monotonic() + seconds
    while token not in path.read_text():
        if proc.poll() is not None or time.monotonic() > deadline:
            raise RuntimeError(f'{path.name} not ready')
        time.sleep(.02)


def start_perf(kind, pid, out):
    """Attach root perf to an unprivileged endpoint PID; perf_event_paranoid is unchanged."""
    if kind == 'stat':
        cmd = ['sudo', '-n', 'taskset', '-c', HARNESS_CPUS, 'perf', 'stat', '-x,', '-e', ','.join(PERF_STAT_EVENTS),
               '-p', str(pid), '-o', str(out)]
    else:
        cmd = ['sudo', '-n', 'taskset', '-c', HARNESS_CPUS, 'perf', 'record', '-e', 'cycles', '-F', '4999', '--call-graph', 'fp',
               '-p', str(pid), '-o', str(out), '--quiet']
    return cmd, subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)


def stop_perf(proc):
    subprocess.run(['sudo', '-n', 'kill', '-INT', str(proc.pid)], check=False)
    try:
        _, err = proc.communicate(timeout=60)
    except subprocess.TimeoutExpired:
        subprocess.run(['sudo', '-n', 'kill', '-KILL', str(proc.pid)], check=False)
        _, err = proc.communicate()
    return proc.returncode, err


def run_case(phase, variant, workload, pair, controller, path='loopback', seed=None, profile=False, heap=False, heap_series=False,
             perf=None, tag=''):
    """Run one matched observation; refuse to replace any prior attempt."""
    name = f'{phase}-{path}-{workload}-p{pair}-{variant}-{controller}' + (f'-{tag}' if tag else '')
    directory = OBS / name
    if directory.exists():
        receipt = json.loads((directory / 'receipt.json').read_text())
        assert receipt['exit_codes'] == [0, 0], 'retain failed attempt; use a fresh phase'
        return summarize(directory)
    directory.mkdir(parents=True)
    binary = ART / 'bin' / f'{variant}-fixture'
    timing = PATHS[path]
    cfg = dict(id=name, controller=controller, workload=workload, start_unix_ns=time.time_ns() + 2_000_000_000,
               payload_bytes=16384 if workload == 'stream' else 1200, **timing)
    write(directory / 'config.json', cfg)
    write(directory / 'meta.json', dict(phase=phase, variant=variant, pair=pair, path=path, seed=seed,
                                         profiled=profile or heap or heap_series or perf == 'record', heap_series=heap_series,
                                         diag='-diag' in variant, perf=perf, tag=tag, affinity=AFFINITY))
    common = [str(binary), '-config', str(directory / 'config.json'),
              '-cert', str(ART / 'campaign.pem'), '-key', str(ART / 'campaign-key.pem'), '-trace=false']
    processes, commands, streams, relay, endpoint_pids = [], {}, [], None, {}
    started = time.time_ns()
    limit_ns = (timing['warmup_ms'] + timing['measure_ms'] + 30_000) * 1_000_000
    try:
        if path != 'loopback':
            # v1 model plus forward-delivery observability and the Linux ECN adapter, for every arm.
            relay_bin = ART / 'bin' / 'relay-linux'
            cmd = [str(relay_bin), '-scenario', path, '-seed', str(seed), '-front', FRONT, '-back', BACK,
                   '-sender', SENDER, '-target', RECEIVER, '-start-unix-ns', str(cfg['start_unix_ns']),
                   '-until', f'{limit_ns // 1_000_000}ms', '-output', str(directory / 'relay.json')]
            commands['relay'] = cmd
            err = (directory / 'relay.stderr').open('w')
            streams.append(err)
            relay = subprocess.Popen(['taskset', '-c', AFFINITY['relay']] + cmd, stdout=subprocess.DEVNULL, stderr=err)
            wait_ready(relay, directory / 'relay.stderr', 'relay ready')
        for role, addr in [('receive', RECEIVER), ('send', SENDER)]:
            cmd = common + ['-role', role, '-local', addr, '-output', str(directory / f'{role}.json')]
            if '-diag' in variant:
                cmd += ['-series-output', str(directory / f'{role}.series.json')]
            if role == 'send':
                cmd += ['-peer', FRONT if relay else RECEIVER]
            if profile:
                cmd += ['-cpu-profile', str(directory / f'{role}.cpu.pprof'), '-alloc-profile', str(directory / f'{role}.alloc.pprof')]
            if heap:
                cmd += ['-heap-profile', str(directory / f'{role}.heap.pprof')]
            if heap_series:
                (directory / f'{role}-heap').mkdir()
                cmd += ['-heap-series-dir', str(directory / f'{role}-heap')]
            env = None
            if variant.endswith('-counted'):
                env = {**os.environ, 'BBR_WORK_OUTPUT': str(directory / f'{role}.work.json')}
            commands[role] = cmd
            out = (directory / f'{role}.stdout').open('w')
            err = (directory / f'{role}.stderr').open('w')
            streams += [out, err]
            # The launcher keeps the runner's resident high-water mark out of the endpoint's ru_maxrss.
            proc = subprocess.Popen(['taskset', '-c', AFFINITY[role], str(ART / 'bin/launch'), str(directory / f'{role}.pid'), '--'] + cmd,
                                    stdout=out, stderr=err, env=env)
            processes.append(proc)
            endpoint_pids[role] = read_pid(directory / f'{role}.pid', proc)
            if role == 'receive':
                wait_ready(proc, directory / 'receive.stderr', 'ready', 1.5)
        pids = dict(endpoint_pids, **({'relay': relay.pid} if relay else {}))
        measure_start = cfg['start_unix_ns'] + timing['warmup_ms'] * 1_000_000
        measure_end = measure_start + timing['measure_ms'] * 1_000_000
        host, facts, perfs = [], {}, {}
        snmp_before, ticks_before = udp_snmp(), all_proc_ticks()
        while any(p.poll() is None for p in processes):
            if time.time_ns() - started > limit_ns:
                raise TimeoutError(name)
            now = time.time_ns()
            host.append(dict(unix_ns=now, cpus=proc_stat_cpus(), fixture_ticks={r: proc_cpu_ticks(p) for r, p in pids.items()},
                             vm_hwm_kib={r: vm_hwm_kib(endpoint_pids[r]) for r in ['send', 'receive']}))
            if perf and not perfs and now >= measure_start:
                for role in ['send', 'receive']:
                    ext = 'perf-stat.csv' if perf == 'stat' else 'perf.data'
                    perfs[role] = start_perf(perf, pids[role], directory / f'{role}.{ext}')
                commands.update({f'perf-{r}': c for r, (c, _) in perfs.items()})
            if perfs and 'perf_stopped' not in facts and now >= measure_end:
                facts['perf_stopped'] = {r: stop_perf(p) for r, (_, p) in perfs.items()}
            if not facts.get('mid') and now >= measure_start + timing['measure_ms'] * 500_000:
                facts['mid'] = dict(unix_ns=now, processes={r: process_facts(p) for r, p in pids.items()}, sockets=socket_facts())
            wake = [(now // 1_000_000_000 + 1) * 1_000_000_000]
            if perf and not perfs:
                wake.append(measure_start)
            elif perfs and 'perf_stopped' not in facts:
                wake.append(measure_end)
            time.sleep(max(0.001, (min(w for w in wake if w > now) - time.time_ns()) / 1e9) if any(w > now for w in wake) else 0.001)
        if perfs and 'perf_stopped' not in facts:
            facts['perf_stopped'] = {r: stop_perf(p) for r, (_, p) in perfs.items()}
            facts['perf_stopped_late'] = True
        if relay:
            relay.send_signal(signal.SIGTERM)
            relay.wait(timeout=5)
        ticks_after = all_proc_ticks()
        own = set(pids.values()) | {p.pid for p in processes}
        foreign = {pid: (t - ticks_before.get(pid, (0,))[0], comm) for pid, (t, comm) in ticks_after.items()
                   if int(pid) not in own}
        facts.update(udp_before=snmp_before, udp_after=udp_snmp(), measure_window_unix_ns=[measure_start, measure_end],
                     top_foreign_ticks=sorted(([c, t, pid] for pid, (t, c) in foreign.items() if t > 0), key=lambda x: -x[1])[:20])
        write(directory / 'host-cpu.json', host)
        write(directory / 'host-facts.json', facts)
        write(directory / 'receipt.json', dict(commands=commands, start_unix_ns=started, end_unix_ns=time.time_ns(),
              exit_codes=[p.returncode for p in processes], relay_exit=relay.returncode if relay else None,
              binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
              relay_sha256=hashlib.sha256(pathlib.Path(commands['relay'][0]).read_bytes()).hexdigest() if relay else None,
              perf_exit={r: c for r, (c, _) in facts.get('perf_stopped', {}).items()}))
        assert all(p.returncode == 0 for p in processes), name
        assert relay is None or relay.returncode == 0, name
        for r, (code, err) in facts.get('perf_stopped', {}).items():
            # perf re-raises SIGINT after writing its output, so -2 is a normal stop.
            assert code in (0, -2), (name, r, err)
        row = summarize(directory)
        print(json.dumps(dict(id=name, goodput_mbps=round(row['goodput_mbps'], 1),
                              sender_cpu_per_gib=round(row['send']['cpu_seconds_per_gib'], 2),
                              rss=[round(row['send']['peak_rss_mib'], 2), round(row['receive']['peak_rss_mib'], 2)],
                              control_p95_ms=row['control_p95_ms'])), flush=True)
        return row
    finally:
        for proc in processes + ([relay] if relay else []):
            stop(proc)
        for s in streams:
            s.close()


def ensure_credentials():
    if not (ART / 'campaign-key.pem').exists():
        subprocess.run([str(ART / 'bin/reno-fixture'), '-role', 'cert'], cwd=ART, check=True)
