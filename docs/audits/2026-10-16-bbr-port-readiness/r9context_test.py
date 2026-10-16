#!/usr/bin/env python3
"""Tests for r9context.py on synthetic summaries."""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import r9context as C  # noqa: E402


def flag(median, raised=False, crossing=0):
    return dict(median=median, raised=raised, blocks_crossing=crossing, aa=dict(median=1.0, minimum=0.98, maximum=1.02, n=5))


def summary(arms, paired=()):
    return dict(readiness=[dict(path=p, workload=w, arm=a, blocks=5, flags=f) for (p, w, a), f in arms.items()], paired=list(paired))


class Readiness(unittest.TestCase):
    def test_r9_value_beside_each_cell(self):
        this = summary({('S5', 'stream', 'cand-bbrv3'): dict(send_rss=flag(1.3, True, 5))})
        r9 = summary({('S5', 'stream', 'cand-bbrv3'): dict(send_rss=flag(1.235, True, 5))})
        row = C.readiness(this, r9)[0]['measures']['send_rss']
        self.assertEqual((row['median'], row['raised'], row['r9']['median'], row['r9']['raised']), (1.3, True, 1.235, True))

    def test_missing_r9_cell_is_none(self):
        this = summary({('S5', 'stream', 'cand-reno'): dict(goodput=flag(1.0))})
        self.assertIsNone(C.readiness(this, summary({}))[0]['measures']['goodput']['r9'])


class CandOverRenoOnCand(unittest.TestCase):
    def test_frozen_reno_cancels_per_block(self):
        paired = [dict(path='loopback', workload='stream', block=b,
                       vs_reno={'cand-bbrv3': dict(receive_rss=1.2 * k), 'cand-reno': dict(receive_rss=1.15 * k)})
                  for b, k in [(1, 1.0), (2, 1.1), (3, 0.9)]]
        out = C.cand_over_reno_on_cand(paired, [('loopback', 'stream', 'receive_rss')])[0]
        self.assertAlmostEqual(out['ratio']['median'], 1.2 / 1.15)
        self.assertEqual(out['ratio']['n'], 3)

    def test_only_raised_reno_on_candidate_cells(self):
        s = summary({('S5', 'stream', 'cand-reno'): dict(send_rss=flag(1.12, True, 3), goodput=flag(1.0)),
                     ('S5', 'stream', 'cand-bbrv3'): dict(send_rss=flag(1.3, True, 5))})
        self.assertEqual(C.raised_reno_on_cand(s), [('S5', 'stream', 'send_rss')])


class Timeline(unittest.TestCase):
    def test_ranges_per_workload(self):
        dec = lambda m: dict(deficit=0.03, idle=0.03, model=m, timing=0.0002, unexplained=0.01)
        t = dict(verdict='model behaviour', workloads=dict(stream='model', datagram='model'),
                 observations=[dict(workload='stream', verdict='model', decomposition=dec(m)) for m in (0.017, 0.025)])
        out = C.timeline_ranges(t)
        self.assertEqual((out['blocks']['stream']['model']['minimum'], out['blocks']['stream']['model']['maximum']), (0.017, 0.025))
        self.assertIsNone(out['blocks']['datagram']['model'])

    def test_lateness_and_shares(self):
        dec = dict(deficit=0.03, idle=0.03, model=0.02, timing=0.0002, unexplained=0.01)
        obs = [dict(workload='datagram', verdict='model', decomposition=dec, late_count=c, late_ms_total=c / 100, late_max_ms=0.05,
                    phase_share=dict(cruise=0.6, down=d), state_share=dict(paced_ns=0.96)) for c, d in [(28709, 0.17), (29008, 0.23)]]
        out = C.timeline_ranges(dict(observations=obs))['blocks']['datagram']
        self.assertEqual((out['late_count']['minimum'], out['late_count']['maximum']), (28709, 29008))
        self.assertEqual((out['phase_share']['down']['minimum'], out['phase_share']['down']['maximum']), (0.17, 0.23))
        self.assertEqual(out['state_share']['paced_ns']['n'], 2)

    def test_missing_field_is_dropped_and_zero_is_kept(self):
        dec = dict(deficit=0.03, idle=0.03, model=0.02, timing=0.0002, unexplained=0.01)
        obs = [dict(workload='stream', verdict='model', decomposition=dec, late_count=0, phase_share=dict(cruise=0.6)),
               dict(workload='stream', verdict='model', decomposition=dec, phase_share=dict(cruise=0.5, down=0.2))]
        out = C.timeline_ranges(dict(observations=obs))['blocks']['stream']
        self.assertEqual((out['late_count']['n'], out['late_count']['median']), (1, 0))
        self.assertIsNone(out['late_ms_total'])
        self.assertEqual(out['phase_share']['down']['n'], 1)

    def test_absent_stage(self):
        self.assertIsNone(C.timeline_ranges({}))


if __name__ == '__main__':
    unittest.main()
