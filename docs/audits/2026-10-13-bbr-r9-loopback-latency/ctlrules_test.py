#!/usr/bin/env python3
"""#744 synthetic cases for the registered rules (README "Synthetic cases").

Data-level cases construct per-request segment rows for two recorder arms over
six blocks of 120 requests, so each case has a known cause. Every case named
by D3 is here: a deep sender queue, a deep receiver queue, a slow worker, a busy
receiver loop, a delayed reply write, a near-zero p95 rise, a delay the
recorder cannot place, and the second consideration's two counterexamples.
They also cover missing request chains, the Stage 2 bound, efficacy and outcome
rules, and fail-closed inputs.
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
PASS = dict(state='pass', reasons=[])


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
            r['spiked'] = True
        r['total'] = sum(r[s] for s in SEGMENTS)
        r['sender_queue'] = dict(entries_ahead=sender_bytes[0], bytes_ahead=sender_bytes[0] * sender_bytes[1], capacity=32,
                                 entry_bytes=sender_bytes[1])
        r['receiver_queue'] = dict(entries_ahead=receiver_bytes[0], bytes_ahead=receiver_bytes[0] * receiver_bytes[1], capacity=32,
                                   entry_bytes=receiver_bytes[1])
        out.append(r)
    return out


def pair(seed, a_kw=None, b_kw=None, blocks=6, n=120, drop_a=None, drop_b=None, variation=0.05):
    """Paired blocks; drop_a/drop_b(row, rng) -> True moves a request to the arm's incomplete list (its chain is
    missing; its fixture latency is kept)."""
    rng = random.Random(seed)
    out = []
    for _ in range(blocks):
        s = rng.uniform(1 - variation, 1 + variation)  # block-level variation
        blk = {}
        for arm, kw, drop in (('a', a_kw, drop_a), ('b', b_kw, drop_b)):
            rs = rows(rng, n=n, scale=s, **(kw or {}))
            blk[arm + '_fixture'] = [r['total'] / 1e6 for r in rs]
            keep = [not (drop and drop(r, rng)) for r in rs]
            blk[arm] = [r for r, k in zip(rs, keep) if k]
            blk[arm + '_missing'] = [r['total'] for r, k in zip(rs, keep) if not k]
        out.append(blk)
    return out


def mech(blocks, min_blocks=C.MIN_BLOCKS):
    return C.mechanism_reading(blocks, WIDE, PASS, SEGMENTS, QUEUE_SEGMENTS, min_blocks=min_blocks)


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
        self.assertFalse(d['unplaced'])

    def test_unplaceable_delay_gets_no_segment_label(self):
        out, d = mech(pair(6, a_kw=dict(add=dict(t_req=300))))
        self.assertEqual(out, 'load or other')
        # Only the residual carries it: no placed segment is named, and the reading says unplaced.
        self.assertTrue(set(d['named_segments']) <= set(RESIDUAL))
        self.assertIn('t_req', d['named_segments'])
        self.assertTrue(d['unplaced'])

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

    def test_unstable_share_is_inconclusive_not_mixed(self):
        # Six blocks of 60 requests, a 20 us rise split between the sender's queue and the reply's application
        # segment, with wider block variation: an interval that does not exclude a zero share gets no label.
        for seed in (60, 64):
            out, d = mech(pair(seed, a_kw=dict(add=dict(s_queue=20, r_app=20)), n=60, variation=0.1))
            self.assertEqual(out, 'inconclusive', seed)

    def test_prerequisite_and_consistency_gates(self):
        blocks = pair(1, a_kw=dict(add=dict(s_queue=300)))
        for state in ('instrumentation gap', 'evidence gap'):
            out, d = C.mechanism_reading(blocks, WIDE, dict(state=state, reasons=['x']), SEGMENTS, QUEUE_SEGMENTS)
            self.assertEqual(out, state)
        out, d = C.mechanism_reading(blocks, (0.5, 0.9), PASS, SEGMENTS, QUEUE_SEGMENTS)
        self.assertEqual(out, 'inconclusive')
        self.assertIn('interval', d['reason'])
        self.assertEqual(mech(blocks[:4])[0], 'evidence gap')
        self.assertEqual(C.mechanism_reading(blocks, None, PASS, SEGMENTS, QUEUE_SEGMENTS)[0], 'evidence gap')

    def test_counterfactual_identity(self):
        rng = random.Random(11)
        a = rows(rng)
        self.assertEqual(C.counterfactual_p95(a, a, ['s_queue', 'r_queue']), C.pooled_p95([r['total'] for r in a]))


class Missing(unittest.TestCase):
    def test_reversal_by_missing_slow_chains_is_not_labelled(self):
        # Every A request carries 100 us more sender queue wait, and 6% carry 600 us more in the reply's
        # application segment, which sets the full-sample p95: with every chain present the reading is mixed.
        # Five in six of those slow requests lose their chains (about 5% of requests, as usability allows). On
        # complete rows alone the rise looks like queue wait; with the missing requests' latencies kept, no
        # placement-independent label survives.
        kw = dict(add=dict(s_queue=100), spikes=('r_app', 600, 0.06))
        self.assertEqual(mech(pair(70, a_kw=kw))[0], 'mixed')  # the truth, every chain present
        blocks = pair(70, a_kw=kw, drop_a=lambda r, rng: r.get('spiked') and rng.random() < 5 / 6)
        complete_only = [dict(b, a_missing=[], b_missing=[]) for b in blocks]
        self.assertEqual(mech(complete_only)[0], 'queue wait')  # the censored analysis the rule must not follow
        out, d = mech(blocks)
        self.assertEqual(out, 'inconclusive')
        self.assertGreater(d['missing']['a'], 0)

    def test_random_missingness_keeps_a_strong_label(self):
        out, d = mech(pair(71, a_kw=dict(add=dict(s_queue=300)), drop_a=lambda r, rng: rng.random() < 0.02,
                           drop_b=lambda r, rng: rng.random() < 0.02))
        self.assertEqual(out, 'queue wait')

    def test_missingness_at_each_endpoint_widens_its_envelope(self):
        for seg in ('s_queue', 'r_queue'):
            blocks = pair(72, a_kw=dict(add={seg: 150, 'r_app': 150}),
                          drop_a=lambda r, rng: r[seg] > (150 + 2 * BASE[seg]) * US and rng.random() < 0.5)
            out, d = mech(blocks)
            self.assertIsNotNone(d['queue_share']['imputations'], seg)
            ci = [v['ci'] for v in d['queue_share']['imputations'].values()]
            self.assertEqual(d['queue_share']['ci'], [min(c[0] for c in ci), max(c[1] for c in ci)])


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
        # No depth effect: non-increase in most seeds. Six clusters make the block bootstrap anti-conservative:
        # over 200 seeds (ctlrules_oc.py) the null reads "depth raises latency" about 4.5% of the time.
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
        narrow = plain(23, 0.16, 0.16)
        wide = [dict(b, aa=[x * random.Random(i).uniform(0.5, 2.0) for x in b['aa']]) for i, b in enumerate(narrow)]
        self.assertEqual(C.depth_reading(narrow)[0], C.depth_reading(wide)[0])
        self.assertEqual(C.depth_reading(narrow)[1]['ci'], C.depth_reading(wide)[1]['ci'])

    def test_wide_aa_blocks_raises(self):
        b = plain(24, 0.19, 0.16, aa=0.26)
        out, d = C.depth_reading(b)
        self.assertGreater(d['ci'][0], 1)
        self.assertNotEqual(out, 'depth raises latency')

    def test_evidence_gap(self):
        self.assertEqual(C.depth_reading(plain(25, 0.26, 0.16, blocks=4))[0], 'evidence gap')
        self.assertEqual(C.depth_reading(plain(25, 0.26, 0.16, n=90))[0], 'evidence gap')
        self.assertEqual(C.depth_reading([])[0], 'evidence gap')


class Perturbation(unittest.TestCase):
    def blocks(self, g_ratio, p_ratio, n=6):
        rng = random.Random(30)
        out = []
        for _ in range(n):
            b = {}
            for rev in ('cand', 'cand8'):
                b[f'{rev}_goodput'] = 5000.0
                b[f'{rev}_ctl_goodput'] = 5000.0 * g_ratio
                b[rev] = [rng.uniform(0.1, 0.2) for _ in range(120)]
                b[f'{rev}_ctl'] = [x * p_ratio for x in b[rev]]
            out.append(b)
        return out

    def test_states(self):
        self.assertEqual(C.perturbation(self.blocks(0.995, 1.05))['state'], 'pass')
        r = C.perturbation(self.blocks(0.985, 1.0))
        self.assertEqual(r['state'], 'fail')
        self.assertFalse(r['cand']['goodput_passes'])
        r = C.perturbation(self.blocks(1.0, 1.2))
        self.assertEqual(r['state'], 'fail')
        self.assertFalse(r['cand']['p95_passes'])
        # Too few blocks is insufficient evidence, not a measured failure.
        self.assertEqual(C.perturbation(self.blocks(0.9, 1.5, n=4))['state'], 'insufficient')
        self.assertEqual(C.perturbation([])['state'], 'insufficient')


class Bound(unittest.TestCase):
    GOOD = dict(cand=600e6, cand8=500e6)  # useful bytes per second
    KAPPA = dict(cand=1.04, cand8=1.04)

    def test_sender_bound_between_depths(self):
        rng = random.Random(40)
        a = rows(rng, n=600, sender_bytes=(30, 1400))
        b = rows(rng, n=600, sender_bytes=(7, 1400))
        bound, d = C.stage2_bound(a, b, 'sender', self.GOOD, self.KAPPA)
        self.assertIsNotNone(bound)
        self.assertEqual(bound % 1400, 0)
        self.assertGreater(bound, d['b8'])
        self.assertLess(bound, d['b9'])

    def test_goodput_moves_the_bound(self):
        rng = random.Random(42)
        a = rows(rng, n=600, sender_bytes=(30, 1400))
        b = rows(rng, n=600, sender_bytes=(7, 1400))
        lo = C.stage2_bound(a, b, 'sender', dict(cand=600e6, cand8=300e6), self.KAPPA)[1]['time_ahead_s']
        hi = C.stage2_bound(a, b, 'sender', dict(cand=600e6, cand8=600e6), self.KAPPA)[1]['time_ahead_s']
        self.assertNotAlmostEqual(lo, hi)

    def test_receiver_granularity_uses_its_entry_size(self):
        rng = random.Random(43)
        # 31 against 7 ACK-only entries of 60 bytes: a bound in whole 60-byte entries fits between them.
        a, b = rows(rng, receiver_bytes=(31, 60)), rows(rng, receiver_bytes=(7, 60))
        bound, d = C.stage2_bound(a, b, 'receiver', self.GOOD, dict(cand=0.02, cand8=0.02))
        self.assertIsNotNone(bound)
        self.assertEqual(bound % 60, 0)
        self.assertTrue(d['b8'] < bound < d['b9'])

    def test_empty_receiver_queue_is_not_attempted(self):
        rng = random.Random(41)
        a, b = rows(rng, receiver_bytes=(0, 60)), rows(rng, receiver_bytes=(0, 60))
        bound, d = C.stage2_bound(a, b, 'receiver', self.GOOD, self.KAPPA)
        self.assertIsNone(bound)
        self.assertEqual(d['outcome'], 'not attempted')

    def test_invalid_goodput_is_not_attempted(self):
        rng = random.Random(44)
        a, b = rows(rng, sender_bytes=(30, 1400)), rows(rng, sender_bytes=(7, 1400))
        self.assertIsNone(C.stage2_bound(a, b, 'sender', dict(cand=0, cand8=500e6), self.KAPPA)[0])

    def test_common_bound(self):
        s = (22400, dict(interval=[11200, 40600]))
        r = (900, dict(interval=[480, 1800]))
        self.assertEqual(C.common_bound(dict(sender=s, receiver=r), ['sender']), (22400, 'attempted'))
        self.assertEqual(C.common_bound(dict(sender=s, receiver=r), ['receiver']), (900, 'attempted'))
        b, why = C.common_bound(dict(sender=s, receiver=r), ['receiver', 'sender'])
        self.assertIsNone(b)
        self.assertIn('no common bound', why)
        b, why = C.common_bound(dict(sender=s, receiver=(None, dict(interval=[0, 0]))), ['receiver', 'sender'])
        self.assertIsNone(b)
        overlap = (12000, dict(interval=[1000, 30000]))
        self.assertEqual(C.common_bound(dict(sender=s, receiver=overlap), ['receiver', 'sender']), (22400, 'attempted'))


class Stage2(unittest.TestCase):
    def lat(self, new, cand=0.26, aa=None, noise=0.02, seed=50, sigma=0.42):
        rng = random.Random(seed)
        out = []
        for _ in range(6):
            s = rng.uniform(1 - noise, 1 + noise)
            out.append(dict(cand=latencies(rng, 120, cand * s, sigma), aa=latencies(rng, 120, (aa or cand) * s, sigma),
                            new=latencies(rng, 120, new * s, sigma)))
        return out

    def test_null_margin(self):
        self.assertAlmostEqual(C.stage2_null_margin(1.10), 1.05)
        self.assertAlmostEqual(C.stage2_null_margin(1.60), 1.15)

    def test_efficacy_endings(self):
        m = C.stage2_null_margin(1.30)
        self.assertEqual(C.stage2_latency(self.lat(0.18), m)[0], 'effective')
        self.assertEqual(C.stage2_latency(self.lat(0.34), m)[0], 'demonstrated worsening')
        self.assertEqual(C.stage2_latency(self.lat(0.26, sigma=0.2), m)[0], 'null')
        # A wide interval such as 0.80-1.15 is inconclusive or partial, never null.
        out, d = C.stage2_latency(self.lat(0.25, noise=0.30, seed=51), m)
        self.assertNotEqual(out, 'null')
        self.assertEqual(C.stage2_latency(self.lat(0.18)[:4], m)[0], 'evidence gap')

    def test_small_increment_margin_resists_null(self):
        # With a small Stage 1 increment the margin is narrow: equality with r9 at ordinary noise is not null.
        out, d = C.stage2_latency(self.lat(0.26, seed=53), C.stage2_null_margin(1.04))
        self.assertNotEqual(out, 'null')

    def test_partial(self):
        out, d = C.stage2_latency(self.lat(0.22, aa=0.16, seed=52), C.stage2_null_margin(1.3))
        self.assertEqual(out, 'partial')

    def guards(self, gl='inside', cpu='inside', integrity=True, pres='inside', screen='no regression'):
        mv = lambda m: dict(movement=m)
        loop = {w: dict(goodput=mv(gl), cpu=mv(cpu)) for w in ('stream', 'datagram')}
        return C.guard_states(loop, integrity, dict(send=pres, receive=pres), {w: dict(reading=screen) for w in ('stream', 'datagram')})

    def test_outcomes(self):
        G = self.guards
        self.assertEqual(C.stage2_outcome('inert', 'effective', G()), ('inert', False, ['inert']))
        self.assertEqual(C.stage2_outcome(None, 'null', G())[:2], ('null', True))
        self.assertEqual(C.stage2_outcome(None, 'demonstrated worsening', G())[:2], ('demonstrated worsening', True))
        self.assertEqual(C.stage2_outcome(None, 'partial', G())[:2], ('partial', False))
        self.assertEqual(C.stage2_outcome(None, 'inconclusive', G())[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome(None, 'evidence gap', G())[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome(None, 'effective', G())[:2], ('kept', False))
        for kw in (dict(gl='lower'), dict(cpu='higher'), dict(integrity=False), dict(pres='above'), dict(pres='below'),
                   dict(screen='regression')):
            self.assertEqual(C.stage2_outcome(None, 'effective', G(**kw))[:2], ('effective, guard-failed', True), kw)
        for kw in (dict(screen='inconclusive'), dict(gl='unusable'), dict(integrity=None), dict(pres='insufficient')):
            self.assertEqual(C.stage2_outcome(None, 'effective', G(**kw))[:2], ('inconclusive', False), kw)

    def test_fail_closed(self):
        # Empty or partial evidence never keeps, and an invalid state never disposes.
        self.assertEqual(C.stage2_outcome(None, 'effective', C.guard_states({}, True, {}, {}))[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome(None, 'effective', {})[:2], ('inconclusive', False))
        partial = self.guards()
        del partial['datagram cpu']
        self.assertEqual(C.stage2_outcome(None, 'effective', partial)[:2], ('inconclusive', False))
        odd = self.guards(pres='unusable')
        self.assertEqual(C.stage2_outcome(None, 'effective', odd)[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome('maybe', 'effective', self.guards())[:2], ('inconclusive', False))
        self.assertEqual(C.stage2_outcome(None, 'great', self.guards())[:2], ('inconclusive', False))
        # A demonstrated regression with another guard insufficient still disposes, and records both.
        out, disposes, reasons = C.stage2_outcome(None, 'effective', self.guards(gl='lower', screen='inconclusive'))
        self.assertEqual((out, disposes), ('effective, guard-failed', True))
        self.assertTrue(any('also insufficient' in r for r in reasons))

    def test_range_movement(self):
        self.assertEqual(C._range_movement([(0.9, 0.99), (0.91, 1.01), (0.9, 1.0), (0.92, 0.98), (0.9, 1.02)])['movement'], 'lower')
        self.assertEqual(C._range_movement([(1.0, 0.99)] * 4)['movement'], 'unusable')


if __name__ == '__main__':
    unittest.main()
