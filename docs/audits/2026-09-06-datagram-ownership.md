# Datagram receive ownership audit

Status: candidate investigation. Base: `686bce6541438101c96732a023984e031faf6df7`. This is a standalone follow-up authorized by the owner's instruction to continue the received-payload ownership audit. D1/D2 remain completed; their plans and tracking are not reopened. Public API and wire compatibility remain governed by ADR 0001.

## Finding and candidate

Keep the queue's owning copy. The smaller change is borrowing the bounded packet payload in `wire.parseDatagramFrame` until synchronous connection handling admits or discards it. Admission already copies while holding the queue lock, before packet processing can return its buffer. This removes the parser payload allocation without adding a second queue API or imposing a new lifetime rule on applications.

The parser's internal lifetime contract changes: parsed DATAGRAM Data aliases its input and must not outlive input reuse without a copy. Document this on `DatagramFrame.Data` and `FrameParser.ParseDatagramFrame`; cap the borrowed slice to the payload length. The queue remains the lifetime boundary, including for directly constructed frames. This candidate is not a zero-copy public receive API.

## Audited owners

| Boundary | Source evidence at the base | Implication |
| --- | --- | --- |
| Wire parsing | `internal/wire/datagram_frame.go` validates length before allocating and copying; `FrameParser.ParseDatagramFrame` wraps errors without storing the frame | A bounded borrowed slice can replace the first copy after the same validation |
| Production parser caller | `Conn.handleFrames` is the only non-test caller of `ParseDatagramFrame`; it parses, logs and handles the frame in one synchronous loop | No production parser result is asynchronously handed to another consumer |
| Packet lifetime | `Conn.handleOnePacket` calls short/long header processing synchronously; normal packet release follows processing; queued undecryptable packets retain their packet ownership before parsing | Borrowing need not escape packet processing |
| qlog | `toQlogFrame` constructs a distinct `qlog.DatagramFrame` containing Length only before queue admission | Retained logging frames do not retain payload or packet memory |
| Debug logging | `wire.LogFrame` performs synchronous formatting; datagram values do not escape through a payload callback | No asynchronous debug observer requires an owning parser buffer |
| Admission | `Conn.handleDatagramFrame` validates frame size, then `datagramQueue.HandleDatagramFrame` checks capacity and copies under `rcvMx` | Caller/input reuse remains safe after admission; rejected payloads need no owning copy |
| Application | `ReceiveDatagram` delegates to queue Receive, which returns the admitted allocation | Retention and mutation of returned bytes remain independent of packet storage and other receipts |
| Nonproduction parser users | Unit tests, parser benchmarks and FuzzFrames use their input while examining or reserializing the parsed frame | No inspected test/fuzz consumer relies on an independently owned parsed payload |

Directly deleting the queue copy would violate `TestDatagramReceivePayloadOwnership`, which deliberately reuses and mutates its input after admission. Transferring parser ownership into a new queue path could be safe after a separate API audit, but adds an unnecessary ownership mode and still allocates before overflow admission. Borrowed parsing preserves the existing queue contract and avoids both problems.

## Preservation and finite verification

Preserve length-present and length-absent parsing, zero-length records, exact consumption and malformed/truncated errors; retain negotiation/encryption-level checks. The owning queue copy, FIFO, capacity 128, drop-new, notifications, close/cancellation precedence and independent application buffers must remain unchanged. No pool, queue representation change, send-side change, new dependency or public API is included.

Add a parser regression for bounded borrowing with present/absent length, empty data and trailing frames. Add connection-level characterization that poisons/reuses the packet bytes after parsing and retains/mutates returned datagrams, with qlog retention on and off. Add a connection overflow case proving a prefilled queue remains ordered after a parsed packet is discarded and reused. Reuse existing queue ownership, concurrency, cancellation, closure, malformed parser and real-QUIC datagram tests. Run the new tests before the candidate; the borrowing assertion is expected to fail, while public ownership characterization must already pass.

Run focused parser/connection/queue tests normally and under the race detector, the existing real-QUIC Datagram tests, and a bounded parser fuzz run. Compare the real parsing-plus-admission allocation path before and after with identical 1071-byte records; isolated queue benchmarks cannot measure the removed parser allocation. Retain the candidate as experimental until matched real-QUIC evidence and review justify adoption. No production merge or program completion is part of this audit receipt.
