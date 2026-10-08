#!/usr/bin/env python3
"""#739 unattended follow-up steps on minimax, applying registered rules only.

Written before any observation of this ticket. Each step decides from host
state or from the registered readiness flags, never from an attribution
outcome:

  reruns <phase>   rerun once, with the same seed, every block of <phase> whose
                   counted attempt is contaminated or unusable and that has no
                   rerun yet (the operator's #714 rule, rules.choose_block);
                   readiness (`main`) reruns stop at the registered cap of ten
                   blocks, attribution reruns at the attribution cap.
  conditional      run the conditional preservation stages the readiness flags
                   trigger (README.md, "Registration: preservation"): `presrss`
                   for each raised Reno-on-candidate RSS cell (at most two),
                   `preslat` for a raised Reno-on-candidate loopback control p95
                   cell (at most one), then their reruns.

The s5timeline escalation depends on that stage's verdicts and is left to the
operator's session. Run under `taskset -c 1-3` (the driver does).

Usage: follow.py reruns main|s5timeline
       follow.py conditional
"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import stages as S  # noqa: E402  (imports art first, so run.ART is this ticket's)

READINESS_CAP = 10
ATTRIBUTION_CAP = 60
READINESS_STAGE = {'loopback': 'loopback', 'S5': 's5', 'S6': 's6'}
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


def attribution_count():
    n = 0
    for root in S.ROOTS:
        for p in root.glob('*/meta.json'):
            if json.loads(p.read_text())['phase'] in ('s5timeline', 'presrss', 'preslat'):
                n += 1
    return n


def pending_reruns(phase, path=None):
    """Blocks whose counted attempt is not clean and that have not been rerun."""
    _, rec = S.chosen(phase, path)
    return [r for r in rec if r['status'] != 'clean' and len(r['attempts']) == 1]


def reruns(phase, stage_of, cap=None, arms_per_block=None):
    todo = pending_reruns(*phase) if isinstance(phase, tuple) else pending_reruns(phase)
    done = []
    for r in todo:
        if cap is not None and len(done) >= cap:
            record('reruns_not_run_cap', dict(phase=str(phase), **{k: r[k] for k in ('path', 'workload', 'block', 'status')}))
            continue
        if arms_per_block and attribution_count() + arms_per_block > ATTRIBUTION_CAP:
            record('reruns_not_run_cap', dict(phase=str(phase), **{k: r[k] for k in ('path', 'workload', 'block', 'status')}))
            continue
        name = stage_of(r)
        staged(f'rerun-{name}-{r["workload"]}-p{r["block"]}', 'rerun', name, r['workload'], str(r['block']))
        done.append(r)
        record('reruns', dict(phase=str(phase), stage=name, workload=r['workload'], block=r['block'], status=r['status']))
    return done


def readiness_reruns():
    log = json.loads(LOG.read_text()) if LOG.exists() else {}
    prior = sum(x['phase'] == 'main' for x in log.get('reruns', []))
    reruns('main', lambda r: READINESS_STAGE[r['path']], cap=READINESS_CAP - prior)


def conditional():
    S.readiness()
    summ = S.readiness_summary()
    rss = S.raised(summ, 'cand-reno', 'rss')
    p95 = S.raised(summ, 'cand-reno', 'p95')
    rss_cells = sorted({(p, w) for p, w, _ in rss})
    lat_cells = sorted({w for p, w, _ in p95 if p == 'loopback'})
    record('conditional_triggers', dict(rss=rss, p95=p95, presrss_cells=rss_cells[:2], preslat_cells=lat_cells[:1],
                                        not_run=dict(presrss=rss_cells[2:], preslat=lat_cells[1:])))
    for path, workload in rss_cells[:2]:
        if attribution_count() + 12 > ATTRIBUTION_CAP:
            record('not_run_cap', f'presrss-{path}-{workload}')
            continue
        staged(f'presrss-{path}-{workload}')
    for workload in lat_cells[:1]:
        if attribution_count() + 12 > ATTRIBUTION_CAP:
            record('not_run_cap', f'preslat-{workload}')
            continue
        staged(f'preslat-{workload}')
    for path, workload in rss_cells[:2]:
        reruns(('presrss', path), lambda r, p=path: f'presrss-{p}-{r["workload"]}', arms_per_block=2)
    if lat_cells[:1]:
        reruns('preslat', lambda r: f'preslat-{r["workload"]}', arms_per_block=2)


def main(argv):
    if argv[1] == 'reruns' and argv[2] == 'main':
        readiness_reruns()
    elif argv[1] == 'reruns' and argv[2] == 's5timeline':
        reruns('s5timeline', lambda r: 's5timeline', arms_per_block=1)
    elif argv[1] == 'conditional':
        conditional()
    else:
        raise SystemExit(__doc__)


if __name__ == '__main__':
    main(sys.argv)
