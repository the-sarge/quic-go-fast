#!/usr/bin/env python3
"""#740 memory-attribution runner: #715's run_case_x (stage_run.py, unchanged) copied with recorded changes.

- MEM_OUTPUT for both endpoints of a `-mem` variant: the per-second memory
  accounting series, {role}.mem.jsonl;
- the receiver-controller arm (tag `rxreno`): MEM_LOCAL_CONTROLLER=reno for the
  receiver only, so the receiver's congestion controller is Reno while the shared
  run configuration, the sender and the build stay the candidate's; meta.json
  records the receiver's controller;
- no perf, thread sampling or timeline output (D6's memory stages use none);
- summarize_m: #712's run.summarize with one assertion changed. The fixture
  reports each endpoint's effective controller; #712 requires both to equal the
  run configuration's, and summarize_m requires the sender's to equal it and the
  receiver's to equal meta.json's `receive_controller` (Reno in the
  receiver-controller arm, the configuration's otherwise).
Everything else (roles, affinity, launcher, relay, contamination sampling,
receipts, refusal to replace an attempt) is #712's, through #715's copy.
"""
import hashlib
import inspect
import json
import os
import pathlib
import signal
import subprocess
import time

import art  # noqa: F401  (redirects run.ART/OBS)
import run
from run import (AFFINITY, BACK, FRONT, HARNESS_CPUS, PERF_STAT_EVENTS, RECEIVER, SENDER, all_proc_ticks, proc_cpu_ticks,
                 proc_stat_cpus, process_facts, read_pid, socket_facts, stop, stop_perf, summarize, udp_snmp, vm_hwm_kib,
                 wait_ready, write)

PATHS = dict(run.PATHS, **{'loopback-long': dict(warmup_ms=5000, measure_ms=120000)})

_SUMMARIZE = inspect.getsource(run.summarize)
_ASSERT = "    assert tx['result']['controller'] == rx['result']['controller'] == cfg['controller']\n"
assert _SUMMARIZE.count(_ASSERT) == 1
_ns = dict(vars(run))
exec(_SUMMARIZE.replace('def summarize(', 'def summarize_m(').replace(_ASSERT,
     "    assert tx['result']['controller'] == cfg['controller']\n"
     "    assert rx['result']['controller'] == json.loads((directory / 'meta.json').read_text()).get('receive_controller', cfg['controller'])\n"),
     _ns)
summarize_m = _ns['summarize_m']


def thread_sample(pid):
    """{tid: [run_ns, wait_ns, ticks, comm]} for one process; empty if it has exited."""
    out = {}
    try:
        for t in pathlib.Path(f'/proc/{pid}/task').iterdir():
            try:
                run_ns, wait_ns, _ = (t / 'schedstat').read_text().split()
                stat = (t / 'stat').read_text().rsplit(')', 1)[1].split()
                out[t.name] = [int(run_ns), int(wait_ns), int(stat[11]) + int(stat[12]), (t / 'comm').read_text().strip()]
            except (FileNotFoundError, ProcessLookupError, ValueError):
                pass
    except (FileNotFoundError, ProcessLookupError):
        pass
    return out


def start_perf_x(kind, pid, directory, role):
    """Attach root perf to an unprivileged endpoint PID; perf_event_paranoid is unchanged."""
    procs = []
    if kind in ('stat', 'both'):
        cmd = ['sudo', '-n', 'taskset', '-c', HARNESS_CPUS, 'perf', 'stat', '-x,', '-e', ','.join(PERF_STAT_EVENTS),
               '-p', str(pid), '-o', str(directory / f'{role}.perf-stat.csv')]
        procs.append((cmd, subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)))
    if kind in ('record', 'both'):
        cmd = ['sudo', '-n', 'taskset', '-c', HARNESS_CPUS, 'perf', 'record', '-e', 'cycles', '-F', '4999', '--call-graph', 'fp',
               '-p', str(pid), '-o', str(directory / f'{role}.perf.data'), '--quiet']
        procs.append((cmd, subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)))
    return procs


def run_case_m(phase, variant, workload, pair, controller, path='loopback', seed=None, tag=''):
    heap_series, perf, threads = False, None, False
    """Run one matched attribution observation; refuse to replace any prior attempt."""
    name = f'{phase}-{path}-{workload}-p{pair}-{variant}-{controller}' + (f'-{tag}' if tag else '')
    directory = run.OBS / name
    if directory.exists():
        receipt = json.loads((directory / 'receipt.json').read_text())
        assert receipt['exit_codes'] == [0, 0], 'retain failed attempt; use a fresh phase'
        return summarize_m(directory)
    directory.mkdir(parents=True)
    binary = run.ART / 'bin' / f'{variant}-fixture'
    timing = PATHS[path]
    loopback = path.startswith('loopback')
    cfg = dict(id=name, controller=controller, workload=workload, start_unix_ns=time.time_ns() + 2_000_000_000,
               payload_bytes=16384 if workload == 'stream' else 1200, **timing)
    write(directory / 'config.json', cfg)
    write(directory / 'meta.json', dict(phase=phase, variant=variant, pair=pair, path=path, seed=seed,
                                         profiled=heap_series or perf in ('record', 'both'), heap_series=heap_series,
                                         diag='-diag' in variant, perf=perf, threads=threads, tag=tag, affinity=AFFINITY,
                                         timeline=False, mem='-mem' in variant,
                                         receive_controller='reno' if tag == 'rxreno' else controller))
    common = [str(binary), '-config', str(directory / 'config.json'),
              '-cert', str(run.ART / 'campaign.pem'), '-key', str(run.ART / 'campaign-key.pem'), '-trace=false']
    processes, commands, streams, relay, endpoint_pids = [], {}, [], None, {}
    started = time.time_ns()
    limit_ns = (timing['warmup_ms'] + timing['measure_ms'] + 30_000) * 1_000_000
    try:
        if not loopback:
            relay_bin = run.ART / 'bin' / 'relay-linux'
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
                cmd += ['-peer', RECEIVER if loopback else FRONT]
            if heap_series:
                (directory / f'{role}-heap').mkdir()
                cmd += ['-heap-series-dir', str(directory / f'{role}-heap')]
            env = None
            if '-mem' in variant:
                env = {**os.environ, 'MEM_OUTPUT': str(directory / f'{role}.mem.jsonl')}
                if tag == 'rxreno' and role == 'receive':
                    env['MEM_LOCAL_CONTROLLER'] = 'reno'
            assert tag != 'rxreno' or '-mem' in variant, 'the receiver-controller arm needs a -mem build'
            commands[role] = cmd
            out = (directory / f'{role}.stdout').open('w')
            err = (directory / f'{role}.stderr').open('w')
            streams += [out, err]
            proc = subprocess.Popen(['taskset', '-c', AFFINITY[role], str(run.ART / 'bin/launch'), str(directory / f'{role}.pid'), '--'] + cmd,
                                    stdout=out, stderr=err, env=env)
            processes.append(proc)
            endpoint_pids[role] = read_pid(directory / f'{role}.pid', proc)
            if role == 'receive':
                wait_ready(proc, directory / 'receive.stderr', 'ready', 1.5)
        pids = dict(endpoint_pids, **({'relay': relay.pid} if relay else {}))
        measure_start = cfg['start_unix_ns'] + timing['warmup_ms'] * 1_000_000
        measure_end = measure_start + timing['measure_ms'] * 1_000_000
        host, facts, perfs, thread_rows = [], {}, {}, []
        snmp_before, ticks_before = udp_snmp(), all_proc_ticks()
        while any(p.poll() is None for p in processes):
            if time.time_ns() - started > limit_ns:
                raise TimeoutError(name)
            now = time.time_ns()
            host.append(dict(unix_ns=now, cpus=proc_stat_cpus(), fixture_ticks={r: proc_cpu_ticks(p) for r, p in pids.items()},
                             vm_hwm_kib={r: vm_hwm_kib(endpoint_pids[r]) for r in ['send', 'receive']}))
            if threads:
                thread_rows.append(dict(unix_ns=time.time_ns(), threads={r: thread_sample(endpoint_pids[r]) for r in ['send', 'receive']}))
            if perf and not perfs and now >= measure_start:
                for role in ['send', 'receive']:
                    perfs[role] = start_perf_x(perf, pids[role], directory, role)
                commands.update({f'perf-{r}-{i}': c for r, ps in perfs.items() for i, (c, _) in enumerate(ps)})
            if perfs and 'perf_stopped' not in facts and now >= measure_end:
                facts['perf_stopped'] = {f'{r}-{i}': stop_perf(p) for r, ps in perfs.items() for i, (_, p) in enumerate(ps)}
            if not facts.get('mid') and now >= measure_start + timing['measure_ms'] * 500_000:
                facts['mid'] = dict(unix_ns=now, processes={r: process_facts(p) for r, p in pids.items()}, sockets=socket_facts())
            wake = [(now // 1_000_000_000 + 1) * 1_000_000_000]
            if perf and not perfs:
                wake.append(measure_start)
            elif perfs and 'perf_stopped' not in facts:
                wake.append(measure_end)
            if threads:
                wake += [measure_start, measure_end]
            time.sleep(max(0.001, (min(w for w in wake if w > now) - time.time_ns()) / 1e9) if any(w > now for w in wake) else 0.001)
        if perfs and 'perf_stopped' not in facts:
            facts['perf_stopped'] = {f'{r}-{i}': stop_perf(p) for r, ps in perfs.items() for i, (_, p) in enumerate(ps)}
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
        if threads:
            write(directory / 'threads.json', thread_rows)
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
        row = summarize_m(directory)
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
