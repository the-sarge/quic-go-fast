# October 30 prerequisite closeout

This supplements the October 29 registration; it does not rewrite or discharge its launch binding. Registration SHA-256 remains `4dc4c3a7e4c257a067d720edcad4fd6b47b4cc345459165c7775a94967d6f46e`. PR #1056 remains draft. No experiment, benchmark, experimental calibration, paid provisioning/reservation, live guard/ledger change, Stage B or #599 dispatch occurred.

## Native Linux correctness passed

[Native receipt](native-correctness-receipt.json) records one execution of each of the three commands in the registered remaining-correctness inventory. All passed on existing shared host `minimax`, Linux/amd64 with Go 1.27.3, in 7.843 seconds including orchestration. The source export used pinned candidate `23c83b9896980511286ca8b81cd632f7ec56afed`; the driver verified overlay input hashes against the registered build receipt. GOMAXPROCS=4 and GOFLAGS=-p=4 bounded parallelism. No benchmark or stress loop ran. The [evidence manifest](evidence-manifest.json) identifies the transported source archive and retained output.

This closes the three source-correctness executions previously pending. It does not certify measurement isolation, clock alignment, routed capacity, the future endpoint binaries, or native calibration. The shared host was not selected as the measurement topology. Future allocated-host identity/capability checks remain mandatory; do not remove their time allowance on the basis of this eight-second run.

## Hosted CI failure retained

At `078441f33bf146667278548b33a4f1014a1297c8`, 32 checks succeeded and one failed: [push integration run 40983561620](https://github.com/the-sarge/quic-go-fast/actions/runs/40983561620), macOS job 123829131327, `TestHTTPServerIdleTimeout`. The [same-head PR integration run](https://github.com/the-sarge/quic-go-fast/actions/runs/40983567705) passed. The failed artifact is retained in [ci/](ci/) with [run identity](ci/run.json), [result](ci/result.json), [suite output](ci/output.log) and [HTTP capture](ci/capture.jsonl). Its run manifest explicitly says `source_complete=false`; the empty source patch and pinned Git tree are source evidence, not a claim that the artifact alone is a complete source archive.

The captured suite used Go 1.27.3 Darwin/arm64, TIMESCALE_FACTOR=3, version 1 and shuffle seed 1793322371700799000. The server reset its 30 ms idle timer at 01:06:56.117029Z. The client closed the consumed body at .118252Z. The test reached pre-teardown at .215184Z, after its 90 ms deadline. The timer fire was recorded at .219599Z, 102.570 ms after reset and after teardown began; the server's idle-timeout close followed at .219662Z. Capture completion reports zero drops/encoding errors. These timestamps are recorder observations, not a total ordering of timer events.

The leading hypotheses are delayed runtime scheduling and capture-recorder contention; a timer lifecycle error is not excluded. The timer callback logs synchronously before closing the connection, and the recorder timestamps after acquiring its mutex and writes under that mutex. Thus this capture cannot distinguish when the runtime invoked the callback from when it acquired the recorder. No externally caused infrastructure failure is established. No hosted job was rerun, timeout increased or product code repaired. The Stage A diff changes neither `http3` nor `integrationtests/self` relative to its base.

One [targeted local reproduction](ci-local-reproduction.json), with the same Go version and timescale, passed in 2.026 seconds (test package 0.422 seconds). It was not the shuffled whole-suite workload and cannot erase the hosted failure. Diagnosis is incomplete: a separate bounded HTTP timer/capture investigation must establish the cause and an accepted disposition before merge certification. Do not treat a later green run as resolving this recorded failure.

## Partial accounting reconciliation

[Accounting proposal](accounting-proposal.json) preserves the unchanged September 30 cumulative ledger and every retained reservation. The basis is 79.5346666667 experiment-hours, $87.085 and 25.3983234731 preparation-hours. Add the proposed two-hour Stage A validation allowance exactly once, including all preceding local checks and this closeout's correctness tests: the provisional experiment subtotal is 81.5346666667 hours. No paid resources were created; the retained cloud subtotal stays $87.085, pending billing reconciliation. These figures are not certified available credit.

[Preparation intervals](preparation-intervals.json) recover the previously unlogged implementation/preflight work from the native timestamps of the two relevant chats. Unioned occupied-turn intervals through the receipt cutoff add 3.2527777778 hours, yielding a provisional preparation subtotal of 28.6511012508 hours. Overlapping coordinator/child intervals count once. The proposal conservatively includes waits inside those turns and rounds timestamp boundaries outward; it is not a measurement of active human effort. It excludes gaps between turns. Other chats, earlier October work, external/human effort and work after the cutoff remain unresolved. Do not substitute this partial reconciliation for the full ledger.

Read-only billing linkage inspection returned `billingEnabled=false` and no linked account for historical project `bbr-q635-20260925`. This does not establish settled charges or resource cleanup. The bounded controller evidence inventory still ends at September 30. The historical forecast and launch reservation details overlap the retained basis and are not added again. Keep the cumulative ledger's higher cloud amount until its $0.0533333333 difference from the launch inventory is reconciled.

## Remaining decisions and evidence

1. Resolve the recorded macOS failure under a bounded HTTP diagnosis scope before certifying merge. No root cause or infrastructure-only retry justification has been established.
2. Supply or locate the settled campaign billing/export and the missing October obligation/effort records; reconcile the full remaining forecast without releasing failed or unused experiment reservations.
3. Select an accessible, billing-linked project and approve the separate infra guard change and deployment verification. Current staged source still uses $150; accepted authority is $200, with 192 experiment-hours and 72 preparation-hours. The patch remains unapplied.
4. Bind and price the exclusive Linux topology, obtain remaining native isolation/capability/clock/calibration evidence, and return the exact six-observation request for launch approval. No Stage B or 520-case qualification dispatch follows implicitly.

This closeout is supplemental evidence only. Immutable registration, registered binaries, original receipts and accepted scope remain unchanged.
