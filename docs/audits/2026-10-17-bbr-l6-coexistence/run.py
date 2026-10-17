#!/usr/bin/env python3
"""Finite, local-only BBRv3 L6 coexistence runner for one qualification ticket (#777).

Adapted from the #748 slice 6 readiness record's run.py (9b38f0ab, itself #712's
Linux runner): the same fixture configuration, receiver checks, launcher,
per-second host sampling, process and socket facts, and receipt. Recorded
changes:
- Each observation runs five processes: the two-flow relay (relay2) and two
  endpoint pairs, the focal pair and a competitor pair. The competitor is always
  the frozen Reno fixture, selecting Reno, with a reliable stream and Q1's
  accepted bulk pause (no bulk demand during measured t = 240-270 s, same
  connection, control probe continuing).
- An arm names the focal fixture and controller: `reno` (frozen Reno, matched
  reference), `cand` (the advanced port selecting BBRv3) and `aa` (frozen Reno
  again, the A/A arm).
- L6 timing: 60 s warm-up and 300 s measured, both flows starting together; the
  relay's model time zero is the measured start, as Q1's router used it.
- Core layout: focal sender 8-11, focal receiver 12-15, relay 4-5,
  competitor sender 2-3, competitor receiver 6-7, this runner 1; every SMT
  sibling (n + 16) and core 0 stay out of every affinity set.
- No perf, profiles or overlays.
- `summarize` reports both participants, per measured period, with the relay's
  per-flow statistics; it judges nothing (l6rules.py does).
"""
import hashlib
import json
import math
import pathlib
import signal
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[3]
ART = ROOT / '.local/bbr-l6-coexistence'
OBS = ART / 'observations'
ADDR = {
    'focal': dict(receiver='127.0.0.1:24761', sender='127.0.0.1:24762', front='127.0.0.1:24771', back='127.0.0.1:24772'),
    'competitor': dict(receiver='127.0.0.1:24781', sender='127.0.0.1:24782', front='127.0.0.1:24791', back='127.0.0.1:24792'),
}
# Physical cores; SMT sibling of CPU n is n + 16 and is never in any affinity set.
AFFINITY = {'focal-send': '8-11', 'focal-receive': '12-15', 'competitor-send': '2,3', 'competitor-receive': '6,7', 'relay': '4,5'}
HARNESS_CPUS = '1'  # this runner; matrix.py is launched under it
FIXTURE_CPUS = [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15]
SIBLING_CPUS = [c + 16 for c in FIXTURE_CPUS]
WARMUP_MS, MEASURE_MS = 60000, 300000
PAUSE_MS = (240000, 270000)
LEAD_NS = 10_000_000_000  # fixture start after launch; the fixture accepts at most two minutes
# Measured periods in seconds: [start, end). Classified: equal, short, resumed (#777 D2).
PERIODS = {'equal': (0, 100), 'short': (100, 200), 'back': (200, 240), 'absent': (240, 270), 'resumed': (270, 300)}
ARMS = {'reno': ('reno', 'reno'), 'cand': ('cand', 'bbrv3'), 'aa': ('reno', 'reno')}  # arm: (fixture variant, controller)
ROLES = ['focal-receive', 'competitor-receive', 'focal-send', 'competitor-send']


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


def quantile(values, q):
    """Ceil-index quantile, as the readiness summaries compute control p50 and p95."""
    s = sorted(values)
    return s[math.ceil(len(s) * q) - 1] if s else None


def control_periods(control, measured_start_ns):
    """Assign each measured control latency to the period of its offer.

    An opportunity is offered when no request is pending, and replies arrive in
    offer order. With no skipped and no unresolved request, every measured
    opportunity was offered and answered, so latency i belongs to opportunity i
    (exact). Otherwise the pending interval is reconstructed from each offer's
    observed time plus its latency (`reconstructed`); a count that does not
    match the fixture's own counts marks the assignment unusable.
    """
    opps, lat = control['opportunity_times_ns'], control['latency_ns']
    assigned = []
    if control['skipped'] == 0 and control['unresolved'] == 0 and len(lat) == len(opps):
        assigned = [(o[1], l) for o, l in zip(opps, lat)]
        method = 'exact'
    else:
        pending_until, k, offered, skipped = -math.inf, 0, 0, 0
        for _, observed in opps:
            if observed < pending_until:
                skipped += 1
                continue
            offered += 1
            if k < len(lat):
                assigned.append((observed, lat[k]))
                pending_until = observed + lat[k]
                k += 1
            else:
                pending_until = math.inf
        # An offer left without a reply is the fixture's unresolved request.
        counts = (offered, skipped, k, offered - k)
        method = 'reconstructed' if counts == (control['offered'], control['skipped'], control['replies'], control['unresolved']) else 'unusable'
    out = dict(method=method, periods={})
    for name, (a, b) in PERIODS.items():
        values = [l for at, l in assigned if a * 1e9 <= at - measured_start_ns < b * 1e9] if method != 'unusable' else []
        out['periods'][name] = dict(replies=len(values), p50_ms=None if not values else quantile(values, .5) / 1e6,
                                    p95_ms=None if not values else quantile(values, .95) / 1e6)
    return out


def participant(directory, who, measured_start_ns):
    tx = json.loads((directory / f'{who}-send.json').read_text())
    rx = json.loads((directory / f'{who}-receive.json').read_text())
    cfg = tx['result']['run']
    delivery = rx['result']['receiver']
    assert delivery == tx['result']['receiver'], f'{who}: receiver report differs'
    assert delivery['corrupt'] == 0 and delivery['duplicates'] == 0, who
    assert not tx.get('error') and not rx.get('error'), who
    assert not tx['result'].get('errors') and not rx['result'].get('errors'), who
    assert tx['result']['controller'] == rx['result']['controller'] == cfg['controller'], who
    assert sum(delivery['per_second_bytes']) == delivery['useful_bytes'], who
    assert len(delivery['per_second_bytes']) == MEASURE_MS // 1000, who
    per_second = delivery['per_second_bytes']
    gib = delivery['useful_bytes'] / 2**30
    control = tx['result']['control']
    lat = control['latency_ns']
    row = dict(id=cfg['id'], controller=cfg['controller'], workload=cfg['workload'], source=tx['source'],
               useful_bytes=delivery['useful_bytes'], goodput_mbps=delivery['useful_bytes'] * 8 / (MEASURE_MS / 1000) / 1e6,
               per_second_mbps=[b * 8 / 1e6 for b in per_second],
               periods={name: dict(mbps=sum(per_second[a:b]) * 8 / (b - a) / 1e6) for name, (a, b) in PERIODS.items()},
               control_p95_ms=None if not lat else quantile(lat, .95) / 1e6,
               control_p50_ms=None if not lat else quantile(lat, .5) / 1e6,
               control_replies=control['replies'], control_skipped=control['skipped'],
               control_unresolved=control['unresolved'], control_missed=control['missed_opportunities'],
               control_error=control.get('error'), control_periods=control_periods(control, measured_start_ns),
               bulk_pause_observed_s=[(t - measured_start_ns) / 1e9 for t in tx['result'].get('bulk_pause_observed_ns') or []])
    for role, rec in [('send', tx), ('receive', rx)]:
        res = rec['resources']
        row[role] = dict(total_cpu_seconds=res['cpu_seconds'], cpu_seconds_per_gib=res['cpu_seconds'] / gib if gib else None,
                         peak_rss_mib=res['peak_rss_bytes'] / 2**20, allocated_gib=res['total_alloc_bytes'] / 2**30)
    return row


def relay_periods(samples):
    """Per-period forward queue occupancy and drops per flow from the relay's 100 ms samples."""
    out = {}
    for name, (a, b) in PERIODS.items():
        rows = [s for s in samples if a * 1000 <= s[0] < b * 1000]
        if not rows:
            out[name] = None
            continue
        before = [s for s in samples if s[0] < a * 1000]
        base = before[-1] if before else [0] * 10
        out[name] = dict(samples=len(rows),
                         focal_queue_mean_bytes=sum(r[1] for r in rows) / len(rows), competitor_queue_mean_bytes=sum(r[2] for r in rows) / len(rows),
                         queue_max_bytes=max(r[1] + r[2] for r in rows),
                         focal_drops=rows[-1][5] - base[5], competitor_drops=rows[-1][6] - base[6],
                         max_late_ms=max(r[9] for r in rows) / 1e6)
    return out


def summarize(directory):
    meta = json.loads((directory / 'meta.json').read_text())
    cfg = json.loads((directory / 'focal-config.json').read_text())
    measured_start_ns = cfg['start_unix_ns'] + WARMUP_MS * 1_000_000
    row = dict(**meta, focal=participant(directory, 'focal', measured_start_ns),
               competitor=participant(directory, 'competitor', measured_start_ns))
    relay = json.loads((directory / 'relay.json').read_text())
    assert not relay.get('Error'), relay.get('Error')
    assert relay['StartUnixNS'] == measured_start_ns, 'relay model zero is not the measured start'
    for d in ['Forward', 'Reverse']:
        assert relay[d]['Stats']['PropagationOverflow'] == 0, d
        for f in ['Focal', 'Competitor']:
            assert relay[d][f]['SendErrors'] == 0 and relay[d][f]['TruncatedRead'] == 0, (d, f)
    row['relay'] = {d.lower(): dict(stats=relay[d]['Stats'], focal=relay[d]['Focal'], competitor=relay[d]['Competitor'],
                                    max_late_ms=relay[d]['MaxLateNS'] / 1e6, late_over_1ms=relay[d]['LateOver1ms'])
                    for d in ['Forward', 'Reverse']}
    row['relay']['max_arrival_lag_ms'] = relay['MaxArrivalLagNS'] / 1e6
    row['relay']['forward_batch_histogram'] = relay['ForwardBatchHistogram']
    row['relay']['periods'] = relay_periods(relay['Samples'])
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


def run_case(phase, workload, pair, arm, seed, tag=''):
    """Run one coexistence observation; refuse to replace any prior attempt."""
    name = f'{phase}-L6-{workload}-p{pair}-{arm}' + (f'-{tag}' if tag else '')
    directory = OBS / name
    if directory.exists():
        receipt = json.loads((directory / 'receipt.json').read_text())
        assert receipt['exit_codes'] == [0] * len(ROLES) and receipt['relay_exit'] == 0, 'retain failed attempt; use a fresh phase'
        return summarize(directory)
    directory.mkdir(parents=True)
    variant, controller = ARMS[arm]
    start_ns = time.time_ns() + LEAD_NS
    timing = dict(start_unix_ns=start_ns, warmup_ms=WARMUP_MS, measure_ms=MEASURE_MS)
    configs = {
        'focal': dict(id=name, controller=controller, workload=workload, payload_bytes=16384 if workload == 'stream' else 1200, **timing),
        'competitor': dict(id=name + '-competitor', controller='reno', workload='stream', payload_bytes=16384, **timing,
                           bulk_pause_start_ms=PAUSE_MS[0], bulk_pause_end_ms=PAUSE_MS[1]),
    }
    binaries = {'focal': ART / 'bin' / f'{variant}-fixture', 'competitor': ART / 'bin' / 'reno-fixture'}
    for who, c in configs.items():
        write(directory / f'{who}-config.json', c)
    write(directory / 'meta.json', dict(phase=phase, path='L6', workload=workload, pair=pair, arm=arm, variant=variant,
                                         controller=controller, seed=seed, tag=tag, affinity=AFFINITY))
    measure_start = start_ns + WARMUP_MS * 1_000_000
    measure_end = measure_start + MEASURE_MS * 1_000_000
    limit_ns = LEAD_NS + (WARMUP_MS + MEASURE_MS + 30_000) * 1_000_000
    processes, commands, streams, relay, pids = [], {}, [], None, {}
    started = time.time_ns()
    try:
        relay_bin = ART / 'bin' / 'relay2'
        cmd = [str(relay_bin), '-scenario', 'L6', '-seed', str(seed)]
        for who in ['focal', 'competitor']:
            a = ADDR[who]
            cmd += [f'-{who}-front', a['front'], f'-{who}-back', a['back'], f'-{who}-sender', a['sender'], f'-{who}-target', a['receiver']]
        cmd += ['-start-unix-ns', str(measure_start), '-until', f'{limit_ns // 1_000_000}ms', '-output', str(directory / 'relay.json')]
        commands['relay'] = cmd
        err = (directory / 'relay.stderr').open('w')
        streams.append(err)
        relay = subprocess.Popen(['taskset', '-c', AFFINITY['relay']] + cmd, stdout=subprocess.DEVNULL, stderr=err)
        wait_ready(relay, directory / 'relay.stderr', 'relay ready')
        for role in ROLES:
            who, kind = role.split('-')
            a = ADDR[who]
            cmd = [str(binaries[who]), '-config', str(directory / f'{who}-config.json'),
                   '-cert', str(ART / 'campaign.pem'), '-key', str(ART / 'campaign-key.pem'), '-trace=false',
                   '-role', kind, '-local', a['receiver' if kind == 'receive' else 'sender'], '-output', str(directory / f'{role}.json')]
            if kind == 'send':
                cmd += ['-peer', a['front']]
            commands[role] = cmd
            out = (directory / f'{role}.stdout').open('w')
            err = (directory / f'{role}.stderr').open('w')
            streams += [out, err]
            # The launcher keeps the runner's resident high-water mark out of the endpoint's ru_maxrss.
            proc = subprocess.Popen(['taskset', '-c', AFFINITY[role], str(ART / 'bin/launch'), str(directory / f'{role}.pid'), '--'] + cmd,
                                    stdout=out, stderr=err)
            processes.append(proc)
            pids[role] = read_pid(directory / f'{role}.pid', proc)
            if kind == 'receive':
                wait_ready(proc, directory / f'{role}.stderr', 'ready', 1.5)
        all_pids = dict(pids, relay=relay.pid)
        host, facts = [], {}
        snmp_before, ticks_before = udp_snmp(), all_proc_ticks()
        while any(p.poll() is None for p in processes):
            if time.time_ns() - started > limit_ns:
                raise TimeoutError(name)
            now = time.time_ns()
            host.append(dict(unix_ns=now, cpus=proc_stat_cpus(), fixture_ticks={r: proc_cpu_ticks(p) for r, p in all_pids.items()},
                             vm_hwm_kib={r: vm_hwm_kib(p) for r, p in pids.items()}))
            if not facts.get('mid') and now >= measure_start + MEASURE_MS * 500_000:
                facts['mid'] = dict(unix_ns=now, processes={r: process_facts(p) for r, p in all_pids.items()}, sockets=socket_facts())
            time.sleep(max(0.001, ((now // 1_000_000_000 + 1) * 1_000_000_000 - time.time_ns()) / 1e9))
        relay.send_signal(signal.SIGTERM)
        relay.wait(timeout=5)
        ticks_after = all_proc_ticks()
        own = set(all_pids.values()) | {p.pid for p in processes}
        foreign = {pid: (t - ticks_before.get(pid, (0,))[0], comm) for pid, (t, comm) in ticks_after.items() if int(pid) not in own}
        facts.update(udp_before=snmp_before, udp_after=udp_snmp(), measure_window_unix_ns=[measure_start, measure_end],
                     top_foreign_ticks=sorted(([c, t, pid] for pid, (t, c) in foreign.items() if t > 0), key=lambda x: -x[1])[:20])
        write(directory / 'host-cpu.json', host)
        write(directory / 'host-facts.json', facts)
        write(directory / 'receipt.json', dict(commands=commands, start_unix_ns=started, end_unix_ns=time.time_ns(), roles=ROLES,
              exit_codes=[p.returncode for p in processes], relay_exit=relay.returncode,
              binary_sha256={who: hashlib.sha256(b.read_bytes()).hexdigest() for who, b in binaries.items()},
              relay_sha256=hashlib.sha256(relay_bin.read_bytes()).hexdigest()))
        assert all(p.returncode == 0 for p in processes), name
        assert relay.returncode == 0, name
        row = summarize(directory)
        print(json.dumps(dict(id=name, focal_mbps={k: round(v['mbps'], 1) for k, v in row['focal']['periods'].items()},
                              competitor_mbps={k: round(v['mbps'], 1) for k, v in row['competitor']['periods'].items()},
                              focal_p95_ms=row['focal']['control_p95_ms'], competitor_p95_ms=row['competitor']['control_p95_ms'])), flush=True)
        return row
    finally:
        for proc in processes + ([relay] if relay else []):
            stop(proc)
        for s in streams:
            s.close()


def ensure_credentials():
    if not (ART / 'campaign-key.pem').exists():
        subprocess.run([str(ART / 'bin/reno-fixture'), '-role', 'cert'], cwd=ART, check=True)
