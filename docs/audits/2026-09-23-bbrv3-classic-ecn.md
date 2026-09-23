# Classic ECN responses for BBRv3

Research for [Evaluate classic ECN responses for BBRv3](https://github.com/the-sarge/quic-go-fast/issues/555), under [Design opt-in BBRv3 for GridCast bulk transfers](https://github.com/the-sarge/quic-go-fast/issues/552). Inspected on 2026-09-23 at local revision `432696f50c71e9a305112613dd7df290ab6724ab`. This report recommends candidates and evidence; the owner has not selected a policy. It neither implements BBR nor changes the default controller.

## Recommendation

Carry a **binary, validated classic-CE congestion event with explicit rate and flight limits** into design and comparison. Preserve ECT(0) on capable paths, respond to the first eligible positive CE delta, and retain loss as a separate signal. Compare a conservative 0.5 retained-limit candidate with a 0.7 model-oriented candidate before choosing the coefficient and release rule. These are proposed BBR extensions, not behavior specified by the pinned BBR draft. A single-ACK cwnd reduction that the next BBR update immediately restores is not an adequate design.

Do not import Google's low-latency ECN assumptions, wait for a 50% marking fraction, disable ECN merely because BBR is selected, or treat endpoint qualification as proof of an L4S service. The detailed grounds follow. Final selection belongs to [Settle the implementation-ready BBRv3 design](https://github.com/the-sarge/quic-go-fast/issues/558).

## What the primary sources establish

| Source | Observation and consequence |
| --- | --- |
| [BBR draft 06 §3.7–3.8][bbr-ecn] | An ECN-advertising connection must treat CE as congestion. The draft intentionally does not choose a classic, ABE or L4S response. An explicit local policy is necessary; this report does not claim the draft supplies one. |
| [RFC 3168 §5 and §6.1.2][classic] | Classic ECN substitutes a congestion indication for a drop; the original response is essentially the response to one loss. TCP suppresses repeated decreases for the same window. This supplies an event-based baseline, not a BBR-specific formula. |
| [RFC 8311 §4.1][experiments] | Differing CE and loss responses are permitted through changes documented in an IETF-stream Experimental RFC. It is not blanket evidence that any locally invented coefficient is an approved Internet standard. |
| [RFC 9002 §7, §7.1, §7.3.2 and §8.3][recovery] | QUIC permits alternative controllers, subject to RFC 8085 congestion-control guidance. CE responses can differ. Its NewReno example halves the window and uses a recovery period to avoid repeated cuts for one flight; that 0.5 constant is not a universal BBR requirement. Congestion control is per path. |
| [RFC 8087 §2–3][benefits] | ECN can signal congestion without losing delivered data, avoiding repair and associated application delay. ECN-capable traffic can still suffer real loss. Preserving both signals is necessary; lower loss alone is not proof of lower queue delay or fair coexistence. |
| [RFC 8511 §3–4][abe] | ABE recommends 0.8 retention for standard TCP congestion avoidance and discusses algorithm-specific tuning. It does not specify BBR behavior or establish 0.8 as its correct coefficient; Startup needs separate treatment. |
| [RFC 8257 §3 and §5][dctcp] | DCTCP estimates a marked-byte fraction, smooths it and scales the decrease; its shallow-threshold deployment assumptions require additional measures for public-Internet use. This differs from sparse classic events. |
| [RFC 9331 §4][l4s] and [RFC 9332 §2][dualq] | L4S uses ECT(1), sufficiently detailed feedback and a scalable response with coexistence requirements. QUIC supplies suitable feedback, but feedback capability alone does not establish that response or the network's dual-queue treatment. ECT(0) remains this effort's candidate marking. |

These sources motivate the design alternatives; none establishes a measured result for this fork. Their standards-status distinctions do not prevent local research or controlled comparison. In particular, “BBR supports ECN” is incomplete unless it names both marking/validation and the selected congestion response.

## Pinned implementation evidence

The [baseline investigation][baseline] fixes TCP `90210de4b779d40496dee0b89081780eeddf2a60` and QUICHE `41ee992597583771801dcb8569e1eca71842ef74`. Their ECN behavior differs materially:

- **Google TCP:** eligibility requires ECN negotiation plus `TCP_ECN_LOW`, whose comment requires a shallow-threshold environment and precise feedback. Sender eligibility additionally checks the default 5ms minimum-RTT limit. The EWMA gain is 1/16, initially one; ECN reduces `inflight_lo` by approximately `1 - alpha/3`, while this ECN branch does not reduce `bw_lo`. Startup exits after two qualifying rounds at or above 50% marks; the probe excessive-inflight test uses greater than 50%. These are fixed-point implementation details, not classic-ECN defaults. [Eligibility][tcp-eligibility], [constants][tcp-constants], [EWMA/Startup][tcp-alpha], [probe predicate][tcp-predicate], [bound adaptation][tcp-bounds].
- **QUICHE Bbr3Sender:** `EnableECT0()` and `EnableECT1()` both return false, and its congestion callback ignores `num_ect` and `num_ce`. That implementation cannot supply this map's required CE policy by translation. [ECT methods][quiche-enable], [callback][quiche-callback].

Inference: extending the TCP path to arbitrary WAN RTTs by removing its eligibility gate would also export its marking-threshold, feedback and response assumptions. Accurate QUIC counters solve only the feedback part. Conversely, copying QUICHE's disabled ECT would violate the map's approved scope.

## Existing transport: observations, not requested fixes

The following trace uses the pinned local revision. It identifies contracts the BBR design must resolve while preserving [Managed ECN qualification boundary][qualification].

| Stage | Observed behavior |
| --- | --- |
| Endpoint metadata authority | The managed reader correlates the exact buffer/range/address and lease operation before exposing ECN. Marked writes use qualified direct or checked singleton routes; managed capability requires the qualified endpoint and an eligible route, and does not advertise GSO. [Read correlation][managed-read], [write routes][managed-write], [capabilities][managed-cap] |
| Marking and batches | The sender tracker uses ECT(0) during testing/capable states. Unless capability is confirmed earlier, it counts ten registered test packets, then uses Not-ECT while unknown; failed state is terminal for that tracker. Native GSO construction splits at marking changes, and GSO fallback preserves the mark on each segment. [Tracker][tracker-mode], [GSO grouping][gso], [fallback][fallback] |
| Receive reporting | Receive trackers retain ECT(0), ECT(1), CE totals and copy them into ACK frames; separate handlers serve packet-number spaces. [Counts][receive-counts], [spaces][receive-spaces] |
| Validation | The tracker checks impossible ECT totals, missing/decreasing/too-small counters and loss/all-CE test outcomes. It trusts a positive CE delta only in the capable state, including an ACK that establishes that state. Its public result is only a boolean. [Validation][validation], [capability and CE][capable] |
| Controller notification | ACK processing returns early without newly acknowledged tracked packets. For advancing 1-RTT largest-ACK values, accepted CE calls `OnCongestionEvent(largestAcked, 0, priorInFlight)` before loss detection and per-packet ACK callbacks. Counts, validation state, CE reason, send/receive timing and path identity are absent from that callback. [ACK processing][ack-flow], [interface][cc-interface] |
| Current response | The shared callback increments `PacketsLost` even for CE with zero lost bytes. It suppresses repeated cutbacks using the last-cutback sent boundary. The current Reno branch multiplies cwnd by **0.7**, unlike RFC 9002's 0.5 example. Neither establishes the right BBR coefficient. [Local constant][local-beta], [callback][local-response] |
| Path transition | `MigratedPath` resets RTT, retires outstanding history and constructs a new Reno controller, but does not replace/revalidate `ecnTracker`. Existing connection `pathGeneration` is separate from ECN feedback. This is a source-observed design gap, not a newly reproduced runtime failure. [Migration][migration], [generation][generation] |

The [GridCast workload investigation][workload] establishes a distinct consumer boundary: its pinned ordinary wrapper is not the managed lease path, and its consumed release has no ordinary Windows ECN capability despite later managed Windows qualification. Actual ECN engagement must therefore be recorded on each tentative macOS/arm64, Linux/amd64 and Windows/amd64 test path. GridCast's fresh authenticated Attempt/connection replacement also differs from migration within one QUIC connection.

## Required feedback semantics

RFC 9000 supplies cumulative counts per packet-number space, including each processed coalesced QUIC packet; duplicates do not add counts. Validate before consuming them. Lost ACKs can make count growth exceed newly ACKed packets; reordered ACKs that do not advance the largest ACK must not cause validation failure. Failure disables marking; new paths require validation. [RFC 9000 §13.4][transport-ecn]

The proposed adapter contract should preserve those facts rather than expose an unqualified boolean:

| Information | Required interpretation in the design |
| --- | --- |
| Validation outcome and marking eligibility | Distinguish unavailable, testing, capable and failed feedback. Invalid counts must not become either CE congestion or a zero-mark sample. Disabling marking must not disable ordinary loss response or receive-side ECN reporting. |
| Accepted totals and deltas | Carry validated ECT(0), ECT(1), CE totals and their exactly-once increments, packet-number space and feedback watermark. A binary policy needs only positive CE plus epoch context; retaining counts supports diagnostics and future comparison without changing wire format. |
| Units | Counts are packets, not bytes. For an ECT(0)-only fraction experiment, `deltaCE / (deltaECT0 + deltaCE)` describes the same feedback interval. Dividing CE growth by this ACK's newly acknowledged packet count can exceed one. Converting to a marked-byte fraction requires an additional justified estimator; ACK_ECN does not identify which packet lengths were CE-marked. |
| Timing and attribution | Preserve ACK receive time, relevant send-time/boundary evidence, current flight bytes and packet-timed-round context. Declare packet-registration versus socket-completion semantics under send failure; registration alone does not prove emission. The largest acknowledged packet is an event anchor, not proof that it was marked. Delayed aggregated feedback can span rounds and controller phases. |
| Signal separation and ordering | Preserve actual ACKed and lost bytes independently. CE must not fabricate lost packets, retransmissions, sampler loss ratios or loss statistics. Merge CE and actual loss reductions in one event using an explicit rule; avoid two unrelated multiplicative cuts from one feedback batch. |
| Path identity | Attach path generation to sent evidence and validation/controller state. A changed path must not inherit the old path's capable status or congestion cap blindly. Retain cumulative counter baselines independently of path-local policy state. |

**Migration is not a counter reset.** ACK ECN counters belong to the packet-number space, not a newly selected path. Zeroing a new validator's sent totals while accepting old cumulative counts would reject otherwise ordinary feedback. Worse, one cumulative delta can include receipts from old and new paths without identifying their individual CE packets. The integration design must choose a conservative transition procedure, retain enough old send evidence, and define when current-path feedback becomes usable. Simply stamping the ACK's receiving path onto the entire delta cannot solve attribution.

**Counter-only feedback needs an explicit rule.** The current early return and largest-ACK filter can skip reports independently of CE growth. Specify how safely validated count progress is consumed without replay, how late ACKs for retired/lost packets are treated, and when unavailable evidence causes conservative fallback. This report does not claim that every repeated-largest ACK can safely be processed as fresh validated feedback.

## Response candidates and trade-offs

Here `r` means the fraction retained after a congestion event. Candidate values are comparison parameters, not selected constants or performance promises.

| Candidate | Concrete response to compare | Benefit and risk |
| --- | --- | --- |
| **A: conservative event bound; recommended starting comparator** | On the first eligible validated positive CE delta in an event epoch, stop aggressive growth/probing and apply separately tracked pacing-rate and flight caps with `r = 0.5`, subject to declared minimums. Retain measured bandwidth/RTT evidence rather than pretending capacity halved. | Easy to observe and independent of mark density; supplies a strong response to sparse classic marking. Risks underutilization and slow recovery on long-RTT shallow queues. This is a proposed BBR safety bound inspired by classic response, not an RFC-prescribed BBR algorithm. |
| **B: model-oriented event bound** | Use the same binary trigger and event suppression with `r = 0.7`; integrate caps into BBR's sending limits and phase transitions. No minimum CE fraction is required. | May retain more bulk throughput, but could drain more slowly or crowd Reno/CUBIC. Sharing a number with BBR loss bounds or local Reno is not evidence of equivalent dynamics. |
| **C: ABE-inspired event bound** | Explore a less severe fixed decrease, such as `r = 0.8`, while retaining a real response to every eligible event. | A documented TCP precedent motivates a hypothesis, not a BBR constant. Extra tuning/validation is needed, especially for Startup and persistent marking. Lower priority than A/B for the initial design. |
| **D: proportional marked-fraction response** | Build a defined round estimator and EWMA with a classic-ECN safety response; compare it only if A/B evidence shows a reason. | Adds smoothing, interval attribution and tuning complexity. A pure shallow-threshold TCP/DCTCP response without safeguards is not recommended for this initial WAN design. |

All viable candidates need the following **explicit decisions**, not just a coefficient:

- The first eligible CE event exits Startup growth and aborts ProbeUP growth; specify the Drain/Down transition and treatment in Cruise, Refill and ProbeRTT. Existing low pacing or flight does not justify forgetting the congestion signal.
- Anchor the event epoch to transmissions and acknowledge the uncertainty in aggregate CE attribution. Do not multiply by `r` once per marked packet or once per ACK. Repeated CE from subsequent eligible flights must still cause further response; “once per connection” is inadequate.
- Define which pre-event rate and flight quantities are capped, minimums, and cap persistence. Normal gain updates, ACK aggregation allowance, ProbeRTT cwnd restoration and lower-bound resets must not erase the response immediately. Give an explicit release/reprobe rule after clean post-event feedback; idle time alone is not evidence that congestion cleared.
- Define simultaneous CE/loss handling, preferably choosing the stricter resulting allowance for the shared epoch instead of blindly multiplying both reductions. Preserve every real loss for recovery and model accounting. Spurious-loss undo must not undo independently validated CE.

These common requirements make the alternatives reviewable without pretending to finish the algorithm choice in a research session. Disabling ECN for every BBR connection, ignoring low-rate CE, or changing to ECT(1) without a separate L4S design are outside the approved initial requirement.

## Evidence needed before selection

| Evidence layer | Cases and observations |
| --- | --- |
| Deterministic feedback contract | Missing, bleached, decreased, impossible and ECT-remarked counters; lost/reordered/duplicate ACKs; count-only advancement; late ACKs after loss/disposal; coalesced packets and mixed packet sizes. Assert exactly-once valid deltas, no false loss and no false validation failure from reordering. |
| Validation versus congestion | All test packets lost; all-CE or mixed CE/loss testing; partial successful ECT(0) validation; then sustained 100% CE on an already capable path. The last case must drive congestion response, not be reclassified as initial mangling solely because the mark fraction is high. |
| Controller state traces | Isolated and sustained CE in every phase, one event split over many ACKs, ACK thinning, CE plus loss, spurious-loss undo, idle/resume and long RTT. Record pacing, flight limits, actual flight, phase, cap release and response epochs. Show that the next model update cannot cancel a cut. |
| Path lifecycle | Capable-to-unavailable, capable-to-capable and failed-to-new-path changes; mixed old/new outstanding feedback; monotonic cross-path counters; fresh connections. Prove attribution/fallback and validation transitions separately from controller replacement. |
| Native transport engagement | Record exact endpoint/wrapper/lease, platform, address family, ECN capability and validation, ordinary/batch sends and fallback. A successful socket option or compilation cannot substitute for preserved peer-observed marks. |
| Network and consumer comparison | Compare A/B with the existing default under declared classic ECN AQM and non-ECN/drop cases, short/long and mixed RTTs, sparse/heavy marks, capacity changes, reverse-path impairment and competing Reno/CUBIC/BBR flows. Measure application goodput, transfer completion, queue delay, real loss, per-flow throughput and CPU. Include real GridCast admission/repair behavior; transport ACK rate is not application goodput. |

The acceptance investigation owns numeric thresholds, topology, durations, repetition and budget. A deterministic model can reject self-canceling caps or duplicate cuts; it cannot demonstrate fairness, native metadata preservation or WAN goodput. No performance result is claimed here.

## Handoff questions and limits

[Identify BBRv3 transport integration requirements](https://github.com/the-sarge/quic-go-fast/issues/556) should settle validated-feedback event shape, callback ordering, epoch anchors, missing-history behavior and migration attribution/baselines. [Choose acceptance criteria and an evidence budget](https://github.com/the-sarge/quic-go-fast/issues/557) should select A/B comparison conditions and coexistence thresholds. [Settle the implementation-ready BBRv3 design](https://github.com/the-sarge/quic-go-fast/issues/558) should choose the coefficient, pre-event reference quantities, phase effects, cap persistence/release and standards/deployment characterization together. These existing owners cover the newly precise questions; this investigation does not require a duplicate prototype ticket.

This is source research only. No runtime change, network experiment or BBR simulation was performed. Protocol pages are versioned RFCs and draft 06; code citations pin complete commits. Validation checked all 36 reference definitions, source-file existence and cited line ranges, and all ten specification fragment anchors; Markdown whitespace checks passed. The report preserves ECN metadata authority and qualification constraints and proposes no conflict with their accepted ADR.

[bbr-ecn]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-3.7
[classic]: https://www.rfc-editor.org/rfc/rfc3168.html#section-5
[experiments]: https://www.rfc-editor.org/rfc/rfc8311.html#section-4.1
[recovery]: https://www.rfc-editor.org/rfc/rfc9002.html#section-7
[benefits]: https://www.rfc-editor.org/rfc/rfc8087.html#section-2
[abe]: https://www.rfc-editor.org/rfc/rfc8511.html#section-3
[dctcp]: https://www.rfc-editor.org/rfc/rfc8257.html#section-3
[l4s]: https://www.rfc-editor.org/rfc/rfc9331.html#section-4
[dualq]: https://www.rfc-editor.org/rfc/rfc9332.html#section-2
[transport-ecn]: https://www.rfc-editor.org/rfc/rfc9000.html#section-13.4
[tcp-eligibility]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L361-L372
[tcp-constants]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L259-L313
[tcp-alpha]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L1048-L1113
[tcp-predicate]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L1172-L1197
[tcp-bounds]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L1314-L1399
[quiche-enable]: https://github.com/google/quiche/blob/41ee992597583771801dcb8569e1eca71842ef74/quiche/quic/core/congestion_control/bbr3_sender.h#L97-L98
[quiche-callback]: https://github.com/google/quiche/blob/41ee992597583771801dcb8569e1eca71842ef74/quiche/quic/core/congestion_control/bbr3_sender.cc#L255-L261
[baseline]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/docs/audits/2026-09-23-bbrv3-algorithm-baseline.md
[workload]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/docs/audits/2026-09-23-gridcast-bbr-workload.md
[qualification]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/docs/adr/0007-managed-ecn-qualification.md
[managed-read]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/managed_packet_ecn.go#L33-L72
[managed-write]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/managed_packet_ecn.go#L80-L154
[managed-cap]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/managed_packet_ecn.go#L169-L178
[tracker-mode]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/ecn.go#L38-L139
[gso]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/packet_emission.go#L236-L278
[fallback]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/send_conn.go#L84-L102
[receive-counts]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/received_packet_tracker.go#L12-L116
[receive-spaces]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/received_packet_handler.go#L28-L56
[validation]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/ecn.go#L168-L270
[capable]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/ecn.go#L272-L317
[ack-flow]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/sent_packet_handler.go#L398-L459
[cc-interface]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/congestion/interface.go#L8-L19
[local-beta]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/congestion/cubic_sender.go#L13-L20
[local-response]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/congestion/cubic_sender.go#L199-L224
[migration]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/internal/ackhandler/sent_packet_handler.go#L1120-L1143
[generation]: https://github.com/the-sarge/quic-go-fast/blob/432696f50c71e9a305112613dd7df290ab6724ab/connection.go#L1309
