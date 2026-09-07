# E3 certification handoff audit

This scoped audit evaluates the unmerged E3 candidate and its unresolved local certification failure. It does not implement or merge E3, change runtime behavior, reopen E1, or certify a later head. The normative continuation belongs in the E3 section of the [packet-emission plan](../adr/2026-09-07-packet-emission-plan.md).

## Retained evidence

The parent is `40877a7fc8d6c33a9181a46d832e45c79bb08abb`; [PR #41](https://github.com/the-sarge/quic-go-fast/pull/41) remains draft at `e2b5c7f471214dc05ef08f0d2decd2d41b3f9686`. Its [review disposition](https://github.com/the-sarge/quic-go-fast/pull/41#issuecomment-5575036064) records a completed initial RAS review with no accepted fixes or follow-ups; five reviewers completed and two Codex CLI executions failed. Its [certification stop](https://github.com/the-sarge/quic-go-fast/pull/41#issuecomment-5575062061) retains the failed macOS full suite, passing bounded diagnostics and passing independent checks. The candidate contains the E3 ownership/performance receipt and merge-time completion/frontier transition; those are not current default-branch state.

The P3/P4 campaign passed ten paired comparisons per cell without replacement, with no added steady-state allocations. All 33 hosted checks on the candidate passed. Native Linux Go 1.27.1 full tests, focused races and vet passed on that exact head. macOS Go 1.27.0 focused races, vet, lint and gcassert passed, but `go test ./...` failed `TestHandshakeWithPacketLoss/drop_1/3_of_packets_in_direction_to_server/retry:_false/server_speaks_first#02` at `handshake_drop_test.go:113` with “server connection not closed.”

Twenty targeted parent runs and twenty complete loss-test runs on each of parent and candidate subsequently passed. Those runs are diagnostic observations, not a cause, fix, or replacement certification. The earlier E1 receipt reports the same line under a different direction/toolchain; that is evidence of previous instability, not evidence excluding an E3 regression.

## Source-grounded questions

At the pinned parent, `integrationtests/self/handshake_drop_test.go:109–113` waits for the client receive goroutine after the server has written and closed its stream. The message labels this as a connection-close failure, but the goroutine may still be accepting a stream, reading data, or closing the connection (`:81–99`). A useful diagnosis must distinguish those phases before assigning a runtime owner.

The fixture sets a two-minute simulated-time deadline (`:204–207`). Its one-third drop callback calls global `math/rand/v2.IntN`, retains no seed or decision trace and ignores its requested direction argument (`:168–200`); actual packet direction only selects the consecutive-drop counter. The first/first-three callback does apply the requested direction (`:149–165`). These are verified fixture facts, not a causal explanation of the observed timeout and not authorization to alter loss semantics opportunistically.

`integrationtests/self/simnet_helper_test.go:91` owns the router boundary for observing actual direction and packet delivery decisions. The sender-first protocol helper owns observation of stream receive and connection-close completion. The runtime candidate changes established sending and short-header registration, so the failed test cannot be dismissed as outside the compiled changed surface merely because its name contains handshake. No evidence establishes an external infrastructure failure or a Go patch-level defect.

## Invariant and root disposition

The required preservation outcome is completion of the accepted handshake/transfer scenario under its existing test conditions. The test observation is a verification aid; runtime emission and caller-buffer cleanup remain shipped behavior and required safety enforcement. No maintained diagnostic framework is accepted. The root and central enforcement seam of the timeout are unknown. There is no supported repeated-root conclusion: the old E1 and current observations share an assertion location, but no established causal owner or distinct semantic counterexamples after a central fix.

The original shared baseline prohibits green-seeking reruns. Any change to the disposition of this specific failed run must therefore be explicit in the normative contract with its rationale, authority and exact limits. Passing other tests does not relabel the failed run.

## Grilled disposition and slice audit

Retain E3’s one child (#27), task and product PR #41. Keep certification strict. The operator explicitly chose “Bounded investigation; keep certification strict” during this scoped handoff. This package grants no isolated-failure exception. The governing contract permits one instrumentation revision, at most 100 exact-subtest capture attempts or 60 minutes, and at most ten paired causal replays. Those are new diagnostic allowances, not replacement certification passes or a renewed broad random campaign. This handoff itself runs no diagnostic experiment or implementation.

Independent read-only audit of the pinned parent/candidate agreed that the observed phase and actual loss chronology must precede root attribution. It confirmed the ignored direction parameter and identified the failed duplicate-name configuration as post-quantum enabled with a short certificate chain. The audit found no evidence establishing an emission defect, an external infrastructure fault or a harmless timeout. Its recommended finite budget and stop conditions are incorporated into the current E3 contract.

| Slice gate | Current disposition |
| --- | --- |
| End-to-end outcome and existing work | Retain ordinary/GSO ownership and caller-buffer cleanup in PR #41; do not accept unmerged work as a baseline for E4/E5. Diagnose the required preservation failure before certification. |
| Single owner and authority | One packer, recovery state and queue on the connection goroutine; short-header registration stays in emission. Diagnostic observations grant no protocol or persisted authority. |
| Transitional seams | Retain the five synchronous policy hooks and typed slots, the legacy registration bridge, and existing handshake/ACK/PTO/probe/close/token/queue-lifecycle paths. Their removal remains E4/E5/E6; no diagnostic seam may widen production authority. |
| Blast radius | The captured failure may involve accept/read/close, actual loss sequence, or emission/registration. Only a demonstrated correction within those accepted E3/runtime or narrow fixture seams may continue here. Handshake/recovery policy and untraced scheduler/transport effects remain stops, not silently accepted scope. |
| Representation and guarantee | Existing typed packets/router own diagnostic inputs; no new parser. Original ownership guarantees remain universal within their entrypoints. Replay and regression evidence is example-level and must expose ordering divergence. |
| Artifacts and evidence | Production code is shipped/safety behavior; capture and tests are disposable verification aids; plan/receipts/mirrors are metadata. No maintained aid, exhaustive scheduler proof, new platform matrix or statistical rerun criterion is added. |
| Context fit | Fresh continuation receives at most 30k input tokens of current contract, compact unresolved evidence and relevant code. Failure to isolate a cause or fit the correction within that budget stops. |
| Strongest further split | A separate “certify later” slice cannot make E3 independently green. A separately owned handshake-policy repair is appropriate only after concrete causal evidence establishes that boundary; it is not invented now. |
| Strongest adjacent merge | Merging E3 with E4 to investigate a test named handshake would mix independently assigned ownership families and spend context without causal evidence. Rejected. |
| Blocking edges | E2 and the adoption decision are satisfied. E4/E5 depend on the unmerged common emission owner, and E6 depends on E4/E5. No new ordering dependency is invented. |
| Terminating outcomes | A demonstrated in-scope correction plus remaining review/performance and exact-head gates permits E3 merge; no capture/cause or a scope crossing preserves the stop without another campaign. |

This is a scoped current-contract repair, not a legacy-program rebaseline: no recursive proof obligations or obsolete product requirements are removed. The ownership closure matrix remains unchanged; the timeout has no established central runtime owner to add as a speculative row. The strict-certification continuation composes with both shared baselines and the fork’s inherited hosted workflows. No repository overlay copy is needed because no baseline waiver is selected.
