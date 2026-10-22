# Issue 962: reordered-path loss detection and Reno undo

Status: completed reproduction and adoption assessment; recommendations await maintainer acceptance. Issue [#962](https://github.com/the-sarge/quic-go-fast/issues/962) remains `enhancement` + `needs-triage`. No production port is included. This directory is frozen assessment evidence, not a maintained CI dependency.

## Recommendation

Adopt the detection/history corrections as one independent correctness slice. Adapt packet-threshold suppression and bounded time-threshold growth as a second, explicitly accepted recovery-policy slice. Adapt Reno undo as a third slice using the controller-owned, space-qualified prefix contract already accepted in [#961's agent brief](https://github.com/the-sarge/quic-go-fast/issues/961#issuecomment-6331591220). Defer performance claims and execution of a comparison campaign until a candidate exists and its finite measurement plan is accepted. Reject replacing the fork's BBR evidence pipeline or importing upstream's shared packet-number generator/history merely to obtain these behaviors.

The packet/time policies are permitted design choices rather than evidence of protocol noncompliance in the current fixed thresholds. [RFC 9002 sections 6.1–6.1.2](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.1) describe adaptive reordering thresholds and their tradeoff: greater tolerance also delays genuine-loss detection. The missed detection/history cases are correctness defects within the existing diagnostic mechanism. Keep the umbrella assessment categorized as an enhancement; a separately accepted hygiene ticket can be a bug.

## Sources and replay boundary

Fork source: `1c4a28b7bc000bbc521332f5d7145e6493b42709`. Upstream control: `2ccf27f2a1883b1a08cbbbe65bd3db168b4f8dd4`, the snapshot named in the issue. Runtime: Go 1.27.3, darwin/arm64. The four fork commits after the issue's original `71581cfc` inspection concern DATAGRAM closure/cancellation and journals; they do not change the assessed recovery/congestion source.

The frozen fixture uses real `SentPacket`, `ReceivedAck`, loss timeout, migration and Retry entrypoints, real default Reno, real ECN validation, fixed monotonic timestamps and explicitly enumerated ACK ranges. Frame callbacks and qlog events expose the effects. It has 19 leaf cases. The fork fails eight candidate-behavior cases and passes eleven controls; upstream passes all 19. A second fork run reproduces the same observations and assertions, differing only in Go's elapsed-time footer. The unmodified fork also passes `go test ./internal/ackhandler ./internal/congestion`, including its existing BBR recovery, undo, disposal, CE and PTO evidence tests.

The same fixture is used on both trees. The replay driver changes only `PopPacketNumber`'s argument from encryption level to upstream's packet-number-space type and the return arity of `OnLossDetectionTimeout`. It does not patch production code. Upstream contains other changes, including its shared-numbering rewrite: this is a behavioral control, not a demonstrated cherry-pick or fork port. The tests register PING-bearing packets at the recovery boundary; they do not exercise packet serialization, TLS/UDP, real path reordering, stream payloads or application DATAGRAM delivery. They establish neither deployment frequency nor throughput benefit.

## Observations

| Scenario | Fork observation | Upstream observation |
| --- | --- | --- |
| ACK covering only an already-lost packet and the previously ACKed largest packet | No spurious-loss event | One event; flight unchanged |
| Reordered ACK covering an old loss and one newly live packet | No spurious-loss event | One event; only the live packet leaves flight |
| Non-reordered ACK with a newly live packet | One event | One event |
| MTU probe / ACK-only packet previously retired by ordinary loss processing | One false spurious-loss event for each | No spurious-loss event |
| Path-probe receipt | No spurious-loss event | No spurious-loss event |
| Migration followed by ACK of an old loss alongside a fresh packet | One old-path spurious-loss event | No old-path spurious-loss event |
| Retry disposal followed by a fresh ACK and a delayed old 0-RTT receipt | No stale receipt effect | No stale receipt effect |
| A new application flight after demonstrated spurious loss | Two packets immediately declared lost by packet distance | No immediate loss; timer later declares all four missing packets lost exactly once |
| No spurious proof / Initial / Handshake / new path after migration | Packet-distance loss remains active | Packet-distance loss remains active |
| Late-only proof of time-based spurious loss | Deadline stays at 1.625 s; two later packets lost at that deadline | Deadline moves to 1.750 s; the old deadline loses nothing; genuine losses occur at the new deadline |
| All losses supporting the Reno cutback later acknowledged | Window stays 26,880 bytes, outside slow start | Window restored to 38,400 bytes with pre-cutback slow-start state |
| One genuine loss remains | Window stays 26,880 bytes | Window stays 26,880 bytes |
| 77 reported losses, only 64 retained history records | Window stays 26,880 bytes after all receipts | Window stays 26,880 bytes: missing history cannot prove complete undo |
| New genuine losses in the ACK that also proves old losses spurious | New reduction retained at 18,816 bytes | New reduction retained at 18,816 bytes |
| Real validated CE plus old spurious receipts, then a duplicate ACK | CE reduction retained at 18,816 bytes; no duplicate reduction | Same |

The genuine-loss case ages the new flight beyond both the fixed and relaxed thresholds. During fixture development, a shorter interval was legitimately retained by upstream's newly relaxed threshold; treating that as a required new loss would have conflated the policy choice with undo safety. The archived final fixture removes that ambiguity.

The path-probe result narrows the issue's initial source-level claim. The fork's ordinary history stores a path-probe placeholder with a zero send time; the broad loss-history insertion can admit that placeholder, but same-ACK age cleanup removes it before the later receipt in this replay. No externally observable path-probe false report was reproduced. Exclude path probes explicitly alongside MTU and ACK-only records to avoid depending on placeholder/cleanup details, and preserve the passing path-probe control. The independent path-probe store and its frame ownership stay separate.

## Per-behavior disposition

| Upstream behavior | Recommendation | Fork-specific reason and condition |
| --- | --- | --- |
| [#5895](https://github.com/quic-go/quic-go/pull/5895), ACKs covering only lost packets, `3c77707a` | Adopt behavior | Run validated application spurious-history processing even when no newly live packet remains. Preserve the fork's existing late-only BBR feedback and no-new-RTT semantics. |
| [#5900](https://github.com/quic-go/quic-go/pull/5900), reordered ACKs, `3225efce` | Adopt behavior | Remove the advancing-largest restriction for spurious-history discovery; retain ACK-range/unsent/skipped validation and report each retained loss once. Reordered ACKs do not automatically become new RTT or legacy ECN observations. |
| [#5903](https://github.com/quic-go/quic-go/pull/5903), migration history reset, `6a7d632f` | Adopt behavior | Reset the legacy application lost history at path migration alongside the existing BBR reset owner. Old-path receipts must not train new-path thresholds or restore new-path Reno state. |
| [#5906](https://github.com/quic-go/quic-go/pull/5906), probe/history admission, `0376d1b8` | Adopt behavior, clarified scope | Admit only ordinary congestion-relevant application packets to legacy spurious history. MTU and ACK-only false reports are reproduced; path probes are a preservation/control case. Keep ordinary flight retirement and frame callbacks intact. |
| [#5905](https://github.com/quic-go/quic-go/pull/5905), disable packet threshold after proof, `fca19c32` | Adapt | Application space only, after retained ordinary-loss proof; reset on migration. Initial/Handshake keep their default rule. This changes recovery classification for both selected controllers, even though their congestion responses remain separate. Genuine loss must still be detected through the timer/PTO path. |
| [#5925](https://github.com/quic-go/quic-go/pull/5925), bounded time-threshold growth, `53a52d50` | Adapt | Preserve the proposed 9/8 → 5/4 → 3/2 → 2 RTT bound and new-send gate, using the fork's existing per-space numbering. The replay verifies the first adjustment and timer rescheduling; later increments/cap are source-inspected and require maintained port tests. Make the effective delay explicit per space for both ordinary loss and BBR PTO-retired classification. |
| [#5923](https://github.com/quic-go/quic-go/pull/5923), all-spurious Reno undo, `6b98bc98` | Adapt after #961 | Restore only the latest fully proved cutback's saved Reno state using #961's typed identity and controller-owned prefix boundary. Do not import raw packet-number interval comparisons from upstream or replace BBR's episode membership. |

The first four recommendations are independent of #961's implementation. Threshold adaptation depends on reliable discovery/history hygiene. Reno undo depends on hygiene and #961's coherent prefix interface; it does not require adaptive thresholds to land first. The existence of #626's separate lookup proposal and #619's consumer experiment supplies no adoption evidence and creates no implementation dependency here. #887 governs later BBR readiness baseline decisions; it is not a prerequisite for this assessment or a correctness fix.

## Existing owners and integration obligations

`sentPacketHandler` owns ACK validation, per-space sent history, ordinary loss classification, RTT eligibility, flight subtraction and frame retirement. Its legacy `lostPacketTracker` holds at most 64 application-space packet-number/send-time records, with age cleanup at three PTOs. It is currently used for spurious-loss diagnostics, not BBR delivery authority. The hygiene slice should leave threshold and congestion policy unchanged while making this existing record discovery accurate.

Default Reno's `cubicSender` owns its window, slow-start threshold, additive-increase credit and accepted cutback boundary. #961 is now `bug` + `ready-for-agent`, with the snapshot-only design accepted but not present at the pinned fork baseline. Its complete registration prefix is captured at the latest ACK-eliciting non-path send; MTU sends participate, and earlier excluded registrations are included in that prefix. The controller alone freezes accepted boundaries; HybridSlowStart owns its separate round boundary. Reno undo must save/restore the corresponding previous prefix and state, never create a second independently updated recovery boundary. The final callback representation belongs to the #961 implementation contract.

`congestionDispatch` assigns BBR's connection-wide ordinals, constructs synchronous rich events, discovers retained late ACKs and owns path/sample reset dispatch. `recoveryEvidence` owns BBR loss-episode membership, complete-evidence undo eligibility and persistent-congestion outcomes. Its 32,768-outcome bound and the delivery sampler's 4,096 retained-record bound remain independent of the 64-entry legacy history. `BBRSender` consumes episode identity/eligibility to restore saved loss-derived model state while composing with CE and ProbeRTT caps; persistent-congestion evidence invalidates undo. These existing mechanisms already handle late receipts and lifecycle disposal and must not be replaced by Reno's new callback or legacy tracker.

Time adaptation reaches a second classification path: `confirmPTOOutcomes` currently calls the same `lossDelay()` used by ordinary loss detection. The proposed adaptation should preserve that single calculation authority, parameterized by the normalized space. Receipts and legitimate RTT updates must precede classification; any threshold increase from the current ACK must also precede PTO-retired classification and ordinary loss checks. A late-only ACK must reschedule a moved loss deadline without fabricating an RTT sample. Otherwise the same event could first assert persistent-congestion evidence under the old threshold and then relax ordinary recovery under the new one. The persistent-span duration itself remains three PTOs; packet-threshold suppression must not turn PTO alone into loss.

For Reno undo, account for every real congestion event supporting the saved cutback, including a loss whose second cut was suppressed. Process current-ACK CE and newly declared real losses before allowing old spurious reports to restore state. CE is not a packet loss and cannot be canceled by a late packet receipt; conservatively invalidate pending undo when current applicable CE evidence makes completeness unsafe. A history expiry/eviction, missing event, superseding cutback or lifecycle disposal must prevent unsupported restoration. Consume each retained receipt once. Restore the saved window, slow-start threshold, additive-increase credit and correct previous boundary only when the latest cutback is fully proved spurious; do not shrink a window that has already grown beyond the saved one. Payload/frame ownership stays with existing recovery actions.

## Bounded implementation slices and evidence

These are proposed slices, not published child issues or accepted implementation briefs.

1. **Detection and history hygiene.** Implement the four adopted behaviors together, with fresh maintained tests for only-lost and reordered ACKs, duplicate receipts, ordinary/MTU/path/ACK-only distinctions, migration and existing Retry/key-disposal fences. Preserve current thresholds, Reno behavior, the two BBR ACK branches, history bounds and ownership. No #961 blocker.
2. **Adaptive application loss policy.** Build on hygiene. Add packet-threshold suppression and bounded time growth with one space-aware delay authority. Required cases: default/no-proof and Initial/Handshake controls; all growth steps and cap; several late losses in one ACK; old-loss/new-send eligibility; exact-threshold and ACK-delay boundaries; timer rescheduling on late-only proof; real loss after adaptation; shared 0/1-RTT space; migration resets; same-ACK receipt/adaptation/PTO classification; existing BBR episode/ordinal/persistent and payload-accounting controls. The policy affects both Reno and BBR; record that deliberate behavior change and its limits.
3. **Reno undo.** Build on hygiene and the landed #961 prefix contract. Required cases: one/all/partly spurious cutbacks, late proof after recovery exit, superseding cutback, suppressed losses, validated CE before/with/after proof, duplicate reports, 64-entry overflow/expiry, window already above saved state, overlapping spaces and excluded-send frontier cases. Retry, key discard, rejected 0-RTT and migration invalidate stale proof without resurrecting disposed packets or changing BBR resets. A genuine Initial/Handshake loss without surviving proof cannot be erased by application-space receipts. Use one controller-owned boundary and fresh maintained tests, not this archive as CI input.

Each accepted slice should run its new regressions and the affected recovery/congestion suites, with race/vet and repository checks appropriate to its final production diff. Use existing BBR preservation tests for all-spurious/mixed/superseded/missing episodes, CE/ProbeRTT caps, persistent restart/deduplication and PTO receipt/lifecycle/accounting. This assessment ran the unmodified package suites; it does not claim validation of an unimplemented adaptation or of every future acceptance case.

ADR 0001's existing public/wire/ownership contracts, ADR 0003's selected validated upstream adoption, and ADR 0004's connection-owned registration remain binding. The policy and Reno undo would need their own narrow accepted update to ADR 0009/D04 and maintained feedback/design documentation: #961's acceptance of an ordering correction does not itself accept these additional behaviors. BBR's opt-in sidecar, bounded evidence and controller identity remain intact. The PTO-retired threshold contract from #617 must explicitly describe the effective per-space adaptive delay if slice 2 is accepted. Frozen historical evidence and qualification decisions remain historical.

## Finite performance comparison proposal

No performance measurements were made. Before claiming a benefit, use one common candidate source revision with an experiment-only selection between the pre-#962 policy and the accepted #962 behavior. Both arms must include the same #961 ordering correction, workload, compiler, transport configuration and emulator event schedule. Select default Reno or BBRv3 explicitly; compare each controller to itself under matched conditions. Record the intentional Reno change without substituting it for #887's readiness reference.

Proposed budget: three path profiles (clean control, bounded reordering without drop, ordinary loss without injected reordering), two policy arms, two controllers and three paired seeds: 36 measured runs, each 30 seconds, with a 60-minute wall-clock stop including setup and failures. Use one saturated reliable-stream workload with verified useful-delivery accounting. Before execution, freeze the exact link rate/RTT/queue/MTU, reorder delay/depth, drop schedule, warm-up and measurement windows, seed list, commands and revision/patch hashes. Use the same event schedules for each pair; do not expand profiles or repetitions after seeing results. Register thresholds for acceptable genuine-loss delay before running.

Report useful-delivery goodput/completion, spurious and genuine loss/PTO counts, retransmitted stream bytes, recovery delay, window/RTT traces and endpoint CPU. Inspect the ordinary-loss arm for the cost of abandoning packet-distance detection. This would be a bounded local comparison, not native qualification, GridCast rollout, coexistence certification or evidence for application DATAGRAM workloads. An inconclusive result stays inconclusive; expanding its scope needs a separate decision.

## Reproduction and artifacts

From an evidence-bearing checkout with the pinned fork production source:

```sh
python3 docs/audits/issue-962-loss-recovery/run.py
```

Exit 1 is expected on the fork: eight candidate-behavior cases fail. For an independently prepared worktree at the pinned upstream SHA:

```sh
python3 docs/audits/issue-962-loss-recovery/run.py --source upstream --repo /absolute/path/to/upstream-control
```

Exit 0 is expected on upstream. The driver refuses tracked Go/module drift and untracked Go files, and uses a temporary compiler overlay without editing the target source tree. [reproduction_test.go.txt](reproduction_test.go.txt) and [run.py](run.py) are the frozen fixture and driver; [fork.log](fork.log), [fork-repeat.log](fork-repeat.log), [upstream.log](upstream.log) and [baseline-tests.log](baseline-tests.log) retain the captured results. [receipt.json](receipt.json) records sources, result counts and checksums.
