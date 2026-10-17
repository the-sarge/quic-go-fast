#!/usr/bin/env python3
"""#777 analysis: counted observations -> summary.json, through the registered rules.

Loads every counted observation (`main`, and `rerun` for rerun triples), judges
usability and contamination with l6rules and matrix.judge, chooses each triple
with the #714 rule (l6rules.choose_triple) and applies l6rules.analyze. Run it
anywhere the observation tree is available: python3 analyze.py
"""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import l6rules  # noqa: E402
import matrix  # noqa: E402
import run  # noqa: E402

OUT = Path(__file__).resolve().parent / 'summary.json'


def triple_rows(phase, pair, workload):
    out = {}
    for arm in matrix.ARMS:
        try:
            out[arm] = run.summarize(run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}')
        except Exception:  # noqa: BLE001 - an unusable arm leaves the triple unusable
            return None
    return out


def main():
    triples, choices, observations = {}, [], []
    for pair in range(1, l6rules.PAIRS + 1):
        for workload in matrix.workloads(pair):
            attempts = []
            for phase in ['main', 'rerun']:
                if not (run.OBS / f'{phase}-L6-{workload}-p{pair}-{matrix.ARMS[0]}').exists() and phase == 'rerun':
                    continue
                usable, contaminated, reasons = matrix.judge(phase, pair, workload)
                attempts.append((phase, usable, contaminated))
                observations.append(dict(phase=phase, pair=pair, workload=workload, usable=usable, contaminated=contaminated, reasons=reasons))
            label, status = l6rules.choose_triple(attempts)
            choices.append(dict(pair=pair, workload=workload, chosen=label, status=status))
            triples[(workload, pair)] = triple_rows(label, pair, workload) if status == 'clean' else None
    analysis = l6rules.analyze(triples)
    out = dict(classification=analysis['classification'], cells=analysis['cells'], triples=choices, observations=observations,
               analysis=analysis)
    OUT.write_text(json.dumps(out, indent=2) + '\n')
    print('classification:', analysis['classification'])
    for cell, outcome in analysis['cells'].items():
        ratios = [r['ratio'] for r in analysis['periods'][cell]]
        aa = [r['aa_ratio'] for r in analysis['periods'][cell]]
        fmt = lambda xs: ' '.join('  -  ' if x is None else f'{x:.3f}' for x in xs)
        print(f'{cell:18} {outcome:12} ratios {fmt(ratios)} | A/A {fmt(aa)}')


if __name__ == '__main__':
    main()
