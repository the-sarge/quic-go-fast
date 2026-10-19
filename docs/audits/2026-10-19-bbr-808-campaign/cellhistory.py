#!/usr/bin/env python3
"""Each raised readiness cell's history on minimax, for the #808 advancement disposition (descriptive).

Reads this campaign's readiness `summary.json` and, through `git show`, the
readiness `summary.json` of every earlier record on the same host and stage:
#712 (4323dae8), #715 (36f7ce01), #736 (r8, 3bf2245e), #739 (r9, 138223ac) and
#748 slice 6 (the port, 9b38f0ab); and slice 7's accepted magnitudes
(`raised-cells.json` at ebfd9339). For every cell this record raises, and
every Reno-on-candidate cell raised in any of them, it writes `cell-history.json`
and prints one line per cell: this record's median (blocks crossing), its A/A
range, the same cell in each earlier record (median, raised or not), the A/A
arm's value there, and slice 7's accepted magnitude for the port. It sets no
flag and changes no rule; the disposition's text is checked against it.

Usage: cellhistory.py [readiness summary.json]
"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
RECORDS = [
    ('#712', '4323dae8', 'docs/audits/2026-10-03-bbr-linux-diagnostic'),
    ('#715', '36f7ce01', 'docs/audits/2026-10-05-bbr-linux-redemonstration'),
    ('#736 r8', '3bf2245e', 'docs/audits/2026-10-06-bbr-r8-linux-redemonstration'),
    ('#739 r9', '138223ac', 'docs/audits/2026-10-08-bbr-r9-linux-redemonstration'),
    ('slice 6 port', '9b38f0ab', 'docs/audits/2026-10-16-bbr-port-readiness'),
]
SLICE7 = ('ebfd9339', 'docs/audits/2026-10-16-bbr-port-advancement-decision/raised-cells.json')


def show(sha, path):
    return json.loads(subprocess.check_output(['git', 'show', f'{sha}:{path}'], cwd=ROOT))


def flags(summary):
    return {(e['path'], e['workload'], e['arm'], k): f for e in summary['readiness'] for k, f in e['flags'].items()}


def main(argv):
    this_path = Path(argv[1]) if len(argv) > 1 else HERE.parent / '2026-10-19-bbr-808-readiness' / 'summary.json'
    this = flags(json.loads(this_path.read_text()))
    prior = [(label, flags(show(sha, f'{d}/summary.json'))) for label, sha, d in RECORDS]
    accepted = {(c['path'], c['workload'], c['arm'], c['measure']): c for c in show(*SLICE7)['cells']}
    keys = {k for k, f in this.items() if f['raised'] and k[2] != 'reno-reno-aa'}
    keys |= {k for _, fl in prior for k, f in fl.items() if f['raised'] and k[2] == 'cand-reno'}
    out = []
    for k in sorted(keys):
        path, workload, arm, measure = k
        f = this.get(k)
        aa_key = (path, workload, 'reno-reno-aa', measure)
        row = dict(path=path, workload=workload, arm=arm, measure=measure,
                   this=None if f is None else dict(median=f['median'], raised=f['raised'], blocks_crossing=f['blocks_crossing'], aa=f.get('aa')),
                   history=[dict(record=label, median=fl[k]['median'] if k in fl else None, raised=fl[k]['raised'] if k in fl else None,
                                 aa_median=fl[aa_key]['median'] if aa_key in fl else None, aa_raised=fl[aa_key]['raised'] if aa_key in fl else None)
                            for label, fl in prior],
                   slice7_accepted=None)
        acc = accepted.get(k)
        if acc:
            row['slice7_accepted'] = {x: acc.get(x) for x in ('ratio', 'blocks_crossing', 'difference_of_arm_medians_ms', 'excess_median_mib', 'excess_max_mib') if x in acc}
        out.append(row)
    (HERE / 'cell-history.json').write_text(json.dumps(out, indent=1) + '\n')
    fmt = lambda x: '  -  ' if x is None else f'{x:.3f}'
    for r in out:
        t = r['this']
        head = f"{r['path']:8} {r['workload']:8} {r['arm']:10} {r['measure']:12} this " + \
            ('  -  ' if t is None else f"{fmt(t['median'])}{'*' if t['raised'] else ''}({t['blocks_crossing']}) A/A {fmt((t['aa'] or {}).get('minimum'))}-{fmt((t['aa'] or {}).get('maximum'))}")
        hist = ' | '.join(f"{h['record']} {fmt(h['median'])}{'*' if h['raised'] else ''} aa {fmt(h['aa_median'])}{'*' if h['aa_raised'] else ''}" for h in r['history'])
        print(head, '||', hist, '|| slice7', r['slice7_accepted'])


if __name__ == '__main__':
    main(sys.argv)
