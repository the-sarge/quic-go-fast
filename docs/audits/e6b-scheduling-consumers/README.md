# E6b scheduling and batching consumer preservation

The [E6b contract](../../adr/2026-09-07-packet-emission-plan.md#e6b--migrate-scheduling-and-batching-packer-consumers) governs this test-only migration. Production Go, module files, runtime hooks and CI configuration are byte-identical to the implementation parent. Tests are verification aids; this mapping and committed frontier transition are process metadata. No maintained harness or runtime guarantee is introduced.

| Existing consumer | Retained observable behavior |
| --- | --- |
| `testConnectionReceivePrioritization` | All five receives precede actual output before and after handshake; real Initial or 1-RTT construction replaces empty pack callbacks. The queue handoff occurs synchronously on the connection goroutine. |
| `TestConnectionPacketPacing` | Three decoded DATAGRAM payloads: first two at the same time, third after the existing 50 ms step, then a final pacing wakeup without more output. |
| `TestConnectionIdleTimeout` | Real PING output establishes the last-send time; idle timeout remains exactly 500 ms after that send in synctest. |
| `testConnectionKeepAlive` | Enabled keepalive emits a decoded PING at half the negotiated idle timeout; disabled keepalive reaches the existing idle error without output. |
| `TestConnectionACKTimer` | Actual output acknowledges packets 1 and 2, separated by the existing 500 ms alarm. Received-packet state sets the second deadline after the first ACK is consumed; no packer callback drives it. |
| `TestConnectionGSOBatch` | Four full segments with distinct decoded payloads form one ECT1 batch and stop when data is exhausted. |
| `TestConnectionGSOBatchPacketSize` | Three full segments and one segment one byte shorter end the first batch; the final distinct payload is sent in a second batch. The segment budget remains the connection MTU. |
| `TestConnectionGSOBatchECN` | Three full ECT1 segments precede a separate CE batch. The controlled upstream ECN input changes after the real recovery packet number reaches three. |
| `testConnectionSendQueue` | Ordinary and GSO run-loop capacity guards block after the first real output, leave the second DATAGRAM and packet number untouched, and resume on availability to emit that second payload. `TestEmissionFullQueueResume` retains the real queue/worker saturation scenario. |
| `TestEmissionFatalCallerBuffer` | Real append fails through the sealing manager on append one or two. Sequential empty-pool observation checks each allocation before reuse: ordinary second-append failure leaves the first buffer queued and releases the second; partial GSO releases its single large buffer. Queue occupancy, packet-number consumption and successful ACK of the prior registration retain no-refund evidence. |
| `TestEmissionEmptyCallerBuffer` | Both pool sizes release their single allocation, report no data/no progress and leave the queue empty. |

The assigned functions contain no broad packer expectations, and `emissionObservedPacker` is removed. Existing shared mock defaults and E6c/close consumers are retained. `schedulingRecoveryInputs` controls only existing send-mode, pacing-deadline, loss-alarm and ECN inputs; the original recovery owner retains packet numbers, registration and storage. The real packer, wire parser, frame sources, timers and emission path produce the asserted behavior. Pool observations run sequentially without a worker and restore the original constructor with matching capacity.

Evidence is example-level over these existing scenarios; the consumer-removal claim is checked over the finite named source census. Closure is not triggered. Focused tests, the prescribed focused race run, ordinary full suite, vet, existing lint/gcassert and inherited hosted checks supply the bounded validation; exact heads, commands, review dispositions and hosted receipts belong in the product PR discussion. Runtime byte identity discharges performance nonimpact. No additional platform, statistical repetition, performance capture or recursive fixture-completeness obligation is introduced.

On merge, E6b is complete, E6c remains the sole ready frontier, and E6 remains blocked by E6c. No newly ready successor is predicted. There are no admitted untraced runtime effects.
