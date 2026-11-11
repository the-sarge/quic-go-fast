import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location("native_qualification", Path(__file__).with_name("native_qualification.py"))
driver = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(driver)

INJECTED_LINE = '100 sendmsg(7, {}, 0) = -1 EAGAIN (Resource temporarily unavailable) (INJECTED)\n'
EPERM_LINE = '100 sendmsg(7, {}, 0) = -1 EPERM (Operation not permitted)\n'
SIGNAL_LINE = '101 --- SIGURG {si_signo=SIGURG} ---\n'


class OutputTests(unittest.TestCase):
    def test_exactly_one_pass_is_required(self):
        driver.check_output("expired-before-dispatch", "--- PASS: TestNativeQualification/expired-before-dispatch (0.10s)\nPASS\n")
        for output in ("PASS\n", "--- PASS: TestNativeQualification/queued-expiry (0.10s)\nPASS\n"):
            with self.subTest(output=output), self.assertRaisesRegex(ValueError, "exactly once"):
                driver.check_output("expired-before-dispatch", output)
        line = "--- PASS: TestNativeQualification/expired-before-dispatch (0.10s)\n"
        with self.assertRaisesRegex(ValueError, "exactly once"):
            driver.check_output("expired-before-dispatch", line + line + "PASS\n")

    def test_skip_or_failure_fails(self):
        for word in ("SKIP", "FAIL"):
            with self.subTest(word=word), self.assertRaisesRegex(ValueError, word):
                driver.check_output("shutdown-join", f"--- {word}: TestNativeQualification/shutdown-join (0.00s)\n")


class TraceTests(unittest.TestCase):
    def test_eagain_classes_need_an_injected_call(self):
        driver.check_trace("eagain-to-deadline", INJECTED_LINE + SIGNAL_LINE)
        with self.assertRaisesRegex(ValueError, "injected"):
            driver.check_trace("eagain-to-deadline", SIGNAL_LINE)
        with self.assertRaisesRegex(ValueError, "injected"):
            driver.check_trace("queued-expiry", "100 sendmsg(7, {}, 0) = 8\n")

    def test_retry_class_needs_real_eperm_then_two_injected_calls(self):
        driver.check_trace("retry-fallback", EPERM_LINE + INJECTED_LINE + INJECTED_LINE)
        for trace in (INJECTED_LINE * 3, EPERM_LINE + INJECTED_LINE, EPERM_LINE * 3):
            with self.subTest(trace=trace), self.assertRaisesRegex(ValueError, "retry"):
                driver.check_trace("retry-fallback", trace)
        injected_eperm = EPERM_LINE.rstrip() + " (INJECTED)\n"
        with self.assertRaisesRegex(ValueError, "retry"):
            driver.check_trace("retry-fallback", injected_eperm + INJECTED_LINE + INJECTED_LINE)

    def test_split_call_counts_once_with_its_result(self):
        split = '100 sendmsg(7, {}, 0 <unfinished ...>\n101 --- SIGURG {} ---\n100 <... sendmsg resumed>) = -1 EAGAIN (Resource temporarily unavailable) (INJECTED)\n'
        driver.check_trace("retry-fallback", EPERM_LINE + split + INJECTED_LINE)
        with self.assertRaisesRegex(ValueError, "retry"):
            driver.check_trace("retry-fallback", EPERM_LINE + split)

    def test_uninstrumented_class_has_no_trace(self):
        with self.assertRaisesRegex(ValueError, "no injection"):
            driver.check_trace("shutdown-join", INJECTED_LINE)
        driver.check_trace("shutdown-join", "")


class EvidenceTests(unittest.TestCase):
    def evidence(self, name, **facts):
        return {"class": name, "go": "go1.27.4", "traversal": True, "timing_ms": dict.fromkeys(driver.CLASSES[name]["timing"], 1.5)} | facts

    def test_toolchain_and_class_must_match(self):
        driver.check_evidence("eagain-to-deadline", "go1.27.4", self.evidence("eagain-to-deadline"))
        with self.assertRaisesRegex(ValueError, "toolchain"):
            driver.check_evidence("eagain-to-deadline", "go1.26.11", self.evidence("eagain-to-deadline"))
        with self.assertRaisesRegex(ValueError, "class"):
            driver.check_evidence("eagain-to-deadline", "go1.27.4", self.evidence("queued-expiry"))

    def test_injected_class_needs_traversal(self):
        with self.assertRaisesRegex(ValueError, "traversal"):
            driver.check_evidence("eagain-to-deadline", "go1.27.4", self.evidence("eagain-to-deadline", traversal=False))

    def test_tolerance_binds_only_uninstrumented_timing(self):
        def timing(name, value):
            return self.evidence(name, timing_ms=dict.fromkeys(driver.CLASSES[name]["timing"], value))
        with self.assertRaisesRegex(ValueError, "tolerance"):
            driver.check_evidence("expired-before-dispatch", "go1.27.4", timing("expired-before-dispatch", 20.5))
        driver.check_evidence("expired-before-dispatch", "go1.27.4", timing("expired-before-dispatch", 20.0))
        driver.check_evidence("eagain-to-deadline", "go1.27.4", timing("eagain-to-deadline", 20.5))

    def test_missing_timing_fails(self):
        with self.assertRaisesRegex(ValueError, "timing"):
            driver.check_evidence("shutdown-join", "go1.27.4", self.evidence("shutdown-join", timing_ms={}))

    def test_partial_timing_fails(self):
        partial = self.evidence("request-contention")
        del partial["timing_ms"]["pressure_publication"]
        with self.assertRaisesRegex(ValueError, "pressure_publication"):
            driver.check_evidence("request-contention", "go1.27.4", partial)


class AccountingTests(unittest.TestCase):
    def instances(self):
        return [(go, name) for go in driver.GO_VERSIONS for name in driver.CLASSES]

    def test_twelve_fixed_instances(self):
        self.assertEqual(len(self.instances()), 12)
        driver.check_accounting(self.instances())

    def test_missing_or_duplicate_instance_fails(self):
        instances = self.instances()
        for bad in (instances[1:], instances + instances[:1], []):
            with self.subTest(count=len(bad)), self.assertRaisesRegex(ValueError, "instances"):
                driver.check_accounting(bad)


class CommandTests(unittest.TestCase):
    def test_injection_rules(self):
        eagain = " ".join(driver.command_for("eagain-to-deadline", "bin", "trace"))
        self.assertIn("--seccomp-bpf", eagain)
        self.assertIn("inject=sendmsg:error=EAGAIN ", eagain + " ")
        retry = " ".join(driver.command_for("retry-fallback", "bin", "trace"))
        self.assertIn("inject=sendmsg:error=EAGAIN:when=2+", retry)
        self.assertIn("unshare -n", retry)
        self.assertIn("meta l4proto udp drop", retry)

    def test_uninstrumented_classes_have_no_tracer(self):
        for name in ("expired-before-dispatch", "shutdown-join", "request-contention"):
            command = driver.command_for(name, "bin", "trace")
            self.assertNotIn("strace", " ".join(command))
            self.assertEqual(command[0], "bin")

    def test_namespaced_class_passes_evidence_through_sudo(self):
        command = driver.with_evidence(driver.command_for("retry-fallback", "bin", "trace"), "out.json")
        self.assertEqual(command[:4], ["sudo", "-n", "env", "NATIVE_QUAL_EVIDENCE=out.json"])
        self.assertEqual(command[4:6], ["unshare", "-n"])
        plain = driver.command_for("queued-expiry", "bin", "trace")
        self.assertEqual(driver.with_evidence(plain, "out.json"), plain)

    def test_selector_names_exactly_one_class(self):
        self.assertIn("-test.run=^TestNativeQualification$/^queued-expiry$", driver.command_for("queued-expiry", "bin", "trace"))

    def test_missing_prerequisite_fails_before_execution(self):
        with self.assertRaisesRegex(ValueError, "Linux amd64"):
            driver.check_prerequisites("darwin", "arm64", lambda _: "/bin/x")
        with self.assertRaisesRegex(ValueError, "strace"):
            driver.check_prerequisites("linux", "x86_64", lambda tool: None if tool == "strace" else "/bin/x")


if __name__ == "__main__":
    unittest.main()
