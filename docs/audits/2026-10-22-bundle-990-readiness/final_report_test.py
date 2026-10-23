"""Concrete replacement-review regressions; no network measurements."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import campaign as C
import report as R
import stages as S


class FinalReportTests(unittest.TestCase):
    def test_escalated_tie_has_stable_counts_cache_and_prose(self):
        with tempfile.TemporaryDirectory() as tmp:
            dest=Path(tmp)/'docs/audits/record'
            shutil.copytree(R.HERE,dest,ignore=shutil.ignore_patterns('__pycache__','result.md','raw*','summary*.json','attribution.json','datagram-loss.json'))
            code="""
import json,sys
from unittest.mock import patch
import report as R
import stages as S
# Preserve four-block and unambiguous six-block outcomes; the competing
# qualifying labels in a 3-3 escalation must not select a hash-order winner.
assert S.timeline_verdict(['model']*2+['timing']*2)=='inconclusive'
assert S.timeline_verdict(['model']*3+['timing'])=='model'
assert S.timeline_verdict(['model']*4+['timing']*2)=='model'
assert S.timeline_verdict(['unusable']*4)=='evidence gap'
obs=[dict(id=f'{w}-{i}',workload=w,block=i+1,verdict=v,decomposition=None,phase_share={}) for w in ['stream','datagram'] for i,v in enumerate(['model','timing','model','timing','model','timing'])]
s=dict(blocking_reference=R.C.build.REVISIONS['cand'],descriptive_reference=R.C.build.REVISIONS['old'],readiness=[],groups=[],paired=[],historical_readiness=[],blocking_outcomes=[])
(R.HERE/'summary.json').write_text(json.dumps(s))
if not S.OUT.exists():S.OUT.write_text(json.dumps(dict(cpu_split={},presrss={},preslat={})))
(R.HERE/'datagram-loss.json').write_text(json.dumps(dict(cells={})))
def derive():
    S.s5timeline()
with patch.object(S,'chosen',return_value=(obs,[])),patch.object(S,'timeline_obs',side_effect=lambda o:o),patch.object(R,'derive_outputs',side_effect=derive),patch.object(R.C,'reporting_inventory',return_value=([],[dict(stage='s5timeline',workload='stream',block=1,status='not run')])):
    if '--check' not in sys.argv:derive()
    R.main()
a=json.loads(S.OUT.read_text())['s5timeline']
assert a['workloads']==dict(stream='inconclusive',datagram='inconclusive')
assert a['verdict']=='inconclusive'
assert all(c==dict(model=3,timing=3) for c in a['workload_counts'].values())
"""
            for seed,args in [('1',[]),('3',['--check'])]:
                p=subprocess.run([sys.executable,'-c',code,*args],cwd=dest,env={**os.environ,'PYTHONHASHSEED':seed},capture_output=True,text=True)
                self.assertEqual(p.returncode,0,p.stdout+p.stderr)
            self.assertIn('workload_counts',(dest/'result.md').read_text())

    def test_missing_indexed_profile_retains_checked_gap_and_no_retry(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);art=root/'art';art.mkdir()
            stage='presrss-loopback-stream';directories=[]
            for variant in ['reno-diag','cand-diag']:
                name=f'presrss-loopback-stream-p1-{variant}-reno'
                d=art/'observations'/name;d.mkdir(parents=True)
                cfg=dict(C.run.PATHS['loopback'],id=name,controller='reno',workload='stream',payload_bytes=16384,start_unix_ns=1_000_000_000)
                start=cfg['start_unix_ns']+cfg['warmup_ms']*1_000_000
                meta=dict(phase='presrss',variant=variant,path='loopback',pair=1,seed=None,tag='',affinity=C.run.AFFINITY)
                delivery=dict(corrupt=0,duplicates=0,per_second_bytes=[1000000],useful_bytes=1000000)
                endpoint=dict(source=C.source(variant),go_version='go1.27.0',platform='linux/amd64',gomaxprocs=4,managed_direct=True,trace_enabled=False,
                    result=dict(run=cfg,controller='reno',receiver=delivery,control=dict(latency_ns=[1000000],replies=1,skipped=0,unresolved=0,missed_opportunities=0)),
                    resources=dict(cpu_seconds=1,peak_rss_bytes=20*2**20,total_alloc_bytes=2**20))
                receipt=dict(exit_codes=[0,0],binary_sha256=C.build.REGISTERED[(variant,'fixture')])
                facts=dict(measure_window_unix_ns=[start,start+cfg['measure_ms']*1_000_000])
                samples=[dict(unix_ns=start+i*1_000_000_000,cpus={str(c):dict(busy=i) for c in C.run.FIXTURE_CPUS+C.run.SIBLING_CPUS},fixture_ticks=dict(send=i*10,receive=i*10)) for i in range(2)]
                series=dict(columns=['unix_ns','/memory/classes/heap/objects:bytes'],samples=[[start+1,2**20]],heap_series=[[0,start+1]])
                for file,value in [('meta',meta),('config',cfg),('receipt',receipt),('send',endpoint),('receive',endpoint),('host-facts',facts),('host-cpu',samples),('send.series',series)]:
                    (d/f'{file}.json').write_text(json.dumps(value))
                directories.append(str(d.relative_to(art)))
            original=art/f'blocks/{stage}-stream-p1/original.json';original.parent.mkdir(parents=True)
            original.write_text(json.dumps(dict(fatal=False,usable=True,contaminated=False,directories=directories,pre_data_gates={},attempt='original',stage=stage,phase='presrss')))
            stats=dict(median=1.0,minimum=1.0,maximum=1.0,n=5)
            entry=dict(path='loopback',workload='stream',arm='cand-reno',role='preservation blocking',blocks=5,complete=True,
                ratios={k:dict(stats) for k in R.KEYS},flags={k:dict(median=1.0,raised=k=='send_rss',blocks_crossing=5 if k=='send_rss' else 0,aa=stats) for k in R.KEYS})
            entry['ratios']['send_rss']['median']=1.2;entry['flags']['send_rss']['median']=1.2
            summary=dict(blocking_reference=C.build.REVISIONS['cand'],descriptive_reference=C.build.REVISIONS['old'],readiness=[entry],groups=[],paired=[],historical_readiness=[],observations=[],blocking_outcomes=C.blocking_outcomes([entry]))
            (root/'summary.json').write_text(json.dumps(summary))
            shutil.copy(R.HERE/'historical-context.json.gz',root/'historical-context.json.gz')
            import dgloss
            with patch.object(C.run,'ART',art),patch.object(S,'ART',art),patch.object(S,'HERE',root),patch.object(S,'OUT',root/'attribution.json'),patch.object(R,'HERE',root),patch.object(dgloss,'OUT',root/'datagram-loss.json'),patch.object(S,'readiness'):
                (art/'conditional-plan.json').write_text(json.dumps(R.conditional_plan()))
                ds,_=C.selected(stage,'stream',1)
                self.assertEqual(len(ds),2) # Canonical admitted preservation evidence.
                raw={p:hashlib.sha256(p.read_bytes()).hexdigest() for p in art.rglob('*.json')}
                R.derive_outputs() # Real pprof fails on the indexed unavailable profile.
                gap=json.loads(S.OUT.read_text())['presrss']
                self.assertEqual(gap['outcome'],'evidence gap')
                self.assertNotEqual(gap['diagnostic_failure']['exit_code'],0)
                self.assertIn('pprof',gap['diagnostic_failure']['command'])
                self.assertIn('heap-000.pprof',gap['diagnostic_failure']['stderr'])
                for args in [['report.py'],['report.py','--check']]:
                    with patch.object(sys,'argv',args):R.main()
                self.assertIn('diagnostic_failure',(root/'result.md').read_text())
                self.assertIn('send_rss',(root/'result.md').read_text())
                self.assertEqual(raw,{p:hashlib.sha256(p.read_bytes()).hexdigest() for p in raw})
                with patch.object(C,'preflight',return_value={}):
                    with self.assertRaisesRegex(AssertionError,'clean block not rerunnable'):
                        C.execute(stage,'stream',1,rerun=True)
                self.assertEqual(len(C.selected(stage,'stream',1)[0]),2)
