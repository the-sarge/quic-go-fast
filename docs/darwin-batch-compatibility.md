# Darwin batch-send compatibility qualification

This maintained policy governs extending the existing private `sendmsg_x` batch-send implementation to additional Darwin kernels. [ADR 0008](adr/0008-darwin-batch-compatibility.md) separates this compatibility exercise from the original D1 performance-adoption experiment. Keep the kernel allowlist, production-shape startup self-check, accepted-count checks, process latch, kill switch and ordinary-send fallback. A kernel version is an admission boundary, not proof of compatibility.

## When to qualify

Qualify before admitting a new Darwin kernel major. Revisit an existing qualification when a relevant syscall ABI or semantic change, implementation change, or compatibility regression invalidates its evidence, including changes within a kernel major. An ordinary library release or OS patch alone does not mandate repeating qualification. Investigate known regressions and narrow or disable admission where necessary; a previous pass is not permission to ignore contradictory evidence.

Use one issue for the target kernel and link this policy. Darwin 27 issue [#516](https://github.com/the-sarge/quic-go-fast/issues/516) is the first application. It is separate from the completed Darwin 25 managed ECN qualification in [#504](https://github.com/the-sarge/quic-go-fast/issues/504).

## Declare the finite protocol

Commit a new protocol under `docs/audits/` before collecting qualification evidence. Record the base commit, target kernel major, exact macOS product/build and Darwin release, intended architectures, Go toolchain, commands, required test cases, test-only activation mechanism, output locations and pass/fail/inconclusive rules. Record the exact candidate SHA before execution and in the results. If an activation overlay is needed, record its exact bytes and base SHA so the tested tree is reproducible.

Map each row below to existing behavioral tests and identify gaps before collection. Run each declared native matrix case once, the relevant package suite once, and its race gate once per intended native architecture; overlap between the focused matrix and package gates is intentional. Run the declared cross-build and opt-out checks once. Reuse existing tests and add only missing behavioral coverage. Required native skips or missing hosts produce an inconclusive result, not a pass. Do not add benchmarks, repetitions, platforms or fixture combinations during collection to rescue a result.

The runtime admission must match the native architecture evidence. The existing allowlist is keyed only by kernel major; an arm64-only qualification cannot justify adding a new entry that also enables amd64. Either provide native evidence for every architecture the new admission enables or scope the new admission by architecture and test the excluded fallback. Cross-compilation establishes build compatibility only. This rule does not retroactively expand the historical Darwin 25 evidence or silently change its shipped admission.

## Exercise the real path before admission

Use test-only admission for the actual target kernel, retaining the real production-shape self-check and runtime error handling. Do not spoof the host as Darwin 25, force the qualified flag, substitute a fake syscall for native evidence, or ship an experimental bypass. Restore test state and keep forced admission out of ordinary builds. The self-check must be asserted successful: its existing informational result on an unlisted kernel is insufficient by itself.

Verify native engagement through increased batch-submission counters together with exact peer-observed datagrams and metadata. Capability availability or a green suite containing skipped native cases is insufficient. In disabled, unqualified and opt-out cases, verify ordinary sends still deliver correctly and no native batch submission occurs.

## Required correctness matrix

| Area | Required evidence |
| --- | --- |
| Representation | ABI layout, correct delivered payloads and destinations, accepted counts, and outgoing/received ECN marks through the supported paths. |
| Address families | IPv4-only, IPv6-only, and dual-stack sockets carrying mapped IPv4 and native IPv6 traffic. All admitted usable families must execute. |
| Progress and errors | Full and partial acceptance, oversized datagram/message-size attribution, no retry of accepted entries, conservative handling of unknown progress, and process latching on unavailable syscall or structurally invalid results. |
| Integration | Ordinary transport, fixed-peer restrictions, external batch writer, managed lease, and supported synchronous exact-forwarding wrappers. Preserve socket authority and peer filtering. |
| Lifecycle | Deadlines, closed sockets, concurrent operations where already supported, lease release/reacquisition and terminal cleanup. |
| Fallback and builds | Kill switch, unqualified kernel/architecture, and `quic_go_no_private_syscalls` preserve ordinary sends. Check Darwin arm64/amd64 builds and the iOS exclusion; build success is not native evidence. |

Existing coverage starts in `sys_conn_sendmsg_x_darwin_test.go`, `sys_conn_sendmsg_x_qual_darwin_test.go`, `send_conn_sendmsg_x_darwin_test.go`, the send-worker progress tests, and `managed_packet_ecn_darwin_test.go` / `managed_packet_ecn_sendmsg_x_darwin_test.go`. These are navigation pointers, not a frozen test inventory or a requirement to invent new coverage for every wording change.

Exercise deterministic native errors where feasible and retain injected-result tests for the caller's reaction to partial, unavailable, invalid and unknown-progress results. Label evidence as native, injected or source-derived. Injected tests do not establish the target kernel's behavior; do not claim native coverage for an error that was only simulated.

Review the kernel assumptions that the finite tests cannot establish, especially which errors guarantee zero accepted datagrams and how a partial count reaches userspace. Cite source applicable to the target kernel with its revision and explain the version mapping; an unrelated latest source snapshot is not target-kernel evidence. If applicable source is unavailable, identify an alternative direct native observation that establishes the required assumption or record the gap as inconclusive. Do not silently inherit Darwin 25 semantics.

## Disposition and publication

Publish one results record alongside the protocol with exact candidate and host identities, commands and outputs, executed/skipped cases, native engagement, evidence types, architecture scope and remaining gaps. Preserve the original D1 protocol, results, collectors and all other frozen evidence unchanged.

- **Pass:** every required case executes successfully, the error assumptions are supported, fallbacks are preserved and the proposed admission matches the native architecture evidence. Only then add the admission entry, recording the tested product/build/kernel/architecture and linking the results. Verify the final production gate engages on the qualified target and excludes unqualified targets; the retained startup self-check still controls each process.
- **Fail:** a required contract is contradicted. Record the incompatibility and retain ordinary-send fallback for the target. Scope any fix separately.
- **Inconclusive:** required evidence is missing, skipped, ambiguous or invalidated by the test environment. Record the gap and retain ordinary-send fallback for the target.

Stop after the declared run and disposition. A corrected candidate or invalidated run requires an explicit protocol revision identifying what changed and the bounded revalidation needed; retain earlier outcomes. Never repeat collection until it passes. An admission-only change after a pass requires the final gate checks above, not a new performance campaign; any other product change must be assessed for evidence invalidation.

There is no throughput threshold, statistical resampling, timing campaign or new benchmark harness in compatibility qualification. Actual batch engagement is required. A new optimization, material batching redesign or observed performance regression may warrant separately scoped performance work; the original D1 performance-adoption requirements continue to describe that historical adoption, not an automatic tax on each new OS version.
