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
ART = ROOT / '.local/bbr-causal-diagnosis'


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
        print(json.dumps(dict(id=row['id'], goodput_mbps=row['goodput_mbps'], sender_cpu_per_gib=row['send']['total_cpu_seconds_per_measured_gib'], control_p95_ms=row['control_p95_ms'])), flush=True)
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
        for role, addr in [('receive', '127.0.0.1:24721'), ('send', '127.0.0.1:24722')]:
            cmd = common + ['-role', role, '-local', addr, '-output', str(directory / f'{role}.json')]
            if role == 'send':
                cmd += ['-peer', '127.0.0.1:24721']
            cmd += extra
            if phase.startswith('profile') or phase.startswith('diagnostics'):
                cmd += ['-cpu-profile', str(directory / f'{role}.cpu.pprof'),
                        '-alloc-profile', str(directory / f'{role}.alloc.pprof')]
            if phase.startswith('diagnostics'):
                cmd += ['-heap-profile',str(directory / f'{role}.heap.pprof')]
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
        print(json.dumps(dict(id=row['id'], goodput_mbps=row['goodput_mbps'], sender_cpu_per_gib=row['send']['total_cpu_seconds_per_measured_gib'], control_p95_ms=row['control_p95_ms'])), flush=True)
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
parser.add_argument('phase')
parser.add_argument('--binary', default='baseline')
parser.add_argument('--controllers', nargs='+', default=['reno','bbrv3'])
parser.add_argument('--pairs', type=int, default=1)
parser.add_argument('--workloads', nargs='+', default=['stream','datagram'])
parser.add_argument('--trace', action='store_true')
args = parser.parse_args()
ART.mkdir(exist_ok=True, parents=True)
binary = ART / args.binary
if not (ART / 'campaign-key.pem').exists():
 subprocess.run([str(binary), '-role', 'cert'], cwd=ART, check=True)
rows=[]
for pair in range(1,args.pairs+1):
 for workload in args.workloads:
  for controller in args.controllers if pair%2 else list(reversed(args.controllers)):
   extra=['-trace='+('true' if args.trace else 'false')]
   rows.append(run_case(binary,args.phase,workload,pair,controller,extra))
write(ART/f'{args.phase}-summary.json',rows)
