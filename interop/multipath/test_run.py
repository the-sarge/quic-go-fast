import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest


sys.dont_write_bytecode = True
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

    def test_matrix_requires_all_eight_scenarios_in_both_roles(self):
        scenarios = (
            "useful-transfer", "absent-negotiation", "path-zero-ack",
            "key-update-silent-path", "status-backup-failover",
            "abandon-reopen-late-feedback", "same-id-migration-rebinding",
            "unequal-mtu-invalid-frame",
        )
        required = [f"TestPicoquicMultipathMatrix/{scenario}/{role}"
                    for scenario in scenarios for role in ("client", "server")]
        self.assertEqual(runner.MATRIX_EXPECTED, required)
        events = [json.dumps({"Action": "pass", "Package": runner.SMOKE_PACKAGE, "Test": case})
                  for case in required]
        runner.check_cases(events, runner.SMOKE_PACKAGE, runner.MATRIX_EXPECTED)
        for missing in (events[:1], events[:-1], events + events[:1]):
            with self.subTest(events=missing), self.assertRaisesRegex(ValueError, "missing or duplicate"):
                runner.check_cases(missing, runner.SMOKE_PACKAGE, runner.MATRIX_EXPECTED)
        with self.assertRaisesRegex(ValueError, "skip"):
            runner.check_cases(events + [json.dumps({"Action": "skip", "Package": runner.SMOKE_PACKAGE,
                                                  "Test": required[0]})],
                               runner.SMOKE_PACKAGE, runner.MATRIX_EXPECTED)

    def test_matrix_invocation_runs_one_exact_role_with_the_existing_timeout(self):
        command, expected = runner.matrix_invocation("path-zero-ack", "server")
        self.assertEqual(expected, ["TestPicoquicMultipathMatrix/path-zero-ack/server"])
        self.assertIn("-run=^TestPicoquicMultipathMatrix$/^path-zero-ack$/^server$", command)
        self.assertIn("-timeout=90s", command)
        for scenario, role in (("unknown", "server"), ("path-zero-ack", "other")):
            with self.subTest(scenario=scenario, role=role), self.assertRaises(ValueError):
                runner.matrix_invocation(scenario, role)

    def test_aggregate_requires_case_logs_and_one_candidate_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for case in runner.MATRIX_EXPECTED:
                _, scenario, role = case.split("/")
                directory = root / scenario / role
                directory.mkdir(parents=True)
                (directory / "go-test.jsonl").write_text(json.dumps({"Action": "pass", "Package": runner.SMOKE_PACKAGE, "Test": case}) + "\n")
                (directory / "result.json").write_text(json.dumps({"status": "pass", "executed": [case], "identity": {"candidate": "a" * 40}}))
            with self.assertRaisesRegex(ValueError, "zero"):
                runner.check_matrix(root)
            zero = root / "negotiated-zero"
            zero.mkdir()
            (zero / "go-test.jsonl").write_text("\n".join(json.dumps({"Action": "pass", "Package": runner.SMOKE_PACKAGE, "Test": case}) for case in runner.ZERO_EXPECTED))
            (zero / "result.json").write_text(json.dumps({"status": "pass", "executed": runner.ZERO_EXPECTED, "identity": {"candidate": "a" * 40}}))
            self.assertEqual(runner.check_matrix(root)["executed"], runner.MATRIX_EXPECTED)
            case = root / "path-zero-ack" / "server"
            (case / "result.json").write_text(json.dumps({"status": "pass", "executed": ["TestPicoquicMultipathMatrix/path-zero-ack/server"], "identity": {"candidate": "b" * 40}}))
            with self.assertRaisesRegex(ValueError, "identity"):
                runner.check_matrix(root)
            (case / "go-test.jsonl").unlink()
            with self.assertRaisesRegex(ValueError, "missing"):
                runner.check_matrix(root)


if __name__ == "__main__":
    unittest.main()
