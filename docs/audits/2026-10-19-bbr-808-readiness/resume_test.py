#!/usr/bin/env python3
"""Tests for resume.py with the runner and quiet-host gate stubbed out (no observation is run)."""
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import matrix as M  # noqa: E402
import resume  # noqa: E402
import run  # noqa: E402


class Resume(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.saved = (run.ART, run.ensure_credentials, M.wait_quiet)
        run.ART, run.ensure_credentials, M.wait_quiet = Path(self.tmp.name), (lambda: None), (lambda: None)
        self.calls = []

    def tearDown(self):
        run.ART, run.ensure_credentials, M.wait_quiet = self.saved
        M.STAGES.pop('fake', None)
        self.tmp.cleanup()

    def stage(self, failing):
        def fake(stage, variant, workload, b, controller, seed=None, **kw):
            self.calls.append((b, workload, variant, controller, seed))
            if (b, workload, variant, controller) in failing:
                raise AssertionError('retain failed attempt; use a fresh phase')
            return dict(id=f'{b}-{workload}-{variant}-{controller}', goodput_mbps=1.0)
        M.STAGES['fake'] = ('main', 'S5', M.FOUR, 3, 9000, fake, {}, None)

    def test_failed_attempt_is_kept_and_every_other_observation_runs(self):
        self.stage({(2, 'stream', 'cand', 'bbrv3')})
        resume.main('fake')
        self.assertEqual(len(self.calls), 3 * 2 * 4)
        self.assertEqual({c[4] for c in self.calls}, {9001, 9002, 9003})
        out = json.loads((run.ART / 'fake-resume-summary.json').read_text())
        self.assertEqual(len(out['failed']), 1)
        self.assertEqual(len(out['rows']), 3 * 2 * 4 - 1)

    def test_mark_writes_a_marker_only_where_the_receipt_is_missing(self):
        for name, files in [('done', ['meta.json', 'config.json', 'receipt.json']), ('cut', ['meta.json', 'config.json'])]:
            d = run.ART / 'observations' / name
            d.mkdir(parents=True)
            for f in files:
                (d / f).write_text('{"exit_codes": [0, 0]}' if f == 'receipt.json' else '{}')
        self.assertEqual(resume.mark(), ['observations/cut'])
        marker = json.loads((run.ART / 'observations/cut/receipt.json').read_text())
        self.assertIsNone(marker['exit_codes'])
        self.assertEqual(json.loads((run.ART / 'observations/done/receipt.json').read_text()), {'exit_codes': [0, 0]})
        self.assertEqual(resume.mark(), [])

    def test_systematic_failure_stops(self):
        self.stage({(b, w, 'cand', 'bbrv3') for b in (1, 2, 3) for w in ('stream', 'datagram')})
        with self.assertRaises(SystemExit):
            resume.main('fake')


if __name__ == '__main__':
    unittest.main()
