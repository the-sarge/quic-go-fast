# Ordinary receive policy for non-batch wrappers

Approved implementation contract for [issue #371](https://github.com/the-sarge/quic-go-fast/issues/371). This is one standalone maintenance PR, separate from the external packet-I/O architecture program.

## Outcome and acceptance criteria

Ordinary receive honors a supported wrapper's overridden method.

1. Change receive selection in `newConn`. Keep wrapper-provided `ReadBatch` first and descriptor batching for exact `*net.UDPConn` values. For currently accepted non-batch wrappers, use a private adapter that calls `ReadMsgUDP` once and passes its payload length, ancillary length, flags and source address into the existing packet decoder.
2. Preserve existing boundaries. Keep GRO disabled for these wrappers, even with receive permission. Preserve the current rejection of connections implementing neither `net.Conn` nor `ReadBatch`, native socket batching, participating-wrapper batching, send behavior, deadlines, buffer ownership and caller-owned socket lifetime.
3. Add focused regressions before implementation: queue foreign and selected ordinary datagrams through a filtering wrapper without `ReadBatch` and verify only selected data reaches the reader; verify ancillary metadata, source address and flags survive the adapter, including successive reads without stale metadata; verify read errors propagate, failed reads deliver no packet, and buffers are released; verify native and participating `ReadBatch` selection remains intact; retain the unsupported-wrapper rejection and Linux GRO-denial tests.
4. Update the public interface documentation to explain when batching applies and when wrappers receive through `ReadMsgUDP`.

## Boundary and representation

The supported domain is existing accepted OOB UDP connections on Darwin, Linux and FreeBSD. The private adapter receives the existing internal single-buffer message shape with zero input flags. Go interfaces select the receive path in `newConn`; the adapter maps `ReadMsgUDP` results; existing OS ancillary parsers and the packet decoder retain interpretation ownership. The guarantee is universal method dispatch within that domain, supported by finite behavioral regressions, with no claim to validate arbitrary wrapper implementations. The policy wrapper is trusted to implement its filtering correctly; the transport must invoke its chosen receive method, not bypass it via the socket descriptor.

The approved seams are connection receive selection, ordinary `ReadPacket` delivery through a supplied `ReadMsgUDP` wrapper, the message metadata boundary and receive-error buffer cleanup. Production changes are confined to `sys_conn_oob.go` and the interface documentation in `sys_conn.go`, with focused colocated tests and this contract. Tests observe filtering, metadata transfer, errors and ownership through those seams. Existing native read, batch, metadata and lifetime tests supply preservation evidence.

Receive dispatch and the adapter are shipped behavior and required policy enforcement. Tests are verification aids. The plan and receipts are process/traceability metadata. No new maintained verification tool or blocking verification-aid completeness obligation is approved.

Affected Linux wrappers read one datagram per call instead of descriptor batching; native sockets and participating batch wrappers retain their optimization. The implementation introduces no new workers, synchronization or shared state. There are no new wrapper shapes, GRO support for non-batch wrappers, send-path changes, Windows changes, dependencies, wire changes or performance campaigns. Keep the existing internal batch/storage owner rather than creating a second packet-decoding or buffer-lifetime path.

Contract closure is not triggered: bypassing a filtering policy is material, but the dispatch cases and adapter outcomes are small enough for focused tests. No expanded semantic matrix or mutation campaign is needed.

## Terminating evidence and execution

Run the named filtering/metadata/error/selection regressions, existing OOB lifetime and native-read coverage, and the Linux non-batch GRO-denial regression. Final local certification comprises affected root-package tests, focused race tests, `go vet .`, `go mod tidy -diff`, configured formatting/lint and diff checks on the exact pushed candidate. Existing hosted platform checks supply Linux behavior and the repository's supported platform/toolchain matrix; no additional native-platform campaign, stress repetition or performance gate is required.

Use one initial fully briefed RAS review, independent disposition of its findings, verification of accepted fixes, and at most one replacement review. Findings are bounded by the criteria and named regressions above; no stronger wrapper-validation or verification-harness completeness guarantee is implied. A representation mismatch, repeated precise semantic root under the shared policy, or required boundary/evidence expansion stops for a decision. Keep review chronology and certification receipts outside this normative contract.

Follow the [repository execution overlay](../REVIEW-LOOP.md): draft PR, exact-head local certification, successful applicable hosted checks, ready status and squash merge of the certified live head. After merge reaches remote main, append the dev journal without RAS, revalidate deferred findings and reconcile the issue and OmniFocus task `nc2d5sT5XaF`.
