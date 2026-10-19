#!/usr/bin/env python3
"""Tests for #808's L6 readings (analyze.py) on synthetic inputs: #803's ECN rule and #800's marking shares."""
import sys
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))
import analyze as A  # noqa: E402


def relay(not_ect, ect0, drained=True, switch_at_ms=None):
    """A relay record for one flow: 1,400-byte packets, no drops; samples every 100 ms to 302 s. With switch_at_ms,
    every ECT(0) packet precedes that time; drained means every packet has arrived by the last sample."""
    total = not_ect + ect0
    samples = []
    for t in range(0, 302_001, 100):
        frac = min(1.0, t / 300_000)
        arrived = int(total * frac) if drained else int(total * frac * 0.99)
        samples.append([t, 0, 0, 0, 0, 0, 0, arrived * 1400])
    focal = dict(ECNIn=[not_ect, 0, ect0, 0], Delivered=total, DeliveredBytes=total * 1400)
    return dict(Forward=dict(Focal=focal, Competitor=dict(ECNIn=[3, 0, 1000, 0])), Samples=samples)


def row(share, contaminated=False, trace_failed=None):
    r = dict(focal=dict(not_ect_share=share), contaminated=contaminated)
    if trace_failed is not None or share is None:
        r['trace_ecn_failed_at_s'] = trace_failed
    return r


class ECN(unittest.TestCase):
    def test_handshake_counts_are_not_a_switch(self):
        for n in (3, 4):
            for drained in (True, False):
                self.assertIsNone(A.relay_switch(relay(n, 3_000_000, drained)), (n, drained))

    def test_share_boundary(self):
        limit = int(3_000_000 * A.NOT_ECT_SHARE_LIMIT)
        self.assertIsNone(A.relay_switch(relay(limit, 3_000_000 - limit)))
        self.assertIsNotNone(A.relay_switch(relay(limit + 1, 3_000_000 - limit - 1)))

    def test_switching_flow_gets_crossruns_estimate(self):
        # 40% Not-ECT: every ECT(0) packet arrives in the first 60% of the run (about 180 s).
        t = A.relay_switch(relay(1_200_000, 1_800_000))
        self.assertIsNotNone(t)
        self.assertAlmostEqual(t, 180.0, delta=0.2)

    def test_trace_failure_decides_despite_low_share(self):
        self.assertFalse(A.ecn_kept(row(0.000001, trace_failed=166.8)))
        self.assertTrue(A.ecn_kept(row(0.000001)))
        self.assertFalse(A.ecn_kept(row(0.002)))

    def test_contaminated_rows_do_not_decide(self):
        self.assertTrue(A.ecn_decision({'a': row(0.000001), 'b': row(0.4, contaminated=True)}))
        self.assertFalse(A.ecn_decision({'a': row(0.000001), 'b': row(0.4)}))
        self.assertFalse(A.ecn_decision({'b': row(0.000001, contaminated=True)}))  # no deciding row
        self.assertFalse(A.ecn_decision({}))


def period(checks, by_reason, limited_share):
    return dict(two_percent_checks=dict(checks=checks, checks_by_reason=by_reason, limited_share=limited_share, crossings=0,
                                        crossings_limited=0),
                inflight_longterm_over_path=1.0, window_over_path=0.8, limited_marks=dict(cycle_advances=1, max_bw_aging_events=0),
                up_exits={}, probe_interval_s_median=5.0, drops_by_bbr_phase={}, properties={})


class Marking(unittest.TestCase):
    def figures(self, checks, by_reason, limited_share):
        return A.trace_figures({p: period(checks, by_reason, limited_share) for p in A.CLASSIFIED})['equal']

    def test_application_only(self):
        f = self.figures(10, dict(application=10, flow_control=0, probe_rtt=0), 1.0)
        self.assertEqual((f['application_share'], f['supply_limited_share']), (1.0, 1.0))

    def test_flow_control_and_probe_rtt_are_not_application(self):
        for reasons in (dict(application=0, flow_control=10, probe_rtt=0), dict(application=0, flow_control=0, probe_rtt=10)):
            f = self.figures(10, reasons, 1.0)
            self.assertEqual((f['application_share'], f['supply_limited_share']), (0.0, 1.0))

    def test_mixed_reasons(self):
        f = self.figures(10, dict(application=3, flow_control=2, probe_rtt=1), 0.6)
        self.assertEqual((f['application_share'], f['supply_limited_share']), (0.3, 0.6))

    def test_no_checks(self):
        f = self.figures(0, dict(application=0, flow_control=0, probe_rtt=0), None)
        self.assertIsNone(f['application_share'])


if __name__ == '__main__':
    unittest.main()
