# Datagram receive ownership audit

Status: ownership audit and matched native-QUIC comparison complete; experimental candidate locally validated, with higher-rate tail noninferiority unresolved and review outstanding. Base: `686bce6541438101c96732a023984e031faf6df7`. This is a standalone follow-up authorized by the owner's instruction to continue the received-payload ownership audit. D1/D2 remain completed; their plans and tracking are not reopened. Public API and wire compatibility remain governed by ADR 0001.

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
| Debug logging | `wire.LogFrame` passes DATAGRAM to the internal logger; the production `defaultLogger` formats synchronously through `log.Printf`, with no public asynchronous frame observer | No asynchronous debug observer requires an owning parser buffer |
| Admission | `Conn.handleDatagramFrame` validates frame size, then `datagramQueue.HandleDatagramFrame` checks capacity and copies under `rcvMx` | Caller/input reuse remains safe after admission; rejected payloads need no owning copy |
| Application | `ReceiveDatagram` delegates to queue Receive, which returns the admitted allocation | Retention and mutation of returned bytes remain independent of packet storage and other receipts |
| Nonproduction parser users | Unit tests, parser benchmarks and FuzzFrames use their input while examining or reserializing the parsed frame | No inspected test/fuzz consumer relies on an independently owned parsed payload |

Directly deleting the queue copy would violate `TestDatagramReceivePayloadOwnership`, which deliberately reuses and mutates its input after admission. Transferring parser ownership into a new queue path could be safe after a separate API audit, but adds an unnecessary ownership mode and still allocates before overflow admission. Borrowed parsing preserves the existing queue contract and avoids both problems.

## Preservation and finite verification

Preserve length-present and length-absent parsing, zero-length records, exact consumption and malformed/truncated errors; retain negotiation/encryption-level checks. The owning queue copy, FIFO, capacity 128, drop-new, notifications, close/cancellation precedence and independent application buffers must remain unchanged. No pool, queue representation change, send-side change, new dependency or public API is included.

Add a parser regression for bounded borrowing with present/absent length, empty data and trailing frames. Add connection-level characterization that poisons/reuses the packet bytes after parsing and retains/mutates returned datagrams, with qlog retention on and off. Add a connection overflow case proving a prefilled queue remains ordered after a parsed packet is discarded and reused. Reuse existing queue ownership, concurrency, cancellation, closure, malformed parser and real-QUIC datagram tests. Run the new tests before the candidate; the borrowing assertion is expected to fail, while public ownership characterization must already pass.

Run focused parser/connection/queue tests normally and under the race detector, the existing real-QUIC Datagram tests, and a bounded parser fuzz run. Compare the real parsing-plus-admission allocation path before and after with identical 1071-byte records; isolated queue benchmarks cannot measure the removed parser allocation. Retain the candidate as experimental until matched real-QUIC evidence and review justify adoption. No production merge or program completion is part of this audit receipt.


## Results

The characterization base is `8f883e3d4b56a42cfc243aa357173d482aa07142`; it adds the audit and tests but keeps production code identical to main. The candidate runtime commit is `391b3df6` and changes only the parser payload assignment plus internal lifetime documentation. The queue and connection runtime code are byte-for-byte unchanged. The borrowing assertion failed on both nonempty wire forms before the patch; packet-reuse and overflow ownership characterization already passed. An initial empty-case assertion compared nil versus empty slices rather than payload content; that test-only mismatch was corrected before the refined baseline and candidate runs.

All five samples per case on each host reported the following allocation counts for the same parser-plus-admission loop:

| 1071-byte input | Baseline bytes/op | Candidate bytes/op | Baseline allocations/op | Candidate allocations/op |
| --- | --- | --- | --- | --- |
| Admitted, immediately drained | 2360 | 1208 | 4 | 3 |
| Full queue, dropped | 1184 | 32 | 2 | 1 |

The reduction is exactly one 1152-byte allocator block per parsed payload: 48.8% fewer allocated bytes in the admitted parser/queue loop and 97.3% fewer on overflow. A DATAGRAM frame object still allocates, so this is not zero-allocation parsing or overflow. This is a per-operation microbenchmark result, not full receiver-process allocation or network throughput.

The Mac used Go 1.27.0, darwin/arm64, Apple M4 Max, GOMAXPROCS 16; minimax used its newly installed Go 1.27.1, linux/amd64, Ryzen AI Max+ 395, GOMAXPROCS 32. Each baseline/candidate group ran five 100 ms benchmark samples. The tests establish allocation removal; their sequential, unreserved CPU timing does not establish an application speedup, tail-latency result or fixed-regression-threshold acceptance. For transparency, Linux median ns/op was 246.8 → 139.4 admitted and 122.4 → 21.01 overflow; Mac medians were 314.7 → 146.6 and 160.4 → 12.3. The Go benchmark's MB/s field counts input bytes processed or discarded and must not be reported as delivered traffic.

Validation completed:

- On macOS, all focused Datagram tests in the root/wire packages passed normally and under the race detector, including the new borrowing and packet-reuse cases. Existing real-QUIC Datagram integration tests passed. `FuzzFrames` passed a bounded 20-second, four-worker run with 695,019 executions.
- On minimax Go 1.27.1, baseline Datagram preservation tests passed with the intentionally failing borrowing test excluded. The candidate passed the complete root and internal/wire package tests, then focused root/wire/real-QUIC Datagram tests under the race detector.
- Existing disabled-Datagram negotiation, malformed/truncated lengths, frame conversion, empty datagrams, FIFO, copy ownership, concurrent producer/drainer, cancellation and close tests remain passing. New qlog-on/off connection tests retain records while packet storage and application buffers are mutated.

The [raw receipts](datagram-ownership/README.md) retain before/refined-before failures, baseline characterization, both hosts' allocation samples, race/integration results and fuzz output. Their [summary](datagram-ownership/summary.json) reports each sample group's allocation values and timing medians. Source archives and Linux logs remain under `/home/josh/benchmarks/quic-go-fast/datagram-ownership-20260906`.

## Disposition

The lifetime audit supports borrowed parsing with the existing queue as the sole owning-copy boundary, and the candidate removes the expected allocation while passing the stated preservation evidence. The [matched native-QUIC comparison](2026-09-06-datagram-native-comparison.md) on minimax Go 1.27.1 found about 47% lower receiver allocation and 2.1–4.8% lower receiver CPU per delivered byte, with practically unchanged goodput. Its 29 complete pairs establish the 5% tail bound only at 100 Mbps; the 1 and 4 Gbps intervals remain inconclusive. Keep the candidate experimental pending tail disposition and review. Sender-generation misses remain explicit and cannot be relabeled as queue loss. Neither comparison silently revises completed D1/D2 gates or turns microbenchmark MB/s into delivered throughput.

Full repository certification, hosted checks, independent review and merge remain outstanding for adoption. No runtime change is on the default branch, no queue candidate was substituted, and no program or OmniFocus task was completed by this follow-up.
