"""Finite continuation boundary and actual detached parent-death verification."""
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

cloud = load('cloud3', HERE / 'run-linux-v3.py')
launcher = load('launcher3', HERE / 'launch-linux-v3.py')

class ContinuationTests(unittest.TestCase):
    def prepared(self, base, used=60.98466666666667, forecast=99.868):
        root = base / 'new'; root.mkdir()
        project = base / 'bbr-q635-test'; project.mkdir()
        (project / 'launch.json').write_text(json.dumps(dict(status='destroyed')))
        request = dict(project_id=project.name, platform='linux', competitors=False,
                       gateway_cores=4, lifetime_minutes=429, experiment_hours_used=used,
                       spent_usd=61.97, preparation_hours_used=24.731656806388887,
                       hourly_usd=1.3, reserve_usd=0.25)
        ledger = dict(baseline_experiment_hours=used, baseline_reserved_usd=61.97,
                      reservations=[], experiment_ceiling_hours=192,
                      preparation_budget_guard_hours=24.731656806388887,
                      forecast_experiment_hours=forecast, forecast_reserved_usd=127.058333)
        for name, value in [('request.json', request), ('ledger.json', ledger),
                            ('stage-manifest.json', dict(local_files={}, source_files={}))]:
            (root / name).write_text(json.dumps(value))
        return root

    def test_suffix_starts_at_complete_pair_and_never_selects_prefix(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);root=self.prepared(base)
            with patch.object(cloud,'ROOT',root), patch.object(cloud,'RECORDS',base), patch.object(cloud,'check_prefix') as prefix:
                lease, _, _, _=cloud.preflight('Q2-01')
                self.assertEqual(lease['case_indices'], list(range(12,108)))
                prefix.assert_called_once()
                with patch.object(cloud,'check_prefix',side_effect=ValueError('prefix receipt changed')):
                    with self.assertRaisesRegex(ValueError,'prefix receipt changed'):cloud.preflight('Q2-01')

    def test_cumulative_192_hour_boundary_and_forecast_are_enforced(self):
        for used, forecast, allowed in [(184.85,192,True),(184.851,192,False),(60.98466666666667,192.001,False)]:
            with self.subTest(used=used,forecast=forecast), tempfile.TemporaryDirectory() as temp:
                base=Path(temp);root=self.prepared(base,used,forecast)
                with patch.object(cloud,'ROOT',root), patch.object(cloud,'RECORDS',base), patch.object(cloud,'check_prefix'):
                    if allowed:cloud.preflight('Q2-01')
                    else:
                        with self.assertRaises(ValueError):cloud.preflight('Q2-01')

    def test_detached_job_survives_killed_launching_process_group(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);worker=base/'worker.py'
            worker.write_text('import os,time,pathlib\np=pathlib.Path(__file__).parent\n(p/"worker-start").write_text(str(os.getpid()))\ntime.sleep(2)\n(p/"worker-done").write_text("completed")\n')
            parent=base/'parent.py'
            parent.write_text('import importlib.util,pathlib,time,os,json\np=pathlib.Path(__file__).parent\ns=importlib.util.spec_from_file_location("launch",'+repr(str(HERE / 'launch-linux-v3.py'))+')\nm=importlib.util.module_from_spec(s);s.loader.exec_module(m)\nx=m.detached_process(['+repr(sys.executable)+',str(p/"worker.py")],p)\n(p/"parent-submitted").write_text(json.dumps({"pid":x.pid,"session":os.getsid(x.pid),"parent_session":os.getsid(0)}))\ntime.sleep(30)\n')
            process=subprocess.Popen([sys.executable,str(parent)],start_new_session=True)
            child=None
            try:
                deadline=time.monotonic()+5
                while not (base/'parent-submitted').exists() or not (base/'worker-start').exists():
                    if process.poll() is not None or time.monotonic()>deadline:self.fail('detached/start did not complete')
                    time.sleep(.05)
                receipt=json.loads((base/'parent-submitted').read_text());child=receipt['pid']
                self.assertEqual(receipt['session'],child)
                self.assertNotEqual(receipt['session'],receipt['parent_session'])
                os.killpg(process.pid,signal.SIGKILL);process.wait(timeout=2)
                deadline=time.monotonic()+5
                while not (base/'worker-done').exists():
                    if time.monotonic()>deadline:self.fail('detached worker died with launching process group')
                    time.sleep(.05)
                self.assertEqual((base/'worker-done').read_text(),'completed')
            finally:
                if process.poll() is None:os.killpg(process.pid,signal.SIGKILL);process.wait(timeout=2)
                if child:
                    try:os.killpg(child,signal.SIGKILL)
                    except ProcessLookupError:pass

if __name__=='__main__':unittest.main()
