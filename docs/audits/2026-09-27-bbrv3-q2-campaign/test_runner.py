"""Offline regressions at the campaign receipt and lifecycle boundaries."""
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('runner', HERE / 'run-mac-v2.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class Receipts(unittest.TestCase):
    def setUp(self):
        sample = json.loads((HERE / 'receipt-sample.json').read_text())
        self.case, self.roles = sample['case'], sample['roles']

    def test_native_go_receipt_with_empty_optional_competitors(self):
        before = copy.deepcopy(self.case)
        self.assertEqual([], runner.validate(self.case, self.roles))
        self.assertEqual(before, self.case)

    def test_configuration_identity_is_still_strict(self):
        for field, value in [('Scenario', 'S4'), ('Seed', 123), ('StartUnixNS', 1)]:
            with self.subTest(field=field):
                roles = copy.deepcopy(self.roles)
                roles['router'][1]['Config'][field] = value
                self.assertTrue(runner.validate(self.case, roles))
        self.roles['router'][1]['Config']['A']['Competitor'] = '10.64.1.11'
        self.assertTrue(runner.validate(self.case, self.roles))

    def test_missing_or_malformed_required_observations_fail_closed(self):
        mutations = [
            lambda r: r['router'][1]['Observations'][0].pop('SocketDrops'),
            lambda r: r['router'][1]['Observations'][0].pop('MaxIngressLagNS'),
            lambda r: r['router'][1]['Observations'][0]['Stats'].pop('PropagationOverflow'),
            lambda r: r['router'][1]['Observations'].__setitem__(1, r['router'][1]['Observations'][0]),
            lambda r: r['router'][1].__setitem__('Profiled', True),
            lambda r: r['focal-receive'][1]['result'].pop('receiver'),
            lambda r: r['focal-send'][1].pop('trace'),
            lambda r: r['focal-send'][1]['result']['control'].__setitem__('error', 'clock failure'),
            lambda r: r.__setitem__('router', [0, []]),
            lambda r: r.pop('focal-send'),
        ]
        for i, mutate in enumerate(mutations):
            with self.subTest(mutation=i):
                roles = copy.deepcopy(self.roles)
                mutate(roles)
                self.assertTrue(runner.validate(self.case, roles))

    def test_existing_hard_gates_reject_real_failures(self):
        for key, value in [('SocketDrops', 1), ('MaxIngressLagNS', 5000001),
                           ('MaxEgressLagNS', 5000001), ('MissingTimestamps', 1)]:
            with self.subTest(key=key):
                roles = copy.deepcopy(self.roles)
                roles['router'][1]['Observations'][0][key] = value
                self.assertTrue(runner.validate(self.case, roles))
        for key, value in [('Source', 'wrong'), ('PacketVersion', 3)]:
            roles = copy.deepcopy(self.roles)
            roles['router'][1][key] = value
            self.assertTrue(runner.validate(self.case, roles))

    def test_completion_requires_boolean_disposition_and_delivery_proof(self):
        self.case['case']['group'] = 'completion'
        self.case['focal_config']['completion_bytes'] = 16777216
        for label in ['focal-send', 'focal-receive']:
            result = self.roles[label][1]['result']
            result['run'] = copy.deepcopy(self.case['focal_config'])
            result['completion_verified'] = False
            result['censored'] = True
        self.assertEqual([], runner.validate(self.case, self.roles))
        self.roles['focal-receive'][1]['result']['censored'] = 'true'
        self.assertTrue(runner.validate(self.case, self.roles))

    def test_recorded_native_completion_requires_exact_useful_byte_total(self):
        sample = json.loads((HERE / 'completion-sample.json').read_text())
        self.assertEqual([], runner.validate(sample['case'], sample['roles']))
        sample['roles']['focal-receive'][1]['result']['receiver']['useful_bytes'] -= 1
        self.assertTrue(runner.validate(sample['case'], sample['roles']))


class Cleanup(unittest.TestCase):
    def setUp(self):
        runner.configure('q2-mac-20260927-offline-test')

    def test_owned_monitor_child_is_reaped_on_stop(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(runner, 'STAGE', tmp):
            script = Path(tmp) / 'monitor.sh'
            script.write_text(runner.monitor_script('sleep 600', False))
            proc = subprocess.Popen(['/bin/sh', str(script)])
            try:
                for _ in range(100):
                    if (Path(tmp) / 'monitor-child.pid').exists():
                        break
                    time.sleep(0.01)
                child = int((Path(tmp) / 'monitor-child.pid').read_text())
                stopped = subprocess.run(runner.stop_monitor_command(), shell=True, capture_output=True, text=True, timeout=10)
                self.assertEqual(0, stopped.returncode, stopped.stderr)
                proc.wait(timeout=5)
                self.assertNotEqual(0, subprocess.run(['ps', '-p', str(child)], capture_output=True).returncode)
            finally:
                if proc.poll() is None:
                    proc.terminate()
                    proc.wait(timeout=5)

    def test_clock_timeout_does_not_skip_gateway_or_admission_cleanup(self):
        calls = []
        def external(args, **kwargs):
            calls.append(args)
            if args[0] == 'python3':
                raise subprocess.TimeoutExpired(args, 60)
            return subprocess.CompletedProcess(args, 0, '', '')
        with tempfile.TemporaryDirectory() as tmp, patch.object(runner, 'ROOT', Path(tmp)), patch.object(runner.subprocess, 'run', side_effect=external):
            result = runner.finish_lease([], 'completed', None)
            self.assertEqual('failed', result['status'])
            self.assertTrue(result['gateway_cleanup_ok'])
            self.assertTrue(any('--release-host-admission-consumer' in c[-1] for c in calls))
            self.assertTrue(any('/q2-mac-20260927-offline-test/q1fixture' in c[-1] for c in calls))
            self.assertTrue((Path(tmp) / 'lease-result.json').is_file())

    def test_gateway_failure_retains_failed_admission(self):
        calls = []
        def external(args, **kwargs):
            calls.append(args)
            if args[0] == 'python3':
                raise OSError('clock observer unavailable')
            code = 1 if 'q1-gateway-network.sh down' in args[-1] else 0
            return subprocess.CompletedProcess(args, code, '', '')
        with tempfile.TemporaryDirectory() as tmp, patch.object(runner, 'ROOT', Path(tmp)), patch.object(runner.subprocess, 'run', side_effect=external):
            result = runner.finish_lease([], 'failed', 'original case failure')
            self.assertEqual('original case failure', result['error'])
            self.assertFalse(result['gateway_cleanup_ok'])
            self.assertTrue(any('--fail-host-admission-consumer' in c[-1] for c in calls))
            self.assertFalse(any('--release-host-admission-consumer' in c[-1] for c in calls))

    def test_endpoint_cleanup_failure_retains_lease_and_admission_error_is_recorded(self):
        calls = []
        def external(args, **kwargs):
            calls.append(args)
            if args[0] == 'python3':
                raise subprocess.TimeoutExpired(args, 60)
            code = 1 if 'pkill' in args[-1] or '--fail-host-admission-consumer' in args[-1] else 0
            return subprocess.CompletedProcess(args, code, '', '')
        with tempfile.TemporaryDirectory() as tmp, patch.object(runner, 'ROOT', Path(tmp)), patch.object(runner.subprocess, 'run', side_effect=external):
            result = runner.finish_lease([], 'failed', 'case failure')
            self.assertFalse(result['endpoint_cleanup_ok'])
            self.assertEqual(1, result['admission_disposition_exit'])
            self.assertFalse(any('--release-host-admission-consumer' in c[-1] for c in calls))

    def test_success_requires_clock_receipt_and_all_cleanup_steps(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(runner, 'ROOT', Path(tmp)):
            def external(args, **kwargs):
                if args[0] == 'python3':
                    data = {'hosts': {h: {'consistent': True, 'samples': [1], 'intersection_lower_ns': 0, 'intersection_upper_ns': 1} for h in ('m4mini-eth', 'mbp128', 'minimax')}}
                    (Path(tmp) / 'clock-after.json').write_text(json.dumps(data))
                return subprocess.CompletedProcess(args, 0, '', '')
            with patch.object(runner.subprocess, 'run', side_effect=external):
                result = runner.finish_lease([], 'completed', None)
            self.assertEqual('completed', result['status'])
            self.assertEqual([], result['cleanup_errors'])


class Dispatch(unittest.TestCase):
    def test_ledger_exhaustion_and_used_attempt_stop_before_dispatch(self):
        runner.configure('q2-mac-20260927-offline-test')
        with tempfile.TemporaryDirectory() as tmp, patch.object(runner, 'ROOT', Path(tmp)):
            root = Path(tmp)
            clock = {'hosts': {h: {'consistent': True, 'samples': [1], 'intersection_lower_ns': 0, 'intersection_upper_ns': 1} for h in ('m4mini-eth', 'mbp128', 'minimax')}}
            lease = dict(zip(['instance_id', 'host', 'consumer_kind', 'consumer_id'], runner.IDENTITY), exclusive_host=True)
            ledger = {'experiment_ceiling_hours': 96, 'cloud_ceiling_usd': 150, 'preparation_ceiling_hours': 72,
                      'baseline_experiment_hours': 24.718, 'baseline_reserved_usd': 30.02,
                      'reservations': [{'hours': 0.5, 'usd': 0}, {'hours': 338/60, 'usd': 0}],
                      'forecast_experiment_hours': 76.118, 'forecast_reserved_usd': 96.084}
            for name, data in [('mac-case-indices.json', list(range(340, 400)) + list(range(500, 510))), ('host-lease.json', lease), ('clock-before.json', clock), ('ledger.json', ledger)]:
                (root / name).write_text(json.dumps(data))
            (root / 'followup-clock-observe.py').touch()
            before = (root / 'ledger.json').read_bytes()
            runner.preflight()
            self.assertEqual(before, (root / 'ledger.json').read_bytes())
            (root / 'dispatch-started.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'already used'):
                runner.preflight()
            (root / 'dispatch-started.json').unlink()
            ledger['baseline_experiment_hours'] = 95
            (root / 'ledger.json').write_text(json.dumps(ledger))
            with self.assertRaisesRegex(ValueError, 'allowance'):
                runner.preflight()

    def test_all_seventy_original_mac_commands_expand_offline(self):
        inventory = [json.loads(line) for line in (runner.SOURCE / 'case-inventory.jsonl').read_text().splitlines()]
        indices = list(range(340, 400)) + list(range(500, 510))
        self.assertEqual(70, len(indices))
        for index in indices:
            with self.subTest(index=index):
                d = json.loads(subprocess.check_output(['python3', str(runner.SOURCE / 'case-commands.py'), str(index), '--start-unix-ns', '1790553600000000000'], text=True))
                self.assertEqual(inventory[index], d['case'])
                self.assertEqual('darwin/arm64', d['case']['platform'])
                self.assertEqual(['native-path' if d['case']['scenario'] == 'S1' else 'router', 'focal-receive', 'focal-send'], [c['label'] for c in d['commands']])
                for c in d['commands'][1:]:
                    self.assertEqual(d['focal_config'], c['config'])
                if index % 2:
                    self.assertNotEqual(inventory[index-1]['fixture']['controller'], d['focal_config']['controller'])
                    self.assertEqual(inventory[index-1]['seed'], d['case']['seed'])

    def test_fresh_attempt_required_before_any_external_command(self):
        proc = subprocess.run(['python3', str(HERE / 'run-mac-v2.py')], capture_output=True, text=True)
        self.assertNotEqual(0, proc.returncode)
        self.assertIn('--attempt', proc.stderr)

    def test_clock_inconsistency_is_rejected_even_with_successful_observer(self):
        data = {'hosts': {h: {'consistent': True, 'samples': [1], 'intersection_lower_ns': 0, 'intersection_upper_ns': 1} for h in ('m4mini-eth', 'mbp128', 'minimax')}}
        runner.validate_clock(data)
        data['hosts']['minimax']['consistent'] = False
        with self.assertRaises(ValueError):
            runner.validate_clock(data)

    def test_old_or_invalid_attempt_cannot_select_original_evidence(self):
        for attempt in ['q2-mac-20260927', '../q2-mac-20260927', 'anything']:
            with self.subTest(attempt=attempt), self.assertRaises(ValueError):
                runner.configure(attempt)


if __name__ == '__main__':
    unittest.main()
