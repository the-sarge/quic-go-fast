# Queued-send delivery sampling — evidence accepted

Evidence for [Test delivery sampling across queued and batched sends](https://github.com/the-sarge/quic-go-fast/issues/566), within [Design opt-in BBRv3 for GridCast bulk transfers](https://github.com/the-sarge/quic-go-fast/issues/552). Inspected repository baseline: `c2157d0804ab6bfeef35fc570b7723a79fd456e9`. **The owner accepted this bounded, deterministic timing experiment as sufficient planning evidence after reviewing a plain-language executive summary. Final implementation contracts remain with the design investigation; real-world validation is still required before shipping.**

Registration-based samples recover the configured delivery rate in the ordinary cases, but cannot identify where delay occurred. A per-opportunity quantum does not bound a stalled worker's eventual burst. Subtracting queued bytes bounds that backlog in this model but does not bound residence time, and can materially reduce delivery when worker delay is persistent. Even perfect socket-acceptance feedback leaves post-acceptance queueing invisible. These observations support an explicit approximation and backlog contract; they do not establish that production BBR needs a successful-send timestamp for every packet.

## Inspect and reproduce

- [Replay the frozen disposable terminal viewer](https://github.com/the-sarge/quic-go-fast/blob/331f8165b09bcd71a478c42b5e26fc01e6e3f221/internal/congestion/prototype-delivery-sampling/README.md). Its model, shell and attribution notices are preserved at that revision; the disposable package has been removed from the investigation branch.
- [Summary of all 108 runs](summary.csv): 18 scenarios, three quanta and two queue policies; each run compares three independent samplers.
- [Selected per-packet timing traces](packets.csv): six scenarios, quantum four, both queue policies; every QUIC packet retains its own identity, including GSO members.
- [Selected per-ACK sampler snapshots](samples.csv): delivery count, live record count, timing origins, application-limited marker, interval components, RTT and rate for each boundary. Other traces are accessible in the viewer or by adjusting the explicit exporter selection.
- [Timing and sampling figure](timing.png): representative trace details, not a performance benchmark.

Run `go run ./internal/congestion/prototype-delivery-sampling -export /tmp/delivery-sampling-replay` from frozen revision `331f8165b09bcd71a478c42b5e26fc01e6e3f221` to regenerate the three CSV files. The model needs only the repository's Go toolchain. Fetch the retained investigation branch and use a dedicated replay worktree:

```sh
git fetch origin codex/bbr-delivery-sampling
git worktree add -b codex/delivery-sampling-replay /Volumes/worktrees/quic-go-fast/delivery-sampling-replay 331f8165b09bcd71a478c42b5e26fc01e6e3f221
cd /Volumes/worktrees/quic-go-fast/delivery-sampling-replay
go run ./internal/congestion/prototype-delivery-sampling -export /tmp/delivery-sampling-replay
```

Recreate the figure with `python3 docs/audits/2026-09-24-delivery-sampling-prototype/plot.py` (matplotlib required). The figure is derived evidence; CSV values are authoritative.

## Main observations

Path capacity is fixed at 1,200,000 transport bytes/s (9.6 Mbit/s), with 40ms propagation RTT. Ordinary packets require another 1.01ms of combined host and bottleneck serialization. The producer's configured pacing rate equals that known path capacity; this deliberately avoids testing the full controller's capacity discovery or feedback loop.

| Scenario, quantum four | Per-opportunity quantum | Subtract queued bytes | Implication |
| --- | --- | --- | --- |
| Ordinary | All three maximum observed rates reach 9.6 Mbit/s; minimum registration RTT 41.01ms | Same | Registration is a useful approximation when local delay is small in these schedules. Individual early samples still differ from departure sampling. |
| 80ms worker stall, GSO capacity 16 | Worker wakeup submits 38,400 bytes (32 packets); contiguous host burst reaches 43,200 bytes | Both fall to 4,800 bytes | An emission quantum alone does not cap queued work; rapid refill can also extend a host burst beyond one worker drain. |
| Same stall plus 80ms ACK compression | Maximum paired rate difference is 25.6% of path capacity; registration RTT bias reaches 80.03ms | The same worst difference and bias persist despite the smaller burst | Queue credit is not a clock correction. All-time maximum rate alone hides transient sample disagreement. |
| Post-acceptance stall plus ACK compression | Worker submits at most 4,800 bytes per wakeup, but contiguous host burst reaches 100,800 bytes; registration/acceptance remain identical | Same | Acceptance timestamps and user-space queue credit do not reveal downstream queueing. |
| Persistent 40ms entry delay | Minimum registration RTT 81.01ms versus departure RTT 41.01ms; max registration/departure rates 2.280/2.612 Mbit/s | Same minimum RTT bias; both maxima only 0.958 Mbit/s | Tight queue credit can underfill the path when workers are delayed. The model does not select an acceptable trade-off. |

With GSO capacity 16 and the same worker stall, changing the per-opportunity quantum from one to four to sixteen packets produces wakeup submission bursts of 9,600, 38,400 and 115,200 bytes. The latter schedule does not fill all eight entries before the stall ends; it is not a universal queue maximum. With quantum four, GSO capacities four and sixteen are identical because the quantum already limits each entry to four segments. This is an expected interaction, not evidence that segment count never matters.

Ordinary worker drain capacity matters separately: a worker taking one entry per 4ms reaches 2.4 Mbit/s here; draining up to eight per 4ms reaches 9.6 Mbit/s. All packets remain application-backlogged in these cases. Queue-blocked and credit-blocked ticks are counted separately and never set the application-limited marker.

In genuine idle/restart, supply stops from 501ms to 701ms, all previous work drains, and a marker is set at 601ms. With quantum four, 44 returning samples capture the application-limited marker before subsequent delivery clears it. The normal rate maximum returns to 9.6 Mbit/s. No worker-stall case is mislabeled as genuine application idle. This validates only the supplied limitation scenario, not a production idle detector.

The partial-send case accepts one leading entry, rejects the next with a modeled size error, then submits the tail without resending the prefix. With quantum four, 1,227,600 bytes become transport-delivered instead of 1,228,800. The rejected packet's registration snapshot persists until an explicitly supplied disposal event 200ms later, then disappears without being counted as delivery. This delay is a fixture, not a chosen loss timer or loss response. In the unknown-progress case, the oracle knows one entry departed, while the transport must stop and dispose outstanding state without retrying any offered entry. ACKs arriving after modeled closure do not update delivery. No acceptance result is counted as peer delivery. Quantum-one with queue credit never offers two entries together, so the batch fault is not triggered; `fault_triggered=false` exposes those two runs.

Across the 108 runs, all three samplers end with zero live records and agree on delivered bytes. Peak registration snapshot occupancy is 202 records in this finite campaign. No sample is rejected by the positive/minimum-RTT interval checks in these schedules, so they do not exercise rejection coverage. The largest observed registration rate is 9.6 Mbit/s; this is not proof that overestimation is impossible, especially with pacing above capacity, reordered feedback or a closed control loop. The model retains trace history for inspection; its allocation footprint is unrelated to the live-record metric and is not a production memory measurement.

## Source correspondence and declared translation choices

The sampling subset follows [published draft-06, section 4.1.2](https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-4.1.2), pinned by the prior baseline investigation to source `6db913f5ec575a8852bea926bd6001884e579496`: delivered-byte and timing snapshots at send, delivery counted on first ACK, and rate over the greater send/ACK interval. The model applies a positive-interval guard and a measured minimum-RTT check, snapshots an application-limited marker, and clears it only after delivery passes the marker. Each packet generates a separate ACK, making newest-packet selection a singleton operation. Compression delays these distinct ACKs to a shared arrival time; it does not merge their logical events. No QUICHE maximum-across-one-ACK variant is implemented.

Translation choices: test empty outstanding state before inserting a new snapshot; use map presence instead of a zero-time sentinel; compute raw RTT before the interval guard; keep individual QUIC packet snapshots across GSO; use a byte-valued limitation marker. These are declared experiment semantics, not acceptance of a final algorithm baseline. Loss-adjusted empty-flight behavior, reordering, multi-packet logical ACK aggregation and late-loss undo remain outside this subset.

The three samplers receive the same packet/ACK history but take complete snapshots at different boundaries: registration, acceptance and modeled departure. The latter two are ideal observational comparisons, not proposed worker-owned protocol implementations. In particular, the acceptance comparison assumes instant, complete and ordered feedback, which no existing general production callback supplies. Merely changing the timestamp in a registration snapshot is not equivalent to those comparisons: delivery counts and timing origins may also have advanced in the meantime. The departure-based sampler is still an estimator, not an infallible capacity oracle; actual event times and the configured bottleneck are the experimental ground truth.

Current source was refreshed at the baseline above. Relevant seams inspected before choosing this model:

- [Emission ownership ADR](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/docs/adr/0004-packet-emission-ownership.md): registration before asynchronous I/O and connection-owned protocol state remain intact.
- [Packet emission](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/packet_emission.go): ordinary and GSO construction register QUIC packets before queue handoff.
- [Queue implementation](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/send_queue.go): eight queued entries, grouping, accepted-prefix/no-duplicate behavior, size feedback and fatal cleanup.
- [Batch behavior tests](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/send_queue_batch_test.go), [size-feedback tests](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/send_queue_batch_feedback_test.go), and [real-packer emission tests](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/connection_emission_test.go): existing seams already cover buffer and output behavior; duplicating them would not answer the sampler timing question.
- [Transport integration research](https://github.com/the-sarge/quic-go-fast/blob/e1e402594fb8afd4c471b7812c53c0918cd34b25/docs/audits/2026-09-23-bbrv3-transport-integration.md) and [algorithm comparison](https://github.com/the-sarge/quic-go-fast/blob/5dd88547fc14d198a16b2b91936924743cc39a07/docs/audits/2026-09-23-bbrv3-algorithm-baseline.md) establish the outstanding final-design questions. The model does not adopt their recommendations on the owner's behalf.

## Exact schedule and limits

The producer emits 1,024 datagrams (except fatal closure), one 1,200-byte ack-eliciting QUIC packet per datagram. An entry contains up to the configured GSO segment limit, constrained by the quantum. Ordinary entries can be submitted together as a batch. GSO entries are separate socket submissions at the same modeled worker wakeup; grouping never turns their members into one QUIC packet. Mixed sizes, handshake coalescing, ACK-only packets and ECN batch splits are not modeled.

Time uses integer microseconds. Producer/worker wakeups are on a 100us grid; packet departure, bottleneck completion and ACK times are exact integer events. Sending starts at 1ms. After emitting N datagrams, the next opportunity is N milliseconds later; a delayed opportunity receives no catch-up token surplus. The queue holds at most eight entries; a per-entry delay makes that entry unavailable until registration plus the delay. The worker takes up to its drain limit at each wakeup, immediately accepting eligible entries unless the fault fixture applies. Worker stalls cover [401ms,481ms). ACK compression rounds each ACK arrival upward to a 20ms or 80ms boundary.

This queue abstracts eligibility delay and draining, not a blocked real syscall with a separate in-service group. Real production workers can own a dequeued entry/group while the channel refills. Therefore the eight-entry and burst numbers here are properties of the declared model, not universal upper bounds on all production pending work. An eventual byte-credit contract must include worker-owned/in-service entries, not just channel occupancy, and specify when credit returns.

Host transmission starts at the later of socket acceptance plus host delay or the previous host serialization completion. Each packet takes 10us on the host link. An optional post-acceptance stall defers departures in [401ms,481ms) to 481ms, then serializes the backlog. The bottleneck starts after both host serialization and previous bottleneck service; each packet consumes 1ms there. Forward and reverse propagation are 20ms each, with no receiver ACK delay. `receive` is bottleneck completion plus forward propagation; ACK arrival adds reverse propagation and optional compression. Contiguous host burst bytes count successive packets with no gap between their host serialization intervals. They are distinct from bytes accepted at one worker wakeup.

At equal timestamps, ACKs are processed before registration, acceptance, departure, disposal and idle events, with packet ID as the within-kind tie-break. ACKs are ordered, all normal traffic is eventually acknowledged, and source supply is effectively unlimited except the named idle case. There is no modeled cwnd feedback or adaptation of pacing to estimates. The partial/fatal fixtures provide disposal events but do not implement QUIC loss recovery. The 60-second generation horizon is a fail-fast ceiling, and every normal run emits all 1,024 datagrams; the slowest queue-credit case is allowed to finish instead of truncating residual state.

No native scheduler, socket, offload, allocation, CPU, fairness, competing flow, loss, ECN, migration or production idle-classification claim follows. The unchanged managed endpoint and ECN qualification boundary remains [ADR 0007](https://github.com/the-sarge/quic-go-fast/blob/c2157d0804ab6bfeef35fc570b7723a79fd456e9/docs/adr/0007-managed-ecn-qualification.md).

## Findings to carry into final design

Carry three obligations into [Settle the implementation-ready BBRv3 design](https://github.com/the-sarge/quic-go-fast/issues/558): state the registration-clock approximation explicitly; define byte-based pending-work/quantum accounting across producer, worker and offloads; keep queue limitation distinct from application idleness and dispose snapshots explicitly on non-ACK termination. Do not describe a successful write callback as a wire-departure timestamp.

The bounded result does not force a new per-packet success-feedback API. Such feedback could provide queue-residence diagnostics or restore credit, but has sequencing, overflow, partial-progress, shutdown and path-generation semantics of its own, and cannot eliminate all host delay. The model also demonstrates a cost to overly tight credit. Final design must select an approximation and what evidence is acceptable; there are no invented production tolerances here.

The owner accepted proceeding to final design without a separate native timing experiment at this planning step. If implementation qualification or subsequent design work exposes a need for focused timing evidence, the precise follow-up question is: under the accepted native workload/host matrix, how large are registration-to-socket-acceptance residence and pending bytes (including worker-owned groups), and how do candidate byte-credit limits change delivery and wakeup/drain behavior? Record paired registration/acceptance/ACK traces with sequence/generation identities and explicit dropped-observation accounting. Acceptance traces alone must not be presented as actual wire departures. This experiment would require a separately charted ticket and use the accepted evidence budget; it has not been launched or declared a prerequisite by this prototype.

## Owner disposition and verification

The owner requested an executive summary, then replied “ok yes” to the recommendation to accept these findings as sufficient to finish the design, with real-world validation still required before shipping. This resolves the prototype investigation and does not select a final quantum, timing-feedback API or complete controller design.

Carry the timing approximation, byte accounting across pending work, distinction between local queueing and genuine application idleness, and explicit sample cleanup into the final design discussion. No new native experiment is a prerequisite for that discussion. The existing acceptance campaign still governs implementation qualification; native performance, fairness and deployment suitability remain unestablished by this model.

The disposable model and terminal shell were removed after preserving their frozen source revision and evidence. No consequential architectural choice was settled here, so no ADR was added. Existing glossary terms suffice; no implementation details were added to CONTEXT.md.

All 108 runs completed; the three CSV exports reproduced byte-for-byte; delivery totals, zero residual registration state, queue capacity, unique packet identities and timing order were checked. The terminal viewer passed advance, next-ACK, jump, reset and quit checks. `go vet` and Markdown whitespace checks passed, and the rendered figure was inspected. The sampler subset and queue/failure semantics were manually compared with the pinned sources. No prototype unit tests or production performance campaign were introduced. Production transport code is unchanged.

SHA-256 of frozen CSV outputs:

```text
63dbcd2e4c3c2868d3c55e63a8124de663eb6f36673c0bcf7d1704b6181f3105  summary.csv
2285778ed3c888811c4b5fc05fa35b7f098e79c29cddab780ee497a7e8fbaf44  packets.csv
d7980c3e4587797262f9addd05585b9b76ca832caf35c972a709d5e28a7af5d7  samples.csv
```
