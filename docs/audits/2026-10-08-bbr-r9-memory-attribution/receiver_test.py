#!/usr/bin/env python3
"""Synthetic cases for receiver.py, written and passing before any counted observation."""
import math
import random
import unittest

import receiver as rc

F_REF = 2.99e9  # MPERF ticks per second of C0 time


def obs(T=10.0, I=2.0e10, cpi=0.5, uncounted=1.0, freq=0.85, closure=1.0, jitter=0.0, rnd=None):
    """Synthetic per-GiB receiver counters: T seconds of task-clock at a running clock of freq x F_REF.

    uncounted: A/C, the ratio of APERF to PMC cycles; closure scales task-clock against MPERF.
    """
    j = (lambda: 1 + rnd.uniform(-jitter, jitter)) if rnd else (lambda: 1.0)
    C = I * cpi * j()
    A = C * uncounted * j()
    M = A / freq * j()
    return dict(T=M / F_REF * closure, I=I, C=C, A=A, M=M)


def blocks(arm_kw, n=6, jitter=0.002, seed=1):
    rnd = random.Random(seed)
    return [dict(reno=obs(jitter=jitter, rnd=rnd), aa=obs(jitter=jitter, rnd=rnd), arm=obs(jitter=jitter, rnd=rnd, **arm_kw))
            for _ in range(n)]


class Identity(unittest.TestCase):
    def test_partition_is_exact(self):
        rnd = random.Random(7)
        for _ in range(200):
            a, b = obs(cpi=rnd.uniform(.3, .8), uncounted=rnd.uniform(1, 2), freq=rnd.uniform(.5, 1.1)), obs()
            c = rc.components(a, b)
            self.assertAlmostEqual(sum(c[k] for k in rc.COMPONENTS), c['t'], places=12)


class Verdicts(unittest.TestCase):
    def test_clock_rate(self):
        # Equal cycles, 14% lower running clock: the hypothesis.
        v = rc.verdict(blocks(dict(freq=0.85 / 1.15)))
        self.assertEqual(v['verdict'], 'clock rate', v)

    def test_uncounted(self):
        # Equal PMC cycles and clock, more APERF clocks inside the scheduled time: the competitor.
        v = rc.verdict(blocks(dict(uncounted=1.15)))
        self.assertEqual(v['verdict'], 'uncounted receiver work', v)

    def test_counted_work(self):
        v = rc.verdict(blocks(dict(I=2.0e10 * 1.15)))
        self.assertEqual(v['verdict'], 'counted receiver work', v)

    def test_counted_cpi(self):
        v = rc.verdict(blocks(dict(cpi=0.5 * 1.15)))
        self.assertEqual(v['verdict'], 'counted receiver work', v)
        self.assertGreater(v['components']['cpi']['median'], 0.1)

    def test_mixed(self):
        v = rc.verdict(blocks(dict(freq=0.85 / 1.07, uncounted=1.07)))
        self.assertTrue(v['verdict'].startswith('mixed'), v)
        self.assertIn('clock rate', v['verdict'])
        self.assertIn('uncounted receiver work', v['verdict'])

    def test_no_excess(self):
        v = rc.verdict(blocks(dict()))
        self.assertEqual(v['verdict'], 'no in-window CPU-time excess', v)

    def test_closure_failure(self):
        v = rc.verdict(blocks(dict(freq=0.85 / 1.15, closure=1.03)))
        self.assertTrue(v['verdict'].startswith('unusable counters'), v)

    def test_evidence_gap(self):
        self.assertEqual(rc.verdict(blocks(dict(freq=0.7), n=3))['verdict'], 'evidence gap')

    def test_lead_inside_aa(self):
        # A/A noise in the clock factor offset by work, so A/A task-clock stays flat while the clock range is wide.
        rnd = random.Random(3)
        bl = []
        for k in range(6):
            s = 1.12 if k == 0 else 1.0
            bl.append(dict(reno=obs(), aa=obs(freq=0.85 / s, I=2.0e10 / s),
                           arm=obs(freq=0.85 / 1.10 * (1 + rnd.uniform(-.001, .001)))))
        v = rc.verdict(bl)
        self.assertTrue(v['verdict'].startswith('inconclusive: clock rate leads but lies inside'), v)


class Idle(unittest.TestCase):
    def test_rates(self):
        snap = lambda u, t: {'state0': dict(name='C1', usage=u, time_us=t), 'state1': dict(name='C3', usage=2 * u, time_us=3 * t)}
        idle = dict(cpus=[12, 13], start=dict(cpus={'12': snap(0, 0), '13': snap(0, 0)}), stop=dict(cpus={'12': snap(10, 1e6), '13': snap(30, 1e6)}))
        r = rc.idle_rates(idle, gib=2.0, window_s=10.0)
        self.assertEqual(r['C1']['entries_per_gib'], 20.0)
        self.assertEqual(r['C3']['entries_per_gib'], 40.0)
        self.assertTrue(math.isclose(r['C3']['residency'], 0.3))


if __name__ == '__main__':
    unittest.main()
