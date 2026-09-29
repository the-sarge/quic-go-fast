"""Offline checks for the finite non-competitor cloud execution aid."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('cloud', HERE / 'run-linux.py')
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)

class CloudRunnerTests(unittest.TestCase):
    def test_frozen_linux_case_expansion(self):
        cloud.configure('q2-linux-20260929-offline')
        rows = [json.loads(s) for s in (cloud.SOURCE / 'case-inventory.jsonl').read_text().splitlines()]
        leases = json.loads((cloud.SOURCE / 'lease-plan-candidate.json').read_text())['leases'][:3]
        for lease in leases:
            for index in lease['case_indices']:
                d = cloud.expand(index, 1800000000000000000)
                self.assertEqual(d['case'], rows[index])
                self.assertEqual(d['focal_config'], dict(rows[index]['fixture'], start_unix_ns=1800000000000000000))
                self.assertEqual([c['label'] for c in d['commands']], ['native-path' if rows[index]['scenario']=='S1' else 'router', 'focal-receive', 'focal-send'])
                self.assertTrue(all(c['host'] in ['bbr-a','bbr-b','bbr-gateway'] for c in d['commands']))
        with self.assertRaises(ValueError): cloud.expand(340, 1800000000000000000)
        with self.assertRaises(ValueError): cloud.expand(240, 1800000000000000000)

    def test_clock_failure_still_destroys_and_checks_both_inventories(self):
        with tempfile.TemporaryDirectory() as temp:
            cloud.ROOT = Path(temp)
            from types import SimpleNamespace
            with patch.object(cloud, 'clock', side_effect=ValueError('inconsistent')), patch.object(cloud, 'destroy_owned') as action, patch.object(cloud.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout='[]', stderr='')) as inventories:
                result = cloud.finish({'project_id':'bbr-q635-20260925'}, 'completed', None, [], True)
                self.assertEqual(result['status'], 'failed')
                self.assertEqual(action.call_count, 1)
                self.assertEqual(inventories.call_count, 2)
                self.assertIn('clock-after', result['cleanup_errors'][0])

    def test_destroy_failure_preserves_failed_disposition(self):
        with tempfile.TemporaryDirectory() as temp:
            cloud.ROOT=Path(temp)
            from types import SimpleNamespace
            with patch.object(cloud, 'destroy_owned', side_effect=RuntimeError('destroy failed')), patch.object(cloud.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout='[]', stderr='')) as inventories:
                result=cloud.finish({'project_id':'bbr-q635-20260925'}, 'completed', None, [], False)
                self.assertEqual(result['status'], 'failed')
                self.assertEqual(inventories.call_count, 2)
                self.assertIn('destroy', result['cleanup_errors'][0])

    def test_unrelated_live_launch_is_never_destroyed(self):
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp)
            project=base/'bbr-q635-20260925';project.mkdir()
            (project/'launch.json').write_text(json.dumps(dict(status='active',request={'other':'launch'})))
            with patch.object(cloud,'RECORDS',base), patch.object(cloud,'local') as action:
                with self.assertRaisesRegex(RuntimeError,'identity differs'):
                    cloud.destroy_owned({'project_id':'bbr-q635-20260925'})
                action.assert_not_called()

    def test_duplicate_attempt_refused_before_cloud_action(self):
        with tempfile.TemporaryDirectory() as temp:
            cloud.ROOT=Path(temp)
            (cloud.ROOT/'dispatch-started.json').write_text('{}')
            with self.assertRaisesRegex(ValueError,'already used'): cloud.preflight('Q2-01')

if __name__ == '__main__': unittest.main()
