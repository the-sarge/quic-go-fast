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


WANT = MS.arm_names('mem', 'S5', 'not run')  # reno, cand, aa, rx
ARM_PARTS = {'reno-mem-reno': ('reno-mem', 'reno', ''), 'cand-mem-bbrv3': ('cand-mem', 'bbrv3', ''),
             'cand-mem-bbrv3-aa': ('cand-mem', 'bbrv3', 'aa'), 'cand-mem-bbrv3-rxreno': ('cand-mem', 'bbrv3', 'rxreno')}


class Selection(unittest.TestCase):
    """choose() and pending_reruns() on temporary observation roots. Failure paths use #739's real readers;
    only a valid run's usability, contamination and instrument checks are emulated (by marker files)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        self.roots = [root / 'observations', root / 'observations-rerun']
        for r in self.roots:
            r.mkdir()
        self.real_usable, self.real_cont = MS.S.usable, MS.S.contaminated
        emulate = lambda real, key, fake: (lambda d: fake(d) if (d / key).exists() else real(d))
        self.patches = [mock.patch.object(MS.S, 'ROOTS', self.roots),
                        mock.patch.object(MS.S, 'usable', emulate(self.real_usable, 'valid', lambda d: True)),
                        mock.patch.object(MS.S, 'contaminated', emulate(self.real_cont, 'valid',
                                                                         lambda d: dict(contaminated=(d / 'contaminated').exists()))),
                        mock.patch.object(MS, 'instrument_check', lambda d: (True, None) if (d / 'valid').exists() and not (d / 'sparse').exists()
                                          else (False, 'memory window coverage 3/30 s'))]
        for p in self.patches:
            p.start()

    def tearDown(self):
        for p in self.patches:
            p.stop()
        self.tmp.cleanup()

    def make(self, root, workload, b, arm, valid=True, receipt=True, contaminated=False, sparse=False, meta=True):
        variant, controller, tag = ARM_PARTS[arm]
        d = self.roots[root] / f'mem-S5-{workload}-p{b}-{arm}'
        d.mkdir()
        if meta:
            (d / 'meta.json').write_text(json.dumps(dict(phase='mem', path='S5', pair=b, variant=variant, tag=tag)))
        else:
            (d / 'meta.json').write_text('{not json')
        (d / 'config.json').write_text(json.dumps(dict(workload=workload, controller=controller)))
        if receipt:
            (d / 'receipt.json').write_text('{}')
        for flag, on in (('valid', valid), ('contaminated', contaminated), ('sparse', sparse)):
            if on:
                (d / flag).write_text('')

    def block(self, root, b, workload='stream', **per_arm):
        for arm in WANT:
            self.make(root, workload, b, arm, **per_arm.get(arm, {}))

    def records(self):
        _, rec = MS.choose('mem', 'S5', WANT)
        return {(r['workload'], r['block']): r for r in rec if 'block' in r}, [r for r in rec if 'malformed' in r]

    def test_selection_and_reruns(self):
        self.block(0, 1)
        self.block(0, 2, **{'cand-mem-bbrv3-aa': dict(receipt=False)})                 # an arm left no receipt
        self.block(0, 3, **{'cand-mem-bbrv3': dict(contaminated=True)})                # contaminated original ...
        self.block(1, 3)                                                                # ... clean same-seed rerun
        self.block(0, 4, **{'cand-mem-bbrv3-rxreno': dict(sparse=True)})               # instrument-invalid arm
        status, malformed = self.records()
        self.assertEqual(status[('stream', 1)]['status'], 'clean')
        self.assertEqual(status[('stream', 2)]['status'], 'unusable')
        self.assertEqual(status[('stream', 3)]['counted'], 'rerun')
        self.assertEqual(status[('stream', 4)]['status'], 'unusable')
        self.assertTrue(any('coverage' in r for r in status[('stream', 4)]['reasons']['original']))
        self.assertEqual(status[('datagram', 1)]['status'], 'missing')
        self.assertEqual(malformed, [])
        pending = {(r['workload'], r['block']) for r in F.pending_reruns('mem', 'S5', WANT)}
        self.assertEqual(pending, {('stream', 2), ('stream', 4), ('datagram', 1), ('datagram', 2), ('datagram', 3), ('datagram', 4)})

    def test_arm_identity_not_count(self):
        # Four directories, but the A/A arm is duplicated in place of the receiver-controller arm.
        for arm in ['reno-mem-reno', 'cand-mem-bbrv3', 'cand-mem-bbrv3-aa']:
            self.make(0, 'stream', 1, arm)
        d = self.roots[0] / 'mem-S5-stream-p1-cand-mem-bbrv3-aa-copy'
        d.mkdir()
        (d / 'meta.json').write_text(json.dumps(dict(phase='mem', path='S5', pair=1, variant='cand-mem', tag='aa')))
        (d / 'config.json').write_text(json.dumps(dict(workload='stream', controller='bbrv3')))
        (d / 'receipt.json').write_text('{}')
        (d / 'valid').write_text('')
        status, _ = self.records()
        self.assertEqual(status[('stream', 1)]['status'], 'unusable')

    def test_real_readers_on_failed_and_malformed_artifacts(self):
        # Not emulated: no 'valid' marker, so #739's usability reader runs on a receipt without endpoint results.
        self.block(0, 1, **{a: dict(valid=False) for a in WANT})
        self.make(0, 'stream', 2, 'reno-mem-reno', meta=False)                         # malformed metadata
        status, malformed = self.records()
        self.assertEqual(status[('stream', 1)]['status'], 'unusable')
        self.assertEqual(len(malformed), 1)
        self.assertEqual(status[('stream', 2)]['status'], 'missing')

    def test_latency_reads_only_clean_blocks(self):
        # A selected but unusable latcand block (no send.json) must not be read; the rule sees None.
        for b in range(1, 7):
            for arm, (variant, controller) in {'reno-reno': ('reno', 'reno'), 'cand-bbrv3': ('cand', 'bbrv3')}.items():
                d = self.roots[0] / f'latcand-loopback-long-stream-p{b}-{arm}'
                d.mkdir()
                (d / 'meta.json').write_text(json.dumps(dict(phase='latcand', path='loopback-long', pair=b, variant=variant, tag='')))
                (d / 'config.json').write_text(json.dumps(dict(workload='stream', controller=controller)))
                (d / 'receipt.json').write_text('{}')
        captured = {}
        with mock.patch.object(MS.S.R, 'latency_preservation', lambda blocks: (captured.setdefault('b', blocks), ('evidence gap', {}))[1]), \
                mock.patch.object(MS, 'save', lambda *a: None):
            MS.latency()
        self.assertEqual(captured['b'], [None] * 6)


class Preflight(unittest.TestCase):
    def test_missing_activation_is_unusable_stops_and_is_never_revised(self):
        with tempfile.TemporaryDirectory() as tmp:
            art = Path(tmp)
            (art / 'observations').mkdir()
            (art / 'observations' / 'memsmoke-S5-stream-p1-cand-mem-ring-bbrv3').mkdir()  # a failed run: no files
            with mock.patch.object(MS, 'ART', art), mock.patch.object(MS, 'save', lambda *a: None):
                with self.assertRaises(SystemExit) as stop:
                    MS.preflight()
                self.assertEqual(stop.exception.code, 3)
                res = json.loads((art / 'ring-preflight.json').read_text())
                self.assertEqual(res['decision'], 'unusable')
                self.assertFalse(res['instrument']['ok'])
                with self.assertRaises(SystemExit) as again:
                    MS.preflight()
                self.assertIn('never revised', str(again.exception.code))


class S6Scoring(unittest.TestCase):
    """p95() end to end on fabricated S6 observations with real relay.json files."""
    T0 = 10**18

    def run_p95(self, phases, queue, blocks=4, statuses=None):
        with tempfile.TemporaryDirectory() as tmp:
            grouped, status = {}, {}
            for b in range(1, blocks + 1):
                for w in ('stream', 'datagram'):
                    g = {}
                    for k in ('cand', 'aa'):
                        d = Path(tmp) / f'{w}-{b}-{k}'
                        d.mkdir()
                        (d / 'relay.json').write_text(json.dumps(dict(QueueSamples=queue)))
                        g[k] = dict(dir=str(d), usable=True, cfg=dict(start_unix_ns=self.T0, warmup_ms=10_000, measure_ms=30_000),
                                    summary=dict(id=f'{w}-{b}-{k}', control_p95_ms=150.0), send=dict(tail=dict(phases=phases)))
                    grouped[(w, b)] = g
                    status[(w, b)] = (statuses or {}).get((w, b), 'clean')
            saved = {}
            with mock.patch.object(MS, 'stage_obs', lambda phase, path: ([], status, [])), mock.patch.object(MS, 'group', lambda obs: grouped), \
                    mock.patch.object(MS, 'perturbation_ratios', lambda: []), mock.patch.object(MS, 'save', lambda k, v: saved.setdefault(k, v)):
                MS.p95()
            return {w: v['attribution'] for w, v in saved['s6_p95'].items()}

    def phases(self, refill=True):
        p = [[self.T0, 0], [self.T0 + 10_000 * 10**6, 3]]
        if refill:
            p += [[self.T0 + 15_000 * 10**6, 4], [self.T0 + 16_000 * 10**6, 3]]
        return p + [[self.T0 + 20_000 * 10**6, 5], [self.T0 + 21_000 * 10**6, 2], [self.T0 + 22_000 * 10**6, 3]]

    def queue(self):
        return [[t, 400_000 if 20_000 <= t < 22_000 else 1_000, 0] for t in range(10_000, 40_000, 10)]

    def test_complete_inputs_attribute(self):
        self.assertEqual(self.run_p95(self.phases(), self.queue()), dict(stream='selected ProbeBW Up policy', datagram='selected ProbeBW Up policy'))

    def test_sparse_timeline_missing_log_and_rejected_block_are_gaps(self):
        sparse = [[15_500, 1_000, 0], [17_000, 1_000, 0], [20_500, 400_000, 0]]
        self.assertEqual(set(self.run_p95(self.phases(), sparse).values()), {'evidence gap'})
        self.assertEqual(set(self.run_p95([], self.queue()).values()), {'evidence gap'})
        self.assertEqual(self.run_p95(self.phases(), self.queue(), statuses={('stream', 2): 'contaminated'})['stream'], 'evidence gap')

    def test_complete_timeline_without_refill_is_unresolved(self):
        self.assertEqual(set(self.run_p95(self.phases(refill=False), self.queue()).values()), {'unresolved'})


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
