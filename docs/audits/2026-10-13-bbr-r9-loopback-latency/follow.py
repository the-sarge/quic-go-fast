#!/usr/bin/env python3
"""#744 unattended follow-up steps on minimax, applying registered rules only.

Adapted from #740's follow.py (ddbac0a3). Written before any comparative
observation of this ticket. Each step decides from host state only, never from
an outcome:

  reruns   for each named stage in inventory.ORDER, rerun once, with the same
           seed, every (workload, block) whose original attempt is not clean
           under the stage's masks and that has no rerun yet (the operator's
           #714 rule), in workload then block order, through `matrix.py rerun`,
           which applies inventory.admit_rerun (eligibility, the 180-observation
           cap and the 24-rerun cap) itself. A refused rerun is recorded.

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
import inventory  # noqa: E402

LOG = S.ART / 'follow-decisions.json'


def matrix(*args):
    print('matrix', *args, flush=True)
    return subprocess.run([sys.executable, str(HERE / 'matrix.py'), *args]).returncode


def record(key, value):
    out = json.loads(LOG.read_text()) if LOG.exists() else {}
    out.setdefault(key, []).append(value)
    LOG.write_text(json.dumps(out, indent=1) + '\n')


def reruns(stages):
    for stage in [s for s in inventory.ORDER if s in stages]:
        for r in S.chosen(stage):
            if r['status'] == 'clean' or any(a[0] == 'rerun' for a in r['attempts']):
                continue
            key = dict(stage=stage, workload=r['workload'], block=r['block'], status=r['status'])
            name = f'rerun-{stage}-{r["workload"]}-p{r["block"]}'
            matrix('hostfacts', f'{name}-start')
            code = matrix('rerun', stage, r['workload'], str(r['block']))
            matrix('hostfacts', f'{name}-end')
            record('reruns' if code == 0 else 'reruns_refused_or_failed', dict(key, exit=code))


if __name__ == '__main__':
    if sys.argv[1:2] == ['reruns']:
        unknown = [s for s in sys.argv[2:] if s not in inventory.STAGES]
        if unknown:
            raise SystemExit(f'unregistered stages: {unknown}')
        reruns(sys.argv[2:] or ['syn', 's1'])
    else:
        raise SystemExit(__doc__)
