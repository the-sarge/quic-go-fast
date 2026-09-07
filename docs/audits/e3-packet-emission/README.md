# E3 ordinary/GSO packet emission receipt

The concrete private `packetEmission` value now owns ordinary and GSO capacity checks, construction, short-header registration, batch boundaries and queue transfer. `Conn` consumes its progress, stop reason, pacing deadline and capacity wakeup while retaining lifecycle and receive fairness. The initial run-loop queue guard still precedes handshake-MTU feedback. Recovery queries remain interleaved with registration during GSO assembly; registration still precedes asynchronous I/O.

The owner binds typed references to the same packer, recovery and queue slots used by the unmigrated connection paths. Five synchronous policy operations expose packet size, logging, idle activity, connection-ID notification and pending receive work. There is no unrestricted connection dependency, shadow accounting, new worker, timer, per-packet object or completion round trip. Short-header registration has one implementation; the connection's legacy registration method forwards to it, including the existing path-probe distinction.

## Ownership and finite evidence

The supported domain is constructed ordinary/GSO 1-RTT output, the existing recovery modes and queue occupancy 0..8. Existing packet/frame types and packer/recovery objects own representation. Ownership and ordering are universal obligations inside these entrypoints; the tests and performance observations provide example-level evidence. Runtime extraction is shipped behavior and caller-buffer cleanup is required safety enforcement. Tests and capture scripts are disposable verification aids; this receipt, source binding and frontier changes are metadata. No maintained verification tool is introduced.

Contract closure applies to the plan's third ownership row. Emission owns each construction buffer until `Send` consumes it. Empty construction and fatal appends release that storage; a fatal append does not refund packet numbers or already registered protocol state. E2 retains sole ownership after handoff. No new mutation campaign was required because the new lifetime regressions distinguished the pre-fix behavior directly.

| Semantic case | Enforcement and finite evidence |
| --- | --- |
| Ordinary/GSO output, full and short final segments | `TestEmissionDatagramOutput` decodes real-packer output at the socket boundary and accepts ACKs there, establishing prior registration. Existing GSO batch/size tests remain. |
| Full queue, progress and capacity wakeup | `TestEmissionFullQueueResume` exercises the real run loop and worker; `TestEmissionResultQueueWakeup` observes occupied/full queue outcomes without consuming pending data when full. |
| ECN transition | `TestEmissionECNBatchBoundary` changes only ECN policy after real registration and observes separate marked batches; existing GSO ECN tests remain. |
| No data | `TestEmissionEmptyCallerBuffer` observes release and no handoff through the real packer. |
| Fatal first append or later append | `TestEmissionFatalCallerBuffer` covers ordinary/GSO with failure on append 1 or 2. All four cases failed with refcount 1 before cleanup and pass afterward. It observes retained prior registration and packet numbers, partial GSO disposal and prior ordinary handoff. |
| Receive fairness | `TestEmissionReceiveFairness` leaves the next DATAGRAM queued and requests an immediate retry after pending receive work; existing receive-prioritization tests remain. |
| Recovery outcomes and connection consumption | `TestEmissionResultRecoveryOutcome` preserves real registration while selecting congestion, hard-block, pacing and PTO outcomes. `TestEmissionResultConnection` covers no data and receive yield through connection dispatch. |

Lifetime observations occur before any later buffer allocation, so pool reuse cannot disguise release. Fault injection is at the append boundary; successful appends retain the real packer and recovery. This aid is not a new production hook or an exhaustive packet-packer failure proof. The accepted finite family is covered; there are no uncovered E3 cells.

Full `go test ./...`, focused emission/connection/handshake-MTU race checks and `go vet ./...` passed on macOS with Go 1.27.0 and native minimax Linux with Go 1.27.1. Local `go tool gcassert ./...` and golangci-lint passed. Existing real stream/DATAGRAM, QUIC v1/v2, handshake, migration, MTU, close and qlog tests remain in the full suite. Exact final-head and hosted certifications are recorded in PR #41.

## Retained legacy boundary

The connection retains `triggerSending`'s ACK/PTO/recovery dispatch and `emitPackets`'s path/MTU/control/handshake prelude; `maybeSendAckOnlyPacket`, `sendProbePacket`, `sendPackedCoalescedPacket`, `sendConnectionClose` and direct path-probe entrypoints remain. Packer operations retained for those paths are `PackCoalescedPacket`, `PackAckOnlyPacket`, `PackPTOProbePacket`, `PackPathProbePacket`, `PackMTUProbePacket`, `PackConnectionClose`, `PackApplicationClose` and `SetToken`. Queue startup, close, replacement and direct `SendProbe` remain connection-owned. E4/E5 narrow these typed seams and E6 retires the remaining legacy packer surface. No ordinary/GSO construction loop or parallel short-header registration implementation remains in `Conn`.

The trace covers established sends, synchronous idle/qlog/connection-ID effects, the legacy registration bridge and E2 queue handoff. Unmigrated constructor cleanup, direct-probe disposal, retained close storage and physical-link/application performance remain outside E3. The owner adds 48 embedded bytes to `Conn` (1168 to 1216), with no separate setup allocation or cold-close change.

## Performance comparison

**Disposition: pass.** Both current four-core cells completed ten adjacent alternating parent/candidate pairs with 2 s warmup, 60 s measurement and 1 s drain. Native Linux Go 1.27.1 endpoints use separate processes on loopback, established QUIC v1, qlog off and 1071-byte application DATAGRAMs offered at 4 Gbps. GSO is active in P3 and disabled in P4. All 40 captures passed payload, probe, ledger, capability and sampled host-contention checks. No sample or campaign was replaced. The paired qlog-on P3 setup diagnostic also passed; it is not acceptance evidence.

Client cores are 8–11 and server cores 12–15, with four physical cores and GOMAXPROCS=4 per endpoint and SMT siblings 24–31 reserved. Other user/system/machine work is restricted to cores 0–7 and 16–23. The collector is pinned to core 0 and the monitor to core 1, outside endpoint placement; the retained host snapshot observes those affinities and both endpoint affinities. Thus this capture corrects the tooling placement limitation of the historical E1 receipt. Temporary CPU restrictions and reservation/timer state were restored after capture. These results qualify this finite Linux loopback comparison, not physical links or non-Linux performance.

Maximum per-run average measured guest CPU was 0.00000%; maximum average sibling busy time was 0.12437%, within the frozen 0.1% and 1% thresholds.

The [analysis](summary.json) uses 20000 whole-pair bootstrap resamples, seed 20260907, geometric paired ratios and one-sided 95% percentile bounds. A whole run is the experimental unit. The [analyzer](analyze.py) is unchanged from E1; extract the capture and invoke it on `campaign-01` to reproduce these bounds.

| Cell | Goodput lower bound | CPU/unit upper bound | Bytes/unit upper bound | Probe p99 upper bound | Bad-probe upper difference | Result |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| P3 | 0.997562 | 1.003098 | 1.001199 | 0.947787 | 0 percentage points | Pass |
| P4 | 0.997487 | 1.001926 | 1.001648 | 1.018224 | 0 percentage points | Pass |

Bounds are candidate/parent ratios except the absolute bad-probe difference. Required limits are goodput ≥0.95, CPU/bytes/p99 ≤1.05 and bad-probe difference ≤+0.1 percentage point. No failed or missed probes occurred in the acceptance samples.

| Cell / variant | Goodput Gbps | Mean offered | Mean admitted | Mean delivered | Mean ingress drop | Mean post-admission drop | Mean probe p50 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| P3 base | 3.722 | 28011204.000 | 26062834.800 | 26062758.700 | 1948369.200 | 76.100 | 1.159 |
| P3 candidate | 3.718 | 28011204.000 | 26033312.500 | 26033220.200 | 1977891.500 | 92.300 | 1.066 |
| P4 base | 3.795 | 28011204.000 | 26576678.500 | 26576495.900 | 1434525.500 | 182.600 | 0.473 |
| P4 candidate | 3.791 | 28011204.000 | 26546888.800 | 26546812.600 | 1464315.200 | 76.200 | 0.465 |

The DATAGRAM ledger unit is one application message; goodput is the geometric mean across runs, and the other columns are arithmetic means. CPU and allocation measurements include both endpoints and fixture costs from measurement start through drain. Focused allocation checks use the identical opt-in fixture and report 37 allocations per ordinary batch and 104 per three-packet GSO batch on both revisions: no added steady-state allocations. `testing.AllocsPerRun(1000)` internally uses one Go processor, with the same outer four-core process budget on both variants.

The [capture archive](capture.tar.gz), SHA-256 `888438a48c873c280b45ff3d97f11f3d9a015abcc054a57abceca65ef582ca2a`, retains all samples, endpoint logs, CPU observations, manifests, scripts, build provenance, setup/allocation diagnostics and restoration records. [Source binding](source-binding.bundle) retains the measured source relative to the declared parent; verify it with `git bundle verify` in a clone containing that parent.


## Source and review binding

The measured parent is `40877a7fc8d6c33a9181a46d832e45c79bb08abb`; the measured runtime candidate is `fe35b808db043f976f455f3f1f40298ae1efea47`. The source bundle retains the candidate and subsequent comment/test-import correction relative to the parent. That correction adds a linter explanation and formats test imports; it does not alter runtime behavior. Later receipt/frontier changes are metadata. The retained build manifest records source revisions, commands, toolchain/environment, fixture hashes and compiled binary hashes. The endpoint source is identical on both variants and is retained with the capture sources.

The review budget is one initial review plus at most one replacement, with manual dispositions and exact-head verification of accepted findings. PR #41 owns both product changes and its merge-time E3 completion/E4–E5 frontier transition. Review history and certification receipts belong in the PR discussion, separate from the normative plan and mutable tracking surfaces.
