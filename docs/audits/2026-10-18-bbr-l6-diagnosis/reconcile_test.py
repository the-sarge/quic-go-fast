#!/usr/bin/env python3
"""Tests for reconcile.py on a synthetic observation whose sender and relay records agree."""
import copy
import math
import pathlib
import sys
import unittest

sys.dont_write_bytecode = True
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import draft06check  # noqa: E402  (column names)
import reconcile as r  # noqa: E402

START = 1_800_000_000_000_000_000
RATE = 10e6       # QUIC bytes per second delivered
WIRE = 1428       # mean wire bytes per packet (1400 + 28)
DROP_EVERY_S = 0.37


def synthetic():
    cols = ['kind', 'unix_ns', 'mono_ns', 'seq', 'round', 'phase', 'bandwidth', 'bandwidth_short', 'inflight_long', 'inflight_short',
            'window', 'rate', 'min_rtt_ns', 'cycle', 'rounds_since_probe', 'probe_wait_ns', 'cycle_stamp_mono', 'flags', 'plateau',
            'full_bandwidth', 'ack_phase', 'size', 'bdp1', 'extra', 'quantum', 'prior_in_flight', 'post_in_flight', 'delivered', 'lost',
            'sample_rate', 'latest_rate', 'latest_volume', 'loss_bytes', 'loss_flight', 'probe_rtt_stamp_mono', 'probe_rtt_min_ns',
            'min_rtt_stamp_mono', 'ce_rate', 'ce_flight', 'sample_generation', 'a0', 'a1', 'a2', 'a3', 'a4', 'a5', 'a6', 'a7']
    c = {n: i for i, n in enumerate(cols)}
    drops = [k * DROP_EVERY_S for k in range(math.ceil(-60 / DROP_EVERY_S), math.ceil(302 / DROP_EVERY_S))]
    rows, prev = [], -60.0
    t = -60.0
    while t < 302:
        row = [0] * len(cols)
        row[c['kind']] = r.ROUND
        row[c['unix_ns']] = START + round(t * 1e9)
        row[c['delivered']] = round(RATE * (t + 60 - r.DELIVERY_SHIFT_NS / 1e9))
        row[c['a1']] = sum(1 for d in drops if prev < d + r.LOSS_SHIFT_NS / 1e9 <= t)
        row[c['bandwidth']] = round(0.9 * r.LINK_BYTES_PER_S * 1400 / WIRE)
        row[c['min_rtt_ns']] = 100_500_000
        rows.append(row)
        prev, t = t, t + 0.15
    samples = []
    for ms in range(-60000, 302001, 100):
        s = ms / 1000
        samples.append([ms, 0, 0, 0, 0, sum(1 for d in drops if d <= s), 0, round(RATE * (s + 60) * WIRE / 1400), 0, 0])
    total = samples[-1][7]
    relay = dict(Samples=samples, Forward=dict(Focal=dict(Delivered=round(total / WIRE), DeliveredBytes=round(total / WIRE) * WIRE)))
    return dict(columns=cols, flags=[], rows=rows), relay, c


class Reconcile(unittest.TestCase):
    def test_columns_match_the_trace(self):
        trace, _, c = synthetic()
        for name in ('unix_ns', 'delivered', 'bandwidth', 'min_rtt_ns', 'a1'):
            self.assertIn(name, c)
        self.assertEqual(r.ROUND, draft06check.ROUND)

    def test_agreeing_records_pass(self):
        trace, relay, _ = synthetic()
        out = r.reconcile(trace, relay, START)
        self.assertTrue(out['ok'], out['findings'])
        self.assertAlmostEqual(out['quic_share'], 1400 / WIRE, places=6)

    def test_extra_declared_losses(self):
        trace, relay, c = synthetic()
        for row in trace['rows'][::3]:
            row[c['a1']] += 1
        self.assertIn('R1', {f['rule'] for f in r.reconcile(trace, relay, START)['findings']})

    def test_delivery_disagrees(self):
        trace, relay, c = synthetic()
        for row in trace['rows']:
            row[c['delivered']] = round(row[c['delivered']] * 1.05)
        self.assertIn('R2', {f['rule'] for f in r.reconcile(trace, relay, START)['findings']})

    def test_inflated_bandwidth(self):
        trace, relay, c = synthetic()
        trace['rows'][len(trace['rows']) // 2][c['bandwidth']] = round(1.2 * r.LINK_BYTES_PER_S)
        self.assertIn('R3', {f['rule'] for f in r.reconcile(trace, relay, START)['findings']})

    def test_impossible_rtt(self):
        trace, relay, c = synthetic()
        trace['rows'][len(trace['rows']) // 2][c['min_rtt_ns']] = 95_000_000
        self.assertIn('R4', {f['rule'] for f in r.reconcile(trace, relay, START)['findings']})

    def test_frozen_sender_delivery_fails(self):
        trace, relay, c = synthetic()
        for row in trace['rows']:
            row[c['delivered']] = 1000
        out = r.reconcile(trace, relay, START)
        self.assertFalse(out['ok'])
        self.assertIn(('R2', 'fail'), {(f['rule'], f['status']) for f in out['findings']})

    def test_missing_sender_rounds_are_incomplete(self):
        trace, relay, c = synthetic()
        trace['rows'] = [row for row in trace['rows'] if not (START + 100 * 10**9 <= row[c['unix_ns']] < START + 201 * 10**9)]
        out = r.reconcile(trace, relay, START)
        self.assertFalse(out['ok'])
        self.assertIn(('short', 'incomplete'), {(f['period'], f['status']) for f in out['findings']})
        # A stale record from before the gap never stands for a boundary: incomplete, not a false failure.
        self.assertEqual(out['status'], 'incomplete')
        self.assertFalse([f for f in out['findings'] if f['status'] == 'fail'])

    def test_missing_relay_boundary_is_incomplete(self):
        trace, relay, _ = synthetic()
        relay['Samples'] = [s for s in relay['Samples'] if s[0] >= 0]
        out = r.reconcile(trace, relay, START)
        self.assertFalse(out['ok'])
        self.assertTrue(any(f['period'] == 'equal' and f['status'] == 'incomplete' for f in out['findings']))

    def test_empty_relay_delivery_is_incomplete(self):
        trace, relay, _ = synthetic()
        for s in relay['Samples']:
            s[7] = 0
        out = r.reconcile(trace, relay, START)
        self.assertFalse(out['ok'])
        self.assertIn(('R2', 'incomplete'), {(f['rule'], f['status']) for f in out['findings']})

    def test_missing_bandwidth_and_rtt_are_incomplete(self):
        trace, relay, c = synthetic()
        for row in trace['rows']:
            row[c['bandwidth']] = 0
            row[c['min_rtt_ns']] = 0
        out = r.reconcile(trace, relay, START)
        self.assertEqual(out['status'], 'incomplete')
        self.assertEqual({('R3', 'incomplete'), ('R4', 'incomplete')}, {(f['rule'], f['status']) for f in out['findings']})

    def test_no_drops_in_a_period(self):
        trace, relay, c = synthetic()
        relay = copy.deepcopy(relay)
        for s in relay['Samples']:
            s[5] = 0
        for row in trace['rows']:
            row[c['a1']] = 0
        self.assertTrue(r.reconcile(trace, relay, START)['ok'])


if __name__ == '__main__':
    unittest.main()
