"""Focused registration-review regressions; no network observations."""
import copy
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import campaign as C
import report as R


class ReportingTests(unittest.TestCase):
    def test_four_pair_plan_round_trip_and_repeat(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/'summary.json').write_text('{}')
            raised=[(p,w,'send_rss') for p in ['S5','loopback'] for w in ['stream','datagram']]
            with patch.object(R,'HERE',root),patch.object(C.run,'ART',root),patch.object(R.S,'readiness_summary',return_value={}),patch.object(R.S,'raised',side_effect=lambda s,a,k:raised if k=='rss' else []),patch.object(C,'selection',return_value=([],[])),patch.object(sys,'argv',['report.py','--plan']):
                R.main();saved=json.loads((root/'conditional-plan.json').read_text());R.main()
                self.assertEqual(saved,R.conditional_plan())
                self.assertEqual(saved['selected'],['presrss-S5-datagram','presrss-S5-stream'])
                self.assertEqual(saved['not_run_presrss'],[['loopback','datagram'],['loopback','stream']])

    def test_reporting_inventory_preserves_unresolved_and_cap_gaps(self):
        with tempfile.TemporaryDirectory() as tmp,patch.object(C.run,'ART',Path(tmp)):
            root=Path(tmp)
            (root/'conditional-plan.json').write_text(json.dumps(dict(selected=['presrss-S5-datagram'])))
            stop=root/'stage-stops/presrss-S5-datagram-datagram-p6.json';stop.parent.mkdir()
            stop.write_text(json.dumps(dict(status='not run (cap)')))
            def select(stage,workload,b,escalation=False):
                if b==6:raise FileNotFoundError('cap stopped')
                return [Path(f'clean-{b}')],dict(stage=stage,workload=workload,block=b,status='clean')
            with patch.object(C,'selected',side_effect=select):
                dirs,records=C.reporting_inventory('presrss')
                self.assertEqual(len(dirs),5)
                self.assertEqual(records[-1]['status'],'not run (cap)')
            with patch.object(C,'selected',side_effect=AssertionError('unresolved original')):
                dirs,records=C.reporting_inventory('main')
                self.assertEqual(dirs,[]);self.assertEqual(len(records),30)
                self.assertTrue(all(r['status']!='clean' for r in records))

    def test_raw_correspondence_rejects_stale_and_missing_caches(self):
        current={'summary.json':{'readiness':[]},'attribution.json':{'s5timeline':{'observations':['replacement/escalation']},'presrss':{},'preslat':{},'cpu_split':{}},'datagram-loss.json':{'cells':{'loss':0.1}}}
        with tempfile.TemporaryDirectory() as tmp,patch.object(C.run,'ART',Path(tmp)),patch.object(R,'derive_outputs'):
            for defect in ['timeline','missing','loss']:
                old=copy.deepcopy(current)
                if defect=='timeline':old['attribution.json']['s5timeline']={'observations':['old']}
                elif defect=='missing':old['attribution.json'].pop('s5timeline')
                else:old['datagram-loss.json']['cells']['loss']=0.0
                with self.subTest(defect=defect),patch.object(R,'cache_values',side_effect=[old,current]):
                    with self.assertRaisesRegex(AssertionError,'cache differs'):R.verify_raw_correspondence()
            self.assertEqual(len(list(Path(tmp).glob('cache-mismatch-*.json'))),3)
            # Untriggered preservation has valid empty outcome dictionaries.
            with patch.object(R,'cache_values',side_effect=[current,current]):
                self.assertEqual(R.verify_raw_correspondence(),current)

    def test_candidate_s6_benefit_outcomes(self):
        for blocks,above,expected in [(5,4,'not met'),(5,5,'met'),(4,4,'evidence gap')]:
            outcome=C.benefit_outcome(blocks,above)
            self.assertEqual(outcome,expected)
            entries=[dict(path='S6',workload=w,arm='cand-bbrv3',role='candidate blocking',complete=blocks==5,blocks=blocks,flags={},benefit_outcome=outcome,useful_benefit=dict(pairs_above_reno=above)) for w in ['stream','datagram']]
            benefit=[e for e in C.blocking_outcomes(entries) if e['criterion']=='S6 repeatable benefit']
            self.assertEqual(bool(benefit),expected!='met')

    def test_cap_and_selection_omissions_are_mechanically_checked(self):
        s=dict(blocking_reference=C.build.REVISIONS['cand'],descriptive_reference=C.build.REVISIONS['old'],
               readiness=[],groups=[],paired=[],historical_readiness=[],blocking_outcomes=[])
        a=dict(s5timeline=dict(observations=[],workloads={},verdict='evidence gap'),presrss={},preslat={},cpu_split={})
        caches={'summary.json':s,'attribution.json':a,'datagram-loss.json':dict(cells={})}
        plan=dict(selected=['presrss-S5-datagram'],not_run_presrss=[['loopback','datagram']],not_run_preslat=[])
        records=[dict(stage='presrss-S5-datagram',workload='datagram',block=b,status='clean' if b<6 else 'not run (cap)') for b in range(1,7)]
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            shutil.copy(R.HERE/'historical-context.json.gz',root/'historical-context.json.gz')
            (root/'conditional-plan.json').write_text(json.dumps(plan))
            with patch.object(R,'HERE',root),patch.object(C.run,'ART',root),patch.object(R.S,'readiness_summary',return_value=s),patch.object(R,'verify_raw_correspondence',return_value=caches),patch.object(R,'conditional_plan',return_value=plan),patch.object(C,'reporting_inventory',side_effect=lambda phase:([],records if phase=='presrss' else [])):
                with patch.object(sys,'argv',['report.py']):R.main()
                with patch.object(sys,'argv',['report.py','--check']):R.main()
            prose=(root/'result.md').read_text()
            self.assertIn('5/6 | incomplete; evidence gap',prose)
            self.assertIn('not run (cap)',prose)
            self.assertIn('not run (selection limit), evidence gap',prose)

    def test_empty_campaign_has_checked_gap_report_and_disclosure(self):
        # End-to-end reporting with no observations must yield checked gaps,
        # not an exception, a passing flag or fabricated zero measurements.
        with tempfile.TemporaryDirectory() as tmp:
            dest=Path(tmp)/'docs/audits/record'
            shutil.copytree(R.HERE,dest,ignore=shutil.ignore_patterns('__pycache__','result.md','raw*','summary*.json','attribution.json','datagram-loss.json'))
            for command in [('stages.py','all'),('dgloss.py',),('report.py',),('report.py','--check')]:
                p=subprocess.run([sys.executable,*command],cwd=dest,capture_output=True,text=True)
                self.assertEqual(p.returncode,0,p.stdout+p.stderr)
            result=(dest/'result.md').read_text()
            self.assertIn('incomplete (0 of 5)',result)
            self.assertIn('not run',result)
            self.assertIn('plain and diagnostic binaries are identical',result)
            self.assertIn('do not establish preservation against old frozen Reno',result)


    def test_original_validation_boundary_and_checked_fatal_gap(self):
        # Canonical loopback block: exercise real loaders without host activity.
        import hashlib
        for state in ['clean', 'contaminated', 'unknown', 'unknown_receipt', 'missing_heap']:
            with self.subTest(state=state), tempfile.TemporaryDirectory() as tmp:
                dest=Path(tmp)/'docs/audits/record'
                shutil.copytree(R.HERE,dest,ignore=shutil.ignore_patterns('__pycache__','result.md','raw*','summary*.json','attribution.json','datagram-loss.json'))
                art=Path(tmp)/'.local/bundle-990-readiness'
                directories=[]
                for variant,controller,tag in C.matrix.FOUR:
                    name=f'main-loopback-stream-p1-{variant}-{controller}'+(f'-{tag}' if tag else '')
                    d=art/'observations'/name;d.mkdir(parents=True)
                    cfg=dict(C.matrix.run_case_x.__globals__['PATHS']['loopback'],id=name,controller=controller,workload='stream',payload_bytes=16384,start_unix_ns=1_000_000_000)
                    start=cfg['start_unix_ns']+cfg['warmup_ms']*1_000_000
                    meta=dict(phase='main',variant=variant,path='loopback',pair=1,seed=None,tag=tag,affinity=C.run.AFFINITY)
                    delivery=dict(corrupt=0,duplicates=0,per_second_bytes=[1000000],useful_bytes=1000000)
                    endpoint=dict(source=C.source(variant),go_version='go1.27.0',platform='linux/amd64',gomaxprocs=4,managed_direct=True,trace_enabled=False,
                        result=dict(run=cfg,controller=controller,receiver=delivery,control=dict(latency_ns=[1000000],replies=1,skipped=0,unresolved=0,missed_opportunities=0)),
                        resources=dict(cpu_seconds=1,peak_rss_bytes=20*2**20,total_alloc_bytes=2**20),
                        resource_samples=[dict(unix_ns=start-1,heap_bytes=2**20),dict(unix_ns=start+1,heap_bytes=2**20)])
                    if state=='missing_heap':endpoint['resource_samples']=[]
                    receipt=dict(exit_codes=[0,0],binary_sha256=C.build.REGISTERED.get((variant,'fixture'),C.build.EXPECTED.get((variant,'fixture'))))
                    counters=dict(RcvbufErrors=0,SndbufErrors=0,InErrors=0,MemErrors=0)
                    facts=dict(measure_window_unix_ns=[start,start+cfg['measure_ms']*1_000_000],udp_before=counters,udp_after=counters)
                    samples=[dict(unix_ns=start+i*1_000_000_000,cpus={str(c):dict(busy=i*(100 if state=='contaminated' else 0)) for c in C.run.FIXTURE_CPUS+C.run.SIBLING_CPUS},fixture_ticks=dict(send=i,receive=i)) for i in range(cfg['measure_ms']//1000+1)]
                    if state in ['unknown','unknown_receipt']:samples=[]
                    for file,value in [('meta',meta),('config',cfg),('receipt',receipt),('send',endpoint),('receive',endpoint),('host-facts',facts),('host-cpu',samples)]:
                        (d/f'{file}.json').write_text(json.dumps(value))
                    directories.append(str(d.relative_to(art)))
                original=art/'blocks/loopback-stream-p1/original.json';original.parent.mkdir(parents=True)
                original.write_text(json.dumps(dict(fatal=state=='unknown',usable=state!='unknown',contaminated=state=='contaminated',directories=directories,pre_data_gates={},attempt='original')))
                raw={p:hashlib.sha256(p.read_bytes()).hexdigest() for p in art.rglob('*.json')}
                for command in [('stages.py','all'),('dgloss.py',),('report.py',),('report.py','--check')]:
                    result=subprocess.run([sys.executable,*command],cwd=dest,capture_output=True,text=True)
                    self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                selected=json.loads((dest/'summary.json').read_text())
                originals=json.loads((dest/'summary-original.json').read_text())
                self.assertEqual(len(selected['observations']),5 if state=='clean' else 0)
                self.assertEqual(len(originals['observations']),5 if state in ['clean','contaminated'] else 0)
                self.assertEqual(raw,{p:hashlib.sha256(p.read_bytes()).hexdigest() for p in raw})
                prose=(dest/'result.md').read_text()
                self.assertIn('incomplete; evidence gap',prose)
                self.assertIn('Original-only block inventory',prose)
                if state in ['unknown','unknown_receipt','missing_heap']:
                    self.assertIn('incomplete (retained original evidence)',prose)
                if state=='contaminated':self.assertIn('known contaminated',prose)


class CoordinationTests(unittest.TestCase):
    def test_unstarted_stops_and_started_interrupt(self):
        gate={'window_end':(datetime.now(timezone.utc)+timedelta(hours=1)).isoformat()}
        for stop in [SystemExit('host never quiet; stage not continued'),C.CoordinationStop('outside registered window')]:
            with tempfile.TemporaryDirectory() as tmp,patch.object(C.run,'ART',Path(tmp)),patch.object(C,'preflight',return_value=gate),patch.object(C.run,'ensure_credentials'):
                root=Path(tmp)
                with patch.object(C.matrix,'block',side_effect=stop):
                    with self.assertRaises(type(stop)):C.execute('s5','datagram',1)
                d=root/'blocks/s5-datagram-p1'
                self.assertFalse((d/'original.json').exists())
                self.assertEqual(len(list(d.glob('not-started-*.json'))),1)
                # A later admitted execution is not stopped by the old stop receipt.
                with patch.object(C.matrix,'block',return_value=[]),patch.object(C,'validate_block',return_value=False):
                    C.execute('s5','datagram',1)
                self.assertTrue((d/'original.json').exists())
        with tempfile.TemporaryDirectory() as tmp,patch.object(C.run,'ART',Path(tmp)),patch.object(C,'preflight',return_value=gate),patch.object(C.run,'ensure_credentials'):
            root=Path(tmp)
            def interrupted(*args,**kwargs):
                (root/'observations/main-S5-datagram-p1-reno-reno').mkdir(parents=True)
                raise KeyboardInterrupt()
            with patch.object(C.matrix,'block',side_effect=interrupted):
                with self.assertRaises(KeyboardInterrupt):C.execute('s5','datagram',1)
            r=json.loads((root/'blocks/s5-datagram-p1/original.json').read_text())
            self.assertTrue(r['fatal']);self.assertFalse(r['usable'])
