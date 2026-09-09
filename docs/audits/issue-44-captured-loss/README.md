# Issue #44: captured ACK blackout and stream gap

The September 9, 2026 failure on PR #144 is a server stream read waiting for bytes 2508–3760. The router dropped every transmission containing that range and every server acknowledgment of client application packets. The client continued sending PTO probes until the unchanged two-minute read timeout fired. A reconstruction reproduces the 2508-byte deadline failure; two single-decision interventions restore delivery. This capture supports a probabilistic test-expectation defect, not a production recovery fix.

## Provenance and scope

- Failed job: <https://github.com/the-sarge/quic-go-fast/actions/runs/34396439182/job/102617218042>.
- Exact source: `73e370df589655aae94181c2061caf00c88d5f70`; macOS, Go 1.27.x, QUIC v2.
- Original selector: `TestHandshakeWithPacketLoss/drop_1/3_of_packets_in_direction_to_client/retry:_false/client_speaks_first#03`.
- Configuration: requested direction to client, one-third random loss, no Retry, client speaks first, post-quantum TLS, long certificate chain, 5000-byte payload, 20 ms RTT, two-minute timeout.
- Original files, read only: `/Volumes/worktrees/quic-go-fast/issue-46-merge-control/diagnostic-head-ci-failure.log` and `/Volumes/worktrees/quic-go-fast/issue-46-merge-control/issue44-captured-diagnostics.log`.
- Full job log SHA-256: `7f2a1e4c7dd99698ff67d1b1e448760ae03a10f90c0d0a8e7bfe24038a6032a4`.
- Extracted diagnostics SHA-256: `365e986a398dbbae3787de9bc6903f665cf56e369740a620ee214331d4d707d8`.
- Investigation checkout: `/Volumes/worktrees/quic-go-fast/issue44-captured-loss`, branch `codex/issue44-captured-loss`, based on the exact failing commit. The initiating checkout and the #46 workflow checkout were not modified.

This report diagnoses this natural capture. It does not establish that every historical #44 failure has the same cause. No CI rerun or PR #144 merge was requested by this investigation. The corruption suite passed in the original job and is outside this diagnosis.

## Captured timeline

Times below are seconds from the synctest epoch. Stream intervals use inclusive byte indexes; all application packet numbers refer to the 1-RTT packet-number space.

| Time | Observation | Consequence |
| --- | --- | --- |
| 0.1125 | Client sends packets 0, 1, 2, 3 and 4: bytes 0–1254, 1255–2507, 2508–3760, 3761–4999, and FIN at offset 5000. Router drops packet 2, CRC32c `4051129371`. | A 1253-byte gap remains. |
| 0.1225 | Server receives packets 0, 1, 3 and 4. | The reader can consume exactly 2508 contiguous bytes. Later data and FIN cannot complete an ordered stream across a gap. |
| 0.1225 | Server packet 2 acknowledges ranges 0–1 and 3–4, along with handshake completion/control frames. Router drops it, CRC32c `784704909`. | Client learns neither which application data arrived nor which range is missing. |
| 0.1325 | Client receives server packet 3 carrying handshake completion/control frames, without an ACK. | Handshake completes, but application recovery has no acknowledgment evidence. |
| 0.2185–88.1105 | Client sends two probes per PTO. PTO expiries are 0.2185, 0.3905, 0.7345, 1.4225, 2.7985, 5.5505, 11.0545, 22.0625, 44.0785 and 88.1105. | Probes rotate through the outstanding stream frames, FIN and connection-ID retirement; the timeout backs off exponentially. |
| 0.3905, 2.7985, 22.0625 | Client retransmits the missing range in packets 10, 20 and 29. Each is dropped: CRC32c `1692027534`, `854227110`, `2179534616`. | Including the first transmission, all four attempts to fill the gap are explicitly lost at the router. |
| 0.2285, 0.4005, 0.7445, 1.4325, 5.5605, 44.0885 | Server emits further application ACKs in packets 4, 8, 12, 13, 14 and 15. Each corresponding router decision drops the datagram. | No application ACK reaches the client during the captured transfer. |
| 0.509732 / 0.519732 | Client receives server control retransmissions and its ACK reaches the server. | The server can retire its own outstanding control frames. This does not acknowledge client stream data. |
| 88.1105 | Client reaches PTO count 10 and sends offsets 0 and 1255, both dropped. Next loss timer is 60 seconds away. | The next probe opportunity is later than the read deadline. |
| 120.1225 | Server read returns 2508 bytes and `deadline exceeded`; assertion cleanup closes the client connection. | Connection close follows the read failure; it did not cause the stall. |

The retained tail contains 23 router decisions toward the client (14 drops; longest run of drops 4) and 32 toward the server (15 drops; longest run 5). Thus the fixture's ten-consecutive-drop guard never needs to intervene. Successful control and duplicate-data datagrams break those runs without ensuring delivery of the missing bytes or an ACK.

The sample's observed drop fraction need not equal one third: the callback makes a Bernoulli draw per datagram, rather than enforcing a quota. `dropCallbackDropOneThird(_ direction)` also ignores its requested direction and applies loss in both directions. This explains why a selector labeled “to client” can lose client stream data. Neither behavior was changed here.

The collector omits the first 35 events. Data-bearing sends with nonzero CRC32c can be joined directly to router decisions. Several small packet send/receive events have no checksum; their ACK-to-router association uses direction, time, datagram size and the surrounding event sequence. It is not claimed to be a checksum join. The independent reconstruction supplies additional causal evidence for these associations.

## Mechanism in the exact source

`integrationtests/self/handshake_drop_test.go` calls `io.ReadAll` through `readerWithTimeout` on the accepted unidirectional server stream. The wrapper in `integrationtests/self/self_test.go` interrupts a blocked stream read by setting its deadline when the per-read timeout expires. The successful client `SendStream.Close` marks writing finished and schedules FIN; it does not wait for remote delivery.

In `internal/ackhandler/sent_packet_handler.go`, `QueueProbePacket` selects the first outstanding packet and queues its frames again. Newly acknowledged packets retire outstanding work and reset PTO state in `ReceivedAck`; without application ACKs, the client cannot use the server's knowledge of the received stream ranges. `OnLossDetectionTimeout` schedules two probes and increases `ptoCount`; `getScaledPTO` doubles the base interval, capped at 60 seconds. The captured cycling and timer values follow these paths.

[RFC 9002 sections 6.2.1 and 6.2.4](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.2) describe exponential PTO backoff, ACK-driven reset, and probe retransmission choices. They provide no guarantee that arbitrary finite losses resolve within an application's read deadline. Changing the production backoff or probe-selection policy is not justified by this capture.

## Causal experiments

The temporary harness uses the existing production transport, simulator, TLS fixture, diagnostics and `dropTestProtocolClientSpeaksFirst`. It substitutes recorded per-direction datagram drop decisions for random draws. It preserves the payload, RTT, PQ/long-chain settings, QUIC version, and deadlines. The two missing client Initial router decisions are inferred as forwarded from the server's retained ACK of Initial packets 0 and 1. All retained router decisions are used in order. After a direction's recorded sequence ends, additional datagrams are forwarded; both successful interventions finish before that fallback is reached.

| Experiment | Single intervention | Result |
| --- | --- | --- |
| Reconstructed capture | None | Fails with exactly 2508 bytes and `deadline exceeded`; retained 256 / omitted 35; PTO count 10. |
| Deliver gap | Forward client-to-server datagram index 8 (zero based), the initial missing stream range. | All 5000 bytes arrive and the original helper's equality/EOF assertions pass. |
| Deliver first application ACK | Forward server-to-client datagram index 11, containing ACK plus control frames. | Full transfer passes. This intervention also advances delivery of its control frames. |
| Deliver ACK only | Forward server-to-client datagram index 13, the standalone ACK sent at 0.2285. | Full 5000-byte transfer finishes at 0.296 seconds, with no read error. This removes the control-frame confound. |

The first three cases ran together once; the ACK-only case ran separately once. No random duplicate campaign was used as evidence. These interventions change one router decision in a diagnostic harness, not the maintained loss callback.

All 256 reconstructed tail entries align in event order and timestamps with the capture. After excluding fresh cryptographic values/checksums and connection-ID frame ordering, 12 entries differ: one fresh transport-parameter CID and 11 entries reflecting a five-byte difference in generated handshake material and associated sizes/accounting. All remaining application-data events, recovery metrics/timers and router decisions match through the timeout. This is a reconstructed behavioral sequence, not a byte-for-byte replay.

## Reproduce the evidence

The harness is stored as text so its intentionally failing baseline does not enter the normal test suite. In a dedicated clean worktree at the exact source commit, copy `issue44_capture_experiment_test.go.txt` from this directory to `integrationtests/self/issue44_capture_experiment_test.go`, then run with Go 1.27.0 on macOS:

```sh
go test ./integrationtests/self -run '^TestIssue44CapturedLossExperiment$/(captured|deliver_gap|deliver_ack)$' -version=2 -count=1 -timeout=60s -v
go test ./integrationtests/self -run '^TestIssue44CapturedLossExperiment$/deliver_ack_only' -version=2 -count=1 -timeout=60s -v
```

The first command is expected to exit 1 because the captured case invokes the original success assertion and fails; both intervention cases pass. The second command exits 0. Original execution was `go version go1.27.0 darwin/arm64`; the first command originally ran before the ACK-only case and final result logging were added. `reconstruction.log` records that first execution and `ack-only-control.log` records the later control. Source versions, the full original job log, parsed timeline and comparison are additionally preserved in `/Users/josh/.codex/diagnostics/quic-go-fast/issue44-pr144-capture`.

The ordinal harness is pinned evidence, not a portable test corpus. Packetization or handshake changes may invalidate its mapping; a maintained regression must assert its intended loss targets and progress facts rather than silently accepting a different scenario.

## Proposed disposition; policy decision pending

There is no evidenced production defect to fix. The existing mandatory randomized test expects success for a permitted loss sequence that demonstrably cannot meet its deadline. Increasing that deadline merely moves the tail risk; honoring the requested direction alone would leave the bidirectional random case vulnerable.

The proposed fix is to give mandatory CI a documented deterministic loss corpus with explicit success contracts, retain the unchanged one-third random loss policy as opt-in stress, and add this capture's expected timeout plus gap-delivery and ACK-delivery recovery controls as regression coverage. A maintained version should assert the data gap, missing ACK evidence and recovery outcome at stable protocol seams. It should not assert that every arbitrary loss sample succeeds, select a lucky seed as the diagnosis, suppress an unexpected failure, or change recovery timers.

This changes mandatory CI loss selection, so explicit agreement was requested under the user's preservation constraint. No such policy change is included in this investigation. The original random loss callback, deadlines, production recovery code and PR #144 merge state remain untouched. A maintained regression and fixture fix remain pending that decision.
