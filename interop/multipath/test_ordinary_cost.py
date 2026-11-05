import copy
import unittest
import tempfile
import contextlib
import io
import subprocess
import json
from pathlib import Path
from unittest import mock

import ordinary_cost as driver


def flow(label="ordinary", bytes_per_second=1000, control_p99=1000.0):
    return {
        "label": label,
        "series": [bytes_per_second] * driver.WINDOW_S,
        "useful_bytes": bytes_per_second * driver.WINDOW_S,
        "control": {"count": 6000, "p99_us": control_p99} if label == "ordinary" else None,
        "timeout_close": False,
        "close_cause": "",
    }


def observation(cell="stream-gso-on", profile="unregistered", round_=0, **changes):
    record = {
        "cell": cell,
        "profile": profile,
        "round": round_,
        "seed": driver.BASE_SEED + 100 * list(driver.CELLS).index(cell) + round_,
        "mode": "campaign",
        "contaminated": False,
        "server": {
            "go": driver.GO_VERSION,
            "granted": profile == "opted-in",
            "gso_capable": cell not in ("stream-gso-off", "datagram-batch-off"),
            "window_cpu_s": 60.0,
            "rss_kb_at_20s": 1000,
            "rss_kb_end": 1000,
            "write_errors": 0,
            "joined": profile == "opted-in",
            "application_send_call_boundary": driver.METRIC_BOUNDARIES["application_send_call"],
            "application_send_call": {"ordinary": {"count": 6000, "p99_us": 1000.0}},
            "connections": [{"label": "ordinary", "timeout_close": False, "close_cause": ""}],
        },
        "client": {"go": driver.GO_VERSION, "flows": [flow()]},
    }
    if cell == "mixed-bulk":
        if profile == "opted-in":
            background = flow("background", 2000)
            background["multipath_negotiated"] = True
            background["paths"] = ["live", "live"]
            record["client"]["flows"].append(background)
        else:
            record["client"]["flows"] += [flow("background"), flow("background")]
    if cell == "stateless-pressure":
        record["client"]["flood"] = {"sent": 600000, "resets": 300000}
    record.update(changes)
    return record


def campaign():
    return [observation(cell, profile, round_) for cell, round_, profile, _ in driver.schedule()]


def pair_rounds(cell="stream-gso-on", opted=None):
    """Three matched rounds; opted edits each opted-in record."""
    pairs = []
    for round_ in range(driver.ROUNDS):
        control = observation(cell, "unregistered", round_)
        treated = observation(cell, "opted-in", round_)
        if opted:
            opted(treated)
        pairs.append((control, treated))
    return pairs


def scale_goodput(record, factor):
    for item in record["client"]["flows"]:
        if item["label"] == "ordinary":
            item["series"] = [int(value * factor) for value in item["series"]]
            item["useful_bytes"] = sum(item["series"])


class ScheduleTests(unittest.TestCase):
    def test_thirty_six_fixed_instances(self):
        instances = driver.schedule()
        self.assertEqual(len(instances), 36)
        self.assertEqual(len({instance[:3] for instance in instances}), 36)
        self.assertEqual(len(driver.CELLS), 6)

    def test_profile_order_alternates_by_round(self):
        for round_ in range(driver.ROUNDS):
            pair = [i for i in driver.schedule() if i[0] == "mixed-bulk" and i[1] == round_]
            first = "unregistered" if round_ % 2 == 0 else "opted-in"
            self.assertEqual(pair[0][2], first)

    def test_pair_shares_its_seed_and_rounds_differ(self):
        seeds = {}
        for cell, round_, profile, seed in driver.schedule():
            seeds.setdefault((cell, round_), set()).add(seed)
        self.assertTrue(all(len(pair) == 1 for pair in seeds.values()))
        self.assertEqual(len({next(iter(pair)) for pair in seeds.values()}), 18)

    def test_only_off_arms_disable_acceleration(self):
        disabled = {name for name, cell in driver.CELLS.items() if cell["env"]}
        self.assertEqual(disabled, {"stream-gso-off", "datagram-batch-off"})


class AccountingTests(unittest.TestCase):
    def test_complete_campaign_passes(self):
        driver.check_accounting(campaign())

    def test_missing_or_duplicate_instance_fails(self):
        records = campaign()
        for bad in (records[1:], records + records[:1], []):
            with self.subTest(count=len(bad)), self.assertRaisesRegex(ValueError, "instances"):
                driver.check_accounting(bad)

    def test_wrong_toolchain_fails(self):
        record = observation()
        record["server"]["go"] = "go1.26.10"
        with self.assertRaisesRegex(ValueError, "toolchain"):
            driver.check_observation(record)

    def test_short_series_fails(self):
        record = observation()
        record["client"]["flows"][0]["series"].pop()
        with self.assertRaisesRegex(ValueError, "seconds"):
            driver.check_observation(record)

    def test_useful_bytes_must_match_series(self):
        record = observation()
        record["client"]["flows"][0]["useful_bytes"] += 1
        with self.assertRaisesRegex(ValueError, "useful bytes"):
            driver.check_observation(record)

    def test_profile_must_match_grant(self):
        record = observation(profile="opted-in")
        record["server"]["granted"] = False
        with self.assertRaisesRegex(ValueError, "grant"):
            driver.check_observation(record)
        record = observation(profile="unregistered")
        record["server"]["granted"] = True
        with self.assertRaisesRegex(ValueError, "grant"):
            driver.check_observation(record)

    def test_opted_in_must_return_write_authority(self):
        record = observation(profile="opted-in")
        record["server"]["joined"] = False
        with self.assertRaisesRegex(ValueError, "join"):
            driver.check_observation(record)

    def test_acceleration_must_match_cell(self):
        record = observation("stream-gso-on")
        record["server"]["gso_capable"] = False
        with self.assertRaisesRegex(ValueError, "acceleration"):
            driver.check_observation(record)
        record = observation("stream-gso-off")
        record["server"]["gso_capable"] = True
        with self.assertRaisesRegex(ValueError, "acceleration"):
            driver.check_observation(record)

    def test_mixed_baseline_must_represent_the_workload(self):
        record = observation("mixed-bulk", "unregistered")
        del record["client"]["flows"][2]
        with self.assertRaisesRegex(ValueError, "two independent"):
            driver.check_observation(record)
        record = observation("mixed-bulk", "opted-in")
        record["client"]["flows"][1]["multipath_negotiated"] = False
        with self.assertRaisesRegex(ValueError, "two-path"):
            driver.check_observation(record)
        record = observation("mixed-bulk", "opted-in")
        for paths in (["live"], ["live", "closed"]):
            record["client"]["flows"][1]["paths"] = paths
            with self.subTest(paths=paths), self.assertRaisesRegex(ValueError, "two-path"):
                driver.check_observation(record)

    def test_stateless_cell_must_apply_pressure(self):
        record = observation("stateless-pressure")
        record["client"]["flood"]["sent"] = 0
        with self.assertRaisesRegex(ValueError, "pressure"):
            driver.check_observation(record)

    def test_missing_latency_samples_fail(self):
        record = observation()
        record["client"]["flows"][0]["control"]["count"] = 0
        with self.assertRaisesRegex(ValueError, "control"):
            driver.check_observation(record)
        record = observation()
        record["server"]["application_send_call"]["ordinary"]["count"] = 0
        with self.assertRaisesRegex(ValueError, "application send-call"):
            driver.check_observation(record)


class JudgmentTests(unittest.TestCase):
    def test_equal_profiles_pass(self):
        verdict = driver.judge_cell("stream-gso-on", pair_rounds())
        self.assertTrue(verdict["passed"], verdict["failures"])
        self.assertEqual(verdict["goodput_ratio"]["median"], 1.0)

    def test_goodput_floor_is_ninety_five_percent(self):
        for factor, passes in ((0.95, True), (0.94, False)):
            verdict = driver.judge_cell("stream-gso-on", pair_rounds(opted=lambda r: scale_goodput(r, factor)))
            with self.subTest(factor=factor):
                self.assertEqual(verdict["passed"], passes)

    def test_cpu_ceiling_is_one_hundred_ten_percent(self):
        def cpu(value):
            return lambda record: record["server"].update(window_cpu_s=value)

        self.assertTrue(driver.judge_cell("stream-gso-on", pair_rounds(opted=cpu(66.0)))["passed"])
        self.assertFalse(driver.judge_cell("stream-gso-on", pair_rounds(opted=cpu(66.5)))["passed"])

    def test_cpu_counts_all_useful_bytes_of_the_endpoint(self):
        def more_background(record):
            record["client"]["flows"][1]["series"] = [4000] * driver.WINDOW_S
            record["client"]["flows"][1]["useful_bytes"] = 4000 * driver.WINDOW_S

        verdict = driver.judge_cell("mixed-bulk", pair_rounds("mixed-bulk", more_background))
        # Control delivers 3,000 B/s and opted-in 5,000 B/s for the same CPU.
        self.assertAlmostEqual(verdict["cpu_per_useful_byte_ratio"]["median"], 0.6)

    def test_latency_limit_is_the_larger_allowance(self):
        def latency(value):
            def apply(record):
                record["client"]["flows"][0]["control"]["p99_us"] = value
            return apply

        # A 1,000 us baseline allows baseline + 1 ms, which is the larger.
        self.assertTrue(driver.judge_cell("stream-gso-on", pair_rounds(opted=latency(2000.0)))["passed"])
        self.assertFalse(driver.judge_cell("stream-gso-on", pair_rounds(opted=latency(2001.0)))["passed"])

        def slow_base(rounds):
            for control, _ in rounds:
                control["client"]["flows"][0]["control"]["p99_us"] = 20000.0
            return rounds

        # A 20 ms baseline allows 110%, which is the larger.
        self.assertTrue(driver.judge_cell("stream-gso-on", slow_base(pair_rounds(opted=latency(22000.0))))["passed"])
        self.assertFalse(driver.judge_cell("stream-gso-on", slow_base(pair_rounds(opted=latency(22001.0))))["passed"])

    def test_application_send_call_is_judged_separately(self):
        def slow(record):
            record["server"]["application_send_call"]["ordinary"]["p99_us"] = 5000.0

        verdict = driver.judge_cell("stream-gso-on", pair_rounds(opted=slow))
        self.assertFalse(verdict["passed"])
        self.assertIn("application send-call p99", verdict["failures"][0])

    def test_median_decides_and_range_reports(self):
        rounds = pair_rounds()
        scale_goodput(rounds[0][1], 0.5)
        scale_goodput(rounds[2][1], 1.1)
        verdict = driver.judge_cell("stream-gso-on", rounds)
        self.assertTrue(verdict["passed"], verdict["failures"])
        self.assertEqual(verdict["goodput_ratio"]["range"], [0.5, 1.1])

    def test_timeout_close_fails_in_any_round(self):
        rounds = pair_rounds()
        rounds[1][1]["client"]["flows"][0]["timeout_close"] = True
        verdict = driver.judge_cell("stream-gso-on", rounds)
        self.assertFalse(verdict["passed"])
        self.assertIn("timeout", verdict["failures"][0])
        rounds = pair_rounds()
        rounds[1][1]["server"]["connections"][0]["timeout_close"] = True
        self.assertFalse(driver.judge_cell("stream-gso-on", rounds)["passed"])

    def test_unbounded_memory_fails_in_any_round(self):
        rounds = pair_rounds()
        rounds[0][1]["server"]["rss_kb_end"] = 1101
        verdict = driver.judge_cell("stream-gso-on", rounds)
        self.assertFalse(verdict["passed"])
        self.assertIn("memory", verdict["failures"][0])
        rounds = pair_rounds()
        rounds[0][1]["server"]["rss_kb_end"] = 1100
        self.assertTrue(driver.judge_cell("stream-gso-on", rounds)["passed"])

    def test_empty_second_breaks_fair_progress(self):
        rounds = pair_rounds("mixed-bulk")
        background = rounds[2][1]["client"]["flows"][1]
        background["series"][30] = 0
        background["useful_bytes"] = sum(background["series"])
        verdict = driver.judge_cell("mixed-bulk", rounds)
        self.assertFalse(verdict["passed"])
        self.assertIn("fair progress", verdict["failures"][0])

    def test_control_starvation_is_reported_not_judged(self):
        rounds = pair_rounds()
        control = rounds[0][0]["client"]["flows"][0]
        control["series"][3] = 0
        control["useful_bytes"] = sum(control["series"])
        verdict = driver.judge_cell("stream-gso-on", rounds)
        self.assertTrue(verdict["passed"], verdict["failures"])
        self.assertEqual(verdict["empty_seconds"]["unregistered"], [1, 0, 0])

    def test_ordinary_and_background_are_reported_separately(self):
        verdict = driver.judge_cell("mixed-bulk", pair_rounds("mixed-bulk"))
        self.assertEqual(verdict["background_bytes"]["unregistered"], [2000 * driver.WINDOW_S] * 3)
        self.assertEqual(verdict["background_bytes"]["opted-in"], [2000 * driver.WINDOW_S] * 3)

    def test_stateless_share_is_reported(self):
        rounds = pair_rounds("stateless-pressure")
        verdict = driver.judge_cell("stateless-pressure", rounds)
        self.assertEqual(verdict["stateless_reset_share"]["opted-in"], [0.5, 0.5, 0.5])

    def test_summary_lists_every_cell(self):
        summary = driver.summarize(campaign())
        self.assertEqual(sorted(summary["cells"]), sorted(driver.CELLS))
        self.assertTrue(summary["passed"])

    def test_one_failed_cell_fails_the_summary(self):
        records = copy.deepcopy(campaign())
        for record in records:
            if record["cell"] == "mixed-bulk" and record["profile"] == "opted-in":
                scale_goodput(record, 0.5)
        summary = driver.summarize(records)
        self.assertFalse(summary["passed"])
        self.assertEqual(summary["failed_cells"], ["mixed-bulk"])


class ContaminationTests(unittest.TestCase):
    def test_limit_scales_with_the_observation(self):
        self.assertAlmostEqual(driver.foreign_limit_s(70), 3.5)

    def test_contaminated_pair_reruns_once(self):
        clean, dirty = {"contaminated": False}, {"contaminated": True}
        self.assertFalse(driver.rerun_due([clean, clean], attempt=0, reruns_used=0))
        self.assertTrue(driver.rerun_due([clean, dirty], attempt=0, reruns_used=0))
        self.assertFalse(driver.rerun_due([clean, dirty], attempt=1, reruns_used=1))

    def test_rerun_budget_is_finite(self):
        dirty = {"contaminated": True}
        self.assertFalse(driver.rerun_due([dirty, dirty], attempt=0, reruns_used=driver.MAX_RERUN_PAIRS))

    def test_rerun_never_reads_a_result(self):
        # The rule sees only the contamination flag, whatever the goodput says.
        slow = {"contaminated": False, "client": {"flows": [{"useful_bytes": 0}]}}
        self.assertFalse(driver.rerun_due([slow, slow], attempt=0, reruns_used=0))

    def test_flagged_observation_is_reported(self):
        records = campaign()
        records[0]["contaminated"] = True
        summary = driver.summarize(records)
        self.assertEqual(summary["contaminated_observations"], 1)
        self.assertFalse(summary["passed"], "unreplaced contamination cannot qualify")


class ExecutionTests(unittest.TestCase):
    def invoke(self, root, extra=(), failure=None, edit=None):
        binary = root / "fixture"
        binary.write_bytes(b"fixture identity")
        output = root / "output"
        processes = []

        class Process:
            returncode = None

            def wait(self, timeout=None):
                if failure == "timeout" and timeout is not None:
                    raise subprocess.TimeoutExpired("fixture", timeout)
                self.returncode = 7 if failure == "exit" else 0
                return self.returncode

            def kill(self):
                self.returncode = -9

        def launch(command, env, stdout, **kwargs):
            if failure == "launch" and processes:
                raise OSError("second child launch failed")
            spec = json.loads(env["ORDINARY_COST_SPEC"])
            process = Process()
            processes.append(process)
            stdout.write("--- PASS: TestOrdinaryCostRole (0.01s)\n")
            record = observation(spec["cell"], spec["profile"])
            if edit:
                edit(record)
            if failure != "missing":
                Path(spec["output"]).write_text(json.dumps(record[spec["role"]]))
            return process

        with contextlib.ExitStack() as stack:
            for name, value in (("wait_quiet", {}), ("schedstat_running_ns", dict.fromkeys(driver.FIXTURE_CPUS, 0)),
                                ("softirqd_ns", 0)):
                stack.enter_context(mock.patch.object(driver, name, return_value=value))
            stack.enter_context(mock.patch.object(driver.platform, "platform", return_value="Linux-test"))
            stack.enter_context(mock.patch.object(driver.platform, "system", return_value="Linux"))
            stack.enter_context(mock.patch.object(driver.platform, "machine", return_value="x86_64"))
            stack.enter_context(mock.patch.object(driver.shutil, "which", return_value="taskset"))
            stack.enter_context(mock.patch.object(driver.subprocess, "run", return_value=mock.Mock(stdout=f"fixture: {driver.GO_VERSION}\n")))
            stack.enter_context(mock.patch.object(driver.subprocess, "Popen", side_effect=launch))
            stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
            code = driver.main(["--candidate", "a" * 40, "--binary", str(binary), "--output", str(output),
                                "--remaining-instances", "42", "--remaining-host-seconds", "6000",
                                "--timeout-seconds", "135", *extra])
        return code, json.loads((output / "summary.json").read_text()), processes

    def test_failed_child_or_check_retains_charge_and_reaps_peers(self):
        for failure in ("launch", "exit", "timeout", "missing", "invalid", "baseline-write", "baseline-close", "baseline-timeout"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                def invalidate(record):
                    if failure == "invalid":
                        record["server"]["application_send_call"]["ordinary"]["p99_us"] = float("nan")
                    elif failure == "baseline-write":
                        record["server"]["write_errors"] = 1
                    else:
                        record["client"]["flows"][0].update(close_cause="failed connection", timeout_close=failure == "baseline-timeout")
                code, summary, processes = self.invoke(Path(temporary), failure=failure,
                                                       edit=invalidate if failure == "invalid" or failure.startswith("baseline-") else None)
                self.assertEqual(code, 1)
                self.assertEqual(summary["instances_charged"], 1)
                self.assertGreater(summary["host_seconds_charged"], 0)
                self.assertEqual(summary["attempts"][0]["status"], "failed")
                self.assertTrue(summary["attempts"][0]["error"])
                self.assertTrue(all(process.returncode is not None for process in processes))
                self.assertFalse(summary["campaign_complete"])

    def test_insufficient_allowance_launches_nothing(self):
        for extra in (("--remaining-instances", "35"), ("--remaining-host-seconds", "100")):
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as temporary:
                code, summary, processes = self.invoke(Path(temporary), extra)
                self.assertEqual(code, 1)
                self.assertEqual(processes, [])
                self.assertEqual(summary["instances_charged"], 0)
                self.assertIn("insufficient", summary["error"])

    def test_campaign_tolerance_failure_exits_nonzero(self):
        for failure in ("goodput", "write", "close", "timeout"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                def fail(record):
                    if record["cell"] != "mixed-bulk" or record["profile"] != driver.OPTED_IN:
                        return
                    if failure == "goodput":
                        scale_goodput(record, 0.5)
                    elif failure == "write":
                        record["server"]["write_errors"] = 1
                    else:
                        record["server"]["connections"][0].update(close_cause="failed connection", timeout_close=failure == "timeout")
                code, summary, _ = self.invoke(Path(temporary), edit=fail)
                self.assertEqual(code, 1)
                self.assertEqual(summary["failed_cells"], ["mixed-bulk"])
                self.assertEqual(summary["instances_charged"], 36)
                self.assertTrue(summary["campaign_complete"])

    def test_diagnostic_is_charged_and_cannot_complete_campaign(self):
        for option in ("--smoke-cell", "--diagnostic-cell"):
            with self.subTest(option=option), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                code, summary, _ = self.invoke(root, (option, "stateless-pressure"))
                self.assertEqual(code, 0)
                self.assertEqual(summary["instances_charged"], 2)
                self.assertFalse(summary["campaign_complete"])
                self.assertNotEqual(summary["mode"], "campaign")
                self.assertIn("neither native return nor peer delivery", summary["metric_boundaries"]["application_send_call"])
                self.assertEqual(summary["diagnostic"]["stateless_offered_packets"][driver.OPTED_IN], [600000])
                records = [json.loads(line) for line in (root / "output" / f"{summary['mode']}.jsonl").read_text().splitlines()]
                with self.assertRaises(ValueError):
                    driver.check_accounting(records)



if __name__ == "__main__":
    unittest.main()
