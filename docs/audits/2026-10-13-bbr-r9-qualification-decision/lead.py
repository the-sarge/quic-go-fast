#!/usr/bin/env python3
"""Extract #738 Stage 2's loopback STREAM control latency per counted block.

Reads the per-observation summaries inside #738's raw.tar.gz without
extracting it, keeps the attempt each block counted (matched to results.json
by goodput), and writes latency-lead.json beside this script.
"""
import json
import pathlib
import statistics
import tarfile

HERE = pathlib.Path(__file__).resolve().parent
RECORD = HERE.parent / '2026-10-07-bbr-loopback-handoff'
ARMS = {'cand-hand-timeline': 'r8', 'cand-hand-timeline-aa': 'r8_aa', 'new-hand-timeline': 'r9',
        'new': 'reno_on_r9', 'reno': 'reno', 'reno-aa': 'reno_aa'}


def summaries():
    found = {}
    with tarfile.open(RECORD / 'raw.tar.gz') as tar:
        for m in tar.getmembers():
            p = pathlib.PurePosixPath(m.name)
            if p.name == 'summary.json' and p.parent.name.startswith('s2-loopback-stream-p'):
                s = json.load(tar.extractfile(m))
                found[(s['pair'], round(s['goodput_mbps'], 3))] = s
    return found


def spread(values):
    return dict(median=statistics.median(values), minimum=min(values), maximum=max(values), n=len(values))


def main():
    counted = json.loads((RECORD / 'results.json').read_text())['s2']['stream']['rules_input']
    obs = summaries()
    blocks = []
    for b in counted:
        row = {'block': b['block']}
        for arm, goodput in b['goodput'].items():
            s = obs[(b['block'], round(goodput, 3))]
            row[ARMS[arm]] = dict(id=s['id'], source=s['source'], goodput_mbps=s['goodput_mbps'],
                                  control_p95_ms=s['control_p95_ms'], control_p50_ms=s['control_p50_ms'],
                                  control_replies=s['control_replies'])
        blocks.append(row)

    def ratio(a, b, q):
        return [r[a][q] / r[b][q] for r in blocks]

    out = dict(
        note='Descriptive lead only: #738 Stage 2, instrumented hand-off builds for r8 and r9, '
             'plain frozen Reno; not registered for latency.',
        blocks=blocks,
        p95=dict(r9_over_r8=spread(ratio('r9', 'r8', 'control_p95_ms')),
                 r8_aa_over_r8=spread(ratio('r8_aa', 'r8', 'control_p95_ms')),
                 r8_over_reno=spread(ratio('r8', 'reno', 'control_p95_ms')),
                 r9_over_reno=spread(ratio('r9', 'reno', 'control_p95_ms'))),
        p50=dict(r9_over_r8=spread(ratio('r9', 'r8', 'control_p50_ms')),
                 r8_aa_over_r8=spread(ratio('r8_aa', 'r8', 'control_p50_ms'))),
    )
    aa95, aa50 = out['p95']['r8_aa_over_r8']['maximum'], out['p50']['r8_aa_over_r8']['maximum']
    out['p95']['r9_over_r8_blocks_above_aa_max'] = sum(v > aa95 for v in ratio('r9', 'r8', 'control_p95_ms'))
    out['p50']['r9_over_r8_blocks_above_aa_max'] = sum(v > aa50 for v in ratio('r9', 'r8', 'control_p50_ms'))
    (HERE / 'latency-lead.json').write_text(json.dumps(out, indent=2) + '\n')


if __name__ == '__main__':
    main()
