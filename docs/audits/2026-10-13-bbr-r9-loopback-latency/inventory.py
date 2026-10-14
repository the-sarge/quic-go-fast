#!/usr/bin/env python3
"""#744 inventory and rerun eligibility, enforced at the launch boundary (README "Inventory and endings").

Every rerun, whether follow.py or a direct `matrix.py rerun` asks for it, passes
`admit_rerun` first. A rerun is admitted only when:
- its stage is registered and its (workload, block) exists in that stage;
- the block's original attempt is not clean (contaminated or unusable under
  the stage's masks; ctlstages.chosen) and the block has no rerun yet;
- for the counted stages (`s1`, `s2-*`), the counted observations retained so
  far (originals and reruns, every directory, failed or partial launches
  included) plus the rerun's arms stay within 180, and counted rerun
  observations within 24.

Synthetic (`syn`) reruns are excluded from both caps, as the synthetic runs
are. Development (`dev*`) and activation (`act*`) runs are never counted and
never rerun. Reruns are allocated in a fixed order: the registered stage order,
then workload, then block (follow.py).
"""
from pathlib import Path

CAP = 180         # counted observations, originals and reruns
RERUN_CAP = 24    # counted rerun observations
COUNTED = ('s1-', 's2-')
# stage -> (phase, path, workloads); the arms come from matrix.STAGES
STAGES = {
    'syn': ('syn', 'loopback-syn', ('stream',)),
    's1': ('s1', 'loopback-long', ('stream',)),
    's2-loopback': ('s2', 'loopback', ('stream', 'datagram')),
    's2-long': ('s2', 'loopback-long', ('stream',)),
    's2-s5': ('s2', 'S5', ('stream', 'datagram')),
}
ORDER = ['syn', 's1', 's2-loopback', 's2-long', 's2-s5']


def counted(roots):
    """Retained attempts of the counted phases, once per directory by its name."""
    return sum(1 for root in roots if Path(root).exists() for d in Path(root).iterdir() if d.is_dir() and d.name.startswith(COUNTED))


def admit_rerun(stage, workload, block, records, roots, n_arms):
    """(admitted, reason). records: ctlstages.chosen(...) for the stage; roots: [observations, observations-rerun]."""
    if stage not in STAGES:
        return False, f'unregistered stage {stage!r}'
    if workload not in STAGES[stage][2]:
        return False, f'{stage} has no workload {workload!r}'
    rec = [r for r in records if r['workload'] == workload and r['block'] == block]
    if not rec:
        return False, f'{stage} {workload} block {block} has no original attempt'
    r = rec[0]
    if any(a[0] == 'rerun' for a in r['attempts']):
        return False, 'the block already has its one rerun'
    if r['status'] == 'clean':
        return False, 'the block is clean; reruns are triggered by host state only'
    if stage == 'syn':
        return True, 'synthetic rerun (excluded from the caps)'
    if counted(roots) + n_arms > CAP:
        return False, f'cap: {counted(roots)} + {n_arms} > {CAP}'
    if counted(roots[1:]) + n_arms > RERUN_CAP:
        return False, f'rerun cap: {counted(roots[1:])} + {n_arms} > {RERUN_CAP}'
    return True, 'admitted'
