#!/usr/bin/env python3
"""Synthetic cases for rules.py, written and passing before any counted observation existed."""
import unittest

import rules as R

D = R.DELTA_NS


def ana(shares=None, late_events=(), count=1000, sum_ns=3e9, ambiguous=0.0, align=0.0, covers=True, error=None):
    sh = shares or {}
    return dict(error=error, trace=dict(covers_window=covers), alignment=dict(share=align),
                late=dict(count=count, sum_ns=sum_ns, shares=sh, segments_ns=dict(ambiguous=ambiguous * sum_ns)),
                late_events=list(late_events))


def seg(**kw):
    s = dict(delivery_timer=0, delivery_other=0, unarmed=0, scheduling=0, loop_running=0, loop_blocked=0, ambiguous=0)
    s.update(kw)
    return s


def events(n, **segkw):
    truth, found = [], []
    for i in range(n):
        d = 1_000_000 * (i + 10)
        truth.append(dict(D=d, O=d + 400_000, Arms=1, Folded=0, Superseded=0, Delta=D, Sent=d + D))
        found.append(dict(d_mono=d, o_mono=d + 400_000, late_ns=400_000, arms=1, folded=0, superseded=0,
                          segs=seg(**segkw), ended_by='packet' if 'delivery_other' in segkw else 'timer'))
    return truth, found


class Stage0(unittest.TestCase):
    def check(self, case, shares, ok, **segkw):
        truth, found = events(200, **segkw)
        r = R.recovery(case, ana(shares, found), dict(late=truth), 0, 10**12)
        self.assertEqual(r['recovered'], ok, r)

    def test_delivery(self):
        self.check('delivery', dict(delivery=0.98, scheduling=0.01, loop=0.01), True, delivery_timer=D + 50_000)
        self.check('delivery', dict(delivery=0.98), False, delivery_timer=D - 50_000)  # delay not shown
        self.check('delivery', dict(delivery=0.4, scheduling=0.6), False, delivery_timer=D + 1)  # wrong verdict

    def test_scheduling(self):
        self.check('scheduling', dict(scheduling=0.8, delivery=0.2), True, scheduling=D)
        self.check('scheduling', dict(scheduling=0.8, delivery=0.2), False, scheduling=0.5 * D)

    def test_handler(self):
        self.check('handler', dict(loop=0.8, delivery=0.2), True, loop_running=D)
        self.check('handler', dict(loop=0.4, delivery=0.6), False, loop_running=D)

    def test_nontimer(self):
        self.check('nontimer', dict(delivery=0.99), True, delivery_other=D)
        truth, found = events(200, delivery_other=D)
        for f in found:
            f['ended_by'] = 'timer'
        self.assertFalse(R.recovery('nontimer', ana(dict(delivery=0.99), found), dict(late=truth), 0, 10**12)['recovered'])

    def test_reset(self):
        truth, found = events(300, delivery_timer=1000)
        for i, (t, f) in enumerate(zip(truth, found)):
            k = i % 3
            t['Arms'] = f['arms'] = 2 if k != 1 else 2
            t['Folded'] = f['folded'] = 1 if k == 1 else 0
            t['Superseded'] = f['superseded'] = 1 if k == 2 else 0
        a = ana(dict(delivery=0.99), found)
        self.assertTrue(R.recovery('reset', a, dict(late=truth), 0, 10**12)['recovered'])
        for f in found[:10]:
            f['arms'] = 1  # bookkeeping lost on 10 of 299 matched events (>1%)
        self.assertFalse(R.recovery('reset', a, dict(late=truth), 0, 10**12)['recovered'])

    def test_coverage_and_usability(self):
        truth, found = events(200, delivery_timer=D + 1)
        r = R.recovery('delivery', ana(dict(delivery=0.99), found[:150]), dict(late=truth), 0, 10**12)
        self.assertFalse(r['recovered'])
        self.assertFalse(R.recovery('delivery', ana(dict(delivery=0.99), found, align=0.01), dict(late=truth), 0, 10**12)['recovered'])
        self.assertFalse(R.recovery('delivery', ana(dict(delivery=0.99), found, ambiguous=0.05), dict(late=truth), 0, 10**12)['recovered'])
        self.assertFalse(R.recovery('delivery', ana(dict(delivery=0.99), found, covers=False), dict(late=truth), 0, 10**12)['recovered'])

    def test_perturbation(self):
        self.assertTrue(R.perturbation([(99.5, 100), (100, 100), (98, 100)])['passed'])
        self.assertFalse(R.perturbation([(98.5, 100), (98.9, 100), (100, 100)])['passed'])
        self.assertFalse(R.perturbation([])['passed'])


class Stage1(unittest.TestCase):
    def test_shares(self):
        self.assertEqual(R.shares_verdict(dict(delivery=0.9, scheduling=0.05, loop=0.05)), 'delivery')
        self.assertEqual(R.shares_verdict(dict(delivery=0.2, scheduling=0.7, loop=0.1)), 'scheduling')
        self.assertEqual(R.shares_verdict(dict(delivery=0.1, scheduling=0.1, loop=0.8)), 'loop')
        self.assertEqual(R.shares_verdict(dict(delivery=0.55, loop=0.3, scheduling=0.15)), 'mixed')
        self.assertEqual(R.shares_verdict(dict(delivery=0.45, loop=0.2, scheduling=0.2, unarmed=0.15)), 'inconclusive')
        self.assertEqual(R.shares_verdict(None), 'no lateness')

    def test_observation(self):
        a = ana(dict(delivery=0.9), count=25000, sum_ns=3e9)
        self.assertEqual(R.s1_observation(a, (25000, 3e9))[0], 'delivery')
        self.assertEqual(R.s1_observation(a, (25000, 2.8e9))[0], 'unusable')  # disagrees with the in-process count
        self.assertEqual(R.s1_observation(ana(dict(delivery=0.9), count=10, sum_ns=1e6), (10, 1e6))[0], 'no lateness')

    def test_workloads(self):
        self.assertEqual(R.workload_verdict(['delivery'] * 3 + ['mixed']), 'delivery')
        self.assertEqual(R.workload_verdict(['delivery', 'delivery', 'mixed', 'loop']), 'inconclusive')
        self.assertEqual(R.workload_verdict(['delivery', 'unusable', 'unusable', 'delivery']), 'evidence gap')
        self.assertTrue(R.needs_escalation(['delivery', 'delivery', 'mixed', 'loop']))
        self.assertFalse(R.needs_escalation(['delivery'] * 3 + ['loop']))
        self.assertEqual(R.stage1_verdict(dict(stream='delivery', datagram='delivery')), 'timer delivery')
        self.assertEqual(R.stage1_verdict(dict(stream='scheduling', datagram='scheduling')), 'goroutine scheduling')
        self.assertEqual(R.stage1_verdict(dict(stream='delivery', datagram='mixed')), 'inconclusive')
        self.assertEqual(R.stage1_verdict(dict(stream='mixed', datagram='mixed')), 'inconclusive')
        self.assertEqual(R.stage1_verdict(dict(stream='delivery', datagram='evidence gap')), 'evidence gap')


class Loopback(unittest.TestCase):
    W = 20e9
    CLS = ['immediate', 'app', 'paced', 'cwnd', 'credit', 'hard', 'other']

    def agg(self, paced=0, credit=0, immediate=0, emit=0, late_ns=0, late_bytes=0):
        st = [immediate * self.W, 0, paced * self.W, 0, credit * self.W, 0, 0]
        return dict(state_classes=self.CLS, state_ns=st, emit_ns=emit * self.W, late_ns=late_ns, late_bytes=late_bytes,
                    late_count=1, opportunities=1)

    def reno_bytes(self, frac):
        return frac * R.LB_RENO_MBPS * 1e6 / 8 * self.W / 1e9

    def test_paced_waits(self):  # paced, late, and the discarded credit accounts for the deficit
        o = R.lb_observation(self.agg(paced=0.5, late_ns=0.15 * self.W, late_bytes=self.reno_bytes(0.10)), self.W)
        self.assertEqual(o['verdict'], 'timing-limited goodput')

    def test_credit_waits(self):
        self.assertEqual(R.lb_observation(self.agg(credit=0.45, emit=0.4, immediate=0.1), self.W)['verdict'], 'no timing exposure')

    def test_processing_delay(self):
        self.assertEqual(R.lb_observation(self.agg(emit=0.6, immediate=0.35), self.W)['verdict'], 'no timing exposure')

    def test_exposure_only(self):  # visibly late, but the lost credit is far below the deficit
        o = R.lb_observation(self.agg(paced=0.2, late_ns=0.05 * self.W, late_bytes=self.reno_bytes(0.01)), self.W)
        self.assertEqual(o['verdict'], 'timing exposure')

    def test_mixed(self):  # loss between a quarter and half of the deficit
        o = R.lb_observation(self.agg(paced=0.2, credit=0.3, late_ns=0.05 * self.W, late_bytes=self.reno_bytes(0.05)), self.W)
        self.assertEqual(o['verdict'], 'inconclusive')


class Stage2(unittest.TestCase):
    def block(self, dd=-0.05, lr=0.3, wr=1.0, cr=0.98, aa=(0.0, 1.0, 1.0, 1.0), classes=None):
        return dict(deficit_diff_new=dd, deficit_diff_aa=aa[0], late_ratio_new=lr, late_ratio_aa=aa[1],
                    wakes_ratio_new=wr, wakes_ratio_aa=aa[2], cpu_ratio_new=cr, cpu_ratio_aa=aa[3],
                    wake_classes_new=classes or dict(timer=900, packet=100), wake_classes_base=dict(timer=900, packet=100))

    def blocks(self, n=6, **kw):
        aas = [(-0.004, 0.97, 0.98, 0.99), (0.003, 1.02, 1.01, 1.01), (0.0, 1.0, 1.0, 1.0),
               (0.001, 0.99, 1.0, 1.0), (-0.002, 1.01, 1.02, 0.995), (0.002, 1.0, 0.99, 1.005)]
        return [self.block(aa=aas[i], **kw) for i in range(n)]

    def outcome(self, s, d, integrity=True, pres=None):
        return R.s2_outcome(dict(stream=R.s2_workload(s), datagram=R.s2_workload(d)), integrity,
                            pres or {'send': 'inside', 'receive': 'inside'})[0]

    def test_improvement_both(self):
        self.assertEqual(self.outcome(self.blocks(), self.blocks()), 'keep')

    def test_one_workload_regression(self):
        self.assertEqual(self.outcome(self.blocks(), self.blocks(dd=0.03, lr=1.4)), 'negative')
        self.assertEqual(self.outcome(self.blocks(), self.blocks(cr=1.05)), 'negative')  # CPU above the A/A maximum
        self.assertEqual(self.outcome(self.blocks(), self.blocks(wr=1.2)), 'negative')   # wakes above the A/A maximum

    def test_insufficient(self):
        self.assertEqual(self.outcome(self.blocks(4), self.blocks()), 'inconclusive')

    def test_zero_lateness(self):  # predecessor lateness zero in two blocks: lateness ratio unusable there
        s = self.blocks()
        for b in s[:2]:
            b['late_ratio_new'] = None
        self.assertEqual(self.outcome(s, self.blocks()), 'inconclusive')
        s = self.blocks()
        s[0]['late_ratio_new'] = None  # one block only: five usable remain
        self.assertEqual(self.outcome(s, self.blocks()), 'keep')

    def test_changed_wake_mix(self):
        self.assertEqual(self.outcome(self.blocks(classes=dict(other=500, timer=500)), self.blocks()), 'inconclusive')
        w = R.s2_workload(self.blocks(classes=dict(wakepath=900, packet=100)), registered_classes=('wakepath',))
        self.assertEqual(R.s2_outcome(dict(stream=w, datagram=R.s2_workload(self.blocks())), True,
                                      {'send': 'inside', 'receive': 'inside'})[0], 'keep')

    def test_preservation_failure(self):
        self.assertEqual(self.outcome(self.blocks(), self.blocks(), pres={'send': 'above', 'receive': 'inside'}), 'negative')
        self.assertEqual(self.outcome(self.blocks(), self.blocks(), pres={'send': 'below', 'receive': 'inside'}), 'inconclusive')

    def test_integrity(self):
        self.assertEqual(self.outcome(self.blocks(), self.blocks(), integrity=False), 'negative')

    def test_null(self):
        self.assertEqual(self.outcome(self.blocks(dd=0.0, lr=1.0, wr=1.0, cr=1.0), self.blocks(dd=0.0, lr=1.0, wr=1.0, cr=1.0)), 'null')

    def test_partial(self):  # deficit falls but lateness does not: neither keep nor null
        self.assertEqual(self.outcome(self.blocks(lr=1.0), self.blocks(lr=1.0)), 'inconclusive')


class Rerun(unittest.TestCase):
    def test_choose(self):
        self.assertEqual(R.choose_block([('original', True, False)]), ('original', 'clean'))
        self.assertEqual(R.choose_block([('original', True, True), ('rerun', True, False)]), ('rerun', 'clean'))
        self.assertEqual(R.choose_block([('original', True, True), ('rerun', True, True)]), ('original', 'contaminated'))
        self.assertEqual(R.choose_block([('original', False, False), ('rerun', False, False)]), ('original', 'unusable'))


if __name__ == '__main__':
    unittest.main()
