#!/usr/bin/env python3
"""Synthetic cases for intervene.py, run before any #714 measurement observation existed.

The empirical null control uses #712's retained counters stage (localization.json at
4323dae8): Reno built from the unchanged candidate against frozen Reno, with that
stage's own A/A arm. An unchanged Reno path must pass the preservation check.
"""
import json
import subprocess
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import intervene as I  # noqa: E402

BASE = {'instructions:u/pkt': 31600.0, 'cycles:u/pkt': 14400.0, 'cycles/gib': 28e9, 'instructions/gib': 50e9}


def endpoint(scale=None):
    scale = scale or {}
    return {k: v * scale.get(k, 1.0) for k, v in BASE.items()}


def block(prev=None, new=None, aa=None, renonew=None, recv=None):
    """Arms as multiplicative factors on frozen Reno's sender metrics; receiver factors optional."""
    recv = recv or {}
    return {'reno': {'send': endpoint(), 'receive': endpoint()},
            'aa': {'send': endpoint(aa), 'receive': endpoint(recv.get('aa'))},
            'prev': {'send': endpoint(prev), 'receive': endpoint()},
            'new': {'send': endpoint(new), 'receive': endpoint()},
            'renonew': {'send': endpoint(renonew), 'receive': endpoint(recv.get('renonew'))}}


def series(effect, aa_spread, n=6, cycles_effect=None, effects=None):
    """n blocks: new = prev * effect (instructions), A/A spread +-aa_spread, Reno on new = A/A-like."""
    out = []
    for i in range(n):
        a = 1 + aa_spread * (2 * i / (n - 1) - 1)
        e = effects[i] if effects else effect
        ce = cycles_effect if cycles_effect is not None else e
        r = 1 + aa_spread * (2 * ((i + 2) % n) / (n - 1) - 1) * 0.5
        out.append(block(prev={'instructions:u/pkt': 1.36, 'cycles:u/pkt': 1.49},
                         new={'instructions:u/pkt': 1.36 * e, 'cycles:u/pkt': 1.49 * ce},
                         aa={'instructions:u/pkt': a, 'cycles:u/pkt': a, 'cycles/gib': a},
                         renonew={'cycles/gib': r},
                         recv={'aa': {'cycles/gib': a}, 'renonew': {'cycles/gib': r}}))
    return out


class Work(unittest.TestCase):
    def test_null(self):
        r = I.analyze({'stream': series(1.0005, 0.002), 'datagram': series(0.9995, 0.002)}, True)
        self.assertEqual(r['work'], 'null')
        self.assertEqual(r['outcome'], 'null')

    def test_reduction_keeps(self):
        r = I.analyze({'stream': series(0.97, 0.002), 'datagram': series(0.98, 0.002)}, True)
        self.assertEqual(r['outcome'], 'keep', r['reasons'])
        d = r['workloads']['stream']['delta_per_packet']
        self.assertAlmostEqual(d, 31600 * 1.36 * (0.97 - 1), delta=1)

    def test_one_workload_reduction_other_null_keeps(self):
        r = I.analyze({'stream': series(0.97, 0.002), 'datagram': series(1.0, 0.002)}, True)
        self.assertEqual(r['outcome'], 'keep')

    def test_increase_is_negative(self):
        r = I.analyze({'stream': series(1.02, 0.002), 'datagram': series(1.0, 0.002)}, True)
        self.assertEqual(r['outcome'], 'negative')

    def test_noisy_effect_not_in_every_block(self):
        # Median beyond the A/A range, but blocks straddle 1: no effect is claimed.
        noisy = [0.985, 0.99, 0.995, 1.004, 0.99, 1.006]  # median 0.9925, two blocks above 1
        r = I.analyze({'stream': series(None, 0.001, effects=noisy), 'datagram': series(1.0, 0.001)}, True)
        self.assertTrue(r['workloads']['stream']['work'].startswith('inconclusive'), r['workloads']['stream']['work'])
        self.assertEqual(r['outcome'], 'inconclusive')

    def test_conflicting_counters(self):
        # Instructions fall, cycles rise beyond their A/A range.
        r = I.analyze({'stream': series(0.97, 0.002, cycles_effect=1.02), 'datagram': series(1.0, 0.002)}, True)
        self.assertIn('cycles rose', r['workloads']['stream']['work'])
        self.assertEqual(r['outcome'], 'inconclusive')

    def test_cycles_inside_noise_do_not_block(self):
        r = I.analyze({'stream': series(0.97, 0.002, cycles_effect=1.0), 'datagram': series(1.0, 0.002)}, True)
        self.assertEqual(r['outcome'], 'keep')

    def test_workloads_disagree(self):
        r = I.analyze({'stream': series(0.97, 0.002), 'datagram': series(1.02, 0.002)}, True)
        self.assertEqual(r['outcome'], 'inconclusive')

    def test_unusable_blocks_are_an_evidence_gap(self):
        r = I.analyze({'stream': series(0.97, 0.002, n=3), 'datagram': series(1.0, 0.002, n=3)}, True)
        self.assertTrue(r['workloads']['stream']['work'].startswith('inconclusive: evidence gap'))
        self.assertEqual(r['outcome'], 'inconclusive')

    def test_integrity_failure_is_never_kept(self):
        r = I.analyze({'stream': series(0.97, 0.002), 'datagram': series(0.98, 0.002)}, False)
        self.assertEqual(r['outcome'], 'negative')


class Preservation(unittest.TestCase):
    def test_reno_regression_blocks_keep(self):
        s = series(0.97, 0.002)
        for b in s:
            b['renonew']['send']['cycles/gib'] *= 1.02
        r = I.analyze({'stream': s, 'datagram': series(0.98, 0.002)}, True)
        self.assertEqual(r['preservation']['send:cycles/gib']['position'], 'above')
        self.assertEqual(r['outcome'], 'negative')

    def test_reno_faster_outside_range_is_not_kept(self):
        s, t = series(0.97, 0.002), series(0.98, 0.002)
        for b in s + t:
            b['renonew']['receive']['cycles/gib'] *= 0.98
        r = I.analyze({'stream': s, 'datagram': t}, True)
        self.assertEqual(r['outcome'], 'inconclusive')

    def test_empirical_null_from_712(self):
        """Unchanged Reno path (Reno built from fc4c1bf1) against frozen Reno, #712 counters stage."""
        loc = json.loads(subprocess.check_output(['git', 'show', '4323dae8:docs/audits/2026-10-03-bbr-linux-diagnostic/localization.json'],
                                                 cwd=HERE))['counters']
        blocks = []
        for wl in ['stream', 'datagram']:
            aa = {(r, b['block']): b for r in ['send', 'receive'] for b in loc[wl][f'reno-reno-aa/{r}']['blocks']}
            cr = {(r, b['block']): b for r in ['send', 'receive'] for b in loc[wl][f'cand-reno/{r}']['blocks']}
            for blk in sorted({k[1] for k in aa} & {k[1] for k in cr}):
                x = block()
                for r in ['send', 'receive']:
                    x['aa'][r]['cycles/gib'] = BASE['cycles/gib'] * aa[(r, blk)]['cycles']
                    x['renonew'][r]['cycles/gib'] = BASE['cycles/gib'] * cr[(r, blk)]['cycles']
                blocks.append(x)
        self.assertEqual(len(blocks), 12)
        for key in I.PRESERVATION:
            self.assertEqual(I.preservation_cell(blocks, key)['position'], 'inside', key)


if __name__ == '__main__':
    unittest.main()
