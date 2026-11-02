import importlib.util
import json
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location("peer_run", Path(__file__).with_name("run.py"))
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


class AccountingTests(unittest.TestCase):
    def events(self, cases, action="pass"):
        return [json.dumps({"Action": action, "Package": runner.PACKAGE, "Test": case}) for case in cases]

    def test_both_roles_are_required(self):
        runner.check_cases(self.events(runner.EXPECTED))
        with self.assertRaisesRegex(ValueError, "missing"):
            runner.check_cases(self.events(runner.EXPECTED[:1]))

    def test_skipped_expected_case_fails(self):
        with self.assertRaisesRegex(ValueError, "skip"):
            runner.check_cases(self.events(runner.EXPECTED, "skip"))

    def test_no_tests_and_duplicate_passes_fail(self):
        for events in ([], self.events(runner.EXPECTED + runner.EXPECTED)):
            with self.subTest(events=events), self.assertRaises(ValueError):
                runner.check_cases(events)

    def test_unrelated_package_cannot_supply_evidence(self):
        events = [json.dumps({"Action": "pass", "Package": "other", "Test": case}) for case in runner.EXPECTED]
        with self.assertRaisesRegex(ValueError, "missing"):
            runner.check_cases(events)

    def test_missing_prerequisite_fails_before_execution(self):
        with self.assertRaisesRegex(ValueError, "binary"):
            runner.check_prerequisites("/nonexistent/r1-peer", "linux", "x86_64")
        with self.assertRaisesRegex(ValueError, "Linux amd64"):
            runner.check_prerequisites(__file__, "darwin", "arm64")


if __name__ == "__main__":
    unittest.main()
