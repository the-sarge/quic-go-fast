# R01-L Linux managed receive evidence

R01-L installs the existing Linux `oobConn` decoder inside the lifetime-stable endpoint before enabling UDP_GRO. Factory leases and policy wrappers retain their ordinary `ReadFrom` interface: each call copies one decoded datagram into the caller's buffer. No raw descriptor, alternate wrapper path, second decoder or new native algorithm is introduced. Normalization persists after QUIC use, including when a later lease is ordinary. Already consumed QUIC storage is discarded after joining I/O; kernel-queued data is left for the next normalized read. Terminal return releases storage even when deadline restoration failed on an ordinary lease.

## Native domain and reuse

The finite correctness run used the existing `wiremux-e01-l` Linux 6.8.0-134-generic arm64 VM, Go 1.27.1, native IPv4 and IPv6 loopback, and UDP_SEGMENT-generated aggregates. The fixture observes actual buffered GRO siblings, rather than inferring engagement solely from a successful socket option. This establishes local normalization, filtering and handback semantics, not physical-network capacity or a performance result. E02-L owns the assembled comparison.

The [G2 adoption receipt](2026-09-11-g2-gro-results.md) remains historical evidence for its measured native algorithm and domain. Since its measured revision, Q03 hardened ancillary validation and receive-storage cleanup; those source differences are covered by the existing Q03 regressions. R01-L changes none of `sys_conn_oob.go`, `sys_conn_helper_linux.go`, `coalesced_delivery.go`, `coalesced_slab.go` or `buffer_pool.go` relative to its current default-branch base. It composes those owners with the endpoint lifecycle and does not rerun the adoption campaign or claim the historical throughput for this managed path.

## Finite evidence

| Contract class | Evidence |
| --- | --- |
| Queued aggregate at release, parent read, next lease; independently mutable public addresses and short-tail preservation | `TestManagedReceiveQueuedAcrossHandback`, native IPv4 and IPv6 |
| Selected/foreign traffic, actual aggregate engagement, no consumed-tail replay, bounded storage | `TestManagedReceiveDiscardsConsumedQUICStorage`; endpoint storage stays within eight 65535-byte backing buffers, excluding caller-owned copies and existing pool caches |
| Truncated metadata rejected before publication | `TestManagedReceiveRejectsTruncatedMetadata`, plus existing `TestExternalGRORejectsInvalidRead` and partial-batch tests |
| Retained sibling lifetime | Existing `TestExternalGRORetainedSibling`; public endpoint reads copy one datagram and do not export slab references |
| Logical deadlines with buffered siblings | `TestManagedReceiveBufferedDeadline` plus existing endpoint deadline-restoration tests |
| Cancellation/returning, serialized active readers, parent Close | `TestManagedReceiveCloseJoinsReaders`, existing generation and active-I/O joining tests |
| Terminal restoration failure | `TestManagedReceiveFailedRestoreDisposesStorage` plus the existing interrupt/read/write restoration-failure table |
| Explicit provenance, disabled fallback and normalized diagnostics | `TestManagedReceiveDiagnostics`, `TestManagedReceiveRetainedDiagnostics`, existing registration/provenance tests |
| Initialization failure after normalization, stale writers and actual QUIC use | Existing `TestManagedPacketIOFailedInit`, delayed queued send, concurrent batch-close and probe-then-QUIC regressions |

The native focused command is `CGO_ENABLED=1 go test -race . -run '^TestManaged|^TestExternalGRO' -count=1 -v`, followed by root package tests and vet. The host suite uses `TIMESCALE_FACTOR=3 go test ./...`, affected-package vet, module tidiness and repository lint. Hosted workflows provide their existing OS/compiler and integration coverage. No extra fuzz, repetition, mutation campaign or maintained verification aid is introduced; the red regressions directly establish sensitivity to the changed behavior.

## Development observations

The first native regression failed because registration left UDP_GRO disabled, then passed after normalization was installed. The buffered-deadline regression failed by returning a sibling after timeout, then passed with logical deadline admission. The terminal-restoration regression failed by retaining a consumed slab, then passed with cleanup after either QUIC return or terminal failure.

The initial copied test binary needed the repository's certificate fixtures at its compiled source path; the native test snapshot supplied them. The VM initially lacked a C compiler for race instrumentation; GCC and libc headers were installed and the native race run used `CGO_ENABLED=1`. An initial full local suite used unit timing factor 10; two integration diagnostics reached the fixed idle timeout before their expected context timeout. The corrected run uses the integration workflow's factor 3. These setup failures are retained as limitations of the initial invocations, not omitted or treated as product fixes.

Exact review heads, validation results and hosted run receipts belong to the product PR discussion. The normative plan records the committed completion/frontier transition; this receipt does not predict a merge identity.
