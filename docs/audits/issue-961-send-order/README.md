# Issue 961: recovery ordering reproduction and design comparison

Assessment dated October 21, 2026, for [#961](https://github.com/the-sarge/quic-go-fast/issues/961). This records a confirmed recovery-handler defect and a proposed adaptation. It does not approve or implement a production fix, change an accepted ADR, qualify BBR, or establish deployment impact.

## Sources and reproduction boundary

The fork is pinned to `71581cfc3227ca579643fea1f8afa38e3ce5536d`. The upstream control is the actual [#5920](https://github.com/quic-go/quic-go/pull/5920) merge, `d05f943bff0e8421f06b17e2fba1ec13dba80a2f`. Upstream code was inspected directly from that commit, including generator, history, packet packer, ACK validation, loss-distance calculation, Retry and key-discard changes. The upstream commit already contains other earlier upstream work; passing this control is evidence for the published upstream implementation, not evidence that cherry-picking this commit alone onto the fork is safe.

Pinned fork evidence: [space-local generation and recovery dispatch](https://github.com/the-sarge/quic-go-fast/blob/71581cfc3227ca579643fea1f8afa38e3ce5536d/internal/ackhandler/sent_packet_handler.go), [Reno and slow-start comparison inputs](https://github.com/the-sarge/quic-go-fast/blob/71581cfc3227ca579643fea1f8afa38e3ce5536d/internal/congestion/cubic_sender.go), [rich-path ordinals](https://github.com/the-sarge/quic-go-fast/blob/71581cfc3227ca579643fea1f8afa38e3ce5536d/internal/ackhandler/congestion_dispatch.go), and [bounded per-space boundary witnesses](https://github.com/the-sarge/quic-go-fast/blob/71581cfc3227ca579643fea1f8afa38e3ce5536d/internal/ackhandler/bbr_recovery.go).

Environment: Go 1.27.3, darwin/arm64. The fixture uses the real default Reno sender behind `NewSentPacketHandler`, allocates numbers with `PopPacketNumber`, registers real PING frames with `SentPacket`, and delivers valid decoded ACK ranges through `ReceivedAck`. It does not mock congestion decisions or modify controller fields. Fixed monotonic times and the sequential Initial/Handshake generators remove clock and skip-schedule randomness. The server perspective permits outstanding Initial and Handshake packets together; client Initial-key retirement at first Handshake registration is not bypassed to claim a client handshake reproduction.

These are deterministic unit-level recovery inputs, not a TLS/UDP deployment reproduction. The fixture does not exercise the wire parser, packet packer, key-discard callbacks, ECN, or BBR. Those remain implementation validation requirements. The fork's 32-packet initial window and existing Reno backoff factor of 0.7 are preserved; this assessment does not change congestion policy to the RFC's illustrative parameters.

## Confirmed behavior

Each scenario has a control that substitutes the Initial space for Handshake while preserving the logical sequence of sends and ACKs. Cross-space packet numbers overlap in the fork; the upstream control allocates globally increasing values while keeping ACK/loss processing space-qualified.

| Scenario | Expected with the fork's existing Reno policy | Fork with overlapping spaces | Single-space fork control | Upstream cross-space control |
| --- | --- | --- | --- | --- |
| Send Initial 0–6, then Handshake 0; ACK Initial 6 | Four threshold losses belong to one flight: window 38,400 → 26,880; remain in recovery | Window falls to 9,219 through four cuts; recovery ends early | Pass | Pass |
| Interleave three Initial sends with one Handshake send, three times; ACK the last Initial | One window cut for losses predating the same boundary; remain in recovery | Window 38,400 → 9,219; recovery ends early | Pass | Pass |
| Lose Initial 0 via ACK 1–3; send a later Handshake flight and lose Handshake 0 via ACK 1–3 | The later loss reduces window 26,880 → 18,816 | Window stays 26,880: later loss suppressed | Pass | Pass |
| After the Initial cutback, send and ACK Handshake 0 | Acknowledging a post-cutback transmission ends recovery | Recovery remains active | Pass | Pass |

All four cross-space subtests fail on the fork, all four single-space controls pass, and all eight subtests pass on upstream. See [fork output](reproduction.log) and [upstream output](upstream-control.log). The fork's ordinary affected-package suites also pass: `go test ./internal/ackhandler ./internal/congestion`, recorded in [baseline-packages.log](baseline-packages.log). Existing tests therefore do not expose these failures.

The mechanism is direct: the handler feeds raw space-local numbers into one controller; `cubicSender.OnPacketSent` overwrites its latest sent number with the last registration's number, `OnCongestionEvent` freezes that scalar as the cutback boundary, and `OnPacketAcked` takes a maximum across incompatible spaces. The first scenario freezes boundary zero, allowing subsequent losses numbered one, two and three to cut again; ACK six then appears to cross that boundary even though it predates the cutback. The later scenarios fail in the opposite direction because small Handshake numbers cannot cross the earlier Initial boundary. The desired ordering rule agrees with [RFC 9002 §7.3.2](https://www.rfc-editor.org/rfc/rfc9002.html#section-7.3.2): recovery spans spaces and is delimited by transmission timing, not unrelated numeric magnitudes.

## Designs compared

| Design | Advantages | Consequences and limitations | Assessment |
| --- | --- | --- | --- |
| Adopt upstream's shared wire-number generator and shared history | Published implementation passes the reproduction; one number can order transmissions without a new per-packet order field; aligns with later upstream work | Changes generator, history, ACK/loss traversal, space filtering, skip-distance accounting, PTO selection, key discard, Retry and coalesced-packet reservations. The upstream patch spans nine files, with 372 additions and 239 deletions before fork-specific adaptation. BBR must be integrated rather than overwritten. | Viable, broader fallback; not a safe blind cherry-pick |
| Adapt connection-wide registration ordinals at the recovery/controller seam | Preserves current wire numbering, generators, histories, packer, skipped-number validation and loss-distance rules; extends an ordering concept already used by BBR | Requires mandatory per-packet order metadata, a common counter, typed legacy controller inputs, HybridSlowStart conversion, and explicit ECN boundary handling. The current optional sampler cannot be the authority for default Reno. | Preferred for implementation planning, subject to the contract below |
| Carry space-qualified numbers and snapshot three-space boundaries throughout Reno and HybridSlowStart | Constant-sized boundary state; compares even a missing ECN packet against a cutback without retaining a packet-to-ordinal map. BBR already uses this witness technique. | Makes Reno's recovery and slow-start rounds explicitly aware of three packet spaces; needs an atomic conversion of all comparisons, and is less aligned with the existing ordinal controller model | Useful witness mechanism within the ordinal adaptation; a complete alternative if per-packet metadata cost proves unacceptable |

Comparing send timestamps alone is insufficient: registrations can share a timestamp, including all sends in the first scenario. Updating a global maximum of raw packet numbers also fails because a later packet in another space can have a lower number. Installing BBR's rich dispatcher for every Reno connection would unnecessarily bring its delivery sampling and bounded recovery stores into default operation.

Only the upstream design was executed. No ordinal or boundary-snapshot production candidate was built or benchmarked. The preferred design is a source-backed recommendation with concrete validation gates, not a validated port or a claim of performance improvement.

## Proposed adaptation contract

Use one recovery-owned monotonically increasing registration ordinal for every registered QUIC packet, including ACK-only packets, MTU probes and path probes, in the same order currently counted by `captureCongestionSend`. Skipped wire numbers and packet-number reservations are not registrations. Preserve ordinal values across Retry, key discard, 0-RTT rejection and path migration, while preserving their existing disposal and generation fences. Registration remains before asynchronous socket I/O under ADR 0004; a socket completion is not a new transmission identity.

Move the existing rich-path counter's authority to this common registration boundary rather than installing two independent clocks. Retain the ordinal on mandatory packet metadata for default Reno's live ACK/loss processing; initialize it on every pooled-object acquisition. Feed BBR's existing `PacketInfo.Ordinal` from the same source, preserving its present sequence exactly. An eight-byte ordinal is proposed per packet plus constant connection state; actual struct layout, allocator size classes and retained-byte consequences need measurement before acceptance of an implementation. No extra retained history, map, payload copy, or feedback-lifetime extension is justified by this design.

Make private controller order values distinguishable from wire packet numbers. Convert Reno's sent, acknowledged and cutback comparisons and HybridSlowStart's packet-timed round boundaries together. An eligible ACK from any space can cross the connection-wide recovery boundary; late ACKs from the old flight cannot cross it. Preserve eligibility for congestion loss, ACK-only work, MTU probes and alternate-path probes rather than treating every registered packet as congestion-controlled flight. The packet-space key remains authoritative for ACK validation, ECN marking, loss detection, qlog and retransmission ownership.

ECN is part of the atomic conversion. The current legacy path calls `OnCongestionEvent` with the ACK frame's largest application-space packet number, which need not have a surviving live packet record. Do not invent an ordinal from the current counter, use a raw number in an ordinal comparison, or silently depend on optional retained BBR evidence. Freeze a three-space sent-number witness alongside the ordinal recovery boundary and use the application-space witness to determine which side of that boundary a validated CE anchor occupies when its exact ordinal is unavailable. This proves relative ordering from monotonic per-space numbers without reconstructing the missing packet. BBR's existing `recoveryEvidence.latestPackets` / `boundaryPackets` mechanism is precedent; share the small concept without enabling its ring, sampler or undo model for Reno. A private typed event or separate CE callback can express this distinction; the behavioral contract, bounded state and one boundary owner are mandatory.

The exact private event shape, boundary eligibility, pool-layout cost and ECN tests must be settled in the implementation plan before calling the adaptation ready. If these require another retained ledger, guessed ordering, or a larger recovery redesign, return to the upstream option rather than growing an ad hoc mapping subsystem.

## Lifecycle and compatibility comparison

| Boundary | Ordinal adaptation | Shared generator/history adoption |
| --- | --- | --- |
| Initial and Handshake | Keep independent wire sequences and histories; registration ordinal supplies cross-space order | Share number allocation/history while preserving per-space largest ACK, loss timer and filtering; handshake-number membership supports distance calculations |
| 0-RTT and 1-RTT | Preserve their shared application-space key, actual encryption-level disposal and current skip schedule | Allocate from the shared global sequence, preserve common application-space ACK identity and ensure interspersed handshake numbers do not count as application packet distance |
| Retry | Dispose old records and reset existing evidence/sample state; neither renumber nor reuse ordinals | Clear shared history and handshake memberships while retaining the next global number; adapt the fork's Retry fences and BBR state explicitly |
| Key discard / 0-RTT rejection | Keep existing space/level disposal; no false ACK or loss and no ordinal reset | Filter shared history by space and actual encryption level; preserve the 0-RTT/1-RTT distinction and outstanding accounting |
| Skipped-number validation | Leave current per-space validation and distance calculation unchanged | Preserve genuine intentional-skip validation while distinguishing numbers allocated to another space; upstream adds handshake membership accounting |
| Coalescing / packet-number reservation | No packer or reservation change; derive order from registration, not peek/pop | Reserve each long-header number before preparing the next coalesced packet, including close and probe paths; otherwise global peeks can collide |
| Migration | Keep the connection ordinal increasing; reset Reno and BBR model/path authority as today and preserve old-path disposal | Keep wire number continuity and filter/reset shared recovery state while preserving the fork's BBR path and ECN fences |
| API and wire compatibility | No public API or wire-number policy change | No public API change required; wire numbering changes while still using legal space-qualified QUIC packets; interoperability and 0-RTT assumptions need validation |

Both designs must preserve BBR's logical ordering, delivered counters, recovery/undo membership, persistent-span deduplication, 32,768-outcome bound, retained-delivery limits, ECN counter fence, pending-byte accounting, payload/frame ownership and connection-goroutine state ownership. ADRs 0001, 0003, 0004 and 0009 remain binding. Fixing the default controller's ordering does not select BBR by default or reopen its model. No contradictory ADR was found; changing a prior preservation expectation requires disclosure in later qualification.

## Proposed implementation slices and sequencing

These are planning candidates, not dispatched architecture-handoff slices or implementation authorization.

1. **Establish common registration identity.** Add mandatory packet order metadata and one common counter, preserve all BBR ordinal values and lifecycle fences, and prove pooled reuse, ACK-only/probe registration, same-time registration and Retry/migration continuity. Legacy Reno behavior can remain unchanged in this preparatory slice. Confirm actual metadata cost and settle the private event/boundary contract. Do not enable the rich sampler for Reno.
2. **Correct Reno and slow-start comparisons atomically.** After slice 1, convert send/ACK/loss/CE and HybridSlowStart to the accepted order/boundary representation in one green change. Turn all four paired reproductions into maintained regressions. Add overlapping Initial/Handshake/application cases, 0-RTT→1-RTT continuity, old-flight ACKs, subsequent-flight losses, CE before/after the boundary, CE with an absent largest-ACK record, mixed CE/loss, and repeated/reordered feedback. No mixed raw-number/ordinal comparison may ship between slices.
3. **Complete compatibility evidence.** After slice 2, run focused Retry, key discard, 0-RTT rejection, pool reuse, migration, skipped-number rejection, coalesced emission and existing BBR rich-feedback/recovery/ECN tests, including optional-evidence eviction. Run affected-package race/vet checks and relevant existing integration coverage. Record current default-Reno behavior and implementation metadata cost. This does not authorize a broad performance or native BBR campaign.

For [#962](https://github.com/the-sarge/quic-go-fast/issues/962), independently accepted lost-history hygiene can proceed separately; Reno undo and any shared recovery-boundary representation should build on or coordinate with the corrected ordering contract rather than introduce a second boundary owner. Adaptive loss thresholds are a separate decision. [#626](https://github.com/the-sarge/quic-go-fast/issues/626) changes bounded BBR ACK lookup cost and is not an assessment prerequisite. [#887](https://github.com/the-sarge/quic-go-fast/issues/887) governs the comparison policy before subsequent BBR readiness registration; it is not a prerequisite for this reproduction or a correctness implementation, and a Reno fix must not silently rebaseline its frozen reference.

## Replay and triage outcome

Run from the fork checkout containing this directory:

```sh
python3 docs/audits/issue-961-send-order/reproduce.py
python3 docs/audits/issue-961-send-order/reproduce.py --root /path/to/upstream-at-d05f943b --upstream
go test ./internal/ackhandler ./internal/congestion
```

The replay driver prints the checkout revision, adds the fixture through a temporary Go build overlay, and returns `go test`'s exit code. Exit 1 is the expected fork reproduction result; exit 0 is the expected upstream result. For upstream it changes only `PopPacketNumber`'s API argument from encryption level to packet-number space. No Go source is written into either checkout, and ordinary CI does not activate the intentionally failing archived fixture. The two source roots must be pinned to the revisions above to reproduce this comparison; a later implementation must revalidate against its own HEAD. [SHA256SUMS](SHA256SUMS) identifies the captured fixture, runner and logs.

Recommendation: **adapt**, using common registration order and bounded per-space boundary witnesses, after accepting the proposed contract and completing its design gate. Keep **bug + needs-triage** while that choice is pending; no reporter information is missing. Redundancy checks covered the fork's generators, histories, Reno/HybridSlowStart callbacks, BBR dispatcher/recovery witnesses and their tests; no equivalent default-Reno fix exists. There is no `.out-of-scope/` rejection record. The original issue has no prior triage comments to reconcile. No implementation-ready brief, issue closure, or accepted design decision is implied by this assessment.
