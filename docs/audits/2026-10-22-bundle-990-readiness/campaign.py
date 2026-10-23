#!/usr/bin/env python3
"""Finite #990 commands; retained receipts, complete blocks and closed gates.

Historical runners and formulas stay separate. This adapter owns registration
identity, bounded execution and selected-block completeness. It grants no
operator decision. Invoke one block at a time and inspect its receipt.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
import time
from pathlib import Path

import art  # noqa: F401
import build
import guards
import matrix
import run
from localize import contamination

HERE = Path(__file__).resolve().parent
DECISION = 'https://github.com/the-sarge/quic-go-fast/issues/887#issuecomment-6354232820'
MEMORY = 'https://github.com/the-sarge/quic-go-fast/issues/871#issuecomment-6354210729'


class CoordinationStop(RuntimeError):
    """A recognized window boundary; never itself grants another observation."""


def benefit_outcome(blocks, pairs_above):
    if blocks != 5:
        return 'evidence gap'
    return 'met' if pairs_above == 5 else 'not met'


def blocking_outcomes(entries):
    out = []
    for e in entries:
        if not e['role'].endswith(' blocking'):
            continue
        cell = dict(path=e['path'], workload=e['workload'], arm=e['arm'])
        if not e['complete']:
            out.append(dict(cell, criterion='complete readiness', outcome='evidence gap', blocks=e['blocks']))
        for key, value in e['flags'].items():
            if value['raised']:
                out.append(dict(cell, criterion=key, outcome='raised', median=value['median']))
        if e.get('benefit_outcome') in ('not met', 'evidence gap'):
            out.append(dict(cell, criterion='S6 repeatable benefit', outcome=e['benefit_outcome'],
                            pairs_above=e['useful_benefit']['pairs_above_reno'], blocks=e['blocks']))
    for workload in ['stream', 'datagram']:
        if not any((e['path'], e['workload'], e['arm']) == ('S6', workload, 'cand-bbrv3') for e in entries):
            out.append(dict(path='S6', workload=workload, arm='cand-bbrv3', criterion='S6 repeatable benefit', outcome='evidence gap', blocks=0))
    return out


def source(variant):
    rev, diag, timeline = build.VARIANTS[variant]
    return rev + ('+diag' if diag else '') + ('+timeline' if timeline else '')


def preflight(calibration=True, ready=False):
    gate = json.loads((run.ART / 'pre-data-gates.json').read_text())
    assert gate['candidate'] == build.REVISIONS['cand'] == build.REVISIONS['reno']
    assert gate['baseline_decision'] == DECISION and gate['memory_disposition'] == MEMORY
    assert len(gate['registration_commit']) == 40 and gate['review_url'] and gate['resource_authorization']
    now = datetime.now(timezone.utc)
    if not datetime.fromisoformat(gate['window_start']) <= now < datetime.fromisoformat(gate['window_end']):
        raise CoordinationStop('outside registered window')
    assert os.sched_getaffinity(0) == {1, 2, 3}, 'launch under taskset -c 1-3'
    assert not {k for k in os.environ if k in ['GOGC', 'GOMEMLIMIT', 'GODEBUG'] or k.startswith('QUIC_GO_')}
    assert {'campaign.py', 'guards.py', 'build.py', 'matrix.py', 'stages.py', 'README.md'} <= set(gate['registration_files'])
    for rel, digest in gate['registration_files'].items():
        assert hashlib.sha256((HERE / rel).read_bytes()).hexdigest() == digest, ('registration identity', rel)
    for variant in build.VARIANTS:
        receipt = json.loads((run.ART / 'bin' / f'{variant}-build.json').read_text())
        assert receipt['revision'] == build.VARIANTS[variant][0]
        item = receipt['builds']['fixture']
        expected = build.REGISTERED.get((variant, 'fixture'), build.EXPECTED.get((variant, 'fixture')))
        assert expected == item['sha256'] == hashlib.sha256((run.ART / item['binary']).read_bytes()).hexdigest()
    relay = json.loads((run.ART / 'bin/relay-build.json').read_text())
    for name, item in relay['builds'].items():
        assert item['sha256'] == build.EXPECTED[('relay', name)] == hashlib.sha256((run.ART / item['binary']).read_bytes()).hexdigest()
    if calibration:
        guards.validate_calibration(json.loads((run.ART / 'prereq/host-facts.json').read_text()))
    if ready:
        accepted = json.loads((run.ART / 'prerequisites-accepted.json').read_text())
        assert accepted['registration_commit'] == gate['registration_commit'] and accepted['evidence_url']
        assert accepted['outcome'] == 'pass'
    return gate


def validate_observation(directory, gate=None):
    meta = json.loads((directory / 'meta.json').read_text())
    cfg = json.loads((directory / 'config.json').read_text())
    receipt = json.loads((directory / 'receipt.json').read_text())
    variant = meta['variant']
    assert variant in build.VARIANTS
    expected = build.REGISTERED.get((variant, 'fixture'), build.EXPECTED.get((variant, 'fixture')))
    assert receipt['binary_sha256'] == expected, 'binary identity'
    assert receipt['exit_codes'] == [0, 0], 'failed endpoint'
    assert receipt.get('relay_exit') in (None, 0), 'failed relay'
    if not meta['path'].startswith('loopback'):
        assert receipt['relay_sha256'] == build.EXPECTED[('relay', 'relay-linux')]
    for role in ['send', 'receive']:
        rec = json.loads((directory / f'{role}.json').read_text())
        assert rec['source'] == source(variant), 'source identity'
        assert rec['go_version'] == 'go1.27.0' and rec['platform'] == 'linux/amd64'
        assert rec['gomaxprocs'] == 4 and rec['managed_direct'] is True and rec['trace_enabled'] is False
        assert rec['result']['controller'] == cfg['controller'], 'controller identity'
        assert rec['result']['run'] == cfg, 'endpoint configuration identity'
        assert not rec.get('error') and not rec['result'].get('errors')
        assert not rec['result']['control'].get('error')
    assert meta['affinity'] == run.AFFINITY
    assert cfg['payload_bytes'] == (16384 if cfg['workload'] == 'stream' else 1200)
    timing = matrix.run_case_x.__globals__['PATHS'][meta['path']]
    assert all(cfg[k] == v for k, v in timing.items())
    facts = json.loads((directory / 'host-facts.json').read_text())
    start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    assert facts['measure_window_unix_ns'] == [start, start + cfg['measure_ms'] * 1_000_000]
    if gate:
        assert receipt['start_unix_ns'] >= datetime.fromisoformat(gate['window_start']).timestamp() * 1e9
        assert receipt['end_unix_ns'] <= datetime.fromisoformat(gate['window_end']).timestamp() * 1e9
    row = run.summarize(directory)
    for counters in [directory / 'send.perf-stat.csv', directory / 'receive.perf-stat.csv']:
        if counters.exists():
            values = run.perf_counters(counters)
            assert values and all(v['value'] is not None and v['running_pct'] is not None and v['running_pct'] >= 95 for v in values.values()), 'inadequate perf counters'
    row['contamination'] = contamination(json.loads((directory / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])
    guards.validate_contamination([row])
    return row


def spec(stage, workload, block, escalation=False):
    phase, path, arms, n, base, runner, opts, only = matrix.STAGES[stage]
    assert workload in ('stream', 'datagram') and (only is None or only == workload)
    assert 1 <= block <= n or (escalation and stage == 's5timeline' and block in (5, 6))
    return phase, path, arms, base + block if base else None, runner or run.run_case, opts


def block_key(stage, workload, block):
    return f'{stage}-{workload}-p{block}'


def validate_block(rows, stage, workload, block, escalation=False):
    phase, path, arms, seed, _, _ = spec(stage, workload, block, escalation)
    expected = {(v, c, t) for v, c, t in arms}
    assert len(rows) == len(arms)
    assert {(r['variant'], r['controller'], r.get('tag', '')) for r in rows} == expected, 'incomplete or duplicate arms'
    assert all((r['phase'], r['path'], r['workload'], r['pair'], r['seed']) == (phase, path, workload, block, seed) for r in rows), 'block identity'
    return guards.validate_contamination(rows)


def selected(stage, workload, block, escalation=False):
    directory = run.ART / 'blocks' / block_key(stage, workload, block)
    original = json.loads((directory / 'original.json').read_text())
    if original.get('fatal'):
        raise AssertionError('fatal original remains blocked')
    chosen = original
    if not original['usable'] or original['contaminated']:
        chosen = json.loads((directory / 'rerun.json').read_text())
    assert chosen['usable'] and chosen['contaminated'] is False and not chosen.get('fatal'), 'unresolved block'
    dirs = [run.ART / d for d in chosen['directories']]
    rows = [validate_observation(d, chosen['pre_data_gates']) for d in dirs]
    assert not validate_block(rows, stage, workload, block, escalation), 'selected contamination'
    return dirs, dict(stage=stage, workload=workload, block=block, counted=chosen['attempt'], status='clean')


def execute(stage, workload, block, rerun=False, escalation=False):
    gate = preflight(ready=True)
    phase, path, arms, seed, runner, opts = spec(stage, workload, block, escalation)
    if phase in ('presrss', 'preslat'):
        plan = json.loads((run.ART / 'conditional-plan.json').read_text())
        assert stage in plan['selected'], 'unregistered conditional stage'
    directory = run.ART / 'blocks' / block_key(stage, workload, block)
    attempt = 'rerun' if rerun else 'original'
    output = directory / f'{attempt}.json'
    assert not output.exists(), 'never overwrite block receipts'
    root = run.ART / ('observations-rerun' if rerun else 'observations')
    assert not list(root.glob(f'{phase}-{path}-{workload}-p{block}-*')), 'retained partial or existing block'
    if escalation:
        decision = json.loads((run.ART / f'timeline-escalation-{workload}.json').read_text())
        assert decision['decision_url'] and decision['agreeing_blocks'] == 2
    receipts = [json.loads(p.read_text()) for p in (run.ART / 'blocks').glob('*/*.json') if p.name in ('original.json', 'rerun.json')]
    assert sum(not r['usable'] for r in receipts if r['stage'] == stage) <= 3, 'failed attempt stop limit'
    if rerun:
        original = json.loads((directory / 'original.json').read_text())
        assert not original.get('fatal'), 'fatal failure is not rerunnable'
        assert not original['usable'] or original['contaminated'], 'clean block not rerunnable'
        if not original['usable']:
            decision = json.loads((directory / 'failure-disposition.json').read_text())
            assert decision['decision_url'] and decision['explanation']
        if phase == 'main':
            assert sum(r['phase'] == 'main' and r['attempt'] == 'rerun' for r in receipts) < 10
    if phase != 'main':
        count = sum(1 for root2 in [run.ART / 'observations', run.ART / 'observations-rerun'] for p in root2.glob('*/meta.json') if json.loads(p.read_text())['phase'] in ('s5timeline', 'presrss', 'preslat'))
        if count + len(arms) > 60:
            stop = run.ART / 'stage-stops' / f'{block_key(stage, workload, block)}.json'
            stop.parent.mkdir(parents=True, exist_ok=True)
            if not stop.exists():
                run.write(stop, dict(status='not run (cap)', stage=stage, workload=workload, block=block, attempted=count, additional=len(arms), cap=60))
            raise AssertionError('attribution cap')
    run.OBS = root
    run.ensure_credentials()
    directory.mkdir(parents=True, exist_ok=True)
    dirs = []
    def guarded(*args, **kwargs):
        current = preflight(ready=True)
        if (datetime.fromisoformat(current['window_end']) - datetime.now(timezone.utc)).total_seconds() < 180:
            raise CoordinationStop('fewer than 180 seconds remain')
        phase_, variant, workload_, block_, controller = args
        name = f'{phase_}-{path}-{workload_}-p{block_}-{variant}-{controller}' + (f"-{kwargs['tag']}" if kwargs.get('tag') else '')
        d = root / name
        try:
            runner(*args, **kwargs)
            row = validate_observation(d, gate)
        except AssertionError as exc:
            if variant in build.REVISIONS and not path.startswith('loopback') and guards.retained_operational_exit(d, variant, controller):
                raise guards.UnusableObservation(str(d)) from exc
            raise
        dirs.append(str(d.relative_to(run.ART)))
        return row
    receipt = dict(stage=stage, phase=phase, workload=workload, block=block, seed=seed, attempt=attempt, pre_data_gates=gate)
    try:
        rows = matrix.block(phase, path, arms, block, seed=seed, runner=guarded, only=workload, **opts)
        dirty = validate_block(rows, stage, workload, block, escalation)
    except BaseException as exc:
        recognized_stop = isinstance(exc, CoordinationStop) or (isinstance(exc, SystemExit) and str(exc) == 'host never quiet; stage not continued')
        # Successfully appended rows are insufficient: a failed runner may
        # already have created evidence before returning a row.
        started = list(root.glob(f'{phase}-{path}-{workload}-p{block}-*'))
        if recognized_stop and not started:
            stop = directory / f'not-started-{time.time_ns()}.json'
            run.write(stop, dict(receipt, status='not started (coordination stop)', error=f'{type(exc).__name__}: {exc}'))
            raise
        run.write(output, dict(receipt, usable=False, fatal=not isinstance(exc, guards.UnusableObservation), error=f'{type(exc).__name__}: {exc}', directories=dirs))
        raise
    run.write(output, dict(receipt, usable=True, fatal=False, contaminated=dirty, directories=dirs))


def selection(phase, path=None):
    dirs, records = [], []
    for stage, (ph, pa, arms, n, base, runner, opts, only) in matrix.STAGES.items():
        if ph != phase or (path and pa != path):
            continue
        for workload in [only] if only else ['stream', 'datagram']:
            if phase in ('presrss', 'preslat') and not (run.ART / 'blocks' / block_key(stage, workload, 1)).exists():
                continue
            for block in range(1, n + 1):
                ds, record = selected(stage, workload, block)
                dirs += ds
                records.append(record)
            if stage == 's5timeline' and (run.ART / f'timeline-escalation-{workload}.json').exists():
                for block in (5, 6):
                    ds, record = selected(stage, workload, block, True)
                    dirs += ds
                    records.append(record)
    return dirs, records


def reporting_inventory(phase, path=None):
    """Describe every expected block; only strict selected() admits data.

    Reporting an evidence gap does not permit continuation or admit a partial,
    disturbed or corrupt block. Original and rerun artifacts remain untouched.
    """
    dirs, records = [], []
    plan_path = run.ART / 'conditional-plan.json'
    plan = json.loads(plan_path.read_text()) if plan_path.exists() else {'selected': []}
    for stage, (ph, pa, arms, n, base, runner, opts, only) in matrix.STAGES.items():
        if ph != phase or (path and pa != path):
            continue
        if phase in ('presrss', 'preslat') and stage not in plan['selected']:
            continue
        for workload in [only] if only else ['stream', 'datagram']:
            blocks = list(range(1, n + 1))
            if stage == 's5timeline' and (run.ART / f'timeline-escalation-{workload}.json').exists():
                blocks += [5, 6]
            for block in blocks:
                try:
                    ds, record = selected(stage, workload, block, block > n)
                    dirs += ds
                except (AssertionError, OSError, ValueError, KeyError, TypeError) as exc:
                    directory = run.ART / 'blocks' / block_key(stage, workload, block)
                    attempts = [str(p.relative_to(run.ART)) for p in sorted(directory.glob('*.json'))]
                    status = 'incomplete (retained unresolved evidence)' if attempts else 'not run'
                    stop = run.ART / 'stage-stops' / f'{block_key(stage, workload, block)}.json'
                    if stop.exists():
                        status = json.loads(stop.read_text())['status']
                    record = dict(stage=stage, workload=workload, block=block, status=status,
                                  counted=None, attempts=attempts, error=f'{type(exc).__name__}: {exc}')
                records.append(record)
    return dirs, records


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('stage', choices=matrix.STAGES)
    parser.add_argument('workload', choices=['stream', 'datagram'])
    parser.add_argument('block', type=int)
    parser.add_argument('--rerun', action='store_true')
    parser.add_argument('--escalation', action='store_true')
    args = parser.parse_args()
    execute(args.stage, args.workload, args.block, args.rerun, args.escalation)
