# Darwin batch-send compatibility qualification

This maintained policy governs extending the existing private `sendmsg_x` batch-send implementation to additional Darwin kernels. [ADR 0008](adr/0008-darwin-batch-compatibility.md) separates this compatibility exercise from the original D1 performance-adoption experiment. Keep the kernel allowlist, production-shape startup self-check, accepted-count checks, process latch, kill switch and ordinary-send fallback. A kernel version is an admission boundary, not proof of compatibility.

## When to qualify

Qualify before admitting a new Darwin kernel major. Revisit an existing qualification when a relevant syscall ABI or semantic change, implementation change, or compatibility regression invalidates its evidence, including changes within a kernel major. An ordinary library release or OS patch alone does not mandate repeating qualification. Investigate known regressions and narrow or disable admission where necessary; a previous pass is not permission to ignore contradictory evidence.

Use one issue for the target kernel and link this policy. Darwin 27 issue [#516](https://github.com/the-sarge/quic-go-fast/issues/516) was the first application; its [native comparison and exact-kernel evidence](audits/2026-09-22-darwin27-errors/results.md) completed arm64 qualification. It is separate from the completed Darwin 25 managed ECN qualification in [#504](https://github.com/the-sarge/quic-go-fast/issues/504).

## Declare the finite protocol

Commit a new protocol under `docs/audits/` before collecting qualification evidence. Record the base commit, target kernel major, exact macOS product/build and Darwin release, intended architectures, Go toolchain, commands, required test cases, test-only activation mechanism, output locations and pass/fail/inconclusive rules. Record the exact candidate SHA before execution and in the results. If an activation overlay is needed, record its exact bytes and base SHA so the tested tree is reproducible.

The first protocol must include a bounded comparison against an already-qualified kernel on the same architecture, including the transient-error cases below. Use the same committed probe and case construction on both hosts, recording actual socket settings, toolchains and identities. Applicable archived reference observations may be reused when their probe, inputs, settings and outputs support the declared comparison; identify that provenance before collection. If neither a reference host nor comparable archived evidence is available, record the comparison as inconclusive rather than claiming a match or a difference. Reference runs cover only the declared comparison cells and do not reopen the reference kernel's qualification or performance-adoption campaign.

Map each row below to existing behavioral tests and identify gaps before collection. Run each declared native matrix case once, the relevant package suite once, and its race gate once per intended native architecture; overlap between the focused matrix and package gates is intentional. Run the declared cross-build and opt-out checks once. Reuse existing tests and add only missing behavioral coverage. Required native skips or missing hosts produce an inconclusive result, not a pass. Do not add benchmarks, repetitions, platforms or fixture combinations during collection to rescue a result.

The runtime admission must match the native architecture evidence. The allowlist is keyed by kernel major and supports an architecture constraint on each entry; an arm64-only qualification cannot justify an entry that also enables amd64. Use that constraint unless native evidence supports every architecture the new entry admits. Test the finite admitted/excluded kernel-architecture pairs against the shipped table without replacing it with a synthetic table, and retain excluded-architecture ordinary-delivery coverage. Predicate tests using injected entries alone do not check the production admission data. Cross-compilation establishes build compatibility only. Preserve the historical Darwin 25 entry's existing unrestricted admission; this rule does not retroactively expand its evidence.

## Exercise the real path before admission

Use test-only admission for the actual target kernel, retaining the real production-shape self-check and runtime error handling. Do not spoof the host as Darwin 25, force the qualified flag, substitute a fake syscall for native evidence, or ship an experimental bypass. Restore test state and keep forced admission out of ordinary builds. The self-check must be asserted successful: its existing informational result on an unlisted kernel is insufficient by itself.

Verify native engagement through increased batch-submission counters together with exact peer-observed datagrams and metadata. Capability availability or a green suite containing skipped native cases is insufficient. In disabled, unqualified and opt-out cases, verify ordinary sends still deliver correctly and no native batch submission occurs.

## Required correctness matrix

| Area | Required evidence |
| --- | --- |
| Representation | ABI layout, correct delivered payloads and destinations, accepted counts, and outgoing/received ECN marks through the supported paths. |
| Address families | IPv4-only, IPv6-only, and dual-stack sockets carrying mapped IPv4 and native IPv6 traffic. All admitted usable families must execute. |
| Progress and errors | Full and partial acceptance; EAGAIN, EINTR, ENOBUFS and EMSGSIZE before progress and after an accepted prefix, with each kernel assumption supported by explicitly classified evidence; message-size attribution, no retry of accepted entries, conservative handling of unknown progress, and process latching on unavailable syscall or structurally invalid results. |
| Integration | Ordinary transport, fixed-peer restrictions, external batch writer, managed lease, and supported synchronous exact-forwarding wrappers. Preserve socket authority and peer filtering. |
| Lifecycle | Deadlines, closed sockets, concurrent operations where already supported, lease release/reacquisition and terminal cleanup. |
| Fallback and builds | Kill switch, unqualified kernel/architecture, and `quic_go_no_private_syscalls` preserve ordinary sends. Check Darwin arm64/amd64 builds and the iOS exclusion; build success is not native evidence. |

Existing coverage starts in `sys_conn_sendmsg_x_darwin_test.go`, `sys_conn_sendmsg_x_qual_darwin_test.go`, `send_conn_sendmsg_x_darwin_test.go`, the send-worker progress tests, and `managed_packet_ecn_darwin_test.go` / `managed_packet_ecn_sendmsg_x_darwin_test.go`. These are navigation pointers, not a frozen test inventory or a requirement to invent new coverage for every wording change.

## Compare errors in the first round

Test whether the target preserves the required count/error behavior before proposing a different runtime policy. A new major version or unavailable source is an evidence gap, not an observed incompatibility. Do not silently inherit the reference kernel's semantics or assume the target differs. Preserve the existing error handling while qualifying it; any proposed product change needs separately scoped evidence and review.

Declare a zero-progress and an accepted-prefix case for each errno the implementation treats as safe to retry. Attempt deterministic native UDP EAGAIN, EINTR and EMSGSIZE cases in the first collection instead of discovering their absence after a green package suite. Establish that the failing element produces the intended error on its own, then pair it with a batch containing a distinct valid prefix. Record raw return counts and errno, exact peer payloads and metadata, rejected-suffix non-delivery within the declared observation window, and any signal or timeout evidence needed to interpret the result. Include a successful ordinary-size ancillary-data control so malformed metadata cannot masquerade as the intended trigger.

The [Darwin 25/27 probe protocol](audits/2026-09-22-darwin27-errors/protocol.md) supplies demonstrated starting points, not a requirement to rerun its frozen artifacts or create a maintained framework:

| Case | Bounded construction and interpretation |
| --- | --- |
| UDP EAGAIN | Read back a modest sender buffer capacity B. A B-byte payload plus valid ECN ancillary data can exceed the combined socket-space check while each component fits individually. Use a nonblocking socket, validate the positive control, and observe the actual result on each kernel rather than assuming the trigger still works. |
| UDP EINTR | Use the same insufficient-space construction on a blocking socket, a thread-directed signal with SA_RESTART disabled, and independent send-timeout/watchdog bounds. Record signal arrival and syscall duration. A timeout or signal race is inconclusive, not EINTR evidence. |
| EMSGSIZE | Use an oversized UDP datagram alone and after a valid prefix, preserving exact count and peer-delivery evidence. |
| ENOBUFS | A constrained AF_UNIX datagram receiver provides a bounded native control. It is not a UDP ENOBUFS observation. Establish the relevant shared batch owner through applicable source or verified target-binary evidence before using the control to support protocol-independent count/error suppression. |

Fix the probe source, finite case list, resource bounds and correction budget before collection. Do not manufacture allocation failures through global memory pressure, change machine-wide network settings, inject syscall returns or repeat a cell until it passes. A construction that fails to produce the intended condition remains recorded as inconclusive; a justified replacement needs a committed protocol revision. This does not require native induction of every possible lower-layer error: an infeasible native case needs another evidence route that establishes its specific assumption, with the missing native observation clearly labeled.

For each paired observation, report matching, different or inconclusive behavior separately from the qualification disposition. Matching examples are not proof that entire kernels behave identically. A measured difference must be assessed against the required count, retry and delivery contract; unrelated timing differences alone are not incompatibility. Retain injected-result tests for the caller's reaction to partial, unavailable, invalid and unknown-progress results, but never count them as observations of the target kernel.

## Establish assumptions beyond the probes

Label evidence as native, injected, source-derived or target-binary-derived. Review the assumptions finite tests cannot establish, especially whether a returned error can hide preceding accepted messages and how a partial count reaches userspace. Every required assumption needs applicable evidence; a successful delivery probe or an unrelated latest source snapshot does not close an error-count gap.

Applicable source must have a pinned revision and a defensible mapping to the target build. If source is unavailable, direct native observations or verified inspection of the actual target kernel binary may establish the same specific assumption. For binary evidence, record the image digest, build and architecture, match its UUID or equivalent identity to the running kernel, resolve the syscall through its actual dispatch entry, and trace the relevant count/error owner and branches for the admitted socket domain. A nearby symbol name, string match or resemblance to older source is insufficient. Retain reproducible extraction commands, tool versions, mapping and minimal address-bearing excerpts; use a scratch output directory rather than rewriting frozen captures. The [Darwin 27 binary analysis](audits/2026-09-22-darwin27-errors/binary/README.md) demonstrates this route and its limits.

Keep each conclusion at the layer actually established. A common batch epilogue can prove that an errno cannot hide preceding successful per-message calls; it does not independently prove every failure inside the current UDP send is atomic, convert an AF_UNIX control into native UDP evidence, or qualify connected sockets when only unconnected sockets were examined. State the existing lower-layer contract being composed with the evidence and the declared empirical guarantee level. If the required assumption remains unsupported, record it as inconclusive. Neither unavailable public source nor the absence of an unsafe error-induction experiment alone requires inconclusive qualification when another applicable route establishes the required assumption.

## Disposition and publication

Publish one results record alongside the protocol with exact candidate and host identities, commands and outputs, executed/skipped cases, native engagement, reference comparisons, evidence types, architecture scope and remaining gaps. Separate observed matches or differences from unsupported cases and the resulting admission decision. Preserve the original D1 protocol, results, collectors and all other frozen evidence unchanged.

- **Pass:** every required case executes successfully, the error assumptions are supported, fallbacks are preserved and the proposed admission matches the native architecture evidence. Only then add the admission entry, recording the tested product/build/kernel/architecture and linking the results. Verify the final production gate engages on the qualified target and the shipped table excludes unqualified kernel-architecture pairs; the retained startup self-check still controls each process.
- **Fail:** a required contract is contradicted. Record the incompatibility and retain ordinary-send fallback for the target. Scope any fix separately.
- **Inconclusive:** required evidence is missing, skipped, ambiguous or invalidated by the test environment. Record the gap and retain ordinary-send fallback for the target.

Stop after the declared run and disposition. A corrected candidate or invalidated run requires an explicit protocol revision identifying what changed and the bounded revalidation needed; retain earlier outcomes. Never repeat collection until it passes. An admission-only change after a pass requires the final gate checks above, not a new performance campaign; any other product change must be assessed for evidence invalidation.

There is no throughput threshold, statistical resampling, timing campaign or new benchmark harness in compatibility qualification. Actual batch engagement is required. A new optimization, material batching redesign or observed performance regression may warrant separately scoped performance work; the original D1 performance-adoption requirements continue to describe that historical adoption, not an automatic tax on each new OS version.
