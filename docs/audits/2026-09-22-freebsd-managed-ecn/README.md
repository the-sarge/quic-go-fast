# FreeBSD managed ECN qualification

F1 (`QGF-ECN-20260922/F1`, [#505](https://github.com/the-sarge/quic-go-fast/issues/505)) is supported on the qualified FreeBSD native domain. The [normative plan](../../adr/2026-09-22-freebsd-managed-ecn-plan.md) owns the contract; this record owns evidence and implementation chronology.

## Native evidence

Run `run-20260922T233325.537976000Z` qualified candidate `f5a3f0e665e651acf6c7186f03532d5bd78f7e2d` on FreeBSD 15.1-RELEASE-p3 (`releng/15.1-n283611-88e7371d9dc2`, amd64), Go 1.27.0, in the existing infra test VM on minimax. The [compressed original receipt](qualification-receipt.json.gz) includes the workload transcript, exact candidate, driver identity, dependency digest, lifecycle facts and successful finalization. Its decompressed SHA-256 is `7b5c764d9b0f420f65055e6b90a653de4df90f2cf268da1f4e5d077a23041a87`. The VM was powered off, its baseline restored, and the run lock released. Exact final-PR-head certification belongs in the PR discussion, so this frozen receipt does not recursively change the candidate being certified.

All four native domain rows passed without family skips: `udp4` IPv4; `udp6` IPv6 with `IPV6_V6ONLY=1`; and `udp` dual stack with `IPV6_V6ONLY=0`, exercised through both IPv4-mapped and IPv6 traffic. FreeBSD rejects `IP_RECVTOS` on AF_INET6 with `EINVAL`, but `IPV6_RECVTCLASS` preserves the mapped IPv4 marks through the existing OOB decoder. The FreeBSD setup hook therefore attributes both admitted families of AF_INET6 to that socket's IPv6 ancillary setup. No second decoder, raw handoff or acceleration was added.

## Bounded coverage

The invariant is that managed ECN is advertised only when the endpoint-owned representation preserves and applies the exact mark through every admitted route. The existing L1 adapter remains the correlation and capability owner; native OOB helpers and the FreeBSD setup hook own ancillary admission. Guarantee: universal within the plan's typed factory/lease and kernel-message domain, with example-level evidence for the declared synchronous wrapper behavior.

| Semantic class | Disposition and evidence | Status |
| --- | --- | --- |
| Receive values and admitted families | `TestManagedFreeBSDECNReceive` exercises Not-ECT, ECT(0), ECT(1), CE on direct and checked-wrapper routes in all four domain rows; `TestManagedFreeBSDNativeRepresentation` separately checks the native path | Covered |
| Full ordinary datagrams, truncation and release | `TestManagedFreeBSDFullDatagramsAndRelease` covers 0, 1452, 2000 and 65507 bytes, short caller-buffer truncation, a queued datagram across release, reacquisition and stale operations | Covered |
| Ordinary and batch marked send, selected/foreign peer | `TestManagedFreeBSDECNSend` observes all ordinary marks and a two-datagram CE batch at a native receiver, rejects foreign ordinary/batch destinations, and preserves the definite prefix on an oversized second datagram | Covered |
| Checked singleton results | `TestManagedFreeBSDSingletonResults` preserves success, EMSGSIZE, terminal errors and one-call EPERM without Linux retry; rejects altered callback counts; shared L1 tests cover result normalization and concurrent singleton association | Covered |
| Family-complete setup, opt-out and optional failure | `TestManagedFreeBSDSetupFallback` checks each native family with opt-out, independently failed family setup, both setup failures and fatal descriptor failure; ordinary fallback stays usable | Covered |
| Managed capability projection | Native fixture asserts ECN without GRO/GSO or normalization; `TestManagedPacketECNProjectsOnlyManagedCapabilities` and the updated diagnostic provenance table preserve the shared projection boundary | Covered |
| Missing/malformed/truncated metadata | `TestManagedFreeBSDMetadataFailures` and existing OOB ancillary tests cover absent metadata, malformed messages, payload truncation and ancillary truncation | Covered |
| Lease revocation, Close and concurrency | Full-datagram table, `TestManagedFreeBSDReadCloseRace`, existing joined-I/O/ConcurrentBatchClose and exact-correlation tests, plus one native managed `-race` gate | Covered |
| Other platforms and public boundaries | Darwin, FreeBSD and OpenBSD cross-builds select exactly one receive setup; production diff is the FreeBSD setup hook and two build constraints | Covered |

The eight new table-driven tests stay within the ten-function budget. The shared Linux-only correlation-join subtest retains its existing skip; it is not claimed as native FreeBSD evidence. Native FreeBSD cleanup and the existing platform-independent joined-I/O tests cover the admitted lifecycle cases. No in-contract cell is left uncovered; other platforms, acceleration, raw/public authority, throughput, parser grammar campaigns and additional repeated stress remain non-goals. No correlation mutation was required because this change does not alter the L1 guard; its existing negative regressions remain in the focused suite.

## Validation and artifacts

The driver [qualify.sh](qualify.sh) ran the focused managed/OOB/ECN tests, the whole affected root package, `go vet .`, and `CGO_ENABLED=1 go test -race -count=1 -timeout=5m . -run 'TestManaged'`, all successfully. Local affected-package tests, `go vet`, `go mod tidy -diff`, FreeBSD static analysis, and Darwin/FreeBSD/OpenBSD cross-builds also passed. The repository's existing hosted checks remain required on the final PR head.

Runtime setup is shipped behavior; existing correlation, generation and family gates are required safety enforcement. Native tests and the one-shot driver are verification aids, not independent maintained products or recursively complete harnesses. This receipt and record are process metadata. Freeze this audit directory after slice closure; do not turn its VM recipe into a new CI or product dependency. The runtime and regression-test owners remain the managed endpoint and native OOB code. Transitional seams: zero. Untraced effects: none accepted or discovered.

## Execution history

- `c607766d13027edfb1933b950dab16447cd63f67`, run `run-20260922T231925.705430000Z`: native red baseline rejected managed capability in all four domain rows while the unsupported stub was present.
- `f62760f83340fee7b6693e74edc08c2363720eb6`, run `run-20260922T232200.966573000Z`: IPv4/IPv6 single-family receive passed; the initial gate rejected mapped IPv4 setup on AF_INET6.
- `006b0a602018e06964dd66b62b42ce82a52c91ef`, run `run-20260922T232520.135832000Z`: independent native receive, ordinary-send and batch-send tables passed every row, establishing the IPv6 ancillary mapping used by the corrected setup hook.
- `99e005116592bca07e041c53fa3253ea37691b54`, run `run-20260922T233027.190926000Z`: behavior tables passed except verification-fixture issues: an old diagnostic expectation omitted FreeBSD, the plain peer's default send buffer could not submit the largest test payload, and pointer identity was incorrectly used for a value-type terminal error. The fixes updated the expectation, enlarged only the test peer's send buffer and checked exact error equality.
- The successful qualification receipt above closes those issues and adds the metadata failure table. The initial controller-toolchain mismatch occurred before native execution and is not product evidence.

Dispatch used the current F1 section plus non-goals, program frontier/binding rules, ADRs 0006/0007, the shared baselines and repository overlay, and the named code/test declarations. The bounded manifest measured 93,575 bytes (approximately 23,400 input tokens), within 24,000. Review budget: one initial review, at most one replacement after accepted fixes. Later review dispositions and exact-head receipts belong in the PR discussion.
