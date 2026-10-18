#!/usr/bin/env python3
"""Tests for analyze.py's attribution evidence: no class is ever decided from a ratio."""
import json
import pathlib
import sys
import unittest

sys.dont_write_bytecode = True
HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import analyze  # noqa: E402


def trace(props_by_period, mismatches=0, findings=(), representative=True, complete=True):
    periods = {p: dict(properties=props_by_period.get(p, {})) for p in ('equal', 'short', 'back', 'absent', 'resumed')}
    return dict(check=dict(coverage=dict(complete=complete), by_rule={'C4': dict(by_period={'equal': mismatches} if mismatches else {})}),
                reconcile=dict(findings=list(findings)), disturbance=dict(representative=representative), mechanism=dict(periods=periods))


def prop(events, pipe=1.0):
    rows, counts = analyze.paired(events, pipe)
    return dict(events=len(events), paired=rows, effect=counts)


class Attribution(unittest.TestCase):
    def test_no_class_from_any_ratio(self):
        for events in ([(1.6, 1.8, False)], [(0.9, 1.8, False)], [(0.68, 0.68, False)], [(1.01, 1.8, False)]):
            with self.subTest(events=events):
                a = analyze.attribute([trace({'equal': {'application_limited_crossings': prop(events)}})] * 2)
                self.assertEqual(a['class'], 'needs an argument')

    def test_effect_counts(self):
        _, counts = analyze.paired([(1.0, 1.0, False), (0.9, 1.2, False), (1.3, 1.1, False), (1.0, 1.0, True), (1.0, None, False)], 1.0)
        self.assertEqual(counts, dict(property_kept_higher=2, equal=1, property_kept_lower=1, masked=1))

    def test_event_median_pools_unequal_periods(self):
        many = prop([(0.9, 1.0, False)] * 100)
        one, two = prop([(1.1, 1.2, False)]), prop([(1.2, 1.3, False)])
        a = analyze.attribute([trace({'equal': {'x': many}, 'short': {'x': one}}), trace({'resumed': {'x': two}})])
        self.assertEqual(a['properties']['x']['events'], 102)
        self.assertAlmostEqual(a['properties']['x']['counterfactual_over_pipe_median'], 0.9)

    def test_unclassified_periods_are_excluded(self):
        a = analyze.attribute([trace({'absent': {'x': prop([(0.5, 2.0, False)])}})] * 2)
        self.assertEqual(a['properties'], {})

    def test_unbounded_actual_is_kept(self):
        a = analyze.attribute([trace({'equal': {'x': prop([(1.0, None, False), (1.0, 1.5, False)])}})] * 2)
        self.assertEqual(a['properties']['x']['actual_unbounded_events'], 2)

    def test_flow_control_crossing_needs_an_argument(self):
        a = analyze.attribute([trace({'short': {'flow_control_limited_crossings': dict(events=1, review='declared deviation')}})] * 2)
        self.assertEqual(a['class'], 'needs an argument')
        self.assertIn('flow_control_limited_crossings', a['properties'])

    def test_gates(self):
        self.assertEqual(analyze.attribute([trace({})])['class'], 'undiagnosed (instrument)')
        self.assertEqual(analyze.attribute([trace({}, representative=False)] * 2)['class'], 'undiagnosed (instrument)')
        self.assertEqual(analyze.attribute([trace({}, complete=False)] * 2)['class'], 'undiagnosed (instrument)')
        self.assertEqual(analyze.attribute([trace({}, mismatches=1)] * 2)['class'], 'needs review')
        self.assertEqual(analyze.attribute([trace({}, findings=[dict(rule='R2')])] * 2)['class'], 'needs review')
        clean = analyze.attribute([trace({})] * 2)
        self.assertEqual(clean['class'], 'needs an argument')
        self.assertIn('rule 4', clean['reason'])


class ClampCeilings(unittest.TestCase):
    """A clamp's paired ceilings take every cap binding at its crossing row (the result review's F5)."""

    def setUp(self):
        data = json.loads((HERE / 'testdata' / 'port-fixture-trace.json').read_text())
        self.t = analyze.dc.Trace(data)
        i = next(i for i, r in enumerate(self.t.rows) if r[0] == analyze.dc.PROBELOSS)
        self.r, s = self.t.rows[i], self.t.rows[i + 1]
        self.r[self.t.c['a4']] = 0  # an unlimited crossing
        self.size = self.t.v(self.r, 'size')
        port = 1000 * self.size
        s[self.t.c['inflight_long']] = port
        self.dev = dict(unix_ns=self.t.v(s, 'unix_ns'), draft=port - 174, port=port, size=self.size)

    def events(self, inflight_short=analyze.dc.INF, min_rtt_ns=0):
        self.r[self.t.c['inflight_short']] = inflight_short
        self.r[self.t.c['min_rtt_ns']] = min_rtt_ns
        self.r[self.t.c['extra']] = 0
        return analyze.clamp_events(self.t, [self.r], [self.dev])

    def test_unmasked(self):
        [(cf, ac, masked)] = self.events()
        self.assertEqual((cf, ac), (analyze.cruise_cap(self.dev['draft'], self.size), analyze.cruise_cap(self.dev['port'], self.size)))
        self.assertLess(cf, ac)
        self.assertFalse(masked)

    def test_short_term_capped(self):
        self.assertEqual(self.events(inflight_short=5 * self.size), [(5 * self.size, 5 * self.size, True)])

    def test_model_capped(self):
        [(cf, ac, masked)] = self.events(min_rtt_ns=1)  # a near-zero BDP: the model target is its floor
        self.assertEqual(cf, ac)
        self.assertLess(ac, analyze.cruise_cap(self.dev['draft'], self.size))
        self.assertTrue(masked)

    def test_unmatched_clamp_fails(self):
        self.dev['port'] += 1
        with self.assertRaises(ValueError):
            self.events()


if __name__ == '__main__':
    unittest.main()
