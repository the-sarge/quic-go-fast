#!/usr/bin/env python3
"""Bounded #871 adapter: original fixture, block order and analysis formulas.

Never invoke matrix.py's BBR stages. Each block command is a separate finite
unit; inspect its receipt before advancing. Registration and booking are human
gates, not something this script can grant.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path

import art  # noqa: F401 -- redirect before imports bind ART/OBS
import build
import matrix
import memory_load
import run
from rules import choose_block

HERE = Path(__file__).resolve().parent
ARMS = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('port', 'reno', ''),
        ('prior', 'reno', ''), ('cand', 'reno', '')]
ARM_NAMES = ['reno-reno', 'reno-reno-aa', 'port-reno', 'prior-reno', 'cand-reno']
METRICS = ['peak_rss_mib', 'heap_peak_measured_mib', 'heap_peak_warmup_mib']


def preflight():
    assert len(build.REVISIONS['cand']) == 40, 'candidate not pinned'
    assert build.REGISTERED, 'candidate binary not registered'
    # Explicit receipts must be supplied after review and operator booking.
    gate = json.loads((run.ART / 'pre-data-gates.json').read_text())
    for key in ['registration_commit', 'review_url', 'booking_decision', 'window_start', 'window_end']:
        assert gate.get(key), ('missing pre-data gate', key)
    assert gate['candidate'] == build.REVISIONS['cand']
    now = datetime.now(timezone.utc)
    assert datetime.fromisoformat(gate['window_start']) <= now < datetime.fromisoformat(gate['window_end']), 'outside booked window'
    assert os.sched_getaffinity(0) == {1, 2, 3}, 'launch with taskset -c 1-3'
    assert len(gate['registration_commit']) == 40
    # The operator runner supplies these hashes from the reviewed Git commit,
    # allowing the remote artifact directory to remain outside a Git checkout.
    assert {'memory.py', 'memory_load.py', 'build.py', 'README.md'} <= set(gate['registration_files'])
    for name, digest in gate['registration_files'].items():
        assert hashlib.sha256((HERE / name).read_bytes()).hexdigest() == digest, ('unreviewed registration change', name)
    assert not {k: v for k, v in os.environ.items() if k in ['GOGC', 'GOMEMLIMIT', 'GODEBUG'] or k.startswith('QUIC_GO_')}, 'unexpected runtime override'
    for variant in build.REVISIONS:
        receipt = json.loads((run.ART / 'bin' / f'{variant}-build.json').read_text())
        assert receipt['revision'] == build.REVISIONS[variant]
        item = receipt['builds']['fixture']
        assert hashlib.sha256((run.ART / item['binary']).read_bytes()).hexdigest() == item['sha256']
        expected = build.REGISTERED.get((variant, 'fixture'), build.EXPECTED.get((variant, 'fixture')))
        assert expected and item['sha256'] == expected, (variant, 'unregistered binary')
    relay = json.loads((run.ART / 'bin' / 'relay-build.json').read_text())
    for name, item in relay['builds'].items():
        assert item['sha256'] == build.EXPECTED[('relay', name)]
        assert hashlib.sha256((run.ART / item['binary']).read_bytes()).hexdigest() == item['sha256']
    facts = json.loads((run.ART / 'prereq' / 'host-facts.json').read_text())
    calibrations = {c['relay']: c for c in facts['calibration']}
    for name in ['relay-v3', 'relay-linux']:
        c = calibrations[name]
        assert c['exit'] == 0 and not c['report']['errors'], ('calibration error', name)
        for direction in ['forward', 'reverse']:
            report = c['report'][direction]
            assert report['sent'] == [200] * 4 and report['unparsed'] == 0
            expected = [[200 if j == (i if name == 'relay-linux' else 0) else 0 for j in range(4)] for i in range(4)]
            assert report['received'] == expected, ('calibration codepoints', name, direction)
    return gate


def run_block(block, rerun):
    gate = preflight()
    root = run.ART / ('observations-rerun' if rerun else 'observations')
    receipt_path = run.ART / f'memory-block-{block}' / ('rerun.json' if rerun else 'original.json')
    assert not receipt_path.exists(), 'never overwrite a block receipt'
    assert not list(root.glob(f'*-p{block}*')), 'partial/existing block: inspect before any new attempt'
    if rerun:
        original = json.loads((receipt_path.parent / 'original.json').read_text())
        assert not original.get('fatal'), 'identity/integrity failure requires repair, not a rerun'
        assert not original['usable'] or original['contaminated'], 'a clean block cannot be rerun'
        if not original['usable']:
            disposition = json.loads((receipt_path.parent / 'failure-disposition.json').read_text())
            assert disposition.get('decision_url') and disposition.get('explanation'), 'unexpected failure remains blocked'
    attempts = [json.loads(p.read_text()) for p in run.ART.glob('memory-block-*/*.json') if p.name in ['original.json', 'rerun.json']]
    assert sum(not a['usable'] for a in attempts) <= 3, 'failed-observation stop limit'
    run.OBS = root
    run.ensure_credentials()
    def guarded_case(*args, **kwargs):
        # matrix.block checks quiet before reaching this callback. Recheck the
        # booking after any quiet-host wait and before every observation.
        current = preflight()
        remaining = (datetime.fromisoformat(current['window_end']) - datetime.now(timezone.utc)).total_seconds()
        assert remaining >= 180, 'insufficient booked time for another observation'
        return run.run_case(*args, **kwargs)
    receipt_path.parent.mkdir(parents=True, exist_ok=True)
    try:
        rows = matrix.block('main', 'S5', ARMS, block, seed=9000 + block, only='datagram', runner=guarded_case)
        loaded = [memory_load.load(root / row['id']) for row in rows]
        assert {r['arm'] for r in loaded} == set(ARM_NAMES)
        validate_identity(root, loaded, gate)
    except Exception as exc:
        run.write(receipt_path, dict(block=block, seed=9000 + block, pre_data_gates=gate,
                                    usable=False, fatal=isinstance(exc, AssertionError),
                                    error=f'{type(exc).__name__}: {exc}', observations=[]))
        raise
    run.write(receipt_path, dict(block=block, seed=9000 + block, pre_data_gates=gate, usable=True,
                                contaminated=any(r['contamination']['contaminated'] for r in loaded),
                                observations=loaded))


def validate_identity(root, loaded, gate):
    for row in loaded:
        directory = root / row['id']
        for role in ['send', 'receive']:
            endpoint = json.loads((directory / f'{role}.json').read_text())
            assert endpoint['source'] == build.REVISIONS[row['variant']]
        receipt = json.loads((directory / 'receipt.json').read_text())
        b = json.loads((run.ART / 'bin' / f"{row['variant']}-build.json").read_text())
        assert receipt['binary_sha256'] == b['builds']['fixture']['sha256']
        assert receipt['start_unix_ns'] >= datetime.fromisoformat(gate['window_start']).timestamp() * 1e9
        assert receipt['end_unix_ns'] <= datetime.fromisoformat(gate['window_end']).timestamp() * 1e9


def render(result):
    lines = ['# #871 memory result', '', f"Candidate: `{result['candidate']}`. Plain Reno at both endpoints; all arms are descriptive. No cause is attributed.", '',
             f"High-memory threshold: {result['high_memory_threshold_mib']:.3f} MiB (1.10 × frozen-Reno arm median).", '',
             '| Arm | Peak RSS MiB, median | Paired RSS median (min–max) | High-memory outcomes | Registered RSS outcome |',
             '| --- | ---: | --- | ---: | --- |']
    for arm, value in result['arms'].items():
        m = value['metrics']['peak_rss_mib']
        r = m['ratio_stats']
        lines.append(f"| {arm} | {m['absolute_stats']['median']:.3f} | {r['median']:.3f} ({r['minimum']:.3f}–{r['maximum']:.3f}) | {value['high_memory']}/5 | {'raised' if value['raised'] else 'not raised'} |")
    lines += ['', 'Placement: ' + '; '.join(result['placement']) + '.', '',
              'Every array below is in block order 1–5. The frozen A/A row beside each metric is the noise reference; range membership does not override the registered median rule.', '']
    for metric in METRICS:
        lines += [f'## Sender {metric}', '', '| Arm | Absolute MiB by block | Paired ratios by block | Excess MiB by block | Median ratio (min–max) |',
                  '| --- | --- | --- | --- | --- |']
        for arm, value in result['arms'].items():
            m = value['metrics'][metric]
            arrays = ['; '.join(f'{v:.3f}' for v in m[k]) for k in ['absolute_mib', 'ratios', 'excess_mib']]
            r = m['ratio_stats']
            lines.append(f"| {arm} | {' | '.join(arrays)} | {r['median']:.3f} ({r['minimum']:.3f}–{r['maximum']:.3f}) |")
        lines.append('')
    lines += ['The operator must disposition this result before selecting the #887 preservation reference. This record grants no acceptance or advancement exception.', '']
    return '\n'.join(lines)


def summarize_blocks(blocks):
    """Pure calculation from five complete selected blocks, all arms retained."""
    assert len(blocks) == 5
    assert [b['block'] for b in blocks] == list(range(1, 6))
    indexed = []
    for block in blocks:
        assert not block['contaminated'], 'incomplete campaign: unresolved contamination'
        rows = {r['arm']: r for r in block['observations']}
        assert len(block['observations']) == 5 and set(rows) == set(ARM_NAMES)
        assert all(r['pair'] == block['block'] and r['seed'] == 9000 + block['block'] for r in rows.values())
        indexed.append(rows)
    threshold = 1.10 * memory_load.stats([b['reno-reno']['send']['peak_rss_mib'] for b in indexed])['median']
    arms = {}
    for arm in ARM_NAMES:
        metrics = {}
        for metric in METRICS:
            vals = [b[arm]['send'][metric] for b in indexed]
            refs = [b['reno-reno']['send'][metric] for b in indexed]
            assert all(v is not None and v > 0 for v in vals + refs)
            ratios = [v / r for v, r in zip(vals, refs)]
            excess = [v - r for v, r in zip(vals, refs)]
            metrics[metric] = dict(absolute_mib=vals, absolute_stats=memory_load.stats(vals),
                                   ratios=ratios, ratio_stats=memory_load.stats(ratios),
                                   excess_mib=excess, excess_stats=memory_load.stats(excess))
        rss = metrics['peak_rss_mib']
        arms[arm] = dict(metrics=metrics, high_memory=sum(v > threshold for v in rss['absolute_mib']),
                         raised=rss['ratio_stats']['median'] > 1.10)
    raised = {a: arms[a]['raised'] for a in ARM_NAMES}
    placement = []
    if not raised['cand-reno']:
        placement.append('not reproduced on this candidate')
    if raised['port-reno']:
        placement.append('rise present at or before the port')
    if not raised['port-reno'] and raised['prior-reno']:
        placement.append('historical appearance in abfa5456..d94b3ec2')
    if not raised['prior-reno'] and raised['cand-reno']:
        placement.append('appearance after d94b3ec2 in the bundle interval')
    if raised['port-reno'] and not raised['prior-reno'] or raised['prior-reno'] and not raised['cand-reno']:
        placement.append('nonmonotonic outcomes; no causal attribution')
    return dict(high_memory_threshold_mib=threshold, arms=arms, placement=placement)


def report():
    blocks, selections = [], []
    for b in range(1, 6):
        folder = run.ART / f'memory-block-{b}'
        original = json.loads((folder / 'original.json').read_text())
        attempts = [('original', original['usable'], original.get('contaminated', False))]
        records = {'original': original}
        if (folder / 'rerun.json').exists():
            records['rerun'] = json.loads((folder / 'rerun.json').read_text())
            attempts.append(('rerun', records['rerun']['usable'], records['rerun'].get('contaminated', False)))
        selected, status = choose_block(attempts)
        assert status == 'clean', (b, 'incomplete campaign', status)
        root = run.ART / ('observations-rerun' if selected == 'rerun' else 'observations')
        loaded = [memory_load.load(root / r['id']) for r in records[selected]['observations']]
        validate_identity(root, loaded, records[selected]['pre_data_gates'])
        assert loaded == records[selected]['observations'], 'cached block differs from raw observations'
        selections.append(dict(block=b, selected=selected, attempts=attempts))
        blocks.append(records[selected])
    result = dict(candidate=build.REVISIONS['cand'], selections=selections, blocks=blocks,
                  **summarize_blocks(blocks))
    path = HERE / 'memory-summary.json'
    assert not path.exists(), 'never overwrite a result'
    run.write(path, result)
    (HERE / 'result.md').write_text(render(result))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    b = sub.add_parser('block')
    b.add_argument('number', type=int, choices=range(1, 6))
    b.add_argument('--rerun', action='store_true')
    sub.add_parser('report')
    sub.add_parser('check-prose')
    args = parser.parse_args()
    if args.command == 'block':
        run_block(args.number, args.rerun)
    elif args.command == 'report':
        report()
    else:
        result = json.loads((HERE / 'memory-summary.json').read_text())
        assert (HERE / 'result.md').read_text() == render(result), 'decisive prose differs from JSON'
        assert {k: result[k] for k in ['high_memory_threshold_mib', 'arms', 'placement']} == summarize_blocks(result['blocks'])
        print('decisive prose and all derived memory figures checked against JSON')
