"""Finite unit-source provenance checks using disposable git fixtures."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

WRAPPER = Path(__file__).with_name("http-capture.py").resolve()


class UnitCaptureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.git("init", "-b", "fixture")
        self.git("config", "core.autocrlf", "false")
        self.git("config", "core.hooksPath", ".git/no-hooks")
        (self.repo / "unit.go").write_text("package fixture\n", encoding="utf-8")
        integration = self.repo / "integrationtests"
        integration.mkdir()
        (integration / "large.txt").write_text("evidence\n" * 100000, encoding="utf-8")
        self.git("add", ".")
        self.git("-c", "user.name=Capture", "-c", "user.email=capture@example.invalid", "commit", "-m", "fixture")
        self.git("rm", "-r", "integrationtests")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, stderr=subprocess.STDOUT)

    def capture(self):
        destination = self.root / "capture"
        result = subprocess.run(
            [sys.executable, str(WRAPPER), "--unit-source", "--root", str(destination), "run", "--",
             sys.executable, "-c", "import sys; sys.stdout.buffer.write(b'fixture output\\n'); sys.exit(7)"],
            cwd=self.repo, capture_output=True, timeout=45,
        )
        self.assertEqual(result.returncode, 7, result.stderr)
        self.assertEqual(result.stdout, b"fixture output\n")
        return destination / "slot-0", result

    def test_deleted_integrations_are_explicit(self):
        slot, _ = self.capture()
        metadata = json.loads((slot / "run.json").read_text())
        self.assertEqual(metadata["source_scope"]["excluded_paths"], ["integrationtests"])
        self.assertTrue(metadata["source_complete"])
        self.assertEqual((slot / "source.patch").read_bytes(), b"")
        self.assertEqual(metadata["status"]["output"], "")
        result = json.loads((slot / "result.json").read_text())
        self.assertEqual(result["exit"], 7)
        self.assertFalse(result["truncated"])
        self.assertEqual(result["errors"], [])
        for line in (slot / "SHA256SUMS").read_text().splitlines():
            digest, name = line.split("  ", 1)
            self.assertEqual(hashlib.sha256((slot / name).read_bytes()).hexdigest(), digest)

    def test_relevant_dirty_and_untracked_source_remains_visible(self):
        (self.repo / "unit.go").write_text("package changed\n", encoding="utf-8")
        (self.repo / "report.xml").write_text("report", encoding="utf-8")
        slot, _ = self.capture()
        metadata = json.loads((slot / "run.json").read_text())
        self.assertFalse(metadata["source_complete"])
        self.assertIn("unit.go", metadata["status"]["output"])
        patch = (slot / "source.patch").read_text()
        self.assertIn("package changed", patch)
        self.assertNotIn("integrationtests", patch)

    def test_present_integrations_report_incomplete_capture(self):
        (self.repo / "integrationtests").mkdir()
        slot, result = self.capture()
        self.assertIn(b"integrationtests must be absent", result.stderr)
        self.assertFalse((slot / "run.json").exists())
        self.assertTrue(json.loads((slot / "result.json").read_text())["errors"])


if __name__ == "__main__":
    unittest.main()
