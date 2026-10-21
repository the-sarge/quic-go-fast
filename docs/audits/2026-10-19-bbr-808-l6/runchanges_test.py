#!/usr/bin/env python3
"""run.py is exactly #777's run.py (71e88789) with runchanges.py's substitutions (#792's test, reading #777's file at its commit)."""
import pathlib
import subprocess
import sys
import unittest

sys.dont_write_bytecode = True
HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import runchanges  # noqa: E402


class RunChanges(unittest.TestCase):
    def test_run_is_777_with_recorded_changes(self):
        self.assertEqual((HERE / 'run.py').read_text(), runchanges.generate())

    def test_777_run_is_its_record(self):
        rel = 'docs/audits/2026-10-17-bbr-l6-coexistence/run.py'
        frozen = subprocess.check_output(['git', 'show', f'71e88789:{rel}'], cwd=HERE, text=True)
        self.assertEqual(runchanges.l6_run(), frozen)


if __name__ == '__main__':
    unittest.main()
