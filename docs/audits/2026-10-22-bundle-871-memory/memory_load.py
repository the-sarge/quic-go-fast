#!/usr/bin/env python3
"""Validate retained #712 Linux observations and apply the registered readiness rule (unchanged from #711).

Linux changes: platform and launcher checks, the A/A frozen-Reno arm keyed by tag
(reported beside every ratio, never overriding the median rule), and the registered
contamination rule with a per-flag sensitivity check.
"""
import json
import math
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import OBS, summarize  # noqa: E402
from localize import contamination  # noqa: E402

HERE = Path(__file__).resolve().parent
LIMITS = dict(goodput=0.95, cpu=1.10, rss=1.10, control_p95=1.20)
BOTTLENECK_BPS = 100e6
ECN = ['not_ect', 'ect1', 'ect0', 'ce']  # relay index = codepoint & 3


def stats(values):
    values = [v for v in values if v is not None]
    return dict(median=statistics.median(values), minimum=min(values), maximum=max(values), n=len(values)) if values else None


def load(d):
    receipt = json.loads((d / 'receipt.json').read_text())
    assert receipt['exit_codes'] == [0, 0], d
    assert receipt.get('relay_exit') in (None, 0), d
    for role in ['send', 'receive']:
        rec = json.loads((d / f'{role}.json').read_text())
        assert rec['gomaxprocs'] == 4 and rec['managed_direct'], d
        assert rec['go_version'] == 'go1.27.0' and rec['platform'] == 'linux/amd64', d
        assert rec['trace_enabled'] is False, d
    row = summarize(d)  # asserts receiver integrity and relay error-freedom
    cfg = json.loads((d / 'config.json').read_text())
    facts = json.loads((d / 'host-facts.json').read_text())
    row['contamination'] = contamination(json.loads((d / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])
    row['udp_errors'] = {k: facts['udp_after'][k] - facts['udp_before'][k] for k in ['RcvbufErrors', 'SndbufErrors', 'InErrors', 'MemErrors']}
    row['arm'] = '-'.join([row['variant'], row['controller']] + ([row['tag']] if row.get('tag') else []))
    lo, hi = cfg['warmup_ms'], cfg['warmup_ms'] + cfg['measure_ms']
    if row['path'] != 'loopback':
        relay = json.loads((d / 'relay.json').read_text())
        fwd = relay['Forward']['Stats']
        measured = [s for s in relay['QueueSamples'] if lo <= s[0] < hi]
        delay = sorted(s[1] * 8 / BOTTLENECK_BPS * 1e3 for s in measured)
        stalls = sum(1 for s in relay.get('ForwardDelivery', []) if s[3] > 5_000_000)
        row['relay'].update(
            forward_queue_delay_ms=dict(p50=delay[len(delay) // 2], p95=delay[math.ceil(len(delay) * .95) - 1],
                                        maximum=delay[-1], mean=statistics.fmean(delay), samples=len(delay)),
            overflow_rate=fwd['Overflow'] / fwd['Received'], random_drop_rate=fwd['RandomDrop'] / fwd['Received'],
            forward_received=fwd['Received'], stall_intervals=stalls,
            ecn={d.lower(): dict(zip(ECN, relay[d]['ECNIn'])) for d in ['Forward', 'Reverse']})
        fd = relay.get('ForwardDelivery') or []
        before = [s[4] for s in fd if s[0] < lo]
        during = [s[4] for s in fd if s[0] < hi]
        if before and during:
            row['relay']['overflow_measured'] = during[-1] - before[-1]
        fwd_ecn = row['relay']['ecn']['forward']
        row['relay']['forward_ect_share'] = (fwd_ecn['ect0'] + fwd_ecn['ect1'] + fwd_ecn['ce']) / max(1, sum(fwd_ecn.values()))
        row['utilization'] = row['goodput_mbps'] * 1e6 / BOTTLENECK_BPS
    measured_start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    for role in ['send', 'receive']:
        samples = json.loads((d / f'{role}.json').read_text())['resource_samples']
        warm = [x['heap_bytes'] for x in samples if x['unix_ns'] < measured_start]
        meas = [x['heap_bytes'] for x in samples if x['unix_ns'] >= measured_start]
        row[role]['heap_peak_warmup_mib'] = max(warm) / 2**20 if warm else None
        row[role]['heap_peak_measured_mib'] = max(meas) / 2**20 if meas else None
    return row
