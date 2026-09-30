"""Actual stream/exclusive-receipt regression; no cloud or native tracing claim."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


old = load('diagnostic_original', 'run-router-diagnostic.py')
new = load('diagnostic_corrected', 'run-router-diagnostic-v2.py')


class StagingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.stage = self.root / 'remote'
        self.stage.mkdir()
        # Execute the generated shell command with a real stdin stream. This
        # models transport delivery, not SSH authentication or cloud availability.
        shim = self.root / 'transport.py'
        shim.write_text('import subprocess,sys\nassert sys.argv[1]=="bbr-gateway"\nassert sys.argv[2].startswith("--command=")\nraise SystemExit(subprocess.call(["/bin/sh", "-c", sys.argv[2][10:]]))\n')
        for module in (old, new):
            for key, value in dict(ROOT=self.root, STAGE=str(self.stage), CONTROLLER=self.root,
                                   SSH=[sys.executable, str(shim)]).items():
                mock = patch.object(module.cloud, key, value)
                mock.start()
                self.addCleanup(mock.stop)
        self.gateway = bytes(range(256)) * 4096
        self.helper = (HERE / 'collect-kernel-timers.py').read_bytes()
        (self.root / 'gateway.tar.gz').write_bytes(self.gateway)
        (self.root / 'diagnostic-helper.tar.gz').write_bytes(self.helper)

    def test_original_exact_collision_is_reproduced_without_overwrite(self):
        old.cloud.stage_archive('bbr-gateway', 'gateway.tar.gz')
        receipt = (self.root / 'bbr-gateway-stage.txt').read_bytes()
        outcome = (self.root / 'bbr-gateway-stage-exit.json').read_bytes()
        with self.assertRaises(FileExistsError):
            old.cloud.stage_archive('bbr-gateway', 'diagnostic-helper.tar.gz')
        self.assertEqual((self.root / 'bbr-gateway-stage.txt').read_bytes(), receipt)
        self.assertEqual((self.root / 'bbr-gateway-stage-exit.json').read_bytes(), outcome)
        self.assertFalse((self.stage / 'diagnostic-helper.tar.gz').exists())

    def test_corrected_primary_plus_helper_actual_stream_keeps_both_identities(self):
        new.cloud.stage_archive('bbr-gateway', 'gateway.tar.gz')
        original = (self.root / 'bbr-gateway-stage-exit.json').read_bytes()
        new.stage_helper()
        for name, expected in [('gateway.tar.gz', self.gateway), ('diagnostic-helper.tar.gz', self.helper)]:
            self.assertEqual(hashlib.sha256((self.stage / name).read_bytes()).digest(), hashlib.sha256(expected).digest())
        self.assertEqual((self.root / 'bbr-gateway-stage-exit.json').read_bytes(), original)
        helper_receipt = self.root / 'bbr-gateway-diagnostic-helper-stage-exit.json'
        self.assertEqual(json.loads(helper_receipt.read_text())['exit_code'], 0)
        self.assertLess((json.loads(helper_receipt.read_text())['ended_unix_ns'] - json.loads(helper_receipt.read_text())['started_unix_ns']) / 1e9, 90)
        frozen = helper_receipt.read_bytes()
        with self.assertRaises(FileExistsError):
            new.stage_helper()
        self.assertEqual(helper_receipt.read_bytes(), frozen)
        self.assertEqual((self.stage / 'diagnostic-helper.tar.gz').read_bytes(), self.helper)

    def test_corrected_main_composes_primary_then_helper_and_retains_finalizer(self):
        records = self.root / 'records'
        (records / 'project').mkdir(parents=True)
        launch = dict(expires_at='2099-01-01T00:00:00+00:00')
        (records / 'project' / 'launch.json').write_text(json.dumps(launch))
        calls = []
        actual_local = new.cloud.local
        def local(args, label, bound, stdin_path=None):
            if label == 'apply':
                calls.append('apply')
                return
            self.assertEqual(bound, 90)
            return actual_local(args, label, bound, stdin_path)
        def prepare(stage):
            calls.append('prepare')
            new.cloud.stage_archive('bbr-gateway', 'gateway.tar.gz')
        def require(host, command, directory, label, bound):
            calls.append(label)
            if label == 'helper-identity':
                self.assertEqual((self.stage / 'diagnostic-helper.tar.gz').read_bytes(), self.helper)
        def finish(request, status, error, monitors, clock_started):
            calls.append(('finish', status, error, clock_started))
            return dict(status=status)
        req = dict(project_id='project', hourly_usd=1.3, reserve_usd=.25)
        with patch.object(new, 'preflight', return_value=(req, dict(reservations=[]), dict(diagnostic_helper_sha256=hashlib.sha256(self.helper).hexdigest()))), \
             patch.object(new.cloud, 'RECORDS', records), patch.object(new.cloud, 'local', side_effect=local), \
             patch.object(new.cloud, 'prepare', side_effect=prepare), patch.object(new.cloud, 'require', side_effect=require), \
             patch.object(new.cloud, 'finish', side_effect=finish), patch.object(new, 'window', side_effect=lambda kind,*args: calls.append(kind)), \
             patch.object(new.signal, 'alarm'):
            self.assertEqual(new.main('q2-linux-20260930-router-diagnostic2'), 0)
        self.assertLess(calls.index('prepare'), calls.index('helper-identity'))
        self.assertLess(calls.index('trace-native-probe'), calls.index('untraced'))
        self.assertEqual(calls[-1], ('finish', 'completed', None, True))
        self.assertTrue((self.root / 'bbr-gateway-stage-exit.json').exists())
        self.assertTrue((self.root / 'bbr-gateway-diagnostic-helper-stage-exit.json').exists())

    def test_prior_authorization_cannot_admit_new_attempt(self):
        (self.root / 'request.json').write_text('{}')
        (self.root / 'ledger.json').write_text('{}')
        (self.root / 'operator-authorization.json').write_text(json.dumps(dict(cloud_cap_usd=200)))
        with patch.object(new.cloud, 'configure'):
            with self.assertRaisesRegex(ValueError, 'explicit operator authorization'):
                new.preflight('q2-linux-20260930-router-diagnostic2')
        with self.assertRaisesRegex(ValueError, 'single operator-authorized'):
            new.preflight('q2-linux-20260930-router-diagnostic1')


if __name__ == '__main__':
    unittest.main()
