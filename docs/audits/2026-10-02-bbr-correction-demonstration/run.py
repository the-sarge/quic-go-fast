#!/usr/bin/env python3
"""Finite, local-only demonstration runner for one Wayfinder ticket.

Adapted from the causal-diagnosis run aid: the same endpoint roles, fixture
configuration and receiver checks, plus an optional frozen-model relay path and
counted-work capture. Artifacts and temporary credentials stay in this owned
worktree's .local. This is an experiment receipt, not a benchmark framework.
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
ART = ROOT / '.local/bbr-correction-demonstration'
OBS = ART / 'observations'
RECEIVER, SENDER, FRONT, BACK = '127.0.0.1:24721', '127.0.0.1:24722', '127.0.0.1:24731', '127.0.0.1:24732'
PATHS = {'loopback': dict(warmup_ms=5000, measure_ms=20000), 'S6': dict(warmup_ms=10000, measure_ms=30000),
         'S5': dict(warmup_ms=10000, measure_ms=30000)}


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
    for role in ['send', 'receive']:
        work = directory / f'{role}.work.json'
        if work.exists():
            row[role]['work'] = json.loads(work.read_text())
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


def run_case(phase, variant, workload, pair, controller, path='loopback', seed=None, profile=False, heap=False, strip_ecn=False):
    """Run one matched observation; refuse to replace any prior attempt."""
    name = f'{phase}-{path}-{workload}-p{pair}-{variant}-{controller}'
    if strip_ecn:
        name += '-noecn'
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
                                         profiled=profile or heap, counted=variant.endswith('-counted'), strip_ecn=strip_ecn))
    common = [str(binary), '-config', str(directory / 'config.json'),
              '-cert', str(ART / 'campaign.pem'), '-key', str(ART / 'campaign-key.pem'), '-trace=false']
    processes, commands, streams, relay = [], {}, [], None
    started = time.time_ns()
    limit_ns = (timing['warmup_ms'] + timing['measure_ms'] + 30_000) * 1_000_000
    try:
        if path != 'loopback':
            # One emulator binary per comparison; relay-v2 only adds the opt-in ECN strip.
            relay_bin = ART / 'bin' / ('relay-v2' if strip_ecn else 'frozen-relay')
            cmd = [str(relay_bin), '-scenario', path, '-seed', str(seed), '-front', FRONT, '-back', BACK,
                   '-sender', SENDER, '-target', RECEIVER, '-start-unix-ns', str(cfg['start_unix_ns']),
                   '-until', f'{limit_ns // 1_000_000}ms', '-output', str(directory / 'relay.json')]
            if strip_ecn:
                cmd.append('-strip-ecn')
            commands['relay'] = cmd
            err = (directory / 'relay.stderr').open('w')
            streams.append(err)
            relay = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=err)
            wait_ready(relay, directory / 'relay.stderr', 'relay ready')
        for role, addr in [('receive', RECEIVER), ('send', SENDER)]:
            cmd = common + ['-role', role, '-local', addr, '-output', str(directory / f'{role}.json')]
            if role == 'send':
                cmd += ['-peer', FRONT if relay else RECEIVER]
            if profile:
                cmd += ['-cpu-profile', str(directory / f'{role}.cpu.pprof'), '-alloc-profile', str(directory / f'{role}.alloc.pprof')]
            if heap:
                cmd += ['-heap-profile', str(directory / f'{role}.heap.pprof')]
            env = None
            if variant.endswith('-counted'):
                env = {**os.environ, 'BBR_WORK_OUTPUT': str(directory / f'{role}.work.json')}
            commands[role] = cmd
            out = (directory / f'{role}.stdout').open('w')
            err = (directory / f'{role}.stderr').open('w')
            streams += [out, err]
            proc = subprocess.Popen(cmd, stdout=out, stderr=err, env=env)
            processes.append(proc)
            if role == 'receive':
                wait_ready(proc, directory / 'receive.stderr', 'ready', 1.5)
        host = []
        while any(p.poll() is None for p in processes):
            if time.time_ns() - started > limit_ns:
                raise TimeoutError(name)
            host.append(dict(unix_ns=time.time_ns(), processes=subprocess.check_output(
                ['ps', '-axo', 'pid,pcpu,comm', '-r'], text=True).splitlines()[:16]))
            time.sleep(1)
        if relay:
            relay.send_signal(signal.SIGTERM)
            relay.wait(timeout=5)
        write(directory / 'host-cpu.json', host)
        write(directory / 'receipt.json', dict(commands=commands, start_unix_ns=started, end_unix_ns=time.time_ns(),
              exit_codes=[p.returncode for p in processes], relay_exit=relay.returncode if relay else None,
              binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
              relay_sha256=hashlib.sha256(pathlib.Path(commands['relay'][0]).read_bytes()).hexdigest() if relay else None))
        assert all(p.returncode == 0 for p in processes), name
        assert relay is None or relay.returncode == 0, name
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
        subprocess.run([str(ART / 'bin/frozen-fixture'), '-role', 'cert'], cwd=ART, check=True)
