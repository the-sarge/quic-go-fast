#!/usr/bin/env python3
"""#744 unattended follow-up steps on minimax, applying registered rules only.

Adapted from #740's follow.py (ddbac0a3). Written before any comparative
observation of this ticket. Each step decides from host state only, never from
an outcome:

  reruns   rerun once, with the same seed, every `syn` or `s1` block whose
           counted attempt is contaminated or unusable and that has no rerun
           yet (the operator's #714 rule, rules.choose_block). Stage 1 reruns
           count toward the ticket's cap: counted observations (originals and
           reruns of `s1-` and `s2-` phases) stay within 180, and reruns within
           24 (README "Inventory and endings"). Synthetic reruns are excluded
           from the cap, as the synthetic cases are.

Run under `taskset -c 1-3` (the driver does).

Usage: follow.py reruns [stage ...]   (default: syn s1)
"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import ctlstages as S  # noqa: E402  (imports art first, so run.ART is this ticket's)
import matrix as M  # noqa: E402

CAP = 180        # counted observations, originals and reruns (README "Inventory and endings")
RERUN_CAP = 24   # counted rerun observations
LOG = S.ART / 'follow-decisions.json'
COUNTED = ('s1-', 's2-')  # directory-name prefixes of the capped phases (never syn- or dev)
ARMS = {'syn': ('syn', 'loopback-syn'), 's1': ('s1', 'loopback-long')}


def matrix(*args):
    print('matrix', *args, flush=True)
    subprocess.run([sys.executable, str(HERE / 'matrix.py'), *args], check=True)


def staged(name, *args):
    matrix('hostfacts', f'{name}-start')
    matrix(*(args or (name,)))
    matrix('hostfacts', f'{name}-end')


def record(key, value):
    out = json.loads(LOG.read_text()) if LOG.exists() else {}
    out.setdefault(key, []).append(value)
    LOG.write_text(json.dumps(out, indent=1) + '\n')


def counted(roots=None):
    """Attempts of the capped phases retained so far, counted once per directory by its name."""
    return sum(1 for root in (roots or S.ROOTS) if root.exists() for d in root.iterdir() if d.is_dir() and d.name.startswith(COUNTED))


def arm_names(stage):
    return ['-'.join(a[:2] + ((a[2],) if a[2] else ())) for a in M.STAGES[stage][2]]


def reruns(stages):
    for stage in stages:
        phase, path = ARMS[stage]
        names = arm_names(stage)
        for r in S.chosen(phase, path, names):
            if r['status'] == 'clean' or any(a[0] == 'rerun' for a in r['attempts']):
                continue
            key = dict(stage=stage, workload=r['workload'], block=r['block'], status=r['status'])
            if stage != 'syn' and (counted() + len(names) > CAP or counted([S.ROOTS[1]]) + len(names) > RERUN_CAP):
                record('reruns_not_run_cap', key)
                continue
            staged(f'rerun-{stage}-{r["workload"]}-p{r["block"]}', 'rerun', stage, r['workload'], str(r['block']))
            record('reruns', key)


if __name__ == '__main__':
    if sys.argv[1:2] == ['reruns']:
        reruns(sys.argv[2:] or ['syn', 's1'])
    else:
        raise SystemExit(__doc__)
