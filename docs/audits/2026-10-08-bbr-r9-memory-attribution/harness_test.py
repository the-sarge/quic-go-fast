#!/usr/bin/env python3
"""Synthetic cases for harness.py; run before any counted observation."""
import unittest

from harness import harness_verdict, heap_cell


def blocks(*pq, w=None):
    return [dict(P=p, Q=q, W=w) for p, q in pq]


class HarnessVerdict(unittest.TestCase):
    def test_evidence_gap(self):
        self.assertEqual(harness_verdict(blocks((1.2, 0.9), (1.2, 0.9)))['verdict'], 'evidence gap')

    def test_not_reproduced(self):
        self.assertEqual(harness_verdict(blocks((1.05, 0.9), (1.08, 0.9), (1.11, 0.9), (1.02, 0.9)))['verdict'], 'not reproduced')

    def test_not_reproduced_at_limit(self):
        self.assertEqual(harness_verdict(blocks((1.10, 1.2), (1.10, 1.2), (1.10, 1.2)))['verdict'], 'not reproduced')

    def test_perf_attachment(self):
        v = harness_verdict(blocks((1.15, 0.92), (1.13, 0.90), (1.16, 0.93), (1.14, 0.91), w=0.89))
        self.assertEqual(v['verdict'], 'perf attachment')
        self.assertAlmostEqual(v['W'], 0.89)

    def test_perf_attachment_tolerates_one_block(self):
        self.assertEqual(harness_verdict(blocks((1.15, 0.92), (1.13, 0.90), (1.16, 0.93), (1.05, 1.08)))['verdict'], 'perf attachment')

    def test_perf_attachment_needs_consistent_blocks(self):
        self.assertEqual(harness_verdict(blocks((1.15, 0.92), (1.13, 1.20), (1.16, 0.93), (1.05, 1.08)))['verdict'], 'inconclusive')

    def test_excess_under_both(self):
        self.assertEqual(harness_verdict(blocks((1.15, 1.14), (1.13, 1.12), (1.16, 1.17), (1.14, 1.15)))['verdict'],
                         'excess under both harnesses')


class HeapCell(unittest.TestCase):
    def test_all_agree(self):
        self.assertEqual(heap_cell(['delivery'] * 4), 'delivery')
        self.assertEqual(heap_cell(['bookkeeping'] * 3), 'bookkeeping')

    def test_one_disagrees(self):
        self.assertEqual(heap_cell(['delivery', 'delivery', 'delivery', 'unresolved']), 'unresolved')
        self.assertEqual(heap_cell(['delivery', 'bookkeeping', 'delivery', 'delivery']), 'unresolved')

    def test_gap(self):
        self.assertEqual(heap_cell(['delivery', 'delivery']), 'unresolved (evidence gap)')


if __name__ == '__main__':
    unittest.main()
