#!/usr/bin/env python3
"""Tests for analyze.py's attribution evidence: no class is ever decided from a ratio."""
import pathlib
import sys
import unittest

sys.dont_write_bytecode = True
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
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


if __name__ == '__main__':
    unittest.main()
