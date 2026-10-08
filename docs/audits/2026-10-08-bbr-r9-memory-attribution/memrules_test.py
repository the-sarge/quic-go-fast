#!/usr/bin/env python3
"""Tests for memrules.py: D6's synthetic accounting cases (real Linux samples, synthetic/), every label path,
and the counterexamples from the consideration of this registration (run 20261008T154923-d4f838f8)."""
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
    win, cov = M.window_medians(rows, brows, r['start_unix_ns'], br['start_unix_ns'], r['warmup_ms'], r['measure_ms'])
    return M.excess(a, base, win, cov)


class Synthetic(unittest.TestCase):
    """D6 Stage 0: known causes, measured by the unchanged sampler on minimax."""

    def test_closure(self):
        for d in sorted(SYN.iterdir()):
            if not d.name.endswith('-none'):
                ex = synthetic_excess(d.name.split('-', 1)[1], int(d.name[1]))
                self.assertAlmostEqual(ex['closure'], 0.0, places=6, msg=d.name)

    def test_retained_allocation(self):
        # 8 MiB retained: live excess 8 MiB, sustained and explained. The registered heap reading localizes it in
        # two of three blocks (recorded sensitivity, not tuned).
        hits = 0
        for b in (1, 2, 3):
            ex = synthetic_excess('retained', b)
            self.assertAlmostEqual(ex['live'], 8.0, delta=0.25)
            self.assertTrue(M.explained(ex))
            self.assertGreaterEqual(ex['live_window_share'], M.LABEL)
            hits += M.live_heap(ex)
        self.assertGreaterEqual(hits, 2)  # two sets recorded: 2 of 3 and 3 of 3 (README)

    def test_warmup_spike(self):
        for b in (1, 2, 3):
            ex = synthetic_excess('spike', b)
            self.assertTrue(M.explained(ex))
            self.assertLess(ex['live_window_share'], M.BOUNDED)
            self.assertFalse(M.live_heap(ex))

    def test_stack_growth(self):
        for b in (1, 2, 3):
            ex = synthetic_excess('stacks', b)
            self.assertAlmostEqual(ex['stacks'], 8.0, delta=0.5)
            self.assertTrue(M.explained(ex))
            self.assertFalse(M.live_heap(ex))

    def test_allocation_rate_at_fixed_live_heap(self):
        for b in (1, 2, 3):
            ex = synthetic_excess('churn', b)
            self.assertLess(abs(ex['live']), 0.25)
            self.assertFalse(M.live_heap(ex))

    def test_unexplained_resident_memory(self):
        for b in (1, 2, 3):
            ex = synthetic_excess('unexplained', b)
            self.assertAlmostEqual(ex['residual'], 8.0, delta=0.5)
            self.assertFalse(M.explained(ex))

    def test_unexplained_case_gets_no_causal_label(self):
        blocks = []
        for b in (1, 2, 3):
            ex = synthetic_excess('unexplained', b)
            blocks.append(dict(E=ex['peak'], aa_diff=0.0, ring=0.95 * ex['peak'], rx=0.95 * ex['peak'], ring_engaged=True,
                               ring_comparable=True, rx_comparable=True, usable=True, ex=ex))
        for role in ('send', 'receive'):
            self.assertTrue(M.label_cell(role, blocks, 'run')['label'].startswith('evidence gap'))


def ex_of(E, objects=None, live=None, live_window=None, objects_window=None, unused=0.0, residual=0.0, gap=0.0, coverage=1.0):
    """A consistent accounting excess by construction (stacks absorb the remainder)."""
    objects = E if objects is None else objects
    live = E / 2 if live is None else live
    cand = dict(file=0.0, objects=objects, unused=unused, free=0.0, stacks=E - objects - unused - residual - gap, metadata=0.0,
                other=0.0, residual=residual, gap=gap, live=live, bbr_explicit=0.0, ring_bytes=0.0, occupancy=0.0, peak=E)
    reno = {k: 0.0 for k in cand}
    win = {'/gc/heap/live:bytes': live if live_window is None else live_window,
           '/memory/classes/heap/objects:bytes': objects if objects_window is None else objects_window}
    return M.excess(cand, reno, win, coverage)


def blk(E=4.0, aa=0.1, ring=None, rx=0.0, engaged=True, ring_comp=True, rx_comp=True, usable=True, ex=None):
    return dict(E=E, aa_diff=aa, ring=ring, rx=rx, ring_engaged=engaged, ring_comparable=ring_comp, rx_comparable=rx_comp,
                usable=usable, ex=ex or ex_of(E))


LOCAL = 'localized to traffic-dependent live heap (cause unresolved)'


class Labels(unittest.TestCase):
    def test_gap_fewer_than_three_usable(self):
        bs = [blk(), blk(), blk(usable=False), blk(E=0.4, ex=ex_of(0.4))]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'evidence gap (fewer than three usable blocks)')

    def test_ring(self):
        self.assertEqual(M.label_cell('send', [blk(ring=3.5) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        self.assertEqual(M.label_cell('send', [blk(ring=3.5, aa=0.6) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        for bs, status in [([blk(ring=3.5, aa=0.9) for _ in range(4)], 'run'), ([blk(ring=3.5, engaged=False) for _ in range(4)], 'run'),
                           ([blk(ring=3.5, ring_comp=False) for _ in range(4)], 'run'), ([blk(ring=3.5) for _ in range(4)], 'inert')]:
            self.assertNotEqual(M.label_cell('send', bs, status)['label'], 'sender bookkeeping (ring)')
        self.assertNotEqual(M.label_cell('receive', [blk(ring=3.5) for _ in range(4)], 'run')['label'], 'sender bookkeeping (ring)')
        self.assertNotEqual(M.label_cell('send', [blk(ring=3.5)] * 2 + [blk(ring=1.0)] * 2, 'run')['label'], 'sender bookkeeping (ring)')

    def test_receiver_controller_state(self):
        bs = [blk(rx=3.5) for _ in range(3)] + [blk(rx=3.5, rx_comp=False)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'receiver-side controller state')
        bs = [blk(rx=3.5, rx_comp=False) for _ in range(2)] + [blk(rx=3.5) for _ in range(2)]
        self.assertNotEqual(M.label_cell('receive', bs, 'not run')['label'], 'receiver-side controller state')

    def test_localized_receiver(self):
        live = ex_of(4.0, live=1.9)
        self.assertEqual(M.label_cell('receive', [blk(rx=0.2, ex=live) for _ in range(4)], 'not run')['label'], LOCAL)
        # Upper bound reaches 0.30 (0.9/4 + 0.4/4 = 0.325): not localized.
        self.assertNotEqual(M.label_cell('receive', [blk(rx=0.9, aa=0.4, ex=live) for _ in range(4)], 'not run')['label'], LOCAL)
        for ex in (ex_of(4.0, live=1.0), ex_of(4.0, live=1.9, live_window=0.5), ex_of(4.0, live=1.9, objects_window=2.0),
                   ex_of(4.0, live=1.9, coverage=0.5)):
            self.assertNotEqual(M.label_cell('receive', [blk(rx=0.2, ex=ex) for _ in range(4)], 'not run')['label'], LOCAL, ex)

    def test_no_delivery_retention_label(self):
        # Flow-control occupancy is descriptive only (consideration item 5): no rule input can name delivery retention.
        live = ex_of(4.0, live=1.9)
        label = M.label_cell('receive', [blk(rx=0.2, ex=live) for _ in range(4)], 'not run')['label']
        self.assertNotIn('delivery', label)

    def test_sender_localization_needs_a_valid_ring_bound(self):
        live = ex_of(4.0, live=1.9)
        self.assertEqual(M.label_cell('send', [blk(ring=0.6, ex=live) for _ in range(4)], 'run')['label'], LOCAL)
        self.assertEqual(M.label_cell('send', [blk(ring=1.6, ex=live) for _ in range(4)], 'run')['label'], 'mixed')
        self.assertEqual(M.label_cell('send', [blk(ex=live) for _ in range(4)], 'not run (below detectability)')['label'],
                         'inconclusive (ring diagnostic not run)')
        self.assertEqual(M.label_cell('send', [blk(ring=0.6, ex=live, ring_comp=False) for _ in range(4)], 'run')['label'], 'inconclusive')

    def test_sender_localization_is_monotone_in_uncertainty(self):
        # Consideration item 11: E 3, ring removal 1, receiver removal 0; more A/A variation never yields localization.
        live = ex_of(3.0, live=1.45)
        for aa in (0.05, 0.10, 0.15, 0.30, 0.60):
            self.assertNotEqual(M.label_cell('send', [blk(E=3.0, ring=1.0, aa=aa, ex=live) for _ in range(4)], 'run')['label'], LOCAL, aa)

    def test_headroom_never_assigned(self):
        ex = ex_of(4.0, objects=0.5, unused=3.0, live=0.2)
        self.assertEqual(M.label_cell('receive', [blk(rx=0.2, ex=ex) for _ in range(4)], 'not run')['label'],
                         'inconclusive (heap unused and free classes hold the excess; no discriminating comparison)')

    def test_treatment_status_and_confounded(self):
        bs = [blk(rx=0.2, ex=ex_of(4.0, live=1.0)) for _ in range(4)]
        self.assertEqual(M.label_cell('send', bs, 'unusable')['label'], 'treatment unusable')
        self.assertEqual(M.label_cell('send', bs, 'inert')['label'], 'treatment inert')
        bs = [blk(rx=0.2, rx_comp=False, ex=ex_of(4.0, live=1.0)) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['label'], 'inconclusive (receiver-controller arm confounded or uncalibrated)')
        self.assertEqual(M.label_cell('receive', [blk(rx=0.2, ex=ex_of(4.0, live=1.0)) for _ in range(4)], 'not run')['label'], 'inconclusive')

    def test_negative_removal_limitation(self):
        # Consideration C-018, kept as a registered limitation: D6 bounds removal above, not memory change, so a
        # receiver-controller arm that adds memory (removal -2 MiB) still satisfies the upper bound.
        self.assertEqual(M.label_cell('receive', [blk(rx=-2.0, aa=0.4, ex=ex_of(4.0, live=1.9)) for _ in range(4)], 'not run')['label'], LOCAL)


class Counterexamples(unittest.TestCase):
    """The consideration's executable counterexamples, against the revised rules."""

    def test_headroom_cannot_pass_as_live_heap(self):
        # Item 3: E 10, objects/live/window-live excess 3.5, unused 6.5, residual 0.
        ex = ex_of(10.0, objects=3.5, unused=6.5, live=3.5, live_window=3.5, objects_window=3.5)
        self.assertGreaterEqual(ex['live_share'], M.LABEL)  # the doubled-live model alone would accept it
        self.assertFalse(M.live_heap(ex))
        self.assertNotEqual(M.label_cell('receive', [blk(E=10.0, rx=0.0, ex=ex) for _ in range(4)], 'not run')['label'], LOCAL)

    def test_uncertainty_cannot_cancel(self):
        # Item 4: residual -8 and gap +8 on E 10 no longer pass, in either direction.
        self.assertFalse(M.explained(ex_of(10.0, residual=-8.0, gap=8.0)))
        self.assertFalse(M.explained(ex_of(10.0, residual=8.0, gap=-8.0)))
        self.assertTrue(M.explained(ex_of(10.0, residual=1.25, gap=-1.25)))   # exactly 0.25
        self.assertFalse(M.explained(ex_of(10.0, residual=1.25, gap=-1.26)))
        bs = [blk(E=10.0, rx=3.5 * 2, ex=ex_of(10.0, residual=-8.0, gap=8.0)) for _ in range(4)]
        self.assertTrue(M.label_cell('receive', bs, 'not run')['label'].startswith('evidence gap'))

    def test_cell_aggregation_of_unexplained_blocks(self):
        # Item 12: three explained and one unexplained usable block may label; two unexplained may not.
        good, bad = blk(rx=3.5), blk(rx=3.5, ex=ex_of(4.0, residual=1.5))
        self.assertEqual(M.label_cell('receive', [good, good, good, bad], 'not run')['label'], 'receiver-side controller state')
        self.assertTrue(M.label_cell('receive', [good, good, bad, bad], 'not run')['label'].startswith('evidence gap'))

    def test_window_coverage(self):
        # Item 8: samples outside the measured window do not count; one sample is not coverage.
        def row(t, live):
            r = {k: 0 for k in M.REQUIRED}
            r.update({'unix_ns': t, '/gc/heap/live:bytes': live, 'rollup_Rss_kb': 1})
            return r
        t0, w, m = 0, 10_000, 30_000
        cand = [row(int(9.6e9), 8 << 20)] + [row(int(k * 1e9), 0) for k in range(41) if k < 10]
        reno = [row(int(k * 1e9), 0) for k in range(41)]
        win, cov = M.window_medians(cand, reno, t0, t0, w, m)
        self.assertIsNone(win['/gc/heap/live:bytes'])
        self.assertEqual(cov, 0.0)
        cand = [row(int(k * 1e9), 8 << 20) for k in range(41)]
        win, cov = M.window_medians(cand, reno, t0, t0, w, m)
        self.assertEqual(cov, 1.0)
        self.assertAlmostEqual(win['/gc/heap/live:bytes'], 8.0)
        bad = [dict(r, **{'/gc/heap/live:bytes': -1}) for r in cand]
        self.assertEqual(M.window_medians(bad, reno, t0, t0, w, m)[1], 0.0)
        self.assertFalse(M.window_ok(ex_of(4.0, live=1.9, coverage=0.79)))


class Preflight(unittest.TestCase):
    def test_decisions(self):
        mib = 2**20
        self.assertEqual(M.ring_preflight(True, [True, True], 0.13 * mib, 1.13 * mib)['decision'], 'run')
        self.assertEqual(M.ring_preflight(True, [True, True], 0.8 * mib, 1.13 * mib)['decision'], 'not run (below detectability)')
        self.assertEqual(M.ring_preflight(True, [True, True], 0.63 * mib, 1.13 * mib)['decision'], 'not run (below detectability)')  # exactly 0.5
        self.assertEqual(M.ring_preflight(False, [True, True], 0.13 * mib, 1.13 * mib)['decision'], 'unusable')
        self.assertEqual(M.ring_preflight(True, [True, False], 0.13 * mib, 1.13 * mib)['decision'], 'unusable')
        self.assertEqual(M.ring_preflight(True, [True, True], 1.13 * mib, 1.13 * mib)['decision'], 'inert')

    def test_engagement(self):
        self.assertTrue(M.ring_engaged(dict(ring_entries=4096, ring_bytes=0.13), dict(ring_bytes=1.13), 4096))
        self.assertFalse(M.ring_engaged(dict(ring_entries=32768, ring_bytes=1.13), dict(ring_bytes=1.13), 4096))


def t(goodput=90.0, control=30, fwd=800_000.0, fb=200_000.0, lost=0.0, overflow=0.002):
    return dict(goodput=goodput, control=control, fwd_per_gib=fwd, feedback_per_gib=fb, lost_per_gib=lost, overflow=overflow)


class Comparability(unittest.TestCase):
    def test_band_and_floors(self):
        pairs = [dict(cand=t(), aa=t(goodput=91.8)), dict(cand=t(), aa=t(control=29)), dict(cand=t(), aa=t())]
        band = M.aa_band(pairs, M.TRAFFIC_WAN)
        self.assertAlmostEqual(band['goodput'], 0.02)
        self.assertEqual(band['control'], 1)
        self.assertAlmostEqual(band['overflow'], M.OVERFLOW_FLOOR)
        self.assertAlmostEqual(band['fwd_per_gib'], M.COMPARABLE_FLOOR)
        self.assertEqual(M.comparable(t(), t(goodput=91.5, control=31), band), (True, []))
        self.assertEqual(M.comparable(t(), t(goodput=93.0), band), (False, ['goodput']))

    def test_uncalibrated_and_missing(self):
        self.assertIsNone(M.aa_band([dict(cand=t(), aa=t())] * 2, M.TRAFFIC_WAN))
        self.assertEqual(M.comparable(t(), t(), None), (False, ['uncalibrated']))
        band = M.aa_band([dict(cand=t(), aa=t())] * 3, M.TRAFFIC_WAN)
        self.assertEqual(M.comparable(t(), dict(t(), feedback_per_gib=None), band), (False, ['feedback_per_gib']))

    def test_packet_density_change_with_unchanged_goodput(self):
        band = M.aa_band([dict(cand=t(), aa=t())] * 3, M.TRAFFIC)
        self.assertEqual(M.comparable(t(), t(fb=260_000.0), band), (False, ['feedback_per_gib']))
        self.assertEqual(M.comparable(t(), t(lost=3000.0), band), (True, []))  # recorded, not compared

    def test_traffic_measures(self):
        rows = lambda key, vals: [{'unix_ns': int(s * 1e9), key: v} for s, v in vals]
        send = [dict(a, **b) for a, b in zip(rows('stats_packets_sent', [(9, 100), (40, 1100)]), rows('stats_packets_lost', [(9, 1), (40, 11)]))]
        recv = rows('stats_packets_sent', [(9, 10), (40, 260)])
        summary = dict(useful_bytes=2**30, goodput_mbps=90.0, control_replies=30)
        out = M.traffic(summary, send, recv, 0, 10_000, 30_000)
        self.assertEqual((out['fwd_per_gib'], out['feedback_per_gib'], out['lost_per_gib']), (1000, 250, 10))
        self.assertIsNone(M.traffic(summary, [], recv, 0, 10_000, 30_000)['fwd_per_gib'])


class UpPolicy(unittest.TestCase):
    T0 = 10**18

    def run_of(self, phases, hot):
        q = [[t, 400_000 if hot(t) else 1_000, 0] for t in range(10_000, 40_000, 10)]
        return M.queue_by_phase([[self.T0 + ms * 10**6, ph] for ms, ph in phases], q, self.T0, 10_000, 30_000)

    def test_up_then_down(self):
        e = self.run_of([(0, 0), (10_000, 3), (15_000, 4), (16_000, 3), (20_000, 5), (21_000, 2), (22_000, 3)],
                        lambda t: 20_000 <= t < 22_000)
        self.assertEqual(e['over_25ms_in_up_or_down'], 1.0)
        self.assertEqual(M.up_policy([e] * 8, 1.061, 8), 'selected ProbeBW Up policy')
        self.assertEqual(M.up_policy([e] * 8, 1.25, 8), 'unresolved')
        self.assertEqual(M.up_policy([e] * 7, 1.061, 8), 'evidence gap')
        self.assertEqual(M.up_policy([e] * 7 + [None], 1.061, 8), 'evidence gap')
        self.assertEqual(M.up_policy([e] * 7 + [dict(e, over_25ms_in_up_or_down=0.6)], 1.061, 8), 'unresolved')

    def test_down_without_up_does_not_count(self):
        # Drain -> Down with no Up: Down's samples are not "the following Down".
        e = self.run_of([(0, 1), (12_000, 2), (14_000, 3), (16_000, 4), (17_000, 3)], lambda t: 12_000 <= t < 14_000)
        self.assertEqual(e['over_25ms_in_up_or_down'], 0.0)
        self.assertEqual(M.up_policy([e] * 8, 1.0, 8), 'unresolved')

    def test_unknown_phase_stays_in_denominator(self):
        e = self.run_of([(20_000, 5), (21_000, 3), (22_000, 4), (23_000, 3)], lambda t: 10_000 <= t < 11_000 or 20_000 <= t < 21_000)
        self.assertAlmostEqual(e['over_25ms_in_up_or_down'], 0.5)
        self.assertIn('unknown', e['by_phase'])

    def test_cruise_and_refill_each_below_1ms(self):
        e = self.run_of([(0, 3), (20_000, 5), (21_000, 3), (25_000, 4), (26_000, 3)], lambda t: 20_000 <= t < 21_000)
        bad = dict(e, cruise_median_ms=0.1, refill_median_ms=1.5)
        self.assertEqual(M.up_policy([bad] * 8, 1.0, 8), 'unresolved')
        self.assertEqual(M.up_policy([dict(e, refill_median_ms=None)] * 8, 1.0, 8), 'unresolved')  # second round F6
        self.assertEqual(M.up_policy([dict(e, cruise_median_ms=0.999, refill_median_ms=0.999)] * 8, 1.0, 8), 'selected ProbeBW Up policy')
        self.assertEqual(M.up_policy([dict(e, cruise_median_ms=1.0)] * 8, 1.0, 8), 'unresolved')

    def test_quiet_queue_is_unresolved_and_missing_log_a_gap(self):
        e = self.run_of([(0, 3), (20_000, 5), (21_000, 3), (25_000, 4), (26_000, 3)], lambda t: False)
        self.assertIsNone(e['over_25ms_in_up_or_down'])
        self.assertEqual(M.up_policy([e] * 8, 1.0, 8), 'unresolved')
        self.assertIsNone(M.queue_by_phase([], [[10_000, 1, 0]], self.T0, 10_000, 30_000))


class SecondRound(unittest.TestCase):
    """Regressions for the second consideration (run 20261008T163921-46a8a643)."""
    T0 = 10**18

    def phases(self):
        return [[self.T0, 0], [self.T0 + 10_000 * 10**6, 3], [self.T0 + 15_000 * 10**6, 4], [self.T0 + 16_000 * 10**6, 3],
                [self.T0 + 20_000 * 10**6, 5], [self.T0 + 21_000 * 10**6, 2], [self.T0 + 22_000 * 10**6, 3]]

    def queue(self, step=10, drop=None):
        return [[t, 400_000 if 20_000 <= t < 22_000 else 1_000, 0] for t in range(10_000, 40_000, step) if not (drop and drop(t))]

    def test_f2_three_sample_timeline_is_a_gap(self):
        q = [[15_500, 1_000, 0], [17_000, 1_000, 0], [20_500, 400_000, 0]]
        e = M.queue_by_phase(self.phases(), q, self.T0, 10_000, 30_000)
        self.assertFalse(e['integrity'])
        self.assertEqual(M.up_policy([e] * 8, 1.0, 8), 'evidence gap')

    def test_f2_truncated_and_interior_gaps(self):
        for q in (self.queue(drop=lambda t: t >= 38_000), self.queue(drop=lambda t: 25_000 <= t < 25_500)):
            self.assertFalse(M.queue_by_phase(self.phases(), q, self.T0, 10_000, 30_000)['integrity'])

    def test_f2_malformed_phase_records(self):
        bad = self.phases()
        bad[3] = [bad[3][0], 9]
        self.assertFalse(M.queue_by_phase(bad, self.queue(), self.T0, 10_000, 30_000)['integrity'])
        swapped = self.phases()
        swapped[2], swapped[3] = swapped[3], swapped[2]
        self.assertFalse(M.queue_by_phase(swapped, self.queue(), self.T0, 10_000, 30_000)['integrity'])

    def test_f2_jitter_is_accepted(self):
        q = [[t + (3 if (t // 10) % 2 else 0), f, r] for t, f, r in self.queue()]
        e = M.queue_by_phase(self.phases(), q, self.T0, 10_000, 30_000)
        self.assertTrue(e['integrity'])
        self.assertEqual(M.up_policy([e] * 8, 1.0, 8), 'selected ProbeBW Up policy')

    def test_f6_complete_timeline_without_a_phase_is_unresolved(self):
        no_refill = [p for p in self.phases() if p[1] != 4]
        no_cruise = [[self.T0, 0], [self.T0 + 20_000 * 10**6, 5], [self.T0 + 21_000 * 10**6, 2], [self.T0 + 25_000 * 10**6, 4]]
        for ph in (no_refill, no_cruise):
            e = M.queue_by_phase(ph, self.queue(), self.T0, 10_000, 30_000)
            self.assertTrue(e['integrity'])
            self.assertEqual(M.up_policy([e] * 8, 1.0, 8), 'unresolved')
        self.assertIsNone(M.queue_by_phase([], self.queue(), self.T0, 10_000, 30_000))

    def test_f1_instrument_validity(self):
        def row(t, ok=True):
            r = {k: 0 for k in M.REQUIRED}
            r['unix_ns'] = int(t * 1e9)
            if not ok:
                r['rollup_Rss_kb'] = -1
            return r
        self.assertEqual(M.instrument_valid([row(s) for s in range(41)], 0, 10_000, 30_000), (True, None))
        self.assertFalse(M.instrument_valid([row(s) for s in range(0, 41, 2)], 0, 10_000, 30_000)[0])          # half the seconds
        self.assertFalse(M.instrument_valid([row(s, ok=s < 10) for s in range(41)], 0, 10_000, 30_000)[0])   # invalid in window
        self.assertFalse(M.instrument_valid([], 0, 10_000, 30_000)[0])

    def test_f7_eligibility(self):
        for E, want in [(-1.0, False), (0.0, False), (0.4, False), (0.5, False), (0.5001, True)]:
            self.assertEqual(M.eligibility(dict(usable=True, E=E))[0], want, E)
        self.assertEqual(M.eligibility(dict(usable=False, E=4.0)), (False, 'unusable'))
        bs = [blk(E=0.4, ex=ex_of(0.4)) for _ in range(4)]
        self.assertEqual(M.label_cell('receive', bs, 'not run')['usable'], 0)


class Perturbation(unittest.TestCase):
    def test_coverage(self):
        self.assertIsNone(M.perturbation([90.0, 91.0], [90.0, 90.0, 90.0]))
        self.assertAlmostEqual(M.perturbation([89.0, 89.1, 89.2], [90.0, 90.0, 90.0]), 89.1 / 90.0)


if __name__ == '__main__':
    unittest.main()
