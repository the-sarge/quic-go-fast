#!/usr/bin/env python3
"""Synthetic cases for #738's registered rules (rules.py). Run: python3 rules_test.py"""
import unittest

import rules

W = 20_000_000_000  # a 20 s window, in ns


def hand(u_w=0.6, blocked=None, overlap=None, late_ns=0, paced_stops=0, over2q=0, max_q=0.9, signal=None, kind='credit',
         sampled=800, early=0, submit_ns=10_000, groups=1000):
    """A synthetic send.hand.json. blocked/overlap: shares of the window by reason."""
    blocked = blocked or {}
    overlap = overlap or {}
    waits = {}
    for k in ['credit', 'queue']:
        n = 8000 if k == kind else 0
        s = sampled if k == kind else 0
        e = early if k == kind else 0
        sig = (signal or 1000) * (s - e)
        waits[k] = dict(waits=n, pre_block_ns=0, blocked_ns=int(blocked.get(k, 0) * W / 8), resume_ns=0,
                        no_block=0, resumed=n, refused_again=0, wake_cases=[0, 0, 0, 0, n, 0], sampled=s,
                        sampled_total_ns=int(blocked.get(k, 0) * W / 8),
                        sampled_blocked=s, sampled_early_wake=e, sampled_release_ns=1000 * (s - e), sampled_signal_ns=sig,
                        sampled_wake_ns=1000 * (s - e))
    return dict(window_ns=W, worker_busy_ns=int(u_w * W),
                blocks={k: 8000 if blocked.get(k) else 0 for k in rules.BLOCKED_KEYS},
                sampled_blocks={k: 1000 if blocked.get(k) else 0 for k in rules.BLOCKED_KEYS},
                sampled_blocked_ns={k: int(blocked.get(k, 0) * W / 8) for k in rules.BLOCKED_KEYS},
                worker_busy_in_sampled_blocked_ns={k: int(overlap.get(k, 0) * W / 8) for k in rules.BLOCKED_KEYS},
                late_ns=late_ns, late_count=paced_stops, stops={'paced': paced_stops, 'credit': 8000},
                state_sampled_ns={'paced': 0, 'credit': W // 16}, state_samples={'paced': 0, 'credit': 1000},
                emit_sampled_ns=W // 16, emit_samples=1000,
                opportunities=1000, entries=1000, entry_bytes=20_000_000, entry_packets=14_000, max_pending_q=max_q,
                admissions_over_2q=over2q, refusals=10, waits=waits, sampled_groups=groups, sampled_submit_ns=submit_ns * groups)


class Measures(unittest.TestCase):
    def test_joint_occupancy(self):
        m = rules.hand_measures(hand(u_w=0.8, blocked={'credit': 0.4, 'cwnd': 0.05}, overlap={'credit': 0.35, 'cwnd': 0.05}))
        self.assertAlmostEqual(m['local'], 0.4)
        self.assertAlmostEqual(m['u_c'], 0.55)
        self.assertAlmostEqual(m['j_wb'], 0.35)
        self.assertAlmostEqual(m['j_hh'], 0.05)
        self.assertAlmostEqual(m['j_ov'], 0.40)
        self.assertAlmostEqual(m['j_lb'], 0.15)

    def test_usable(self):
        self.assertTrue(rules.hand_usable(hand(blocked={'credit': 0.4}, overlap={'credit': 0.3}), 20000)[0])
        self.assertFalse(rules.hand_usable(None, 20000)[0])
        h = hand()
        h['window_ns'] = int(W * 0.9)
        self.assertFalse(rules.hand_usable(h, 20000)[0])
        self.assertFalse(rules.hand_usable(hand(blocked={'credit': 0.4}, overlap={'credit': 0.5}), 20000)[0])
        self.assertFalse(rules.hand_usable(hand(u_w=1.2), 20000)[0])
        h = hand(blocked={'credit': 0.4}, overlap={'credit': 0.3})
        h['sampled_blocks']['credit'] = 50
        self.assertFalse(rules.hand_usable(h, 20000)[0], 'too few sampled intervals for the estimate')


class Verdicts(unittest.TestCase):
    def v(self, **kw):
        return rules.verdict(rules.hand_measures(hand(**kw)))

    def test_handoff(self):
        # #715's loopback-bottleneck pattern: neither stage saturated, the connection waits on local credit.
        self.assertEqual(self.v(u_w=0.8, blocked={'credit': 0.44}, overlap={'credit': 0.35}), 'hand-off')
        self.assertEqual(rules.handoff_kind(rules.hand_measures(hand(blocked={'queue': 0.4, 'credit': 0.01}))), 'queue')

    def test_worker_bound(self):
        self.assertEqual(self.v(u_w=0.97, blocked={'credit': 0.6}, overlap={'credit': 0.58}), 'worker-bound')

    def test_loop_bound(self):
        self.assertEqual(self.v(u_w=0.5, blocked={'credit': 0.02, 'app': 0.03}), 'loop-bound')

    def test_window_bound(self):
        self.assertEqual(self.v(u_w=0.4, blocked={'cwnd': 0.45, 'credit': 0.05}), 'window-bound')
        self.assertEqual(self.v(u_w=0.4, blocked={'paced': 0.30}), 'window-bound')

    def test_mixed(self):
        self.assertEqual(self.v(u_w=0.6, blocked={'cwnd': 0.30, 'credit': 0.30}), 'mixed: hand-off + window-bound')
        self.assertEqual(self.v(u_w=0.95, blocked={'cwnd': 0.30, 'credit': 0.40}), 'mixed: window-bound + worker-bound')

    def test_supply_bound(self):
        self.assertEqual(self.v(u_w=0.5, blocked={'app': 0.35, 'queue': 0.1}), 'supply-bound')
        self.assertEqual(self.v(u_w=0.6, blocked={'app': 0.3, 'queue': 0.3}), 'mixed: hand-off + supply-bound')

    def test_inconclusive(self):
        # Local waits short of a quarter, neither stage saturated, no window waits.
        self.assertEqual(self.v(u_w=0.7, blocked={'credit': 0.15, 'app': 0.1}), 'inconclusive')


class Synthetic(unittest.TestCase):
    P = {'slowworker': dict(synSlowWorkerNS=20_000), 'busyloop': dict(synBusyLoopNS=20_000),
         'delaysignal': dict(synSignalDelayNS=50_000), 'cwnd': dict(cwnd=12_000),
         'mixed': dict(synSignalDelayNS=100_000, cwnd=8_000, synAlternate=True)}

    def r(self, case, **kw):
        return rules.recovery(case, rules.hand_measures(hand(**kw)), self.P[case])

    def test_slow_worker(self):
        self.assertTrue(self.r('slowworker', u_w=0.98, blocked={'queue': 0.6}, overlap={'queue': 0.6}, submit_ns=22_000, kind='queue')['recovered'])
        self.assertFalse(self.r('slowworker', u_w=0.98, blocked={'queue': 0.6}, overlap={'queue': 0.6}, submit_ns=10_000, kind='queue')['recovered'],
                         'the injected submission time must be visible')
        self.assertFalse(self.r('slowworker', u_w=0.8, blocked={'queue': 0.4}, submit_ns=22_000, kind='queue')['recovered'])

    def test_busy_loop(self):
        self.assertTrue(self.r('busyloop', u_w=0.4, blocked={'app': 0.02})['recovered'])
        self.assertFalse(self.r('busyloop', u_w=0.4, blocked={'credit': 0.3})['recovered'])

    def test_delayed_signal(self):
        # Local wait 0.6 with the worker busy for 0.1 of it: j_hh = 0.5.
        self.assertTrue(self.r('delaysignal', u_w=0.3, blocked={'queue': 0.6}, overlap={'queue': 0.1}, kind='queue')['recovered'])
        # The same local wait with the worker busy throughout it: a working worker, not a withheld signal.
        self.assertFalse(self.r('delaysignal', u_w=0.8, blocked={'queue': 0.6}, overlap={'queue': 0.55}, kind='queue')['recovered'])
        self.assertFalse(self.r('delaysignal', u_w=0.3, blocked={'queue': 0.2}, kind='queue')['recovered'], 'no hand-off verdict')

    def test_cwnd(self):
        self.assertTrue(self.r('cwnd', u_w=0.3, blocked={'cwnd': 0.6})['recovered'])
        self.assertFalse(self.r('cwnd', u_w=0.3, blocked={'cwnd': 0.1, 'credit': 0.4})['recovered'])

    def test_mixed(self):
        self.assertTrue(self.r('mixed', u_w=0.4, blocked={'cwnd': 0.3, 'queue': 0.35}, kind='queue')['recovered'])
        self.assertFalse(self.r('mixed', u_w=0.4, blocked={'cwnd': 0.6, 'queue': 0.1}, kind='queue')['recovered'], 'one component only')
        self.assertFalse(self.r('mixed', u_w=0.4, blocked={'cwnd': 0.3, 'queue': 0.3, 'app': 0.3}, kind='queue')['recovered'], 'a third state')

    def test_perturbation(self):
        self.assertTrue(rules.perturbation([0.995, 0.99, 1.01])['passed'])
        self.assertFalse(rules.perturbation([0.98, 0.985, 1.0])['passed'])


class Stage1(unittest.TestCase):
    def test_majority(self):
        self.assertEqual(rules.majority(['hand-off'] * 3 + ['inconclusive']), 'hand-off')
        self.assertEqual(rules.majority(['hand-off', 'hand-off', 'worker-bound', 'inconclusive']), 'inconclusive')
        self.assertEqual(rules.majority(['hand-off', 'hand-off']), 'evidence gap')
        self.assertEqual(rules.majority(['mixed: hand-off + window-bound'] * 4), 'mixed: hand-off + window-bound')

    def blocks(self, ratios, aa=(0.99, 1.0, 1.01, 1.005), engaged=True):
        return [dict(ratio=r, aa=a, cpu_ratio=1.0, cpu_aa=1.0, engaged=engaged) for r, a in zip(ratios, aa)]

    def test_credit_responsive(self):
        r = rules.credit_response(self.blocks([1.06, 1.05, 1.07, 1.0]), True, True)
        self.assertEqual(r['reading'], 'credit-responsive')

    def test_not_credit_responsive(self):
        self.assertEqual(rules.credit_response(self.blocks([1.0, 1.003, 0.998, 1.02]), True, True)['reading'], 'not credit-responsive')
        r = rules.credit_response(self.blocks([0.95, 0.96, 0.94, 0.97]), True, True)
        self.assertEqual((r['reading'], r['direction']), ('not credit-responsive', 'below the A/A range'))

    def test_inert_and_unusable(self):
        self.assertEqual(rules.credit_response(self.blocks([1.1] * 4), True, False)['reading'], 'inert')
        self.assertEqual(rules.credit_response(self.blocks([1.1] * 4, engaged=False), True, True)['reading'], 'inert')
        self.assertEqual(rules.credit_response(self.blocks([1.1] * 4), False, True)['reading'], 'unusable')

    def kb(self, ratios, exposure):
        return [dict(ratio=r, aa=a, exposure=exposure, paced_stops=100) for r, a in zip(ratios, (0.995, 1.0, 1.004, 0.998))]

    def test_kick(self):
        self.assertEqual(rules.kick_reading(self.kb([1.03, 1.02, 1.025, 1.0], 0.02))['reading'], 'kick')
        self.assertEqual(rules.kick_reading(self.kb([1.03, 1.02, 1.025, 1.0], 0.0))['reading'], 'r8 lower, kick not supported')
        self.assertEqual(rules.kick_reading(self.kb([1.0, 1.002, 0.999, 1.001], 0.02))['reading'], 'no r8 effect detected')
        self.assertEqual(rules.kick_reading(self.kb([1.03, 1.02], 0.02)[:2])['reading'], 'evidence gap')

    def test_addressable(self):
        self.assertTrue(rules.addressable('hand-off'))
        self.assertTrue(rules.addressable('mixed: hand-off + window-bound'))
        self.assertFalse(rules.addressable('worker-bound'))
        self.assertFalse(rules.addressable('inconclusive'))


class Stage2(unittest.TestCase):
    AA_D = (-0.004, 0.002, 0.0, -0.001, 0.003, 0.001)   # BBR A/A deficit differences
    AA_C = (0.99, 1.01, 1.0, 1.005, 0.995, 1.002)      # BBR A/A CPU ratios

    def wl(self, deficit, cpu, n=6):
        return rules.s2_workload([dict(deficit_diff_new=d, deficit_diff_aa=a, cpu_ratio_new=c, cpu_ratio_aa=ca)
                                  for d, a, c, ca in list(zip(deficit, self.AA_D, cpu, self.AA_C))[:n]])

    def screen(self, g=1.0, c=1.0, n=4):
        return rules.s5_screen([dict(goodput_ratio_new=g, goodput_ratio_aa=a, cpu_ratio_new=c, cpu_ratio_aa=b)
                                for a, b in list(zip((0.995, 1.0, 1.004, 0.998), (1.0, 0.99, 1.01, 1.0)))[:n]])

    def outcome(self, stream, datagram, targeted, integrity=True, pres=None, screens=None):
        return rules.s2_outcome(dict(stream=stream, datagram=datagram), targeted, integrity,
                                pres or dict(sender='inside', receiver='inside'), screens or dict(stream=self.screen(), datagram=self.screen()))

    IMPROVE = (-0.05,) * 6
    SAME = (0.0,) * 6
    CPU = (1.0,) * 6

    def test_keep_both(self):
        self.assertEqual(self.outcome(self.wl(self.IMPROVE, self.CPU), self.wl(self.IMPROVE, self.CPU), ['stream', 'datagram'])[0], 'keep')

    def test_datagram_only_improvement(self):
        # DATAGRAM improves, STREAM unchanged: kept when only DATAGRAM was targeted, inconclusive (partial) when both were.
        s, d = self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU)
        self.assertEqual(self.outcome(s, d, ['datagram'])[0], 'keep')
        self.assertEqual(self.outcome(s, d, ['stream', 'datagram'])[0], 'inconclusive')
        self.assertTrue(rules.efficacy(d) == 'improved' and rules.efficacy(s) == 'unchanged')

    def test_non_target_regression(self):
        self.assertEqual(self.outcome(self.wl((0.03,) * 6, self.CPU), self.wl(self.IMPROVE, self.CPU), ['datagram'])[0], 'negative')

    def test_partial_improvement(self):
        # Some blocks fall, but the median stays inside the A/A range: nothing moved beyond it (null, as D4 defines it).
        partial = self.wl((-0.003, -0.05, -0.002, -0.06, -0.001, 0.0), self.CPU)
        self.assertEqual(rules.efficacy(partial), 'unchanged')
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), partial, ['datagram'])[0], 'null')
        # Improvement beyond the range in one targeted workload, not the other: inconclusive, not negative.
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU), ['stream', 'datagram'])[0],
                         'inconclusive')

    def test_cpu_regression(self):
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, (1.03,) * 6), ['datagram'])[0], 'negative')

    def test_insufficient_blocks(self):
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU, n=4), ['datagram'])[0], 'inconclusive')
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU), ['datagram'],
                                      screens=dict(stream=self.screen(), datagram=self.screen(n=2)))[0], 'inconclusive')

    def test_reno_preservation_failure(self):
        self.assertEqual(rules.preservation_cell([1.03, 1.02, 1.04], [0.99, 1.01]), 'above')
        for p in ['above', 'below']:
            self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU), ['datagram'],
                                          pres=dict(sender=p, receiver='inside'))[0], 'negative')

    def test_s5_regression_and_integrity(self):
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU), ['datagram'],
                                      screens=dict(stream=self.screen(g=0.97), datagram=self.screen()))[0], 'negative')
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU), ['datagram'],
                                      screens=dict(stream=self.screen(c=1.05), datagram=self.screen()))[0], 'negative')
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.IMPROVE, self.CPU), ['datagram'], integrity=False)[0],
                         'negative')

    def test_null(self):
        self.assertEqual(self.outcome(self.wl(self.SAME, self.CPU), self.wl(self.SAME, self.CPU), ['datagram'])[0], 'null')


class Reruns(unittest.TestCase):
    def test_choose_block(self):
        self.assertEqual(rules.choose_block([('a', True, False)]), ('a', 'clean'))
        self.assertEqual(rules.choose_block([('a', True, True), ('b', True, False)]), ('b', 'clean'))
        self.assertEqual(rules.choose_block([('a', True, True), ('b', True, True)]), ('a', 'contaminated'))
        self.assertEqual(rules.choose_block([('a', False, False), ('b', False, False)]), ('a', 'unusable'))


if __name__ == '__main__':
    unittest.main()
