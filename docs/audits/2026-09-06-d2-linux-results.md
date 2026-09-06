# Linux receive-queue architecture evaluation

**Date:** 2026-09-06. **Outcome:** Both storage-reuse candidates save metadata and improve steady drain; neither clears every timing gate in this fixed experiment. Recommend the head-index slice as the next candidate to investigate under a representative offered load, not as a demonstrated across-workload win or an approved runtime merge. The completed D2 history and production `main` are unchanged.

## What was tested

The [protocol](2026-09-06-d2-linux-protocol.md) was committed before collection. The user authorized one controlled Linux follow-up of the existing ring, with a head-index fallback if the ring's tradeoff remained unresolved. The first set admitted that fallback because its burst/drain timing interval extended beyond the predeclared 5% slowdown allowance. No samples were discarded, no collection was repeated until significance, and no third design was added.

| Phase | Baseline source commit | Candidate source commit | Runtime delta |
| --- | --- | --- | --- |
| Existing ring | `e58f3d99521faaaabfa56816aeeaee2d93f58071` | `2d45050738817abf1ad6c71784d0f7bc84cb9bf5` | Receive slice becomes the existing lazy ring; shared ring code unchanged |
| Head-index slice | `7bfd0eb3955bc858c4cbb6659791f8dce64afe9e` | `5731f248f991c267e3490d7bf6f3e01cf2d6db1b` | Track first live element, clear pops, reset on empty, compact live headers when the append position reaches capacity |

Both baselines have the production runtime from `686bce6541438101c96732a023984e031faf6df7`; they differ only in experiment/test/documentation setup. Within each phase, benchmark and test sources are identical and `datagram_queue.go` is the only runtime difference. The two phases have separate contemporaneous baselines; their absolute candidate timings are not a direct head-to-head comparison. [Ring patch](d2-linux/ring/candidate.patch), [head-index patch](d2-linux/head-index/candidate.patch).

Builds ran natively on minimax using Go 1.26.0, Linux 7.0.0-30-generic/amd64, Ryzen AI MAX+ 395. Each process was pinned to logical CPUs 12 and 13 (distinct physical cores) with `GOMAXPROCS=2`; their SMT siblings are 28 and 29. The amd-pstate-epp/powersave policy stayed unchanged. Go 1.26/Linux/amd64 results cannot isolate an operating-system effect relative to the earlier Go 1.27/macOS/arm64 experiment.

Each workload received 20 paired one-second samples. Variant order alternated each round and workload order rotated. Binaries were precompiled before timing. The ring phase covered four workloads (160 samples); the head-index phase covered those four plus sustained partial refill (200 samples). Fixed-count race smoke runs were validation only, excluded from performance data. Linux received exported commit snapshots without Git metadata; all repository development stayed in dedicated feature worktrees.

## Timing results

Time changes below are the paired geometric-mean candidate/base ratio, with a fixed-seed 10,000-resample paired bootstrap 95% interval. Negative percentages mean less time. The tables also report raw sample medians, which need not yield the paired estimate because the latter compares each contemporaneous pair before aggregating. These are estimates for this fixed sample set, not universal equivalence guarantees. The predeclared gate requires the upper endpoint to be below +5% for every primary workload, alongside consistent allocation savings and correct accounting.

### Existing ring

| Workload | Base median | Ring median | Paired time change | 95% interval | Within +5% budget |
| --- | --- | --- | --- | --- | --- |
| One-message drain (ns/message) | 103.150 | 92.955 | -9.66% | -12.57% to -6.56% | Yes |
| 128-message burst/drain (ns/burst) | 12,506.500 | 12,517.000 | +2.71% | -0.91% to +6.41% | No; inconclusive |
| Full-queue drop (ns/attempt) | 9.372 | 9.375 | +0.02% | -0.00% to +0.04% | Yes |
| Concurrent overload (ns/delivered) | 1,051.000 | 1,020.500 | -2.39% | -5.43% to +0.79% | Yes |

The ring's steady-drain improvement is clear in this workload. Burst/drain's uncertainty reaches +6.41%, so the ring does not pass the complete gate; this is not evidence of a demonstrated 6.41% slowdown. Overflow and useful delivery under concurrent overload fit the allowed margin.

### Head-index slice

| Workload | Base median | Head-index median | Paired time change | 95% interval | Within +5% budget |
| --- | --- | --- | --- | --- | --- |
| One-message drain (ns/message) | 101.750 | 91.420 | -10.16% | -11.45% to -8.67% | Yes |
| 128-message burst/drain (ns/burst) | 12,357.000 | 12,204.500 | -1.01% | -3.55% to +2.27% | Yes |
| Full-queue drop (ns/attempt) | 9.373 | 9.372 | +0.01% | -0.01% to +0.04% | Yes |
| Concurrent overload (ns/delivered) | 1,079.000 | 1,112.500 | +2.11% | -0.82% to +5.01% | No; inconclusive |
| Partial drain/refill (ns/32 messages) | 3,087.000 | 2,977.500 | -3.62% | -6.36% to -0.40% | Yes |

The head-index slice passes steady drain, burst/drain, overflow and partial refill. Concurrent overload's upper endpoint is `1.0501470651` (about +5.015%), just above the fixed `1.05` cutoff. That is a narrow statistical miss, not an engineering cliff or proof of a material regression. The cutoff was not rounded downward or relaxed after seeing the result. No further samples were collected.

## Allocation and delivery accounting

| Workload | Existing slice B/op → reused storage B/op | allocs/op | Interpretation |
| --- | --- | --- | --- |
| One-message drain, either phase | 1176 → 1152 | 1 → 1 | 24 metadata bytes per message removed; integer allocation counts hide fractional metadata churn |
| 128-message burst/drain, either phase | 153600 → 147456 | 130 → 128 | 6144 bytes and two metadata allocations saved per batch; payload copies remain |
| Partial refill, head-index phase | 38229 → 36864 | 32 → 32 | Metadata bytes decrease while all 32 payload copies remain; allocation counts again hide fractional metadata work |

The concurrent benchmark is intentionally unpaced overload: one goroutine offers datagrams as quickly as it can while another receives them. It reports default ns/op and allocation counters per offered message, plus delivered count, drop count, drop fraction, delivered/s and ns/delivered. Final drain and receiver join are included in the measured interval. Delivered plus dropped equals offered in every recorded sample.

| Phase | Median useful delivery rate, base → candidate | Median drop fraction, base → candidate |
| --- | --- | --- |
| Ring | 951287 → 979987 datagrams/s | 98.475% → 98.435% |
| Head-index | 926670 → 898605 datagrams/s | 98.490% → 98.545% |

These medians are descriptive; use the paired ns/delivered intervals for the primary decision. Approximately 98% drops mean this case principally characterizes contention and discard work under overload. It is not an estimate of production loss, ordinary receive latency, or network/file throughput. A lower per-offered-message allocation figure in this workload can come from dropping more input, so it is not used as evidence of payload-allocation savings.

## Host quality and limitations

The ring collection ran from approximately 18:00:51 to 18:04 UTC; the head-index collection ran from 18:09:16 to 18:13:09 UTC. Core monitors recorded no guest activity on CPUs 12, 13, 28 or 29. During the ring phase the SMT siblings averaged 99.957% and 99.904% idle; during the head-index phase they averaged 99.979% and 99.996% idle. Median CPU12 frequency snapshots were approximately 5.024 GHz for both versions in phase one and 5.022 GHz for both in phase two. Frequency snapshots are taken at sample boundaries and are not in-sample hardware frequency counters.

This is materially better-controlled than the original busy-laptop run, but affinity is not exclusive CPU reservation, frequency still varies, and other host work can affect shared resources. Bootstrap resampling assumes the paired observations adequately represent variation; it is not a proof against every source of correlation or scheduler behavior. The collection kept all samples, including early and slower ones. [Ring host summary](d2-linux/ring/host-summary.json), [head-index host summary](d2-linux/head-index/host-summary.json).

## Validation and recommendation

On Linux, both versions in each phase passed focused `TestDatagram` tests, their race variant, and the inherited ring tests. The concurrent benchmark passed a 1024-operation race smoke check in each build. In the fallback phase the partial-refill benchmark also passed that smoke check, and `TestDatagramReceivePartialRefillFIFO` verified distinct messages through sustained partial occupancy and final drain on baseline and candidate. [Ring validation](d2-linux/ring/validation.txt), [head-index validation](d2-linux/head-index/validation.txt). These focused checks are experiment validation, not full production merge certification; no RAS review, hosted CI success, or application qualification is claimed.

The allocation opportunity is real and both candidates improve the steady-drain microbenchmark. The head-index slice merits the next investigation because it also handles the compaction workload well and satisfies four of the five timing gates, but its overload result prevents an unconditional recommendation to ship. This is a judgment about follow-up priority, not evidence that head-index is statistically superior to the ring across hosts or workloads.

Stop this experiment here. If further work is authorized, answer a different, application-relevant question: whether normal offered rates and observed queue occupancy reproduce a meaningful CPU/GC improvement without degrading delivery latency or loss. Specify that traffic model from application evidence before adding another benchmark. Repeating the current overloaded workload merely to move a +5.015% interval endpoint below +5% would not be a sound next step. The 128-slot fixed ring, synchronization replacement and payload pooling remain unevaluated.

## Reproduction and retained evidence

The raw benchmark text, source/binary hashes, validation logs, host observations, per-sample manifests and analysis are under [ring](d2-linux/ring/) and [head-index](d2-linux/head-index/). The manifests are gzip-compressed JSON Lines and retain exact commands, run order, timestamps, frequency observations and stdout/stderr. The archived patches preserve the exact experimental runtime deltas. The remote binaries and source snapshots remain under `/home/josh/benchmarks/quic-go-fast/d2-linux-20260906` and `/home/josh/benchmarks/quic-go-fast/d2-linux-head-index-20260906` on minimax.

Reproduce analysis without rerunning benchmarks:

```sh
python3 docs/audits/d2-linux/analyze.py docs/audits/d2-linux/ring/samples.jsonl.gz
python3 docs/audits/d2-linux/analyze.py docs/audits/d2-linux/head-index/samples.jsonl.gz --candidate head-index --partial-refill
```

Both commands reproduce the saved analysis JSON exactly. [Collection script](d2-linux/collect.py) rejects an existing manifest rather than overwriting or silently resuming a collection. [benchstat outputs](d2-linux/ring/benchstat.txt) and [fallback outputs](d2-linux/head-index/benchstat.txt) use `golang.org/x/perf/cmd/benchstat@v0.0.0-20260825160852-19be9d8e6c70`. The scripts and samples are finite investigation artifacts, not a new maintained benchmark framework or required product dependency.
