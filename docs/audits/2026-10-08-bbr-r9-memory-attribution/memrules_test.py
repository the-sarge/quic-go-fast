#!/usr/bin/env python3
"""Tests for memrules.py: D6's synthetic accounting cases (real Linux samples, synthetic/) and every label path."""
import json
import unittest
from pathlib import Path

import memrules as M

HERE = Path(__file__).resolve().parent
SYN = HERE / 'synthetic'


def load(d):
    _, rows, _ = M.parse_series((d / 'mem.jsonl').read_text())
    r = json.loads((d / 'result.json').read_text())
    return M.peak_account(rows, r['peak_rss_bytes'] / 2**20), rows, r


def synthetic_excess(case, b):
    base, brows, br = load(SYN / f'b{b}-none')
    a, rows, r = load(SYN / f'b{b}-{case}')
    lw = M.window_live(rows, brows, r['start_unix_ns'], br['start_unix_ns'], r['warmup_ms'], r['measure_ms'])
    return M.excess(a, base, lw), a, r


class Synthetic(unittest.TestCase):
    """D6 Stage 0: known causes, measured by the unchanged sampler on minimax."""

    def test_closure(self):
        for d in sorted(SYN.iterdir()):
            if d.name.endswith('-none'):
                continue
            ex, _, _ = synthetic_excess(d.name.split('-', 1)[1], int(d.name[1]))
            self.assertAlmostEqual(ex['closure'], 0.0, places=6, msg=d.name)

    def test_retained_allocation(self):
        # 8 MiB retained: live excess 8 MiB, sustained, and GC-aligned live heap explains the excess.
        for b in (1, 2, 3):
            ex, _, _ = synthetic_excess('retained', b)
            self.assertAlmostEqual(ex['live'], 8.0, delta=0.25)
            self.assertTrue(M.explained(ex))
            self.assertTrue(M.live_heap(ex), ex)

    def test_warmup_spike(self):
        # 24 MiB live during warmup only: the window test refuses a live-heap reading.
        for b in (1, 2, 3):
            ex, _, _ = synthetic_excess('spike', b)
            self.assertTrue(M.explained(ex))
            self.assertLess(ex['live_window_share'], M.BOUNDED, ex)
            self.assertFalse(M.live_heap(ex))

    def test_stack_growth(self):
        for b in (1, 2, 3):
            ex, _, _ = synthetic_excess('stacks', b)
            self.assertAlmostEqual(ex['stacks'], 8.0, delta=0.5)
            self.assertTrue(M.explained(ex))
            self.assertLess(ex['live_share'], M.BOUNDED)
            self.assertFalse(M.live_heap(ex))

    def test_allocation_rate_at_fixed_live_heap(self):
        # Four times the allocation rate at the same live heap: a small excess, never live heap.
        for b in (1, 2, 3):
            ex, _, _ = synthetic_excess('churn', b)
            self.assertLess(ex['peak'], 2 * M.DETECTABLE_MIB + 0.25)
            self.assertLess(abs(ex['live']), 0.25)
            self.assertFalse(M.live_heap(ex))

    def test_unexplained_resident_memory(self):
        for b in (1, 2, 3):
            ex, _, _ = synthetic_excess('unexplained', b)
            self.assertAlmostEqual(ex['residual'], 8.0, delta=0.5)
            self.assertFalse(M.explained(ex))

    def test_unexplained_case_gets_no_causal_label(self):
        # Even with arms that would otherwise give a causal label, an unexplained excess is an evidence gap.
        blocks = []
        for b in (1, 2, 3):
            ex, _, _ = synthetic_excess('unexplained', b)
            blocks.append(dict(E=ex['peak'], aa_diff=0.0, ring=0.95 * ex['peak'], rx=0.95 * ex['peak'], ring_engaged=True,
                               ring_comparable=True, rx_comparable=True, usable=True, ex=ex, occ_excess=None))
        for role in ('send', 'receive'):
            self.assertTrue(M.label_cell(role, blocks, 'run')['label'].startswith('evidence gap'))


def ex_of(E, live=None, heap=None, residual=0.0, live_window=None):
    """A consistent accounting excess: heap and live shares by construction."""
    live = E / 2 if live is None else live
    heap = E if heap is None else heap
    ex = dict(file=0.0, objects=heap, unused=0.0, free=0.0, stacks=E - heap - residual, metadata=0.0, other=0.0, residual=residual,
              gap=0.0, live=live, bbr_explicit=0.0, ring_bytes=0.0, peak=E, closure=0.0)
    return M.excess(dict(ex, peak=E), dict({k: 0.0 for k in ex}, peak=0.0), live if live_window is None else live_window)


def blk(E=4.0, aa=0.1, ring=None, rx=None, engaged=True, ring_comp=True, rx_comp=True, usable=True, ex=None, occ=None):
    return dict(E=E, aa_diff=aa, ring=ring, rx=rx, ring_engaged=engaged, ring_comparable=ring_comp, rx_comparable=rx_comp,
                usable=usable, ex=ex or ex_of(E), occ_excess=occ)


class Labels(unittest.TestCase):
    def test_gap_fewer_than_three_usable(self):
        bs = [blk(rx=0.1), blk(rx=0.1), blk(usable=False, rx=0.1), blk(E=0.4, ex=ex_of(0.4), rx=0.0)]
        self.assertEqual(M.label_cell('send', bs, 'run')['label'], 'evidence gap (fewer than three usable blocks)')

    def test_ring(self):
        bs = [blk(ring=3.5, rx=0.0) for _ in range(4)]
        self.assertEqual(M.label_cell('send', bs, 'run')['label'], 'sender bookkeeping (ring)')
        # Removal must exceed the A/A variation: 3.5/4 - 0.6/4 = 0.725 still counts; 0.9/4 more A/A does not.
        self.assertEqual(M.label_cell('send', [blk(ring=3.5, aa=0.6, rx=0.0) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        self.assertNotEqual(M.label_cell('send', [blk(ring=3.5, aa=0.9, rx=0.0) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        # Not engaged, not comparable, or not run: no ring label.
        self.assertNotEqual(M.label_cell('send', [blk(ring=3.5, rx=0.0, engaged=False) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        self.assertNotEqual(M.label_cell('send', [blk(ring=3.5, rx=0.0, ring_comp=False) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        self.assertNotEqual(M.label_cell('send', [blk(ring=3.5, rx=0.0) for _ in range(4)], 'inert')['label'], 'sender bookkeeping (ring)')
        # The ring label is a sender label.
        self.assertNotEqual(M.label_cell('receive', bs, 'run')['label'], 'sender bookkeeping (ring)')

    def test_ring_needs_three_of_four(self):
        bs = [blk(ring=3.5, rx=0.0)] * 2 + [blk(ring=1.0, rx=0.0)] * 2
        self.assertNotEqual(M.label_cell('send', bs, 'run')['label'], 'sender bookkeeping (ring)')

    def test_receiver_controller_state(self):
        bs = [blk(rx=3.5) for _ in range(3)] + [blk(rx=3.5, rx_comp=False)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'receiver-side controller state')
        bs = [blk(rx=3.5, rx_comp=False) for _ in range(2)] + [blk(rx=3.5) for _ in range(2)]
        self.assertNotEqual(M.label_cell('receive', bs, 'not run')['label'], 'receiver-side controller state')

    def test_localized_needs_bounded_removal_and_live_heap(self):
        live = ex_of(4.0, live=1.9)
        bs = [blk(rx=0.2, ex=live) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'localized to traffic-dependent live heap (cause unresolved)')
        # Upper bound reaches 0.30: not localized (0.9/4 + 0.4/4 = 0.325).
        bs = [blk(rx=0.9, aa=0.4, ex=live) for _ in range(4)]
        self.assertNotEqual(M.label_cell('receive', bs, 'not run')['label'][:9], 'localized')
        # Live heap missing at the peak or over the window: not localized.
        for ex in (ex_of(4.0, live=1.0), ex_of(4.0, live=1.9, live_window=0.5), ex_of(4.0, live=1.9, heap=2.0)):
            bs = [blk(rx=0.2, ex=ex) for _ in range(4)]
            self.assertNotEqual(M.label_cell('receive', bs, 'not run')['label'][:9], 'localized', ex)

    def test_delivery_retention_needs_occupancy(self):
        live = ex_of(4.0, live=1.9)
        bs = [blk(rx=0.2, ex=live, occ=1.5) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'localized to traffic-dependent live heap: delivery retention')
        bs = [blk(rx=0.2, ex=live, occ=1.0) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'localized to traffic-dependent live heap (cause unresolved)')

    def test_sender_localization_blocked_by_partial_ring(self):
        live = ex_of(4.0, live=1.9)
        bs = [blk(ring=1.6, rx=0.0, ex=live) for _ in range(4)]
        self.assertEqual(M.label_cell('send', bs, 'run')['label'], 'mixed')
        bs = [blk(ring=0.6, rx=0.0, ex=live) for _ in range(4)]
        self.assertEqual(M.label_cell('send', bs, 'run')['label'], 'localized to traffic-dependent live heap (cause unresolved)')

    def test_headroom_never_assigned(self):
        bs = [blk(rx=0.2, ex=ex_of(4.0, live=0.2)) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'inconclusive (headroom-dominant accounting; no discriminating comparison)')

    def test_treatment_status_and_confounded(self):
        bs = [blk(rx=0.2, ex=ex_of(4.0, live=1.0)) for _ in range(4)]
        self.assertEqual(M.label_cell('send', bs, 'unusable')['label'], 'treatment unusable')
        self.assertEqual(M.label_cell('send', bs, 'inert')['label'], 'treatment inert')
        bs = [blk(rx=0.2, rx_comp=False, ex=ex_of(4.0, live=1.0)) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'inconclusive (receiver-controller arm confounded)')
        bs = [blk(rx=0.2, ex=ex_of(4.0, live=1.0)) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'inconclusive')

    def test_unexplained_blocks(self):
        bs = [blk(rx=3.5, ex=ex_of(4.0, residual=1.5)) for _ in range(2)] + [blk(rx=3.5) for _ in range(2)]
        self.assertTrue(M.label_cell('receive', bs, 'not run')['label'].startswith('evidence gap (resident'))


class Preflight(unittest.TestCase):
    def test_decisions(self):
        mib = 2**20
        self.assertEqual(M.ring_preflight(True, [True, True], 0.13 * mib, 1.13 * mib)['decision'], 'run')
        self.assertEqual(M.ring_preflight(True, [True, True], 0.8 * mib, 1.13 * mib)['decision'], 'not run (below detectability)')
        self.assertEqual(M.ring_preflight(False, [True, True], 0.13 * mib, 1.13 * mib)['decision'], 'unusable')
        self.assertEqual(M.ring_preflight(True, [True, False], 0.13 * mib, 1.13 * mib)['decision'], 'unusable')
        self.assertEqual(M.ring_preflight(True, [True, True], 1.13 * mib, 1.13 * mib)['decision'], 'inert')

    def test_engagement(self):
        self.assertTrue(M.ring_engaged(dict(ring_entries=4096, ring_bytes=0.13), dict(ring_bytes=1.13), 4096))
        self.assertFalse(M.ring_engaged(dict(ring_entries=32768, ring_bytes=1.13), dict(ring_bytes=1.13), 4096))


class Comparability(unittest.TestCase):
    def test_band(self):
        blocks = [dict(cand=dict(goodput=90.0, control=30, overflow=0.002), aa=dict(goodput=91.8, control=30, overflow=0.0021)),
                  dict(cand=dict(goodput=90.0, control=30, overflow=0.002), aa=dict(goodput=89.1, control=29, overflow=0.002))]
        band = M.aa_band(blocks)
        self.assertAlmostEqual(band['goodput'], 0.02)
        self.assertEqual(band['control'], 1)
        self.assertAlmostEqual(band['overflow'], M.OVERFLOW_FLOOR)
        c = dict(goodput=90.0, control=30, overflow=0.002)
        self.assertEqual(M.comparable(c, dict(goodput=91.5, control=31, overflow=0.0024), band), (True, []))
        self.assertEqual(M.comparable(c, dict(goodput=93.0, control=30, overflow=0.002), band), (False, ['goodput']))


class UpPolicy(unittest.TestCase):
    def test_queue_and_rule(self):
        t0 = 10**18
        phases = [[t0, 0], [t0 + 10_000 * 10**6, 3], [t0 + 20_000 * 10**6, 5], [t0 + 21_000 * 10**6, 2], [t0 + 22_000 * 10**6, 3]]
        q = [[t, 400_000 if 20_000 <= t < 22_000 else 1_000, 0] for t in range(10_000, 40_000, 10)]
        e = M.queue_by_phase(phases, q, t0, 10_000, 30_000)
        self.assertEqual(e['over_25ms_in_up_or_down'], 1.0)
        self.assertLess(e['cruise_refill_median_ms'], 1.0)
        self.assertEqual(M.up_policy([e, e], 1.061), 'selected ProbeBW Up policy')
        self.assertEqual(M.up_policy([e, e], 1.25), 'unresolved')
        self.assertEqual(M.up_policy([e, dict(e, over_25ms_in_up_or_down=0.6)], 1.0), 'unresolved')
        self.assertEqual(M.up_policy([], 1.0), 'evidence gap')


if __name__ == '__main__':
    unittest.main()
