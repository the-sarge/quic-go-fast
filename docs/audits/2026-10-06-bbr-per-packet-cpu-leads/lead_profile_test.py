#!/usr/bin/env python3
"""Synthetic cases for lead_profile.py's lead selection, written and passing before any profile observation."""
import unittest

import lead_profile as pf

Q = 'github.com/quic-go/quic-go'
A = f'{Q}/internal/ackhandler'


def blk(sites, total=30e9, overlaps=None):
    overlaps = overlaps or {}
    return dict(sites=sites, total=total, overlap=lambda s, sel: sum(overlaps.get((s, x), 0.0) for x in sel))


class Pattern(unittest.TestCase):
    def test_bbr_only(self):
        yes = [f'{Q}.(*packetEmission).sendBounded', f'{Q}.(*packetEmission).enableBBR.func1', f'{Q}.(*packetEmission).enableBBRECN.func1',
               f'{Q}.(*localSendCredit).reserve', f'{Q}.(*sendReservation).resize', f'{Q}.(*bbrSendPolicy).budget', f'{Q}.ceilSendDelay',
               f'{Q}.(*pacingKick).arm', f'{A}.(*sentPacketHandler).captureCongestionSend', f'{A}.(*recoveryEvidence).lowerBound',
               f'{Q}/internal/congestion.(*BBRSender).OnPacketAcked', f'{A}.(*bbrECNTracker).onAck']
        no = [f'{Q}.(*Conn).run', f'{Q}.(*packetEmission).sendPackets', f'{Q}.(*packetPacker).appendPacket', 'runtime.selectgo',
              f'{Q}/internal/congestion.(*cubicSender).OnPacketAcked', f'{A}.(*sentPacketHandler).ReceivedAck', 'k:udp_sendmsg']
        for f in yes:
            self.assertTrue(pf.bbr_only(f), f)
        for f in no:
            self.assertFalse(pf.bbr_only(f), f)


class Selection(unittest.TestCase):
    def test_ranked_selection_stops_at_three(self):
        bl = [blk(dict(a=1.0e9, b=.8e9, c=.6e9, d=.5e9)) for _ in range(4)]
        r = pf.select_leads(bl, {k: (True, 'x') for k in 'abcd'})
        self.assertEqual(r['selected'], ['a', 'b', 'c'])

    def test_floor_ends_selection(self):
        bl = [blk(dict(a=1.0e9, b=.1e9)) for _ in range(4)]
        r = pf.select_leads(bl, {k: (True, 'x') for k in 'ab'})
        self.assertEqual(r['selected'], ['a'])
        self.assertIn('below the floor', r['decisions'][-1][1])

    def test_container_skipped(self):
        bl = [blk(dict(run=9e9, a=.5e9), total=30e9) for _ in range(4)]
        r = pf.select_leads(bl, {k: (True, 'x') for k in ['run', 'a']})
        self.assertEqual(r['selected'], ['a'])
        self.assertEqual(r['decisions'][0], ('run', 'container'))

    def test_absent_in_a_block_skipped(self):
        bl = [blk(dict(a=1e9, b=.5e9)), blk(dict(a=1e9, b=.5e9)), blk(dict(a=1e9, b=.5e9)), blk(dict(a=1e9))]
        r = pf.select_leads(bl, {k: (True, 'x') for k in 'ab'})
        self.assertEqual(r['selected'], ['a'])

    def test_overlap_skipped_child_after_ineligible_parent_kept(self):
        # parent p is ineligible (too broad); child c lies under p but p is not selected, so c is not an overlap.
        # d lies mostly under the selected c and is skipped.
        ov = {('d', 'c'): .45e9}
        bl = [blk(dict(p=2e9, c=1e9, d=.5e9), overlaps=ov) for _ in range(4)]
        r = pf.select_leads(bl, {'p': (False, 'too broad'), 'c': (True, 'x'), 'd': (True, 'x')})
        self.assertEqual(r['selected'], ['c'])
        self.assertTrue(r['decisions'][2][1].startswith('overlaps'))

    def test_unjudged_pauses(self):
        bl = [blk(dict(a=1e9, b=.5e9)) for _ in range(4)]
        r = pf.select_leads(bl, {'a': (True, 'x')})
        self.assertEqual(r['selected'], ['a'])
        self.assertIn('pauses', r['decisions'][-1][1])


if __name__ == '__main__':
    unittest.main()
