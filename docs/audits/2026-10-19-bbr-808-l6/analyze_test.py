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

    def test_one_way_rule(self):
        self.assertTrue(A.ecn_decision({'a': row(0.000001), 'b': row(0.000002, contaminated=True)}))
        # A contaminated observation that stopped marking still means ECN does not stay on.
        self.assertFalse(A.ecn_decision({'a': row(0.000001), 'b': row(0.4, contaminated=True)}))
        self.assertFalse(A.ecn_decision({'a': row(0.000001), 'b': row(0.4)}))
        # Contaminated observations alone cannot establish that it stays on.
        self.assertFalse(A.ecn_decision({'b': row(0.000001, contaminated=True)}))
        self.assertFalse(A.ecn_decision({}))

    def test_ecn_failed_at_reads_a_trace(self):
        import json
        import shutil
        import tempfile
        d = Path(tempfile.mkdtemp())
        shutil.copy(Path(__file__).resolve().parent / 'testdata/port-fixture-trace.json', d / 'focal-trace.json')
        data = json.loads((d / 'focal-trace.json').read_text())
        first = data['rows'][0][data['columns'].index('unix_ns')]
        (d / 'focal-config.json').write_text(json.dumps(dict(start_unix_ns=first - A.m792.reconcile.WARMUP_NS)))
        t = A.ecn_failed_at(d)
        self.assertTrue(t is None or t >= 0)


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


class Selection(unittest.TestCase):
    def test_pick_trace(self):
        att = [dict(pair=1, workload='stream', phase='main', trace_clean=False), dict(pair=1, workload='stream', phase='rerun', trace_clean=True)]
        self.assertEqual(A.pick_trace(att, 'main', 1, 'stream'), 'rerun')  # the other attempt's clean trace
        att[0]['trace_clean'] = True
        self.assertEqual(A.pick_trace(att, 'main', 1, 'stream'), 'main')   # the classified attempt's own
        self.assertEqual(A.pick_trace(att, 'rerun', 1, 'stream'), 'rerun')
        self.assertIsNone(A.pick_trace([dict(pair=1, workload='stream', phase='main', trace_clean=False)], 'main', 1, 'stream'))

    def test_decided(self):
        clean = dict(usable=True, contaminated=False)
        self.assertTrue(A.decided(dict(clean)))  # no trace arm
        self.assertTrue(A.decided(dict(clean, trace_clean=True)))
        self.assertFalse(A.decided(dict(clean, trace_clean=False)))  # trace pass not done
        self.assertTrue(A.decided(dict(clean, trace_clean=False, trace_rerun='cap reached')))
        self.assertTrue(A.decided(dict(clean, trace_clean=False, rerun=dict(trigger='trace'))))
        self.assertFalse(A.decided(dict(usable=False, contaminated=False, trace_clean=True)))
        self.assertTrue(A.decided(dict(usable=True, contaminated=True, rerun='cap reached')))


class Follow(unittest.TestCase):
    """matrix.follow's two passes, with judge and triple stubbed: the trace never takes a slot from a triple."""

    def run_follow(self, dirty_triples, dirty_traces):
        import tempfile
        M = A.matrix
        saved = (M.judge, M.triple, A.run.ART)
        calls = []

        def judge(phase, pair, workload):
            key = (pair, workload)
            usable = not (phase == 'main' and key in dirty_triples)
            detail = {arm: dict(usable=True, contaminated=False) for arm in M.ARMS}
            if pair in M.TRACE_PAIRS:
                detail['trace'] = dict(usable=not (phase == 'main' and key in dirty_traces), contaminated=False)
            return usable, False, detail
        M.judge, M.triple, A.run.ART = judge, lambda phase, pair, workload: calls.append((pair, workload)), Path(tempfile.mkdtemp())
        try:
            decisions = M.follow()
        finally:
            M.judge, M.triple, A.run.ART = saved
        return calls, {(e['pair'], e['workload']): e for e in decisions}

    def test_triples_first_then_traces_with_the_slots_left(self):
        triples = {(4, 'stream'), (5, 'datagram')}
        traces = {(1, 'stream'), (1, 'datagram'), (2, 'stream')}
        calls, d = self.run_follow(triples, traces)
        self.assertEqual(calls[:2], [(4, 'stream'), (5, 'datagram')])  # #777's pass, in its order
        self.assertEqual(len(calls), 4)                                  # #777's cap in all
        self.assertEqual(d[(4, 'stream')]['rerun']['trigger'], 'triple')
        self.assertEqual([d[k]['rerun']['trigger'] for k in [(1, 'stream'), (1, 'datagram')]], ['trace', 'trace'])
        self.assertEqual(d[(2, 'stream')]['trace_rerun'], 'cap reached')
        self.assertTrue(all(A.decided(e) for e in d.values()))

    def test_traces_cannot_exhaust_the_triple_cap(self):
        traces = {(1, 'stream'), (1, 'datagram'), (2, 'stream'), (2, 'datagram'), (3, 'stream')}
        calls, d = self.run_follow({(5, 'stream')}, traces)
        self.assertEqual(calls[0], (5, 'stream'))
        self.assertEqual(d[(5, 'stream')]['rerun']['trigger'], 'triple')
        self.assertEqual(sum(1 for e in d.values() if e.get('trace_rerun') == 'cap reached'), 2)


if __name__ == '__main__':
    unittest.main()
