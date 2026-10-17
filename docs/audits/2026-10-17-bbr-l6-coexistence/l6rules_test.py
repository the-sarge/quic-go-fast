#!/usr/bin/env python3
"""Synthetic cases for #777's registered rules (l6rules.py) and the runner's period assignment (run.py).

Run before any observation of this record existed: python3 l6rules_test.py
"""
import unittest

import l6rules as r
import run

PERIODS = list(run.PERIODS)


def fake(comp, focal=50.0, comp_p95=150.0, focal_p95=150.0, cpu=10.0, rss=20.0):
    """A summary whose competitor delivers `comp` Mbit/s in every period unless comp is a dict."""
    comp = comp if isinstance(comp, dict) else {p: comp for p in PERIODS}
    side = lambda mbps, p95: dict(periods={p: dict(mbps=mbps[p] if isinstance(mbps, dict) else mbps) for p in PERIODS},
                                  control_periods=dict(method='exact', periods={p: dict(p95_ms=p95) for p in PERIODS}),
                                  control_p95_ms=p95, send=dict(cpu_seconds_per_gib=cpu, peak_rss_mib=rss),
                                  receive=dict(cpu_seconds_per_gib=cpu, peak_rss_mib=rss))
    return dict(competitor=side(comp, comp_p95), focal=side(focal, focal_p95))


def triples(cand_comp, reno_comp=50.0, aa_comp=50.0, **kw):
    """Every pair of both workloads alike, unless cand_comp is a list (one value per pair)."""
    out = {}
    for w in r.WORKLOADS:
        for pair in range(1, r.PAIRS + 1):
            c = cand_comp[pair - 1] if isinstance(cand_comp, list) else cand_comp
            out[(w, pair)] = dict(reno=fake(reno_comp), cand=fake(c, **kw), aa=fake(aa_comp))
    return out


class Classification(unittest.TestCase):
    def test_period_outcomes(self):
        cases = [
            ([1.0] * 5, 'acceptable'),
            ([0.5] * 5, 'adverse'),
            ([0.8, 0.8, 0.8, 0.6, 0.6], 'inconclusive'),
            ([0.8, 0.8, 0.8, 0.8, 0.6], 'acceptable'),
            ([0.6, 0.6, 0.6, 0.6, 0.9], 'adverse'),
            ([0.70] * 4 + [0.1], 'acceptable'),        # the threshold itself is acceptable
            ([0.6999] * 4 + [1.0], 'adverse'),
            ([None, 0.8, 0.8, 0.8, 0.6], 'inconclusive'),
            ([None, 0.8, 0.8, 0.8, 0.8], 'acceptable'),
            ([None, None, 0.5, 0.5, 0.5], 'inconclusive'),
            ([None] * 5, 'inconclusive'),
        ]
        for ratios, want in cases:
            self.assertEqual(r.classify_period(ratios), want, ratios)

    def test_needs_five_pairs(self):
        with self.assertRaises(AssertionError):
            r.classify_period([1.0] * 4)

    def test_overall(self):
        cells = {(w, p): 'acceptable' for w in r.WORKLOADS for p in r.CLASSIFIED}
        self.assertEqual(r.overall(cells), 'acceptable')
        self.assertEqual(r.overall({**cells, ('datagram', 'resumed'): 'adverse'}), 'adverse')
        self.assertEqual(r.overall({**cells, ('stream', 'short'): 'inconclusive'}), 'inconclusive')
        self.assertEqual(r.overall({**cells, ('stream', 'short'): 'inconclusive', ('stream', 'equal'): 'adverse'}), 'adverse')
        with self.assertRaises(AssertionError):
            r.overall({('stream', 'equal'): 'acceptable'})

    def test_denominator_floor(self):
        self.assertIsNone(r.pair_ratio(10.0, 4.99))
        self.assertAlmostEqual(r.pair_ratio(10.0, 5.0), 2.0)
        self.assertIsNone(r.pair_ratio(None, 50.0))


class Analysis(unittest.TestCase):
    def test_end_to_end_outcomes(self):
        self.assertEqual(r.analyze(triples(50.0))['classification'], 'acceptable')
        self.assertEqual(r.analyze(triples(20.0))['classification'], 'adverse')   # 0.40
        self.assertEqual(r.analyze(triples(35.0))['classification'], 'acceptable')  # exactly 0.70
        self.assertEqual(r.analyze(triples([40, 40, 40, 30, 30]))['classification'], 'inconclusive')

    def test_one_adverse_period_decides(self):
        comp = {p: 50.0 for p in PERIODS}
        comp['resumed'] = 10.0
        t = triples(50.0)
        for pair in range(1, r.PAIRS + 1):
            t[('datagram', pair)]['cand'] = fake(comp)
        out = r.analyze(t)
        self.assertEqual(out['cells']['datagram/resumed'], 'adverse')
        self.assertEqual(out['cells']['stream/resumed'], 'acceptable')
        self.assertEqual(out['classification'], 'adverse')

    def test_unclassified_periods_never_decide(self):
        comp = {p: 50.0 for p in PERIODS}
        comp['back'], comp['absent'] = 1.0, 0.0
        t = triples(50.0)
        for k in t:
            t[k]['cand'] = fake(comp)
        self.assertEqual(r.analyze(t)['classification'], 'acceptable')

    def test_aa_never_changes_the_outcome(self):
        for cand in [50.0, 20.0, [40, 40, 40, 30, 30]]:
            base = r.analyze(triples(cand))['classification']
            for aa in [0.0, 5.0, 500.0]:
                self.assertEqual(r.analyze(triples(cand, aa_comp=aa))['classification'], base)

    def test_missing_triples_make_a_period_inconclusive(self):
        t = triples(50.0)
        t[('stream', 1)] = t[('stream', 2)] = None
        out = r.analyze(t)
        self.assertEqual(out['cells']['stream/equal'], 'inconclusive')
        self.assertEqual(out['classification'], 'inconclusive')

    def test_review_flags_never_change_the_outcome(self):
        out = r.analyze(triples(50.0, comp_p95=400.0, focal_p95=900.0, cpu=99.0, rss=99.0))
        self.assertEqual(out['classification'], 'acceptable')
        self.assertTrue(out['competitor_control']['stream/equal']['raised'])
        self.assertTrue(out['focal_flags']['stream/control_p95']['raised'])
        self.assertTrue(out['focal_flags']['datagram/send_peak_rss']['raised'])
        self.assertFalse(r.analyze(triples(50.0))['focal_flags']['stream/receive_cpu_per_gib']['raised'])


class Usability(unittest.TestCase):
    def summary(self, pause=(240.01, 270.0), lag=0.5, late=2.0, cpu=30.0, error=None):
        side = lambda: dict(control_error=error, send=dict(total_cpu_seconds=cpu), receive=dict(total_cpu_seconds=cpu))
        return dict(competitor=dict(side(), bulk_pause_observed_s=list(pause)), focal=side(),
                    relay=dict(max_arrival_lag_ms=lag, forward=dict(max_late_ms=late), reverse=dict(max_late_ms=late)))

    def test_health(self):
        ok = dict(exit_codes=[0, 0, 0, 0], relay_exit=0)
        self.assertEqual(r.health(self.summary(), ok, 370), [])
        self.assertTrue(r.health(self.summary(), dict(ok, exit_codes=[0, 1, 0, 0]), 370))
        self.assertTrue(r.health(None, ok, 370))
        self.assertTrue(r.health(self.summary(pause=(241.0, 270.0)), ok, 370))
        self.assertTrue(r.health(self.summary(pause=()), ok, 370))
        self.assertTrue(r.health(self.summary(lag=6.0), ok, 370))
        self.assertTrue(r.health(self.summary(late=5.1), ok, 370))
        self.assertTrue(r.health(self.summary(error='timeout'), ok, 370))
        # 600 CPU seconds over 370 s is 0.81 of the competitor's two cores.
        self.assertTrue(any('competitor' in x for x in r.health(self.summary(cpu=600.0), ok, 370)))

    def test_contamination(self):
        def rows(foreign_ticks):
            out, busy = [], 0
            for i, f in enumerate(foreign_ticks):
                busy += f
                out.append(dict(unix_ns=i * 1_000_000_000, cpus={str(c): dict(busy=busy if c == 2 else 0) for c in range(32)},
                                fixture_ticks={'relay': 0}))
            return out
        quiet = r.contamination(rows([0] * 20), [0, 30e9], [2, 3], [18, 19])
        self.assertFalse(quiet['contaminated'])
        busy = r.contamination(rows([0] * 10 + [60] + [0] * 9), [0, 30e9], [2, 3], [18, 19])
        self.assertTrue(busy['contaminated'])  # one 0.6-core second exceeds the 0.50 peak

    def test_correctness(self):
        clean = dict(corrupt=0, duplicates=0)
        self.assertEqual(r.correctness({'focal': clean, 'competitor': clean}), [])
        self.assertEqual(len(r.correctness({'focal': dict(corrupt=1, duplicates=0), 'competitor': dict(corrupt=0, duplicates=2)})), 2)

    def test_choose_triple(self):
        self.assertEqual(r.choose_triple([('a', True, False)]), ('a', 'clean'))
        self.assertEqual(r.choose_triple([('a', True, True), ('b', True, False)]), ('b', 'clean'))
        self.assertEqual(r.choose_triple([('a', False, False), ('b', True, True)]), ('a', 'unusable'))
        self.assertEqual(r.choose_triple([('a', True, True), ('b', False, False)]), ('a', 'contaminated'))
        self.assertEqual(r.choose_triple([('a', True, True), ('b', True, True), ('c', True, False)]), ('a', 'contaminated'))


class ControlPeriods(unittest.TestCase):
    START = 1_000_000_000_000

    def control(self, offers, latencies, skipped=0, unresolved=0):
        opps = [[self.START + int(s * 1e9), self.START + int(s * 1e9) + 1000] for s in offers]
        return dict(opportunity_times_ns=opps, latency_ns=latencies, skipped=skipped, unresolved=unresolved,
                    offered=len(offers) - skipped, replies=len(latencies))

    def test_exact_assignment(self):
        offers = list(range(300))
        lat = [int(100e6) if s < 100 else int(200e6) for s in offers]
        out = run.control_periods(self.control(offers, lat), self.START)
        self.assertEqual(out['method'], 'exact')
        self.assertEqual(out['periods']['equal']['replies'], 100)
        self.assertAlmostEqual(out['periods']['equal']['p95_ms'], 100.0)
        self.assertAlmostEqual(out['periods']['short']['p95_ms'], 200.0)
        self.assertEqual(out['periods']['resumed']['replies'], 30)

    def test_reconstruction_with_a_skip(self):
        # The request offered at t = 10 s takes 1.5 s, so the t = 11 s opportunity is skipped.
        offers = list(range(300))
        lat = [int(1.5e9) if s == 10 else int(100e6) for s in offers if s != 11]
        out = run.control_periods(self.control(offers, lat, skipped=1), self.START)
        self.assertEqual(out['method'], 'reconstructed')
        self.assertEqual(out['periods']['equal']['replies'], 99)
        self.assertAlmostEqual(out['periods']['equal']['p95_ms'], 100.0)

    def test_inconsistent_counts_are_unusable(self):
        offers = list(range(300))
        lat = [int(100e6)] * 298
        out = run.control_periods(self.control(offers, lat, skipped=1), self.START)
        self.assertEqual(out['method'], 'unusable')
        self.assertIsNone(out['periods']['equal']['p95_ms'])


class RelayPeriods(unittest.TestCase):
    def test_drops_and_queue_per_period(self):
        samples = []
        for ms in range(-1000, 300000, 100):
            drops_c = 0 if ms < 100000 else (5 if ms < 200000 else 7)
            samples.append([ms, 1000, 3000 if 100000 <= ms < 200000 else 500, 0, 0, 1, drops_c, 0, 0, 2_000_000])
        out = run.relay_periods(samples)
        self.assertEqual(out['equal']['competitor_drops'], 0)
        self.assertEqual(out['short']['competitor_drops'], 5)
        self.assertEqual(out['back']['competitor_drops'], 2)
        self.assertAlmostEqual(out['short']['competitor_queue_mean_bytes'], 3000)
        self.assertAlmostEqual(out['equal']['max_late_ms'], 2.0)


if __name__ == '__main__':
    unittest.main()
