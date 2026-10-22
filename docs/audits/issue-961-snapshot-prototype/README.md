# Issue 961 snapshot-only feasibility experiment

Question: can private space-qualified packet keys and constant-sized registration-prefix snapshots correct Reno recovery and HybridSlowStart ordering, including a PTO-retired CE anchor, without growing packet metadata, moving BBR's ordinal counter, or changing wire numbering and history lifetimes?

This is an authorized throwaway experiment following RAS consideration `20261022T020026-3262293578b552d5f747aecb`, based on evidence commit `78096061e13bdac5cbfad66ce40db9289ca92959` and production source `71581cfc3227ca579643fea1f8afa38e3ce5536d`. The original assessment, fixture and captures are frozen. No production adoption, shipping, ADR amendment, performance campaign or second RAS cycle is authorized by this experiment.

The selected experimental policy preserves Reno's latest eligible-send frontier: ACK-eliciting non-path sends advance it, MTU sends participate, and path/ACK-only sends do not advance it. A frontier snapshot contains the complete registration prefix at that eligible send, including excluded registrations in other spaces that preceded it. The private controller freezes this prefix only when it accepts a cutback. Strictly greater space-qualified numbers are after the prefix; a space absent from a prefix has no earlier registrations. Recovery exit retains the existing in-flight ACK eligibility and the existing no-prior-ACK reporting behavior. HybridSlowStart owns a separate round snapshot at its existing receive-round start. This selects a preservation policy for the experiment, not a new production decision or an all-registration recovery policy.

The experiment uses the real Go controller and recovery handler as its trace harness because the design question depends on their existing side effects, ECN validator and disposal rules. It does not create a copied state-machine demo. The finite evidence set is the four paired archived regressions, prefix-order/eligibility traces, real-tracker CE feedback with present and PTO-retired anchors on either side of the frontier, HybridSlowStart sample/round traces, affected lifecycle cases and existing affected-package suites. BBR sources and packet layout must remain unchanged. Live prototype source changes will be removed after recording the result; a frozen patch and replay recipe may remain as experiment evidence.

Result: **snapshot-only ordering is feasible under the selected preservation policy in the declared finite domain.** All eight archived cases and nineteen new focused leaf cases pass against the real controller/handler through the archived overlay. The ordinary affected-package suites, affected-package race suites and `go vet` pass. The real ECN tracker confirms the missing-record case without mocked congestion feedback. Packet metadata stays 80 bytes on Go 1.27.3 darwin/arm64. No BBR counter, rich feedback implementation, wire generator, history representation, frame ownership or retention bound was changed. Live prototype Go edits have been removed; only immutable experiment evidence remains on this branch.

## Prefix rule and ownership

For a registration prefix ending at frontier F, store the largest registered packet number in each of the three spaces and a bit indicating whether that space exists in the prefix. For a registered packet (S, N), it is after F exactly when S is absent from the prefix or N exceeds the stored number for S. Equality is before-or-at, never after. The proof follows from strictly increasing packet numbers within each space: every earlier registration in S has a number no larger than its prefix maximum, and every later registration in S has a larger number. The rule does not reconstruct the packet's ordinal and does not require its live record to survive.

The premise is a registered packet identity admitted by the existing handler guards, including its existing skipped-number validation and ECN checks. This is not a new ACK validator or proof about arbitrary peer-supplied integers. 0-RTT and 1-RTT normalize to the same application-space slot. Skipped numbers are not registrations. Pooled-object reuse is irrelevant to this ordering representation because no identity field is added to a packet object.

The controller observes every registration, including path probes through a separate experimental notification path that does not charge pacing or flight. On an ACK-eliciting non-path send, it copies the complete registration prefix into its eligible frontier. This captures excluded registrations in another space that preceded the eligible send. It does not independently collect the last eligible packet from each space: that would omit such earlier excluded registrations and misclassify a missing CE anchor.

Only the private controller owns the cutback decision and frozen cutback prefix. An accepted congestion event freezes the eligible frontier; a suppressed event leaves the frozen prefix unchanged. Recovery exit retains the real handler's in-flight ACK gate. The controller retains the old no-prior-ACK reporting behavior: a cut can reduce the window before any ACK, while `InRecovery` remains false until an eligible ACK is observed. A subsequent old ACK cannot reactivate recovery after a newer eligible ACK has exited it. HybridSlowStart owns its receive-round prefix separately and freezes it when its existing receive-round start runs.

The prefix remains valid after removing the packet that supplied an anchor because wire identity is not reused. Retry continues the generators from their next values, key discard leaves dead-space facts in the controller, and 0-RTT rejection retains shared application numbering. The actual migration path constructs a fresh Reno sender while preserving application wire-number continuity; old disposed records cannot be forwarded as new eligible ACKs by the handler. These lifecycle observations support the finite traces below, rather than a claim of exhaustive lifecycle verification.

### Deliberate policy limit

This experiment preserves the latest eligible-send frontier rather than changing recovery to freeze all registrations at cutback time. A trailing ACK-only or path registration after that frontier is consequently classified as after it even if it already existed when the cutback was processed. The prefix traces explicitly distinguish excluded registrations before and after the eligible frontier. Choosing an all-registration cutback frontier would change that preserved behavior and requires a separate, explicit correctness decision; the successful experiment does not silently select it. HybridSlowStart round eligibility and recovery-exit eligibility remain separate from the predicate used to locate a CE anchor.

## Finite evidence

| Evidence | Result and scope |
| --- | --- |
| Four archived recovery scenarios, each with a single-space control | All eight pass: repeated cuts, early recovery exit, suppressed later-flight cut and stuck recovery are corrected without global wire numbering |
| Handshake/application handler trace | Overlapping Handshake/application numbers cut once, a post-cutback application ACK exits recovery, and a later application loss cuts again |
| Four excluded-registration prefix traces | ACK-only and path registrations both before and after an eligible send in another space receive the expected classification; a suppressed callback does not advance the boundary |
| Initialization / no prior ACK / old ACK after exit | Preserved no-prior-ACK reporting, correct first-ACK transition, new-space exit, and no reactivation from an older ACK |
| 0-RTT/1-RTT identity | Both encryption levels address the same application prefix |
| Four real-tracker CE traces | Present and PTO-retired anchors, each before and after the cutback frontier, produce identical classifications and window results |
| Mixed accepted CE and packet-threshold loss | One cutback, with loss from the just-frozen prefix suppressed |
| Duplicate and reordered CE ACKs | No repeated cutback; existing guard behavior preserved |
| HybridSlowStart | Eight RTT samples accumulate across interleaved Handshake/application sends; pre-frontier ACKs preserve the round and a strictly post-frontier eligible ACK ends it |
| Retry, Initial key discard, 0-RTT rejection and migration | Number continuity, disposal and controller-reset traces pass |
| Forced intentional application skip | An ACK covering the skipped number is rejected; the skipped number does not count toward packet-threshold loss |
| MTU and path eligibility | MTU loss is excluded from congestion response, an eligible MTU ACK can exit recovery, and ACK-only/path-probe ACKs do not exit recovery |

The canonical missing-record construction first sends ten ECT(0) packets and acknowledges them with consistent counts to establish capability through the actual `ecnTracker`. It then retains application packet 10 as ACK-only and sends ordinary packet 11. `QueueProbePacketAt` retires 11 while leaving 10 live. The ACK for 10–11 advances the largest acknowledged number from 9 and contains one newly live record, so both real handler guards pass even though the anchor record is absent. Cumulative ECT(0)=11 and CE=1 counts pass the real validator. With anchor 11 before the frozen frontier, the window remains 26,880; with anchor 11 after it, the window becomes 18,816. Keeping 11 live instead produces the same results. Duplicate and reordered ACKs preserve the result. The absent anchor therefore cannot be dismissed as unreachable, but it needs no retained ordinal map.

The [focused traces](focused-traces.log) print registration, eligible and cutback prefixes, their presence masks, initialization/recovery flags, congestion window, receive-round prefix and sample count. The [paired regression output](paired-regressions.log) uses the original frozen fixture unchanged. The [replay output](replay.log) records all 27 leaf cases, including those eight original cases. These are controller/handler experiments, not a full TLS/UDP handshake or deployment-frequency study. No path-probe ECN feedback construction or broad native/performance campaign was run.

## State cost and preservation

Measured with `unsafe.Sizeof` on Go 1.27.3 darwin/arm64:

| Structure | Pinned baseline | Prototype | Difference |
| --- | --- | --- | --- |
| Mandatory recovery packet | 80 bytes | 80 bytes | 0 per packet |
| Reno sender, including embedded slow-start state | 208 bytes | 352 bytes | +144 bytes per sender |
| HybridSlowStart, already included in the sender row | 40 bytes | 112 bytes | +72 bytes, not an additional cost on top of the sender row |

See [baseline layout measurement](layout-baseline.log) and the layout entry in the focused traces. The experiment trades constant-sized controller state for a per-packet field. Its temporary compatibility wrappers and retained legacy slow-start fields are not an optimized production layout. CPU cost, allocator traffic, throughput, total connection memory and other targets were not measured. In particular, this is not a claim that default-Reno or BBR startup allocation cost is unchanged: construction still initially creates a Reno object even when BBR is then selected.

The patch changes three existing source files and adds one experimental helper plus two trace fixtures. The packet type, packet histories, packet-number generators, BBR dispatcher, BBR recovery/ECN bookkeeping and BBR sender implementations are byte-identical to the pinned source. Existing BBR tests run as part of the affected-package suites. The prototype's optional capability-interface/type-assertion dispatch is disposable scaffolding: a production plan must define typed private space-qualified callbacks and explicit path-registration fact delivery, rather than silently losing context through a legacy wrapper. The experiment does not claim that its temporary private interface is the final interface.

## Validation and replay

The following completed successfully on the live throwaway candidate before it was removed:

```sh
python3 docs/audits/issue-961-send-order/reproduce.py
go test ./internal/ackhandler ./internal/congestion -run '^TestIssue961Prototype' -count=1 -v
go test ./internal/ackhandler ./internal/congestion -count=1
go test -race ./internal/ackhandler ./internal/congestion -count=1
go vet ./internal/ackhandler ./internal/congestion
```

Recorded outputs: [affected-package suites](affected-packages.log), [race suites](race-packages.log), and [vet output](vet.log), which is empty on success. [receipt.json](receipt.json) identifies the source commit, exact candidate file hashes, policy, measured layout and command results. The ordinary package suites ran before the final Handshake/application trace was added; focused and race runs include that final trace. No production logic changed between those runs.

After removing the live Go edits, this one command successfully replayed the experiment:

```sh
python3 docs/audits/issue-961-snapshot-prototype/run.py
```

The driver checks the three original production files against the pinned source, applies [candidate.patch](candidate.patch) only inside a temporary directory, and builds the real packages through Go overlays. It also activates the original archived recovery fixture without editing it. It writes no Go source into the checkout and returns the actual Go test exit code; success is exit 0. The patch, trace fixtures and driver are frozen experiment evidence, not maintained CI inputs or a production fix. [SHA256SUMS](SHA256SUMS) identifies the archive contents.

## Recommendation and remaining adoption work

Prefer **snapshot-only adaptation under an explicitly accepted frontier policy** over the previous mandatory-ordinal hybrid. The finite experiment demonstrates the missing-record CE case, correct cross-space recovery comparisons, and meaningful HybridSlowStart round behavior without a per-packet order field or a BBR counter relocation. Upstream's shared generator/history remains a viable broader alternative if production planning reveals an unmet requirement; nothing here validates a blind cherry-pick or makes the local design exhaustive.

This option avoids the mandatory-packet-metadata exception identified against ADR 0009/D04. A production correctness decision must still explicitly record the changed default-Reno cross-space behavior, selected frontier, controller ownership, constant state cost and unchanged opt-in BBR constraints. It does not follow that the historical “Reno unchanged” wording permits an unrecorded policy change. ADR 0004's registration, connection-goroutine and asynchronous-I/O ownership remains binding.

The next production plan should contain one atomic controller/HybridSlowStart/ECN conversion, with typed private keys and path-registration prefix delivery, rather than a preparatory BBR counter/packet-layout refactor. Attach the paired recovery regressions, accepted real-tracker CE cases, slow-start traces, disposal/migration/skip/probe coverage, affected BBR tests and compatibility checks to that change before it can land. A later slice may record aggregate evidence; it must not postpone checks that protect an earlier shipped contract. Revalidate at the implementation HEAD, use relevant existing emission/integration coverage, and keep native/performance qualification separately scoped. Coordinate #962's default-Reno undo with this single boundary owner; do not absorb its loss policy, #626's lookup work or a #887 baseline change.

This records an experimental technical choice and a recommendation, not accepted production design, implementation authorization, a merge, or completion of #961. No second RAS run or automatic document fixer was used. The original assessment and archived evidence remain unchanged, and the live prototype has been deleted after capturing its answer.
