"""Offline checks for the finite non-competitor cloud execution aid."""
import importlib.util
import ast
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('cloud', HERE / 'run-linux-v2.py')
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)

class CloudRunnerTests(unittest.TestCase):
    def gateway_commands(self):
        # Execute the actual emitted shell boundary, using fake native tools.
        tree = ast.parse((HERE / 'run-linux-v2.py').read_text())
        wanted = ('gateway-native-path', 'gateway-peer-identities')
        return {node.args[3].value: eval(compile(ast.Expression(node.args[1]), '<command>', 'eval'), vars(cloud))
                for node in ast.walk(tree) if isinstance(node, ast.Call)
                and isinstance(node.func, ast.Name) and node.func.id == 'require'
                and len(node.args) > 3 and isinstance(node.args[3], ast.Constant)
                and node.args[3].value in wanted}

    def gateway_shell(self, bad_neighbor=False, offload_failure=False):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            tools = {
                'sudo': '#!/bin/sh\nshift\nexec "$@"\n',
                'ethtool': '#!/bin/sh\nexit ' + ('1' if offload_failure else '0') + '\n',
                'ping': '#!/bin/sh\nexit 1\n',
                'ip': '#!/bin/sh\nif [ "$2" = neigh ]; then cat "$NEIGHBORS"; else echo "[]"; fi\n',
            }
            for name, source in tools.items():
                (base / name).write_text(source); (base / name).chmod(0o700)
            neighbors = [dict(dst='10.63.1.1', lladdr='wrong' if bad_neighbor else '42:01:0a:3f:01:01'),
                         dict(dst='10.63.2.1', lladdr='42:01:0a:3f:02:01')]
            (base / 'neighbors.json').write_text(json.dumps(neighbors))
            commands = self.gateway_commands()
            for label in ('gateway-native-path', 'gateway-peer-identities'):
                result = subprocess.run(['bash', '-c', commands[label]],
                                        env=dict(os.environ, PATH=temp + ':' + os.environ['PATH'], NEIGHBORS=str(base / 'neighbors.json')),
                                        capture_output=True, text=True, timeout=5)
                if result.returncode: return result
            return result

    def test_virtual_gateway_nonreply_allows_verified_neighbors(self):
        result = self.gateway_shell()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_wrong_neighbor_still_fails(self):
        self.assertNotEqual(self.gateway_shell(bad_neighbor=True).returncode, 0)

    def test_offload_failure_still_fails(self):
        self.assertNotEqual(self.gateway_shell(offload_failure=True).returncode, 0)

    def teardown_with_duration(self, seconds):
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            project = base / 'bbr-q635-20260925'; project.mkdir()
            request = {'project_id': project.name}
            (project / 'launch.json').write_text(json.dumps(dict(status='active', request=request)))
            elapsed = [0]
            def wait(timeout=None):
                if timeout is None: raise AssertionError('unbounded process wait')
                elapsed[0] += timeout
                if elapsed[0] < seconds: raise subprocess.TimeoutExpired('destroy', timeout)
                return 0
            process = SimpleNamespace(pid=99999999, wait=wait)
            with patch.object(cloud, 'ROOT', base), patch.object(cloud, 'RECORDS', base), patch.object(cloud.subprocess, 'Popen', return_value=process), patch.object(cloud.os, 'killpg') as signals:
                failure = None
                try: cloud.destroy_owned(request)
                except (RuntimeError, subprocess.TimeoutExpired) as exc: failure = exc
                receipt = json.loads((base / 'destroy-exit.json').read_text()) if (base / 'destroy-exit.json').exists() else None
                return failure, receipt, signals.call_count, elapsed[0]

    def test_observed_slow_destroy_completes_without_interruption(self):
        failure, receipt, signals, _ = self.teardown_with_duration(300)
        self.assertIsNone(failure)
        self.assertEqual(receipt['exit_code'], 0)
        self.assertEqual(signals, 0)

    def test_destroy_timeout_is_finite_and_remains_a_failure(self):
        failure, receipt, signals, elapsed = self.teardown_with_duration(1000)
        self.assertIsNotNone(failure)
        self.assertEqual(signals, 2)
        # Leave room for after-clock observation, monitors and independent inventories.
        self.assertLessEqual(elapsed + 130 + 30 + 60, cloud.CLEANUP_SECONDS)


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
