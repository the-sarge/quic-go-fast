# Connection-owned preparation metadata for N3 #1202

**Verdict: withdrawn from the next prototype.** The cache removed the targeted global-pool work, but native throughput and CPU/byte remained essentially flat against the preceding clean observation. Its clean same-binary pair reached **82.9% baseline goodput and 118.4% CPU/useful byte**, failing both unchanged targets, and also triggered the existing RSS-growth check. This does not establish a causal regression or memory leak; it provides insufficient net benefit to justify the added retention, baseline ACK cost and maintenance surface. The source and evidence are preserved, and independent boundary characterizations are retained.

## Source and ownership

Frozen diagnostic source is `6b11f0e23e6c7038c06b0ad2ceb8eac8e5598e4d`, based on `416ea33fd4010fd5ce3eca596f43dd53716b6111`. Archive SHA-256 is `284076fad99ec9bfe6500ac63ecf2339d047ef812cea046593eb8259e48524b4`. The preceding measured source is `a4f9f1867cd58e4d7ee51befb1ef46e4a1703a10`; the intervening fixture-only change retains the exact contamination-decision duration and limit without changing its rule or Go behavior.

Only an open concrete local root configures the cache, once, with the adapter's existing `maxGSOSegments+3` bound of 67 pointers. This limits retained free metadata; it does not reserve or limit protocol admission. Ordinary native preparation takes an already-cleared packet if available, nils the removed slot and otherwise uses the existing global pool. Unsupported, nil, closed, nonroot and unconfigured recovery receivers use that fallback. Never-registered Discard keeps its global cold path.

Allocation is separate from registration authority. The native caller reads an optional broad recovery interface before packing; it still resolves and asserts the fresh registration receiver after packing, logging and activity callbacks. Metadata acquired from owner A may be registered with owner B. No allocator pointer is added to the packet or preparation handle, and no earlier capability assertion changes callback behavior.

A successfully ACKed packet enters the current ACK receiver's eligible cache only at the existing pool-return point, after that packet's callbacks and removal from flight. Closed, full or unconfigured caches use the original global return. The helper first applies the existing stream-backing ownership rule: only an already-owned cap-one backing is fully cleared and retained with zero length. It drops other backing and resets all packet fields before storage. Cache hits need no further reset. Later ACK-wide callbacks retain their original order. Loss, Retry, key drop, close and native completion do not gain a reuse boundary.

Close marks the owner closed and drops its private cache slice before callbacks can refill it. Every free entry has already lost frame, canonical and protocol references. Pop clears the backing slot before it can retain a newly live packet. Existing pending/live counters, history, tokens, ECN, rich feedback, ACK carriers and admission checks never consult cache occupancy.

## Representation and compiler evidence

Linux amd64 layout remains 152 bytes for packet and 32 bytes for PacketPreparation. The local sidecar grows from 40 to 64 bytes for a slice header. At capacity, free metadata retains at most 67 packets, 67 cleared cap-one StreamFrame backings and 67 pointers: 12,328 logical bytes plus owner-header change and allocator rounding. Retained tokens can keep the local owner alive; its free cache remains bounded and is dropped on close. Cleared entries retain no frame payload. This is an explicit retention tradeoff against global-pool GC turnover.

The final Go 1.27.3 Linux amd64/v1 ordinarycost inspection binary is `9c4f5cdf8a8d65046a301daced34d3fb2a07fb53294a129f7fea990376b0600c`. A hit performs no global Get/Put, reset or allocation. ACK cache admission retains the required 24-byte stream-element clear and 152-byte packet reset, with normal GC write barriers, and calls neither global Put nor allocation. The configured pointer backing is one allocation per configured root; returned packet pointers refer to already-existing heap objects, not new per-packet handles.

The first compiler pass found an unreachable slice-growth path left by append despite the caller's fixed-capacity proof. A bounded reslice/indexed store removed that machinery without changing the proof or lifetime. The first binary and disassembly remain preserved. The final ReceivedAck body has 924 instructions versus 832 before the cache, and a 312-byte frame versus 288. Baseline executes the new local-owner load/test/branch before its original global put; it also pays the larger frame. Eighteen other available inspected functions retain equivalent normalized instructions, with compiler-generated type-assertion-cache renumbering explicitly normalized. This bounded check is not a whole-binary identity claim. There are no new per-packet diagnostic atomics.

## Validation and design choice

Existing-API characterizations passed on the unmodified base before the cache was applied. Paired and new cases cover early ACK and late accepted/uncertain/unsubmitted completion after metadata reuse, acquisition during callbacks, reacquiring an earlier packet from a later packet's callback within one ACK, close during ACK and after a filled cache, exact owned/non-owned cap-one backing behavior, growth/failure cleanup, full-cache fallback, pop-slot clearing, owner replacement during packing, custom/nil/nonroot fallback and unchanged Retry/key-drop old-reader behavior.

Full root tests passed in 12.094 seconds, full recovery race tests in 74.192 seconds, and native/caller race checks in 3.435 seconds. After the fixed-capacity store refinement, affected race checks passed in 3.436 seconds for root and 1.192 seconds for recovery; the unchanged full suites were not repeated. Focused before/after checks, vet, tidy-diff, source/patch hash checks and diff checks passed. Independent source review found no correctness blocker. Linux native/caller semantic checks and the full recovery package precede measurement.

The broader comparison rejected allocation volume as a sufficient reason for a canonical-identity rewrite. A 16-byte epoch/PN key with packet-backed facts would double token copies, add current-history resolution and require new ECN, rich-feedback, ACK-carrier and callback-stack lifetimes. Retry's old reader must observe a nested completion; detaching a value at retirement breaks that behavior. An unlinked registration also exists before arbitrary logging callbacks, so current-history lookup alone is insufficient. That redesign currently removes no additional history/rank/ECN pass. The cache instead targets an observed 1.35 seconds of metadata Pool.Get and 0.83 seconds of Pool.Put in the preceding whole-role native profile, about 0.02292466 ns/byte under the report's normalization. This is a synchronization-cost ceiling, not predicted savings or enough by itself to meet the overall targets.

## Measurement and evidence

Both observations used the same remote Linux amd64 Go 1.27.3 binary, SHA-256 `3d775ea407bdf8d01d4c270cab99a846dfcc971082abb984c15b4dcc785b34d9`, downloaded and locally rehashed. This is distinct from the inspection binary above. The existing fixture retains server CPUs 8–11, client CPUs 12–15 and their monitored siblings 24–31, seed 1038, 10-second warmup and 60-second measurement. All build and semantic phases passed, both observations were clean, and all role exits were zero. No retry ran.

| Clean observation | Baseline | Native |
| --- | ---: | ---: |
| Useful bytes | 98,667,986,944 | 81,765,616,896 |
| Useful MB/s | 1644.466449 | 1362.760282 |
| Server window CPU seconds | 100.748218 | 98.876804 |
| CPU ns/useful byte | 1.021083141 | 1.209271180 |
| Foreign CPU seconds | 0.101056901 | 0.270878285 |
| Exact applied foreign-CPU limit, seconds | 3.852260223 | 3.851960579 |
| Control p99 µs | 819.199 | 1245.183 |
| Application send-call p99 µs | 51.199 | 90.111 |
| RSS high-water at 20 seconds, KiB | 27,996 | 30,708 |
| Final RSS high-water, KiB | 27,996 | 34,584 |
| Lost packets | 785 | 0 |

The native RSS high-water rose **12.6221%**, exceeding the existing 10% growth limit. Its reported end heap-in-use value was 11,395,072 bytes, lower than the preceding observation's 13,746,176 bytes; the sampled memory profile has a different whole-role/shutdown scope. Neither those snapshots nor the cache's roughly 12 KiB logical bound identifies the cause of the RSS increase. Preserve the operational failure without declaring a leak, attributing it to the cache or waiving the rule. Latency tolerances passed; no empty seconds, write errors, DATAGRAM drops, group refusals or expiries were recorded.

Relative to the preceding clean accepted-span pair, native goodput rose only **0.3552%** and CPU/byte fell **0.0829%**. Baseline goodput rose 4.3768% and CPU/byte fell 2.7488%, worsening the same-pair ratios. These cross-run observations do not isolate a cache effect. The cache candidate's CPU gap is 0.086079725 ns/byte above its 1.123191455 ceiling, but that is not the target status of the restored source.

The filtered native metadata-pool Get/Put subtrees fell from 2.18 to 0.66 whole-role sampled seconds, or descriptively from 0.022924660 to 0.006913114 ns/byte. The mechanism worked on observed paths. However, preparation plus ACK owner estimates fell only from 0.083916876 to 0.081805179 ns/byte, and other owners changed enough that total CPU/byte stayed flat. The cache moves required reset work to ACK processing and adds checks and stores. These whole-role sample shares scaled by window CPU are not isolated intervention estimates or exact cache-hit rates. Both prior and current profiles use the same expanded parser; other pools are excluded from the filtered Get/Put view, and overlapping owner/function views are not added together.

Native calls per useful byte remained within 0.023% of baseline. Native counters recorded 4,993,160 calls, 2,132,289 groups and 4,993,153 members, with independent window boundaries and about 2.3417 members/group. The retained accepted-member completion span remains active; it was not bypassed to obtain this result.

This fixture now preserves the actual inner decision duration and computed foreign-CPU limit as well as scoped scheduler, ksoftirq and role-CPU inputs. Independent reconstruction reproduces both strict contamination decisions exactly. The rule and read order are unchanged; the wider outer accounting duration remains a separate value. Background work outside fixture CPUs and their SMT siblings is not used as a contamination signal.

The cache was withdrawn by forward commit `d5e6c6bff571df3840ceab682abe7f4516e05691`, preserving `6b11f0e23e6c7038c06b0ad2ceb8eac8e5598e4d` and its immutable archive. The selected checkout's production Go is restored to the accepted-span design. It retains the exact-decision-input fixture correction and two independent baseline characterization files, so this is a new source revision, not a new measurement of the restored binary. Withdrawal characterizations passed with race detection in 1.291 seconds for root and 1.228 seconds for recovery. No unchanged full suite or operational pair was repeated. The retained design's most recent clean measurement remains 86.2% goodput / 115.3% CPU, still insufficient.

Raw evidence resides in `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/local-packet-cache-v1/`, with source archive, measured binary and the 153-file `local-packet-cache-v1-local-validation/` manifest adjacent. It retains before/final compiler binaries and assembly, escape logs, source/test patches, layout and focused-pool inspection, broader rejected-design analysis, independent reviews and validation receipts. Supplemental evidence records the completed analysis, exact contamination replay, source withdrawal and independent audit. No workload is running or queued.

This adds **five invocations / 183.194969398 measured host-seconds**: 29.085146726 for build/semantic checks and 154.109822672 for observations. Preparation and quiet checks add 4.156450225 seconds separately. Cumulative history is **218 invocations / at least 4513.889150094 measured minimax host-seconds**, including failed and contaminated evidence. Local tests, compiler inspection and local profile analysis are not minimax workload invocations.

Recommendation: carry forward verified improvements while judging the complete pipeline against the unchanged targets. Keep [N3 #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) stopped and [draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) held until a sufficient replacement is supported and packaged.
