#!/usr/bin/env python3
"""#740 ring-treatment gate trees: two injected violations in copies of the cand-mem-ring tree.

The ring treatment's one registered deviation is earlier eviction (maxRecoveryOutcomes
4,096). Each mutant adds one further, deliberate violation; the gate suite must fail on
each (detection of an injected second violation), and pass on cand-mem-ring itself.
- ring-m1 (recovery): eviction no longer reports the evicted ordinal as missing, so an
  undo episode whose member was evicted stays valid.
- ring-m2 (indexing): eviction leaves the slot's receipt-eligibility bit set.

Usage: gate_trees.py   (after build.py cand-mem-ring; never overwrites)
"""
import shutil

import build

MUTANTS = {
    'ring-m1': ('internal/ackhandler/bbr_recovery.go', '\t\tr.missing(x.outcomes[slot].ordinal)\n', ''),
    'ring-m2': ('internal/ackhandler/bbr_recovery.go', '\tx.pending[s].clear(p)\n\tx.eligible[s].clear(p)\n', '\tx.pending[s].clear(p)\n'),
}

src = build.ART / 'src'
for name, (rel, old, new) in MUTANTS.items():
    tree = src / f'gate-{name}'
    assert not tree.exists(), f'{tree} exists'
    shutil.copytree(src / 'cand-mem-ring', tree, symlinks=True)
    build.replace(tree / rel, old, new)
    print(name, 'tree ready')
