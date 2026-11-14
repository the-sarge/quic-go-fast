import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import aggregation as driver


def server_flow(stream_bytes_per_second=1000, datagram_bytes=0, datagrams=0):
    return {
        "stream_series": [stream_bytes_per_second] * driver.WINDOW_S,
        "stream_useful_bytes": stream_bytes_per_second * driver.WINDOW_S,
        "objects_completed": 1,
        "object_completion": {"p50_ms": 10.0, "p99_ms": 20.0},
        "datagrams_useful": datagrams,
        "datagram_useful_bytes": datagram_bytes,
        "datagram_duplicates": 0,
        "datagram_latency": {"p50_ms": 10.0, "p99_ms": 20.0},
    }


def client_flow(label, arm):
    claim = driver.ARM_CLAIMS[arm]
    competitor = label == driver.COMPETITOR
    multipath = claim["multipath"] and not competitor
    return {
        "label": label,
        "controller": "reno" if competitor else claim["controller"],
        "multipath_negotiated": multipath,
        "scheduler": claim["scheduler"] if multipath else "",
        "paths": [{"wire_id": 0, "lifecycle": "live", "local_use": "active", "primary": True},
                  {"wire_id": 1, "lifecycle": "live", "local_use": "active", "primary": False}] if multipath else None,
        "datagrams_offered": 0 if competitor else 1000,
        "datagrams_useful": 0,
        "packets_lost": 0,
        "close_cause": "",
        "timeout_close": False,
    }


def direction(window_bytes):
    return {"overflow": 0, "window_bytes": window_bytes}


def observation(topology="balanced", arm="E", round_=0, focal=1000, competitor=1000, datagrams=False):
    """One valid campaign record; focal is each focal flow's STREAM bytes per second."""
    claim = driver.ARM_CLAIMS[arm]
    index = driver.TOPOLOGIES.index(topology)
    labels = ["focal-a", "focal-b"] if arm == "C" else ["focal-a"]
    if topology == "shared":
        labels.append(driver.COMPETITOR)
    links = [{"name": name, "forward": direction(10 if name in claim["paths"] else 0), "reverse": direction(5)}
             for name in ("a", "b")]
    if topology == "shared":
        links.append({"name": "competitor", "forward": direction(10), "reverse": direction(5)})
    flows = {label: server_flow(competitor if label == driver.COMPETITOR else focal) for label in labels}
    if datagrams:
        flows["focal-a"].update(datagrams_useful=900, datagram_useful_bytes=900_000)
    spec = {"arm": arm, "topology": topology}
    record = {
        "topology": topology, "round": round_, "arm": arm, "seed": driver.BASE_SEED + 100 * index + round_,
        "mode": "campaign", "contaminated": False,
        "relay": {"spec": spec, "result": {"go": driver.GO_VERSION, "links": links}},
        "server": {"spec": spec, "result": {"go": driver.GO_VERSION, "granted": True, "joined": True, "stream_errors": 0,
                                            "connections": [{"timeout_close": False}], "flows": flows}},
        "client": {"spec": spec, "result": {"go": driver.GO_VERSION, "flows": [client_flow(label, arm) for label in labels]}},
    }
    return record


def keyed(values, topology="balanced", competitor=None):
    """{(arm, round): record} with each arm's focal bytes per second from values."""
    out = {}
    for arm in driver.ARMS:
        for round_ in range(driver.ROUNDS):
            other = (competitor or {}).get(arm, 1000)
            out[(arm, round_)] = observation(topology, arm, round_, focal=values.get(arm, 1000), competitor=other)
    return out


def campaign():
    return [observation(topology, arm, round_) for topology, round_, arm, _ in driver.schedule()]


class ScheduleTest(unittest.TestCase):
    def test_fixed_inventory(self):
        instances = driver.schedule()
        self.assertEqual(54, len(instances))
        self.assertEqual(54, len(set(instances)))
        for topology in driver.TOPOLOGIES:
            for round_ in range(driver.ROUNDS):
                arms = [arm for t, r, arm, _ in instances if (t, r) == (topology, round_)]
                self.assertEqual(sorted(driver.ARMS), sorted(arms))
        # Each round rotates the arm order, so no arm always runs first.
        firsts = {arms[0] for arms in ([a for t, r, a, _ in instances if (t, r) == ("balanced", n)] for n in range(3))}
        self.assertEqual(3, len(firsts))

    def test_arm_claims_match_the_plan(self):
        for arm in ("A", "B", "C"):
            self.assertEqual("bbrv3", driver.ARM_CLAIMS[arm]["controller"])
            self.assertFalse(driver.ARM_CLAIMS[arm]["multipath"])
        self.assertEqual(("reno", "aggregate"), (driver.ARM_CLAIMS["D"]["controller"], driver.ARM_CLAIMS["D"]["scheduler"]))
        self.assertEqual(("bbrv3", "aggregate"), (driver.ARM_CLAIMS["E"]["controller"], driver.ARM_CLAIMS["E"]["scheduler"]))
        self.assertEqual(("bbrv3", "round-robin"), (driver.ARM_CLAIMS["F"]["controller"], driver.ARM_CLAIMS["F"]["scheduler"]))
        self.assertEqual((1.20, 0.95, 0.90), (driver.GAIN_FLOOR, driver.INDEPENDENT_FLOOR, driver.COMPETITOR_FLOOR))


class AccountingTest(unittest.TestCase):
    def test_complete_campaign_accounts(self):
        driver.check_accounting(campaign(), driver.GO_VERSION)

    def test_missing_duplicate_and_smoke_records_fail(self):
        records = campaign()
        with self.assertRaisesRegex(ValueError, "do not match"):
            driver.check_accounting(records[1:], driver.GO_VERSION)
        with self.assertRaisesRegex(ValueError, "do not match"):
            driver.check_accounting(records[1:] + records[:1] + records[:1], driver.GO_VERSION)
        smoke = copy.deepcopy(records)
        smoke[0]["mode"] = "smoke"
        with self.assertRaisesRegex(ValueError, "cannot satisfy"):
            driver.check_accounting(smoke, driver.GO_VERSION)

    def test_observation_rejects_mismatched_arm(self):
        cases = {
            "toolchain": lambda r: r["server"]["result"].update(go="go1.26.11"),
            "ran reno": lambda r: r["client"]["result"]["flows"][0].update(controller="reno"),
            "scheduler": lambda r: r["client"]["result"]["flows"][0].update(scheduler="round-robin"),
            "two live active": lambda r: r["client"]["result"]["flows"][0]["paths"][1].update(local_use="backup"),
            "multipath negotiation": lambda r: r["client"]["result"]["flows"][0].update(multipath_negotiated=False),
            "overflowed": lambda r: r["relay"]["result"]["links"][0]["forward"].update(overflow=1),
            "path use": lambda r: r["relay"]["result"]["links"][1]["forward"].update(window_bytes=0),
            "closed before the end": lambda r: r["client"]["result"]["flows"][0].update(close_cause="idle"),
            "series": lambda r: r["server"]["result"]["flows"]["focal-a"].update(stream_useful_bytes=1),
            "stream errors": lambda r: r["server"]["result"].update(stream_errors=1),
            "did not join": lambda r: r["server"]["result"].update(joined=False),
            "another case": lambda r: r["relay"]["spec"].update(arm="F"),
        }
        for message, mutate in cases.items():
            with self.subTest(message):
                record = observation()
                mutate(record)
                with self.assertRaisesRegex(ValueError, message):
                    driver.check_observation(record, driver.GO_VERSION)

    def test_single_path_arm_must_not_use_the_other_path(self):
        record = observation(arm="A")
        driver.check_observation(record, driver.GO_VERSION)
        record["relay"]["result"]["links"][1]["forward"]["window_bytes"] = 10
        with self.assertRaisesRegex(ValueError, "path use"):
            driver.check_observation(record, driver.GO_VERSION)

    def test_shared_topology_requires_the_reno_competitor(self):
        record = observation("shared", "C")
        driver.check_observation(record, driver.GO_VERSION)
        record["client"]["result"]["flows"][-1]["controller"] = "bbrv3"
        with self.assertRaisesRegex(ValueError, "competitor ran bbrv3"):
            driver.check_observation(record, driver.GO_VERSION)


class JudgeTest(unittest.TestCase):
    def test_balanced_thresholds_use_the_better_single_path(self):
        # A 1000, B 900: A is better. C's two flows carry 1000 each. E needs 1.20 x A and 0.95 x C.
        verdict = driver.judge_topology("balanced", keyed({"A": 1000, "B": 900, "C": 1000, "E": 1900}))
        self.assertEqual("A", verdict["better_single_path"])
        self.assertTrue(verdict["passed"], verdict["failures"])
        verdict = driver.judge_topology("balanced", keyed({"A": 900, "B": 1000, "C": 1000, "E": 1900}))
        self.assertEqual("B", verdict["better_single_path"])

    def test_balanced_failures_are_a_negative_verdict(self):
        low_gain = driver.judge_topology("balanced", keyed({"A": 1000, "B": 900, "C": 1100, "E": 1100}))
        self.assertFalse(low_gain["passed"])
        self.assertIn("gain", low_gain["failures"][0])
        low_share = driver.judge_topology("balanced", keyed({"A": 1000, "B": 900, "C": 2000, "E": 1500}))
        self.assertEqual(["E share of C"], [failure[:12] for failure in low_share["failures"]])

    def test_useful_bytes_count_stream_and_datagram_payload(self):
        record = observation(datagrams=True)
        self.assertEqual(1000 * driver.WINDOW_S + 900_000, driver.useful_bytes(record))
        metrics = driver.observation_metrics(record)
        self.assertEqual(0.9, metrics["datagram_useful_delivery"])
        self.assertEqual({"a": 5, "b": 5}, metrics["path_reverse_window_bytes"])

    def test_shared_competitor_floor(self):
        passing = driver.judge_topology("shared", keyed({}, "shared", competitor={"C": 1000, "E": 900}))
        self.assertTrue(passing["passed"], passing["failures"])
        failing = driver.judge_topology("shared", keyed({}, "shared", competitor={"C": 1000, "E": 899}))
        self.assertFalse(failing["passed"])
        self.assertIn("competitor", failing["failures"][0])

    def test_unequal_topology_is_reported_without_a_threshold(self):
        verdict = driver.judge_topology("unequal", keyed({"E": 1}))
        self.assertFalse(verdict["thresholded"])
        self.assertTrue(verdict["passed"])

    def test_summary_is_negative_on_any_failed_thresholded_topology(self):
        records = campaign()
        self.assertEqual("negative", driver.summarize(records, driver.GO_VERSION)["support"])
        for record in records:
            if record["arm"] == "E" and record["topology"] == "balanced":
                record["server"]["result"]["flows"]["focal-a"]["stream_series"] = [2000] * driver.WINDOW_S
                record["server"]["result"]["flows"]["focal-a"]["stream_useful_bytes"] = 2000 * driver.WINDOW_S
        summary = driver.summarize(records, driver.GO_VERSION)
        self.assertEqual("positive", summary["support"], summary["failed_topologies"])

    def test_contaminated_inventory_is_invalid(self):
        records = campaign()
        records[3]["contaminated"] = True
        with self.assertRaisesRegex(ValueError, "contaminated"):
            driver.summarize(records, driver.GO_VERSION)


class LedgerTest(unittest.TestCase):
    def test_rerun_rule_never_sees_a_result(self):
        self.assertTrue(driver.rerun_due({"contaminated": True}, 0, 0))
        self.assertFalse(driver.rerun_due({"contaminated": True}, 1, 0))
        self.assertFalse(driver.rerun_due({"contaminated": True}, 0, driver.MAX_RERUNS))
        self.assertFalse(driver.rerun_due({"contaminated": False}, 0, 0))

    def test_charge_is_durable_before_launch_and_failures_stay_charged(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = driver.AttemptLedger(Path(directory), 2, 1000.0, 100.0)

            def launch(*args, **kwargs):
                saved = json.loads((Path(directory) / "ledger.json").read_text())
                self.assertEqual("started", saved["attempts"][0]["status"])
                raise ValueError("role exit 1")

            with mock.patch.object(driver, "run_observation", side_effect=launch):
                with self.assertRaises(ValueError):
                    ledger.observe("binary", Path(directory) / "x", "balanced", 0, "A", 1, "c" * 40, "campaign", driver.GO_VERSION)
            saved = json.loads((Path(directory) / "ledger.json").read_text())
            self.assertEqual(1, saved["instances_charged"])
            self.assertEqual("failed", saved["attempts"][0]["status"])

    def test_capacity_counts_instances_and_reserved_host_time(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = driver.AttemptLedger(Path(directory), 54, 54 * 100.0, 100.0)
            ledger.require_capacity(54)
            with self.assertRaisesRegex(ValueError, "insufficient"):
                ledger.require_capacity(55)
            short = driver.AttemptLedger(Path(directory), 54, 54 * 100.0 - 1, 100.0)
            with self.assertRaisesRegex(ValueError, "insufficient"):
                short.require_capacity(54)


if __name__ == "__main__":
    unittest.main()
