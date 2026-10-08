#!/usr/bin/env python3
"""Orchestration tests for memstages.py and follow.py on fabricated observations (no measurement data).

Covers block selection with completeness, missing and malformed observations, calibration from clean blocks
only, the perturbation gate, S6 input completeness, endings for every expected cell, and the rerun caps.
"""
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import memrules as M
import memstages as MS
import follow as F

MIB = 2**20


def row(t_s, rss_mib, objects_mib, live_mib, ring=0, sent=0, lost=0):
    r = {k: 0 for k in M.REQUIRED}
    r.update({'unix_ns': int(t_s * 1e9), 'rollup_Rss_kb': int(rss_mib * 1024), 'rollup_Anonymous_kb': int((rss_mib - 2) * 1024),
              'status_VmHWM_kb': int(rss_mib * 1024), '/memory/classes/heap/objects:bytes': int(objects_mib * MIB),
              '/gc/heap/live:bytes': int(live_mib * MIB), '/memory/classes/heap/stacks:bytes': int((rss_mib - 2 - objects_mib) * MIB),
              'ring_bytes': ring, 'ring_entries': 32768 if ring else 0, 'stats_packets_sent': sent, 'stats_packets_lost': lost})
    return r


def obs(arm, block, workload='stream', rss=(20.0, 20.0), objects=(4.0, 4.0), live=(2.0, 2.0), goodput=90.0, ring=1_200_000,
        override=None, usable=True, ring_entries=None):
    """One fabricated, loaded observation (as memstages.observe returns it)."""
    if not usable:
        return dict(dir=f'/x/{arm}-{block}', arm=MS.ARMS[arm], block=block, workload=workload, path='S5', usable=False, error='fabricated')
    cfg = dict(start_unix_ns=0, warmup_ms=10_000, measure_ms=30_000, workload=workload)
    out = dict(dir=f'/x/{arm}-{block}', arm=MS.ARMS[arm], block=block, workload=workload, path='S5', usable=True, goodput=goodput, cfg=cfg,
               summary=dict(id=f'{arm}-{block}', control_p95_ms=100.0))
    for i, role in enumerate(['send', 'receive']):
        r_ring = ring if (arm != 'reno' and not (arm == 'rx' and role == 'receive')) else 0
        rows = [row(s, rss[i], objects[i], live[i], ring=r_ring, sent=1000 * s, lost=s) for s in range(0, 41)]
        if ring_entries is not None:
            for r in rows:
                r['ring_entries'] = ring_entries
        peak = M.peak_account(rows, rss[i])
        out[role] = dict(peak=peak, rows=rows, tail=dict(controller_override=override if role == 'receive' else None, phases=[]))
    out['traffic'] = dict(goodput=goodput, control=30, fwd_per_gib=1000.0, feedback_per_gib=1000.0, lost_per_gib=1.0, overflow=0.002)
    return out


def full_block(b, cand_rss=24.0, reno_rss=20.0, aa_rss=24.1, rx_recv_rss=None, ring_rss=None, **kw):
    """All five S5 arms of one block: the receiver excess is E = cand_rss - reno_rss."""
    g = {'reno': obs('reno', b, rss=(reno_rss, reno_rss), objects=(1.0, 1.0), live=(0.5, 0.5)),
         'cand': obs('cand', b, rss=(cand_rss, cand_rss), objects=(4.0, 4.0), live=(2.0, 2.0)),
         'aa': obs('aa', b, rss=(aa_rss, aa_rss), objects=(4.0, 4.0), live=(2.0, 2.0)),
         'ring': obs('ring', b, rss=(ring_rss or cand_rss - 1, ring_rss or cand_rss - 1), objects=(3.0, 3.0), live=(1.5, 1.5),
                     ring=150_000, ring_entries=4096),
         'rx': obs('rx', b, rss=(cand_rss, rx_recv_rss or cand_rss), objects=(4.0, 4.0), live=(2.0, 2.0), override='reno')}
    g.update(kw.get('override_arms', {}))
    return {('stream', b): g}


FLAGS = {('S5', 'stream', 'send'): dict(value=1.2, raised=True), ('S5', 'stream', 'receive'): dict(value=1.2, raised=True),
         ('S5', 'datagram', 'send'): dict(value=1.2, raised=True)}


def assemble(grouped, status=None, perturbed=None, ring_status='run'):
    status = status or {k: 'clean' for k in grouped}
    return {(c['workload'], c['role']): c for c in MS.assemble('S5', grouped, status, FLAGS, ring_status, perturbed or {})}


class Assemble(unittest.TestCase):
    def blocks(self, n=4, **kw):
        g = {}
        for b in range(1, n + 1):
            g.update(full_block(b, **kw))
        return g

    def test_complete_matrix_labels_every_raised_cell(self):
        out = assemble(self.blocks())
        self.assertIsNotNone(out[('stream', 'send')]['label'])
        self.assertIsNotNone(out[('stream', 'receive')]['label'])
        # A raised cell with no observations still ends with a registered label.
        self.assertEqual(out[('datagram', 'send')]['label']['label'], 'evidence gap (stage not run or no observation)')
        self.assertIsNone(out[('datagram', 'receive')]['label'])

    def test_missing_arm_makes_block_unusable(self):
        g = self.blocks()
        del g[('stream', 2)]['aa']
        cell = assemble(g)[('stream', 'receive')]
        b2 = next(b for b in cell['blocks'] if b['block'] == 2)
        self.assertFalse(b2['usable'])
        self.assertEqual(b2['missing'], ['aa'])

    def test_failed_observation_makes_block_unusable(self):
        g = self.blocks()
        g[('stream', 3)]['rx'] = obs('rx', 3, usable=False)
        cell = assemble(g)[('stream', 'receive')]
        self.assertFalse(next(b for b in cell['blocks'] if b['block'] == 3)['usable'])

    def test_two_bad_blocks_leave_an_evidence_gap(self):
        g = self.blocks()
        status = {k: 'clean' for k in g}
        status[('stream', 1)] = status[('stream', 2)] = 'contaminated'
        self.assertTrue(assemble(g, status)[('stream', 'receive')]['label']['label'].startswith('evidence gap'))

    def test_rejected_aa_outlier_cannot_change_calibration(self):
        # Consideration item 1: a contaminated block with a 50% A/A goodput outlier must not widen the band.
        g = self.blocks()
        status = {k: 'clean' for k in g}
        g[('stream', 4)]['aa']['traffic'] = dict(g[('stream', 4)]['aa']['traffic'], goodput=45.0)
        status[('stream', 4)] = 'contaminated'
        band = assemble(g, status)[('stream', 'receive')]['band']
        self.assertAlmostEqual(band['goodput'], M.COMPARABLE_FLOOR)
        clean = assemble(self.blocks(n=3))[('stream', 'receive')]['band']
        self.assertEqual(band, clean)

    def test_uncalibrated_band(self):
        g = self.blocks(n=2)
        cell = assemble(g)[('stream', 'receive')]
        self.assertIsNone(cell['band'])

    def test_receiver_controller_needs_engagement(self):
        g = self.blocks()
        for b in range(1, 5):
            g[('stream', b)]['rx'] = obs('rx', b, rss=(24.0, 24.0), override=None)  # receiver kept BBR
        cell = assemble(g)[('stream', 'receive')]
        self.assertTrue(all(not b['rx_comparable'] for b in cell['blocks']))

    def test_perturbation_gate(self):
        out = assemble(self.blocks(), perturbed={('S5', 'stream'): dict(ratio=0.98)})
        self.assertEqual(out[('stream', 'receive')]['label']['label'], MS.GAP_PERTURBATION)
        self.assertEqual(out[('stream', 'send')]['label']['label'], MS.GAP_PERTURBATION)

    def test_ring_not_expected_when_not_run(self):
        self.assertEqual(MS.expected_arms('S5', 'not run (below detectability)'), ['reno', 'cand', 'aa', 'rx'])
        self.assertEqual(MS.expected_arms('loopback', 'run'), ['reno', 'cand', 'aa', 'rx'])
        self.assertEqual(MS.expected_arms('S6', 'run'), ['reno', 'cand', 'aa', 'ring', 'rx'])


class Selection(unittest.TestCase):
    """choose(): completeness, missing blocks and the rerun rule, on temporary observation roots."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        self.roots = [root / 'observations', root / 'observations-rerun']
        for r in self.roots:
            r.mkdir()
        self.patches = [mock.patch.object(MS.S, 'ROOTS', self.roots),
                        mock.patch.object(MS.S, 'contaminated', lambda d: dict(contaminated=(d / 'contaminated').exists())),
                        mock.patch.object(MS.S, 'usable', lambda d: not (d / 'bad').exists())]
        for p in self.patches:
            p.start()

    def tearDown(self):
        for p in self.patches:
            p.stop()
        self.tmp.cleanup()

    def make(self, root, workload, b, arm, receipt=True, bad=False, contaminated=False):
        d = self.roots[root] / f'mem-S5-{workload}-p{b}-{arm}'
        d.mkdir()
        (d / 'meta.json').write_text(json.dumps(dict(phase='mem', path='S5', pair=b)))
        (d / 'config.json').write_text(json.dumps(dict(workload=workload)))
        if receipt:
            (d / 'receipt.json').write_text('{}')
        if bad:
            (d / 'bad').write_text('')
        if contaminated:
            (d / 'contaminated').write_text('')

    def test_missing_incomplete_and_rerun(self):
        arms = ['reno', 'cand', 'aa', 'rx']
        for b in (1, 2, 3):
            for a in arms:
                self.make(0, 'stream', b, a, receipt=not (b == 2 and a == 'aa'), contaminated=(b == 3 and a == 'cand'))
        for a in arms:  # block 3's same-seed rerun is clean
            self.make(1, 'stream', 3, a)
        _, rec = MS.choose('mem', 'S5', 4)
        status = {(r['workload'], r['block']): r for r in rec}
        self.assertEqual(status[('stream', 1)]['status'], 'clean')
        self.assertEqual(status[('stream', 2)]['status'], 'unusable')        # an arm left no receipt
        self.assertEqual(status[('stream', 3)]['counted'], 'rerun')
        self.assertEqual(status[('stream', 4)]['status'], 'missing')
        self.assertEqual(status[('datagram', 1)]['status'], 'missing')
        with mock.patch.object(F.MS, 'choose', MS.choose):
            pending = {(r['workload'], r['block']) for r in F.pending_reruns('mem', 'S5', 4)}
        self.assertIn(('stream', 2), pending)
        self.assertIn(('stream', 4), pending)
        self.assertNotIn(('stream', 3), pending)  # already rerun once
        self.assertNotIn(('stream', 1), pending)


class Caps(unittest.TestCase):
    def test_rerun_caps(self):
        calls = []
        pending = [dict(path='S5', workload='stream', block=b, status='contaminated', attempts=[('original', True, True)]) for b in range(1, 9)]

        def counted(roots=None):
            return (len(calls) * 5) if roots else 124 + len(calls) * 5
        with mock.patch.object(F, 'pending_reruns', lambda phase, path, arms: pending if path == 'S5' else []), \
                mock.patch.object(F, 'counted', counted), mock.patch.object(F, 'staged', lambda *a: calls.append(a)), \
                mock.patch.object(F, 'record', lambda *a: None), mock.patch.object(F.MS, 'ring_decision', lambda: 'run'):
            F.reruns()
        self.assertEqual(len(calls), 5)  # 28 rerun observations allow five 5-arm blocks

    def test_total_cap_binds_without_ring(self):
        calls = []
        pending = [dict(path='S5', workload='stream', block=b, status='contaminated', attempts=[('original', True, True)]) for b in range(1, 9)]

        def counted(roots=None):
            return (len(calls) * 4) if roots else 108 + len(calls) * 4
        with mock.patch.object(F, 'pending_reruns', lambda phase, path, arms: pending if path == 'S5' else []), \
                mock.patch.object(F, 'counted', counted), mock.patch.object(F, 'staged', lambda *a: calls.append(a)), \
                mock.patch.object(F, 'record', lambda *a: None), mock.patch.object(F.MS, 'ring_decision', lambda: 'not run (below detectability)'):
            F.reruns()
        self.assertEqual(len(calls), 7)  # 28 // 4 rerun blocks; the total stays at most 136


if __name__ == '__main__':
    unittest.main()
