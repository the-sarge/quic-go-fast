#!/usr/bin/env python3
"""Finite, local-only reproduction aid for the Wayfinder investigation.

Artifacts and temporary credentials stay in this owned worktree's .local.
This is an experiment receipt, not a maintained benchmark framework.
"""
import argparse
import hashlib
import json
import math
import pathlib
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parents[3]
ART = ROOT / '.local/bbr-local-reproduction'


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
    start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    end = start + cfg['measure_ms'] * 1_000_000
    gib = delivery['useful_bytes'] / 2**30
    control = tx['result']['control']
    lat = sorted(control['latency_ns'])
    row = dict(id=cfg['id'], controller=cfg['controller'], workload=cfg['workload'],
               useful_bytes=delivery['useful_bytes'],
               goodput_mbps=delivery['useful_bytes'] * 8 / (cfg['measure_ms'] / 1000) / 1e6,
               control_p95_ms=lat[math.ceil(len(lat) * .95) - 1] / 1e6,
               control_replies=control['replies'], control_skipped=control['skipped'],
               control_unresolved=control['unresolved'],
               control_missed=control['missed_opportunities'])
    for role, rec in [('send', tx), ('receive', rx)]:
        samples = [s for s in rec['resource_samples'] if start <= s['unix_ns'] <= end]
        # Full-run CPU / measured delivery matches the retained campaign metric.
        # Samples supply a strict interior observation; do not invent an exit clock.
        row[role] = dict(interior_cpu_seconds=samples[-1]['cpu_seconds'] - samples[0]['cpu_seconds'],
                         interior_cpu_span_seconds=(samples[-1]['unix_ns'] - samples[0]['unix_ns']) / 1e9,
                         total_cpu_seconds=rec['resources']['cpu_seconds'],
                         total_cpu_seconds_per_measured_gib=rec['resources']['cpu_seconds'] / gib,
                         peak_rss_bytes=rec['resources']['peak_rss_bytes'],
                         total_alloc_bytes=rec['resources']['total_alloc_bytes'],
                         total_alloc_bytes_per_measured_gib=rec['resources']['total_alloc_bytes'] / gib,
                         lost_packets=rec['trace']['lost_packets'] if rec.get('trace_enabled', True) else None)
    write(directory / 'summary.json', row)
    return row


def run_case(binary, phase, workload, pair, controller, extra):
    name = f'{phase}-{workload}-p{pair}-{controller}'
    directory = ART / name
    if directory.exists():
        receipt = json.loads((directory / 'receipt.json').read_text())
        assert receipt['exit_codes'] == [0, 0], 'retain failed attempt; use a fresh phase'
        row = summarize(directory)
        print(json.dumps(row), flush=True)
        return row
    directory.mkdir()  # Refuse to overwrite a prior attempt.
    cfg = dict(id=name, controller=controller, workload=workload,
               start_unix_ns=time.time_ns() + 2_000_000_000,
               warmup_ms=5000, measure_ms=20000,
               payload_bytes=16384 if workload == 'stream' else 1200)
    write(directory / 'config.json', cfg)
    common = [str(binary), '-config', str(directory / 'config.json'),
              '-cert', str(ART / 'campaign.pem'), '-key', str(ART / 'campaign-key.pem')]
    processes = []
    commands = {}
    streams = []
    started = time.time_ns()
    try:
        for role, addr in [('receive', '127.0.0.1:23721'), ('send', '127.0.0.1:23722')]:
            cmd = common + ['-role', role, '-local', addr, '-output', str(directory / f'{role}.json')]
            if role == 'send':
                cmd += ['-peer', '127.0.0.1:23721']
            cmd += extra
            if phase == 'profile':
                cmd += ['-cpu-profile', str(directory / f'{role}.cpu.pprof'),
                        '-alloc-profile', str(directory / f'{role}.alloc.pprof')]
            commands[role] = cmd
            out = (directory / f'{role}.stdout').open('w')
            err = (directory / f'{role}.stderr').open('w')
            streams += [out, err]
            proc = subprocess.Popen(cmd, stdout=out, stderr=err)
            processes.append(proc)
            if role == 'receive':
                deadline = time.monotonic() + 1.5
                while 'ready' not in (directory / 'receive.stderr').read_text():
                    if proc.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError('receiver not ready')
                    time.sleep(.02)
        host = []
        while any(p.poll() is None for p in processes):
            if time.time_ns() - started > 50_000_000_000:
                raise TimeoutError(name)
            host.append(dict(unix_ns=time.time_ns(), processes=subprocess.check_output(
                ['ps', '-axo', 'pid,pcpu,comm', '-r'], text=True).splitlines()[:16]))
            time.sleep(1)
        write(directory / 'host-cpu.json', host)
        write(directory / 'receipt.json', dict(commands=commands, start_unix_ns=started,
              end_unix_ns=time.time_ns(), exit_codes=[p.returncode for p in processes],
              binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest()))
        assert all(p.returncode == 0 for p in processes), name
        row = summarize(directory)
        print(json.dumps(row), flush=True)
        return row
    finally:
        for proc in processes:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
        for stream in streams:
            stream.close()


parser = argparse.ArgumentParser()
parser.add_argument('phase', choices=['frozen', 'trace-on', 'trace-off', 'profile', 'contention'])
args = parser.parse_args()
ART.mkdir(exist_ok=True, parents=True)
local_ignore = ROOT / '.local/.gitignore'
if not local_ignore.exists():
    local_ignore.write_text('*\n')
binary = ART / ('frozen-fixture' if args.phase == 'frozen' else 'diagnostic-fixture')
if not (ART / 'campaign-key.pem').exists():
    subprocess.run([str(binary), '-role', 'cert'], cwd=ART, check=True)
extra = [] if args.phase == 'frozen' else ['-trace=' + ('true' if args.phase == 'trace-on' else 'false')]
rows = []
competitors = []
try:
    if args.phase == 'contention' and not (ART / 'contention-summary.json').exists():
        if (ART / 'contention-competitors.json').exists():
            raise RuntimeError('retain partial contention attempt; replay in a fresh owned worktree')
        code = 'import time\nend=time.monotonic()+480\nx=1\nwhile time.monotonic()<end:\n x=(x*1664525+1013904223)&0xffffffff'
        competitors = [subprocess.Popen([sys.executable, '-c', code]) for _ in range(4)]
        write(ART / 'contention-competitors.json', dict(pids=[p.pid for p in competitors],
              executable=sys.executable, code=code, maximum_seconds=480))
    for workload in ['stream', 'datagram']:
        for pair in range(1, (2 if args.phase in ['profile', 'trace-on'] else 4)):
            for controller in (['reno', 'bbrv3'] if pair % 2 else ['bbrv3', 'reno']):
                rows.append(run_case(binary, args.phase, workload, pair, controller, extra))
finally:
    for proc in competitors:
        if proc.poll() is None:
            proc.terminate()
        proc.wait()
    if competitors:
        write(ART / 'contention-cleanup.json', dict(exit_codes=[p.returncode for p in competitors],
              all_stopped=all(p.poll() is not None for p in competitors)))
write(ART / f'{args.phase}-summary.json', rows)
