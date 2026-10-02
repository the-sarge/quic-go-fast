# BBRv3 correction choice

**Date:** October 2, 2026, America/New_York. **Scope:** [Choose the correction needed for a competent BBRv3 implementation](https://github.com/the-sarge/quic-go-fast/issues/670), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666).

This record selects the correction that the local demonstration will build and measure. It is a scope decision for an experimental candidate. It does not merge production code, judge algorithm suitability, authorize paid work or change the accepted evidence contract. The operator delegated engineering choices to the investigating agent and confirmed the resulting scope, including the model-behavior conformance in [C4](#c4--conform-the-four-undispositioned-model-differences).

## Inputs

- [Causal diagnosis](https://github.com/the-sarge/quic-go-fast/blob/798f499878290fc70a0df9db1c067c326ec3b3af/docs/audits/2026-09-30-bbr-causal-diagnosis/README.md): avoidable recovery-history service is the dominant cause of the reproduced loopback goodput and sender-CPU regression. A diagnostic intervention removed 95.8%/98.7% (STREAM/DATAGRAM) of the median goodput deficit to Reno; with the packet-size-refresh allocation removed, 97.9%/99.4%. Peak RSS, sparse control-latency tails and DATAGRAM sender CPU remain at or beyond the readiness limits.
- [Model-fidelity research](https://github.com/the-sarge/quic-go-fast/blob/ffaa4410c7e2b9982f31c9820ebdad0d4d42dece/docs/audits/2026-09-30-bbr-model-fidelity/README.md): four controller differences from the selected draft-06 authority lack a recorded disposition; each has a focused oracle. Their performance contribution is unknown; two need loss to engage.
- Frozen component `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`. At `main` `55ea687d68c9c6f33785a1875374ec6a0d7570e8`, `internal/ackhandler`, `internal/congestion` and `packet_emission_bbr.go` are byte-identical to the frozen component.

## Why the diagnostic intervention is not the correction

The diagnostic `recovery.patch` skips PTO confirmation until any outcome has *ever* been PTO-retired, and skips persistent-span reduction until any outcome has *ever* been lost. Both flags remain set until path/Retry reset. After a connection's first loss or PTO, two of the three full-ring scans return on every ACK. Its ACK-discovery enumeration also falls back to the full ring scan when ACK ranges cover more packet numbers than the ring holds. The patch therefore proves the mechanism on loss-free loopback, but it cannot be expected to remain affordable on the impaired paths where the demonstration must look for a useful benefit.

A second instance of the same cost class is invisible in the loss-free diagnosis. `appendRetainedAck` (`internal/ackhandler/congestion_dispatch.go`) iterates the whole sampler `retained` map on every ACK, and `expire` (`internal/ackhandler/delivery_sampler.go`) iterates it whenever the earliest expiry passes. That map holds lost and PTO-retired delivery evidence for three PTOs, up to 4096 records. It is empty in the loss-free collectors and fills under loss.

Reno connections do not construct this machinery: `congestionEvents` is nil unless a rich controller is selected. The correction cannot change default Reno behavior.

## Selected correction

### C1 — Affordable recovery and retained-evidence service

Make per-ACK service work proportional to newly resolved information rather than to retained history, without changing the evidence contract.

- **ACK discovery** visits only outcomes that the ACK newly resolves, together with the existing duplicate-receipt witness rules, instead of scanning all retained outcomes per ACK. Lookup must handle skipped packet numbers, mixed packet-number spaces, disposal, eviction and arbitrarily wide ACK ranges without enumerating absent ranges.
- **PTO confirmation** visits only PTO-retired unresolved outcomes, not the whole ring.
- **Persistent-span reduction** is maintained incrementally as outcomes become lost, acknowledged, excluded, disposed or evicted, preserving the existing gap, ACK-only break, measured-endpoint, cross-space ordering and report-deduplication rules.
- **Retained delivery discovery and expiry** do not iterate the whole retained map per ACK or per expiry check.

Preserved without change: the 32,768-outcome and 4,096-retained caps, the full registration history including ACK-only and excluded packets, packet-number spaces, current-path receipt authority, late and duplicate receipts, loss classification, recovery-episode membership and undo eligibility, persistent-congestion rules and reset/disposal ownership. The frozen reducer is the semantic oracle.

### C2 — Contract-preserving ledger representation

Within the C1 rewrite, replace the 32,768-entry `keys` hash map with an index that exploits per-space packet-number monotonicity. The outcome entry may be compacted only where this is free of semantic change. On-demand ring growth is not selected: the bulk workload fills the ring, so it has no measured benefit here. No peak-RSS outcome is promised; the demonstration attributes any remaining excess.

### C3 — Existing scoped fixes, as their tickets specify

- [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630): causally demonstrated allocation saving (69.0%/33.1% normalized sender allocation), negligible goodput effect alone.
- [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636): a reproduced pinned defect (51364 → 1 B/s) whose trigger — unmeasured bandwidth, CE-driven phase exit and genuine idle — is plausible on a CE-marking difficult path though absent from loopback. Leaving it would risk confounding the difficult-path result.

Those issues keep ownership of the production change. The candidate implements their stated bounded outcomes; it does not redefine them.

### C4 — Conform the four undispositioned model differences

Conform each difference to the selected draft-06 behavior, test-first against its focused oracle:

1. All-spurious repair restores the loss-driven phase as well as numeric bounds.
2. The same ACK's phase decisions use the pre-update RTT minimum, retaining the selected old-ProbeRTT-cap save.
3. General packet-round advancement does not depend on rate validity when otherwise valid current-generation delivered-at-send evidence crosses the boundary. Missing-history and invalid-clock events still supply no invented round evidence.
4. Startup loss learns long-term capacity from unquantized BDP and latest delivered volume; the accepted quantized output targets remain.

Rationale: the accepted design selects draft-06 as behavior authority, and these differences have no recorded reason. Conformance restores the contract rather than tuning it. Leaving them would make an impaired-path shortfall ambiguous between the algorithm and this port. If conformance conflicts with a recorded QUIC translation, keep the current behavior, record the conflict and return it to the map; do not change the translation silently.

## Excluded

- [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613): the scratch-reuse probe showed no consistent goodput or CPU gain. It remains independently open.
- Send-loop continuation: no repeated matched benefit once recovery service was repaired; it touches send-loop and receive-fairness ownership.
- Socket batching, queue allowances, classic-ECN policy, moving recovery work off the connection loop, a different BBR version or profile, any change to the outcome or retained caps, and summarizing or discarding evidence history. Each is either unsupported by the evidence or a contract change requiring a separate explicit decision.

## Build and preservation contract

- Build on the frozen component in an owned worktree under `/Volumes/worktrees/quic-go-fast`, preserving comparability with the reproduction and causal diagnosis. Production work later uses Planit against `main`.
- Keep transport-service work (C1–C3) and model conformance (C4) as separately buildable layers so the demonstration can measure a transport-only condition and a full candidate.
- C1/C2 equivalence: a finite differential test against the frozen reducer — extending the retained `recovery-equivalence_test.go.txt` — over loss-free, lossy, PTO-heavy, eviction, mixed-space, skipped-packet-number, wide-range, duplicate/late-receipt, disposal and reset histories. The test compares emitted feedback, persistent-congestion and episode evidence plus sampler retained/expired outputs. No maintained fuzz or benchmark framework.
- C4: one failing-then-passing focused oracle per difference, plus an unchanged-behavior control for each.
- Existing recovery, BBR, ECN, bounded-emission and local-credit tests; one race run on changed seams; vet, scoped lint and module tidiness.
- Receiver integrity at the useful-delivery boundary: zero corruption and duplicates in every endpoint run.

## Expected observable effects

- Loopback STREAM and DATAGRAM, transport-only and full candidate: goodput within 5% of matched Reno and sender CPU per useful GiB within 10%, consistent with the diagnostic result; sender ACK-processing share of CPU profile collapses as it did under the diagnostic intervention.
- Discriminating check for C1 against the diagnostic patch: on an injected-loss local path, per-ACK ledger and retained-evidence visits stay proportional to newly resolved packets for the candidate, while the diagnostic patch's visits return toward the ring size after the first loss.
- Retained ledger bytes fall after C2; peak RSS is measured and any remaining excess attributed rather than assumed.
- C4 may change loopback behavior where differences 2 and 3 engage without loss; the layered build makes that visible.

## Remaining uncertainty

A useful benefit on a difficult path is not established. Peak RSS, sparse control-latency tails and DATAGRAM sender CPU margin may still fail after C1–C3. Native two-host behavior, other platforms and real carrier paths remain unverified. A C4 conformance may expose an undocumented reason for a difference. These are inputs to the demonstration and the later qualification decision, not reasons to reject BBRv3.

## Map consequences

Building C1–C4 exceeds the demonstration ticket's session budget. Two build tasks block [Demonstrate the BBRv3 correction in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/671): one for C1–C3 with equivalence evidence, one for C4. No production merge, paid resource, campaign resumption, default-controller change or campaign-ledger change is authorized here.
