#!/usr/bin/env python3
"""D2 protocol analysis per docs/audits/2026-09-12-d2-recvmsgx-protocol.md.

Paired per-round engaged/disabled ratios over rounds 1-10 (round 0 is the
discarded warmup); fixed-seed paired bootstrap (10,000 resamples) for the
geometric-mean ratio of the primary metric and throughput; engagement and
memory summaries; mechanical disposition.
"""
import json, math, random, statistics, sys

path = sys.argv[1]
rows = [json.loads(l) for l in open(path) if l.strip()]
rows = [r for r in rows if r["round"] >= 1]
by = {}
for r in rows:
    by.setdefault(r["round"], {})[r["cell"]] = r

rounds = sorted(by)
assert rounds == list(range(1, 11)), rounds
for rd in rounds:
    assert set(by[rd]) == {"engaged", "disabled", "unqualified"}, (rd, set(by[rd]))

# preservation/qualification assertions
for rd in rounds:
    for cell in ("disabled", "unqualified"):
        c = by[rd][cell]
        assert c["batch_reads"] == 0 and c["fallback_reads"] > 0 and c["capability"] is False, (rd, cell)
    e = by[rd]["engaged"]
    assert e["capability"] is True and e["batch_datagrams"] > 0, rd

primary = [by[rd]["engaged"]["syscalls_per_datagram"] / by[rd]["disabled"]["syscalls_per_datagram"] for rd in rounds]
tput = [by[rd]["engaged"]["throughput_mbps"] / by[rd]["disabled"]["throughput_mbps"] for rd in rounds]

def geomean(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))

def boot_ci(ratios, seed=20260912, n=10000):
    rng = random.Random(seed)
    stats = []
    k = len(ratios)
    for _ in range(n):
        sample = [ratios[rng.randrange(k)] for _ in range(k)]
        stats.append(geomean(sample))
    stats.sort()
    return stats[int(0.025 * n)], stats[int(0.975 * n) - 1]

p_lo, p_hi = boot_ci(primary)
t_lo, t_hi = boot_ci(tput)
p_pt, t_pt = geomean(primary), geomean(tput)

dpbr = [by[rd]["engaged"]["datagrams_per_batch_read"] for rd in rounds]
bfrac = [by[rd]["engaged"]["batched_fraction"] for rd in rounds]
rss_e = statistics.median(by[rd]["engaged"]["max_rss_bytes"] for rd in rounds)
rss_d = statistics.median(by[rd]["disabled"]["max_rss_bytes"] for rd in rounds)

print(f"primary syscalls/datagram ratio: point {p_pt:.4f}  95% CI [{p_lo:.4f}, {p_hi:.4f}]  (pass needs upper <= 0.75)")
print(f"throughput ratio:                point {t_pt:.4f}  95% CI [{t_lo:.4f}, {t_hi:.4f}]  (pass needs lower >= 0.95)")
print(f"engagement: mean datagrams/batch read {statistics.mean(dpbr):.3f} (>=2.0), batched fraction mean {statistics.mean(bfrac):.4f} (>=0.25)")
print(f"memory: engaged median RSS {rss_e/1e6:.1f} MB vs disabled {rss_d/1e6:.1f} MB (budget +8 MiB = {8*1024*1024/1e6:.1f} MB)")
print(f"per-round primary ratios: {[round(x,4) for x in primary]}")
print(f"per-round throughput ratios: {[round(x,4) for x in tput]}")

# mechanical disposition
passes = (p_hi <= 0.75 and statistics.mean(dpbr) >= 2.0 and statistics.mean(bfrac) >= 0.25
          and t_lo >= 0.95 and rss_e <= rss_d + 8 * 1024 * 1024)
fails = (p_pt > 0.90 or statistics.mean(bfrac) < 0.05 or t_pt < 0.90
         or rss_e > rss_d + 8 * 1024 * 1024)
if passes:
    print("DISPOSITION: PASS -> adopt")
elif fails:
    print("DISPOSITION: FAIL -> retire")
else:
    print("DISPOSITION: INCONCLUSIVE -> retire")
