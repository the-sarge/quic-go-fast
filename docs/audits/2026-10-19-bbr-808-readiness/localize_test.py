#!/usr/bin/env python3
"""Synthetic cases for localize.py, run before any #712 attribution observation existed."""
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import localize as L  # noqa: E402

SYSCALL = ['k:do_syscall_64', 'k:entry_SYSCALL_64_after_hwframe', 'internal/runtime/syscall/linux.Syscall6']


def groups(**kw):
    return {g: float(kw.get(g, 0)) for g in L.GROUPS}


class Classify(unittest.TestCase):
    def test_kernel_regions(self):
        send = ['k:memcpy_orig', 'k:udp_send_skb', 'k:udp_sendmsg', 'k:__sys_sendmsg'] + SYSCALL
        self.assertEqual(L.classify_stack(send), 'kernel_send')
        wake_in_send = ['k:try_to_wake_up', 'k:__wake_up_sync_key', 'k:sock_def_readable', 'k:udp_rcv', 'k:net_rx_action',
                        'k:udp_sendmsg', 'k:__sys_sendmsg'] + SYSCALL
        self.assertEqual(L.classify_stack(wake_in_send), 'kernel_wake_sched')
        epoll = ['k:finish_task_switch', 'k:__schedule', 'k:ep_poll', 'k:do_epoll_wait'] + SYSCALL + ['runtime.netpoll']
        self.assertEqual(L.classify_stack(epoll), 'kernel_wake_sched')
        recv = ['k:copy_to_user', 'k:udp_recvmsg', 'k:do_recvmmsg', 'k:__sys_recvmmsg'] + SYSCALL
        self.assertEqual(L.classify_stack(recv), 'kernel_recv')
        fault = ['k:clear_page_erms', 'k:do_anonymous_page', 'k:asm_exc_page_fault', 'runtime.memclrNoHeapPointers', 'runtime.mallocgc']
        self.assertEqual(L.classify_stack(fault), 'kernel_other')

    def test_user_precedence(self):
        bbr_alloc = ['runtime.mallocgc', 'runtime.newobject',
                     'github.com/quic-go/quic-go/internal/ackhandler.(*sentPacketHandler).captureCongestionSend']
        self.assertEqual(L.classify_stack(bbr_alloc), 'alloc_gc')
        self.assertEqual(L.classify_stack(['runtime.findRunnable', 'runtime.schedule', 'runtime.park_m']), 'user_sched')
        bbr = ['runtime.mapassign_fast64', 'github.com/quic-go/quic-go/internal/ackhandler.(*sentPacketHandler).captureCongestionSend']
        self.assertEqual(L.classify_stack(bbr), 'bbr')
        self.assertEqual(L.classify_stack(['github.com/quic-go/quic-go/internal/congestion.(*BBRSender).Feedback']), 'bbr')
        self.assertEqual(L.classify_stack(['github.com/quic-go/quic-go.(*localSendCredit).reserve']), 'bbr')
        self.assertEqual(L.classify_stack(['crypto/internal/fips140/aes/gcm.gcmAesEnc', 'github.com/quic-go/quic-go.(*packetPacker).appendPacket']),
                         'user_other')
        self.assertEqual(L.classify_stack([]), 'user_other')

    def test_fold_parses_perf_script(self):
        text = ('         5 \n\tffffffffa1c9e124 finish_task_switch.isra.0\n\tffffffffa2f27863 __schedule\n'
                '\t          44b253 runtime.netpoll\n\n'
                '         7 \n\t          4ab000 github.com/quic-go/quic-go/internal/congestion.(*BBRSender).Feedback+0x12\n\n'
                '         3 \n\t          4ab000 github.com/quic-go/quic-go/internal/congestion.(*BBRSender).Feedback\n')
        with mock.patch.object(L.subprocess, 'run', return_value=mock.Mock(stdout=text)):
            folded = L.fold('x')
        self.assertEqual(folded['k:finish_task_switch;k:__schedule;runtime.netpoll'], 5)
        self.assertEqual(folded['github.com/quic-go/quic-go/internal/congestion.(*BBRSender).Feedback'], 10)
        g = L.group_cycles(folded)
        self.assertEqual((g['kernel_wake_sched'], g['bbr']), (5, 10))


class Localize(unittest.TestCase):
    def block(self, cand, reno=None):
        return dict(cand=groups(**cand), reno=groups(**(reno or {})))

    def test_clear_lead(self):
        blocks = [self.block(dict(kernel_send=7, bbr=3)), self.block(dict(kernel_send=8, bbr=2)), self.block(dict(kernel_send=6, bbr=4))]
        r = L.localize(blocks)
        self.assertEqual((r['outcome'], r['lead']), ('localized', 'kernel_send'))

    def test_no_group_reaches_half(self):
        blocks = [self.block(dict(kernel_send=4, bbr=3, user_sched=3))] * 3
        self.assertTrue(L.localize(blocks)['outcome'].startswith('inconclusive: no group'))

    def test_negative_delta_excluded_from_pool(self):
        b = L.block_excess(groups(kernel_send=6, bbr=2), groups(user_other=4))
        self.assertEqual(b['positive_pool'], 8)
        self.assertEqual(b['share']['kernel_send'], 0.75)
        self.assertEqual(b['share']['user_other'], 0.0)
        self.assertEqual(b['total'], 4)

    def test_missing_groups_are_zero(self):
        b = L.block_excess({'bbr': 5.0}, {})
        self.assertEqual(b['delta']['kernel_send'], 0.0)
        self.assertEqual(b['share']['bbr'], 1.0)

    def test_no_repeatable_excess(self):
        blocks = [self.block(dict(bbr=1), dict(bbr=2)), self.block(dict(bbr=1), dict(bbr=3)), self.block(dict(bbr=5))]
        self.assertEqual(L.localize(blocks)['outcome'], 'no repeatable excess')

    def test_lead_must_be_positive_every_block(self):
        blocks = [self.block(dict(bbr=9, kernel_send=1)), self.block(dict(bbr=9, kernel_send=1)),
                  self.block(dict(kernel_send=5), dict(bbr=1))]
        self.assertTrue(L.localize(blocks)['outcome'].startswith('inconclusive: no group'))

    def test_domain_conflict_and_disagreement(self):
        blocks = [self.block(dict(bbr=8, kernel_send=2))] * 3
        r = L.localize(blocks, dict(cycles_user_share=0.2, instructions_user_share=0.8))
        self.assertTrue(r['outcome'].startswith('inconclusive: call-graph lead and counter domain conflict'))
        self.assertTrue(r['instructions_cycles_disagree'])
        r = L.localize(blocks, dict(cycles_user_share=0.7, instructions_user_share=0.3))
        self.assertEqual(r['outcome'], 'localized')
        self.assertTrue(r['instructions_cycles_disagree'])


class Rules(unittest.TestCase):
    def counters(self, iu, ik, cu, ck):
        c = {e: dict(value=1.0, running_pct=100.0) for e in L.COUNTER_EVENTS}
        c.update({'instructions:u': dict(value=iu, running_pct=100.0), 'instructions:k': dict(value=ik, running_pct=100.0),
                  'cycles:u': dict(value=cu, running_pct=100.0), 'cycles:k': dict(value=ck, running_pct=100.0)})
        return c

    def test_counters_block(self):
        r = L.counters_block(self.counters(12, 10, 10, 13), self.counters(10, 10, 10, 10), 1.0, 1.0)
        self.assertAlmostEqual(r['instructions'], 1.1)
        self.assertEqual(r['instructions_user_share'], 1.0)
        self.assertEqual(r['cycles_user_share'], 0.0)
        self.assertAlmostEqual(r['cycles'], 1.15)

    def test_counter_quality(self):
        c = self.counters(1, 1, 1, 1)
        self.assertTrue(L.counter_quality(c))
        c['cycles:k'] = dict(value=1.0, running_pct=60.0)
        self.assertFalse(L.counter_quality(c))
        c['cycles:k'] = dict(value=None, running_pct=None)
        self.assertFalse(L.counter_quality(c))

    def test_preservation(self):
        self.assertTrue(L.preservation([1.0, 1.01, 0.99], 1.05, [0.95, 1.08])['attribution'].startswith('measurement noise'))
        self.assertEqual(L.preservation([1.0, 1.01, 0.99], 1.15, [0.95, 1.08])['attribution'], 'unattributed preservation flag')
        self.assertEqual(L.preservation([1.05, 1.06, 1.04], 1.05, [0.95, 1.08])['attribution'], 'unattributed preservation flag')

    def sample(self, t, busy, ticks):
        cpus = {str(c): dict(busy=busy.get(c, 0), softirq=0) for c in L.FIXTURE_CPUS + L.SIBLING_CPUS}
        return dict(unix_ns=t * 10**9, cpus=cpus, fixture_ticks=dict(send=ticks, receive=0, relay=0))

    def test_contamination(self):
        clean = [self.sample(t, {8: 50 * t}, 50 * t) for t in range(10)]
        self.assertFalse(L.contamination(clean, [0, 9 * 10**9])['contaminated'])
        steady = [self.sample(t, {8: 50 * t, 24: 20 * t}, 50 * t) for t in range(10)]
        r = L.contamination(steady, [0, 9 * 10**9])
        self.assertTrue(r['contaminated'])
        self.assertAlmostEqual(r['sibling_mean_cores'], 0.2)
        spike = [self.sample(t, {8: 50 * t + (60 if t >= 5 else 0)}, 50 * t) for t in range(10)]
        r = L.contamination(spike, [0, 9 * 10**9])
        self.assertTrue(r['contaminated'] and r['foreign_mean_cores'] < 0.10)


if __name__ == '__main__':
    unittest.main()
