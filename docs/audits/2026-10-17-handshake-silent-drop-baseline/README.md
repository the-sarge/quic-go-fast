# Handshake silent-drop baseline: issue #758

**Result: at baseline `a7b8353a`, no endpoint recovers from silently dropped oversized handshake datagrams, and every control behaves as its schedule requires.**

- **No size drop recovers.** Dropping datagrams over 1250 bytes ends every size-drop case in a transport timeout. The effective initial packet size stays 1280 on both endpoints throughout, because writes succeed and H1 only reacts to a local message-size error.
- **The ordinary-loss, healthy and lossless high-delay controls complete,** with a verified 4096-byte echo in each direction.
- **Existing traces undercount PTOs.** One recovery branch counts a PTO expiration but emits no qlog event. A test-side observer at the connection's loss-timer call counts both branches without changing product code. In the server-oversized case the client takes 8 eligible PTOs, but qlog shows 1.
- **Two eligible client PTOs fall after handshake completion and before confirmation.** One is in the loss-after-receipt control; the other is a spurious Handshake PTO on the lossless 300 ms path. This is the input to #759's phase decision.

No product code, active test or CI configuration changed. The fixture is archived in [fixture/](fixture/) as `.go.src` files that [reproduce.sh](reproduce.sh) copies into place, runs and removes.

## Authorization and provenance

- **Scope.** [#758](https://github.com/the-sarge/quic-go-fast/issues/758) is an investigation ticket. The owner approved this execution plan on 2026-10-16 after [#756](https://github.com/the-sarge/quic-go-fast/issues/756) merged. GitHub is authoritative for the acceptance criteria. No policy is chosen and no production change is authorized; [#759](https://github.com/the-sarge/quic-go-fast/issues/759) compares policies.
- **Baseline.** `a7b8353ac2e87e716fcfa23119959ff5aeaef648` on `main`. It contains the #756 merge `81a72481` and the #762 discovery-gating fix `4d89a66e`; see the [ancestry receipt](raw/E0-baseline.log).
- **Toolchain.** go1.27.2 darwin/arm64 with `GOTOOLCHAIN=local`. This matches CI's pin (unit matrix 1.26.9 and 1.27.2; lint 1.27.2). `go.mod` declares `go 1.26.0`.
- **Workspace.** Worktree `/Volumes/worktrees/quic-go-fast/handshake-silent-drop-baseline`, branch `codex/handshake-silent-drop-baseline`; the initiating checkout was not modified.
- **Fixture identity.** The SHA-256 of each fixture file as run is in each run's `receipt.txt` inside [raw/run1.tar.gz](raw/run1.tar.gz).

## #756 behavior on this baseline

| Narrowed #756 fact | Source at `a7b8353a` | Evidence |
| --- | --- | --- |
| An unset size stays distinguishable from an explicit one | `populateConfig` copies `InitialPacketSize` unchanged ([config.go:172](../../../config.go)); `initialPacketSize()` supplies 1280 and `initialPacketSizeUnset()` tests for 0 ([config.go:24-38](../../../config.go)) | Every recorded state in every scenario shows `initial_packet_size_unset: true` and effective size 1280 |
| The fallback is unset-only | `coalescedSendMetadata` marks `handshake` only for an unset size ([connection.go:2699-2702](../../../connection.go)); `lowerUnsetInitialPacketSize` guards unset, pre-confirmation and above 1200, and calls no congestion setter ([connection.go:2616-2629](../../../connection.go)) | #779's focused tests pass at baseline ([E1 log](raw/E1-756-focused-tests.log), 12 tests) |
| H1's feedback ordering and monotone Reno/Cubic sizing remain | `applyHandshakeMTUFallback` consumes the mailbox and checks the path generation ([connection.go:2607-2614](../../../connection.go)); ACK growth passes `max(mtu, initialPacketSize())` ([connection.go:2260](../../../connection.go)); the cubic decrease panic stays ([cubic_sender.go:322](../../../internal/congestion/cubic_sender.go)) | `TestHandshakeMTUFallbackCongestionGrowth` and `...BeforePacking` in E1 |
| Path changes use the effective size and update admission | `resetPathPacketSize` resets congestion, finder and the datagram admission estimate at `effectiveInitialPacketSize` ([connection.go:988-1001](../../../connection.go)); both seams call it ([connection.go:976](../../../connection.go), [:1371](../../../connection.go)) | `TestPathChangeInitialPacketSize` in E1 |

## Timeout observation

**The gap.** The connection calls `OnLossDetectionTimeout` once its alarm is due ([connection.go:731-735](../../../connection.go)) and sees only the returned error. The ordinary PTO branch emits `loss_timer_updated{expired, pto}` and `pto_count`. The client's anti-deadlock branch emits neither. That branch runs when nothing is in flight and the peer has not completed address validation ([sent_packet_handler.go:962-973](../../../internal/ackhandler/sent_packet_handler.go)), and it increments the same private counter. Counting qlog events would therefore miss real expirations.

**The observer.** The fixture wraps each connection's recovery handler, through the existing `newConnection` and `newClientConnection` test seams, before the run loop starts. For each call it records:

- the qlog events emitted inside the call;
- `SendMode` before and after the call;
- connection state, read on the connection's own goroutine.

Every other method passes through unchanged, including the five optional interfaces the connection and emission assert. `TestSilentDropObserverFidelity` checks that the real handler implements them all and the observer forwards them all. Classification rule:

| Signal inside one call | Class | Counted |
| --- | --- | --- |
| `expired` PTO event for a space | `pto` (ordinary branch) | Yes, if Initial or Handshake and before confirmation |
| `expired` ACK event | `ack_loss` (time-threshold loss) | No |
| No expiry event; `SendMode` moves from a non-probe mode (not `none`) into a probe mode | `pto_silent` (the one-probe branch) | Yes, under the same conditions |
| No expiry event; neither side is a probe mode or `none` | `no_op` (a stale return) | No |
| No expiry event while a probe was already pending, or `none` on either side | `indeterminate` | Never counted; reported as a gap |

**Why the rule is sound.**

- Recovery increments its counter only in the ordinary and one-probe branches.
- The ordinary branch always emits an `expired` PTO event when tracing is enabled.
- Both branches raise the pending probe count.
- `SendMode` returns the probe mode whenever probes are pending, unless the amplification or tracked-packet limit returns `none` first.
- The ACK-loss branch returns before touching the counter or the probes.

So the rule never counts a non-expiration. A real expiration is either counted or reported as indeterminate.

**The mapping tests prove the rule.** [silent_drop_mapping_test.go.src](fixture/silent_drop_mapping_test.go.src) drives the real handler through every case below and records the private counter as ground truth. [summarize.py](summarize.py) re-derives each class in Python and finds no violation of the rule.

| Case (all against the real `sentPacketHandler`) | Counter | Events in the call | `SendMode` | Class |
| --- | --- | --- | --- | --- |
| Ordinary Initial PTO, then its two probes sent | +1, then +1 at the next expiry | `expired pto initial`, `pto_count=1`; sending the probes emits none | any → Initial probe | `pto` |
| Ordinary PTO while its probes are still pending | +1 | `expired pto initial`, `pto_count=2` | Initial probe → Initial probe | `pto` (the event decides) |
| Ordinary Handshake PTO; server Initial PTO | +1 | `expired pto` | any → probe | `pto` |
| Client one-probe Initial and Handshake PTOs with nothing in flight | +1 | none (only the re-armed `set`) | any → probe | `pto_silent` |
| A second one-probe expiry before its probe was sent | +1 | none | Initial probe → Initial probe | `indeterminate` |
| Time-threshold loss expiry | 0 | `expired ack initial`, `packet_lost` | any → any | `ack_loss` |
| Call with nothing outstanding; call while only 0-RTT is in flight | 0 | none / re-armed `set` | any → any | `no_op` |

Two probes from one expiration count once, because the count is per call, not per probe. A probe left pending cannot create another count: an `expired` event still marks a real ordinary expiration, and anything else is `indeterminate`.

**Recovery's counter versus the comparison count:**

| Change | Recovery's counter | qlog | Comparison count |
| --- | --- | --- | --- |
| Client Initial ACK before address validation | kept | none | kept |
| Client Handshake ACK; any server ACK | reset | `pto_count=0` | kept: ACK progress does not reset it |
| Initial or Handshake key discard | reset | `pto_count=0` only if it was nonzero | kept |
| Retry | reset | `pto_count=0` always | new epoch |
| Path migration | kept | none | new epoch (no path changes happen before confirmation here) |
| Server reaches its 3× amplification limit | — | `loss_timer_updated{cancelled}` with empty fields; `SendMode` is `none`; re-armed by `set` when bytes arrive | no expiration while limited |

**Results:**

- **Coverage.** The ten captured cases make 37 loss-timer calls: 30 `pto` and 7 `pto_silent`, every one eligible. None was `ack_loss`, `no_op` or `indeterminate`, and the Go and Python classifications agree on every call.
- **What #759 can reuse.** A disposable variant can apply the same rule at the connection's timer call. It uses two extra `SendMode` calls, which the mapping test shows leave recovery state unchanged.
- **The limit.** The rule cannot classify a one-probe expiry while a probe is still pending. A recovery-owned timeout outcome would remove that limit; it is a proposal, not part of this ticket.

## Comparison count

The experimental count is cumulative per handshake attempt and path epoch. It counts eligible expirations: classes `pto` and `pto_silent`, Initial or Handshake space, before confirmation. For a trigger it saturates at two.

- **What does not reset it:** ACK progress and Initial key discard.
- **What does:** Retry or a path-generation change starts a new epoch. Neither occurs in these scenarios.
- **Sizes are separate.** Resetting the count never restores a packet size, and no size changed here.
- **Receipt.** This is `receivedFirstPacket`: the first successfully unpacked long-header peer packet processed by the current connection. The fixture records it from the first long-header `packet_received` event, at which point the flag is confirmed set. Buffered undecryptable packets do not count. In the server-drop case the client buffers 8 undecryptable packets (7 Handshake, one 1-RTT), 2 of them before its receipt.

## Fixture manifest

| Setting | Value |
| --- | --- |
| Harness | Root-package `quic` internal tests: [silent_drop_capture_test.go.src](fixture/silent_drop_capture_test.go.src), [silent_drop_baseline_test.go.src](fixture/silent_drop_baseline_test.go.src) |
| Network | Simnet in `testing/synctest` virtual time; a custom router owns every drop and records every datagram. The real-UDP case uses loopback sockets and the existing `integrationtests/tools/proxy` in wall-clock time |
| Size limits | Simnet: 1452 bytes on every uplink and downlink, set explicitly, so simnet's implicit 1400-byte limit plays no part. Real UDP: the run records the loopback interface (`lo0`, MTU 16384) and the proxy's 1452-byte per-datagram read buffer. The largest attempted datagram is 1280 bytes, so only the selected filter drops anything |
| Size filter | Drops datagrams over 1250 bytes in the selected direction; 1200 passes. Writes always succeed locally; no EMSGSIZE is injected |
| Latency | 10 ms one way (20 ms RTT); 150 ms one way (300 ms RTT) for the high-delay row |
| Configuration | `InitialPacketSize` unset on both endpoints (effective 1280). Default `HandshakeIdleTimeout` (5 s, so a 10 s handshake timeout), `MaxIdleTimeout`, congestion control and path MTU discovery |
| Discovery | Simnet sockets report no DF, so discovery is unavailable; the real-UDP client socket reports DF. Discovery starts only at confirmation, so it never affects a handshake here |
| TLS | `internal/testdata`: one RSA-2048 leaf certificate for `localhost` signed by the quic-go test CA. TLS clock fixed at 2026-01-01, because synctest's clock starts in 2000. Go default curves; every successful case negotiates X25519MLKEM768. The ClientHello always spans two 1280-byte Initial datagrams |
| Deadlines | 30 s Dial context, so a transport timeout ends first. A failed server connection is allowed 60 s to end itself |
| Success path | Accept, a 4096-byte echo in each direction, `CloseWithError`, and a wait for the peer's close |
| Cleanup | Listener, transports, sockets and the simnet are closed. `synctest.Test` returns only after every bubble goroutine exits. In the real-UDP case `Proxy.Close` joins the proxy's workers |

## Results

Times are virtual milliseconds from the start of the dial. The real-UDP times are wall-clock. "Eligible PTOs" counts the expirations defined above; "qlog" is what a count of `expired` PTO events would report.

| Scenario | Client eligible PTOs (ordinary + silent) | qlog | Client receipt | Server eligible PTOs | Outcome |
| --- | --- | --- | --- | --- | --- |
| Client oversized dropped | 4 + 0 at 200, 600, 1400, 3000, all before receipt | 4 | none | no server connection | client `IdleTimeoutError` at 5000 |
| Server oversized dropped, prior receipt | 1 + 7: ordinary at 200; silent at 340, 560, 920, 1530, 2595, 4502, 7993 | 1 | 220 | 6 + 0: Initial at 210, 1010, 4210; Handshake at 410, 2010, 8410 | `HandshakeTimeoutError`: client 10000, server 10010 |
| Both directions oversized dropped | 4 + 0, as client-only | 4 | none | no server connection | client `IdleTimeoutError` at 5000 |
| Ordinary loss after client receipt | 1 + 0: Handshake at 80, after completion (20), before confirmation (100) | 1 | 20 | 0 | success; Dial 20, confirmed 100 |
| Healthy | 0 | 0 | 20 | 0 | success; Dial 20, confirmed 40 |
| Lossless high delay, 300 ms RTT | 2 + 0: Initial at 200 (before receipt); Handshake at 500 (after completion, before confirmation) | 2 | 300 | 1 + 0: Initial at 350 | success; Dial 300, confirmed 600 |
| One lost transmission round | 1 + 0 at 200 (release) | 1 | 220 | 0 | success; Dial 220, confirmed 240 |
| Two lost transmission rounds | 2 + 0 at 200 and 600 (release) | 2 | 620 | 0 | success; Dial 620, confirmed 640 |
| Total loss | 4 + 0, as client-only | 4 | none | no server connection | client `IdleTimeoutError` at 5000 |
| Real UDP, client oversized dropped | 4 + 0 at 202, 603, 1404, 3005 | 4 | none | no server connection | client `IdleTimeoutError` at 5002 |

**At every recorded point** (receipt, completion, confirmation and every timer call), both endpoints have effective initial size 1280 and maximum packet size 1280. The one exception is the server's own first receipt, at 1200, because it has not yet processed the client's transport parameters. Every successful case verified the bidirectional transfer. Every case recorded joined cleanup. The [summary](summary.json) shows every fixture check and scenario witness true.

**Required sequences, per row:**

- **Client oversized dropped.**
  - All 10 client datagrams are 1280 bytes and dropped by the filter.
  - The server never creates a connection. The client times out after the 5 s handshake idle timeout, before the 30 s fixture deadline.
- **Server oversized dropped.**
  - The server processes the client's Initial at 10 ms, before its first dropped write and its first eligible PTO.
  - Of its 23 datagrams, the 7 ServerHello-bearing ones (1280 bytes) are dropped. The Handshake-only datagrams (1185 bytes in run1) and 41-byte ACK-only Initials get through.
  - The client buffers the Handshake packets as undecryptable. It processes the 41-byte ACK at 220 ms, after its first PTO.
  - Because those ACKs acknowledge all of its Initial data, every later client expiry takes the silent branch.
  - Both endpoints process packets throughout, so both end at the 10 s handshake timeout rather than the 5 s idle timeout.
- **Both directions oversized dropped.** Identical to the client-only row: the server never receives a packet.
- **Ordinary loss after client receipt.**
  - The client processes the server's flight at 20 ms and completes, with no earlier eligible PTO.
  - Its next datagram (1280 bytes: Initial ACK, Handshake Finished and 1-RTT) is dropped.
  - Its first eligible PTO, a Handshake PTO at 80 ms, restores delivery inside the timer call. The two 74-byte Handshake probes are sent after the release record at the same instant and are delivered.
  - The server completes at 90 ms.
- **Healthy.** No drops and no eligible PTO on either endpoint.
- **Lossless high delay.**
  - The client's first eligible PTO (200 ms) precedes its receipt (300 ms), and receipt precedes its second eligible PTO (500 ms).
  - That second PTO is spurious. At 200 ms the client re-sent both ClientHello packets and declared the originals lost (`QueueProbePacketAt`, [sent_packet_handler.go:1129-1146](../../../internal/ackhandler/sent_packet_handler.go)). The server's ACK at 300 ms therefore acknowledges no newly acknowledged packet, so it yields no round-trip sample ([sent_packet_handler.go:446-460](../../../internal/ackhandler/sent_packet_handler.go)).
  - The Handshake timer, armed at 300 ms, uses the unsampled 200 ms PTO and fires at 500 ms. Confirmation arrives at 600 ms in a 1-RTT packet carrying HANDSHAKE_DONE and an ACK of the client's 1-RTT data; that ACK gives the first round-trip sample (300 ms). The client's Finished is never acknowledged in the Handshake space (see below).
  - The server's Initial PTO at 350 ms fires before the client's ACK reaches it at 450 ms.
- **One and two lost transmission rounds.** Every client datagram before the release is dropped: 2 and 4 datagrams respectively, so the second row also drops the first PTO's probes. Delivery is restored inside the client's first or second eligible timer call, and the next client datagrams are that expiration's probes. Server-to-client delivery stays enabled.
- **Total loss.** All 10 attempted datagrams are 1280 bytes, and all are dropped. The policy drops every datagram whatever its size, so it would also drop a 1200-byte one, but none was attempted: no fallback ran. The client times out at 5 s with joined cleanup.
- **Real UDP.** It matches the simnet client-oversized row to within 5 ms per timer call. The proxy drops all 10 client datagrams.

**Repeat run.** [run2](raw/run2.tar.gz) matches [run1](raw/run1.tar.gz) in all ten cases on several projections:

- every loss-timer call: time, class, level and count;
- every datagram carrying an Initial or Handshake packet: time, direction, filter side and decision;
- every receipt;
- every outcome.

Two things vary between runs. The server's Handshake-only datagram varies between 1184 and 1203 bytes across scenarios and runs, always under the filter. Packetization of the 1-RTT echo also differs.

**Amplification.** No server send happened at or over three times the client bytes delivered to it, in any scenario. The two 1280-byte ClientHello datagrams give a 7680-byte budget, and the server's first flight is about 2.6 KB. The server's `cancelled` timer events coincide with completion or close, not with the amplification limit.

**Timeout classification.**

- With no processed peer packet, the dial ends with `IdleTimeoutError` after `HandshakeIdleTimeout` (5 s).
- When both endpoints keep processing packets (the server-drop row), each ends with `HandshakeTimeoutError` at twice that (10 s).
- No case reached the 30 s fixture deadline.

## Client Handshake space between completion and confirmation

**Source.**

- The client drops its Initial keys when its first Handshake packet is registered ([connection.go:2692-2696](../../../connection.go)).
- It completes on TLS completion ([connection.go:2138](../../../connection.go)); at completion the client only applies transport parameters ([connection.go:1021-1023](../../../connection.go)).
- It confirms on HANDSHAKE_DONE or an ACK of a 1-RTT packet, which drops the Handshake space ([connection.go:1052-1063](../../../connection.go)).
- **Timers in between.** The client's Finished is outstanding and a Handshake PTO is armed ([sent_packet_handler.go:723-730](../../../internal/ackhandler/sent_packet_handler.go)). A 1-RTT PTO needs confirmation ([:731](../../../internal/ackhandler/sent_packet_handler.go)).
- **A branch these captures never reach.** If a Handshake ACK of the Finished arrived, it would complete address validation ([:415-421](../../../internal/ackhandler/sent_packet_handler.go)), and with no crypto data outstanding no PTO would be armed until confirmation ([:705-708](../../../internal/ackhandler/sent_packet_handler.go)).
- The server confirms at completion ([connection.go:1026-1029](../../../connection.go)). It drops its Handshake keys before it can acknowledge the client's Finished in that space.

**Observed.**

- **No Handshake ACK.** In every successful scenario of both runs, the server sends no Handshake packet after its completion. The client receives no Handshake ACK of its Finished, and it confirms on a 1-RTT packet carrying HANDSHAKE_DONE. So the client's Handshake PTO stays armed for the whole window between completion and confirmation.
- **Window lengths.** Client completion and confirmation are 20 and 40 ms (healthy), 20 and 100 ms (loss after receipt), 300 and 600 ms (high delay), 220 and 240 ms (one round) and 620 and 640 ms (two rounds). These are properties of these schedules, not a minimum delay.
- **PTOs inside the window.** The eligible client PTOs there are the Handshake PTOs at 80 ms (loss after receipt) and 500 ms (high delay). A trigger window that ends at completion would exclude both; one that ends at confirmation includes both. This investigation does not choose between them.

## Discovery-gating status

The path-change discovery-gating defect is fixed on this baseline by #762 (`4d89a66e`). `startMTUDiscovery` starts probing only when discovery is enabled and the socket reports DF ([connection.go:1066-1073](../../../connection.go)). Both path-change seams call it after `resetPathPacketSize`. No path change occurs before confirmation in these scenarios, and the separate defect is outside this ticket.

## Reproduce

From the repository root, in a checkout whose product tree equals the baseline (only `docs/` may differ):

```bash
docs/audits/2026-10-17-handshake-silent-drop-baseline/reproduce.sh capture /tmp/sd-run
```

```bash
docs/audits/2026-10-17-handshake-silent-drop-baseline/reproduce.sh race /tmp/sd-race
```

```bash
python3 -I docs/audits/2026-10-17-handshake-silent-drop-baseline/summarize.py --check
```

The capture mode writes one JSONL stream per scenario plus `mapping.jsonl` and the test logs. Each record has a sequence number and, inside synctest, a virtual timestamp. `summarize.py` reads only the archived tarballs and runs no tests; `--check` fails unless it regenerates [summary.json](summary.json) byte for byte. [SHA256SUMS](SHA256SUMS) covers every other file in this directory.

`reproduce.sh` refuses an output directory that is not empty, because the mapping test appends to `mapping.jsonl`.

**Preflight commands, as run.** E0 was written to `raw/E0-baseline.txt` and renamed to `.log` before commit, because the repository ignores `*.txt`:

```bash
{ echo "date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"; echo "head: $(git rev-parse HEAD)"; echo "go: $(GOTOOLCHAIN=local go version)"; echo "go.mod: $(grep -E '^go ' go.mod)"; echo "ci unit matrix: $(grep -E 'go: \[' .github/workflows/unit.yml | tr -s ' ')"; echo "ci lint go: $(grep -m1 'go-version' .github/workflows/lint.yml | tr -s ' ')"; echo "ancestor 81a72481 (#756 merge): $(git merge-base --is-ancestor 81a72481 HEAD && echo yes)"; echo "ancestor 4d89a66e (#762 fix): $(git merge-base --is-ancestor 4d89a66e HEAD && echo yes)"; uname -srm; } > docs/audits/2026-10-17-handshake-silent-drop-baseline/raw/E0-baseline.log
```

E1 concatenates the filtered output of these two commands:

```bash
GOTOOLCHAIN=local go test -count=1 -run 'TestHandshakeMTUFallback|TestPathChangeInitialPacketSize|TestEmissionCoalescedHandshake|TestConnectionDatagrams' -v . 2>&1 | grep -E '^(=== RUN|--- |ok|FAIL|PASS)'
```

```bash
GOTOOLCHAIN=local go test -count=1 -run 'TestHandshakeMTUFallback' -v ./integrationtests/self 2>&1 | grep -E '^(=== RUN|--- |    --- |ok|FAIL|PASS)'
```

**Run ledger.** The archived captures come from the fixture as archived here; each run's `receipt.txt` holds its file hashes. The counted evidence is:

- E0, the toolchain and ancestry audit;
- E1, #779's focused tests;
- run1, the capture of the mapping tests, the observer fidelity test, the nine scenarios and the real-UDP case;
- run2, an identical repeat capture;
- one race-detector run of the same tests without capture, [race](raw/race.tar.gz), which passed with no race report.

**History.**

- **Development.** Before the first freeze, development used four mapping-test runs, one capture run and one race run. Two corrections came out of it: a string match on qlog output that could never fail was replaced by parsed JSON, and DF fields were added.
- **First captures.** The first frozen fixture's captures are at commit `ce5f926e`.
- **Review round 1** (RAS run `20261017T031328-36c952f9b0c71fea2229d791`) changed the fixture in two ways. The Accept helper is now joined on every outcome; before, the real-UDP case did not wait for it. The manifest also records each network's own size limits, where it had recorded the simnet link MTU for the real-UDP case too. All three runs were then repeated with the corrected fixture, and they replace the first captures. Every timer classification, receipt, delivery decision and outcome is unchanged; only the real-UDP wall-clock times moved, by about a millisecond.
- **Development runs never supplied an outcome.**

## Limitations

- **Fixed schedules.** These are deterministic semantic controls, not estimates of real-network failure rates. Simnet has no real UDP scheduler, GSO, ECN or DF; the single real-UDP case is loopback through the proxy.
- **Run-to-run variation.** Cryptographic randomness changes the server's Handshake-only datagram size by a few bytes between runs. Goroutine order at one virtual instant is not a total causal order. The fixture checks the one ordering that matters, release before probes, explicitly.
- **The observer's limit.** It cannot classify a one-probe expiry while a probe is still pending; none occurred. It adds two `SendMode` calls per timer call and serializes qlog events synchronously, which changes allocation but not packet bytes or virtual timing.
- **What the evidence does not cover.** Effective sizes come from connection state, not from a reduction path, because none ran. Upward discovery, path changes, Retry and Version Negotiation are not exercised; #780 tracks the separate Version Negotiation gap.
- **Not a statement about #759.** Baseline non-recovery is an observation. Nothing here encodes it as desired behavior, and no failing test is merged.

## Handoff to #759

- **Baseline and controls.** Reuse baseline `a7b8353a`, the nine scenario rows, their release rules and this fixture manifest. The fixture's release hook and the router policies are the places a disposable variant plugs in.
- **The trigger.** Use the boundary rule above as the experimental trigger, applied at the connection's timer call. Report any `indeterminate` call; none occurred at baseline.
- **Server-drop row.**
  - The receipt guard is decided by a 41-byte ACK-only server Initial that arrives at 220 ms, after the client's first PTO at 200 ms.
  - So P1 and the guarded P1 would both trigger at the client's first PTO. This is an implication of the baseline timeline, not a measured variant.
  - Later client expiries are silent-branch PTOs.
  - The server takes 6 eligible PTOs, the first at 210 ms after its receipt at 10 ms, so the receipt guard excludes server reduction.
- **Completion-to-confirmation window.** The server never acknowledges the client's Finished in the Handshake space, so the client's Handshake PTO stays armed until HANDSHAKE_DONE arrives.
- **Ordinary-loss and high-delay rows.**
  - In the loss-after-receipt row, the client's only eligible PTO is a Handshake PTO after completion, with receipt true.
  - In the lossless 300 ms row, the client takes a pre-receipt Initial PTO and a spurious post-completion Handshake PTO. P2 would trigger there under a confirmation boundary but not under a completion boundary.
