#!/usr/bin/env python3
"""#740 unattended follow-up steps on minimax, applying registered rules only.

Adapted from #739's follow.py (138223ac). Written before any comparative
observation of this ticket. Each step decides from host state only, never from
an outcome:

  reruns           rerun once, with the same seed, every Stage 1 or `latcand`
                   block (one path, one workload, every arm) whose counted attempt
                   is contaminated or unusable and that has no rerun yet (the
                   operator's #714 rule, rules.choose_block), while the ticket's
                   counted total stays within its registered cap of 152 and the reruns within 28
                   (README.md, "Inventory and cap").

Summaries use mem_run.summarize_m (the receiver-controller assertion). Run under
`taskset -c 1-3` (the driver does).

Usage: follow.py reruns
"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import stages as S  # noqa: E402  (imports art first, so run.ART is this ticket's)
import mem_run  # noqa: E402
import memstages as MS  # noqa: E402

S.summarize = S.run.summarize = mem_run.summarize_m

STAGES = [('mem', 'S5', 'mem-s5'), ('mem', 'S6', 'mem-s6'), ('mem', 'loopback', 'mem-loopback'), ('latcand', 'loopback-long', 'latcand-stream')]
CAP = 152        # counted observations, originals and reruns (README.md, "Inventory and cap")
RERUN_CAP = 28   # rerun observations, whether or not the ring arm runs
LOG = S.ART / 'follow-decisions.json'


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
    """Observations of the capped phases taken so far (originals and reruns, or only the given roots)."""
    return sum(json.loads(p.read_text())['phase'] in ('mem', 'latcand') for root in (roots or S.ROOTS) for p in root.glob('*/meta.json'))


def pending_reruns(phase, path, names):
    """Registered blocks not yet rerun whose counted attempt is not clean (contaminated, unusable, instrument-invalid,
    missing an arm) or that do not exist. A low baseline excess never requests a rerun."""
    _, rec = MS.choose(phase, path, names)
    return [r for r in rec if 'block' in r and r['status'] != 'clean' and not any(a[0] == 'rerun' for a in r['attempts'])]


def reruns():
    done = []
    ring = MS.ring_decision()
    for phase, path, name in STAGES:
        names = MS.arm_names(phase, path, ring)
        arms = len(names)
        for r in pending_reruns(phase, path, names):
            key = {k: r[k] for k in ('path', 'workload', 'block', 'status')}
            if counted() + arms > CAP or counted([S.ROOTS[1]]) + arms > RERUN_CAP:
                record('reruns_not_run_cap', key)
                continue
            staged(f'rerun-{name}-{r["workload"]}-p{r["block"]}', 'rerun', name, r['workload'], str(r['block']))
            done.append(r)
            record('reruns', dict(stage=name, **key))
    return done


def main(argv):
    if argv[1:] == ['reruns']:
        reruns()
    else:
        raise SystemExit(__doc__)


if __name__ == '__main__':
    main(sys.argv)
