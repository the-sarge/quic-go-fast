"""Prospective completeness, reference-role and failure-boundary tests."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import campaign as C
import analysis_load as A


def block(stage='s5', workload='datagram', b=1):
    phase, path, arms, seed, _, _ = C.spec(stage, workload, b)
    return [dict(phase=phase, path=path, workload=workload, pair=b, seed=seed,
                 variant=v, controller=c, tag=t, contamination=dict(contaminated=False)) for v,c,t in arms]


class CampaignTests(unittest.TestCase):
    def test_exact_arm_inventory(self):
        for stage, count in [('loopback',5), ('s5',5), ('s6',4), ('s5timeline',1), ('presrss-S5-datagram',2), ('preslat-datagram',2)]:
            rows=block(stage)
            self.assertEqual(len(rows),count)
            self.assertFalse(C.validate_block(rows,stage,'datagram',1))
            for defect in ('missing','duplicate','seed','path','controller','unknown'):
                bad=copy.deepcopy(rows)
                if defect=='missing':bad.pop()
                elif defect=='duplicate':bad.append(bad[0])
                elif defect=='unknown':bad[0]['contamination']['contaminated']=None
                else:bad[0][defect]='wrong'
                with self.subTest(stage=stage,defect=defect),self.assertRaises(AssertionError):
                    C.validate_block(bad,stage,'datagram',1)

    def test_contamination_does_not_become_clean(self):
        rows=block();rows[0]['contamination']['contaminated']=True
        self.assertTrue(C.validate_block(rows,'s5','datagram',1))

    def test_selection_requires_all_blocks(self):
        with patch.object(C,'selected',side_effect=FileNotFoundError):
            with self.assertRaises(FileNotFoundError):C.selection('main')

    def test_fatal_original_cannot_be_replaced(self):
        with tempfile.TemporaryDirectory() as tmp,patch.object(C.run,'ART',Path(tmp)):
            d=Path(tmp)/'blocks/s5-datagram-p1';d.mkdir(parents=True)
            (d/'original.json').write_text(json.dumps(dict(fatal=True,usable=False)))
            (d/'rerun.json').write_text(json.dumps(dict(fatal=False,usable=True,contaminated=False)))
            with self.assertRaisesRegex(AssertionError,'fatal original'):C.selected('s5','datagram',1)

    def test_dirty_selected_evidence_stays_blocked(self):
        for flag in (None,True):
            with tempfile.TemporaryDirectory() as tmp,patch.object(C.run,'ART',Path(tmp)):
                d=Path(tmp)/'blocks/s5-datagram-p1';d.mkdir(parents=True)
                (d/'original.json').write_text(json.dumps(dict(fatal=False,usable=True,contaminated=True)))
                (d/'rerun.json').write_text(json.dumps(dict(fatal=False,usable=True,contaminated=flag)))
                with self.assertRaises(AssertionError):C.selected('s5','datagram',1)

    def test_candidate_and_reference_binary_identity(self):
        self.assertEqual(C.build.REVISIONS['reno'], C.build.REVISIONS['cand'])
        self.assertEqual(C.build.REGISTERED[('reno','fixture')],C.build.REGISTERED[('cand','fixture')])
        self.assertEqual(C.build.REGISTERED[('reno-diag','fixture')],C.build.REGISTERED[('cand-diag','fixture')])
        self.assertNotEqual(C.source('old'),C.source('reno'))

    def test_baseline_denominator_and_historical_drift(self):
        rows=[]
        for b in range(1,6):
            for arm,mult in [('reno-reno',1),('reno-reno-aa',1.3),('cand-bbrv3',1.2),('cand-reno',1),('old-reno',0.5)]:
                metric=dict(cpu_seconds_per_gib=10*mult,peak_rss_mib=10*mult,allocated_gib_per_gib=1,
                            heap_peak_warmup_mib=1,heap_peak_measured_mib=1)
                rows.append(dict(phase='main',path='S5',workload='datagram',pair=b,seed=9000+b,arm=arm,
                                 goodput_mbps=100,control_p95_ms=10*mult,control_p50_ms=5,send=metric,receive=metric))
        with patch.object(A,'rows',rows):
            _,_,flags=A.readiness('main')
            c=next(e for e in flags if e['arm']=='cand-bbrv3')
            r=next(e for e in flags if e['arm']=='cand-reno')
            self.assertAlmostEqual(c['flags']['send_rss']['median'],1.2)
            self.assertTrue(c['flags']['send_rss']['raised']) # A/A at 1.3 does not erase it.
            self.assertFalse(r['flags']['send_rss']['raised'])
            _,_,history=A.readiness('main',ref_arm='old-reno')
            self.assertAlmostEqual(next(e for e in history if e['arm']=='cand-reno')['flags']['send_rss']['median'],2)
