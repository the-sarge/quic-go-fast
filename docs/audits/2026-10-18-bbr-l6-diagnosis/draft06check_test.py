#!/usr/bin/env python3
"""Tests for draft06check.py: the port's own fixture traces pass every rule,
and each injected deviation is caught by its rule.

The fixtures (testdata/) are traces of the port itself, written by
trace/bbr_trace_test.go's TestTraceFixture and TestTraceFixtureLong in the
exported `cand-trace` tree.
"""
import copy
import json
import pathlib
import sys
import unittest

sys.dont_write_bytecode = True
HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import draft06check as d  # noqa: E402

FIXTURES = {name: json.loads((HERE / 'testdata' / f'{name}.json').read_text()) for name in ('port-fixture-trace', 'port-fixture-trace-long')}


def fixture(name='port-fixture-trace'):
    return copy.deepcopy(FIXTURES[name])


class Tr:
    """Row helpers over one mutable trace."""

    def __init__(self, data):
        self.data, self.c = data, {n: i for i, n in enumerate(data['columns'])}
        self.f = {n: 1 << i for i, n in enumerate(data['flags'])}

    def rows(self, kind=None):
        return [r for r in self.data['rows'] if kind is None or r[0] == kind]

    def get(self, r, name):
        return r[self.c[name]]

    def set(self, r, name, value):
        r[self.c[name]] = value

    def pairs(self):
        rows = self.data['rows']
        return [(rows[i], rows[i + 1]) for i in range(len(rows) - 1)
                if rows[i][0] == d.PRE and rows[i + 1][0] == d.POST and rows[i][self.c['seq']] == rows[i + 1][self.c['seq']]]

    def transition(self, a, b, n=0):
        found = [(p, q) for p, q in self.pairs() if self.get(p, 'phase') == a and self.get(q, 'phase') == b]
        return found[n]

    def seq_rows(self, seq):
        return [r for r in self.data['rows'] if r[self.c['seq']] == seq]


def mismatches(data, rule):
    return d.run(data)['rules'][rule]['mismatches']


class FixturesPass(unittest.TestCase):
    def test_port_fixtures_have_no_mismatch(self):
        for name in FIXTURES:
            with self.subTest(name):
                result = d.run(fixture(name))
                self.assertEqual(result['mismatches'], 0, {k: v['mismatches'][:2] for k, v in result['rules'].items() if v['mismatches']})
                self.assertTrue(result['coverage']['complete'])

    def test_fixtures_exercise_the_rules(self):
        notes = {}
        for name in FIXTURES:
            for rule, x in d.run(fixture(name))['rules'].items():
                self.assertGreater(x['checked'], 0, (name, rule))
                for k, v in x['notes'].items():
                    notes[k] = notes.get(k, 0) + v
        for key in ('refill_by_time', 'refill_by_rounds', 'up_exit_loss', 'up_exit_plateau', 'up_exit_probertt', 'crossings',
                    'binding_bw', 'aging_events', 'stale_until_next_sample'):
            self.assertGreater(notes.get(key, 0), 0, key)


class InjectedDeviations(unittest.TestCase):
    def test_c0_startup_exit_without_plateau(self):
        t = Tr(fixture())
        _, q = t.transition(d.STARTUP, d.DRAIN)
        t.set(q, 'plateau', 0)
        self.assertTrue(mismatches(t.data, 'C0'))

    def test_c1_early_refill(self):
        t = Tr(fixture())
        p, _ = t.transition(d.CRUISE, d.REFILL)
        t.set(p, 'rounds_since_probe', 0)
        t.set(p, 'cycle_stamp_mono', t.get(p, 'mono_ns'))
        self.assertTrue([m for m in mismatches(t.data, 'C1') if 'before' in m['what']])

    def test_c1_missed_round_trigger(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.ROUND) if t.get(r, 'phase') == d.CRUISE and len(t.seq_rows(t.get(r, 'seq'))) == 1)
        t.set(r, 'rounds_since_probe', 63)
        self.assertTrue([m for m in mismatches(t.data, 'C1') if 'Reno-coexistence' in m['what']])

    def test_c1_missed_time_trigger(self):
        t = Tr(fixture('port-fixture-trace-long'))
        r = next(r for r in t.rows(d.ROUND) if t.get(r, 'phase') == d.CRUISE and len(t.seq_rows(t.get(r, 'seq'))) == 1)
        t.set(r, 'cycle_stamp_mono', t.get(r, 'mono_ns') - t.get(r, 'probe_wait_ns') - 1)
        self.assertTrue([m for m in mismatches(t.data, 'C1') if 'wall-clock' in m['what']])

    def test_c2_refill_keeps_short_term_model(self):
        t = Tr(fixture())
        _, q = t.transition(d.CRUISE, d.REFILL)
        t.set(q, 'bandwidth_short', 1000)
        self.assertTrue([m for m in mismatches(t.data, 'C2') if 'reset' in m['what']])

    def test_c2_refill_longer_than_a_round(self):
        t = Tr(fixture())
        _, q = t.transition(d.REFILL, d.UP)
        t.set(q, 'round', t.get(q, 'round') + 1)
        self.assertTrue([m for m in mismatches(t.data, 'C2') if 'one round' in m['what']])

    def test_c3_up_exit_without_condition(self):
        t = Tr(fixture())
        exits = [(p, q) for p, q in t.pairs() if t.get(p, 'phase') == d.UP and t.get(q, 'phase') == d.DOWN
                 and t.get(q, 'plateau') >= 3 and not [r for r in t.seq_rows(t.get(p, 'seq')) if r[0] == d.PROBELOSS]]
        _, q = exits[0]
        t.set(q, 'plateau', 1)
        self.assertTrue(mismatches(t.data, 'C3'))

    def _crossing(self, t, n=0):
        """The n-th crossing's PROBELOSS row, its PROBESET row and its event's last POST row."""
        found = 0
        rows = t.data['rows']
        for i, r in enumerate(rows):
            if r[0] == d.PROBELOSS and d.crosses(d.Trace(t.data), r):
                if found == n:
                    post = [x for x in t.seq_rows(t.get(r, 'seq')) if x[0] == d.POST][-1]
                    return r, rows[i + 1], post
                found += 1
        raise AssertionError('no crossing')

    def test_c4_assignment_too_high(self):
        t = Tr(fixture())
        _, s, post = self._crossing(t)
        t.set(s, 'inflight_long', 2 * t.get(s, 'inflight_long'))
        t.set(s, 'flags', t.get(s, 'flags') | t.f['has_ack'])
        t.set(post, 'flags', t.get(post, 'flags') | t.f['has_ack'])
        self.assertTrue([m for m in mismatches(t.data, 'C4') if "draft-06's assignment" in m['what']])

    def test_c4_bound_never_lowered(self):
        t = Tr(fixture())
        r, s, post = self._crossing(t)
        t.set(s, 'inflight_long', t.get(r, 'inflight_long'))  # stale: the crossing left the prior bound
        t.set(s, 'flags', t.get(s, 'flags') | t.f['has_ack'])
        self.assertTrue([m for m in mismatches(t.data, 'C4') if "draft-06's assignment" in m['what']])

    def test_c4_later_growth_in_the_event_is_not_the_assignment(self):
        t = Tr(fixture())
        _, s, post = self._crossing(t)
        t.set(post, 'inflight_long', t.get(s, 'inflight_long') + 5000)  # a later same-event raise
        t.set(post, 'flags', t.get(post, 'flags') | t.f['has_ack'])
        self.assertFalse([m for m in mismatches(t.data, 'C4') if 'assignment' in m['what']])

    def test_c4_lowered_after_assignment(self):
        t = Tr(fixture())
        _, s, post = self._crossing(t)
        t.set(post, 'inflight_long', t.get(s, 'inflight_long') - 1200)
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'after its crossing' in m['what']])

    def test_c4_reset_to_unbounded_after_assignment(self):
        t = Tr(fixture())
        _, s, post = self._crossing(t)
        t.set(post, 'inflight_long', d.INF)  # an unexplained reset after the assignment
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'after its crossing' in m['what']])
        t.set(post, 'flags', t.get(post, 'flags') | t.f['undo_eligible'])  # an undo restoring an unbounded saved bound
        self.assertFalse([m for m in mismatches(t.data, 'C4') if 'after its crossing' in m['what']])

    def test_c4_undo_to_unbounded_is_explained(self):
        t = Tr(fixture())
        p, q = next((p, q) for p, q in t.pairs() if t.get(p, 'inflight_long') != d.INF
                    and not [r for r in t.seq_rows(t.get(p, 'seq')) if r[0] == d.PROBELOSS])
        t.set(q, 'inflight_long', d.INF)
        t.set(q, 'a0', t.get(q, 'a0') | 1 << 4)
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'without an unlimited crossing' in m['what']])
        t.set(q, 'flags', t.get(q, 'flags') | t.f['undo_eligible'])
        self.assertFalse([m for m in mismatches(t.data, 'C4') if 'without an unlimited crossing' in m['what']])

    def test_c4_crossing_without_assignment_row(self):
        t = Tr(fixture())
        _, s, _ = self._crossing(t)
        t.data['rows'].remove(s)
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'assignment row' in m['what']])

    def test_c4_crossing_without_too_high(self):
        t = Tr(fixture())
        _, s, _ = self._crossing(t)
        t.set(s, 'flags', t.get(s, 'flags') & ~t.f['previous_probe_too_high'])
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'prev_probe_too_high' in m['what']])

    def test_c4_crossing_in_up_stays_in_up(self):
        t = Tr(fixture())
        r, _, post = self._crossing(t)
        self.assertEqual(t.get(r, 'phase'), d.UP)
        t.set(post, 'phase', d.UP)
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'DOWN' in m['what']])

    def _set_crossing(self, t, r, s, lost, flight, length, window, bdp1):
        """Give a crossing chosen inputs and the port's resulting assignment."""
        for name, v in (('a0', lost), ('a1', flight), ('a2', length), ('a3', flight), ('window', window)):
            t.set(r, name, v)
        # bdp() is recomputed from bandwidth and min_rtt: choose bandwidth so BDP = bdp1 at min_rtt.
        t.set(r, 'bandwidth_short', d.INF)
        t.set(r, 'bandwidth', bdp1 * 1_000_000_000 // t.get(r, 'min_rtt_ns'))
        target = min(d.bdp(d.Trace(t.data), r, 1, 1), window)
        port = max(d.inflight_at_loss(lost, flight, length, True), target * 7 // 10)
        for x in t.seq_rows(t.get(r, 'seq')):  # the assignment and the event's end state agree
            if x[0] in (d.PROBESET, d.POST):
                t.set(x, 'inflight_long', port)
        return port, max(d.inflight_at_loss(lost, flight, length, False), 0.7 * target)

    def test_c4_clamp_is_a_deviation_not_a_pass(self):
        t = Tr(fixture())
        r, s, _ = self._crossing(t)
        port, draft = self._set_crossing(t, r, s, lost=1700, flight=10000, length=1200, window=12000, bdp1=10000)
        self.assertGreater(port - draft, 300)  # 8800 against 8469.4: the 0.7 floor (7000) does not hide it
        result = d.run(t.data)['rules']['C4']
        self.assertFalse([m for m in result['mismatches'] if 'assignment' in m['what']])
        self.assertEqual(len(result['deviations']), 1)
        self.assertAlmostEqual(result['deviations'][0]['excess_bytes'], port - draft)

    def test_c4_floor_removes_the_clamp(self):
        t = Tr(fixture())
        r, s, _ = self._crossing(t)
        port, draft = self._set_crossing(t, r, s, lost=1700, flight=10000, length=1200, window=14000, bdp1=14000)
        self.assertEqual(port, 9800)  # 0.7 x 14000 exceeds both prefixes
        self.assertLess(abs(port - draft), 1)
        self.assertFalse(d.run(t.data)['rules']['C4'].get('deviations'))

    def test_c4_integer_rounding_is_not_a_deviation(self):
        t = Tr(fixture())
        r, s, _ = self._crossing(t)
        port, draft = self._set_crossing(t, r, s, lost=1200, flight=20000, length=1200, window=30000, bdp1=30000)
        self.assertLess(abs(port - draft), 1)
        result = d.run(t.data)['rules']['C4']
        self.assertFalse([m for m in result['mismatches'] if 'assignment' in m['what']])
        self.assertFalse(result.get('deviations'))

    def test_c4_limited_crossing_must_not_lower(self):
        t = Tr(fixture())
        r, s, _ = self._crossing(t)
        t.set(r, 'a4', 1)  # the packet was application-limited: the assignment row still shows a lower bound
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'supply-limited packet changed' in m['what']])

    def test_c4_limited_crossing_unchanged_passes(self):
        t = Tr(fixture())
        r, s, post = self._crossing(t)
        t.set(r, 'a4', 1)
        before = t.get(r, 'inflight_long')
        for x in t.seq_rows(t.get(r, 'seq')):
            if x[0] in (d.PROBESET, d.POST):
                t.set(x, 'inflight_long', before)
                t.set(x, 'a0', t.get(x, 'a0') & ~(1 << 3 | 1 << 4)) if x[0] == d.POST else None
        self.assertFalse(mismatches(t.data, 'C4'))

    def test_c4_counts_by_limitation_reason(self):
        t = Tr(fixture())
        checks = t.rows(d.PROBELOSS)
        for row, reason in zip(checks[-3:], (1, 2, 3)):
            t.set(row, 'a4', reason)
        notes = d.run(t.data)['rules']['C4']['notes']
        self.assertEqual((notes.get('checks_application'), notes.get('checks_flow_control'), notes.get('checks_probe_rtt')), (1, 1, 1))

    def test_c4_unexplained_decrease(self):
        t = Tr(fixture())
        p, q = next((p, q) for p, q in t.pairs() if t.get(p, 'inflight_long') > 0
                    and not [r for r in t.seq_rows(t.get(p, 'seq')) if r[0] == d.PROBELOSS])
        t.set(q, 'inflight_long', t.get(p, 'inflight_long') - 1200)
        t.set(q, 'a0', t.get(q, 'a0') | 1 << 3)
        self.assertTrue([m for m in mismatches(t.data, 'C4') if 'without an unlimited crossing' in m['what']])

    def test_c4_draft_crossing_the_port_skips(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.PROBELOSS) if not d.crosses(d.Trace(t.data), r))
        t.set(r, 'a2', 1200)
        t.set(r, 'a0', 1100)   # below the packet length: the port's guard returns
        t.set(r, 'a1', 20000)  # 1100 > 2% of 20000
        self.assertTrue([m for m in mismatches(t.data, 'C4') if "draft's loss check" in m['what']])

    def test_c4_clamp_arithmetic(self):
        # The threshold was crossed before this packet: the draft's prefix is negative.
        port, draft = d.inflight_at_loss(1700, 10000, 1200, True), d.inflight_at_loss(1700, 10000, 1200, False)
        self.assertEqual(port, 8800)
        self.assertAlmostEqual(draft, 8800 + (176 - 500) / 0.98)
        # Below the threshold both agree up to the integer floor.
        self.assertAlmostEqual(d.inflight_at_loss(1200, 20000, 1200, True), d.inflight_at_loss(1200, 20000, 1200, False), delta=1)

    def test_c5_wrong_short_term_bound(self):
        t = Tr(fixture())
        q = next(q for q in t.rows(d.LOSSPOST) if t.get(q, 'phase') in (d.DOWN, d.CRUISE))
        t.set(q, 'bandwidth_short', t.get(q, 'bandwidth_short') + 1000)
        self.assertTrue([m for m in mismatches(t.data, 'C5') if '0.7' in m['what']])

    def test_c5_bound_change_without_loss_round(self):
        t = Tr(fixture())
        seq = next(t.get(p, 'seq') for p, q in t.pairs() if t.get(q, 'a0') & 2 and t.get(q, 'bandwidth_short') != d.INF
                   and [r for r in t.seq_rows(t.get(p, 'seq')) if r[0] == d.LOSSPRE])
        t.data['rows'] = [r for r in t.data['rows'] if not (r[t.c['seq']] == seq and r[0] in (d.LOSSPRE, d.LOSSPOST))]
        self.assertTrue([m for m in mismatches(t.data, 'C5') if 'without a loss-round close' in m['what']])

    def test_c6_window_above_bound(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.POST) if t.get(r, 'phase') in (d.DOWN, d.CRUISE) and t.get(r, 'flags') & t.f['has_ack'])
        t.set(r, 'window', 10 ** 9)
        self.assertTrue(mismatches(t.data, 'C6'))

    def test_c7_wrong_pacing_gain(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.POST) if t.get(r, 'phase') == d.UP and t.get(r, 'flags') & t.f['has_ack'])
        t.set(r, 'rate', t.get(r, 'rate') + 1)
        self.assertTrue(mismatches(t.data, 'C7'))

    def test_c8_max_bw_above_samples(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.ROUND) if t.get(r, 'phase') == d.CRUISE and len(t.seq_rows(t.get(r, 'seq'))) == 1)
        t.set(r, 'bandwidth', 10 * t.get(r, 'bandwidth'))
        self.assertTrue(mismatches(t.data, 'C8'))

    def test_c8_aging_below_a_sample(self):
        t = Tr(fixture())
        q = next(q for q in t.rows(d.POST) if t.get(q, 'a0') & (1 << 9))
        t.set(q, 'bandwidth', 1)
        self.assertTrue([m for m in mismatches(t.data, 'C8') if 'dropped below' in m['what']])

    def test_c9_short_hold(self):
        t = Tr(fixture())
        holds = [(p, q) for p, q in t.pairs() if t.get(q, 'phase') == d.PROBERTT and t.get(q, 'flags') & t.f['probe_rtt_holding']
                 and not t.get(p, 'flags') & t.f['probe_rtt_holding']]
        _, h = holds[0]
        _, exit_post = next((p, q) for p, q in t.pairs() if t.get(p, 'phase') == d.PROBERTT and t.get(q, 'phase') != d.PROBERTT
                            and t.get(q, 'mono_ns') > t.get(h, 'mono_ns'))
        t.set(exit_post, 'mono_ns', t.get(h, 'mono_ns') + 100_000_000)
        self.assertTrue([m for m in mismatches(t.data, 'C9') if '200 ms' in m['what']])

    def test_c6_probertt_cap(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.POST) if t.get(r, 'phase') == d.PROBERTT and t.get(r, 'flags') & t.f['has_ack']
                 and not t.get(r, 'flags') & (t.f['episode_exited'] | t.f['undo_eligible']))
        self.assertFalse(mismatches(t.data, 'C6'))
        cap = max(4 * t.get(r, 'size'), d.bdp(d.Trace(t.data), r, 1, 2))
        t.set(r, 'window', cap + t.get(r, 'size'))
        self.assertTrue(mismatches(t.data, 'C6'))

    def test_c6_restore_within_caps_passes_and_above_fails(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.POST) if t.get(r, 'phase') in (d.DOWN, d.CRUISE) and t.get(r, 'flags') & t.f['has_ack']
                 and t.get(r, 'inflight_long') != d.INF)
        t.set(r, 'flags', t.get(r, 'flags') | t.f['episode_exited'])
        cap = d.lo(t.get(r, 'inflight_short'), t.get(r, 'inflight_long'))
        if t.get(r, 'phase') == d.CRUISE:
            cap = d.lo(cap, d.headroom(d.Trace(t.data), r))
        self.assertNotEqual(cap, d.INF)
        t.set(r, 'window', cap)
        self.assertFalse(mismatches(t.data, 'C6'))
        t.set(r, 'window', cap + 1)
        self.assertTrue([m for m in mismatches(t.data, 'C6') if 'restored' in m['what']])

    def test_c9_expired_without_entry(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.ROUND) if t.get(r, 'phase') == d.CRUISE and len(t.seq_rows(t.get(r, 'seq'))) == 1
                 and t.get(r, 'probe_rtt_min_ns') > 0 and not t.get(r, 'flags') & t.f['idle_restart'])
        t.set(r, 'probe_rtt_stamp_mono', t.get(r, 'mono_ns') - 5_000_000_001)
        self.assertTrue([m for m in mismatches(t.data, 'C9') if 'expired' in m['what']])
        t.set(r, 'flags', t.get(r, 'flags') | t.f['idle_restart'])  # idle restart suppresses the entry
        self.assertFalse(mismatches(t.data, 'C9'))

    def test_c3_up_kept_after_plateau(self):
        t = Tr(fixture())
        r = next(r for r in t.rows(d.ROUND) if t.get(r, 'phase') == d.UP)
        t.set(r, 'plateau', 3)
        self.assertTrue([m for m in mismatches(t.data, 'C3') if 'plateau' in m['what']])

    def test_c9_entry_without_expiry(self):
        t = Tr(fixture())
        p, _ = next((p, q) for p, q in t.pairs() if t.get(q, 'phase') == d.PROBERTT and t.get(p, 'phase') != d.PROBERTT)
        t.set(p, 'probe_rtt_stamp_mono', t.get(p, 'mono_ns'))
        self.assertTrue([m for m in mismatches(t.data, 'C9') if 'expired' in m['what']])

    def test_incomplete_trace(self):
        data = fixture()
        data['overflow'] = 3
        self.assertFalse(d.run(data)['coverage']['complete'])


if __name__ == '__main__':
    unittest.main()
