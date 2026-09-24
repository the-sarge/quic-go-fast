# ProbeRTT filter comparison — owner review pending

Prototype evidence for [Compare ProbeRTT filters on changing-RTT paths](https://github.com/the-sarge/quic-go-fast/issues/560), within [Design opt-in BBRv3 for GridCast bulk transfers](https://github.com/the-sarge/quic-go-fast/issues/552). Repository baseline: `254c57470d89b612713b63ee2b755a2e49c3cd48`. The investigation compares pinned algorithms; it does not choose the final baseline. **Owner reaction and disposition are pending.**

The saved pre-expiration cap is worth carrying into final design discussion: in the deliberately aged-filter scenario, it prevents an inflated first sample from immediately increasing the probing cap. It does not guarantee a fully drained queue when the saved estimate is already inflated. The separate completed-round gate matters on long RTTs. These are model observations and an agent recommendation, not native performance evidence or an accepted design.

## Inspect or replay

[Run the disposable terminal viewer](../../../internal/congestion/prototype-probertt/README.md) with `go run ./internal/congestion/prototype-probertt -scenario expired-queue` from the worktree root. [Transition traces](events.txt), [campaign summary](summary.txt) and [full recorded snapshots](trace.csv) are checked-in outputs from the same code. The [source correspondence](../../../internal/congestion/prototype-probertt/SOURCES.md) pins all three algorithm variants and explicitly lists translation choices.

Nine schedules times three variants were executed. No network was used by the model, and no production controller was added.

## What the runs show

For `expired-queue`, the actual propagation RTT is 100ms, with a 101ms minimum possible measured RTT after packet serialization. The first eligible ACK sees a 501ms queued sample. This is a deliberately initialized boundary case; its prehistory is not simulated.

| Variant | Entry time | Cap at entry | First ProbeRTT duration | RTT estimate at exit | Queue at exit |
| --- | ---: | ---: | ---: | ---: | ---: |
| Published draft-06 | 12,501ms | 365,730 bytes | 400ms | 250ms | 219,000 bytes |
| Pinned filtering proposal | 12,501ms | 73,730 bytes | 552ms | 101ms | 1,460 bytes |
| Pinned QUICHE subset | 13,000ms | 292,000 bytes | 400ms | 200ms | 146,000 bytes |

The two draft-derived variants receive the same first sample: the published design replaces its expired minimum before calculating the target; the proposal uses the lesser of the new target and a cap saved from the old estimate. QUICHE reaches its expiry check later at the supplied Down-exit opportunity, so its entry sample is different. Queue counts are post-send snapshots and include the packet currently being serialized; 1,460 bytes is one newly admitted packet, not a persistent queue.

Other schedules separate the mechanisms:

- **Ordinary queued traffic:** the fresh-seed draft probes first at 6,001ms and exits with a 101ms estimate. Its earlier scheduling avoids the simultaneous-expiry condition in this run. This prevents the deliberately aged snapshot being presented as the routine outcome of draft-06.
- **Remove only the preloaded background queue:** both draft variants enter with a 101ms sample and the same 73,730-byte cap. Their first exit is identical. Filter expiry alone does not imply an inflated estimate.
- **Base RTT rises from 100 to 400ms:** the draft's first probe retains its unexpired old 101ms minimum despite seeing a drained 401ms path; its later main-filter expiry permits the increase. The proposal replaces the minimum but retains the smaller saved cap for that probe. This exposes the trade-off between protecting against queued inflation and temporarily underfilling a genuinely longer path.
- **400ms base RTT:** draft and proposal wait 402ms after starting the hold timer, until a qualifying packet-timed round completes. QUICHE exits after 201ms, with the diagnostic round still incomplete and its estimate still 800ms at that exit. Later ACKs lower that estimate to 401ms. A final summary alone hides the early exit.
- **Add 150ms feedback delay only:** the draft variants wait 552ms after the timer starts; QUICHE waits 201ms. The final minimum is 551ms, not the actual 400ms propagation RTT, because this harness deliberately does not subtract feedback delay. That is an input-policy limitation, not a newly measured path delay.
- **Genuine idle restart:** supply stops at 2s, flight drains, and supply resumes at 14s. Neither draft variant enters on the first returning ACK; QUICHE postpones its minimum timestamp. All record zero ProbeRTT entries through 18s. Empty flight while cwnd-limited in other scenarios is never treated as application idleness.
- **Change only the Down-exit opportunity:** QUICHE entry moves from 13s to 15s. Its first-probe target and duration stay the same. This schedule is an input to the reduced model, not a simulated QUICHE bandwidth-probing cycle.
- **Old 401ms estimate, current 100ms base:** the proposal saves 292,730 bytes, then lowers its cap to 146,000 bytes as its estimate falls to 200ms. It still exits with residual queue. Remembering an old estimate is a guard against a new inflation step, not proof the remembered value is accurate.

## Model contract and limits

`model.go` is a pure reducer plus a small packet queue; the CLI is a separate shell. Each ACK supplies fresh RTT, post-ACK flight, cumulative delivered bytes, the acknowledged packet's delivered-at-send snapshot and an explicit Down-exit opportunity. The round gate compares delivery snapshots, not elapsed wall time. QUICHE's displayed round status is a diagnostic only; its exit ignores it.

Time is integer milliseconds. A synthetic packet/SMSS is 1,460 bytes; one packet takes 1ms of bottleneck service, fixing bandwidth at 1,460,000 bytes/s = 11.68 Mbit/s. Half BDP is `1460 * minRTT_ms / 2` bytes, with a four-packet floor of 5,840 bytes. Caps may be fractional packet multiples; whole-packet admission rounds down. Actual propagation excludes the 1ms serialization and optional feedback delay. Seed minima of 101/401ms represent earlier queue-free measurements, not 100/400ms propagation.

The FIFO departure rule is `departure = max(send_time, previous_departure) + 1ms`; ACK arrival is departure plus the base RTT at send time plus the configured feedback delay. Each packet generates a separate ACK event. A base change affects new sends only, so pending packets retain their scheduled feedback. All ACKs are eligible in this model; there are no losses, CE marks, worker queues, GSO, ACK coalescing, recovery, migration, timer granularity effects or bandwidth-estimator errors. Those omissions prevent throughput, CPU, fairness, resource or deployment conclusions.

Every run starts in ProbeBW with a 400-packet initial burst and a continuously backlogged application except for the named idle case. That burst is an intentional stress fixture, not a recommended send quantum. The outside-ProbeRTT cwnd ceiling is fixed at the greater of the burst size and twice the seeded BDP. Sending is paced at one packet/ms without saved pacing credit. During ProbeRTT the modeled cap constrains cwnd; on exit the draft variants restore the saved normal window, while QUICHE grows by ACKed bytes. **The surrounding ProbeBW model, cwnd gains/aggregation, congestion bounds and Down pacing are deliberately not implemented.** Continued queues and their durations therefore characterize this harness, not the full reference implementations.

The aged snapshot starts at 12s with the main minimum last recorded at 1s, the draft scheduling sample at 6s, and 400ms of preloaded background service ahead of the burst. No further background packets arrive. An application backlog remains present throughout; the idle flag is false. This establishes what the filter/target mechanism does given that state. It does **not** establish whether a full controller normally reaches that state, or its frequency. A reachability/frequency claim would require a full state-machine or native trace; none is needed to expose the conditional behavior here.

Normal schedules start at 1s. Down-exit opportunities are the first ACK at or after successive integer seconds, or successive five-second boundaries in `late-down-exit`; opportunities during ProbeRTT are ignored for entry. This explicitly substitutes a schedule for QUICHE's real Down transition logic. Timer comparison is strictly greater than 200ms for all variants, explaining the 201ms minimum observed hold. QUICHE expiry is greater-than-or-equal at ten seconds; draft expiry is strictly greater. Proposal equal RTT samples refresh the timestamp; draft/QUICHE ordinary minimum updates are strict-lower.

## Disposition to obtain

The proposed disposition is to carry three findings to [Settle the implementation-ready BBRv3 design](https://github.com/the-sarge/quic-go-fast/issues/558): consider a pre-expiration cap, retain an explicit completed-round obligation unless a deviation is justified, and keep probing cadence, RTT input policy and idle detection as separate choices. This ticket need not select the proposal wholesale or accept its ten-second cadence.

The owner should say whether this bounded evidence answers the comparison or identify the remaining concern. The ticket remains open until that live reaction is recorded. If the owner requires a claim about reachability or native impact, chart that precise follow-up instead of treating this reduced model as decisive evidence for it.

## Verification

All 27 scenario/variant runs completed, the full trace reproduced byte-for-byte, `go vet` passed for the disposable package, and the interactive viewer was exercised through advance, jump, reset and quit. The reducer was manually checked against the pinned filter, entry, target, idle and exit source sections, including strict/equal comparisons and delivered-boundary round semantics. No prototype unit tests or transport regression campaign were added; production transport code is unchanged. The source correspondence and licenses travel with the translated prototype.
