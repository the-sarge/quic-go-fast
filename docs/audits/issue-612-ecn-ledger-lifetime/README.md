# Bounded ECN ledger lifetime investigation

## Decision

Retain the existing 4,096-record ECN ledger and conservative fallback. The deterministic evidence below demonstrates an ECN-availability limit from genuinely distinct retained facts, not a compression defect. It does not establish production frequency or justify a new retirement authority. Clarify how the already planned B6 diagnostics should distinguish this fallback; do not implement diagnostics or change the B6 contract here. No new runtime work item is proposed. This completes the investigation in [issue #612](https://github.com/the-sarge/quic-go-fast/issues/612) under its [agent brief](https://github.com/the-sarge/quic-go-fast/issues/612#issuecomment-5820559296).

Inspected runtime and design revision: `b6f34a5576a3d57af85a2bbdb25e3900163a06e4`. The investigation adds one focused characterization test and logging/assertion to the existing recompression case; runtime bytes are unchanged. The PR's certification receipt records the exact candidate and base. The [review contract](contract.md) bounds this report and its evidence.

## Reproduction and results

Run from the repository root with Go 1.27.1 on darwin/arm64:

```sh
go test -count=1 -v ./internal/ackhandler -run '^(TestBBRECNRangeBudgetFallback|TestBBRECNDistinctACKedSuffixPinsRangeBudget)$'
```

All four existing range-budget subtests and the one additional deterministic test passed. The [test source](../../../internal/ackhandler/bbr_ecn_test.go) registers real sent packets through `SentPacket` and processes decoded ACKs through `ReceivedAck`. Direct contiguous packet numbers in the suffix exclude random packet-number skips. The reproduction uses typed recovery inputs, not sockets or a claim about workload frequency.

| Case and observation | Ledger occupancy | Next marking | Feedback |
| --- | --- | --- | --- |
| Existing equivalent ACKed suffix behind unresolved packet 1, through packet 4,200 | 2 records | ECT(0) | Eligible; accepted ECT0=4,200, ECT1=0, CE=0 |
| New distinct ACKed suffix at capacity, through packet 4,096 | 4,096 records | ECT(0) | Eligible; accepted ECT0=2,048, ECT1=0, CE=0 |
| New distinct suffix after packet 4,097 registration overflows and an advancing ACK includes both it and packet 1 | 4,096 records; capacity at most 4,096 | Not-ECT | Failed, ineligible; accepted ECT0 remains 2,048, watermark remains 4,096, response ordinal is zero |
| Existing insertion overflow | At most 4,096 records | Not-ECT | Ineligible; accepted ECT0 remains 1 |
| Existing ACK-split overflow | At most 4,096 retained records | Not-ECT | Split construction fails conservatively; this existing fixture asserts bounded storage and marking |
| Existing individually accounted prefix | At most 1 record | ECT(0) | Prefix can compact; this existing fixture asserts occupancy and marking |

The new case first validates packet 0 with ECT0=1, leaves actual ECT(0) packet 1 unresolved, then acknowledges each packet from 2 through 4,096 individually. Even suffix packets are actually Not-ECT and odd suffix packets are actually ECT(0). Thus the suffix is fully ACKed, yet its adjacent records cannot merge because their sent codepoints differ. The unresolved first record prevents prefix compaction. Capacity alone does not fail validation: the next distinct registration triggers fallback. Packet 4,097 was selected as ECT(0) before its registration discovers the budget breach; fallback governs subsequent marking requests, not retroactive rewriting of a registered marking. Its subsequent ACK reports ECT0=2,050, including the formerly unresolved packet 1, but the validator retains its last accepted total and does not issue new evidence-dependent feedback.

The equivalent suffix case has 4,199 ACKed ECT(0) suffix packets and one unresolved ECT(0) packet. Matching ACK status, generation, actual codepoint and affine ordinals allow those suffix packets to recompress to one record. This contrast isolates distinct representation requirements rather than merely total packet count. The new test intentionally chooses alternating marks; it does not claim an ordinary sender alternates them at this rate.

For the ACK-split case, the old ledger and a temporary replacement can coexist during construction. The cap applies to each range slice; 4,096 is not a claim that peak memory equals exactly one slice, or a measured byte/RSS bound. ACK CPU/allocation investigation remains [#613](https://github.com/the-sarge/quic-go-fast/issues/613).

## Why loss or sampler expiry cannot retire marking facts

[`bbrECNTracker`](../../../internal/ackhandler/bbr_ecn.go) owns the actual sent-codepoint ledger. `appendRange` merges only equivalent adjacent facts before charging capacity; `compact` removes only an individually ACKed prefix whose marked counts have already passed feedback validation. `feedback` anchors an advancing ACK to the actual transmission ordinal and validates cumulative counts before publishing accepted deltas. `resetPath` preserves accepted totals, sent history and the feedback watermark, and cannot repair lost evidence.

Recovery loss is a scheduling/accounting judgment, not evidence that the receiver can never acknowledge that transmission. Likewise, the delivery sampler's bounded retention timer only limits availability of delivery samples. [`ExpireDelivery`](../../../internal/ackhandler/delivery_sampler.go) does not erase the ECN ledger. [`ReceivedAck`](../../../internal/ackhandler/sent_packet_handler.go) and [`captureBBRECN`](../../../internal/ackhandler/congestion_dispatch.go) still dispatch advancing ECN feedback when no live recovery packets remain. The pre-existing `TestBBRECNAdvancingLateOnlyFeedback` characterizes a valid advancing late CE report after PTO retirement and sampler expiry; the package regression run passed, without adding a second investigation scenario.

Any separately accepted alternative would have to preserve these obligations:

| Obligation | Required preservation |
| --- | --- |
| Actual-codepoint authority | Sent facts distinguish ECT(0), ECT(1), Not-ECT and unregistered gaps; numeric interval membership or current capable status cannot invent a mark. |
| Late feedback | Losing a recovery packet or delivery sample cannot turn a potentially valid late report into clean evidence or silently erase necessary marking knowledge. |
| Ordinal anchors | An advancing ACK's largest packet needs its real transmission ordinal; ACK-accounting status and generation distinctions cannot be merged away. |
| Cumulative counter fences | Sent and accepted totals remain connection-wide across path resets; old/mixed counters cannot become fresh-path model evidence. |
| Bounded storage | Registration and ACK splitting remain bounded, with conservative fallback when required details cannot be retained. |

Increasing the budget postpones the same failure while increasing storage and potential ACK work. Evicting on loss or sampler expiry weakens the authority boundary. More aggressive summaries would need a separately accepted representation and late-feedback argument. This investigation supplies neither a necessity finding nor evidence sufficient to choose those designs. Retaining the accepted policy has the smallest blast radius.

## Diagnostics: current, planned, and remaining clarification

| Surface at the inspected revision | What it supplies | Limitation or ownership |
| --- | --- | --- |
| Private `congestion.ECNResult` in [`feedback.go`](../../../internal/congestion/feedback.go) | Reported/accepted/delta counts, watermark, ordinal, path generation, Eligible/Failed/Deferred | No range occupancy or explicit fallback cause; it is internal controller evidence, not a public diagnostic API. |
| Private ledger fields | `len(ranges)`, `evidenceLost`, `counterFailed`, state and drain/fence data | Visible to package tests. `evidenceLost` combines range exhaustion and missing-anchor failure; no persistent insertion-versus-split reason is retained. |
| Legacy [`ecnTracker`](../../../internal/ackhandler/ecn.go) | ECN state updates and validation triggers through qlog/debug logging | `EnableBBRECN` disables this tracker. Its reason strings do not provide BBR ledger diagnostics. |
| `DeliveryStats` | Delivery-history occupancy, eviction, expiry and missing-history information | Describes the sampler/recovery surfaces, not ECN marking-range occupancy. |
| Accepted [consumer diagnostics design](../../designs/bbrv3.md#consumer-adoption-and-diagnostics), incorporated in [B6](../../adr/2026-09-24-bbrv3-controller-plan.md#b6) / [#597](https://github.com/the-sarge/quic-go-fast/issues/597) | Bounded optional connection-owned tracing including validation/fallback reason and history occupancy | Planned, not present at the inspected revision. Immutable public selection getters must not become dynamic controller APIs; disabled tracing must not require per-packet allocation. |

The concrete clarification for B6 is to make ECN marking-history exhaustion distinguishable from counter inconsistency and other validation failures, and to distinguish ECN range occupancy from sampler occupancy. A boolean `Failed` and generic sampler counts cannot answer whether the ledger hit its bound. This fits the already accepted reason/occupancy tracing intent; it is not authorization for a new event schema, mandatory tracing, a maintained telemetry framework, or a new B6 acceptance gate. Whether insertion versus ACK-split attribution merits separate trace detail belongs to B6's bounded design, not another implementation in #612. No additional diagnostics project beyond B6 is justified by this evidence.

## Evidence boundary and completion

Local validation passed: the focused command above, `go test -count=1 ./internal/ackhandler`, `go vet ./internal/ackhandler`, and `go mod tidy -diff`. The package run is the repository's regression gate, not statistical repetition of the investigation. Exact-head diff/link checks, RAS disposition, and applicable hosted check receipts are recorded in the PR rather than appended to this normative report.

There was no benchmark, mutation, statistical repetition, native-platform expansion or production traffic measurement. Production incidence, typical time to exhaustion and whether the availability cost matters to a consumer remain unknown. The evidence does not establish a runtime defect beyond the accepted fallback or qualify BBR for public activation. T4 is not reopened, the implementation frontier gains no blocker, and no retirement, eviction, compaction-policy, budget-size, ECN-policy or diagnostics implementation is authorized. The finite evidence and documented retain decision terminate this investigation.
