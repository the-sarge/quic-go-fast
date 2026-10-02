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

- **ACK transitions.** An ACK newly resolves every covered, non-disposed outcome in the unresolved, lost or excluded state, converting it to ACKed exactly as the frozen `ack()` does; receipt-eligible transitions advance the ACK ordinal. Only these transitions are visited, not every retained outcome.
- **Duplicate-receipt witness.** The returned witness remains the greatest covered, receipt-eligible, non-disposed registration, including already-ACKed outcomes, so later duplicate ACKs can still confirm PTO outcomes. It is answered by a bounded indexed range-maximum or predecessor query over retained eligible registrations. No path enumerates absent packet numbers or falls back to a full-ring traversal for wide or sparse ranges. Disposal, eviction and current-path eligibility are preserved.
- **PTO confirmation** finds only PTO-retired unresolved outcomes that are eligible by space, packet number below the witness and send time at or before the cutoff. Ineligible backlog is pruned by index, not walked.
- **Persistent-span reduction** maintains lost runs incrementally as outcomes become lost, ACKed, excluded, disposed or evicted, including splits by late ACKs, merges by PTO confirmation and head truncation by eviction. Qualification is *not* cached: at each existing ACK-bearing feedback boundary the query applies the current PTO to the strict `duration > 3*PTO` test, with the frozen invalid/overflowing-PTO handling, selects the same last qualifying endpoint above `reported`, and preserves report deduplication, undo invalidation and episode effects. An unchanged span can qualify when PTO falls; the query must find it without walking all runs (for example, an augmented index over run durations).
- **Retained delivery discovery** does not iterate the whole retained map per ACK.
- **Retained expiry** selects due records by expiry time, independently of the existing ordinal-ordered capacity-eviction heap, because a newer record retired with a shorter PTO can expire first. Expired membership, capacity-eviction choice, counters, disposal and undo effects, and the exposed `nextExpiry` behavior — including the frozen stale-early-deadline behavior after the earliest record is removed — are preserved rather than "improved". Auxiliary structures stay bounded and are maintained on every removal path. Expiry entry points from send, retirement and timer are all covered.

Preserved without change: the 32,768-outcome and 4,096-retained caps, the full registration history including ACK-only and excluded packets, packet-number spaces, current-path receipt authority, late and duplicate receipts, loss classification, recovery-episode membership and undo eligibility, persistent-congestion rules and reset/disposal ownership. Derived indexes are added; no history is summarized or dropped. The frozen reducer is the semantic oracle. Packet-number monotonicity within a space is already enforced by `sentPacketHistory.checkSequentialPacketNumberUse`; the replacement index relies on it and needs no new assertion, but its tests still cover mixed spaces and probes.

**Work bound.** Each mechanism has a concrete, falsifiable bound stated before the build is accepted: permitted index or range overhead (for example, logarithmic in retained occupancy per ACK range) plus work proportional to returned, changed or expired information. A bound that walks all pending PTO outcomes or all lost runs merely redefines the denominator and does not satisfy C1.

### C2 — Contract-preserving ledger representation

Within the C1 rewrite, replace the 32,768-entry `keys` hash map with an index that exploits per-space packet-number monotonicity. The outcome entry may be compacted only where this is free of semantic change. On-demand ring growth is not selected: the bulk workload fills the ring, so it has no measured benefit here. No peak-RSS outcome is promised; the demonstration attributes any remaining excess.

The memory observable is named before any saving is claimed. `DeliveryStats.RecordBytes` omits the `keys` map and map overhead, so it cannot alone show the C2 saving, and replacement indexes also consume memory. Evidence uses matched-occupancy heap attribution or explicit structure accounting covering the frozen map and every replacement index, and reports accounted storage, live heap and peak RSS as distinct quantities. Changing `RecordBytes` is optional if a separate experimental measurement supplies this evidence; its omission is disclosed either way.

### C3 — Existing scoped fixes, as their tickets specify

- [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630): causally demonstrated allocation saving (69.0%/33.1% normalized sender allocation), negligible goodput effect alone.
- [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636): a reproduced pinned defect (51364 → 1 B/s) whose trigger — unmeasured bandwidth, CE-driven phase exit and genuine idle — is plausible on a CE-marking difficult path though absent from loopback. Leaving it would risk confounding the difficult-path result.

Those issues keep ownership of the production change. The candidate implements their stated bounded outcomes; it does not redefine them.

### C4 — Conform the four undispositioned model differences

Conform each difference to the selected draft-06 behavior, test-first against its focused oracle:

1. All-spurious repair restores the loss-driven phase as well as numeric bounds.
2. Startup, Drain and ProbeBW decisions on an ACK use the pre-update RTT minimum, following the selected draft's order. `UpdateMinRTT` followed by `CheckProbeRTT` is preserved, including the expiry result passed to the ProbeRTT check and the selected old-ProbeRTT-cap save. ProbeRTT behavior does not move. Round-evidence changes belong only to item 3.
3. General packet-round advancement does not depend on rate validity when otherwise valid current-generation delivered-at-send evidence crosses the boundary. Missing-history and invalid-clock events still supply no invented round evidence.
4. Startup loss learns long-term capacity from unquantized BDP and latest delivered volume; the accepted quantized output targets remain.

Rationale: the accepted design selects draft-06 as behavior authority, and these differences have no recorded reason. Conformance restores the contract rather than tuning it. Leaving them would make an impaired-path shortfall ambiguous between the algorithm and this port. If conformance conflicts with a recorded QUIC translation, keep the current behavior, record the conflict and return it to the map; do not change the translation silently.

## Excluded

- [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613): the scratch-reuse probe showed no consistent goodput or CPU gain. It remains independently open.
- Send-loop continuation: no repeated matched benefit once recovery service was repaired; it touches send-loop and receive-fairness ownership.
- Socket batching, queue allowances, classic-ECN policy, moving recovery work off the connection loop, a different BBR version or profile, any change to the outcome or retained caps, and summarizing or discarding evidence history. Each is either unsupported by the evidence or a contract change requiring a separate explicit decision.

## Build and preservation contract

- Build on the frozen component in an owned worktree under `/Volumes/worktrees/quic-go-fast`, preserving comparability with the reproduction and causal diagnosis. Production work later uses Planit against `main`.
- Two separately buildable layers: the **service correction and scoped fixes** (C1–C3; it includes the controller-side fix from [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636), so it is not a transport-only build) and the **full candidate** (C1–C4).
- **Provenance.** Comparisons name frozen source `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`, the diagnostic `recovery.patch` and `both.patch` from `798f499878290fc70a0df9db1c067c326ec3b3af`, matched Reno built from the same frozen source and fixture, and the exact revisions of both candidate layers. Each claimed effect maps to a named comparison pair.
- **C1/C2 semantic equivalence.** Adapt the retained `recovery-equivalence_test.go.txt` into a representation-independent frozen reference: the reference owns its own state with the frozen transition behavior and shares no storage with the candidate. Drive reference and candidate through identical operations and compare, after each relevant operation: ACK witnesses (already compared by the aid; preserved), `needsAckFeedback()`, feedback presence and contents, persistent-congestion endpoints and suppression, episode and undo fields, retained membership, expiry deadlines, idle authority, outstanding evidence, `evidenceLost` and semantic counters. Intentionally changed representation-byte accounting is excluded from equality. Histories include loss-free, lossy, PTO-heavy, eviction (including eviction of a span's first endpoint), mixed-space, probe, skipped-packet-number, cumulative and sparse ranges wider than the ring, duplicate-only ACKs before and after the PTO cutoff, removal of the greatest eligible witness, lost-to-ACKed and eligible excluded-to-ACKed transitions, late-ACK span splits, PTO-confirmation merges, unchanged spans under increasing, decreasing and invalid-to-valid PTO, equality at `3*PTO`, multiple qualifying runs, retained-cap eviction, out-of-order expiry (older record with long PTO, newer with short PTO; earliest deadline removed by ACK and by disposal; its stale deadline then firing), missing-evidence undo invalidation, disposal and reset. The reference histories must positively produce persistent-congestion reports, episode exits and undo-eligible events; a history set that never emits them does not pass. No maintained fuzz or benchmark framework.
- **C1 work bounds.** Disposable counters for ACK transitions, witness queries, PTO confirmation, persistent-span queries, retained ACK discovery and expiry count inspected records, failed eligibility checks and index-node traversals. Finite occupancy-scaling checks run on both layers: hold ACK-range count and useful transitions fixed while raising ACKed history, unresolved PTO backlog, lost-run population and retained occupancy toward their unchanged caps; include duplicate-only cumulative ACKs, sparse over-wide ranges, zero and one PTO confirmation, and one due expiry among many unexpired records through the timer, send and retirement entry points. Counts must stay within the stated bounds. The same counter definitions are applied to the diagnostic patch for the local comparison below.
- C4: one failing-then-passing focused oracle per difference, plus an unchanged-behavior control for each. For item 2 the controls include ProbeRTT expiry and entry when the same ACK refreshes RTT, with the saved-cap behavior.
- Existing recovery, BBR, ECN, bounded-emission and local-credit tests; one race run on changed seams; vet, scoped lint and module tidiness.
- Receiver integrity at the useful-delivery boundary: zero corruption and duplicates in every endpoint run.

## Expected observable effects

These are hypotheses for the demonstration to test, not results.

- Loopback STREAM and DATAGRAM, service-correction layer versus frozen BBRv3 and matched Reno: goodput within 5% of Reno and sender CPU per useful GiB within 10%, consistent with the diagnostic result; the sender ACK-processing share of the CPU profile collapses as it did under the diagnostic intervention.
- Discriminating check for C1, service-correction layer versus the diagnostic patch on an injected-loss local path: the candidate's per-mechanism work counts stay within the stated bounds, while the diagnostic patch's PTO-confirmation and span-reduction visits return toward ring occupancy after the first loss. The demonstration manifest records the local impairment parameters and evidence that loss, PTO, CE or genuine idle actually engaged; a mechanism that did not engage is reported as unengaged and assigned no performance effect.
- Ledger storage falls after C2 at matched occupancy under the named memory observable; peak RSS is measured and any remaining excess attributed rather than assumed.
- The full candidate's delta from the service-correction layer is unresolved: C4 items 2 and 3 can engage without loss. The full candidate remains subject to every accepted readiness criterion; a shortfall attributable to C4 fails the applicable local criterion and triggers diagnosis. It neither rejects BBRv3 nor authorizes tuning away conformance.

## Remaining uncertainty

A useful benefit on a difficult path is not established. Peak RSS, sparse control-latency tails and DATAGRAM sender CPU margin may still fail after C1–C3. Native two-host behavior, other platforms and real carrier paths remain unverified. A C4 conformance may expose an undocumented reason for a difference. These are inputs to the demonstration and the later qualification decision, not reasons to reject BBRv3.

## Map consequences

Building C1–C4 with this evidence is distinct work from measuring it. Two build tasks block [Demonstrate the BBRv3 correction in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/671): one for C1–C3 with equivalence evidence, one for C4. No production merge, paid resource, campaign resumption, default-controller change or campaign-ledger change is authorized here.

## Independent consideration

RAS consideration run `20261002T150911-f0e6beafbc763f08cf3867f4` (design kind, consider profile; one partial adjudication delivery recovered) reviewed the first revision of this record with the ledger, dispatch and sampler source. It found the C1–C4 scope supportable and no evidence that indexed service requires an evidence-contract change. Its eight findings were preservation and verification clarifications, all adopted above: current-PTO span qualification; a representation-independent reference with positive semantic coverage; falsifiable per-mechanism work bounds; ACK transitions separated from duplicate-witness queries; expiry selection separated from ordinal eviction; C4 item 2 narrowed to the draft's sequencing; a named memory observable; and explicit provenance and attribution. The run's not-to-act-on dispositions were also adopted: no new packet-number assertion (the C1 section names the existing guard), no readiness exemption for C4, and no scope expansion from theoretical capacities.

`ras verify` of that run against revision `46455c0e5be6decb243bd0a034120482d5f34a12` (document fingerprint `sha256:1fe5bb6d7717854045e800b79e3df275fe828adfefb560f91770bc073f3cfdf3`) completed with a clear blocking projection: 24 of 24 prior clusters covered, 22 resolved and two no longer applicable (future ticket identifiers belong to map dispatch; the packet-number ordering guard already exists).
