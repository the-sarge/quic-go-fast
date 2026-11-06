# Jumbo packets implementation plan

**Date:** 2026-11-05. **Status:** Accepted; not yet implemented. **Identity:** `JP-20261105-F`. **Parent:** pending. **Depends on:** nothing for F1. **Normative scope:** current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions. **Audit history:** this plan's merging PR.

This plan comes from a downstream consumer's owner-approved program. It stands on its own: an implementer needs nothing outside this repository. "The consumer" below means that downstream program, whose qualification results reach this repository through F11's and F12's issues.

## Goal

Let an application opt a `Transport` into jumbo packets on links an operator runs at MTU 9000. A configured connection discovers and uses packets up to 8,972 bytes and recovers when the path stops carrying them. Every other connection stays exactly as it is, and 1452 stays the default everywhere.

Throwaway builds that raised the constant and pinned the size carried over 8 Gbps on a Thunderbolt Bridge at MTU 9000 at half the CPU per Gbit. They can't ship: they change every connection in the process, skip discovery, and can't shrink.

## Current shape (verified at `e243f863dd`)

- **The ceiling is a compile-time constant** with 27 production uses: [`protocol.go` L111](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/protocol/protocol.go#L111). [F1's census](#f1) classifies them.
- **The advertised size is built in the connection constructors** ([`connection.go` L385, L520](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L385)). Readers are built in [`wrapConnWithManagedBuffers`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/sys_conn.go#L102) during [`Transport.init`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/transport.go#L498), with no ceiling input.
- **Discovery only ever raises the size** ([`mtu_discoverer.go`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/mtu_discoverer.go)). A size reduction exists only before confirmation ([`connection.go` L2950](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L2950)) and on a path change ([L1115](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L1115)).
- **A shrink stalls a stream with no error.** Lost data is resent at the same size, loss-timeout probes pack queued data or a bare PING ([`packet_packer.go` L916](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_packer.go#L916)), and small keepalives and ACK-only packets refresh both idle timers ([`connection.go` L986, L1083](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L1083)). Read from source and unproved; F6's first test settles it.
- **The kernel's size refusal is swallowed** except during the handshake ([`send_queue.go` L255](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/send_queue.go#L255)). Batch writers retry per packet into that same route ([L336](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/send_queue.go#L336)). Only BBR handoffs stamp a path generation ([`packet_emission_bbr.go` L29](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_emission_bbr.go#L29)); Reno handoffs pass empty metadata ([`packet_emission.go` L293](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_emission.go#L293)).
- **Discovery probes only leave in the normal send state** ([`packet_emission_bbr.go` L132](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_emission_bbr.go#L132)). Loss-timeout probes outrank the congestion check ([`sent_packet_handler.go` L1378](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/ackhandler/sent_packet_handler.go#L1378)), but the alarm cancels when nothing is outstanding ([L983](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/ackhandler/sent_packet_handler.go#L983)). Padded discovery probes don't count as outstanding ([`packet.go` L42](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/ackhandler/packet.go#L42)), so they arm no alarm, and a lost one is only noticed after a later ACK ([`sent_packet_handler.go` L1066](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/ackhandler/sent_packet_handler.go#L1066)).
- **Each packet carries one DATAGRAM frame** ([`packet_packer.go` L777](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_packer.go#L777)), and receive admission assumes one payload per packet ([`connection.go` L2256-L2266](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L2256-L2266)).

## Decision

- **The Transport owns the ceiling.** Reads happen per Transport, and a typical consumer has one Transport per bound socket. Accepted values are 1452 to 8972.
- **Effective ceiling:** the smaller of the local ceiling and the peer's `max_udp_payload_size`, known when transport parameters arrive. A connection whose effective ceiling is above 1452 is **jumbo-active**.
- **Local configuration controls advertisement and receive-buffer cost. Negotiated activation controls the new sending and failure behavior:** stage-two search and recovery. A configured connection that isn't active sends and fails as today. An unconfigured one is unchanged in every respect.
- **The initial size stays where it is.** The handshake never depends on jumbo, and the config clip stays on the constant.
- **Two-stage search.** Stage one is today's search. Stage two runs only if stage one topped out, meaning no loss narrowed its upper bound, and searches up to the effective ceiling. Stage one's result is the tested fallback size, and the connection reports it so a consumer can publish no more than it can uphold.

| Path, initial size 1232 | Today | Two stages (simulated against this finder's logic) |
| --- | --- | --- |
| IPv4, MTU 1500 hop | 1,438: 4 probes, 0 lost | 1,470: first 4 identical, then 13 more, 11 lost |
| IPv4, MTU 9000 | n/a | 8,957: 13 probes, 0 lost |
| WireGuard 1420 | 1,390: 8 probes, 4 lost | Identical to today |

- **Recovery** follows [RFC 8899 section 4.3](https://www.rfc-editor.org/rfc/rfc8899.html#section-4.3): set a lower size, then confirm it. It runs only on a jumbo-active connection, from the first validated size above 1452 until a fallback is confirmed or the initial size is reached. **Discovery probes are never evidence:** neither their loss nor their refusal counts.

| Step | Size in use | What happens | Moves on when |
| --- | --- | --- | --- |
| 1. Watching | Above 1452 | Counts losses above 1452 from both loss detectors, excluding discovery probes | About three loss timeouts with nothing above 1452 acknowledged and at least three such packets declared lost: go to 2. The kernel refuses a non-probe send as too big: go straight to 4 |
| 2. Liveness check | Unchanged | Needs an ACK of a packet at or below 1452 sent after step 1 fired. Any small packet already flowing counts; otherwise the transport sends a bare PING | Small ACK: go to 3. Any jumbo ACK: back to 1. Neither: it's an outage, so no size change |
| 3. Re-check | Unchanged | Sends up to three recovery probes padded to the current size. After an outage ends, small ACKs arrive first, and this keeps a working path from falling back | ACK at the current size: back to 1, no change. Three probes lost: go to 4. No later small ACK: back to 2 |
| 4. Fall back and confirm | Stage one's tested size | Lowers the size, replaces congestion state, resends in-flight data split to fit, then confirms with up to three recovery probes padded to that size | ACK of a probe or any packet at that size: confirmed, recovery ends. Three probes lost: go to 5 |
| 5. Base | The connection's initial size | Same, padded to that size | ACK: confirmed, and stage one runs again without stage two. Three lost: stay there with no more probing, and the connection's normal rules decide its fate (RFC 8899's ERROR state) |

- **Recovery probes have their own clock and verdict.** They arm no loss alarm, so the run loop holds a deadline per probe, about one loss timeout, and sends each one ahead of the congestion check, as loss-timeout probes do. A probe counts as lost only when its deadline has passed and a small packet sent after it has been acknowledged, so a transport PING follows it if nothing else is flowing. Without that later ACK it's an outage, not a verdict. Ordinary loss timeouts don't change. A path change ends recovery by path generation, so it isn't read as lost probes.

- **It stays down.** A connection that fell back never probes above 1452 again, including after a later path change ([`connection.go` L1118](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L1118) would otherwise re-arm stage two). A new connection gets jumbo back.
- **Probes are the transport's own.** A connection carrying only jumbo DATAGRAMs has no small data, a DATAGRAM frame [can't be fragmented](https://www.rfc-editor.org/rfc/rfc9221.html#section-5), and it isn't retransmitted.
- **Recovery speed is a test target, not a promise:** with sustained traffic on a LAN round trip under 1 ms, about 0.3 s on the far side (a window near 190 ms, a liveness ACK, and three probe deadlines near 28 ms each), and the next send where a kernel refusal reaches this module. It scales with round trip and loss-timeout backoff, and a connection that isn't sending has nothing to detect. A consumer's socket layer may absorb the refusal, so far-side detection has to cover both ends on its own. No doc or contract promises a bound.
- **Buffers.** Two new fixed tiers, 8,972 and a read slot of 8,973, used only by configured Transports. Unconfigured connections never take one. Queue depths stay in entries.
- **Experimental until qualified.** The exported method ships disclosed as experimental. It's refused on Transports configured for native multipath I/O, in either order, so the native endpoint route and non-zero paths never see a packet above 1452. On Windows it's gated on the loopback test: if hosted Windows loopback can't carry 8,972, the method refuses values above 1452 there.

**Rejected alternatives (do not do this):**

- Raise the constant. It changes every connection in the process. **Revisit if:** none: rejected on merit.
- A `Config` field. Read buffers are per Transport, so a per-connection value can't size them. **Revisit if:** none: rejected on merit.
- Pin the size with discovery off. It can't cross a smaller hop. **Revisit if:** none: rejected on merit.
- One long search from the initial size to the ceiling. On an MTU 1500 hop it loses 14 probes and changes today's first probes. **Revisit if:** none: rejected on merit.
- Reuse the migration reset as the fallback. It always restarts at the initial size, which would break a consumer's published DATAGRAM limit, and part of its work lives in its callers. **Revisit if:** none: rejected on merit.
- A timeout-only trigger. Reordering detection can declare big packets lost first under steady small return traffic, so timeouts never fire. **Revisit if:** none: rejected on merit.
- Falling back on a small ACK alone. After an outage ends, small ACKs arrive first, so a working path would drop out of jumbo for good. **Revisit if:** none: rejected on merit.
- Re-climb after a fallback, as RFC 8899's raise timer would. It adds a state and risks repeated stalls for a rare event. **Revisit if:** a consumer with long-lived connections needs jumbo back without reconnecting.
- A byte cap on the receive queues. **Revisit if:** the consumer's resource measurements fail or a consumer reports the worst case matters.
- An upstream PR. Its maintainer asks for an issue first and pushed back on AI-written PRs on quic-go/quic-go#5767. **Revisit if:** the maintainer accepts a design on quic-go/quic-go#3162 or quic-go/quic-go#3955.

**Owner's design:** the connection's run loop owns the packet size, and recovery lives there. The send worker reports a refused-size fact with its path generation and segment size, as [`handshakeSendFeedback`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/send_queue.go#L258) already does, and carries no policy. Adopted. A per-buffer pool pointer would remove the capacity-based tier check in [`putBack`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/buffer_pool.go#L56), but it touches every get site and removes only one switch. Rejected: **Revisit if:** none: rejected on merit.

**Non-goals:** packets above 8,972; jumbo on multipath or the native endpoint route; a byte cap; changes at or below the standard size on unconfigured connections; ProbeRTT; partial loss on equal-cost paths; upstream compatibility changes ([ADR 0001](0001-upstream-compatibility.md) still governs).

## Slice graph

| Slice | Disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| [F1](#f1) | rework #1208 | One Transport-owned value at 1452, threaded to advertisement, discovery and readers | None | n/a |
| [F2](#f2) | new | Configured readers and receive queues carry 8,972-byte packets whole | F1 | n/a |
| [F3](#f3) | new | Two-stage search, effective ceiling and stage-one size reporting | F1 | n/a |
| [F4](#f4) | new | Stream frames larger than 1,452 bytes, through writer and a parser limit set by configuration | F1 | n/a |
| [F5](#f5) | new | Two configured Transports carry a stream at about 8,957-byte packets over loopback | F2, F3, F4 | n/a |
| [F6](#f6) | new | A refused send falls back to the tested size and stays down | F5 | n/a |
| [F7](#f7) | new | Transport-generated confirmation probes and the drop to the initial size | F6 | F6's unconfirmed fallback |
| [F8](#f8) | new | Far-side detection: watching, liveness, re-check | F7 | n/a |
| [F9](#f9) | new | The exported opt-in, disclosed as experimental | F8 | F1's test setter |
| [F10](#f10) | new | Linux GSO batches of more than two jumbo packets | F5 | n/a |
| [F11](#f11) | conditional | Several short DATAGRAMs in one jumbo packet, with weighted admission | F5; the consumer's comparison selecting this design | n/a |
| [F12](#f12) | new | The release that claims jumbo, at the qualified commit | The consumer's qualification; the first independent release | n/a |

F1 is the frontier. F2, F3 and F4 are parallel after F1. F10 is parallel with F6–F9. Nothing here waits for the quicquic-go identity transition before F12, and that transition doesn't wait for this.

## Implementation slices

<a id="f1"></a>
### F1 — Separate the packet ceiling from the buffer constant

**Delivers:** one unexported per-Transport value, zero meaning 1452, that decides the largest packet this endpoint advertises, searches for, or cuts a read at where a reader cuts today. It's fixed when Transport configuration seals, and each connection and newly constructed reader holds a copy from construction. A retained managed Windows decoder receives the current lease's frozen copy as an immutable per-read input under existing endpoint read serialization. No production path can set anything else. Readers differ, and each keeps its behavior at the default: the basic reader, default slot and Windows reader cut to a prefix; a read slot rejects a full slot, and slot sizing stays with F2; a managed read delivers whole ([`sys_conn_oob.go` L304](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/sys_conn_oob.go#L304)); a coalesced read delivers up to the large tier whole and only cuts above it ([`coalesced_read.go` L53-L61](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/coalesced_read.go#L53-L61)). The census at the pinned commit:

| Class | Sites | This slice |
| --- | --- | --- |
| Advertised size | `connection.go` L385, L520 | Reads the value |
| Discovery ceiling | `connection.go` L1129, L2903. L2903 repeats `peerMaxPacketSize` and can call it | Reads the value |
| Cutting reads | `sys_conn.go` L166, `sys_conn_oob.go` L245, `sys_conn_windows.go` L207, L231, `coalesced_read.go` L61 | Reads the value; no reader's default behavior changes |
| Retained managed Windows guard | `managed_packet_receive_windows.go` construction, `external_packet_io.go` configuration sealing, `managed_packet_endpoint.go` serialized `ReadFrom`, and the Windows guard above | Copy the final Transport value to its exact bound lease at sealing, then pass that copy to the retained decoder per read |
| Initial-size clip | `config.go` L104-L105 | Stays on the constant permanently |
| Buffer tiers and queue admission | `buffer_pool.go` L57, L79, L120, `coalesced_read.go` L53, `connection.go` L2265, L2291, `internal/wire/pool.go` L14, L29 | Untouched, F2 and F4 |
| Stream frame limits | `send_stream.go` L351, L477, L481, L491, L717, L721 | Untouched, F4 |
| Congestion guards | `bbr_send_policy.go` L38, `internal/congestion/bbr_sender.go` L93 | Untouched, F5 |

**Disposition / blocked by:** reworks #1208, whose earlier brief this replaces. Blocked by nothing. No formal edge to the native endpoint slices: #1206 touches none of these files, and #1202 restructures endpoint control. Rebase if they land first.

**Owner / authority:** the Transport owns the value; connections and new readers hold construction copies. Managed registration can precede final ceiling configuration, so registration does not capture the value. Successful registration establishes the exact bound lease object, whose identity is never recycled. Existing Transport configuration sealing idempotently copies the final value into that registered object under the endpoint mutex, even if the lease has since been revoked. This copy is not I/O admission, never changes the endpoint's current lease, and adds no initialization or Close error. Existing managed `begin`, `ReadFrom` and `checkLocked` remain the sole live-generation I/O admission owner and reject stale reads before decoder access. The endpoint's existing read lock serializes decoder access; its existing mutex protects the lease snapshot. Windows receives this immutable value synchronously for each read, without reconfiguring the retained decoder or changing its URO state, buffer ownership, lease revocation or handback. Ordinary parent reads, pre-start ordinary reads and a new unconfigured lease use zero/default 1452. Started and registered-but-never-started Transport cleanup preserve their existing initialization/binding errors and successful handback behavior. Nothing persists.

**Seams / blast radius:** adds a setter in a `_test.go` file, so production can't reach it, accepting up to 8972 for later slices' tests and removed by F9. F1's own tests stay at or below 1452, since no larger buffers exist yet. Constructor and reader signatures gain one input; the retained managed Windows reader receives that input per serialized read from its frozen lease. No new lock, goroutine, callback, queue or cross-context signal is admitted. No exported identifier, config field or environment variable. No state owner or concurrency model changes: new-reader copies are fixed before construction, and the existing managed read/lease locks carry the retained decoder's per-read input. Untraced: none.

**Artifacts / representation / closure:** no new shipped behavior and no new safety enforcement; tests are ordinary verification aids. Domain: client and server connections and every reader type. Owner: the Transport. Guarantee: canonical-subset, on wire-visible values, read limits and allocations, not identical timing. Closure: this slice supplies the default-value pin for [the first row](#contract-closure).

**TDD and evidence budget:** written first, with `InitialPacketSize` 1232: (1) a default-value pin: both perspectives advertise exactly 1452, discovery on a path carrying 1452 or more probes 1342, 1397, 1424, 1438 and settles at 1438, the cutting readers cut at 1452; (2) a threading proof: with a test-set 1400 at both ends, both advertise 1400, the search probes 1316, 1358, 1379 and settles at 1379, and the cutting readers cut at 1400. An uncapped peer must not hide a missing local discovery ceiling. Through managed Windows registration, set 1400 after registration, seal configuration, and show that a metadata-free 1420-byte URO read trips endpoint fallback; inspect the same retained decoder across handback and a default next lease, where ordinary/default reads use 1452. Started and registered-but-never-started Transport Close after handback must preserve existing success, successor isolation and stale-read rejection. The finite managed-reader classes are in the table below. Existing managed and coalesced delivery tests, such as `TestGROReadCopiesLargeSegmentsIntoLargeTier`, stay green. One guard mutation per owner: bypass the value at the advertisement, the discovery ceiling and a reader, and see the proof fail. Existing deterministic `AllocsPerRun` pins stay green. Capture and report raw before/after allocs/op on `BenchmarkQueueLifetime`, `BenchmarkDatagramParseAndReceive` and `BenchmarkTransfer`: deterministic queue and DATAGRAM counts must match, and the finite real-UDP transfer comparison must show no observed allocation growth, with packet/ACK workload variation and any numerical mismatch disclosed. This evidence does not claim exact transfer-count equality or general allocation preservation. A demonstrated allocation regression remains an in-scope fix or stop; no extra repetitions or timing claim are admitted. The repository's normal lanes, including Windows reader cross-compilation and existing hosted Windows runtime/race coverage of the managed guard; compilation alone does not establish runtime fallback. One review plus at most one replacement.

**Context:** at most 18k dispatch input tokens: this slice, the Decision section, the census sites, the two constructors' callers, and the bounded managed configuration-seal/lease-read path.

**Audit / stops:** further split into advertisement, discovery and readers leaves the value half-threaded with no coherent owner; rejected on merit. Merging with F2 mixes an inert refactor with new tiers; rejected on merit. Stop if any site's behavior changes at the default, threading needs an exported signature change, or a QUIC read can start before its value is settled. Retained decoder mutation at registration is rejected: final Transport configuration may come later and a decoder outlives each lease. Revisit: none, rejected on merit. Moving the ceiling's authority to the endpoint is rejected because it changes the accepted per-Transport owner. Revisit: none, rejected on merit. Adding startup/teardown modes to bypass a copy-time I/O check is rejected: copying has no admission authority, and new modes would spread lifecycle obligations and risk existing Close/initialization errors. Revisit: none, rejected on merit.

**Managed-reader semantic classes:** the existing managed endpoint owns decoder state and serialized reads; the frozen lease copy supplies only the Transport's limit. These rows bound the F1 retained-reader evidence and do not add tiers or recovery behavior.

| Class | Expected value and disposition | Enforcement owner | Evidence |
| --- | --- | --- | --- |
| Ordinary parent or pre-start ordinary read | Zero/default 1452; existing whole managed delivery | Existing serialized endpoint read, passing the default input | Existing managed handback/whole-delivery tests and a 1420-byte metadata-free read |
| Registered lease, ceiling configured after registration, configuration then sealed | Frozen 1400; metadata-free 1420 trips the inner URO guard | Transport sealing copies once to its exact registered lease object; existing read admission owns whether it may read; Windows consumes the per-read input | Managed-registration 1400 guard regression |
| Repeated sealing of the same Transport | Same frozen value; no active decoder mutation or new live-lease validation | Existing Transport copy-once seam and endpoint mutex | Focused idempotent lease-copy regression |
| Started or registered-but-never-started Transport Close after lease handback | Preserve existing successful cleanup and binding errors; historic copy cannot affect a successor; real stale reads still fail | Configuration sealing copies values only; existing managed read admission owns liveness | Common started/unstarted cleanup table, existing stale-generation/ECN-correlation regression and normal runtime/race lanes |
| Handback and next default lease using the retained decoder | Parent and next lease use 1452; old lease cannot supply its ceiling | Existing lease identity/revocation and read serialization | Same-decoder handback/reuse regression, existing revocation tests and focused race lane |

Scoped review evidence and technical disposition are linked from [PR #1233](https://github.com/the-sarge/quic-go-fast/pull/1233#issuecomment-6675429754); they do not change the product outcome or add a review round.

<a id="f2"></a>
### F2 — Carry jumbo packets through readers and receive queues

**Delivers:** a Transport set to 8972 through the test setter advertises it and reads an 8,972-byte datagram whole through every reader type: the basic reader, the default slot, Darwin read slots, the coalesced cursor, the managed reader and Windows, whose receive-coalescing guard stops tripping on a configured Transport. Those packets stay whole in the unprocessed queue past depth 256, where today [a check drops them](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L2265) and [a copy into a small buffer](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/connection.go#L2294) would panic on release. Adds the 8,972 and 8,973 tiers to `buffer_pool.go`.

**Disposition / blocked by:** new. Blocked by F1, which supplies the value the readers consult.

**Owner / authority:** the Transport's value selects the tier, and the buffer's capacity still identifies it in `putBack`, the existing borrowed invariant, recorded next to the new cases.

**Seams / blast radius:** configured Transports hold 71.8 KB of read slots per binding instead of 11.6 KB, and up to 9.2 MB per lagging connection instead of 1.5 MB. Unconfigured Transports never take a new tier. Inert in production until F9. Untraced: none.

**Artifacts / representation / closure:** shipped behavior behind a test seam. Domain: every reader type on a configured Transport. Guarantee: canonical-subset on delivered length and tier. Closure row: unconfigured Transports take no new tier.

**TDD and evidence budget:** written first: a real-socket loopback test per reader type available on the platform, sending 8,972 bytes and asserting whole delivery, plus one byte over asserting that reader's existing handling: a cut prefix, a rejected full slot, or whole delivery on the managed and coalesced readers. A queue test forcing depth past 256 with jumbo packets and asserting release doesn't panic. A pin that an unconfigured Transport takes no jumbo-tier buffer. Heap figures recorded in the PR. Hosted lanes. One review plus at most one replacement.

**Context:** at most 18k: this slice, the Decision section, `buffer_pool.go`, the reader functions and the queue admission code.

**Audit / stops:** splitting readers from queues leaves a read that can't survive a lagging run loop; rejected on merit. Merging with F5 exceeds one context. Stop if a platform can't carry 8,972 on loopback in a way that invalidates the reader contract, rather than only skipping that test.

<a id="f3"></a>
### F3 — Search in two stages and report the ceiling

**Delivers:** the finder runs stage one as today, then stage two if it topped out and the effective ceiling is above 1452. A read-only `Conn` method reports the effective ceiling once transport parameters are known, and stage one's tested size once stage one has finished. `PathMTUSettledV1` closes when the whole search is over. Finder logic is unit-tested with simulated acks and losses; no real packet above 1452 leaves until F5.

**Disposition / blocked by:** new. Blocked by F1, which supplies the local value.

**Owner / authority:** the finder owns the search and stage one's size, and the connection owns the effective ceiling.

**Seams / blast radius:** non-zero multipath paths use [`peerMaxPacketSize`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/multipath_admission.go#L544), which stays capped, because F9 refuses this option alongside native multipath I/O. Lost stage-two probes aren't congestion ([`sent_packet_handler.go` L1127](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/ackhandler/sent_packet_handler.go#L1127)). A loss-concluded stage one always narrowed its bound, so its settled path stays correct. Untraced: none.

**Artifacts / representation / closure:** shipped behavior, inert at 1452. Guarantee: example-level on the probe tables. Closure row: effective ceiling at or below 1452 means the probe sequence is today's.

**TDD and evidence budget:** written first, with `InitialPacketSize` 1232: the three path rows in the Decision table, a peer advertising 1452 running no stage two, a lost stage-one probe reporting its smaller tested size, and settled closing only after stage two. One guard mutation: remove the topped-out check and see the WireGuard row fail. One review plus at most one replacement.

**Context:** at most 12k: this slice, the Decision section, `mtu_discoverer.go` and its connection call sites.

**Audit / stops:** merging with F5 hides a small pure-logic change inside the largest slice; rejected on merit. Stop if settled's existing consumers need its timing unchanged in a way two stages can't meet.

<a id="f4"></a>
### F4 — Carry stream frames larger than 1,452 bytes

**Delivers:** a frame pool with two sizes chosen by payload, frame splitting that picks the right one ([`stream_frame.go` L183](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/wire/stream_frame.go#L183) would panic otherwise), the large-allocation branches in `send_stream.go` no longer capping a frame at 1,452 when the packet allows more, and a parser limit that comes from the connection's local ceiling. An unconfigured connection keeps today's rejection of an oversized STREAM frame ([`stream_frame.go` L66](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/wire/stream_frame.go#L66)), which a peer ignoring the advertised limit can reach today, because large datagrams already arrive whole through some readers. The limit on what `Write` copies ([`send_stream.go` L351](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/send_stream.go#L351)) stays 1,452, so `Write` timing doesn't change. In production, `maxBytes` never exceeds the standard size and the parser limit stays at the default until F9, so this is inert.

**Disposition / blocked by:** new. Blocked by F1, which supplies the value the parser limit comes from.

**Owner / authority:** the wire package's pool owns frame tiers, and the connection's local ceiling sets the parser limit.

**Seams / blast radius:** a small frame in flight still pins a standard buffer, not a jumbo one, and that's the reason for two sizes. Untraced: `Write` through the other large-frame branch; the slice traces it and records the result.

**Artifacts / representation / closure:** shipped behavior, inert. Guarantee: example-level on frame round trips. Closure row: payloads at or below 1,452 use the standard frame.

**TDD and evidence budget:** written first: pop, split and parse frames at 8,900 bytes through both `send_stream.go` branches with a configured parser limit, resend after a split, and a pin that a 100-byte frame takes the standard tier. `TestParseStreamFrameRejectsLongFrames` stays green at the default. `BenchmarkTransfer` allocs/op match. One review plus at most one replacement.

**Context:** at most 14k: this slice, F1's value, `send_stream.go`, `internal/wire/pool.go`, `stream_frame.go` and `frame_parser.go`.

**Audit / stops:** merging with F5 is rejected because frames are independently testable and F5 is already large. Stop if `Write` timing must change for frames to carry jumbo payloads.

<a id="f5"></a>
### F5 — Send at the discovered size

**Delivers:** the first end-to-end jumbo connection. Two Transports set to 8972 through the test setter reach about 8,957-byte packets over real loopback sockets, and a stream carries data intact with both Reno and BBR. Send buffers in emission and packing take the jumbo tier when the size is above 1452, including the padded probe packer ([`packet_packer.go` L945](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_packer.go#L945)). The BBR guards ([`bbr_send_policy.go` L38](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/bbr_send_policy.go#L38), [`bbr_sender.go` L93](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/congestion/bbr_sender.go#L93)) check the connection's own ceiling, so an unconfigured connection keeps today's 1,452 guard.

**Disposition / blocked by:** new. Blocked by F2 (the receiver must hold them), F3 (the size must rise) and F4 (frames must fit).

**Owner / authority:** the run loop's size, as today. Emission asks one helper for a buffer of the right tier.

**Seams / blast radius:** BBR's maximum window scales from 14.5 MB to 89.6 MB, reached only at a very large bandwidth-delay product. Reno and BBR state scale with size. DATAGRAMs at or below today's size are unaffected. Linux GSO stays at two packets until F10. Inert in production until F9. Untraced: none expected; the slice records any it finds.

**Artifacts / representation / closure:** shipped behavior behind the test seam. Guarantee: example-level on loopback. Closure row: size at or below 1452 takes today's buffers.

**TDD and evidence budget:** written first: the loopback stream test, skipped only where the platform's loopback can't carry 8,972, asserting size above 8,900 and data intact, under BBR and Reno. A DATAGRAM of 1,400 bytes delivered at jumbo size. A peer capped at 1452 stays capped, including through a path reset: the effective ceiling stays capped, discovery is unchanged and recovery stays off. F2's no-jumbo-tier pin also observes representative unconfigured sends and padded discovery probes, so it catches a wrong default in this slice's buffer helper. One review plus at most one replacement.

**Context:** at most 24k, by function rather than whole file: this slice, the Decision section, F2–F4's merged interfaces, the buffer sites listed by `getPacketBuffer` in `packet_emission*.go` and `packet_packer.go`, and the two guards.

**Audit / stops:** further split of send buffers from guards can't run end to end; rejected on merit. Stop if the loopback test can't reach above 8,900, or data corrupts.

<a id="f6"></a>
### F6 — Fall back when a send is refused

**Delivers:** step 4's size change through its simplest trigger. On a jumbo-active connection, a kernel refusal of a non-probe packet above 1452 makes the send worker report a fact with its path generation and segment size. The run loop falls back to stage one's size, resends in-flight data split to fit, drops queued DATAGRAMs that no longer fit as permitted loss, replaces congestion state, stays down including after a later path change, and signals the drop read-only. Every send handoff stamps its path generation, Reno as well as BBR. Confirmation here is an ACK of any packet at the new size; F7 adds the transport's own probes.

**Disposition / blocked by:** new. Blocked by F5: there's no size above 1452 to fall from until then.

**Owner / authority:** the run loop owns the size change. The send worker carries the fact, not policy. Delivery behaviors: late or stale facts are discarded by path generation; duplicates after the first change nothing; a fact arriving after shutdown is dropped by the existing queue lifetime; a GSO batch compares its segment size, not its total.

**Seams / blast radius:** an unconfirmed fallback has no drop to the initial size until F7; coherent because nothing can configure jumbo before F9. `PathMTUSettledV1`'s [promise that the limit stays the same](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/path_mtu_settled.go#L36) gets a documented exception. Throughput restarts from congestion startup, a brief dip for a rare event. Reno's [panic on a smaller size](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/congestion/cubic_sender.go#L384) is avoided by replacing the sender. Any errno but a size refusal is fatal on a segmented send today ([`send_queue.go` L255](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/send_queue.go#L255)); some Linux kernels may return EINVAL for an oversized segment (unverified), and the slice traces that.

**Artifacts / representation / closure:** required safety enforcement. Domain: jumbo-active connections with a local refusal. Guarantee: example-level. Closure: [third row](#contract-closure).

**TDD and evidence budget:** written first: a characterization of today's stall with 15 s keepalives and a congestion-limited sender, which settles the idle-timeout question and is then inverted. A fake packet conn that starts refusing above 1,500: the stream finishes intact under Reno and BBR, size settles at stage one's. Rows for a segmented send, an external batch writer, a refused discovery probe causing no fallback, a connection that never passed 1452 ignoring the refusal as today, and a path change after a fallback staying down. One guard mutation: remove the fallback call and see the stream test fail. One review plus at most one replacement.

**Context:** at most 24k: this slice, the Decision section, F5's interfaces, the migration reset as a model, `send_queue.go` and the handoff sites.

**Audit / stops:** merging with F7 or F8 exceeds one context, since each adds its own send or detection path; rejected on merit. Stop if in-flight data can't be resent at a smaller size on a route, or if the routine needs a second owner of the size.

<a id="f7"></a>
### F7 — Confirm the fallback with the transport's own probes

**Delivers:** the recovery-probe machinery and the rest of steps 4 and 5. A run-loop deadline per probe, a send path ahead of the congestion check, the follow-up PING, and the lost verdict in the Decision section. After a fallback, up to three probes padded to the size under test confirm it, so a congestion-limited or DATAGRAM-only connection can confirm with no return traffic. Three lost probes drop to the initial size and repeat. A confirmed initial size runs stage one again without stage two. Three more lost stops probing.

**Disposition / blocked by:** new. Blocked by F6, whose fallback it confirms.

**Owner / authority:** the run loop owns the deadlines and the verdict. Probes are built by the padded probe packer, and their loss isn't reported to congestion control, while other lost packets still are.

**Seams / blast radius:** removes F6's unconfirmed fallback. Adds one emission intent reached only during recovery, so ordinary loss timeouts and discovery probes don't change. A probe's handler is guarded by path generation, because [a path change declares every probe lost](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/internal/ackhandler/sent_packet_handler.go#L1544). Untraced: none expected.

**Artifacts / representation / closure:** required safety enforcement. Guarantee: example-level. Closure: [third row](#contract-closure).

**TDD and evidence budget:** written first: a DATAGRAM-only connection with no other traffic confirming its fallback. A probe reaching a lost verdict with no return traffic other than the follow-up PING's ACK. A congestion-limited sender getting its probe out. A fake that refuses above 1,300 too, reaching the initial size and running stage one again without stage two. All probes lost stops probing with the connection's normal rules still in force. A path change during recovery causes no fallback. One guard mutation: remove the deadline and see the DATAGRAM-only test stall. One review plus at most one replacement.

**Context:** at most 18k: this slice, the Decision section, F6's routine, the emission intent code and the padded probe packer.

**Audit / stops:** merging with F6 rejected above. Stop if a probe can't leave a congestion-limited connection without changing ordinary loss-timeout behavior.

<a id="f8"></a>
### F8 — Detect a far-side shrink

**Delivers:** steps 1, 2 and 3. Losses above 1452 are counted from both loss detectors, excluding discovery probes. The window, liveness check and re-check at the current size trigger F6's routine, using F7's probes. This covers a peer whose interface shrank, a hop that started dropping, and a consumer whose socket layer absorbs the kernel's refusal, including a connection sending only jumbo DATAGRAMs.

**Disposition / blocked by:** new. Blocked by F7, whose probes it uses and which a detected shrink needs to confirm with no small traffic.

**Owner / authority:** the ack handler reports loss facts to the run loop, which decides. Both run in the connection's execution context, so no hand-off is added.

**Seams / blast radius:** could cost a brief dip on a false trigger, and a false fallback is permanent for that connection, which is why step 3 exists. A connection that isn't sending has nothing to detect. A path that drops only some big packets reads as loss, a declared non-goal. The window's exact value is a design value until these tests pin it.

**Artifacts / representation / closure:** required safety enforcement. Guarantee: example-level. Closure: [third row](#contract-closure).

**TDD and evidence budget:** written first, through a proxy with its buffers raised to carry jumbo. A proxy that starts dropping above 1,500, with each test first asserting the size is above 8,000 so it can't pass by never arming: with no return traffic, with steady small return traffic, and with only jumbo DATAGRAMs. A proxy limited to 1,500 from the start, where lost discovery probes cause no fallback and the size settles near 1,470. No size change under 5% random loss, reordering, a full outage longer than the window followed by a restore, or a second outage right after the first small ACK. Time to recovery is reported against the 0.3 s target, and isn't a pass criterion. One guard mutation: count only timeouts and see the return-traffic test fail. One review plus at most one replacement.

**Context:** at most 20k: this slice, the Decision section, F6's and F7's routines, `detectLostPackets`, `QueueProbePacketAt` and the proxy. `QueueProbePacketAt` is the only detector that runs without return traffic and already excludes discovery probes; `detectLostPackets` needs the filter this slice adds.

**Audit / stops:** further split of counting from liveness leaves no testable behavior; rejected on merit. Stop if false triggers can't be avoided without leaving a stall longer than the idle timeout, or a fallback can't be confirmed with no small traffic.

<a id="f9"></a>
### F9 — Export the opt-in

**Delivers:** `Transport.ConfigurePacketCeilingV1(n)`, in the existing `...V1` style, before start, accepting 1452 to 8972, refusing other values and any Transport configured for native multipath I/O in either order ([`nativeIOConflict`](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/external_packet_io.go#L190) gains this case). Removes F1's test setter. Discloses the method as experimental in `CHANGELOG.md` and its doc comment. Applies the Windows gate. A public-API loopback test runs the full path including a shrink.

**Disposition / blocked by:** new. Blocked by F8: the method must not exist in a release without recovery.

**Owner / authority:** the Transport. Validated once, before start.

**Seams / blast radius:** removes the test seam. Additive API only, per [ADR 0001](0001-upstream-compatibility.md). Peers without it advertise 1452 and cap the sender.

**Artifacts / representation / closure:** shipped behavior and the closure invariants' public entry. Guarantee: canonical-subset. Closure: [all rows](#contract-closure).

**TDD and evidence budget:** written first: refusal table (1451, 8973, after start, multipath in both orders), public-API loopback run, default pin rerun through the public surface. One review plus at most one replacement.

**Context:** at most 12k: this slice, F1's setter, `nativeIOConflict` and `CHANGELOG.md`.

**Audit / stops:** merging with F8 is rejected so the export is one reviewable decision. Stop if a release is about to claim jumbo from this slice alone: experimental support isn't the claim.

<a id="f10"></a>
### F10 — Batch jumbo packets on Linux GSO

**Delivers:** on Linux, a connection whose current packet size is above 1452 uses the existing 64 KiB tier and byte budget for segmented sends instead of the [20 KiB limit](https://github.com/the-sarge/quic-go-fast/blob/e243f863dd/packet_emission_bbr.go#L154), so a batch holds 7 jumbo packets instead of 2. A configured connection still sending at the standard size keeps today's batches.

**Disposition / blocked by:** new. Blocked by F5.

**Owner / authority:** emission's buffer helper.

**Seams / blast radius:** Linux send path at sizes above 1452 only, under both the Reno batch path and the BBR budget. Software GSO behavior on specific NICs isn't traced here.

**Artifacts / representation / closure:** shipped behavior. Guarantee: example-level. Closure: not triggered, because failure costs throughput on one platform only.

**TDD and evidence budget:** written first: a Linux test on loopback or a throwaway network namespace asserting more than two segments in a batch and data intact, under Reno and BBR. Batches unchanged on an unconfigured connection, on a configured one whose peer advertises 1452, and on an active one still at the standard size. One review plus at most one replacement.

**Context:** at most 10k: this slice and `packet_emission_gso.go`.

**Audit / stops:** kept apart from F5 because it isn't needed for correctness. Stop if the namespace can't be created in CI; the test then runs where it can, and the slice reports that.

<a id="f11"></a>
### F11 — Pack several short DATAGRAMs into a jumbo packet (conditional)

**Delivers:** when the size is above 1452, the packer fills a packet with queued DATAGRAM frames, leaving room so streams aren't starved, and receive admission is weighted by DATAGRAMs per packet so a discard stays visible to the sender. MTU 1500 packing doesn't change.

**Disposition / blocked by:** conditional. Blocked by F5 and by the consumer's comparison selecting this design. There's no cycle: the comparison runs a throwaway patch the consumer owns, packing without weighted admission, built on a fork commit that contains F5. This slice is the real implementation, and qualification runs on the commit that contains it. It closes with no code if the comparison doesn't select it, because the consumer's rule tries this design first whenever it passes.

**Owner / authority:** the packer owns packing, and connection admission owns the weighting.

**Seams / blast radius:** per-DATAGRAM work at about 910k/s is unmeasured until the consumer's comparison. Receive queues hold more entries per packet.

**Artifacts / representation / closure:** shipped behavior. Guarantee: example-level. Closure row: size at or below 1452 packs one frame per packet as today.

**TDD and evidence budget:** written first: eight 1,099-byte DATAGRAMs in one 8,957-byte packet, a stream frame still making progress under a full DATAGRAM queue, and a lagging reader whose drops are counted before acknowledgment. One review plus at most one replacement.

**Context:** at most 16k: this slice, `composeNextPacket`, `AppendDatagramPacket`, and admission.

**Audit / stops:** stays one slice because packing without weighted admission brings back invisible discards. Stop if the comparison's data shows weighting costs more than the design can carry.

<a id="f12"></a>
### F12 — Publish the release that claims jumbo

**Delivers:** a release, whichever one after `quicquic-go v0.63.0` is ready, tagged at the exact fork commit the consumer's qualification ran on, with notes that claim only what it qualified: Thunderbolt at MTU 9000, with Linux and Windows as tested on loopback or build only.

**Disposition / blocked by:** new. Blocked by the consumer's qualification and by that first independent release. An earlier release carrying the experimental method is separate and claims nothing.

**Owner / authority:** the [release runbook](../runbooks/release.md), which allows any `main` commit that's an ancestor of the default branch.

**Seams / blast radius:** none beyond release rules. Owner approval as usual.

**Artifacts / representation / closure:** process metadata plus the release itself. Closure: not triggered.

**TDD and evidence budget:** the runbook's gates, plus proof the tag peels to the qualified commit.

**Context:** at most 8k: this slice and the runbook.

**Audit / stops:** stop if the release can't be tagged at the qualified commit. Then the pinned commits need requalifying, or the owner decides.

## Contract closure

Triggered for three invariants, each with a material consequence and several independent paths.

| Invariant | Enforcement owner | Classes | Disposition | Evidence |
| --- | --- | --- | --- | --- |
| An unconfigured Transport behaves as today | The Transport's value, default 1452 | Client, server, each reader type; sends and padded probes; STREAM parsing | No new tier, advertisement or probe; oversized STREAM frames still rejected | F1 pin and threading proof; F2 tier pin, also covering F5's sends; F4 parser pin; F9 pin through the public surface |
| A configured connection that isn't jumbo-active sends and fails as today | The connection's effective ceiling | Peer advertises 1452; a size refusal; a path change | Advertises the larger size and pays read buffers; no stage two; no recovery | F3 peer row; F5 peer-capped test, including a path reset; F6 never-above-1452 row; F10 peer-capped batches |
| A jumbo-active connection whose path shrinks continues at a confirmed smaller size or reaches the initial size, and never wedges silently | The run loop's recovery routine | Local refusal; far-side drop with and without return traffic; DATAGRAM-only; a failing fallback; a long outage with restore | Falls back, or no change on an outage | F6, F7 and F8 lists, then the consumer's real-link shrink |

Partial loss on equal-cost paths is a declared non-goal. Guarantees are canonical-subset on wire-visible values and buffer tier for the first two, and example-level for the third.

## Acceptance criteria

- [ ] An unconfigured Transport advertises 1452, probes as today and takes no jumbo-tier buffer. Domain, owner and guarantee: first closure row.
- [ ] Two configured Transports reach about 8,957 on a jumbo path and carry a stream intact.
- [ ] A shrink is survived per the third closure row, with no silent wedge in the tested classes.
- [ ] The method exists only together with recovery, and is refused with native multipath I/O in either order.

## Validation gates

Each slice's focused tests, affected-package tests, `go vet` on changed packages, `go mod tidy -diff`, and the existing hosted workflows on the exact head, per the [repository overlay](../REVIEW-LOOP.md).

## Operating discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern every slice, composed with [the repository overlay](../REVIEW-LOOP.md). Weigh any replacement design in a scoped re-audit with the shared design-cost reference. Stop for a decision if a slice would change behavior at the default.
