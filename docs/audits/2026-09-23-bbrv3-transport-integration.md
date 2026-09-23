# BBRv3 transport integration requirements

Research for [Identify BBRv3 transport integration requirements](https://github.com/the-sarge/quic-go-fast/issues/556), under [Design opt-in BBRv3 for GridCast bulk transfers](https://github.com/the-sarge/quic-go-fast/issues/552). Inspected on 2026-09-23 at `270dda4cf6a4ff66a873473e8dd68e1f12ea619f`. This is a source investigation and proposed integration contract, not an accepted controller/API design, implementation or performance result.

The [algorithm baseline][baseline], [classic ECN investigation][ecn-research] and [GridCast workload investigation][workload] supply earlier recommendations and consumer evidence. They are not silently promoted into accepted decisions here. Local source citations below pin this investigation's complete SHA; earlier reports retain their own explicitly dated evidence.

## Findings and recommendation

**A different congestion-controller constructor is insufficient.** The current interface lacks packet-number-space identity, delivery snapshots, ACK-event boundaries, application limitation, packet disposal and path epochs. Current recovery supplies enough transport facts to build a private adapter, but the adapter must be designed before BBR formulas are translated. Both initial construction and path migration explicitly choose Reno. [Controller interface][interface], [construction][constructor], [migration][migration]

**Keep recovery registration and all model mutations on the connection goroutine.** Registration precedes the asynchronous send queue, and queued submission is not proof of successful socket emission. This is the accepted emission boundary, not an accidental callback location to move. Recommend a connection-owned sampler/event adapter, with an opt-in BBR controller and the existing Reno behavior retained as default. The exact private interface and metadata layout remain design choices. [Emission ADR][emission-adr], [registration][emission], [worker][worker]

**Prototype:** [Test delivery sampling across queued and batched sends](https://github.com/the-sarge/quic-go-fast/issues/566) tests registration-time sampling and explicit send quanta against a modeled-departure oracle. The proposed contract below preserves the accepted ownership boundary without implementing BBR or changing the public API.

The largest risks are falsely resetting a sampler when loss-adjusted flight becomes zero; treating PTO retirement as congestion loss; retaining metadata after pooled packet reuse; using raw packet numbers as globally ordered transmissions; and feeding old-path or unvalidated ECN evidence into a fresh controller. These are integration obligations inferred from the source paths detailed below, not reproduced runtime failures.

## Current send and acknowledgement boundary

| Stage | Observed source behavior | Integration consequence |
| --- | --- | --- |
| Packet creation | Ordinary append packs, logs and registers each QUIC packet before queue submission. A single `now` is reused across the emission loop. | Store registration time honestly; multiple packets can share it. Do not divide by a zero send interval. [Ordinary emission][emission] |
| Coalesced datagrams | Each long-header and short-header packet is registered separately, all with the same `now`, then one UDP buffer is queued. | Sampling identity is a QUIC packet, not one UDP write or one GSO buffer. Account each packet's recorded length once. [Coalesced registration][coalesced] |
| Recovery registration | `SentPacket` increments sent totals, allocates a pooled history packet, and adds ack-eliciting bytes to flight **before** `OnPacketSent`. | Existing `bytesInFlight` is post-send. A new adapter must name pre-send and post-send values explicitly rather than copying reference pseudocode assumptions. [SentPacket][sent] |
| Packet classification | `IsAckEliciting()` tests the presence of stream/control-frame entries; DATAGRAM frames are stored there even without retransmission handlers. ACK-only packets still call the controller and enter history, but add no flight. | The interface's `isRetransmittable` parameter is actually supplied with ack-eliciting classification. Include congestion-controlled DATAGRAM bytes; do not use frame retransmission eligibility as sampler eligibility. [Packet shape][packet], [DATAGRAM packing][packer] |
| Path-validation probes | `isPathProbePacket` returns through separate history before congestion and ECN send callbacks. | Preserve exclusion from ordinary flight/sampling; keep disposal separate from DPLPMTUD probes, which are different packets. [SentPacket][sent] |
| Queue submission | Queue capacity is eight entries. `Send` consumes the buffer; a stopped worker may release it. Entries contain buffer, GSO size, ECN and limited handshake/path metadata. | There is no general successful-emission callback or actual transmit timestamp available to the sampler. Queue entries and QUIC packets are not interchangeable counts. [Queue contract][queue] |
| Socket write | The worker writes asynchronously, ignores eligible size errors except for handshake feedback, and has accepted-prefix/unknown-progress rules for batching. | Registration may remain outstanding for a packet never accepted by the socket. Adding refunds would change recovery semantics and must not be smuggled into a sampler. [Worker][worker], [batch completion][batch] |

The receiver's ACK establishes receipt of an identified packet; it does not reveal when the sender's socket or NIC emitted it. Even a successful syscall timestamp would mark local acceptance, not necessarily wire departure. A proposed worker-feedback channel would therefore add ownership, ordering and error semantics while still requiring a declared timing approximation. This inference follows from the asynchronous queue and accepted-prefix contracts; no wire-timestamp facility is exposed by the inspected `sendConn` interface. [Socket interface and fallback][sendconn]

### ACK event order

For one accepted ACK, current recovery proceeds as follows. [ACK processing][ack], [history removal][acked-removal]

1. Snapshot `priorInFlight` before processing any newly acknowledged packet.
2. Find newly acknowledged history entries in ascending packet-number order; call frame acknowledgement handlers and remove entries from history while retaining packet pointers in a reusable slice.
3. Update recovery RTT when its eligibility rules permit; invoke `MaybeExitSlowStart`.
4. For a 1-RTT ACK advancing the largest acknowledged number, validate ECN and possibly call `OnCongestionEvent(largestAcked, 0, priorInFlight)`.
5. Detect losses among remaining entries in the ACK's packet-number space; remove their flight bytes, queue frames for retransmission, then notify congestion for eligible losses. These loss callbacks share the loss pass's initial flight snapshot. For 1-RTT, also expire path-validation probes before ordinary ACK callbacks; this disposal does not notify congestion. [Path-probe expiry][path-probe-expiry]
6. Call `OnPacketAcked` for each newly acknowledged packet still included in flight, each with the original ACK-event `priorInFlight`; subtract its flight bytes and return ordinary acknowledged packets to the pool.
7. For 1-RTT, detect spurious losses and age diagnostic history when this ACK's largest number equals the updated space watermark. A repeated largest number can qualify if the ACK newly acknowledges a tracked entry; an ACK with none returns earlier. Then clear the reusable slice, reset relevant PTO state and set the next loss timer.

A BBR event adapter should consume a complete logical ACK event, including newly acknowledged metadata, real loss evidence, validated CE, receive time and pre/post flight. It must not assume one callback equals one ACK frame, one loss callback equals a distinct loss range, or that the frame handlers have not already run. Model phase/bounds updates should occur at a declared event boundary. Recommendations for dispatch order are not permission to reorder Reno's existing notifications.

Timer-driven loss detection also runs without an ACK. Give it an explicit event time and loss-only event shape; the existing loss callback has no time argument. Keep recovery's loss thresholds and timer ownership unchanged. [Loss detection][loss], [timeout branches][timeout]

The inspected recovery paths have no persistent-congestion detector. The congestion interface declares `OnRetransmissionTimeout`, and Reno implements a minimum-window reduction when its argument is true, but production code does not call that hook. Persistent congestion is a separate duration/evidence condition, not ordinary PTO expiry or a consecutive-PTO count. The final design must assign detection ownership, retained evidence, an explicit event and BBR's model response, with a prolonged-loss trace as future verification. This report does not authorize connecting the existing timeout hook to PTO or changing Reno. [Interface][interface], [Reno timeout hook][timeout-hook], [loss detection][loss], [timeout branches][timeout], [RFC 9002 §7.6](https://www.rfc-editor.org/rfc/rfc9002.html#section-7.6).

## Delivery sampler contract to design

The earlier [algorithm baseline][baseline] establishes why BBR needs delivery/timing snapshots and why its reference implementations differ in late-ACK behavior. The current local `packet` stores send time, packet length, encryption level and frame ownership, but no delivered/lost/app-limited snapshot. A controller-private map keyed only by packet number would also collide across Initial, Handshake and application spaces. [Packet structure][packet], [space ownership][spaces]

Recommended minimum information is expressed semantically below; this is not a proposed public Go interface or final struct layout.

| Information | Proposed owner and meaning |
| --- | --- |
| Transmission identity | Recovery assigns packet-number-space identity plus packet number and a connection-local monotonically increasing transmission ordinal. Keep path/model epoch separately. 0-RTT and 1-RTT use the same application packet-number space. [Space mapping][space-map] An ordinal or equivalent delivered boundary permits ordering across spaces and equal timestamps. |
| Send snapshot | Connection-owned sampler captures registered send time, relevant send-epoch origin, delivered bytes and delivery timestamp, loss counter/boundary if required by the selected baseline, outstanding evidence and pre/post flight. |
| Limitation snapshot | Capture the sender-limited marker or boundary at transmission; do not ask the current application queue at ACK time whether an old sample was app-limited. |
| Packet byte domain | Use recorded congestion-controlled QUIC packet bytes as an explicit domain, including packet overhead and retransmitted transmissions; do not equate delivery samples with unique stream bytes, acknowledged application objects or GridCast admission. |
| ACK input | Receive time, space, newly acknowledged packet records, ACK-delay qualification, current path/model epoch and the event's prior/post flight. Select/aggregate samples according to the chosen baseline, not incidental callback order. |
| Loss input | Actual newly lost eligible packet records and their send snapshots; contiguous loss ranges if required by the chosen Startup exit rule. Preserve gaps and packet-space boundaries instead of counting callbacks as ranges. |
| Retirement input | Reasoned disposal for keys dropped, rejected 0-RTT, Retry, PTO frame extraction, path replacement and connection teardown. Disposal is neither delivery nor necessarily congestion loss. |
| Missing-history policy | Explicitly reject or conservatively qualify samples whose snapshot was retired or expired; never invent a send timestamp, zero loss count or app-limited value. |

These requirements are inferred from the observed transport omissions and baseline's sampling/Startup requirements. They do not choose draft deviations, a retained-history budget or a late-ACK policy.

### Flight is not outstanding delivery evidence

The transport removes bytes from flight when declaring a loss. It also removes packets for PTO retransmission preparation without invoking the congestion-loss callback or adding them to `lostPackets`; existing spurious-loss diagnostics therefore cannot rediscover those PTO-retired packets. A sender can therefore have zero congestion-accounted flight while ACKs for past transmissions are still possible. The draft-baseline report already flags the open distinction between inflight and unacknowledged bytes. A sampler epoch reset needs its own explicitly selected rule; changing `bytesInFlight` globally is not a solution. [Loss retirement][loss], [PTO extraction][pto-extract], [baseline ambiguity][baseline]

Loss declarations erase the full history entry. Application-space diagnostic retention holds only packet number and send time, is capped at 64 entries at construction, and is aged by three PTOs during eligible ACK handling. It lacks packet length, delivery snapshot, app-limited state and lost-frame identity. A late ACK does not call the current controller's ordinary ACK callback for that retired packet. [Loss tracker][lost-tracker], [construction][constructor], [spurious detection][spurious]

Additionally, `ReceivedAck` returns before spurious detection when no tracked entries are newly acknowledged. Thus the existing diagnostic path is not a complete late-ACK feed and cannot simply be reused as BBR spurious-loss undo. Selecting draft-like late delivery evidence versus a deliberately restricted policy requires changes to both retention and event discovery. [ACK early return and cleanup][ack]

If retained lost metadata is chosen, separate its sampler lifetime from frame retransmission lifetime. The original packet's frame slices are cleared when retransmission is queued; retaining payloads merely to undo a model bound would add unnecessary memory ownership. Undo of loss-derived bounds must not erase independently validated CE bounds. [Frame retirement][pto-extract], [ECN response investigation][ecn-research]

## Application limitation and idle restart

There is no app-limited callback in the present congestion interface. Emission already distinguishes no data, queue full, hard blocked, paced, receive pending, congestion limited and probe outcomes, which provides a useful starting seam. However, `emissionNoData` is also the zero value of an outcome and cannot be blindly interpreted across every entry point as “application has no demand.” [Controller interface][interface], [emission outcomes][emission-state]

A no-data result during ordinary packing is stronger evidence than `framer.HasData()` alone: the framer includes control and retransmission work, DATAGRAM has a separate queue, and stream/connection flow control can prevent queued user bytes from becoming a STREAM frame. Inspecting only one queue misses these distinctions. [Framer inspection][framer], [flow-control packing][flow], [packet payload construction][packer]

Recommended event semantics:

- Mark a sampling interval sender-limited when otherwise permitted ordinary transmission exhausts eligible data; retain the delivery boundary until its effects have left the sample, including restart after idle.
- Preserve the reason: application drained, flow-control limited, transport-credit limited, pacing limited, queue/CPU limited, receive-work yield, handshake/amplification limited or BBR's intentional ProbeRTT limitation. The sampler may classify several as supply limited, but they are not interchangeable evidence about application demand.
- Do not mark cwnd or pacing stops app-limited merely because no packet was emitted in that iteration. Do not classify a send-worker backlog as idle while queued registered packets remain.
- Attribute the marker at send time and clear it using a chosen delivery/transmission boundary, not wall time alone. Application writes can race queue observation; specify a conservative interval policy rather than promise a globally atomic application state snapshot.

These are proposed design rules, not current behavior. The exact flow-control/supply-limited classification and whether pending worker entries prevent an idle-restart epoch are owner choices. Recovery's amplification, history-pressure, PTO, cwnd and pacing decisions already have distinct ordering that must remain authoritative. [Send mode][sendmode]

## RTT and clocks

Current recovery samples RTT only when the largest ACKed packet is newly acknowledged, at least one newly acknowledged packet is ack-eliciting, the candidate is not a path probe, and its send time does not precede the prior cross-space RTT anchor. ACK delay is capped by the peer maximum for 1-RTT and ignored for Initial/Handshake. These rules do not provide a raw RTT sample for every ACKed packet. [RTT eligibility][ack]

After measurement, `RTTStats.MinRTT` tracks the lifetime minimum of supplied raw RTT samples (including the Retry-derived sample described below); `LatestRTT` and smoothed RTT may subtract eligible ACK delay. Before measurement, minimum/latest/smoothed RTT start at 100ms and `hasMeasurement` is false. `SetInitialRTT` changes only latest/smoothed estimates while unmeasured: it does not seed a measured minimum or set `hasMeasurement`, and unmeasured PTO remains twice `DefaultInitialRTT`. Migration resets minimum/latest/smoothed RTT to 100ms, deviation to zero and measurement state to false while retaining maximum ACK delay. Reading `LatestRTT` after every ACK is therefore neither a new raw measurement nor a replacement for BBR's expiring minimum filters. Preserve transport RTT for loss/PTO while defining a separate controller RTT sample/filter policy. [RTT storage and updates][rtt], [migration reset][rtt-reset]

Retry is distinct from token restoration: when `ptoCount == 0`, `ResetForRetry` calls `UpdateRTT` with the duration from the first retained Initial registration to Retry receipt, floored at 5ms, and zero ACK delay. This is a measurement, not a `SetInitialRTT` seed: it establishes measured state if previously unmeasured, updates the raw minimum and smoothing/deviation state, and enables the measured PTO formula. A controller filter must explicitly decide how to use this non-ACK-derived sample. [Retry RTT measurement][retry], [RTT update semantics][rtt]

Use the existing monotonic clock domain for snapshots and event times. `monotime.Time` is an eight-byte integer; nanosecond representation is not a guarantee of nanosecond OS timer resolution. Guard zero/nonpositive sample intervals and rate multiplication/overflow explicitly. The current bandwidth helper uses bits/second and a direct integer multiply/divide; a BBR design that chooses bytes/second needs an intentional conversion boundary. [Monotonic clock][clock], [bandwidth units][bandwidth]

## Pacing, GSO, batching and MTU

The current pacer is a token bucket that multiplies the supplied rate by 5/4. Its burst budget is the greater of ten maximum datagrams and rate times minimum pacing delay plus timer granularity; both timing constants are 1ms. Its sender uses cwnd/SRTT as the base bandwidth estimate. Reusing this policy unchanged would add an unrequested 25% gain and burst rule on top of BBR's own pacing policy. Reuse mechanics only after explicit parameterization or provide a separate private BBR pacer. [Pacer][pacer], [timer constants][timers], [sender rate][sender-rate]

The send interface exposes only “has at least one packet's pacing budget,” an eligibility deadline and a cwnd predicate. Ordinary emission checks them between packets, including within GSO assembly. It has no explicit controller-selected send quantum. A BBR pacing design needs an intentional quantum/offload-budget contract: maximum eligible bytes per opportunity, small-packet treatment, allowed overshoot and whether already queued bytes consume the current quantum. [Interface][interface], [GSO loop][gso]

The present GSO loop groups same-sized packets with identical ECN marks up to buffer capacity; it splits on a smaller packet, ECN transition or recovery stop. All packets retain the same caller-supplied timestamp. The worker can additionally batch up to eight non-GSO entries sharing ECN; GSO groups use ordinary writes. A GSO syscall failure may fall back to sequential segment writes without creating new recovery registrations. [GSO construction][gso], [worker batch][batch], [GSO fallback][sendconn]

Recommendation: controller delivery/flight accounting remains per QUIC packet, while quantum accounting must explicitly connect packets, UDP datagrams, GSO segments and worker submissions. Preserve current accepted-prefix/no-duplicate semantics on batch failure. A platform having no GSO must still get correct pacing and delivery sampling; GSO should change cost and burst shape, not the identity of delivered bytes.

MTU has two distinct local meanings. Packetization can fall back during handshake, while the congestion-controller maximum datagram size is held at least at configured `InitialPacketSize`; later path-MTU discovery can increase it. Reno rejects a decrease and updates its minimum window and pacer on growth. Migration reconstructs the controller with the initial packet size. BBR's MSS-like constants, four-packet floors, packet-equivalent round choices and quantum rounding need a declared size source that respects this separation. [MTU acknowledgement path][mtu], [Reno sizing][reno-size], [migration][migration]

DPLPMTUD probes are included in flight and successful ACK callbacks, but their loss is excluded from controller congestion notification. If BBR samples probe delivery, preserve actual probe length; if it excludes probe bandwidth samples, still retire its accounting on either outcome. Do not treat a failed oversized probe as ordinary path congestion or confuse it with a path-validation probe. [Packet classification][packet], [send accounting][sent], [loss exclusion][loss]

## Lifecycle and ECN requirements

| Transition | Observed transport action | Required BBR integration invariant |
| --- | --- | --- |
| Initial/Handshake keys dropped | Outstanding bytes are removed, history becomes inaccessible, PTO state resets; no controller disposal callback. | Retire every sampler reference for that space without fabricating loss or delivery; retain valid model knowledge only under the selected policy. [Key disposal][drop] |
| 0-RTT rejected | Prefix of 0-RTT application-space history is removed from flight/history. | Distinguish rejection from ACK and congestion loss; 1-RTT's shared number space must not inherit stale snapshots. [Key disposal][drop] |
| Retry | Flight zeroes; Initial/0-RTT frames are requeued; histories restart from current packet-number generators; If no PTO has fired, RTT receives a Retry-derived measurement floored at 5ms. Controller object and its cwnd/cutback state are retained without a lifecycle notification. | Explicit sampler disposal and Retry-derived RTT rules; choose retention/reset of BBR bounds, filters and saved window despite zero flight. No duplicate delivery or assumption that fresh history means reused packet numbers or a fresh public connection. [Retry][retry] |
| PTO extraction | First outstanding packet is declared lost in history, removed from flight and its frames requeued, without congestion-loss notification or insertion into diagnostic `lostPackets`. | Give this a distinct retirement reason and explicit missing-history policy; no invented BBR loss ratio or TCP RTO reduction. Probe sends retain recovery's cwnd/pacing bypass. [PTO extraction][pto-extract], [send mode][sendmode], [PTO timeout][timeout] |
| Ordinary detected loss | History entry removed and flight reduced before congestion callback; full packet metadata is not retained. | Capture loss evidence before disposal; separately choose late-ACK retention/undo budget. [Loss][loss] |
| Migration/rebinding | RTT and controller reset, outstanding application packets retire, and current code always constructs Reno. Path probes are removed separately; existing `lostPackets` diagnostics survive. | Preserve opted-in algorithm identity, create a fresh path model/sampler epoch, and scope or discard old diagnostics so they cannot update the fresh model. [Migration][migration] |
| Connection closure | Emission drains/join-closes the worker and releases any residual queued buffers after fatal errors. | Release controller-owned metadata and never await ACKs for teardown cleanup; no worker callback may mutate an abandoned controller. [Worker close][batch] |

Current client path replacement increments a generation and resets recovery, then publishes the new connection and drains/joins the old queue on its old send connection before using the replacement queue. Server migration resets recovery, increments the generation and rebinds while retaining the active queue; pending writes use the destination current at syscall time. Registrations already retired from flight/history can thus still emit; eligible frame contents may also be retransmitted later. This residual traffic can load a fresh model's path even though it is absent from that model's accounting. This is not a claim that every packet is sent twice: DATAGRAM frames, for example, have no retransmission handler. Tests must cover residual queued traffic and epoch-scoped retained loss diagnostics as well as new controller construction. [Connection path switch][path-switch], [server migration][path-server], [queue replacement/rebind][path-emission], [migration accounting][migration], [DATAGRAM packing][packer]

The existing connection `pathGeneration` is limited feedback metadata, not a per-registration sampler epoch: recovery packets store no generation, and ordinary short-header/GSO and MTU-probe submissions use empty `sendMetadata`. Coalesced metadata copies the current connection generation; its handshake eligibility flag gates size-error feedback. A per-registration model epoch is a proposed addition, and would still not prove actual egress path across queue replacement/rebinding. Any precise path attribution design must account for this boundary; a conservative model may discard ambiguous sampling evidence without changing the established send contract. [Packet fields][packet], [ordinary emission][emission], [GSO emission][gso], [PTO probe submissions][probe-metadata], [MTU probe submissions][mtu-metadata], [coalesced metadata][coalesced-metadata], [feedback worker][worker]

The ECN validator currently returns one boolean after checking aggregate counts; only validated positive CE in capable state becomes congestion. The ACK handler filters by advancing largest 1-RTT ACK and can return early when no tracked packet is newly acknowledged. Current `OnCongestionEvent` overloads zero lost bytes for CE, and Reno increments the loss-packet statistic even in that case. BBR should receive explicit CE and real-loss fields so CE cannot manufacture sampler loss volume. Preserving default Reno behavior and deciding whether to correct shared historical metrics are separate scope decisions. [Validator][ecn], [ECN acceptance][ecn-capable], [ACK ordering][ack], [Reno statistics][reno-event]

Recommended ECN event shape: validation state/outcome, eligible marking mode, accepted packet-count totals/deltas, packet-number space and feedback watermark, receive time, transmission/model epoch context, pre/post flight, and a flag for unavailable/ambiguous path attribution. Counts remain packets; unequal packet sizes prevent interpreting a CE fraction as a measured byte fraction. Real ACK/loss records remain independent. This extends the private evidence contract; it does not select the response coefficient. [Existing tracker fields and results][ecn], [classic ECN rationale][ecn-research]

Migration needs more than resetting the validator: cumulative counters belong to the continuing packet-number space and can cover both old and new path receipts. A fresh controller must not inherit the old path's validated status blindly, while a fresh validator cannot compare old cumulative totals against zero sent totals. Recommend separate counter-validation continuity and path-local policy epochs, a conservative ambiguous-feedback state, and an explicit revalidation procedure. The exact transition rule remains an owner decision requiring deterministic mixed-path traces. Current `MigratedPath` does not replace the ECN tracker. [Migration][migration], [ECN investigation][ecn-research]

Preserve the accepted managed-ECN qualification boundary. Platform or socket-option success alone is not evidence of usable ECN; BBR operates when ECN is unavailable as well as when validated classic CE is available. This investigation grants no raw metadata authority, native feature capability or platform qualification. [ECN ADR][ecn-adr]

## Private interface and state ownership alternatives

| Alternative | Benefit | Cost and risk |
| --- | --- | --- |
| Extend a common congestion interface with send snapshots, event batches and disposal | One explicit contract can serve both controllers; recovery retains protocol ownership. | Broad Reno/mock migration; preserving its callback order and statistics requires a deliberate compatibility adapter and tests. |
| Private event adapter with legacy Reno and richer BBR dispatch paths | Keeps default notification semantics stable while concentrating new metadata and event construction under recovery ownership. | Two input dialects can drift unless shared facts are captured once and dispatch has a bounded, documented lifetime. Recommended starting design candidate. |
| Controller-owned sampler side table keyed by explicit identity | Enables allocation of larger metadata only for BBR connections; controller keeps its algorithm state together. | Every disposal/lifecycle path must notify it; duplicated indexing and retention can diverge from recovery. A bare packet-number key is invalid across spaces. |
| Sampler snapshot embedded in recovery packet | Natural send/ACK lifetime, no per-packet side-table lookup or separate object necessary. | Larger packet object affects default Reno and pool memory; loss retirement still needs a separate bounded tombstone if late-ACK evidence is required. |
| Successful-send feedback owned by worker | Could measure queue residence or acceptance timing. | Adds cross-goroutine sequencing, migration and partial-failure contracts, without supplying true wire time. Moving registration violates the accepted boundary; not recommended as the initial integration. |

Prefer a narrow private adapter with one state owner on the connection goroutine. Keep pacing and model state private; expose immutable diagnostics or snapshots only where the eventual API design requires them. A sampler inside `internal/congestion` with recovery-owned packet values can avoid a dependency cycle because recovery already imports congestion; importing ackhandler packet internals in the reverse direction cannot. [Current dependency/interface][interface], [recovery imports and ownership][spaces]

Reno preservation means omitted opt-in still creates Reno, no BBR metadata changes its packet scheduling, and migration uses the connection's originally selected private factory/policy. Do not expose internal types just to pass that choice between `Conn`, recovery and congestion. Public selection and consumer compatibility are separate decisions.

## Public selection and consumer adoption contract

External references in this section are RFC 9002 (May 2021) and the official Go Modules Reference consulted on 2026-09-23; the latter is a living document. Repository evidence remains pinned to the revisions in each link.

**Observed compatibility constraint:** ADR 0001 preserves existing caller and wire contracts, while ADR 0002 explicitly keeps wiremux's ordinary build functional with upstream quic-go. The application selects this fork through its main-module replacement; a dependency's replacement does not propagate. Consequently, adding a fork-only public `Config` field alone does not provide a usable optional wiremux integration: referencing that field in an ordinary wiremux build would fail against upstream. This is a source-compatibility constraint, not a reason to expose arbitrary congestion-controller implementations. [Compatibility ADR][api-compat], [module-replacement ADR][api-replacement], [Go module replacement rules](https://go.dev/ref/mod#go-mod-file-replace).

The following are alternatives for the owner to decide, not proposed frozen method names or implemented APIs:

| Selection surface | Benefit | Cost and required contract |
| --- | --- | --- |
| Additive per-connection `Config` selector | Naturally follows dialing, listening and per-client configuration; zero value can preserve Reno | Direct field access needs a separately isolated fork-specific consumer adapter/build variant. Define unknown-value errors and effective configuration copying. |
| Optional structural method on `*Config`, with built-in controller names represented using existing Go types | wiremux can assert a small interface at runtime and still compile with upstream; policy follows each effective configuration | Must validate before connection creation, copy hidden selection through every configuration reconstruction, and define clone immutability. The method is a new compatibility commitment. |
| Optional structural registration on `*Transport` before initialization | Matches an established optional-extension pattern and keeps downstream builds upstream-compatible | A transport can serve many connections, so transport-wide policy differs from per-connection policy; define precedence with listener/per-client configuration, freeze timing, reuse and concurrency. Socket ownership remains separate. |
| Public controller factory/interface | Permits external algorithms and policy experiments | Exposes packet, lifecycle, timing and feedback contracts to callers; mutable state must be fresh per connection/path. Substantially broader compatibility and misuse surface than two built-in choices. |

**Recommendation:** retain a private built-in controller constructor and a small explicit immutable selection policy, defaulting to Reno, and prefer a capability-detectable per-connection configuration route for wiremux. The final design must choose between that route and transport registration or isolated adapters. Reject a process-global switch as the adoption contract: matched Reno/BBR connections, concurrent users and tests need independent policy. For an explicit BBR request on an unsupported library, recommend a visible configuration error rather than silently reporting BBR while using Reno; whether the application offers an explicit fallback mode remains a human decision. No local selection needs a new QUIC wire negotiation; congestion control is sender-local, as RFC 9002 allows alternative algorithms. [RFC 9002, congestion control](https://www.rfc-editor.org/rfc/rfc9002.html#section-7).

**Configuration plumbing to cover:** `Clone` is currently a shallow struct copy, but `populateConfig` reconstructs a `Config` with an explicit field list. A new public or private selection field can survive cloning and still vanish during default population unless deliberately carried. Normal preparation has an error result; `prepareConfigForClient` currently does not, and the server uses it for `GetConfigForClient` results. A selection API must define validation and rejection on that path without changing existing nil/default behavior accidentally. Both client and server recovery constructors, then same-connection path reset, must retain the chosen policy while creating fresh mutable model state as required. Do not put a live controller instance in a shallow-copied config. [Configuration preparation][api-config], [per-client preparation][api-client-config], [connection constructors][api-constructors].

### Trace through the consumed libraries

This investigation rechecks the workload report's pinned GridCast `d852c9b0958004a0b235d896332d96958731b0ca` and wiremux `7566390c41ebe79315b29fffcea5c564cae5c20f`. These are the established consumer evidence baseline, not a claim about the newest consumer checkout or deployed binary. The declared fork dependency there resolves to `81b06388832a97d51af5bc408d27ca6c47eb66a9`, distinct from this report's transport inspection revision. [Workload findings][consumer-workload].

| Establishment path | Pinned source observation | Adoption requirement |
| --- | --- | --- |
| Initial pairing | GridCast passes `DialOptions` to `wiremux.Dial`; `WithTransport` configures the direct-upgrade transport, and the default plan selects the built-in `quic.Transport{}` | Thread the selected policy into the built-in QUIC session before Dial or Listen, on both transport roles. |
| Resume | GridCast calls `EstablishAuthenticated` using `ResumeEstablishOptions`; the `ManagedOption` surface provides selected-interface and timeout/channel options but no controller or custom-transport option | Extend the appropriate built-in configuration path and preserve policy across a fresh authenticated Attempt; do not assume initial Dial options carry over. |
| GridCast migration | `PrepareMigration` receives a separate option type; that option domain expressly excludes custom transports | Configure the fresh candidate without weakening admission/authentication/cutover semantics. A new Attempt gets a new QUIC connection and model; it is not a call to quic-go's same-connection migration reset. |

[Initial pairing][consumer-pairing], [initial options][consumer-options], [default transport plan][consumer-plan], [Resume][consumer-resume], [managed options][consumer-managed], [migration call][consumer-migration-call], [migration options][consumer-migration-options].

wiremux's built-in QUIC `Transport` presently exposes only keepalive and idle timeout settings. Each session builds distinct client/server `quicgo.Config` values, preserving DATAGRAM and packet-profile settings, then calls its packet-transport adapter and `Dial`/`Listen`. That is the concrete integration seam. A wrapper supplied as a custom transport is not equivalent: packet permission, selected-interface and managed-endpoint admission use exact built-in type checks. The existing packet adapter already demonstrates structural optional-fork calls, but that pattern conveys packet-I/O permission only; it is not an existing congestion-control selector. [Built-in transport configuration][consumer-transport], [QUIC config and connection creation][consumer-create], [exact-type gates][consumer-gates], [permission origin][consumer-permission], [optional packet adapter][consumer-adapter].

**Contract to carry into final design:** select locally for each sender independent of client/server role; preserve the choice through all three establishment paths; keep reliable control traffic and DATAGRAMs in the same connection congestion budget; expose enough evidence to verify the effective controller and selected direct topology; retain existing packet-profile/MTU, wrapper, lease, cleanup and authentication contracts. Keep application admission, transport ACKs and useful-file progress distinct. Compile and exercise upstream/default mode, fork/default Reno and fork/explicit BBR variants, including explicit unsupported selection. This is a proposed adoption contract; implementation and the multi-repository rollout remain outside this map.


## Allocation and portability implications

The current packet pool resets fields on reuse and returns acknowledged ordinary packets; lost packets are not returned to the pool. A reusable ACK slice avoids per-event allocation and is cleared after use. History uses packet pointers and skipped-number holes; current limits are 20,000 outstanding and 25,000 tracked entries. These are observed limits, not a promise about every retained byte or a BBR sizing target. [Packet pool][packet], [ACK scratch][ack], [history][history], [limits][limits]

Metadata costs must be measured after a representation is selected. For intuition only, an additional 64 bytes for 25,000 live records is 1,600,000 bytes (about 1.53MiB) per connection, excluding allocator size-class rounding, history slots, ACK scratch, retained losses and controller state. This arithmetic is not a measurement of a proposed struct or live workload occupancy. An optional pointer avoids most inline bytes for Reno but can add a pointer field, separate allocation/pooling and another lifetime to reset. A side table has indexing/capacity costs; a shared inline snapshot charges every packet object. Avoid selecting a representation from this arithmetic alone.

Recommended accounting separates live recovery records, retired-loss evidence and temporary event storage. Give each a bounded lifetime, clear references before pool reuse, and prohibit controller storage of borrowed `ackedPackets` pointers beyond the synchronous event. Large ACKs must not force an unbounded per-event allocation; reuse capacity with a declared high-water policy. Measure `allocs/op`, bytes/op and retained heap in both opted-in and default modes before asserting performance preservation.

Clock conversion, timers and portable Go arithmetic can remain platform independent, but scheduler delay, socket batching, GSO and ECN availability are platform/route dependent. The current 1ms pacing floor is a software policy; adjusting it does not prove native wakeup precision. Exercise ordinary send, GSO, sequential fallback and optional batch paths separately. Qualified managed endpoints, ordinary UDP and consumer wrappers have different capability surfaces; retain the prior workload report's tentative platform matrix until the evidence-budget decision selects actual hosts. [Clock][clock], [pacer][pacer], [send capability/fallback][sendconn], [workload][workload]

## Proposed sampler/pacing prototype

This is a proposed new HITL investigation for the map, not an experiment performed here and not authorization to implement the controller. The human should review the traces and tradeoffs before its result becomes a design decision.

**Question:** Can delivery sampling based on the existing packet-registration boundary, combined with an explicit send quantum, distinguish path delivery from local queueing and ACK compression well enough to inform the BBRv3 integration design, or is additional bounded send feedback necessary?

Use the smallest disposable deterministic timing model with an oracle based on modeled departure times. Hold bottleneck capacity and propagation RTT fixed while separately varying registration-to-submission delay, GSO segment count, worker batch draining, ACK compression and genuine application idle/restart. Treat queue saturation separately from application limitation and include a small partial-send/error case. Distinguish registration, socket acceptance, modeled departure, receiver receipt and ACK arrival; acceptance is not actual NIC transmission, and eight queue entries do not bound queue residence time. Compare registration-time sampling and explicit quantum rules for finite samples, exactly-once delivery, bounded outstanding state and explained rejection of evidence. The oracle is experimental ground truth, not an available production timestamp API. If the prototype later exercises production emission, preserve its real-packer test boundary. Native costs remain unmeasured by an abstract model. [Emission conventions][conventions]

[Test delivery sampling across queued and batched sends](https://github.com/the-sarge/quic-go-fast/issues/566) owns the prototype. Its human trace review should inform the private contract; it is not a full BBR port, native allocation campaign, fairness claim or public API. [Choose acceptance criteria and an evidence budget](https://github.com/the-sarge/quic-go-fast/issues/557) owns numeric tolerances, hosts and experiment budget; [Settle the implementation-ready BBRv3 design](https://github.com/the-sarge/quic-go-fast/issues/558) owns the selected semantics and deviations.

## Meaningful verification for implementation

No runtime tests were added or executed for this documentation-only investigation. The following are future contract tests with externally observable or accounting invariants, not tests that restate a proposed algorithm's implementation.

| Test boundary | Required cases and oracle |
| --- | --- |
| Sampler/accounting | Equal packet numbers in different spaces; equal timestamps; mixed sizes and retransmissions; DATAGRAM-only traffic; ACK-only and path probes; zero/invalid intervals. Exactly-once packet delivery, explicit byte units and no division/overflow surprises. |
| ACK event construction | Disjoint ACK ranges, duplicate/reordered ACKs, ACK plus CE plus loss, loss-only timer event, path-probe expiry, repeated-largest diagnostic eligibility, late-ACK-only event and missing history. Fixed pre/post flight, ordered valid evidence, no CE-created packet loss. |
| Limitation and model rounds | Full cwnd versus app drained, stream/connection flow control, queue full, receive pending, anti-amplification and ProbeRTT. Correct sent snapshot and round/idle boundary despite identical times or packet-space transitions. |
| Lifecycle | Key discard, rejected 0-RTT, Retry before/after PTO, PTO extraction, close, path replacement and server rebind. Explicit Retry state policy and PTO disposal without congestion reduction; prolonged-loss evidence distinct from PTO. Account for residual queued traffic and retained old-path diagnostics. No leaked metadata, stale epoch update, duplicate delivery or algorithm reset to Reno after opt-in. |
| Pacing/emission | Real packer with GSO/non-GSO, small final packet, ECN batch split, queue capacity, rate change with queued work, GSO failure and accepted-prefix batch errors. Correct packet budget, no resend of accepted packets and declared burst/quantum bounds. |
| RTT/MTU | ACK-delay eligibility, restored latest/smoothed estimates without a measured minimum, Retry-derived measured RTT, migration measurement reset, cross-space RTT filtering, expiring BBR minima, MTU growth and handshake fallback. Recovery timers preserved and explicit controller RTT/size policy. |
| ECN continuity | Invalid/missing/reordered counters, validation transition, sparse/sustained CE, CE plus loss, mixed old/new-path counts and unavailable capability. Exactly-once usable deltas or explicit conservative exclusion; no invented CE byte attribution. |
| Default and consumer regression | Omitted opt-in and existing clients/servers retain Reno construction, send behavior and build compatibility; selected controller survives private construction/migration and config-copy boundaries. |
| Allocation/ownership | Event scratch reuse, maximum tracked evidence, repeated losses and packet pool reuse under race checking. No borrowed pointers retained, bounded retirement and reported default/BBR allocations. |

Existing recovery tests already cover ACK ranges, cross-space RTT, ACK delays, PTO, 0-RTT, Retry, ECN and spurious-loss diagnostics; use them as regression anchors, not proof that the new sampler contract is covered. Emission and queue tests cover wakeups, real packet behavior, ECN boundaries, accepted-prefix failure and handshake MTU attribution. Extend the relevant behavior-focused seams rather than introduce a packer mock. [Recovery tests][recovery-tests], [emission tests][emission-tests], [batch tests][batch-tests], [feedback tests][feedback-tests]

## Remaining owner decisions and evidence limits

The source investigation resolves where transport facts exist and where information is lost. The implementation-ready design still must select: sample aggregation/late-ACK policy and draft deviations; send/ACK clock and RTT policy; limitation/idle boundaries; metadata ownership/retention budget; event ordering and CE/loss suppression; pacing quantum/queued-byte accounting; Retry state retention, persistent-congestion detection/response, lifecycle and ambiguous migration feedback rules; and the public opt-in mechanism. The proposed prototype makes one empirical uncertainty precise; it does not replace the live design conversation.

No packet capture, network benchmark, allocation benchmark or controller simulation was performed. Existing source behavior and accepted ADRs are the evidence; memory arithmetic, private contracts and the prototype are explicitly recommendations or open choices. Citation paths and line bounds were checked against the pinned local tree, and Markdown whitespace checks were run before committing.

[baseline]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/audits/2026-09-23-bbrv3-algorithm-baseline.md
[ecn-research]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/audits/2026-09-23-bbrv3-classic-ecn.md
[workload]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/audits/2026-09-23-gridcast-bbr-workload.md
[interface]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/interface.go#L1-L27
[constructor]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L120-L160
[migration]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L1120-L1143
[emission-adr]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/adr/0004-packet-emission-ownership.md#L5-L15
[emission]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L186-L230
[worker]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/send_queue.go#L147-L195
[coalesced]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L441-L488
[sent]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L252-L317
[packet]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/packet.go#L10-L60
[packer]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_packer.go#L636-L704
[queue]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/send_queue.go#L11-L131
[batch]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/send_queue.go#L197-L282
[sendconn]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/send_conn.go#L11-L167
[ack]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L378-L482
[acked-removal]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L525-L616
[loss]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L787-L865
[timeout]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L867-L945
[spaces]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L3-L114
[pto-extract]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L1041-L1072
[lost-tracker]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/lost_packet_tracker.go#L11-L73
[spurious]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L485-L523
[emission-state]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L52-L74
[framer]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/framer.go#L83-L102
[flow]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/framer.go#L185-L219
[sendmode]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L980-L1024
[rtt]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/utils/rtt_stats.go#L17-L139
[rtt-reset]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/utils/rtt_stats.go#L141-L148
[clock]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/monotime/time.go#L1-L37
[bandwidth]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/bandwidth.go#L9-L22
[pacer]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/pacer.go#L11-L110
[timers]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/protocol/params.go#L125-L156
[sender-rate]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/cubic_sender.go#L277-L285
[gso]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L233-L281
[mtu]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/connection.go#L2188-L2196
[reno-size]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/cubic_sender.go#L320-L330
[drop]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L173-L222
[retry]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L1074-L1118
[path-switch]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/connection.go#L924-L940
[path-server]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/connection.go#L1271-L1311
[path-emission]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L541-L557
[ecn]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/ecn.go#L41-L303
[ecn-capable]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/ecn.go#L168-L303
[reno-event]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/cubic_sender.go#L183-L224
[ecn-adr]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/adr/0007-managed-ecn-qualification.md#L5-L24
[history]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_history.go#L13-L89
[limits]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/protocol/params.go#L8-L75
[conventions]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/agents/conventions.md
[recovery-tests]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler_test.go#L86-L1772
[emission-tests]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission_test.go#L16-L178
[batch-tests]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/send_queue_batch_test.go#L83-L350
[feedback-tests]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/send_queue_batch_feedback_test.go#L20-L64
[space-map]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L365-L376

[api-compat]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/adr/0001-upstream-compatibility.md
[api-replacement]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/adr/0002-adopt-through-module-replacement.md
[api-config]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/config.go#L11-L166
[api-client-config]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/server.go#L873-L886
[api-constructors]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/connection.go#L289-L470
[consumer-workload]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/docs/audits/2026-09-23-gridcast-bbr-workload.md
[consumer-pairing]: https://github.com/GridCastIO/gridcast/blob/d852c9b0958004a0b235d896332d96958731b0ca/internal/pairing/v1.go#L137-L147
[consumer-options]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/options.go#L76-L94
[consumer-plan]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/upgrade_plan.go#L588-L591
[consumer-resume]: https://github.com/GridCastIO/gridcast/blob/d852c9b0958004a0b235d896332d96958731b0ca/internal/transfer/resume_wire.go#L82-L111
[consumer-managed]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/managed_establishment.go#L18-L59
[consumer-migration-call]: https://github.com/GridCastIO/gridcast/blob/d852c9b0958004a0b235d896332d96958731b0ca/internal/transfer/migration_auth.go#L27-L63
[consumer-migration-options]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/migration.go#L93-L112
[consumer-transport]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/transport/quic/transport.go#L94-L165
[consumer-create]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/transport/quic/transport.go#L251-L393
[consumer-gates]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/upgrade_plan.go#L489-L520
[consumer-adapter]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/transport/internal/adapter/packet_permission.go#L13-L69
[consumer-permission]: https://github.com/GridSwarm/wiremux/blob/7566390c41ebe79315b29fffcea5c564cae5c20f/upgrade_plan.go#L341-L355
[timeout-hook]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/congestion/cubic_sender.go#L287-L297
[path-probe-expiry]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/internal/ackhandler/sent_packet_handler.go#L767-L785
[coalesced-metadata]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/connection.go#L2598-L2600
[probe-metadata]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L381-L399
[mtu-metadata]: https://github.com/the-sarge/quic-go-fast/blob/270dda4cf6a4ff66a873473e8dd68e1f12ea619f/packet_emission.go#L516-L530
