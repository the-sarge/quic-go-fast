"""Finite diagnostic admission/disposition checks; no native Linux tracing claim."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


diagnostic = load('diagnostic', 'run-router-diagnostic.py')
collector = load('collector', 'collect-kernel-timers.py')


class DiagnosticTests(unittest.TestCase):
    def test_real_failed_calibration_is_observation_but_corruption_still_stops(self):
        root = Path('/Users/josh/.local/share/infra/bbr-q635-controller/.local/cloud/bbr-campaign/q2-linux-20260929-lease01-continuation4/case-39')
        commands = json.loads((root / 'commands.json').read_text())
        outcomes = {c['label']: (json.loads((root / (c['label'] + '-exit.json')).read_text())['exit_code'],
            json.loads((root / (c['label'] + '.stdout')).read_text())) for c in commands['commands']}
        self.assertEqual(diagnostic.diagnostic_errors(commands, outcomes), [])
        self.assertTrue(diagnostic.cloud.validate(commands, outcomes))
        changed = copy.deepcopy(outcomes)
        changed['router'][1]['Observations'][0]['SocketDrops'] = 1
        self.assertIn('gateway packet/timestamp/error gate', diagnostic.diagnostic_errors(commands, changed))
        changed = copy.deepcopy(outcomes)
        changed['focal-receive'][1]['result']['receiver']['corrupt'] = 1
        self.assertTrue(diagnostic.diagnostic_errors(commands, changed))

    def test_router_exit_cannot_hide_failed_gate(self):
        root = Path('/Users/josh/.local/share/infra/bbr-q635-controller/.local/cloud/bbr-campaign/q2-linux-20260929-lease01-continuation4/case-39')
        commands = json.loads((root / 'commands.json').read_text())
        outcomes = {c['label']: (json.loads((root / (c['label'] + '-exit.json')).read_text())['exit_code'],
            json.loads((root / (c['label'] + '.stdout')).read_text())) for c in commands['commands']}
        outcomes['router'] = (0, outcomes['router'][1])
        self.assertIn('router exit does not match unchanged timing observation', diagnostic.diagnostic_errors(commands, outcomes))

    def test_finite_windows_always_retain_full_cleanup_reserve(self):
        with patch.object(diagnostic.time, 'time', return_value=1000):
            diagnostic.check_time(2211, 2)
            with self.assertRaisesRegex(ValueError, 'immutable expiry'):
                diagnostic.check_time(2210, 2)

    def test_loss_counters_and_foreign_instance_rejection(self):
        self.assertEqual(collector.losses('overrun: 2\ncommit overrun: 0\ndropped events: 1\n'),
                         {'overrun': 2, 'commit overrun': 0, 'dropped events': 1})
        with self.assertRaisesRegex(ValueError, 'identity'):
            collector.main('unrelated', Path('/tmp/unrelated'), True)


if __name__ == '__main__':
    unittest.main()
