"""Replay the retained local native observations for issue #1210."""

import gzip
import json
import statistics
from pathlib import Path


def summarize(path):
    observations = json.loads(gzip.decompress(path.read_bytes()))
    groups = []
    for observation in observations:
        if not groups or observation["at"] - groups[-1][-1]["at"] > 10_000_000:
            groups.append([])
        groups[-1].append(observation)
    results = []
    for group in groups:
        duration = (group[-1]["at"] - group[0]["at"]) / 1e9
        acked = sum(item["acked"] for item in group[1:])
        median = lambda field: statistics.median(item[field] for item in group)
        observed = acked * 8 / duration / 1e6
        optimistic = statistics.median(item["window"] * 8000 / item["min_rtt"] for item in group)
        results.append({
            "rtt_eligible_ack_events": len(group),
            "observed_interval_ms": duration * 1000,
            "acknowledged_transport_mbps": observed,
            "median_window_bytes": median("window"),
            "median_prior_flight_bytes": median("flight"),
            "median_pending_local_bytes": median("pending"),
            "median_model_min_rtt_us": median("min_rtt") / 1000,
            "median_raw_rtt_us": median("raw_rtt") / 1000,
            "mean_raw_rtt_us": statistics.mean(item["raw_rtt"] for item in group) / 1000,
            "median_model_pacing_mbps": median("rate") * 8 / 1e6,
            "window_over_model_min_rtt_mbps": optimistic,
            "observed_over_optimistic": observed / optimistic,
        })
    return results


def main():
    root = Path(__file__).resolve().parent / "results"
    print(json.dumps({name: summarize(root / f"{name}.json.gz") for name in [
        "native2", "native-ack1", "native-restored", "native-validation"
    ]}, indent=2))


if __name__ == "__main__":
    main()
