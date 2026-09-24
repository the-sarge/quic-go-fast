"""Disposable figure recipe; reads saved evidence, does not rerun the model."""
import csv
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

root = Path(__file__).parent
with (root / "packets.csv").open() as source:
    packets = list(csv.DictReader(source))
with (root / "samples.csv").open() as source:
    samples = list(csv.DictReader(source))

fig, axes = plt.subplots(2, 2, figsize=(13, 8), constrained_layout=True)
colors = {"registration": "#2563eb", "acceptance": "#d97706", "departure": "#15803d"}
run = "stall-compress-gso/q4/per-opportunity"
selected = [p for p in packets if p["run"] == run and 380000 <= int(p["registration_us"]) <= 510000]
ax = axes[0, 0]
for field, label, color in [("registration_us", "Registration", "#2563eb"), ("submission_us", "Acceptance", "#d97706"), ("departure_us", "Departure", "#15803d"), ("ack_us", "ACK arrival", "#9333ea")]:
    ax.plot([int(p[field]) / 1000 for p in selected], [int(p["packet"]) for p in selected], ".-", ms=3, lw=0.8, label=label, color=color)
ax.axvspan(401, 481, alpha=0.08, color="black")
ax.set(title="Same packets, different timing boundaries", xlabel="Time (ms)", ylabel="QUIC packet ID")
ax.legend(fontsize=8)

ax = axes[0, 1]
for boundary, color in colors.items():
    rows = [s for s in samples if s["run"] == run and s["boundary"] == boundary and 350000 <= int(s["time_us"]) <= 850000 and s["valid"] == "true"]
    ax.plot([int(s["packet"]) for s in rows], [float(s["rate_bytes_per_sec"]) * 8 / 1e6 for s in rows], ".-", ms=2, lw=0.8, color=color, label=boundary)
ax.axhline(9.6, color="black", lw=1, ls="--", label="Fixed path capacity")
ax.set(title="Rate maxima hide transient disagreement", xlabel="Acknowledged packet ID", ylabel="Delivery sample (Mbit/s)")
ax.legend(fontsize=8)

ax = axes[1, 0]
for policy, color in [("per-opportunity", "#2563eb"), ("queued-credit", "#dc2626")]:
    rows = [p for p in packets if p["run"] == f"stall-compress-gso/q4/{policy}" and 380000 <= int(p["registration_us"]) <= 510000]
    ax.plot([int(p["registration_us"]) / 1000 for p in rows], [(int(p["departure_us"]) - int(p["registration_us"])) / 1000 for p in rows], ".", ms=4, label=policy, color=color)
ax.set(title="Smaller bursts still retain stalled-packet delay", xlabel="Registration (ms)", ylabel="Registration → departure (ms)")
ax.legend(fontsize=8)

ax = axes[1, 1]
for policy, color in [("per-opportunity", "#2563eb"), ("queued-credit", "#dc2626")]:
    rows = [s for s in samples if s["run"] == f"stall-compress-gso/q4/{policy}" and s["boundary"] == "registration"]
    ax.plot([int(s["time_us"]) / 1000 for s in rows], [int(s["live_records"]) for s in rows], lw=1, color=color, label=policy)
ax.set(title="Outstanding snapshots after each ACK", xlabel="ACK arrival (ms)", ylabel="Live records")
ax.legend(fontsize=8)
for ax in axes.flat:
    ax.grid(alpha=0.2)
fig.suptitle("Disposable model: worker stall + GSO + ACK compression, quantum 4\nFixed 9.6 Mbit/s path / 40ms propagation RTT — no native performance claim", fontsize=14)
fig.savefig(root / "timing.png", dpi=150, metadata={"Software": "matplotlib; disposable delivery sampling evidence"})
