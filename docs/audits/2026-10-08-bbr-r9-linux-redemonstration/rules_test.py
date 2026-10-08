#!/usr/bin/env python3
"""Synthetic cases for #715's registered rules (rules.py), run before any stage data existed.

D3 names five required cases for the preservation rules: unchanged controls;
unchanged instructions with increased retention; a repeatable latency
regression inside a wide A/A envelope; sparse samples; unusable measurements.
Run: python3 rules_test.py
"""
import random
import unittest

import rules as R


def gauss_list(rng, n, mu, sd):
    return [max(0.01, rng.gauss(mu, sd)) for _ in range(n)]


class ContaminationReruns(unittest.TestCase):
    def test_clean_original_stands(self):
        self.assertEqual(R.choose_block([('o', True, False), ('r', True, False)]), ('o', 'clean'))

    def test_clean_rerun_replaces_contaminated(self):
        self.assertEqual(R.choose_block([('o', True, True), ('r', True, False)]), ('r', 'clean'))

    def test_contaminated_rerun_keeps_original(self):
        self.assertEqual(R.choose_block([('o', True, True), ('r', True, True)]), ('o', 'contaminated'))

    def test_only_one_rerun_counts(self):
        self.assertEqual(R.choose_block([('o', True, True), ('r', True, True), ('r2', True, False)]), ('o', 'contaminated'))

    def test_unusable_original_without_rerun(self):
        self.assertEqual(R.choose_block([('o', False, False)]), ('o', 'unusable'))


class LoopbackBottleneck(unittest.TestCase):
    def test_stage_of(self):
        self.assertEqual(R.stage_of(['k:udp_sendmsg', 'syscall.Syscall6', 'github.com/quic-go/quic-go.(*sendQueue).Run', 'runtime.goexit']), 'send_worker')
        self.assertEqual(R.stage_of(['github.com/quic-go/quic-go/internal/congestion.(*BBRSender).Feedback',
                                     'github.com/quic-go/quic-go.(*Conn).run', 'github.com/quic-go/quic-go.(*Transport).doDial.func1']), 'conn_loop')
        self.assertEqual(R.stage_of(['runtime.findRunnable', 'runtime.schedule', 'runtime.mstart']), 'runtime')
        self.assertEqual(R.stage_of(['main.(*session).write', 'runtime.goexit']), 'app')

    def test_utilization(self):
        u = R.stage_utilization({'conn_loop': 450, 'send_worker': 300, 'runtime': 250}, process_cpu_s=36.0, window_s=20.0)
        self.assertAlmostEqual(u['conn_loop'], 0.81)
        self.assertEqual(R.side_state(u, 'send'), 'ambiguous')
        self.assertEqual(R.side_state({'conn_loop': 0.95}, 'send'), 'saturated')
        self.assertEqual(R.side_state({'conn_loop': 0.5, 'read_loop': 0.6}, 'receive'), 'unsaturated')

    def arms(self, send, receive):
        return {a: {'send': [send] * 4, 'receive': [receive] * 4} for a in ('prev', 'cand')}

    def test_sender_limited_consistent(self):
        v, _ = R.bottleneck_verdict(self.arms('saturated', 'unsaturated'), (0.90, 1.08, 4, 4))
        self.assertEqual(v, 'sender per-packet work')

    def test_reciprocal_movement_without_saturation_is_not_a_verdict(self):
        # Goodput rises as cycles per packet fall, but nothing saturates: no sender verdict.
        v, _ = R.bottleneck_verdict(self.arms('unsaturated', 'unsaturated'), (0.90, 1.08, 4, 4))
        self.assertEqual(v, 'neither endpoint saturated')

    def test_conflict_saturated_but_goodput_flat(self):
        v, why = R.bottleneck_verdict(self.arms('saturated', 'unsaturated'), (0.90, 1.00, 2, 4))
        self.assertEqual(v, 'inconclusive')
        self.assertIn('conflict', why)

    def test_unchanged_work_needs_no_goodput_change(self):
        v, _ = R.bottleneck_verdict(self.arms('saturated', 'unsaturated'), (0.995, 1.00, 2, 4))
        self.assertEqual(v, 'sender per-packet work')

    def test_receiver_competitor(self):
        v, _ = R.bottleneck_verdict(self.arms('unsaturated', 'saturated'), (0.90, 1.0, 2, 4))
        self.assertEqual(v, 'receiver-side limit')

    def test_both_saturated_is_ambiguous(self):
        v, _ = R.bottleneck_verdict(self.arms('saturated', 'saturated'), (0.90, 1.08, 4, 4))
        self.assertEqual(v, 'inconclusive')

    def test_arms_disagree(self):
        arms = {'prev': {'send': ['saturated'] * 4, 'receive': ['unsaturated'] * 4},
                'cand': {'send': ['unsaturated'] * 4, 'receive': ['saturated'] * 4}}
        self.assertEqual(R.bottleneck_verdict(arms, (0.9, 1.0, 2, 4))[0], 'inconclusive')

    def test_majority_needs_three_blocks(self):
        self.assertEqual(R.majority(['saturated', 'saturated', 'ambiguous', 'unsaturated']), 'ambiguous')


def synth_timeline(rate_frac, late_frac=0.0, credit_frac=0.0, cwnd_frac=0.0, sent_frac=None, seconds=30, overflow=0,
                   queue_bytes=0, missing_queue=False):
    """A sender paced at rate_frac of capacity, with lateness and credit waits as shares of time, and a constant queue."""
    cap = R.CAPACITY_BPS / 8
    size, wire = 1400, 1428
    rate_payload = rate_frac * cap * size / wire
    buckets, rows, queue = [], [(0, rate_payload)], []
    span = 10_000_000
    sent = sent_frac if sent_frac is not None else max(0.0, min(rate_frac, 1.0) * (1 - late_frac - credit_frac))
    if sent_frac is None and rate_frac > 1:
        sent = rate_frac * (1 - late_frac - credit_frac)
    per_bucket_payload = sent * cap * size / wire * span / 1e9
    for i in range(seconds * 100):
        t = i * span
        states = {'paced_ns': span * (1 - late_frac - credit_frac - cwnd_frac), 'credit_ns': span * credit_frac, 'cwnd_ns': span * cwnd_frac}
        buckets.append((t, states, per_bucket_payload, per_bucket_payload / size, span * late_frac))
        if not missing_queue:
            queue.append((t, queue_bytes))
    return buckets, rows, (0, seconds * 1_000_000_000), overflow, wire, queue


class Timeline(unittest.TestCase):
    def verdict(self, *a, **k):
        d = R.timeline_decomposition(*synth_timeline(*a, **k))
        return R.timeline_verdict(d), d

    def test_model_paces_below_capacity(self):
        v, d = self.verdict(0.90)
        self.assertEqual(v, 'model')
        self.assertAlmostEqual(d['model'], 0.10, places=2)

    def test_timing_lateness_with_model_at_capacity(self):
        v, d = self.verdict(1.0, late_frac=0.10)
        self.assertEqual(v, 'timing')
        self.assertAlmostEqual(d['timing'], 0.10, places=2)

    def test_credit_waits_count_as_timing(self):
        self.assertEqual(self.verdict(1.0, credit_frac=0.10)[0], 'timing')

    def test_model_takes_precedence_then_timing(self):
        v, d = self.verdict(0.95, late_frac=0.06)
        self.assertEqual(v, 'mixed')
        self.assertAlmostEqual(d['model'], 0.05, places=2)
        self.assertAlmostEqual(d['timing'], 0.057, places=2)

    def test_lateness_absorbed_by_a_queue_is_not_counted(self):
        # Down-like: paced at 0.9C with lateness, but a standing queue covers the gap: the link never idles.
        # (A standing queue under an under-capacity sender breaks conservation, so the verdict is 'unusable';
        # the decomposition itself must still charge nothing.)
        v, d = self.verdict(0.90, late_frac=0.10, queue_bytes=50_000)
        self.assertEqual((d['idle'], d['timing'], d['model']), (0.0, 0.0, 0.0))
        self.assertEqual(v, 'unusable')

    def test_lateness_above_capacity_loses_only_below_capacity(self):
        # Paced at 1.25C with an empty queue: lateness of 10% still leaves sending above capacity.
        d = R.timeline_decomposition(*synth_timeline(1.25, late_frac=0.10))
        self.assertAlmostEqual(d['timing'], 0.0, places=6)

    def test_no_deficit(self):
        self.assertEqual(self.verdict(1.0)[0], 'no deficit')

    def test_unexplained_shortfall(self):
        # Model at capacity, no lateness or credit waits, yet the sender emits 0.9: neither hypothesis.
        self.assertEqual(self.verdict(1.0, sent_frac=0.90)[0], 'unexplained')

    def test_short_coverage_unusable(self):
        self.assertEqual(self.verdict(0.9, seconds=20)[0], 'unusable')

    def test_missing_queue_unusable(self):
        self.assertEqual(self.verdict(0.9, missing_queue=True)[0], 'unusable')

    def test_inconsistent_attribution_unusable(self):
        # Drops cut delivery while the attributed idle share is zero: the attribution does not describe the deficit.
        cap_packets = 30 * R.CAPACITY_BPS / 8 / 1428
        self.assertEqual(self.verdict(1.0, overflow=int(0.08 * cap_packets))[0], 'unusable')

    def test_cwnd_limited_uses_sent_rate(self):
        # Pacing at capacity but window-limited sending 0.88 of it: model.
        self.assertEqual(self.verdict(1.0, cwnd_frac=0.6, sent_frac=0.88)[0], 'model')

    def test_workload_and_stage(self):
        self.assertEqual(R.workload_verdict(['model', 'model', 'mixed', 'model']), 'model')
        self.assertEqual(R.workload_verdict(['model', 'timing', 'mixed', 'model']), 'inconclusive')
        self.assertEqual(R.workload_verdict(['model', 'unusable', 'unusable', 'model']), 'evidence gap')
        self.assertEqual(R.stage_verdict({'stream': 'model', 'datagram': 'model'}), 'model behaviour')
        self.assertEqual(R.stage_verdict({'stream': 'model', 'datagram': 'timing'}), 'inconclusive')
        self.assertEqual(R.mac_comparison({'stream': 'model', 'datagram': 'model'}, {'stream': 'model', 'datagram': 'mixed'}),
                         {'stream': 'same', 'datagram': 'different'})


class ReceiverSplit(unittest.TestCase):
    def split(self, before, inside, after):
        return dict(before=before, inside=inside, after=after)

    def test_interpolation(self):
        s = [(1_000, 0), (2_000, 100), (3_000, 300)]
        self.assertEqual(R.interpolate_ticks(s, 2_500), 200)
        sp = R.cpu_split(s, 0, (1_500, 2_500), total_cpu_s=4.0)
        self.assertAlmostEqual(sp['before'], 0.5)
        self.assertAlmostEqual(sp['inside'], 1.5)
        self.assertAlmostEqual(sp['after'], 2.0)

    def test_outside_window(self):
        blocks = [dict(cand=self.split(2.0, 10.3, 1.0), reno=self.split(1.0, 10.0, 0.5), cand_gib=1, reno_gib=1)] * 5
        self.assertEqual(R.receiver_localization(blocks)['outcome'], 'outside window')

    def test_inside_window(self):
        blocks = [dict(cand=self.split(1.0, 12.0, 0.5), reno=self.split(1.0, 10.0, 0.5), cand_gib=1, reno_gib=1)] * 5
        self.assertEqual(R.receiver_localization(blocks)['outcome'], 'inside window')

    def test_mixed(self):
        blocks = [dict(cand=self.split(1.2, 10.8, 0.5), reno=self.split(1.0, 10.0, 0.5), cand_gib=1, reno_gib=1)] * 5
        self.assertEqual(R.receiver_localization(blocks)['outcome'], 'mixed')


class RSSPreservation(unittest.TestCase):
    def block(self, c_rss, r_rss, c_ret, r_ret, sites=None):
        return dict(cand_rss=c_rss, reno_rss=r_rss, cand_retained=c_ret, reno_retained=r_ret, site_excess=sites or {})

    def test_unchanged_control(self):
        rng = random.Random(1)
        blocks = [self.block(18.4 + rng.uniform(-.3, .3), 18.4 + rng.uniform(-.3, .3), 6 + rng.uniform(-.2, .2), 6 + rng.uniform(-.2, .2))
                  for _ in range(6)]
        self.assertEqual(R.rss_preservation(blocks, high_threshold_mib=20.2)[0], 'not reproduced')

    def test_unchanged_instructions_increased_retention(self):
        # Instructions play no part in the rule: retention alone decides.
        blocks = [self.block(20.9, 18.4, 7.4, 6.0, {'delivery': 1.2, 'bookkeeping': 0.1, 'other': 0.1}) for _ in range(6)]
        out, d = R.rss_preservation(blocks, high_threshold_mib=20.2)
        self.assertEqual(out, 'retention excess')
        self.assertEqual(d['lead'], 'delivery')

    def test_bimodal_rss_without_retention(self):
        blocks = [self.block(20.9 if i < 4 else 18.4, 18.4, 6.0, 6.0) for i in range(6)]
        self.assertEqual(R.rss_preservation(blocks, high_threshold_mib=20.2)[0], 'RSS excess without retained heap')

    def test_range_membership_is_not_a_pass(self):
        # RSS median above the limit while retention is flat: never 'not reproduced'.
        blocks = [self.block(20.5, 18.4, 6.0, 6.0) for _ in range(6)]
        self.assertNotEqual(R.rss_preservation(blocks, high_threshold_mib=25)[0], 'not reproduced')

    def test_sparse_and_unusable(self):
        blocks = [self.block(18.4, 18.4, 6, 6), None, None, self.block(18.4, 18.4, 6, 6), None, None]
        self.assertEqual(R.rss_preservation(blocks, high_threshold_mib=20.2)[0], 'evidence gap')


class LatencyPreservation(unittest.TestCase):
    def test_unchanged_control(self):
        rng = random.Random(2)
        blocks = [dict(cand=gauss_list(rng, 120, 0.15, 0.04), reno=gauss_list(rng, 120, 0.15, 0.04)) for _ in range(6)]
        self.assertEqual(R.latency_preservation(blocks)[0], 'not reproduced')

    def test_repeatable_regression_inside_wide_aa_envelope(self):
        # Heavy-tailed noise gives a wide per-block A/A envelope; the arm's tail is 1.5x in every block.
        rng = random.Random(3)

        def tail(scale):
            return [scale * (0.15 + (rng.paretovariate(3) - 1) * 0.1) for _ in range(120)]
        blocks = [dict(cand=tail(1.5), reno=tail(1.0)) for _ in range(6)]
        aa = [R.pooled_p95(tail(1.0)) / R.pooled_p95(tail(1.0)) for _ in range(6)]
        self.assertTrue(max(aa) / min(aa) > 1.2)  # wide envelope
        self.assertEqual(R.latency_preservation(blocks)[0], 'regression reproduced')

    def test_sparse_samples(self):
        rng = random.Random(4)
        blocks = [dict(cand=gauss_list(rng, 20, 0.5, 0.2), reno=gauss_list(rng, 20, 0.3, 0.1)) for _ in range(6)]
        self.assertEqual(R.latency_preservation(blocks)[0], 'evidence gap')

    def test_unusable(self):
        rng = random.Random(5)
        blocks = [None, None, None, dict(cand=gauss_list(rng, 120, .2, .05), reno=gauss_list(rng, 120, .2, .05)), None, None]
        self.assertEqual(R.latency_preservation(blocks)[0], 'evidence gap')

    def test_intermittent_regression_is_reproduced(self):
        # Half the blocks carry a 1.3-1.5x tail: conservative for a preservation check.
        rng = random.Random(6)
        blocks = [dict(cand=gauss_list(rng, 120, 0.15, 0.04 * (2.2 if i % 2 else 1.0)), reno=gauss_list(rng, 120, 0.15, 0.04))
                  for i in range(6)]
        self.assertEqual(R.latency_preservation(blocks)[0], 'regression reproduced')

    def test_small_effect_straddling_the_limit_is_inconclusive(self):
        rng = random.Random(7)
        blocks = [dict(cand=gauss_list(rng, 120, 0.15, 0.04 * 1.6), reno=gauss_list(rng, 120, 0.15, 0.04)) for _ in range(6)]
        out, d = R.latency_preservation(blocks)
        self.assertEqual(out, 'inconclusive', d)
        self.assertLess(d['ci'][0], 1.20)
        self.assertGreater(d['ci'][1], 1.20)

if __name__ == '__main__':
    unittest.main(verbosity=1)
