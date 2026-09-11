"""Collector lifecycle regressions. All host effects are mocked."""

import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import Mock, call, mock_open, patch


spec = importlib.util.spec_from_file_location("burst_collect", Path(__file__).with_name("collect.py"))
collect = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collect)
POPEN = subprocess.Popen


class CollectorLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.commands = []
        self.receipts = {}
        self.processes = []
        self.socket_ready = False
        self.setup_error = None
        self.cleanup_error = None
        self.receipt_error = None
        self.precheck_error = None
        self.start_error = None
        self.configure_process = lambda process, role: None

        def check_output(command, **kwargs):
            if command[0] == "mpstat":
                if self.precheck_error:
                    raise self.precheck_error
                return "mock precheck"
            action = command[-1]
            self.commands.append(action)
            error = self.setup_error if action == "setup" else self.cleanup_error if action == "cleanup" else None
            if error:
                raise error
            return '{"mock_isolation": "' + action + '"}'

        def write_text(path, text):
            if path.name == self.receipt_error:
                raise OSError("cannot persist " + path.name)
            self.receipts[path.name] = text

        def popen(command, **kwargs):
            role = "monitor" if command[0] == "mpstat" else "pacer" if "QUEUE_PACER=1" in command else "receiver"
            if role == self.start_error:
                raise OSError("cannot start " + role)
            process = Mock(spec=POPEN, args=command, returncode=0)
            process.poll.return_value = None if role == "monitor" else 0
            process.wait.return_value = 0

            def communicate(**kwargs):
                self.socket_ready = False
                return "mock output", ""

            process.communicate.side_effect = communicate
            self.socket_ready = role == "receiver"
            self.configure_process(process, role)
            self.processes.append((role, process))
            return process

        self.enterContext(patch.object(collect.Path, "resolve", lambda path: path))
        self.enterContext(patch.object(collect.Path, "mkdir"))
        self.files = self.enterContext(patch.object(collect.Path, "open", mock_open()))
        self.enterContext(patch.object(collect.Path, "write_text", write_text))
        self.enterContext(patch.object(collect.Path, "read_text", return_value="1000"))
        self.enterContext(patch.object(collect.Path, "exists", lambda path: self.socket_ready))
        self.enterContext(patch.object(collect.signal, "signal"))
        self.enterContext(patch.object(collect.time, "sleep"))
        self.enterContext(patch.object(collect.subprocess, "check_output", side_effect=check_output))
        self.enterContext(patch.object(collect.subprocess, "Popen", side_effect=popen))
        self.enterContext(patch("sys.argv", ["collect.py", "/mock-experiment", "--smoke"]))
        self.enterContext(patch("builtins.print"))

    def test_initial_receipt_failure_still_cleans_up_once(self):
        self.receipt_error = "isolation-before.json"
        with self.assertRaisesRegex(OSError, "isolation-before.json"):
            collect.main()
        self.assertEqual(self.commands, ["setup", "cleanup"])
        self.assertNotIn("isolation-before.json", self.receipts)
        self.assertIn("isolation-after.json", self.receipts)

    def test_receiver_timeout_escalates_and_tears_down_pacer_and_monitor(self):
        def configure(process, role):
            if role == "receiver" and "QUEUE_PACING=external" in process.args:
                process.communicate.side_effect = subprocess.TimeoutExpired("receiver communication", 20)
                process.poll.return_value = None
                process.wait.side_effect = [subprocess.TimeoutExpired("receiver teardown", 10), 0]
            elif role == "pacer":
                process.poll.return_value = None

        self.configure_process = configure
        with self.assertRaisesRegex(Exception, "receiver communication"):
            collect.main()
        self.assertEqual(self.commands.count("cleanup"), 1)
        receiver = [p for role, p in self.processes if role == "receiver"][-1]
        self.assertEqual(receiver.method_calls[-4:], [call.terminate(), call.wait(timeout=10), call.kill(), call.wait(timeout=10)])
        for role, process in self.processes:
            if role in ("monitor", "pacer"):
                process.terminate.assert_called_once()
                process.wait.assert_called_once_with(timeout=10)

    def test_monitor_timeout_has_bounded_kill_fallback(self):
        self.precheck_error = OSError("precheck failed")

        def configure(process, role):
            process.wait.side_effect = [subprocess.TimeoutExpired("monitor teardown", 10), 0]

        self.configure_process = configure
        with self.assertRaisesRegex(RuntimeError, "precheck failed") as result:
            collect.main()
        self.assertIn("monitor wait after terminate", str(result.exception))
        monitor = self.processes[0][1]
        self.assertEqual(monitor.method_calls, [call.poll(), call.terminate(), call.wait(timeout=10), call.kill(), call.wait(timeout=10)])
        self.assertEqual(self.commands, ["setup", "cleanup"])

    def test_individual_teardown_errors_preserve_other_teardown_and_cleanup(self):
        for failing_role in ("receiver", "pacer", "monitor"):
            for operation in ("poll", "terminate", "wait", "kill", "final_wait"):
                with self.subTest(role=failing_role, operation=operation):
                    self.commands.clear()
                    self.processes.clear()
                    self.receipts.clear()
                    self.socket_ready = False

                    def configure(process, role):
                        # Reach the first external pair so both children exist.
                        if role == "receiver" and "QUEUE_PACING=internal" in process.args:
                            return
                        process.poll.return_value = None
                        if role == "receiver":
                            process.communicate.side_effect = OSError("initiating collection failure")
                        if role == failing_role:
                            error = OSError(f"injected {failing_role} {operation}")
                            if operation in ("kill", "final_wait"):
                                process.wait.side_effect = [subprocess.TimeoutExpired(role, 10), error if operation == "final_wait" else 0]
                                if operation == "kill":
                                    process.kill.side_effect = error
                            else:
                                getattr(process, operation).side_effect = [error, 0] if operation == "wait" else error

                    self.configure_process = configure
                    with self.assertRaisesRegex(RuntimeError, "initiating collection failure") as result:
                        collect.main()
                    self.assertIn(f"injected {failing_role} {operation}", str(result.exception))
                    self.assertIsInstance(result.exception.__cause__, OSError)
                    self.assertEqual(self.commands.count("cleanup"), 1)
                    live_processes = [(role, p) for role, p in self.processes if role == "monitor" or "QUEUE_PACING=external" in p.args]
                    self.assertEqual(len(live_processes), 3)
                    for role, process in live_processes:
                        process.terminate.assert_called_once()
                        self.assertTrue(process.wait.called)
                        for wait in process.wait.call_args_list:
                            self.assertEqual(wait, call(timeout=10))

    def test_normal_lifecycle_cleans_up_once(self):
        collect.main()
        self.assertEqual(self.commands.count("setup"), 1)
        self.assertEqual(self.commands.count("cleanup"), 1)
        self.assertEqual(self.receipts["isolation-before.json"], '{"mock_isolation": "setup"}\n')
        self.assertEqual(self.receipts["isolation-after.json"], '{"mock_isolation": "cleanup"}\n')
        self.assertEqual(len([p for role, p in self.processes if role == "receiver"]), 12)
        self.assertEqual(len([p for role, p in self.processes if role == "pacer"]), 6)

    def test_setup_failure_leaves_partial_rollback_to_helper(self):
        self.setup_error = OSError("setup failed")
        with self.assertRaisesRegex(OSError, "setup failed"):
            collect.main()
        self.assertEqual(self.commands, ["setup"])
        self.assertEqual(self.receipts, {})
        self.assertEqual(self.processes, [])

    def test_manifest_open_failure_never_attempts_setup(self):
        self.files.side_effect = OSError("manifest exists")
        with self.assertRaisesRegex(OSError, "manifest exists"):
            collect.main()
        self.assertEqual(self.commands, [])
        self.assertEqual(self.receipts, {})

    def test_cleanup_failure_is_not_recorded_as_restoration(self):
        self.cleanup_error = OSError("cleanup failed")
        with self.assertRaisesRegex(RuntimeError, "restoration unconfirmed") as result:
            collect.main()
        self.assertIn("cleanup failed", str(result.exception))
        self.assertNotIn("cleanup confirmed", str(result.exception))
        self.assertEqual(self.commands.count("cleanup"), 1)
        self.assertNotIn("isolation-after.json", self.receipts)

    def test_final_receipt_failure_is_reported_after_cleanup(self):
        self.receipt_error = "isolation-after.json"
        with self.assertRaisesRegex(RuntimeError, "cannot persist isolation-after.json"):
            collect.main()
        self.assertEqual(self.commands.count("cleanup"), 1)
        self.assertNotIn("isolation-after.json", self.receipts)

    def test_initiating_teardown_and_cleanup_failures_are_all_reported(self):
        self.precheck_error = OSError("precheck failed")
        self.cleanup_error = OSError("cleanup failed")

        def configure(process, role):
            process.terminate.side_effect = OSError("termination failed")

        self.configure_process = configure
        with self.assertRaises(RuntimeError) as result:
            collect.main()
        for diagnostic in ("precheck failed", "termination failed", "cleanup failed", "restoration unconfirmed"):
            self.assertIn(diagnostic, str(result.exception))
        self.assertIs(result.exception.__cause__, self.precheck_error)
        self.assertEqual(self.commands, ["setup", "cleanup"])
        self.assertNotIn("isolation-after.json", self.receipts)

    def test_sample_persistence_failure_still_cleans_up(self):
        self.files.return_value.write.side_effect = OSError("sample write failed")
        with self.assertRaisesRegex(OSError, "sample write failed"):
            collect.main()
        self.assertEqual(self.commands.count("cleanup"), 1)
        self.processes[0][1].wait.assert_called_once_with(timeout=10)

    def test_process_start_failure_still_cleans_up(self):
        for role in ("monitor", "receiver", "pacer"):
            with self.subTest(role=role):
                self.commands.clear()
                self.processes.clear()
                self.socket_ready = False
                self.start_error = role
                with self.assertRaisesRegex(OSError, "cannot start " + role):
                    collect.main()
                self.assertEqual(self.commands.count("cleanup"), 1)

    def test_keyboard_interrupt_still_cleans_up(self):
        self.precheck_error = KeyboardInterrupt("interrupted")
        with self.assertRaises(KeyboardInterrupt):
            collect.main()
        self.assertEqual(self.commands, ["setup", "cleanup"])


if __name__ == "__main__":
    unittest.main()
