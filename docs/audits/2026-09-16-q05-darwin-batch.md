# Q05 Darwin checked batch writer qualification

Q05 shares the existing Darwin submission owner with `UDPBatchWriterV1` and the managed endpoint's private writer. Explicitly registered callbacks retain destination authorization; the send worker retains retry, ordering, packet-specific error feedback and buffer disposal. The factory serializes its own bounded scratch and clears borrowed payload/OOB pointers before returning. No socket Close ownership changes.

## Domain and retained evidence

Native evidence was collected on Darwin 25.6.0 arm64 with Go 1.27.1. The [D1 adoption receipt](2026-09-12-d1-sendmsgx-results.md) remains the performance baseline for the unchanged kernel algorithm. Its recorded source `1af9bb81` was compared with Q05's base `b359a3e589e36c0f07ca0ea0a993e5f43623595a`: intervening changes include the receipt's disclosed progress classification, wrapper eligibility and zone handling fixes, plus explicit registration and fixed-peer dispatch. Q05 does not alter `sys_conn_sendmsg_x_darwin.go`, `sys_conn_sendmsg_x_qual_darwin.go`, the allowlist, startup self-check or private-syscall opt-outs.

The standard library owns UDP/address semantics. Acceleration admits the existing unconnected native batch shape, with two through `maxSendBatch` nonempty datagrams, one valid UDP address and one shared OOB block. Connected sockets, empty datagrams, larger batches and addresses outside that shape retain standard writes. Runtime enforcement is universal within the declared API domain; the following tests are finite regression evidence. No new OS qualification or assembled performance claim is made; E02-D owns that comparison.

## Finite coverage

| Semantic class | Enforcement owner | Evidence | Disposition |
| --- | --- | --- | --- |
| Qualified external wrapper; wrong peer | Checked callback then shared Darwin writer | `TestExternalDarwinBatchWriterEngagement`: ordered delivery, native counter increase, rejected peer causes no native submission | Covered |
| Disabled/unqualified capability | Existing qualification owner; ordinary factory fallback | `TestExternalDarwinBatchWriterFallback`; existing opt-out build regressions | Covered |
| Two connection callbacks; distinct IPv4/IPv6 destinations and OOB | Factory mutex and shared native scratch | `TestExternalDarwinBatchWriterConcurrent` under race detector, exact delivery and ECN preservation | Covered |
| Standard UDP shapes and invalid addresses | Standard `WriteMsgUDP` fallback | `TestUDPBatchWriter`, `TestExternalDarwinBatchWriterStandardInputs` | Covered |
| Full/short/zero/invalid prefix and unknown-progress errors | Existing native classifier and send worker | `TestSendmsgX*`, `TestExternalPacketIOBatchProgress`, `TestSendQueueBatch*` | Covered |
| Packet-specific MTU feedback and suffix delivery | Existing send worker | `TestExternalPacketIOMessageSizeFeedback`, `TestSendQueueBatchPartialAcceptanceMsgSizeFeedback` | Covered |
| Cancellation, terminal failures, release and lease-close joining | Existing queue and endpoint lifecycle owners | `TestManagedPacketIODelayedQueuedSend`, `TestManagedPacketIOConcurrentBatchClose`, queue failure/disposal regressions | Covered by retained regressions |
| Arbitrary wrapper unwrapping, new OS qualification, raw receive handback and performance campaigns | Outside Q05 | Existing opaque-wrapper preservation; separate program slices | Explicit non-goals |

The new engagement test failed before implementation because the factory made zero native submissions, and passed after sharing native submission. No guard mutation is necessary: the red engagement regression and retained native classifier/failure tests provide the admitted evidence. Tests are verification aids, not a new maintained analyzer or recursive closure obligation. There are no uncovered in-contract matrix cells or newly untraced effects.

## Validation boundary

The focused native/race command is `go test -race . -run '^(TestExternalDarwin.*|TestUDPBatchWriter|TestExternalPacketIOBatch.*|TestExternalPacketIOMessageSizeFeedback|TestSendmsgX.*|TestSendQueueBatch.*|TestManagedPacketIO.*)$' -count=1`. The private-syscall opt-out uses `go test -tags quic_go_no_private_syscalls . -run 'TestExternal|TestUDPBatchWriter|TestManagedPacketIO|TestSendQueueBatch' -count=1`. Ordinary local certification includes `TIMESCALE_FACTOR=3 go test ./...`, `go vet .`, `go mod tidy -diff`, lint and clean-diff checks. Exact-head certification, review dispositions and hosted receipts belong to the product PR discussion; this receipt does not predict their identities.

An initial broad-suite invocation used the unit workflow's `TIMESCALE_FACTOR=10` for integration tests too. Two corruption-diagnostics fixtures then observed the default 5-second handshake idle timeout before their scaled 10-second context deadline, contradicting their expected context-deadline text. The integration workflow declares factor 3; certification uses that supported setting. This invocation error did not require a product or fixture change.
