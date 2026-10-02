# BBRv3 recovery-service correction: build and equivalence evidence

**Date:** October 2, 2026, America/New_York. **Scope:** [Build the BBRv3 recovery-service correction with equivalence evidence](https://github.com/the-sarge/quic-go-fast/issues/706), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666), implementing the C1–C3 layer of the [BBRv3 correction choice](https://github.com/the-sarge/quic-go-fast/blob/724b033612fb6d87b898b689abd034147f611218/docs/audits/2026-10-02-bbr-correction-choice/README.md).

This record builds an experimental candidate layer and its equivalence and work-bound evidence. It does not measure endpoint performance, merge production code, close [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630) or [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636), or change any cap or evidence contract.

## Answer

Yes. The service-correction layer was built on frozen component `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` without changing the evidence contract:

- A representation-independent frozen reference matches the candidate on every compared observable across 16 scripted cases and four seeded histories (220,000 registrations). The histories positively produce persistent-congestion reports, episode exits, undo-eligible events, ring and retained-capacity evictions, retained expiry and stale deadlines.
- Every mechanism has a stated per-call work bound. All 72 occupancy-scaling cases stay within it, and the counts are flat or logarithmic as ACKed history, PTO backlog, lost-run population and retained occupancy rise to their caps. Under the same counter definitions, the diagnostic `recovery.patch` inspects records in proportion to occupancy in every mechanism except fresh and late ACKs.
- The named memory observable falls: at full occupancy the ledger's live heap drops from 3.32 MB to 1.20 MB (−63.7%), while retained delivery evidence rises from 0.97 MB to 1.20 MB (+23.6%) for its new indexes. The net is about −1.9 MB per full connection. Peak RSS is an endpoint quantity and remains for the demonstration.
- C3 removes the three allocations per packet-size refresh and restores the unmeasured-bandwidth fallback on genuine idle (51,364 B/s instead of 1 B/s in the extended regression).
- Affected-package, race, vet, scoped-lint and module-tidiness gates pass.

No bound or equivalence needed an evidence-contract change.

## Provenance

| Name | Revision |
| --- | --- |
| Frozen component | `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` |
| Diagnostic recovery intervention | [`recovery.patch`](https://github.com/the-sarge/quic-go-fast/blob/798f499878290fc70a0df9db1c067c326ec3b3af/docs/audits/2026-09-30-bbr-causal-diagnosis/recovery.patch) and [`both.patch`](https://github.com/the-sarge/quic-go-fast/blob/798f499878290fc70a0df9db1c067c326ec3b3af/docs/audits/2026-09-30-bbr-causal-diagnosis/both.patch) at `798f499878290fc70a0df9db1c067c326ec3b3af` |
| Diagnostic intervention with counters | [`diagnostic-workcount.patch`](diagnostic-workcount.patch): `recovery.patch` plus the same counting hooks, applied to the frozen component |
| C1–C3 service-correction layer | `c6bb13b72e2d5de55af36543aa27c8cf06812a11` on `codex/bbr-recovery-service` |

Matched Reno for later comparisons builds from the same frozen source and fixture; it never constructs this machinery (`congestionEvents` is nil unless a rich controller is selected).

## C1 — Affordable recovery and retained-evidence service

The outcome ring, its 32,768-entry cap and every registration are unchanged. Derived indexes over physical ring slots let each service path visit only the information it resolves or returns:

- **ACK transitions.** For each ACK range, two binary searches over the space's slot list find the covered registrations. A per-space bitset of unresolved, lost or excluded outcomes then yields exactly the outcomes that become ACKed. ACKed and disposed history is never visited.
- **Duplicate-receipt witness.** A per-space bitset of receipt-eligible, non-disposed outcomes, already-ACKed ones included, answers a predecessor query within each covered range. Absent packet numbers and over-wide ranges are never enumerated.
- **PTO confirmation.** A per-space bitset holds PTO-retired unresolved outcomes. Two existing invariants make the eligible ones a prefix of it in ring order: packet numbers rise within a space (`sentPacketHistory.checkSequentialPacketNumberUse`), and dispatch marks a registration valid only when its send time is not before any earlier registration (`captureCongestionSend`). Only valid, non-probe registrations start unresolved. Confirmation therefore stops at the first ineligible outcome; it does not walk the backlog.
- **Persistent-span reduction.** Bitsets track lost outcomes, lost endpoints and run starts, and every state change that enters or leaves Lost updates them. That covers late-ACK splits, PTO-confirmation merges, disposal, and head truncation by eviction. Each run's first-to-last endpoint duration is kept as a maximum per 16-slot block, under a max tree. Endpoint ordinals and send times both rise in ring order, so a run's last endpoint qualifies whenever any of its endpoints does. The frozen result is then the rightmost run whose duration exceeds `3*PTO`, provided its last endpoint is above `reported`. The query applies the current PTO each time and caches no qualification; the frozen invalid and overflowing PTO handling is unchanged.
- **Retained discovery.** An AVL tree over retained records keyed by (space, packet number) returns the records in each ACK range.
- **Retained expiry.** An expiry-ordered heap, separate from the ordinal capacity heap, selects due records. The exposed deadline keeps the frozen behavior, including a stale early deadline after the earliest record leaves by ACK or disposal. All three entry points use it: timer, send and retirement.

Disposal of a packet-number space and path or Retry reset keep their frozen once-per-lifecycle cost: a walk of that space's slot list and of retained records. They are lifecycle events, not ACK service.

## C2 — Ledger representation

The 32,768-entry `keys` hash map is gone. Lookups binary-search the per-space slot lists of `uint16` ring indexes, which packet-number monotonicity keeps sorted. Each outcome entry is 32 bytes instead of 48, because the key's padded struct is replaced by its fields, a free change. The ring is still allocated in full at first registration.

The named memory observable is the forced-GC live heap retained by each structure at matched, full occupancy, measured for the frozen reference and the candidate in one process ([memory.json](memory.json); three runs agree within 6 KB):

| Structure | Occupancy | Frozen live heap | Candidate live heap | Change |
| --- | --- | --- | --- | --- |
| Recovery ledger, after three ring wraps | 32,768 outcomes | 3,320,720 B (ring 1,572,864; key map 1,747,856) | 1,204,544 B (ring 1,048,576; bitsets 50,880; slot lists 65,536; run tree 32,768) | −63.7% |
| Retained delivery evidence | 4,096 records | 972,848 B | 1,202,288 B (index links and expiry heap) | +23.6% |

Accounted storage, live heap and peak RSS are distinct quantities. `DeliveryStats.RecordBytes` now accounts the candidate's ring and every index (1,197,904 B for the ledger). The frozen `RecordBytes` counted only the ring and omitted the key map, so the two are not like-for-like. No peak-RSS outcome is claimed; the demonstration measures it and attributes any remaining excess.

## C3 — Scoped fixes, as their tickets specify

- [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630): `NewBBRSender` and `SetMaxDatagramSize` share one validated initial-window function. Invalid-size rejection, the formula, window bounds and unchanged-size behavior are preserved. A one-off `testing.AllocsPerRun` probe, not committed, measured 3 allocations per refresh on the frozen sender and 0 on the candidate. Escape analysis shows the refresh inlines the arithmetic and no longer calls the constructor.
- [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636): genuine-idle pacing uses `modelBandwidth`, which applies the same unmeasured fallback as ordinary model output, under the unchanged CE cap. The existing unmeasured-bandwidth CE regression was extended first. It failed on the frozen code with 1 B/s and passes with 51,364 B/s. Measured-bandwidth idle behavior is unchanged, because `modelBandwidth` equals `min(bandwidth, bandwidthShort)` whenever bandwidth is measured.

Both issues keep ownership of the production change.

## Semantic equivalence

`recovery_reference_test.go` holds the frozen reducer under renamed types: `recoveryEvidence`, `deliverySampler`, its retained heap, `removeRetained`, `retire` and `expire`, plus frozen copies of the dispatch paths that call them. It owns its own state. `recoveryTwin` drives the reference and the candidate's real dispatch hooks through identical operations in the handler's order. After every operation it compares:

- send events and ACK witnesses
- `needsAckFeedback()` and feedback presence
- full feedback events: delivery sample, persistent-congestion endpoints, episode and undo fields
- dispatch, ledger and sampler state: episode, members, report suppression, retained count, `nextExpiry`, outstanding bytes, `evidenceLost`, idle inputs and all counters

At checkpoints it also compares the full ledger contents in ring order, every key lookup, retained membership with expiries and live delivery records. It then recomputes every candidate index from the ring: slot lists, bitsets and summaries, run starts, block maxima and the max tree, plus the AVL balance and order and both heaps. Representation-byte accounting is excluded from equality.

Scripted cases cover:

- equality at exactly `3*PTO`
- an unchanged span that qualifies when PTO falls
- invalid-to-valid PTO
- report deduplication and extension
- a PTO increase
- multiple qualifying runs
- a later run at exactly `3*PTO` in the same index block as an earlier qualifying run
- a run in the first block behind later history
- late-ACK splits
- PTO-confirmation merges
- duplicate-only ACKs before and after the cutoff
- removal of the greatest eligible witness
- lost-to-ACKed and eligible excluded-to-ACKed receipts that exit recovery
- out-of-order expiry: an older long-PTO record and a newer short-PTO one, with the earliest removed by ACK and by disposal, and the stale deadline firing through the send and retirement entry points
- undo invalidation by expired member evidence and by loss of an evicted outcome
- eviction of a run's first endpoint
- retained-capacity eviction
- cumulative and sparse ranges wider than the ring
- multi-space ring wrap: Initial and Handshake entries evicted while application entries live, and a lost run that crosses spaces and the physical wrap, then loses its first endpoint so its start moves to another index block
- a zero send time
- space disposal, including 0-RTT, and reset

Four seeded, finite histories cover the rest: loss-free with ring eviction, lossy with bursts, spurious losses and PTO shifts, PTO-heavy with duplicate ACKs, and mixed spaces with probes, skipped packet numbers, invalid registrations, retained pressure and resets. Together they produced:

- 37,784 compared feedback events and 22,774 suppressed empty feedbacks
- 77 persistent-congestion reports
- 1,041 episode exits and 167 undo-eligible events
- at least 74,464 ring evictions (resets clear the counter, so this is the per-history maximum)
- 715 retained-capacity evictions
- 29,930 retained expiries
- 43 stale deadlines

The test fails if any of these counts is zero.

A mutation sweep ([mutations.txt](mutations.txt), [script](mutations.sh)) checks that the suite can fail. All 20 mutations fail the suite. 17 are caught by the differential equivalence or slot-set search checks, including an inclusive `3*PTO` comparison that only a same-block scenario exposes, a skipped left partial block, and a clear search that escapes the summary width. The remaining 3 are bound-only mutations that keep results identical, so only the work-bound checks catch them: PTO confirmation that keeps scanning, ACK service that walks every covered registration, and expiry that walks every retained record.

## Work bounds

Counters exist only with the `bbrworkcount` build tag; ordinary builds compile the hooks away. For each mechanism, they count inspected records, failed eligibility checks, and index nodes: bitset words, binary-search steps, tree nodes and heap sift steps. The bounds, stated per call before acceptance, are below. n is ring occupancy, m retained occupancy, R ACK ranges, k transitions or returned records, c confirmations, L outcome lost-state changes, and h ≤ 1.44·log2(m+2) the AVL height.

| Mechanism | Inspected | Failed | Nodes |
| --- | --- | --- | --- |
| ACK transitions | k | 0 | R·(2⌈log2(n+1)⌉ + 10) + 10k + 1000L |
| ACK witness | ≤ R | 0 | 10R |
| PTO confirmation | c + 1 | ≤ 1 | 10(c+1) + 1000c |
| Persistent span | ≤ 49 | ≤ 49 | ≤ 2000 |
| Retained discovery | k | ≤ 2Rh | R(2h+2) + k(3h + 2⌈log2(m+1)⌉ + 2) |
| Retained expiry | expired + 1 | ≤ 1 | 1 + expired·(3h + 4⌈log2(m+1)⌉ + 2) |

The constant bounds follow from fixed structure sizes. Each bitset search reads at most five words per physical range, or ten per logical range. A lost-state change refreshes at most three 16-slot blocks, each holding at most eight run starts. A span query scans at most three blocks per physical range.

[Occupancy-scaling results](work-candidate.json) (candidate) and [the same cases on the diagnostic patch](work-diagnostic.json), summarized from smallest to largest population:

| Mechanism | Scenario | Population, smallest → largest | Candidate inspected / failed / nodes | Stated bound at largest | Diagnostic inspected |
| --- | --- | --- | --- | --- | --- |
| ack-transitions | fresh ACK | acked-history: 1,024 → 32,768 | 7/0/47 → 7/0/67 | 7/0/154 | 7 → 7 |
| ack-witness | fresh ACK | acked-history: 1,024 → 32,768 | 2/0/2 → 2/0/2 | 2/0/20 | 0 → 0 |
| ack-transitions | duplicate cumulative ACK | acked-history: 1,024 → 32,768 | 0/0/24 → 0/0/34 | 0/0/42 | 1,024 → 32,768 |
| ack-witness | duplicate cumulative ACK | acked-history: 1,024 → 32,768 | 1/0/1 → 1/0/1 | 1/0/10 | 0 → 0 |
| ack-transitions | sparse ACK over the whole history | acked-history: 1,024 → 32,768 | 3/0/47 → 3/0/67 | 3/0/114 | 1,026 → 32,768 |
| ack-witness | sparse ACK over the whole history | acked-history: 1,024 → 32,768 | 2/0/2 → 2/0/2 | 2/0/20 | 0 → 0 |
| ack-transitions | late ACK of lost outcomes | lost-runs: 16 → 10,000 | 2/0/62 → 2/0/78 | 2/0/2058 | 2 → 2 |
| pto-confirmation | 0 confirmation(s) | pto-backlog: 0 → 32,640 | 0/0/1 → 1/1/1 | 1/1/10 | 0 → 32,673 |
| pto-confirmation | 1 confirmation(s) | pto-backlog: 0 → 32,640 | 1/0/8 → 2/1/8 | 2/1/1020 | 34 → 32,674 |
| persistent-span | no qualifying run | lost-runs: 1 → 10,000 | 0/0/3 → 0/0/3 | 49/49/2000 | 67 → 30,064 |
| persistent-span | latest run qualifies | lost-runs: 1 → 10,000 | 2/0/8 → 2/0/8 | 49/49/2000 | 69 → 30,066 |
| retained-discovery | one retained receipt | retained: 16 → 4,096 | 1/3/19 → 1/11/51 | 1/32/110 | 16 → 4,096 |
| retained-discovery | duplicate-only ACK | retained: 16 → 4,096 | 0/5/5 → 0/13/13 | 0/32/34 | 16 → 4,096 |
| retained-discovery | sparse over-wide ACK | retained: 16 → 4,096 | 1/8/24 → 1/24/64 | 1/64/144 | 16 → 4,096 |
| retained-expiry | timer entry point | retained: 16 → 4,096 | 2/1/19 → 2/1/51 | 2/1/103 | 16 → 4,096 |
| retained-expiry | send entry point | retained: 16 → 4,096 | 2/1/19 → 2/1/51 | 2/1/103 | 16 → 4,096 |
| retained-expiry | retirement entry point | retained: 16 → 4,096 | 2/1/19 → 2/1/51 | 2/1/103 | 16 → 4,096 |
| retained-expiry | stale deadline entry point | retained: 16 → 4,096 | 1/1/1 → 1/1/1 | 1/1/1 | 15 → 4,095 |

The ACKed-history population is ring occupancy; its largest case registers 98,304 packets, so the ring is full and has wrapped. Its sparse ACK covers all 98,304 packet numbers.

The diagnostic patch keeps fresh-ACK enumeration cheap while ranges are narrow. A duplicate cumulative or sparse over-wide ACK falls back to its full ring scan. Its PTO-confirmation and span visits return to full occupancy once any PTO or loss has occurred, and its retained discovery and expiry walk every retained record. In its counts, witness work is charged to transitions because one loop computes both.

## Gates

[Gate log](gates.log), Go 1.27.1 darwin/arm64:

- `internal/ackhandler` and `internal/congestion`, including the long histories
- the counting build
- `-race` on both changed packages
- 34 root-package BBR, bounded-emission and local-credit tests, with and without `-race`; 2 skip for platform reasons
- `go vet`, including the counting build
- `golangci-lint` on both packages and the counting build
- `go mod tidy -diff`

## Independent review

An adversarial read-only review checked the candidate against the frozen reference and the handler's call paths. It found no divergence and no reachable index inconsistency. It confirmed both relied-upon invariants on reachable paths: Retry and migration clear the ring before numbering continues, history rejects non-sequential packet numbers before registration completes, and probe, MTU-probe, zero-time or backdated registrations are excluded. It also confirmed that the inspected and failed bounds follow from the code.

It noted four gaps, all closed here:
- The reference was asserted, not shown, to be frozen code. A mechanical check finds all 28 extracted frozen declarations verbatim in it (whitespace-normalized, types renamed), with `markLimited` the only intended omission.
- Multi-space wrap and a run across the physical wrap were only incidentally covered. A scripted case now covers them.
- Zero send times were not generated. The same case now registers one.
- The lost-change node budget was analytically tight. Its derivation (at most 901 nodes) is now stated beside the 1,000-node bound.

## Limits

This is local evidence over finite, constructed histories and the handler's call order; it is not a proof, a fuzz campaign or a maintained benchmark. The relied-upon invariants are enforced at registration by existing code; the candidate adds no new assertion. Retained-evidence memory rose and is disclosed above. No endpoint goodput, CPU, RSS, control-latency or impaired-path result is claimed. Those belong to [Demonstrate the BBRv3 correction in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/671), after [Conform the four undispositioned BBRv3 model differences to the selected draft](https://github.com/the-sarge/quic-go-fast/issues/707) builds the full candidate on this layer.

## Reconstruction

Check out `codex/bbr-recovery-service` at the service-layer revision:

```bash
go test ./internal/ackhandler/ ./internal/congestion/
```

```bash
go test -tags bbrworkcount -run TestRecoveryServiceWorkScaling -v ./internal/ackhandler/
```

```bash
BBR_MEMORY_OUT=memory.json go test -run TestRecoveryMemoryObservable -v ./internal/ackhandler/
```

For the diagnostic counts, apply [`diagnostic-workcount.patch`](diagnostic-workcount.patch) to a fresh frozen-component worktree. Copy in `recovery_helpers_test.go` and `recovery_work_test.go` from the service layer, then run the scaling test with `BBR_WORK_REPORT_ONLY=1`. No paid resource, campaign resumption, default-controller change or ledger change occurred.
