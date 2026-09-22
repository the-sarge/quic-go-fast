# Darwin 27 arm64 batch compatibility protocol

Status: approved for one bounded collection; runtime admission requires pass. Issue: [#516](https://github.com/the-sarge/quic-go-fast/issues/516). Governing policy: [Darwin batch compatibility](../../darwin-batch-compatibility.md). Base: `67c5232464364e6d453fb41f5d51dd4f54d600cd`. The user approved this plan on 2026-09-22. This protocol is committed before qualification collection; the exact prepared candidate SHA and overlay hashes must be recorded in `identity.json` before execution.

## Outcome and acceptance criteria

- Qualify the existing private sendmsg_x implementation on Darwin 27 arm64 using one finite native run and publish pass, fail or inconclusive with exact candidate, host, commands, outputs, executed cases, skips and remaining gaps.
- Only pass permits Darwin 27 arm64 admission. Darwin 27 amd64 remains unqualified. Preserve Darwin 25's existing architecture admission, startup self-check, kill switch, process latch, accepted-count checks, ordinary fallback, socket authority, public API and wire behavior.
- Assert successful production self-check, increased native submission counters and exact peer-observed datagrams/metadata for required native paths. Skips, forced capability flags and spoofed kernel identity are not qualification evidence.
- Establish target-kernel partial-count and zero-progress error assumptions. Distinguish native observations, injected caller behavior and source-derived semantics; missing support for an assumption is inconclusive.
- Preserve frozen D1 evidence. Do not add throughput thresholds, benchmarks, statistical repetition, broad product fixes or a maintained qualification framework.

## Scope and representation

The target host reports macOS 27.0 build 26A428, Darwin 27.0.0, XNU 13432.1.9~1/RELEASE_ARM64_T6041, arm64 and Go 1.27.1. Recheck immediately before collection; an identity mismatch requires a protocol revision before collecting. Only arm64 receives native qualification. Darwin amd64 and iOS checks establish build compatibility and exclusion, not native qualification.

Production changes are limited to architecture-aware admission and its direct callers. Each allowlist entry records product evidence and an optional architecture constraint; the historical Darwin 25 entry remains unrestricted. Tests use the same predicate as production. Test-only activation uses Go's overlay facility to insert a Darwin 27 arm64 admission entry into the prepared source, while keeping real uname lookup, the production self-check and syscall/error paths intact. Archive the complete replacement source as `admission.go.txt` and its overlay JSON; no experimental activation switch enters ordinary builds. Do not execute native tests while fake syscall or kernel hooks are installed. Existing injected tests restore their state serially.

The representation owners are the production sockaddr and control-message encoders, syscall adapter and target kernel. The admitted data domain is existing unconnected UDP batch sends and supported transport, external writer, managed lease and synchronous exact-forwarding routes. Compatibility evidence is example-level for the declared host/cases, not a proof of the private ABI on all patches. Admission table tests exhaust the finite declared kernel/architecture cases. Third-party address parsing is unchanged.

Runtime activation is shipped behavior; admission is required safety enforcement. Tests and the one-run overlay are verification aids. They support this qualification but are not a new maintained harness or a recursive completeness obligation. Retire the overlay from execution after this run, preserving its exact bytes as frozen evidence. Protocol, identity, logs and results are traceability metadata. Maintained regression tests remain with the existing package tests.

## Bounded semantic matrix

Closure is triggered for mistaken admission and retry safety: duplicate or lost datagrams have a material consequence across multiple independently reachable send paths. The following classes bound the obligation; no fixture Cartesian product is implied.

| Class | Expected behavior / owner | Required evidence |
| --- | --- | --- |
| Admission | Reject unlisted major/architecture; preserve 25; only declared 27 arm64 test overlay admitted; sendmsgXQualify owns activation | TestSendmsgXArchitectureAdmission and existing kill-switch/allowlist tests; one architecture-guard bypass mutation must fail the new test |
| ABI and addresses | Production msghdrX/destination encoders preserve bytes and destinations | TestSendmsgXMsghdrXLayout, TestSendmsgXDestSockaddr, TestSendmsgXSelfCheckAgainstRunningKernel |
| Native families and metadata | Assert batch engagement, exact payloads and ECT0 for udp4, udp6, dual-stack mapped IPv4 and native IPv6 | TestDarwinManagedECNNativeBatch; TestDarwinManagedECNReceive and TestDarwinManagedECNSendRoutes preserve other existing ECN marks/routes |
| Full and partial progress | Raw adapter returns count; submission/worker retry only unsent entries and attribute message-size feedback correctly | New TestSendmsgXNativeMessageSizeProgress (oversized-first, accepted-prefix); TestExternalPacketIOMessageSizeFeedback; TestSendQueueBatch* |
| Unknown/unavailable/invalid progress | Conservative fatal result or process latch, without unsafe retry; sendmsgXSubmit owns partition | Existing TestSendmsgXENOSYSLatch, TestSendmsgXStructuralLatch, TestSendmsgXZeroProgressErrnoDoesNotLatch, TestSendmsgXUnknownProgressErrnoIsFatal; injected evidence only |
| Authority and integration | Native ordinary transport, fixed peer and external writer engage; foreign peer/custom wrapper restrictions preserved | TestSendmsgXBatchSendEndToEnd, TestSendmsgXCustomConnKeepsWriteMsgUDP, TestFixedPeerNativeBatch, TestExternalDarwinBatchWriter* |
| Lifecycle | Deadlines, close, supported concurrency, stale lease and reacquisition retain ownership | Existing external/managed deadline and close tests; TestManagedEndpoint*, TestManagedPacketIO*; new TestSendmsgXManagedLeaseReacquisition with native counters and exact delivery before/after reacquisition |
| Fallback/builds | Disabled/unqualified/opt-out paths deliver ordinarily with no native submission; iOS excludes private path | Existing TestSendmsgXDisabledPathInert and TestExternalDarwinBatchWriterFallback plus architecture-excluded variant; TestDarwinManagedECNNativeBatch/disabled; tagged TestUDPBatchWriter and TestManagedPacketIOBatchDatagrams; file selection and cross-build logs |

Target XNU source must be pinned by revision and mapped to the running build. The public Apple XNU tag inventory will be captured once. A missing target source cannot be replaced by Darwin 25 or unrelated latest-source semantics. Native oversized-first and accepted-prefix observations establish EMSGSIZE behavior only; they cannot establish EAGAIN, EINTR or ENOBUFS suppression. If these remain unsupported, publish inconclusive even when tests pass.

## Preparation and finite commands

Before collection, commit the architecture predicate and missing regression tests, then record clean HEAD and create the overlay from those exact bytes. Development-only admission unit tests and one guard mutation are preparation evidence, not native collection. Do not run new native cases before the candidate is recorded. The qualification overlay adds only `27: {product: "test-only Darwin 27 arm64 qualification", arch: "arm64"}` to the candidate allowlist. The recorded replacement source must otherwise match exactly. Overlay paths are absolute for this worktree; replays must remap paths while preserving replacement bytes.

Use `go test -json -count=1 -timeout=10m -overlay=<absolute overlay.json>` for the following native commands, sequentially on the same candidate:

1. Focused matrix: `go test ... -run 'Test(SendmsgX|FixedPeerNativeBatch|ExternalDarwin|DarwinManagedECN|SendQueueBatch|ExternalPacketIOMessageSizeFeedback|ManagedEndpoint|ManagedPacketIO)' .` once.
2. Root package: `go test ... .` once.
3. Root race gate: `go test ... -race .` once.

The intentional overlap does not authorize extra repetitions. Required native cases must execute in the focused run; catalog all skips and distinguish non-required platform skips in package runs. JSON output supplies names and results. Assertion failures and race reports are failures; environmental inability to execute is inconclusive. Keep logs and exit status even on failure. Continue only the remaining already-declared commands to document the bounded result; never retry a failed cell during collection.

Run without the qualification overlay, once each:

- `go test -json -count=1 -timeout=10m -tags quic_go_no_private_syscalls -run 'Test(UDPBatchWriter|ManagedPacketIOBatchDatagrams|ExternalPacketIOMessageSizeFeedback)' .`
- `GOOS=darwin GOARCH=arm64 go build ./...`
- `GOOS=darwin GOARCH=amd64 go build ./...`
- `GOOS=darwin GOARCH=arm64 go build -tags quic_go_no_private_syscalls ./...`
- `GOOS=darwin GOARCH=amd64 go build -tags quic_go_no_private_syscalls ./...`
- `.github/workflows/cross-compile.sh ios/arm64`
- Record `CGO_ENABLED=0 GOOS=ios GOARCH=arm64 go list -f '{{.GoFiles}}' .` and the native opt-out equivalent; neither may select the active sendmsg_x files.

Use `go vet .`, `go mod tidy -diff` and `git diff --check` for candidate certification. Logs go beside this protocol, with a command/exit-status manifest. Final exact-head certification may reuse the completed native evidence when only documentation changes; repeat lightweight docs checks and ordinary admission unit tests as needed for the final SHA. Product changes other than the conditional admission entry invalidate evidence and require an explicit protocol revision and bounded revalidation decision.

## Disposition and completion

Pass requires every required case, supported kernel assumptions, preserved fallback and admission matching native architecture evidence. After pass only, add the production Darwin 27 arm64 entry with identity/results link and run the production self-check, admission, native engagement and excluded-fallback tests once without overlay. This admission-only check does not repeat the campaign. Fail records a contradicted requirement; inconclusive records missing/skipped/ambiguous evidence. Both retain fallback and leave #516 and its OmniFocus task open with current blockers.

The review budget is one initial RAS review and at most one replacement after accepted fixes, with verification scoped to accepted findings. Brief reviewers with these criteria verbatim. Do not promote verification-aid polish, alternate encodings or adjacent defects into product blockers. Stop for a broader product fix, representation mismatch, repeated precise semantic root, evidence-budget expansion or invalidated candidate; preserve the evidence and request the necessary revised decision.

Follow [the repository execution overlay](../../REVIEW-LOOP.md): draft PR, bounded review, exact-head local certification, applicable existing hosted checks, ready, squash merge matching the live head. No task preflight or ci-* workflow is invented. Journal only after merge; revalidate deferred findings against merged HEAD and update issue/OmniFocus pointers. The implementation context is this current contract, the maintained compatibility policy, relevant source/tests and unresolved evidence gaps; historical review chronology belongs in separate receipts.
