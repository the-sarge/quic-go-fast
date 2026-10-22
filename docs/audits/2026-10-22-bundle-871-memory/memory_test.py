"""Prospective outcome and completeness tests; no host measurements."""
import copy
from pathlib import Path
import unittest

import memory


def blocks(port=1, prior=1, cand=1, aa=1):
    out = []
    ratios = dict(zip(memory.ARM_NAMES, [1, aa, port, prior, cand]))
    for b in range(1, 6):
        rows = [dict(arm=a, pair=b, seed=9000+b, send={m: (10+b)*ratio for m in memory.METRICS})
                for a, ratio in ratios.items()]
        out.append(dict(block=b, contaminated=False, observations=rows))
    return out


class MemoryTests(unittest.TestCase):
    def test_placement_intervals_and_nonmonotonic_outcomes(self):
        self.assertIn('historical appearance in abfa5456..d94b3ec2', memory.summarize_blocks(blocks(prior=1.2, cand=1.2))['placement'])
        self.assertIn('appearance after d94b3ec2 in the bundle interval', memory.summarize_blocks(blocks(cand=1.2))['placement'])
        self.assertIn('rise present at or before the port', memory.summarize_blocks(blocks(port=1.2, prior=1.2, cand=1.2))['placement'])
        p = memory.summarize_blocks(blocks(prior=1.2))['placement']
        self.assertIn('not reproduced on this candidate', p)
        self.assertIn('nonmonotonic outcomes; no causal attribution', p)

    def test_aa_range_does_not_erase_raise(self):
        r = memory.summarize_blocks(blocks(cand=1.2, aa=1.3))
        self.assertTrue(r['arms']['cand-reno']['raised'])
        self.assertAlmostEqual(r['high_memory_threshold_mib'], 14.3)
        self.assertEqual(r['arms']['reno-reno']['high_memory'], 1)
        self.assertEqual(r['arms']['cand-reno']['high_memory'], 4)

    def test_no_partial_contaminated_or_duplicate_blocks(self):
        cases = [blocks()[:-1], blocks(), blocks(), blocks()]
        cases[1][0]['contaminated'] = True
        cases[2][0]['observations'].pop()
        cases[3][1] = copy.deepcopy(cases[3][0])
        for case in cases:
            with self.assertRaises(AssertionError):
                memory.summarize_blocks(case)

    def test_historical_formulas_are_verbatim(self):
        here = Path(__file__).parent
        source = (here / 'analyze.py').read_text().split('\nrows = [load(p.parent)')[0].rstrip() + '\n'
        self.assertEqual(source, (here / 'memory_load.py').read_text())


if __name__ == '__main__':
    unittest.main()
