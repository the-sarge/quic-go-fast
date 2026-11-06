# ECN settlement and writer reconciliation prototype for N3 #1202

**Verdict: retain as useful diagnostic components, with a mixed overall result.** The clean composed pair reached **82.7% baseline goodput and 114.5% CPU/useful byte**, failing both unchanged 95%/110% targets. Against the preceding retained accepted-member observation, native CPU/byte fell 1.74% while native throughput fell 2.49%. The intended ECN and notification mechanisms improved, but this is neither a sufficient repair nor an isolated causal estimate of either component. No automatic retry, readiness change or product qualification follows.

## Source and exact scope

The candidate is `35b83b13a3b94ec3665889c0f6a0844d0f851060`, based on the cache-withdrawal revision `d5e6c6bff571df3840ceab682abe7f4516e05691`. It retains preparation-owned construction, the exact empty-loss-prefix guard and callback-free accepted-member completion, plus the useful packet-lifetime characterizations and exact contamination-decision inputs. The [packet cache](2026-11-05-native-packet-cache-prototype.md) remains withdrawn. The source archive SHA-256 is `a4774d47913cd893ea67d683347add9c0ad0c8746c3b3fe99a3d51ae6a4ce4f5`; the measured Linux binary is `dfa76d8f73e4842ba786d0c2cc296f103edddb690d2528dc97f0edb9c9bb46ad`. Both compared modes use that same binary and unchanged 10-second warmup/60-second ordinary STREAM GSO-on workload. There is no wire, native-member geometry, capacity, admission, fairness, deadline or outcome-contract change.

Accepted ECN settlement now resolves one record index within the already-proved callback-free packet interval, runs unchanged qualification/testing/constraint effects, checks ledger failure and coalesces using that index. The generic path still searches afresh after observers. The storage owner merges equivalent adjacent settled ranges with one deletion, choosing the survivor that minimizes movement; every vacated pointer slot is still cleared. A nonescaping pointer predicate eliminates record-value comparison copies. No packet outcome is published early, and no pointer crosses a storage edit or generic callback.

After atomically publishing the final native fact, the writer tries to enter the existing canonical control turn immediately. Success carries the acquired lock into the one existing progress/claim body; failure notifies the independent controller before waiting for the lock. The existing armed-deadline predicate still covers new/earlier obligations, terminal always notifies, and all native I/O remains outside control. Nothing reads the old token or payload after publication. This replaces a redundant event when the mutation is already observed; it does not remove the independent watchdog. TryLock may change normal-mode mutex waiter scheduling, so scheduling equivalence is not claimed.

## Local proof and generated code

Full root tests passed in 12.234 seconds, full recovery race tests in 70.230 seconds, and the broad native race family including the final new writer tests in 3.349 seconds. Vet, tidy and whitespace checks passed. ECN behavior tests first passed on unchanged source, including a nested qlog completion that shifts the outer target's index. New writer cases distinguish direct successor/park/resume without a redundant notification, notification before a contended lock wait, irreversible fault/timely-fact ordering, terminal join and earlier retry coverage. Their private delayed-runner fixture uses longer logical deadlines to avoid a 50 ms scheduling hazard; production limits are unchanged. The expected old-source redundant-wake assertion failure and an earlier ECN harness run that selected zero tests are retained, with neither counted as passing candidate evidence.

The complete local inspection binary is `4cc6bed7ce43d050ae5d0f6ed315fe7a5c2fb0edb8ca3dcf53629b456395f4bd`, distinct from the remotely built measured binary. On Go 1.27.3/Linux amd64, the coalescing body shrinks from 276 instructions/88 frame bytes to 189/48 and loses eight MOVUPS comparison-copy instructions. The accepted ECN helper has 92 instructions/40 frame bytes versus the generic helper's 152/80; the accepted-span frame shrinks 80→72. Scratch append loses another 32-byte comparison spill. Generic local coalescing gains a small PN adapter call, explicitly retained for observer safety. The writer grows 816→860 instructions while keeping its 1168-byte frame; successful TryLock skips notification and ordinary Lock, while failure preserves notify-before-wait. No added allocation is found. Nine relevant final functions match the reviewed combined compiler binary; nineteen other available common functions normalize identically against the accepted-span baseline. This is bounded instruction evidence, not whole-binary layout equivalence or predicted savings.

## Clean pair and tradeoff

| Observation | Baseline | Native |
| --- | ---: | ---: |
| Useful MB/s | 1600.309056 | 1324.160341 |
| Useful bytes in 60 seconds | 96,018,543,360 | 79,449,620,480 |
| Server window CPU seconds | 99.754036 | 94.480343 |
| CPU ns/useful byte | 1.038903867 | 1.189185580 |
| Scoped foreign CPU seconds | 0.098295737 | 0.252323011 |
| Control p99 microseconds | 786.431 | 1376.255 |
| Application send-call p99 microseconds | 51.199 | 86.015 |
| RSS high-water KiB at 20 seconds / end | 27,092 / 27,092 | 30,832 / 32,372 |

Exact recorded inner durations reproduce contamination limits of 3.851731336 and 3.852152072 seconds on fixture CPUs 8–15 and their SMT siblings 24–31. Both observations are clean. Latency, RSS-growth, close, empty-second and error checks pass; native RSS increases 4.995%. There are no write errors, datagram drops, refused/expired groups or native lost packets. Passing this memory check does not establish the cause of the earlier cache experiment's RSS failure.

The ratios are 0.827440384949 goodput and 1.144654108659 CPU/byte. The same-pair CPU ceiling is 1.142794253695 ns/byte, leaving **0.046391326163 ns/byte** to remove. Native goodput needs approximately 14.81% improvement to reach the unchanged floor, and CPU/byte needs 3.90% reduction to reach its ceiling. Relative to the earlier [accepted-member pair](2026-11-05-native-accepted-span-prototype.md), the native absolute changes are −2.49% goodput and −1.74% CPU/byte; baseline changes are +1.57% goodput and −1.05% CPU/byte. These single cross-run changes do not isolate either source component or prove a causal throughput regression.

## Mechanism and residual evidence

Notifications fall from 4,360,341 to 25,588, over 99%, while admitted groups fall from 2,124,740 to 1,994,247; notifications per group fall from approximately 2.052 to 0.01283. Progress calls fall from 9,455,866 to 6,009,187. Current groups contain 4,850,896 members, averaging 2.432 per group. Native calls/useful byte differ from the same-binary baseline by approximately 0.0009%. Accepted spans cover 99.9384% of consumed packet credits. Counter boundaries can straddle operations; these are mechanism observations, not exact per-operation causality.

| Callable owner, estimated ns/useful byte | Prior native | Current native |
| --- | ---: | ---: |
| Completion including preflight | 0.034177 | 0.024474 |
| Endpoint control/exchange | 0.046691 | 0.035471 |
| Writer excluding syscall/control | 0.029865 | 0.024905 |
| Registration | 0.095905 | 0.099404 |
| Preparation outside registration | 0.017562 | 0.014555 |
| ACK processing | 0.066355 | 0.074284 |
| UDP send syscall subtree | 0.265316 | 0.275033 |

The separate ECN mechanism view falls 0.027972→0.019730 ns/byte; it overlaps these owners and must not be added to them. The prior generic ECN-completion subtree has 1.78 sampled seconds versus 0.95 for the current accepted-only subtree; coalescing falls 0.87→0.42 seconds. Profiles apportion window CPU using whole-role samples, including warmup/shutdown; they establish neither exact removed CPU nor an isolated throughput effect. Other owners moved, and registration/preparation remains substantial.

Block/mutex samples do not identify TryLock barging as the throughput cause. Writer blocked-lock attribution falls roughly 31→13 ms and runner 27→10 ms; producer exchange rises 9→22 ms across approximately 77-second profiles. Corresponding mutex delay does not show a material increase. Runner select waiting grows as notifications disappear, which describes sleeping rather than CPU. Request-boundary writer-gap samples and queue occupancy change, but group sizes also change; these are not inter-syscall gaps. Keep waiter scheduling as a qualification concern without inventing a causal finding from counters.

## Evidence, accounting and disposition

Raw evidence resides in `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/ecn-writer-v1/`, with source archive, measured binary, exact runner and the 386-file `ecn-writer-v1-local-validation/` manifest adjacent. Supplemental analysis preserves parsers, independent verification, profile/lock interpretation and this receipt's review. Independent replay passes **2,770 checks with zero failures**, including identities, file hashes, exact contamination decisions, numerical verdict, profiles, histograms and invocation accounting.

This candidate adds **five invocations / 182.974409726 measured minimax host-seconds**: 28.870609972 build/semantic seconds and 154.103799755 observation seconds. Preparation and quiet overhead is 4.156866950 seconds separately. Cumulative history is **223 invocations / at least 4696.863559821 measured host-seconds**, preserving every earlier failure and contaminated observation. No workload or automatic retry is running or queued.

Retain the bounded ECN and writer changes as input to the larger redesign: their intended work reductions are supported, while the total result remains insufficient and mixed. Do not spend another experiment on the now-small merge subtree or infer that changing only wake policy will meet the targets. The next read-only assessment concerns larger connection-path work and whether any further native batching can preserve the accepted exact-outcome/fallback/revocation contract and fixed-geometry diagnostic scope. No such backend change is selected or authorized by this receipt. N3 #1202 remains stopped; draft PR #1230 remains held; R17 #1037/R18 #1038 remain blocked. No issue or OmniFocus state changes, replacement slice graph or shipping qualification follows.
