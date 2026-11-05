import copy
import unittest

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
            "send_completion": {"ordinary": {"count": 6000, "p99_us": 1000.0}},
            "connections": [{"label": "ordinary", "timeout_close": False, "close_cause": ""}],
        },
        "client": {"flows": [flow()]},
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
        record["client"]["flows"][1]["paths"] = ["live"]
        with self.assertRaisesRegex(ValueError, "two-path"):
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
        record["server"]["send_completion"]["ordinary"]["count"] = 0
        with self.assertRaisesRegex(ValueError, "send-completion"):
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

    def test_send_completion_is_judged_separately(self):
        def slow(record):
            record["server"]["send_completion"]["ordinary"]["p99_us"] = 5000.0

        verdict = driver.judge_cell("stream-gso-on", pair_rounds(opted=slow))
        self.assertFalse(verdict["passed"])
        self.assertIn("send-completion p99", verdict["failures"][0])

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


if __name__ == "__main__":
    unittest.main()
