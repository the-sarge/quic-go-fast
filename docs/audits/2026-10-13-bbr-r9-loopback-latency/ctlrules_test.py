#!/usr/bin/env python3
"""#744 synthetic cases for the registered rules (README "Synthetic cases").

Data-level cases construct per-request segment rows for two recorder arms over
six blocks of 120 requests, so each case has a known cause. Every case named
by D3 is here: a deep sender queue, a deep receiver queue, a slow worker, a busy
receiver loop, a delayed reply write, a near-zero p95 rise, a delay the
recorder cannot place, and the second consideration's two counterexamples.
"""
import math
import random
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import ctlrules as C  # noqa: E402
from ctl import QUEUE_SEGMENTS, RESIDUAL, SEGMENTS  # noqa: E402

US = 1000
# Base segment means (us); queue waits are exponential, the rest jitter by +-30%.
BASE = dict(s_pack=10, s_entry=1, s_queue=40, s_submit=4, t_req=15, r_backlog=6, r_wake=6, r_app=1, r_pack=3, r_entry=1,
            r_queue=3, r_submit=5, t_rep=12, s_backlog=6, s_wake=6)
WIDE = (0.0, 100.0)  # a consistency interval that never binds (the data-level cases test the estimator)


def rows(rng, n=120, scale=1.0, add=None, spikes=None, sender_bytes=(30, 1400), receiver_bytes=(0, 60)):
    """n request rows. add: {segment: us added to every request}; spikes: (segment, us, share) on a random share."""
    out = []
    for _ in range(n):
        r = {}
        for s, m in BASE.items():
            v = rng.expovariate(1 / (m * scale)) if s.endswith('queue') else m * scale * rng.uniform(0.7, 1.3)
            r[s] = int(v * US)
        for s, v in (add or {}).items():
            r[s] += int(v * US)
        if spikes and rng.random() < spikes[2]:
            r[spikes[0]] += int(spikes[1] * US)
        r['total'] = sum(r[s] for s in SEGMENTS)
        ahead = sender_bytes[0] * sender_bytes[1]
        r['sender_queue'] = dict(entries_ahead=sender_bytes[0], bytes_ahead=ahead, capacity=32, entry_bytes=1400)
        r['receiver_queue'] = dict(entries_ahead=receiver_bytes[0], bytes_ahead=receiver_bytes[0] * receiver_bytes[1], capacity=32,
                                   entry_bytes=60)
        out.append(r)
    return out


def pair(seed, a_kw=None, b_kw=None, blocks=6):
    rng = random.Random(seed)
    out = []
    for _ in range(blocks):
        s = rng.uniform(0.95, 1.05)  # block-level variation
        a = rows(rng, scale=s, **(a_kw or {}))
        b = rows(rng, scale=s, **(b_kw or {}))
        out.append(dict(a=a, b=b, a_fixture=[r['total'] / 1e6 for r in a], b_fixture=[r['total'] / 1e6 for r in b]))
    return out


def mech(blocks):
    return C.mechanism_reading(blocks, WIDE, True, SEGMENTS, QUEUE_SEGMENTS)


class Mechanism(unittest.TestCase):
    def test_deep_sender_queue(self):
        out, d = mech(pair(1, a_kw=dict(add=dict(s_queue=300))))
        self.assertEqual(out, 'queue wait')
        self.assertEqual(C.targets(d), ['sender'])
        self.assertIn('s_queue', d['named_segments'])

    def test_deep_receiver_queue(self):
        out, d = mech(pair(2, a_kw=dict(add=dict(r_queue=300))))
        self.assertEqual(out, 'queue wait')
        self.assertEqual(C.targets(d), ['receiver'])

    def test_slow_worker(self):
        # Every submission takes 20 us longer and a request waits behind about 30 slow entries.
        out, d = mech(pair(3, a_kw=dict(add=dict(s_submit=20, s_queue=600))))
        self.assertEqual(out, 'queue wait')
        self.assertEqual(C.targets(d), ['sender'])

    def test_busy_receiver_loop(self):
        out, d = mech(pair(4, a_kw=dict(add=dict(r_backlog=200, r_pack=20))))
        self.assertEqual(out, 'load or other')
        self.assertIn('r_backlog', d['named_segments'])

    def test_delayed_reply_write(self):
        out, d = mech(pair(5, a_kw=dict(add=dict(r_app=300))))
        self.assertEqual(out, 'load or other')
        self.assertEqual(d['named_segments'], ['r_app'])

    def test_unplaceable_delay_gets_no_segment_label(self):
        out, d = mech(pair(6, a_kw=dict(add=dict(t_req=300))))
        self.assertEqual(out, 'load or other')
        # Only the residual carries it: no placed segment is named.
        self.assertTrue(set(d['named_segments']) <= set(RESIDUAL))
        self.assertIn('t_req', d['named_segments'])

    def test_near_zero_rise_is_inconclusive(self):
        out, d = mech(pair(7, a_kw=dict(add=dict(r_wake=0.5))))
        self.assertEqual(out, 'inconclusive')
        self.assertIn('rise', d['reason'])

    def test_counterexample_rise_outside_queue_while_queue_dominates_the_extreme_tail(self):
        # The p95 rises by a shift in the receiver's application segment; 1% of A's requests carry 2 ms of
        # sender queue wait. A tail-mean decomposition credits the queue; the counterfactual p95 must not.
        out, d = mech(pair(8, a_kw=dict(add=dict(r_app=60), spikes=('s_queue', 2000, 0.01))))
        self.assertEqual(out, 'load or other')
        self.assertIn('r_app', d['named_segments'])
        tm = d['tail_mean_difference_ns']
        self.assertGreater(tm['s_queue'], tm['r_app'])  # the decomposition this estimator must not follow

    def test_counterexample_rise_while_the_upper_tail_mean_falls(self):
        # A's p95 rises by queue wait while B carries 3 ms receive-backlog spikes on 3% of requests, so the
        # upper-tail mean falls from B to A. The counterfactual p95 must still read queue wait.
        blocks = pair(9, a_kw=dict(add=dict(s_queue=60)), b_kw=dict(spikes=('r_backlog', 3000, 0.03)))
        a = [r['total'] for b in blocks for r in b['a']]
        bb = [r['total'] for b in blocks for r in b['b']]
        pa, pb = C.pooled_p95(a), C.pooled_p95(bb)
        tail = lambda v, p: sum(x for x in v if x >= p) / len([x for x in v if x >= p])
        self.assertGreater(pa, pb)
        self.assertLess(tail(a, pa), tail(bb, pb))
        out, d = mech(blocks)
        self.assertEqual(out, 'queue wait')

    def test_mixed(self):
        out, d = mech(pair(10, a_kw=dict(add=dict(s_queue=60, r_app=60))))
        self.assertEqual(out, 'mixed')
        self.assertTrue(0 < d['queue_share']['ci'][0] and d['queue_share']['ci'][1] < 1)

    def small(self, seed, n):
        # Six blocks of n requests, a 20 us rise split between the sender's queue and the reply's application
        # segment, with wider block variation: intervals too wide to label.
        rng = random.Random(seed)
        out = []
        for _ in range(6):
            s = rng.uniform(0.9, 1.1)
            a, b = rows(rng, n=n, scale=s, add=dict(s_queue=20, r_app=20)), rows(rng, n=n, scale=s)
            out.append(dict(a=a, b=b, a_fixture=[r['total'] / 1e6 for r in a], b_fixture=[r['total'] / 1e6 for r in b]))
        return out

    def test_unstable_share_is_inconclusive_not_mixed(self):
        # An interval that does not exclude a zero share (here about -0.30 to 0.68) gets no label.
        out, d = mech(self.small(60, 60))
        self.assertEqual(out, 'inconclusive')
        self.assertEqual(d['reason'], 'unstable share')
        out, d = mech(self.small(64, 60))
        self.assertEqual(out, 'inconclusive')

    def test_consistency_and_perturbation_gate(self):
        blocks = pair(1, a_kw=dict(add=dict(s_queue=300)))
        self.assertEqual(C.mechanism_reading(blocks, WIDE, False, SEGMENTS, QUEUE_SEGMENTS)[0], 'instrumentation gap')
        out, d = C.mechanism_reading(blocks, (0.5, 0.9), True, SEGMENTS, QUEUE_SEGMENTS)
        self.assertEqual(out, 'inconclusive')
        self.assertIn('interval', d['reason'])
        self.assertEqual(C.mechanism_reading(blocks[:4], WIDE, True, SEGMENTS, QUEUE_SEGMENTS)[0], 'evidence gap')
        self.assertEqual(C.mechanism_reading(blocks, None, True, SEGMENTS, QUEUE_SEGMENTS)[0], 'evidence gap')

    def test_counterfactual_identity(self):
        rng = random.Random(11)
        a = rows(rng)
        self.assertEqual(C.counterfactual_p95(a, a, ['s_queue', 'r_queue']), C.pooled_p95([r['total'] for r in a]))


def latencies(rng, n, p95_ms, sigma=0.42):
    """Lognormal reply latencies (ms) whose p95 is p95_ms."""
    return [p95_ms * math.exp(sigma * (rng.gauss(0, 1) - 1.645)) for _ in range(n)]


def plain(seed, cand, cand8, aa=None, blocks=6, n=120, noise=0.02):
    rng = random.Random(seed)
    out = []
    for _ in range(blocks):
        s = rng.uniform(1 - noise, 1 + noise)
        out.append(dict(cand=latencies(rng, n, cand * s), cand8=latencies(rng, n, cand8 * s), aa=latencies(rng, n, (aa or cand) * s)))
    return out


class Depth(unittest.TestCase):
    def test_raises(self):
        out, d = C.depth_reading(plain(20, 0.26, 0.16))
        self.assertEqual(out, 'depth raises latency')

    def test_null_operating_characteristic(self):
        # No depth effect: non-increase in most seeds.
        # Six clusters make the block bootstrap anti-conservative: over 200 seeds (ctlrules_oc.py) the null reads
        # "depth raises latency" about 4.5% of the time, not 2.5%. Disclosed in the README.
        outs = [C.depth_reading(plain(seed, 0.16, 0.16))[0] for seed in range(100, 120)]
        self.assertGreaterEqual(outs.count('non-increase'), 16)
        self.assertLessEqual(outs.count('depth raises latency'), 2)

    def test_large_effect_operating_characteristic(self):
        outs = [C.depth_reading(plain(seed, 0.26, 0.16))[0] for seed in range(120, 140)]
        self.assertEqual(outs.count('depth raises latency'), 20)

    def test_reversed_block_blocks_raises(self):
        # A large rise in four blocks and a reversal in two: fewer than five blocks above 1, and the interval
        # reaches past the margin, so neither "raises" nor "non-increase".
        b = plain(21, 0.30, 0.16)
        for i in (1, 4):
            b[i] = dict(b[i], cand=b[i]['cand8'], cand8=b[i]['cand'])
        out, d = C.depth_reading(b)
        self.assertLess(d['blocks_above_1'], 5)
        self.assertEqual(out, 'inconclusive')

    def test_wide_aa_never_eases_non_increase(self):
        # The same direct comparison with a wide A/A arm: the non-increase ending must not change.
        narrow = plain(23, 0.16, 0.16)
        wide = [dict(b, aa=[x * random.Random(i).uniform(0.5, 2.0) for x in b['aa']]) for i, b in enumerate(narrow)]
        self.assertEqual(C.depth_reading(narrow)[0], C.depth_reading(wide)[0])
        self.assertEqual(C.depth_reading(narrow)[1]['ci'], C.depth_reading(wide)[1]['ci'])

    def test_wide_aa_blocks_raises(self):
        # A rise inside a wide A/A interval is not "depth raises latency".
        b = plain(24, 0.19, 0.16, aa=0.26)
        out, d = C.depth_reading(b)
        self.assertGreater(d['ci'][0], 1)
        self.assertNotEqual(out, 'depth raises latency')

    def test_evidence_gap(self):
        self.assertEqual(C.depth_reading(plain(25, 0.26, 0.16, blocks=4))[0], 'evidence gap')
        self.assertEqual(C.depth_reading(plain(25, 0.26, 0.16, n=90))[0], 'evidence gap')


class Perturbation(unittest.TestCase):
    def blocks(self, g_ratio, p_ratio):
        rng = random.Random(30)
        out = []
        for _ in range(6):
            b = {}
            for rev in ('cand', 'cand8'):
                b[f'{rev}_goodput'] = 5000.0
                b[f'{rev}_ctl_goodput'] = 5000.0 * g_ratio
                b[rev] = [rng.uniform(0.1, 0.2) for _ in range(120)]
                b[f'{rev}_ctl'] = [x * p_ratio for x in b[rev]]
            out.append(b)
        return out

    def test_pass(self):
        self.assertTrue(C.perturbation(self.blocks(0.995, 1.05))['passes'])

    def test_goodput_fails(self):
        r = C.perturbation(self.blocks(0.985, 1.0))
        self.assertFalse(r['passes'])
        self.assertFalse(r['cand']['goodput_passes'])

    def test_p95_fails(self):
        r = C.perturbation(self.blocks(1.0, 1.2))
        self.assertFalse(r['passes'])
        self.assertFalse(r['cand']['p95_passes'])


class Bound(unittest.TestCase):
    def test_sender_bound_between_depths(self):
        rng = random.Random(40)
        # r9: 30 entries ahead (42 KB) and about 75 us of wait; depth 8: 7 entries (9.8 KB), 18 us.
        a = rows(rng, n=600, add=dict(s_queue=35), sender_bytes=(30, 1400))
        b = rows(rng, n=600, add=dict(s_queue=-22), sender_bytes=(7, 1400))
        for r in b:
            r['s_queue'] = max(r['s_queue'], 1)
        bound, d = C.stage2_bound(a, b, 'sender')
        self.assertIsNotNone(bound)
        self.assertEqual(bound % C.MAX_PACKET, 0)
        self.assertGreater(bound, d['b8'])
        self.assertLess(bound, d['b9'])

    def test_receiver_with_empty_queue_is_not_attempted(self):
        rng = random.Random(41)
        a, b = rows(rng, receiver_bytes=(0, 60)), rows(rng, receiver_bytes=(0, 60))
        bound, d = C.stage2_bound(a, b, 'receiver')
        self.assertIsNone(bound)
        self.assertEqual(d['outcome'], 'not attempted')


class Stage2(unittest.TestCase):
    def lat(self, new, cand=0.26, aa=None, noise=0.02, seed=50):
        rng = random.Random(seed)
        out = []
        for _ in range(6):
            s = rng.uniform(1 - noise, 1 + noise)
            out.append(dict(cand=latencies(rng, 120, cand * s), aa=latencies(rng, 120, (aa or cand) * s), new=latencies(rng, 120, new * s)))
        return out

    def test_efficacy_endings(self):
        self.assertEqual(C.stage2_latency(self.lat(0.18))[0], 'effective')
        self.assertEqual(C.stage2_latency(self.lat(0.34))[0], 'demonstrated worsening')
        self.assertEqual(C.stage2_latency(self.lat(0.26))[0], 'null')
        # A wide interval such as 0.80-1.15 is inconclusive, not null.
        out, d = C.stage2_latency(self.lat(0.25, noise=0.30, seed=51))
        self.assertIn(out, ('inconclusive', 'partial'))
        self.assertNotEqual(out, 'null')

    def test_partial(self):
        # Upper bound below 1 but the point not below a wide A/A interval's lower bound.
        b = self.lat(0.22, aa=0.16, seed=52)
        out, d = C.stage2_latency(b)
        self.assertEqual(out, 'partial')

    def guards(self, gl='inside', cpu='inside', integrity=True, pres='inside', screen='no regression'):
        mv = lambda m: dict(movement=m)
        loop = {w: dict(goodput=mv(gl), cpu=mv(cpu)) for w in ('stream', 'datagram')}
        return loop, integrity, dict(send=pres, receive=pres), {w: dict(reading=screen) for w in ('stream', 'datagram')}

    def test_outcomes(self):
        self.assertEqual(C.stage2_outcome('inert', 'effective', *self.guards()), ('inert', False, ['inert']))
        self.assertEqual(C.stage2_outcome(None, 'null', *self.guards())[:2], ('null', True))
        self.assertEqual(C.stage2_outcome(None, 'demonstrated worsening', *self.guards())[:2], ('demonstrated worsening', True))
        self.assertEqual(C.stage2_outcome(None, 'partial', *self.guards())[:2], ('partial', False))
        self.assertEqual(C.stage2_outcome(None, 'inconclusive', *self.guards())[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards())[:2], ('kept', False))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(gl='lower'))[:2], ('effective, guard-failed', True))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(cpu='higher'))[:2], ('effective, guard-failed', True))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(integrity=False))[:2], ('effective, guard-failed', True))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(pres='above'))[:2], ('effective, guard-failed', True))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(screen='regression'))[:2], ('effective, guard-failed', True))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(screen='inconclusive'))[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome(None, 'effective', *self.guards(gl='unusable'))[:2], ('inconclusive', False))

    def test_range_movement(self):
        self.assertEqual(C._range_movement([(0.9, 0.99), (0.91, 1.01), (0.9, 1.0), (0.92, 0.98), (0.9, 1.02)])['movement'], 'lower')
        self.assertEqual(C._range_movement([(1.0, 0.99)] * 4)['movement'], 'unusable')


if __name__ == '__main__':
    unittest.main()
