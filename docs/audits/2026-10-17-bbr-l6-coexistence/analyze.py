#!/usr/bin/env python3
"""#777 analysis: counted observations -> summary.json, through the registered rules.

Loads every counted observation (`main`, and `rerun` for rerun triples), judges
usability and contamination with l6rules and matrix.judge, chooses each triple
with the #714 rule (l6rules.choose_triple) and applies l6rules.analyze.
summary.json keeps every attempt's per-arm reasons, contamination, competitor
pause timing and CPU loads, and the full summary of every selected observation,
so each figure the record reports can be checked against it alone. Run it
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


def attempted(phase, pair, workload):
    """A phase counts as attempted for a triple when any of its arms has a directory (a partial rerun included)."""
    return any((run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}').exists() for arm in matrix.ARMS)


def main():
    triples, choices, attempts_out, selected = {}, [], [], {}
    for pair in range(1, l6rules.PAIRS + 1):
        for workload in matrix.workloads(pair):
            attempts = []
            for phase in ['main', 'rerun']:
                if not attempted(phase, pair, workload):
                    continue
                usable, contaminated, detail = matrix.judge(phase, pair, workload)
                attempts.append((phase, usable, contaminated))
                attempts_out.append(dict(phase=phase, pair=pair, workload=workload, usable=usable, contaminated=contaminated,
                                         complete=all((run.OBS / f'{phase}-L6-{workload}-p{pair}-{a}' / 'receipt.json').exists()
                                                      for a in matrix.ARMS),
                                         detail=detail))
            label, status = l6rules.choose_triple(attempts) if attempts else (None, 'unusable')
            choices.append(dict(pair=pair, workload=workload, chosen=label, status=status))
            rows = triple_rows(label, pair, workload) if status == 'clean' else None
            triples[(workload, pair)] = rows
            for arm, row in (rows or {}).items():
                selected[f'{workload}/p{pair}/{arm}'] = row
    analysis = l6rules.analyze(triples)
    out = dict(classification=analysis['classification'], cells=analysis['cells'], triples=choices, attempts=attempts_out,
               analysis=analysis, selected_observations=selected)
    OUT.write_text(json.dumps(out, indent=2) + '\n')
    print('classification:', analysis['classification'])
    fmt = lambda xs: ' '.join('  -  ' if x is None else f'{x:.3f}' for x in xs)
    for cell, outcome in analysis['cells'].items():
        rows = analysis['periods'][cell]
        print(f"{cell:18} {outcome:12} ratios {fmt([r['ratio'] for r in rows])} | A/A {fmt([r['aa_ratio'] for r in rows])}")


if __name__ == '__main__':
    main()
