#!/usr/bin/env python3
"""Tests for figures.py's input table: incomplete checks render, zero ratios are kept."""
import pathlib
import sys
import unittest

sys.dont_write_bytecode = True
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import figures  # noqa: E402


def period(r2=None, r3=None, r4=None):
    ok = lambda **v: dict(status='ok', reason=None, **v)
    return dict(r1=ok(declared=10, dropped=10), r2=r2 or ok(ratio=1.0), r3=r3 or ok(max_over_capacity=0.97),
                r4=r4 or ok(min_ms=100.2, max_ms=100.5))


def summary(per):
    trace = dict(reconcile=dict(periods={p: per for p in ('equal', 'short', 'back', 'absent', 'resumed')}))
    return dict(pairs={'stream/p1': dict(selected='main', trace=trace), 'datagram/p1': dict(selected='main', trace=trace)})


class Inputs(unittest.TestCase):
    def test_normal(self):
        text = figures.inputs(summary(period()))
        self.assertIn('| none |', text)

    def test_incomplete_checks_render(self):
        missing = dict(status='incomplete', reason='missing')
        text = figures.inputs(summary(period(r2=missing, r3=missing, r4=missing)))
        self.assertIn('R2 incomplete, R3 incomplete, R4 incomplete', text)

    def test_zero_ratio_is_kept(self):
        text = figures.inputs(summary(period(r2=dict(status='fail', reason='x', ratio=0.0))))
        self.assertIn('0.0000–0.0000', text)
        self.assertIn('R2 fail', text)


if __name__ == '__main__':
    unittest.main()
