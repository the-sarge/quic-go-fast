# Handshake silent-drop policy comparison: issue #759

**Result: P1 and P2 recover every size-drop row on both endpoints; the receipt guard recovers only client-direction drops. Recommended for the owner's decision: P2, with eligibility ending at handshake completion.**

- **Recovery.** P1 and P2 recover client-direction, server-direction and both-direction drops, and the real-UDP case; P1 also recovers under BBRv3. The guarded P1 recovers only the client-direction drop. Under a server-direction drop it times out at 10 s like the baseline, because a server has always processed the client's Initial before its first PTO.
- **Delay.** P1 completes the three simnet size-drop rows at 220, 220 and 420 ms; P2 at 620, 1020 and 1620 ms. The server-direction cost of P2 is larger than one round. Its second server PTO is a Handshake PTO, whose probes cannot carry the ServerHello the client needs, so recovery waits for the next Initial PTO at 1010 ms.
- **False positives.** No reduction changed any control's completion or confirmation time; the cost is the 1200-byte size the connection keeps afterwards. Every candidate reduces under two lost rounds and total loss. P1 also reduces under one lost round, loss after receipt and the lossless 300 ms path (on both endpoints). P2 also reduces on the lossless 300 ms path, at 500 ms, after completion. The guarded P1 also reduces under one lost round and on the lossless 300 ms path.
- **Collateral.** Every candidate reduces the client in the server-direction row, where the client's direction was never limited. The guard does not prevent it: the client's first PTO (200 ms) comes before the 41-byte server ACK it processes at 220 ms.
- **Phase boundary.** Only two reductions came after completion, both unnecessary: P1's at 80 ms (loss after receipt) and P2's at 500 ms (lossless 300 ms). P1's call at 500 ms on the 300 ms path was a third post-completion trigger, a no-op because it had reduced at 200 ms. No necessary reduction came after completion, and on an unchanged path none can.
- **Preservation.** Under every candidate, explicit 1200 and 1280, explicit-size local message-size errors and unset native-error recovery behave exactly as the baseline, loss-timer call for loss-timer call.
- **Controls.** The baseline variant reproduces #758's run1 exactly, the mapping evidence is identical to #758's, a repeat run matches with one explained packetization difference, and the race-detector run is clean.

No product code, active test or CI configuration changed. The variants exist only in [fixture/silent_drop_policy_test.go.src](fixture/silent_drop_policy_test.go.src), which [reproduce.sh](reproduce.sh) copies into place beside #758's unchanged capture fixture, runs and removes. Nothing here is a public switch or a production dependency.

## Authorization and provenance

- **Scope.** [#759](https://github.com/the-sarge/quic-go-fast/issues/759) compares three disposable fallback policies and recommends a production contract. The owner approved this execution plan on 2026-10-17. GitHub is authoritative for the acceptance criteria. No policy is chosen here and no production change is authorized; the owner decides after reading this record, and an implementation ticket follows only an accepted decision.
- **Baseline.** #758's baseline `a7b8353ac2e87e716fcfa23119959ff5aeaef648` on `main`, which contains the #756 merge `81a72481` and the #762 discovery-gating fix `4d89a66e`. The runs used a checkout at `43e0f2fd6fbb6bde82415097f4f9ec1c2b59ab10` (`main`, after #758's record and journal), whose tree outside `docs/` equals the baseline; see the [identity receipt](raw/E0-identity.log).
- **Toolchain.** go1.27.2 darwin/arm64 with `GOTOOLCHAIN=local`, matching CI's pin and #758.
- **Workspace.** Worktree `/Volumes/worktrees/quic-go-fast/silent-drop-policy-compare`, branch `codex/silent-drop-policy-compare`; the initiating checkout was not modified.
- **Fixture identity.** #758's [capture fixture](../2026-10-17-handshake-silent-drop-baseline/fixture/silent_drop_capture_test.go.src) (observer, recorder, router) and [mapping test](../2026-10-17-handshake-silent-drop-baseline/fixture/silent_drop_mapping_test.go.src) run byte for byte; `reproduce.sh` refuses to run if either differs from #758's `SHA256SUMS`. This record's policy file replaces #758's scenario file; [fixture/policy-vs-baseline.diff](fixture/policy-vs-baseline.diff) is the complete difference. Each run's `receipt.txt` holds the hashes of all three files as run.
- **Host.** mbp128 also sends for wiremux's `tb` measurement cell. Every CPU-heavy step here waited until no wiremux campaign was running on it.

## Candidates and the trigger

| Variant | Experimental trigger |
| --- | --- |
| baseline | none; the control, which must reproduce #758 |
| P1 | the endpoint's first eligible PTO |
| P2 | the endpoint's second eligible PTO |
| P1 + receipt guard | P1, only if the connection has processed no long-header peer packet (`receivedFirstPacket` false) |

**Eligibility and counting are #758's, unchanged, in every variant.** #758's observer classifies each loss-timer call from in-call qlog events and `SendMode` before and after. An eligible expiration is class `pto` or `pto_silent`, in the Initial or Handshake space, before confirmation. The count is cumulative per attempt and path epoch. ACK progress and key discards do not reset it; Retry and a path-generation change start a new epoch. Emitted probes, stale calls and no-op calls are never counted. Resetting the count never restores a size. Receipt is #758's: the first successfully unpacked long-header peer packet processed by the current connection. Every variant acts on every endpoint, so server coverage comes from the comparison itself.

**Where the trigger runs.** The observer calls its eligible-PTO hook after recovery has handled the expiration and queued its probes, on the endpoint's connection goroutine, inside the connection's loss-timer call ([connection.go:731-735](../../../connection.go)). The variant decides there, from the eligible count and the receipt flag, and calls `lowerUnsetInitialPacketSize` ([connection.go:2616-2629](../../../connection.go)). That function remains the sole owner of the reduction for every trigger, and keeps its guards: unset size only, before confirmation, above 1200. The variant adds no state to the connection and calls nothing else.

| Step after a trigger | Source | What happens |
| --- | --- | --- |
| Connection-owned size | `lowerUnsetInitialPacketSize` | `effectiveInitialPacketSize` becomes 1200 |
| Finder | same | if a finder exists (the server once it has the client's transport parameters; the client after completion), a fresh one starts at 1200 with the old ceiling; no probe exists before confirmation |
| Datagram admission | same | `maxPayloadSizeEstimate` drops to the 1200-byte payload estimate, so `SendDatagram` refuses payloads that no longer fit |
| Packet budget | `maxPacketSize` ([connection.go:2730-2744](../../../connection.go)) | the client without a finder returns the effective size; with a finder, its current size; a server before transport parameters already uses 1200 |
| Capacity gate | run loop ([connection.go:767-774](../../../connection.go)) | runs after the timer call. A full queue defers packing; the size is already lower when packing resumes |
| H1's feedback | `send` and `sendBounded` ([packet_emission.go:118-128](../../../packet_emission.go), [packet_emission_bbr.go:74-76](../../../packet_emission_bbr.go)) | consumed where it always was, after the capacity gate and before dispatch; its order is unchanged. If both fire, the second call to the owner is a no-op |
| Reno/Cubic admission | `SendMode` and the congestion window | unchanged. The owner calls no congestion setter, so congestion accounting keeps 1280: H1's monotone rule |
| BBR admission | `sendBounded` ([packet_emission_bbr.go:77-87](../../../packet_emission_bbr.go)) | reads `maxPacketSize` on every send, refreshes BBR's datagram size and quantum, and reserves that size. Already-reserved and pending bytes keep their historical sizes |
| Packing | `sendProbePacket` ([packet_emission.go:419-465](../../../packet_emission.go)) | the expiration's probes are packed with the new budget |
| Registration | `sendCoalesced` ([packet_emission.go:478-521](../../../packet_emission.go)) | each packet registers with recovery at its packed length |
| Handoff | `handoff` ([packet_emission.go:522](../../../packet_emission.go)) | the registered datagram goes to the send queue; the asynchronous worker writes it ([send_queue.go](../../../send_queue.go)). Registration stays before asynchronous I/O |

**Rejected placements.** Patching `connection.go` on a throwaway branch would behave the same but make a product diff that must never merge, and it would defeat the tree-identity check. Posting into H1's message-size mailbox would route a same-goroutine signal through a cross-goroutine mailbox, inherit its coalescing and stale-generation rules, and defer the admission update to the next send. Both were rejected on merit.

## Fixture and run matrix

The fixture manifest is #758's: simnet in `testing/synctest` virtual time, a 1452-byte limit on every link so only the selected filter drops, a filter that drops datagrams over 1250 bytes, 10 ms one way (150 ms for the high-delay row), default timeouts, congestion control and discovery, the `internal/testdata` certificate, a 30 s Dial deadline, a 4096-byte echo in each direction after success, and joined cleanup. The real-UDP case uses loopback sockets and the existing proxy in wall-clock time.

| Group | Rows | Variants | Captures per run |
| --- | --- | --- | --- |
| Comparison | #758's nine rows, with their schedules and release rules unchanged | all four | 36 |
| Real UDP | client datagrams over 1250 bytes dropped by the proxy | all four | 4 |
| Preservation | explicit 1280: client drop, server drop, one lost round; explicit 1200: one lost round, total loss; explicit 1280 with local message-size errors; unset with local message-size errors | all four | 28 |
| BBR | client drop with BBRv3 on both endpoints | baseline, P1 | 2 |

- **Explicit sizes** set `InitialPacketSize` on both endpoints.
- **Local message-size errors** come from a wrapper on the client's simnet socket that rejects writes over 1250 bytes with the native error, as #779's integration test does on real sockets. `TestSilentDropMsgSizeConnFidelity` shows the wrapper has exactly the simnet socket's method set, so every interface the transport asserts resolves the same way.
- **BBR** is one focused row: the trigger reaches BBR admission through the same `maxPacketSize` read for every candidate, so P2 and the guard would differ only in when it fires.

## Results

Times are virtual milliseconds from the start of the dial; the real-UDP row is wall-clock. "Dial" is when `Dial` returned. Every successful case verified the 4096-byte echo in each direction, and every case recorded joined cleanup. No loss-timer call was indeterminate, no server send reached three times the client bytes delivered to it, and no endpoint sent a datagram over 1250 bytes after its reduction. The [summary](summary.json) lists every check; `failed_checks` is empty.

**Baseline control.** The baseline variant's nine rows and real-UDP case match #758's archived run1 on the same projections (real UDP without timestamps). The mapping test's 32 records (17 cases) match #758's on its own comparison key, with no rule violation. So the new scenario file reproduces #758's runs, and #758's limits and stop conditions carry forward unchanged.

**Outcomes:**

| Scenario | baseline | p1 | p2 | p1-guarded |
| --- | --- | --- | --- | --- |
| client-oversized-dropped | idle timeout 5000 | success, Dial 220 | success, Dial 620 | success, Dial 220 |
| server-oversized-dropped | handshake timeout 10000 | success, Dial 220 | success, Dial 1020 | handshake timeout 10000 |
| both-oversized-dropped | idle timeout 5000 | success, Dial 420 | success, Dial 1620 | handshake timeout 10000 |
| loss-after-client-receipt | success, Dial 20 | success, Dial 20 | success, Dial 20 | success, Dial 20 |
| healthy | success, Dial 20 | success, Dial 20 | success, Dial 20 | success, Dial 20 |
| lossless-high-delay | success, Dial 300 | success, Dial 300 | success, Dial 300 | success, Dial 300 |
| one-lost-round | success, Dial 220 | success, Dial 220 | success, Dial 220 | success, Dial 220 |
| two-lost-rounds | success, Dial 620 | success, Dial 620 | success, Dial 620 | success, Dial 620 |
| total-loss | idle timeout 5000 | idle timeout 5000 | idle timeout 5000 | idle timeout 5000 |
| real-udp-client-oversized-dropped | idle timeout 5003 | success, Dial 209.482 | success, Dial 610.663 | success, Dial 204.648 |

**Client reductions.** Each cell gives the time, the eligible count that triggered, the phase, and whether the reduction was needed. "Necessary" means the endpoint's own direction was size-limited, "collateral" means the row limited only the other direction, and "unnecessary" means the row had no size limit at all.

| Scenario | p1 | p2 | p1-guarded |
| --- | --- | --- | --- |
| client-oversized-dropped | 200 (#1, before receipt; necessary) | 600 (#2, before receipt; necessary) | 200 (#1, before receipt; necessary) |
| server-oversized-dropped | 200 (#1, before receipt; collateral) | 340 (#2, after receipt, before completion; collateral) | 200 (#1, before receipt; collateral) |
| both-oversized-dropped | 200 (#1, before receipt; necessary) | 600 (#2, before receipt; necessary) | 200 (#1, before receipt; necessary) |
| loss-after-client-receipt | 80 (#1, after completion, before confirmation; unnecessary) | none (1 below threshold) | none (guard abstained 1×) |
| healthy | none | none | none |
| lossless-high-delay | 200 (#1, before receipt; unnecessary) | 500 (#2, after completion, before confirmation; unnecessary) | 200 (#1, before receipt; unnecessary) |
| one-lost-round | 200 (#1, before receipt; unnecessary) | none (1 below threshold) | 200 (#1, before receipt; unnecessary) |
| two-lost-rounds | 200 (#1, before receipt; unnecessary) | 600 (#2, before receipt; unnecessary) | 200 (#1, before receipt; unnecessary) |
| total-loss | 200 (#1, before receipt; unnecessary) | 600 (#2, before receipt; unnecessary) | 200 (#1, before receipt; unnecessary) |
| real-udp-client-oversized-dropped | 203.391 (#1, before receipt; necessary) | 604.709 (#2, before receipt; necessary) | 201.846 (#1, before receipt; necessary) |

**Server reductions:**

| Scenario | p1 | p2 | p1-guarded |
| --- | --- | --- | --- |
| client-oversized-dropped | none | none | none |
| server-oversized-dropped | 210 (#1, after receipt, before completion; necessary) | 410 (#2, after receipt, before completion; necessary) | none (guard abstained 6×) |
| both-oversized-dropped | 410 (#1, after receipt, before completion; necessary) | 1010 (#2, after receipt, before completion; necessary) | none (guard abstained 6×) |
| loss-after-client-receipt | none | none | none |
| healthy | none | none | none |
| lossless-high-delay | 350 (#1, after receipt, before completion; unnecessary) | none (1 below threshold) | none (guard abstained 1×) |
| one-lost-round | none | none | none |
| two-lost-rounds | none | none | none |
| total-loss | no connection | no connection | no connection |
| real-udp-client-oversized-dropped | none | none | none |

**Effective initial size at completion / confirmation** (C client, S server; – where the endpoint never reached that point):

| Scenario | baseline | p1 | p2 | p1-guarded |
| --- | --- | --- | --- | --- |
| client-oversized-dropped | C –/– | C 1200/1200, S 1280/1280 | C 1200/1200, S 1280/1280 | C 1200/1200, S 1280/1280 |
| server-oversized-dropped | C –/–, S –/– | C 1200/1200, S 1200/1200 | C 1200/1200, S 1200/1200 | C –/–, S –/– |
| both-oversized-dropped | C –/– | C 1200/1200, S 1200/1200 | C 1200/1200, S 1200/1200 | C –/–, S –/– |
| loss-after-client-receipt | C 1280/1280, S 1280/1280 | C 1280/1200, S 1280/1280 | C 1280/1280, S 1280/1280 | C 1280/1280, S 1280/1280 |
| healthy | C 1280/1280, S 1280/1280 | C 1280/1280, S 1280/1280 | C 1280/1280, S 1280/1280 | C 1280/1280, S 1280/1280 |
| lossless-high-delay | C 1280/1280, S 1280/1280 | C 1200/1200, S 1200/1200 | C 1280/1200, S 1280/1280 | C 1200/1200, S 1280/1280 |
| one-lost-round | C 1280/1280, S 1280/1280 | C 1200/1200, S 1280/1280 | C 1280/1280, S 1280/1280 | C 1200/1200, S 1280/1280 |
| two-lost-rounds | C 1280/1280, S 1280/1280 | C 1200/1200, S 1280/1280 | C 1200/1200, S 1280/1280 | C 1200/1200, S 1280/1280 |
| total-loss | C –/– | C –/– | C –/– | C –/– |
| real-udp-client-oversized-dropped | C –/– | C 1200/1200, S 1280/1280 | C 1200/1200, S 1280/1280 | C 1200/1200, S 1280/1280 |

**Per row:**

- **Client oversized dropped.** P1 and the guarded P1 reduce at the client's first PTO (200 ms), before any receipt, and its probes pass at 1200 bytes. P2 waits for the second PTO (600 ms). The server never needs to reduce.
- **Server oversized dropped.**
  - P1 reduces the server at its first PTO (210 ms, Initial). Its probes carry the ServerHello at 1200 bytes, and the client completes at 220 ms.
  - P2's second server PTO is a Handshake PTO (410 ms). Its 1200-byte probes reach the client, but the client cannot decrypt Handshake packets without the ServerHello. That comes only with the next Initial PTO at 1010 ms, so Dial returns at 1020 ms.
  - The guarded P1 abstains at all six server PTOs, and both endpoints time out at 10 s, as at baseline.
  - All three reduce the client too. P1 and the guard do it at 200 ms, before the client's receipt at 220 ms; P2 does it at 340 ms, on a silent one-probe PTO after receipt.
- **Both directions oversized dropped.** The client's reduction lets its Initial reach the server, so the server connection only starts then. P1 reduces the server at its first PTO (410 ms); P2 at its second (1010 ms). The guarded P1 recovers the client's direction and then times out at 10 s.
- **Loss after client receipt.** The client's only eligible PTO is a Handshake PTO at 80 ms, after completion (20 ms). P1 reduces there; the client keeps 1200 after confirmation at 100 ms. P2 is below threshold and the guard abstains. Timing is identical in every variant.
- **Healthy.** No eligible PTO; nothing happens.
- **Lossless high delay (300 ms RTT).**
  - The client's first PTO (200 ms) precedes its receipt (300 ms), so P1 and the guard reduce the client there.
  - The server's only PTO (350 ms) comes after its receipt, so P1 reduces the server and the guard abstains.
  - P2 reduces the client at its second PTO, the spurious post-completion Handshake PTO at 500 ms, and leaves the server at 1280.
  - Completion (300 ms) and confirmation (600 ms) are unchanged in every variant.
- **One and two lost rounds.** The release restores delivery inside the client's first or second eligible timer call, exactly as at baseline. With one lost round, P1 and the guard reduce at 200 ms, and P2 stays below threshold. With two, all three reduce, P2 at 600 ms. Timing is unchanged.
- **Total loss.** Every candidate reduces, at 200 ms (P1, guard) or 600 ms (P2). No server connection exists, and the client times out at 5 s as at baseline.
- **Real UDP.** P1 and the guard recover in about 205–210 ms and P2 in about 611 ms. Their timer calls fall within 5 ms of the simnet row's. The baseline times out at 5003 ms.

**BBRv3.** P1's trigger at 200 ms lowers the packet budget that bounded emission reads. The client's next twelve datagrams are at most 1200 bytes, and Dial returns at 224 ms. BBR's diagnostic trace fires at most once a second, so it does not show BBR's internal datagram size after the trigger; the source trace above covers that step.

| Variant | Outcome | Client reduction |
| --- | --- | --- |
| baseline | idle timeout 5000 | none |
| p1 | success, Dial 224.372 | 200 (#1, before receipt; necessary) |

**Eligible rounds.** P1 reduces at each limited endpoint's first eligible PTO and P2 at its second. In the both-direction row the two reductions are sequential, because the server connection exists only once the client's reduced Initial arrives. Before completing, the client takes this many eligible PTOs:

- client-direction row: 1 under P1, 2 under P2;
- server-direction row: 1 under P1, 4 under P2;
- both-direction row: 1 under P1, 3 under P2.

The baseline client takes 4, 8 and 4 before it times out.

**Repeat run.** [run2](raw/run2.tar.gz) matches [run1](raw/run1.tar.gz) on #758's projections in all 70 cells: every loss-timer call (time, class, level and count), every receipt and every phase. The datagrams carrying Initial or Handshake packets also match, with one exception. At P2's server trigger in the server-direction row (410 ms), the server's Handshake probes were two 1197-byte datagrams in run1, and 1200, 1200 and 45 bytes in run2. The certificate flight's length varies by a few bytes between runs, as #758 recorded, and after the reduction run2's flight needed a third datagram. The difference is pinned exactly in `summarize.py`; any other difference fails the summary.

## Preservation

Each candidate's run is compared with the baseline variant's run of the same row, in the same capture: every loss-timer call with its virtual time, every handshake datagram decision, every receipt and every phase.

| Row | Baseline outcome | p1 | p2 | p1-guarded |
| --- | --- | --- | --- | --- |
| explicit-1280-client-oversized-dropped | idle timeout 5000 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |
| explicit-1280-server-oversized-dropped | handshake timeout 10000 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |
| explicit-1280-one-lost-round | success, Dial 220 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |
| explicit-1200-one-lost-round | success, Dial 220 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |
| explicit-1200-total-loss | idle timeout 5000 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |
| explicit-1280-client-local-msgsize | idle timeout 5000 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |
| unset-client-local-msgsize | success, Dial 220 | same as baseline; no reduction | same as baseline; no reduction | same as baseline; no reduction |

- **Explicit sizes are never lowered.** Each explicit row ran its candidate's trigger: P1 and the guard at the first eligible PTO, and P2 at the second wherever one came. The size owner's unset guard made every call a no-op. Explicit 1280 with a client-direction or server-direction drop keeps the documented timeout (5 s idle, or a 10 s handshake timeout) with no early close. Ordinary loss recovers at the first PTO at both sizes, and total loss at 1200 times out at 5 s.
- **Explicit-size local message-size errors keep #756's behavior.** The client's writes over 1250 bytes fail with the native error, and the connection times out at 5 s.
- **Native-error recovery is unchanged.** With an unset size, H1 lowers the size at the first failed write, before any PTO, and the handshake completes at 220 ms. A candidate firing at the 200 ms PTO finds 1200 already set; the owner's call is a no-op (`reduced: false`).
- **H1's monotone congestion rule holds.** Every trigger goes through the size owner, which calls no congestion setter. A decrease would have hit cubic's panic ([cubic_sender.go:322](../../../internal/congestion/cubic_sender.go)), and no run panicked. ACK growth keeps the configured floor. #779's tests, including `TestHandshakeMTUFallbackCongestionGrowth` and `TestHandshakeMTUFallbackBeforePacking`, pass at this head ([E1 log](raw/E1-756-focused-tests.log), 12 tests). The phase and path guards are the owner's and H1's own: `TestHandshakeMTUFallbackPhaseAndPath` passes.

## Completion to confirmation

**Source**, carried from #758's [trace](../2026-10-17-handshake-silent-drop-baseline/README.md#client-handshake-space-between-completion-and-confirmation) and rechecked:

- **The server.** It confirms at completion ([connection.go:1026-1029](../../../connection.go)) and drops its Handshake keys before it can acknowledge the client's Finished in that space. For the server, the two boundaries are the same point.
- **The client between completion and confirmation.** Its Finished stays unacknowledged, so its Handshake PTO stays armed until a 1-RTT HANDSHAKE_DONE or an ACK of a 1-RTT packet. Each such Handshake PTO is eligible under a confirmation boundary.
- **Why no post-completion client reduction can be necessary on an unchanged path.**
  - Client Initial datagrams are always padded to the full current packet size ([packet_packer.go:306-315](../../../packet_packer.go)).
  - The client can complete only after the server answered one of them, so its current size has already crossed the client-to-server path.
  - Before confirmation the size only falls, and the client starts no path change, so every later client datagram fits a size already delivered.
  - A Handshake PTO after completion therefore means loss or delay, not a size limit.

**Measured.** The only eligible PTOs after completion are the client's Handshake PTOs at 80 ms (loss after receipt) and 500 ms (lossless 300 ms). Three trigger calls fell there:

- P1 at 80 ms, which reduced;
- P1 at 500 ms, a no-op, because P1 had already reduced at 200 ms;
- P2 at 500 ms, which reduced.

Neither reduction was needed. No necessary reduction in any row came after completion.

**A completion boundary, derived from these runs.** With a completion boundary, the two post-completion reductions disappear, and so does P1's no-op call. Each removed reduction was its cell's only reduction, so the run would be the baseline run of that row, which completed and confirmed at the same times. Every other reduction happened before completion and is unaffected, including P1's at 200 ms on the 300 ms path. So a completion boundary removes one measured false positive for P1 and one for P2, and loses no recovery.

## Persistent sizing

A reduction lasts for the connection, not the handshake.

- **It is retained.** Nothing raises `effectiveInitialPacketSize` after `lowerUnsetInitialPacketSize` sets it; it is assigned only at construction ([connection.go:535](../../../connection.go)) and by that function. Epoch resets in the count do not touch it, so a Retry or path change never restores 1280.
- **It becomes every path's start.** `resetPathPacketSize` ([connection.go:988-1001](../../../connection.go)) starts each new path's congestion size, finder and datagram admission at the effective size, under #756. Both path-change seams call it ([:976](../../../connection.go), [:1371](../../../connection.go)). `TestPathChangeInitialPacketSize` in the [E1 log](raw/E1-756-focused-tests.log) covers it. So a reduction on the original path also lowers the start on every later path, whatever that path's real limit.
- **Discovery is separate growth.** At the client's completion the finder is built from the effective size ([connection.go:2599-2604](../../../connection.go)), and probing starts only at confirmation and after each path change, and only with discovery enabled and DF ([connection.go:1066-1073](../../../connection.go)). Growth above 1200 therefore comes only from acknowledged probes; the retained start does not. Congestion accounting is separate again: ACK growth passes `max(mtu, initialPacketSize())` ([connection.go:2253-2262](../../../connection.go)), so Reno/Cubic never drops below 1280.
- **Without discovery the cost stays.** With discovery disabled or no DF (simnet here; some deployments), a reduced connection sends 1200-byte packets for its lifetime. That is 80 bytes less per full packet, about 6% less payload per packet; this record does not measure throughput.
- **The path-change probing defect is fixed on this baseline.** #761 (probing started after a path change even with discovery disabled or without DF) was fixed by #762 (`4d89a66e`), an ancestor of the baseline. `startMTUDiscovery` now gates both seams. No path change happens before confirmation in these scenarios.

## State and ownership cost

These are source-derived; nothing here was implemented in product code.

**Every candidate needs the same new signal.** Today `OnLossDetectionTimeout` returns only an error, and its one-probe branch emits no qlog event ([sent_packet_handler.go:962-973](../../../internal/ackhandler/sent_packet_handler.go)). The fixture infers eligibility from qlog events and `SendMode`. Product code cannot do that, because tracing is optional. A production trigger therefore needs recovery, the timeout owner, to report that an eligible Initial or Handshake PTO expired. That is an internal interface change in `internal/ackhandler`, for example a returned outcome, plus its mock. The connection would then call the size owner synchronously from its timer call, as the fixture does: no goroutine, mailbox or background timer. The size owner stays as it is.

| Candidate | State beyond the shared signal | Ownership notes |
| --- | --- | --- |
| P1 | none | The connection reacts to the first eligible outcome; the owner's guards make every later call a no-op |
| P2 | a connection-owned count and the attempt/path epoch it belongs to, reset on Retry and on a path-generation change | Recovery's own `ptoCount` cannot stand in: it resets on ACK progress and on key discard ([#758's table](../2026-10-17-handshake-silent-drop-baseline/README.md#timeout-observation)), which these semantics exclude |
| P1 + receipt guard | none; it reads `receivedFirstPacket` | A borrowed invariant: the flag is set at the first unpacked long-header packet ([connection.go:1718-1719](../../../connection.go)) and never cleared, and it already gates Retry and Version Negotiation handling. A product guard would need a comment or test pinning that meaning. By construction it excludes every server reduction: the server connection is created for the client's Initial and handed that packet before its run loop starts ([server.go:1029](../../../server.go)), so the server has processed a peer packet before its first PTO |

**A completion boundary adds one read.** Ending eligibility at completion rather than confirmation would add `!handshakeComplete` to the trigger condition; the owner's confirmation guard stays as the floor. On the server, completion and confirmation coincide ([connection.go:1026-1029](../../../connection.go)), so the choice affects only the client.

## Recommendation

Each cell is tagged **M** (measured here), **S** (derived from source) or **U** (unmeasured). Times are Dial in the simnet rows unless noted.

| | P1 | P2 | P1 + receipt guard |
| --- | --- | --- | --- |
| Client-direction drop | recovers, 220 ms (M) | recovers, 620 ms (M) | recovers, 220 ms (M) |
| Server-direction drop | recovers, 220 ms (M) | recovers, 1020 ms (M) | 10 s handshake timeout (M) |
| Both directions | recovers, 420 ms (M) | recovers, 1620 ms (M) | 10 s handshake timeout (M) |
| Real UDP, client direction | recovers, 209 ms (M) | recovers, 611 ms (M) | recovers, 205 ms (M) |
| BBRv3, client direction | recovers, 224 ms (M) | same path as P1, later (S) | same path as P1 (S) |
| Server coverage | yes (M) | yes (M) | none, by construction (S, M) |
| Unnecessary reductions, confirmation boundary | one lost round, two lost rounds, loss after receipt, lossless 300 ms on both endpoints, total loss (M) | two lost rounds, lossless 300 ms (client, after completion), total loss (M) | one lost round, two lost rounds, lossless 300 ms (client), total loss (M) |
| The same with a completion boundary | loses loss after receipt (derived from M) | loses lossless 300 ms (derived from M) | no change: the guard already abstained there (M) |
| Collateral reduction, server-direction row | client at 200 ms (M) | client at 340 ms (M) | client at 200 ms, before receipt (M) |
| Effect of a false positive on handshake timing | none in any control (M) | none (M) | none (M) |
| Cost of any reduction afterwards | the connection keeps 1200 bytes, and every later path starts there; only discovery (enabled, with DF) grows it after confirmation, and congestion accounting keeps 1280 (S) | same (S) | same (S) |
| Longer RTTs | fires before the first response once RTT exceeds the first client PTO (200 ms with the default initial RTT); measured at 300 ms (M, S) | on a lossless path, fires before the first response once RTT exceeds the second client PTO (600 ms with the default initial RTT); with any loss, a spurious first PTO plus one lost round suffices, since the count is cumulative (S) | as P1 on the client (S) |
| State beyond the shared recovery signal | none (S) | a per-epoch counter (S) | a borrowed read of `receivedFirstPacket` (S) |
| Real-network false-positive rate, throughput or lifetime cost, Retry, Version Negotiation, path changes | U | U | U |

**Recommendation: P2, with eligibility ending at handshake completion.** The owner decides; nothing here is accepted until then.

- **Server coverage.** It rules out the guard. In a server-direction or both-direction drop the guarded P1 never acts on the server, so the handshake fails exactly as at baseline. It also does not prevent the collateral client reduction it was meant to prevent.
- **False-positive cost.** It decides between P1 and P2. A false positive does not slow the handshake, but the connection keeps 1200 bytes for its life, and for every later path, unless discovery grows it after confirmation. Discovery needs it enabled and a DF-capable socket.
  - P1 takes that cost on every handshake that loses one round. It also takes it whenever the RTT exceeds the first client PTO; on the lossless 300 ms path P1 reduced both endpoints.
  - P2 takes it when an endpoint reaches a second eligible expiration before completion in the same attempt and path epoch. The count is cumulative: ACK progress and key discards do not reset it. Two consecutive lost rounds do it (measured). Other combinations count the same way but are not measured here: a spurious first PTO on a path whose RTT exceeds 200 ms, followed by any later loss before completion; or two separate losses. In the server-direction row the client's two counted PTOs straddled its receipt (200 and 340 ms). On a lossless path, P2 fires before the first response only once RTT exceeds the second client PTO.
  - This record does not measure how often each happens on real networks.
- **Delay cost of P2.** One more PTO round at the limited endpoint, in failures that otherwise never recover and end in a 5–10 s timeout. Here that is +400 ms for a client-direction drop, +800 ms for a server-direction drop and +1200 ms for both. The server-direction cost is larger because the second server PTO here is a Handshake PTO: its probes cannot carry the ServerHello, so recovery waits for the next Initial PTO.
- **Phase boundary: completion.**
  - It removes P2's only measured post-completion false positive: the spurious Handshake PTO at 500 ms on the 300 ms path, which fired because the client had no round-trip sample yet.
  - It loses no recovery: none was needed after completion, and on an unchanged path none can be.
  - On the server, completion and confirmation coincide, so the boundary changes only the client.
- **Implementation scope, for the ticket that would follow an accepted decision.**
  - Recovery reports an eligible Initial or Handshake PTO expiration from `OnLossDetectionTimeout`, including the one-probe branch. This is an internal interface change in `internal/ackhandler`, plus its mock.
  - The connection keeps a count per attempt and path epoch, resets it on Retry and path-generation changes, and calls `lowerUnsetInitialPacketSize` at the second eligible expiration before completion.
  - The size owner and its guards stay as they are. There is no public API, configuration switch or default change, and no new goroutine, timer, mailbox or per-packet ledger.
  - Tests: recovery outcomes for both PTO branches and the ACK-loss and no-op cases; the connection's count, epoch and phase rules; and a silent-drop regression for each direction.
- **Known limits.** These remain under P2:
  - the collateral client reduction in a server-direction drop;
  - the false positive at any second eligible expiration before completion: measured for two consecutive lost rounds; other combinations, and lossless paths with an RTT beyond the second PTO, are not measured;
  - the retained 1200-byte start;
  - Retry, Version Negotiation (#780) and path changes, which were not exercised.

**The alternative the owner could pick: P1 with a completion boundary.** Choose it if recovery latency on broken paths matters more than packet size on lossy or long paths. It recovers 400–1200 ms sooner here and needs no counter. It reduces on every handshake that loses one round, and on long-RTT paths; at 300 ms it reduced both endpoints.

## Reproduce

From the repository root, in a checkout whose product tree equals the baseline (only `docs/` may differ):

```bash
docs/audits/2026-10-17-handshake-silent-drop-policies/reproduce.sh capture /tmp/sdp-run
```

```bash
docs/audits/2026-10-17-handshake-silent-drop-policies/reproduce.sh race /tmp/sdp-race
```

```bash
python3 -I docs/audits/2026-10-17-handshake-silent-drop-policies/summarize.py --check
```

`summarize.py` reads only the archived tarballs, #758's archived `run1` and #758's own summarizer, whose functions it loads after checking its hash. `--check` fails unless [summary.json](summary.json) and [tables.md](tables.md) regenerate byte for byte and this README quotes every generated table verbatim. [SHA256SUMS](SHA256SUMS) covers every other file in this directory.

`reproduce.sh` refuses an output directory that is not empty, because the mapping test appends to `mapping.jsonl`.

**Preflight commands, as run.** E0 checks identity and E1 reruns #779's focused tests, as in #758; E0 additionally records the product-tree comparison and #758's archived hashes:

```bash
{ echo "date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"; echo "head: $(git rev-parse HEAD)"; echo "baseline: a7b8353ac2e87e716fcfa23119959ff5aeaef648"; echo "product tree equals baseline (outside docs/): $(git diff --quiet a7b8353ac2e87e716fcfa23119959ff5aeaef648 HEAD -- . ':(exclude)docs' && echo yes)"; echo "go: $(GOTOOLCHAIN=local go version)"; echo "go.mod: $(grep -E '^go ' go.mod)"; echo "ci unit matrix: $(grep -E 'go: \[' .github/workflows/unit.yml | tr -s ' ')"; echo "ci lint go: $(grep -m1 'go-version' .github/workflows/lint.yml | tr -s ' ')"; echo "ancestor 81a72481 (#756 merge): $(git merge-base --is-ancestor 81a72481 HEAD && echo yes)"; echo "ancestor 4d89a66e (#762 fix): $(git merge-base --is-ancestor 4d89a66e HEAD && echo yes)"; echo "#758 fixture and summarizer as archived:"; (cd docs/audits/2026-10-17-handshake-silent-drop-baseline && shasum -a 256 -c SHA256SUMS 2>&1 | grep -E 'fixture/silent_drop_(capture|mapping)_test.go.src|summarize.py|raw/run1.tar.gz'); uname -srm; } > docs/audits/2026-10-17-handshake-silent-drop-policies/raw/E0-identity.log
```

```bash
{ GOTOOLCHAIN=local go test -count=1 -run 'TestHandshakeMTUFallback|TestPathChangeInitialPacketSize|TestEmissionCoalescedHandshake|TestConnectionDatagrams' -v . 2>&1 | grep -E '^(=== RUN|--- |ok|FAIL|PASS)'; GOTOOLCHAIN=local go test -count=1 -run 'TestHandshakeMTUFallback' -v ./integrationtests/self 2>&1 | grep -E '^(=== RUN|--- |    --- |ok|FAIL|PASS)'; } > docs/audits/2026-10-17-handshake-silent-drop-policies/raw/E1-756-focused-tests.log
```

The archives were packed with `COPYFILE_DISABLE=1 tar --no-xattrs --uid 0 --gid 0 --uname '' --gname '' -czf raw/<run>.tar.gz -C <parent> <run>`, so they carry no macOS metadata entries or local account names.

**Run ledger.** The counted evidence is:

- E0, the identity receipt;
- E1, #779's focused tests (12 passing);
- run1, the capture of the mapping test, both fidelity tests, the 66 simnet cells and the four real-UDP cells;
- run2, an identical repeat capture;
- one race-detector run of the same tests without capture ([race](raw/race.tar.gz)), which passed with no race report.

All of them ran with the policy file exactly as archived; each run's `receipt.txt` holds its hash.

**History.** One development capture preceded them, with the same policy file, to confirm that every scenario was realized; its outputs were not archived and supplied no outcome. Afterwards only `summarize.py` changed: it now writes no bytecode cache and pins the one explained repeat difference. Bytecode caches that the development summaries wrote into both record directories were deleted before commit, and #758's `SHA256SUMS` still verifies.

## Limitations

- **#758's limits carry forward.**
  - These are fixed schedules on one host, not estimates of real-network failure rates.
  - Simnet has no real UDP scheduler, GSO, ECN or DF, and the single real-UDP case is loopback through the proxy.
  - Goroutine order at one virtual instant is not a total causal order. The release and the variant both act inside the timer call, before the probes are packed.
- **The trigger is inferred.** Eligibility comes from #758's observer, which reads qlog events and `SendMode`. It cannot classify a one-probe expiry while a probe is pending; no such call occurred. A product trigger needs recovery to report the outcome itself.
- **Not exercised:**
  - Retry and path-change epoch resets (the count implements them, but no row has either);
  - Version Negotiation (#780);
  - upward discovery after a reduction;
  - throughput or lifetime cost of the retained 1200-byte start.
- **No latency sweep.** RTT dependence beyond the 20 ms and 300 ms rows is derived from the PTO schedule, not measured.
- **BBR has one focused row.** It ran P1 only. BBR's internal datagram size after the trigger is not visible in its once-a-second trace; the packed sizes and the source trace cover it.
- **P2's server-direction delay depends on PTO interleaving.** Here the second server PTO is a Handshake PTO, whose probes cannot carry the ServerHello. Other schedules are not measured.
- **Not a decision.** The recommendation is input to the owner's decision. Nothing here encodes it as desired behavior, and no failing test is merged.
