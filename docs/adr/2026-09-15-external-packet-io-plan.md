# External packet I/O and managed endpoints implementation plan

**Date:** 2026-09-15
**Status:** In progress; Q01, Q02, Q03, Q04, R01-A, R01-B, P01-A and P01-B complete; Q05, R01-L and R01-W form the fork frontier
**Track:** Q of the QUIC packet-I/O program
**Normative scope:** Current slice contracts plus the [design contract](2026-09-15-external-packet-io-design.md)
**Audit history:** [Handoff audit](2026-09-15-packet-io-handoff-audit.md); [evidence revision](2026-09-15-packet-io-evidence-reuse-audit.md)
**Issue links:** [Track Q #324](https://github.com/the-sarge/quic-go-fast/issues/324)

## Goal and settled decisions

Deliver the complete optional integration through real external sockets, wrappers, safe reusable endpoints, fixed-peer enforcement, native qualification and consumer adoption. The design contract records the accepted interface and ownership decisions. Upstream builds, authenticated same-socket establishment and existing close-transfer contracts remain intact. Do not implement socket replacement after probing, automatic authority inferred from methods, opaque-wrapper unwrapping, receive-format mutation on borrowed raw sockets, or raw handback inferred from toggling an option. No new wire grammar, authenticated migration, helper module, monitoring service or verification framework is in scope.

## Evidence reuse and remaining qualification

The fork's existing adoption receipts are the baseline evidence for unchanged native algorithms. Reuse their protocols, source identities, raw results, known venue limitations and fallback tests; do not repeat their adoption campaigns. E01 is complete. E01-L/E01-W/E01-D are retired as superseded baseline campaigns, not completed measurements. Their removal unblocks Q02–Q05 after Q01; it does not waive native correctness or observed engagement on the new external/wrapped paths.

Q02–Q05 and R01-L/W own only their named finite correctness, native engagement and lifecycle regressions. Compare changed source owners with the adoption receipt before relying on it. A materially changed kernel algorithm or newly supported OS/architecture requires a scoped re-audit of that affected capability; unchanged neighboring algorithms do not inherit another campaign. R03 retains its bounded raw-handback investigation.

E02-L/W/D each own one final assembled comparison under the current wiremux measurement protocol: one predeclared control/candidate pair, three paced workloads, ten pairs per workload, and no separate baseline campaign. Managed reuse, mixed upstream/fork peers, TURN, disabled/unavailable and IPv6 cases retain representative correctness checks where their behavior differs; they do not multiply the performance matrix. No capacity pilot, impairment campaign or TURN performance harness is a required deliverable. Historical results establish only their recorded native domains, not current full-Wire performance. Insufficient statistical resolution terminates with an explicit limitation and ordinary defaults; correctness failure or absent native qualification still blocks the affected capability. Releases, named consumers, rollback and Z01 remain required.

P02's owner has selected wrapper retention. The wiremux selected-peer wrapper remains the enforcement owner on direct and managed paths; the fork's fixed-peer API remains independently useful to other consumers. E02 evaluates acceleration through the retained wrapper and cannot reopen its removal. P02's existing experiment keeps its original protocol and conclusions; its documentation PR must finish its own merge/journal gates before P02 is complete.

## Current shape

At fork `8d3d151a4da565a21c6c2e77c17d83bd5e07ff06`, `transport.go:382` initializes socket wrapping with createdConn, `:441` initializes WriteTo and `:477` initializes Close. `:448` owns asynchronous stateless/close output; Close waits for listening, not every send. `send_conn_sendmsg_x_darwin.go:95` owns native batch dispatch. `connection.go:2885` admits alternate local paths; fixed-peer mode must guard both origin and target. Current Q/R/P interfaces do not exist.

## Slice graph

| Slice | Delivery | Blocked by | Disposition |
| --- | --- | --- | --- |
| Q01 | Register external packet I/O and provide ordinary writer | None | Complete |
| Q02 | Enable external Windows segmented sends | Q01 | Complete |
| Q03 | Enable permissioned Linux coalesced receive | Q01 | Complete |
| Q04 | Enable permissioned Windows coalesced receive | Q01 | Complete |
| Q05 | Accelerate checked batch writers on Darwin | Q01 | Retain/rework [PR #375](https://github.com/the-sarge/quic-go-fast/pull/375) |
| R01-A | Create ordinary managed endpoints and exclusive leases | None | Complete |
| R01-B | Bind a lease to QUIC with generation-safe handback | R01-A, Q01 | Complete |
| R01-L | Linux managed coalesced normalization | R01-B, Q03 | New |
| R01-W | Windows managed coalesced normalization | R01-B, Q04 | New |
| R03 | Decide raw handback feasibility | R01-L, R01-W | New |
| P01-A | Consolidate packet-policy hooks without activating policy | None | Complete |
| P01-B | Activate complete immutable fixed-peer policy | P01-A, Q01 | Complete |
| L01 | Release qualified fork capability | E02 | New |

Cross-repository blockers refer to the other track in the program index. The tracker owns live completion state. A stage is one intended PR in its named code/evidence repository; consumer PRs live in their own repositories while their issue and normative contract stay with this integration track.

## Implementation slices

### Q01 — Register external packet I/O and provide ordinary writer

**Current state:** Complete. Both standard-type signatures below are implemented as named in the design contract. W01 is complete; other Q01 successors retain their independent blockers. P01-B is complete; P02 retains the wrapper; its documentation is in progress in the wiremux track. Live cross-track readiness remains in the linked issues.

**What it delivers and acceptance criteria:** Implement immutable registration and its binding checks in the fork transport/socket initialization module. Deliver both extension methods, including a working synchronous UDP writer using ordinary WriteMsgUDP with the declared prefix/error semantics, so W01 can discover the complete extension without Q05. Q05 adds accelerated Darwin submission behind that factory. Freeze the standard-type structural signatures using isolated upstream/fork consumer builds. Keep newly registered receive coalescing unavailable until platform implementations land. Define diagnostics for requested, permitted, supported/enabled, disabled reason and exercised counters through the existing tracing/diagnostic route; do not expose a private-state API merely for tests.

Acceptance: upstream import compatibility; valid registration accepted; duplicate, late, swapped, typed-nil and unsupported identity rejected; ordinary unregistered connections unchanged; absence and invalid configuration distinguishable; baseline writer preserves complete UDP messages and definite-prefix/terminal-error semantics. Factory writers and registered callbacks support concurrent calls from multiple connection send workers; factory scratch is synchronized or per-call. Include one concurrent-call race regression. Constructor state is not close authority. Tests own these finite classes, including Close-before-register and WriteTo-before-register. Bounds: transport init/registration, ordinary writer factory, private capability state and focused tests; no accelerated packet algorithm, new helper module, wiremux runtime or receive restoration changes.

**Blocked by:** None.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Transport initialization owns immutable registration; existing socket owner retains Close; existing send worker owns retry.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to transport initialization/WriteTo/Close in `transport.go`, socket initialization in `sys_conn.go` and `external_packet_io*`, common send dispatch in `send_conn.go`, and focused tests. The send module includes the Darwin active/stub native-method adapters, non-Darwin stubs, and platform-specific OOB encoding. `sconn` satisfies `batchSender` across platforms; unregistered non-Darwin availability remains false, while unregistered Darwin dispatch retains existing qualification and native submission. Both factory and registration signatures must land together. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Valid/absent registration; duplicate, late via Dial/Listen/Close/WriteTo/ReadNonQUICPacket/AddPath, swapped, nil/typed-nil/nonpointer targets; ordinary writer full/short/error; isolated upstream/fork structural builds. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and transport.go:382,441,477; sys_conn.go; send_conn.go; focused init tests. Both factory and registration signatures must land together. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Splitting factory from registration makes the accepted two-method structural interface undiscoverable. Platform algorithms remain separate; no baseline dependency for ordinary I/O.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### Q02 — Enable external Windows segmented sends

**Current state:** Complete. The read-only USO probe supports external ordinary and registered connections while preserving wrapper submission, receive-format permission and close ownership. Q03–Q05 remain ready; E02-W remains blocked by Q04 and R01-W. Live cross-track readiness remains in the linked issues.

**What it delivers and acceptance criteria:** Separate the read-only USO capability probe from receive-format permission and close ownership. Preserve wrapper WriteMsgUDP submission and ordinary fallback. Validate actual segmented delivery and ancillary metadata on native Windows, with ordinary and registered external sockets plus capability-disabled/unavailable cases. Preserve existing Linux GSO. Bounds: Windows capability plumbing, relevant cross-platform capability declaration and focused tests. No URO or caller reuse semantics.

**Blocked by:** Q01.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Windows capability probe owns USO availability; wrapper owns submission policy.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to sys_conn_windows.go capability setup and existing USO tests; Q01 contract. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Native USO external ordinary/registered, unavailable/disabled; payload segmentation and OOB identity, plus existing Linux GSO regression. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and sys_conn_windows.go capability setup and existing USO tests; Q01 contract. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Receive mutation is independent and deferred to Q04. Baseline is needed for the accepted native qualification decision, Q01 for registration/fallback contract.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### Q03 — Enable permissioned Linux coalesced receive

**Current state:** Complete. Explicit external receive permission enables Linux GRO through the participating wrapper without transferring Close ownership. The receive owner rejects invalid GRO segment metadata and truncated reads before splitting. Native wrapper engagement and selected/foreign filtering are qualified on Linux IPv4 and IPv6; the existing direct and TURN packet profiles are unchanged. R01-L is ready after completed R01-B and Q03. Q04 and Q05 remain ready; E02-L retains R01-L. Live cross-track readiness remains in the linked issues.

**What it delivers and acceptance criteria:** Enable GRO on explicitly registered, disposal-guaranteed native wrapper paths only. Validate ReadBatch large-buffer/metadata preservation, selected/foreign filtering, coalesced split boundaries, invalid/truncated metadata handling, sibling storage lifetime, read error, cancellation and initialization failure after mutation. Preserve existing 1232-byte direct and fixed 1200-byte TURN packet profiles; coalescing is not a path-MTU or Datagram-ceiling change. Native engagement and focused correctness regressions are required; reuse G2 adoption evidence for the unchanged algorithm, with assembled performance owned by E02-L. Bounds: socket initialization, existing receive/split owner and relevant wrapper evidence; no reusable raw handback.

**Blocked by:** Q01.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Transport grant owns permission; Linux receive decoder owns segmentation/storage; existing root owns disposal.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to sys_conn_oob.go; Linux GRO helper/tests; incoming packet/storage owner and Q01. Wiremux adapter source is read-only contract context. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Native GRO through participating wrapper; selected/foreign, invalid/truncated metadata, partial batch error, retained sibling, cancellation and setup failure after mutation. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and sys_conn_oob.go; Linux GRO helper/tests; incoming packet/storage owner and Q01. Wiremux adapter source is read-only contract context. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Decoder and enablement must merge together; otherwise changed format precedes its reader. Windows is independent.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### Q04 — Enable permissioned Windows coalesced receive

**Current state:** Complete. Explicit external receive permission enables Windows URO through the supplied message-I/O wrapper without transferring Close ownership. The receive owner rejects malformed or truncated coalesced reads before splitting and preserves genuine socket errors. Separate-endpoint Windows wrapper engagement is qualified; see the [native receipt](../audits/2026-09-16-q04-windows-uro.md). R01-W is ready after completed R01-B and Q04. Q05 and R01-L remain ready; E02-W retains R01-W. Live cross-track readiness remains in the linked issues.

**What it delivers and acceptance criteria:** Apply the same explicit permission contract to URO, retaining Windows message I/O and supported ancillary metadata. Use the same receive-owner semantic classes; do not depend on Linux implementation state. Require a two-endpoint native Windows environment that actually engages URO; hosted same-host loopback is not equivalent evidence. Bounds: Windows receive setup/decoder, common permission wiring and focused tests. Preserve USO and ordinary Windows reads.

**Blocked by:** Q01.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Transport grant owns permission; Windows decoder owns URO metadata/storage.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to sys_conn_windows.go; URO tests and Q01 permission contract. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Separate-endpoint URO engagement; metadata truncation/splitting/storage/error/cancel/setup failure; preserve ordinary receive and USO. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and sys_conn_windows.go; URO tests and Q01 permission contract. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Q03 is not necessary: Q01 owns shared registration and this slice owns Windows receive. Keep USO separate because it requires no receive-format mutation.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### Q05 — Accelerate checked batch writers on Darwin

**What it delivers and acceptance criteria:** Refactor existing qualified Darwin submission into the fork-owned writer factory without duplicating the kernel implementation. Register a checked outer-wrapper callback; keep the current one-destination/one-OOB batch domain. Validate qualified, disabled and unqualified behavior, wrong destination, full/short/zero/invalid progress, unknown-progress error, packet-specific MTU feedback, and release on cancellation. The helper's baseline fallback uses standard socket writes; the existing send worker remains retry/order owner. Native Darwin evidence retains private-syscall build/runtime opt-outs. The factory owns concurrency-safe scratch; current per-sconn single-worker assumptions must not leak into a transport-wide callback. Exercise concurrent calls under the race detector. Bounds: current batch helper, send adapter and progress interpretation; no new OS qualification claims or `recvmsg_x` revival.

**Blocked by:** Q01.

**Existing-work disposition:** Retain and rework [product PR #375](https://github.com/the-sarge/quic-go-fast/pull/375) within Q05. Dispatch is paused until the child pointer is synchronized with this scoped contract revision; then resume the triggering review's inner fix/verify loop. The [re-audit receipt](https://github.com/the-sarge/quic-go-fast/pull/375#issuecomment-5699832633) owns review chronology and dispositions.

**Single owner after merge:** Native writer owns qualification/syscall scratch and the internal distinction between a submission that never ran and a native progress result; registered wrapper authorizes output; existing send worker owns retry. The send-worker adapter retains zero-progress per-packet attribution for pre-dispatch socket failures. Factory callers use ordinary WriteMsgUDP when native submission never ran, preserving terminal deadline and closed-socket errors at UDPBatchWriterV1 and managed WriteBatchV1. Never retry a native result with unknown progress.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to send_conn_sendmsg_x_darwin.go:95; send_queue.go; Q01 writer factory and existing qualified syscall tests. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Retain the matching send-worker and lifecycle owner rows in the design contract semantic matrix. The scoped error correction has no new material-risk closure root: it restores the existing public terminal-error behavior at the single native submission result owner, with the following finite semantic census.

| Native outcome / consumer | Required behavior | Enforcement owner | Evidence / status |
| --- | --- | --- | --- |
| Raw submission never ran / send worker | Preserve zero-progress per-packet error attribution | Shared native result, existing send-worker adapter | Existing queue progress and MTU regressions retained |
| Raw submission never ran / factory and managed lease | Ordinary WriteMsgUDP surfaces terminal deadline or closed-socket error | Shared native result, factory fallback | Real native factory expired-deadline and cached closed-socket regressions; real managed-lease expired-deadline regression required |
| Native known full/short/zero progress / either consumer | Preserve definite prefix; send worker owns suffix fallback | Existing native classifier and send worker | Existing progress regressions retained |
| Native unknown progress / either consumer | Preserve terminal error and never resend | Existing native classifier and send worker | Existing unknown-progress regressions retained |

The deadline regression clears the deadline and confirms subsequent native delivery, covering recovery without a timing campaign. Socket Close and lease generation ownership remain unchanged. The only uncovered cells before implementation are the named real-native pre-dispatch failure regressions; fake socket joining tests remain evidence for joining, not for native dispatch errors.

**Evidence budget and TDD:** Native qualified/disabled/unqualified; full/short/zero/invalid counts, unknown progress, wrong peer, MTU feedback, cancellation and disposal. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. The scoped re-audit admits the three real-native pre-dispatch failure regressions named above, then verification of the triggering review and at most one final fully briefed fresh review. This replaces the exhausted initial/replacement budget only for the accepted error-contract repair, under implement-architecture-slice's scoped re-audit branch; it does not authorize an open-ended fix loop. Any further unmet obligation requiring another fresh cycle stops for operator routing. Terminate when this finite evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and send_conn_sendmsg_x_darwin.go:95; send_queue.go; Q01 writer factory and existing qualified syscall tests. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. Load only the linked PR's unresolved pre-dispatch error finding and its re-audit disposition; other findings retain their settled dispositions.

**Slice decision audit:** One real callback path must include progress interpretation and native helper; splitting would expose unsafe retries. W02 is not needed for fork test wrapper qualification.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### R01-A — Create ordinary managed endpoints and exclusive leases

**Current state:** Complete. The ordinary endpoint and exclusive lease factory is implemented on `*Transport`. R01-B is complete; P01-A and P01-B are complete. Platform normalization retains its separate slice contracts.

**What it delivers and acceptance criteria:** Add NewManagedPacketEndpointV1(network string, laddr *net.UDPAddr) (net.PacketConn, func() (net.PacketConn, error), error). It creates and owns a fresh UDP socket, returning ordinary endpoint and acquire closure. No raw adoption/detach API. Each lease initially supports ordinary establishment datagrams. Lease Close revokes its generation, interrupts and joins active I/O, restores endpoint logical deadlines, then releases acquisition. Endpoint Close is terminal and interrupts parent/lease operations. Reject parent I/O during lease and concurrent acquisition; acquisition returns busy if parent I/O is active, without silently canceling it. No receive coalescing yet.

**Blocked by:** None.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Managed endpoint is the sole native socket, deadline, active-I/O and lease-generation owner; each lease delegates to it.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** Managed endpoint intentionally supports ordinary reads only until R01-B/L/W. No unsafe coalesced state is admitted; later slices add capability without replacing an owner.

**Blast radius:** Limited to New endpoint module, std net.PacketConn semantics, Q01 factory style; no QUIC decoder or Windows/Linux offload implementation. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Ordinary read/write; second acquisition; parent read rejection; cancellation/blocked read and write; concurrent Close/acquire; stale lease write after release; logical deadline restoration; parent terminal close. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and New endpoint module, std net.PacketConn semantics, Q01 factory style; no QUIC decoder or Windows/Linux offload implementation. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Ordinary endpoint is useful and independently green. Split optimized transition and OS normalization out. No runtime predecessor is required: the ordinary factory is independent.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### R01-B — Bind a lease to QUIC with generation-safe handback

**Current state:** Complete. Managed registration seals the exact active lease under the endpoint's operation lock; its private batch writer participates in generation checks and I/O joining. Lease Close, rather than Transport.Close, permits reuse. P01-B is complete; P02 retains the wrapper; its documentation is in progress in the wiremux track. R01-L and R01-W retain Q03 and Q04 blockers; R02 retains its W02 blocker. No additional successor becomes ready from this slice alone.

**What it delivers and acceptance criteria:** Add ConfigureManagedPacketIOV1(conn net.PacketConn, lease net.PacketConn, sendBatch func([][]byte, []byte, *net.UDPAddr) (int,error)) error. Exact lease must originate from the factory and still be active; exact outer conn binds through normal registration rules. Explicit registration transitions ordinary establishment to exclusive QUIC only after caller readers join. Serialize phase transition with active operations, reject any concurrent ordinary operation, and seal phase until lease Close. Keep ordinary receive on every platform. The factory-proven lease exposes WriteBatchV1([][]byte, []byte, *net.UDPAddr) (int,error), backed by its endpoint-private Q01 native writer; ordinary fallback works now, and Q05 acceleration remains conditional. Managed registration claims the same immutable slot instead of calling ordinary registration first. Generation checks cover every lease I/O/deadline method and native writer closure. Delayed QUIC workers must fail after lease revocation; Transport.Close alone is not handback evidence.

**Blocked by:** R01-A, Q01.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Endpoint owns lease phase/generation and joining; Transport owns immutable registration, never native Close.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** Ordinary managed QUIC is maintained; optimized receive remains off until platform normalization exists.

**Blast radius:** Limited to transport.go:382,448,477; endpoint from R01-A; socket wrapper initialization and focused real transport fixtures. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Real QUIC lease lifecycle after ordinary probe datagram; failed init; transport Close with blocked queued send; lease Close then new lease with stale worker send/deadline; concurrent batch call versus lease Close; terminal parent Close. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and transport.go:382,448,477; endpoint from R01-A; socket wrapper initialization and focused real transport fixtures. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Joining and registration must be complete together. Linux/Windows normalization remains disabled and separately green.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### R01-L — Linux managed coalesced normalization

**What it delivers and acceptance criteria:** Enable managed receive coalescing on Linux only after persistent endpoint normalization is installed. Reuse the platform decoder and bounded storage owner. Ordinary endpoint/lease reads always return one datagram; optimized coalesced representation stays inside registered QUIC mode. Retain normalization across leases to interpret pending kernel data. Dispose old consumed QUIC storage without replay. Failure to restore logical readiness is terminal.

**Blocked by:** R01-B, Q03.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Endpoint retains native receive-format/storage ownership across generations; platform decoder is the only metadata interpreter.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to R01-B endpoint; Linux receive decoder and native fixtures; no other platform implementation. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Native queued coalesced data at release, next ordinary read then next lease; selected/foreign data, truncated metadata, retained sibling, cancellation while returning, parent Close; bounded retained bytes. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and R01-B endpoint; Linux receive decoder and native fixtures; no other platform implementation. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Separate platform changes are independently green with ordinary fallback elsewhere. Both lease lifecycle and native permissioned decoder are prerequisites.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### R01-W — Windows managed coalesced normalization

**What it delivers and acceptance criteria:** Enable managed receive coalescing on Windows only after persistent endpoint normalization is installed. Reuse the platform decoder and bounded storage owner. Ordinary endpoint/lease reads always return one datagram; optimized coalesced representation stays inside registered QUIC mode. Retain normalization across leases to interpret pending kernel data. Dispose old consumed QUIC storage without replay. Failure to restore logical readiness is terminal.

**Blocked by:** R01-B, Q04.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Endpoint retains native receive-format/storage ownership across generations; platform decoder is the only metadata interpreter.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to R01-B endpoint; Windows receive decoder and native fixtures; no other platform implementation. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Native queued coalesced data at release, next ordinary read then next lease; selected/foreign data, truncated metadata, retained sibling, cancellation while returning, parent Close; bounded retained bytes. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and R01-B endpoint; Windows receive decoder and native fixtures; no other platform implementation. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Separate platform changes are independently green with ordinary fallback elsewhere. Both lease lifecycle and native permissioned decoder are prerequisites.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### R03 — Decide raw handback feasibility

**What it delivers and acceptance criteria:** Inspect native Linux/Windows option and queued-buffer semantics and perform one predeclared finite handback experiment per platform if a plausible bounded procedure exists. Required result: either a separately scoped safe raw-return proposal with explicit pending implementation children, or a recorded rejection of raw return after coalescing with managed reuse as the supported route. Draining unbounded traffic, discarding unrelated queued data, hoping queues are empty, or suppressing restoration errors cannot pass. No additional repeated experiments without a distinct corrected hypothesis. The program cannot close with this decision unresolved; raw borrowed sockets remain ordinary-receive meanwhile.

**Blocked by:** R01-L, R01-W.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Native queue semantics constrain the documented contract; endpoint remains supported owner.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to Linux/Windows option and queued receive documentation; managed normalization contract; bounded receipt. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Documentation, protocols, receipts and dependency records: process/traceability metadata. Existing tests and measurement tools: verification aids; no new maintained framework or parser is authorized.

**Representation contract:** Named raw-return procedure on qualified Linux/Windows only; example-level feasibility, no universal restoration claim.

**Contract closure:** No new runtime enforcement; proposed raw adoption cannot become authoritative in this slice. Positive finding requires re-handoff of named implementation children before closeout.

**Evidence budget and TDD:** One source-backed hypothesis and at most one native experiment per Linux/Windows if plausible; explicit rejection is a complete outcome. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and Linux/Windows option and queued receive documentation; managed normalization contract; bounded receipt. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Native normalized reuse must exist to make a negative raw outcome useful. Do not merge implementation of a new restoration protocol into this investigation.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### P01-A — Consolidate packet-policy hooks without activating policy

**Current state:** Complete. The transport-owned packet adapter established the socket ingress, ordinary/stateless egress and native/registered batch admission seams while retaining existing capability extraction. P01-B activated those seams and the origin/target path guard. P01-B is complete; P02 retains the wrapper; its documentation is in progress in the wiremux track.

**What it delivers and acceptance criteria:** Refactor the current packet admission and output seams into internal pass-through hooks owned by the existing transport/raw connection/send worker. Cover socket ingress before routing, ordinary and stateless egress, native Darwin batches and connection path attachment. Preserve native extraction, batching, buffer ownership, errors and packet order. No public policy API, new configurable callback framework, new persisted state or active filtering. The hooks must be behaviorally inert and usable by P01-B without a second policy state machine.

**Blocked by:** None.

**Existing-work disposition:** New inert prefactor; no implementation adopted.

**Single owner after merge:** Existing receive, send and path owners retain authority; the inert hooks own no new fact.

**Authority completeness:** No new authority or persisted representation. Existing upstream interfaces retain their behavior.

**Transitional-seam budget:** One internal pass-through hook representation is retained for P01-B policy activation; no duplicate state or public authority. P01-B is its named consumer, and existing paths stay valid indefinitely if dispatch pauses.

**Blast radius:** transport.go receive/send; rawConn and send_conn implementations including Darwin native batch; connection.go AddPath and path-manager tests (about ten files). Internal consolidation must not change allocation/storage lifetimes, callback routing, concurrency or capability detection. Untraced effects require re-audit.

**Artifact classification:** Internal runtime routing is shipped behavior; tests are verification aids, no maintained-aid exception.

**Representation contract:** Existing typed packet/address/path representations owned by quic-go; universal preservation within supported public APIs, finite characterization evidence.

**Contract closure:** No new authority is triggered. Existing emission/storage contracts remain preserved; inspect hook paths against their accepted owner matrix.

**Evidence budget and TDD:** Characterization first: ordinary receive/send, stateless output, native Darwin path and AddPath behavior; preserve existing storage and send-progress tests. Focused affected tests and existing platform gates, no benchmark campaign for inert calls. One representative per distinct route, no new mutation or operational campaigns. One initial review plus at most one replacement; terminate on passing characterization and required gates.

**Dispatch context budget:** This slice, existing emission/incoming-lifetime ADRs and transport.go receive/send; rawConn and send_conn implementations including Darwin native batch; connection.go AddPath and path-manager tests (about ten files). Target below 25k source/context tokens; reserve the remaining fresh context for implementation, review and certification.

**Slice decision audit:** This inert prefactor is the smallest behavior-preserving slice that bounds later full policy activation. No blocker: current unrestricted behavior can be characterized now. Merging P01-B would combine seam refactoring with security activation. Splitting by direction is unnecessary only if the listed owners fit; otherwise re-audit separate inert migration batches.

**Stop conditions:** Non-inert behavior, a second owner, loss of a native path, or a wider migration design means return to slicing before activation.

### P01-B — Activate complete immutable fixed-peer policy

**Current state:** Complete. `ConfigureFixedPeerV1` binds a copied UDP peer and the direct local socket before initialization. One immutable policy gates receive routing, ordinary/stateless/segmented and batch output, and origin/target path admission. P02 has selected wrapper retention; its documentation remains in progress. No additional fork slice becomes ready; its remaining blockers are unchanged.

**What it delivers and acceptance criteria:** Implement immutable transport-wide peer admission before initialization, with no changes to ordinary unrestricted transports. Census the packet entry/exit paths listed above against current source and enforce at shared receive/send owners. Test address canonicalization, pre-connection foreign traffic, known-connection-ID foreign traffic, stateless output, direct/batch sends, path-add/switch attempts and teardown. Preserve fork emission and incoming-storage ownership. Scope excludes authenticated migration, multipath, identity, NAT traversal and UDP-connect shortcuts. The optional setter uses standard types and explicitly rejects late/unsupported configuration. Freeze ConfigureFixedPeerV1(peer *net.UDPAddr) error with pre-init validation. Store the policy in one object used by ingress/egress and connection path admission. Reject Conn.AddPath on fixed-origin connections and attachment to fixed target transports, preserving the selected local socket as well as remote address. Deep-copy address; no UDP-connect substitution. Private native batch output must use the same policy.

**Blocked by:** P01-A, Q01.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** One immutable transport peer/local-path policy owns admission; connections retain its origin constraint, packet storage stays with existing owners.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to transport.go ingress/send/init; sys_conn wrappers; send_conn.go/native writer; connection.go:2885 path manager; current policy tests. Load these owners, not entire connection history. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Runtime API and diagnostics: shipped behavior. Admission, progress and lifecycle guards: required safety enforcement. Tests: verification aids; no maintained-aid exception.

**Representation contract:** The standard-library packet/address and explicit registration domain in the design contract; universal enforcement within that domain, finite regression evidence.

**Contract closure:** Use the matching owner row in the design contract semantic matrix; write the named positive and distinct failure regressions first.

**Evidence budget and TDD:** Foreign preconnection, known CID, stateless/nonQUIC output, batches/segmentation, AddPath on fixed origin and onto fixed target, teardown, IPv4/mapped and directional IPv6 zones; ordinary transport unchanged. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and transport.go ingress/send/init; sys_conn wrappers; send_conn.go/native writer; connection.go:2885 path manager; current policy tests. Load these owners, not entire connection history. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Ingress-only or connection-only activation would create incomplete security authority. P01-A first establishes inert central hooks; Q01 establishes immutable configuration. Activation then fits policy, hooks, init, path admission and focused tests (about twelve files). No W02 dependency. Stop if unrelated migration redesign is required.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

### L01 — Release qualified fork capability

**What it delivers and acceptance criteria:** L01 follows the fork release runbook: reviewed default-branch source, exact-head hosted checks, vulnerability/dependency audit on the release graph, immutable tag, consumer module download and provenance, accurate support/limitations, explicit version. Choose the real version at execution, never invent a published tag in advance.

**Blocked by:** E02.

**Existing-work disposition:** New slice. Prior local design commits are replaced by this audited plan; no unmerged implementation is adopted. See audit for source/history disposition.

**Single owner after merge:** Fork release procedure owns immutable tag/artifacts; release documentation owns support claims.

**Authority completeness:** No new persisted fact or restart format. For in-memory authority, construction, validation, failure cleanup and each destructive/security-sensitive consumer named in this slice land together. Unregistered existing behavior retains its current owner.

**Transitional-seam budget:** No temporary duplicate owner. Ordinary upstream/fallback paths remain supported permanently.

**Blast radius:** Limited to docs/runbooks/release.md, accepted support matrix, actual module graph and release workflows. Shared/global state changes are forbidden. The ownership, concurrency, public interface, failure, security, performance and dependency effects are those explicitly named in delivery and tests; any newly found effect outside these owners is a stop condition, not an accepted unknown.

**Artifact classification:** Documentation, protocols, receipts and dependency records: process/traceability metadata. Existing tests and measurement tools: verification aids; no new maintained framework or parser is authorized.

**Representation contract:** Exact released module version/archive; authoritative Go module tooling and release workflow, universal immutability within that version.

**Contract closure:** Use existing release enforcement, no new persisted representation. Stop if release would replace an existing immutable tag.

**Evidence budget and TDD:** Existing release gate once at exact source, immutable module archive/provenance and consumer download; no invented version. One positive per materially distinct behavior and one negative per failure mode; at most one discriminating guard-bypass per new owner when necessary, no mutation required for prose. Only E02-L/W/D run the final assembled performance comparison; this slice adds no standalone adoption campaign. One initial review and at most one replacement. Terminate when named evidence passes or records an allowed negative design disposition; missing required environment stays blocked.

**Dispatch context budget:** This slice, referenced design subsections/matrix rows and docs/runbooks/release.md, accepted support matrix, actual module graph and release workflows. Load at most these named owners and their focused tests (target under 25k source/context tokens, no whole historical plan); reserve the rest of a fresh context for implementation, review fixes and verification. If the relevant diff cannot fit, re-audit before implementation rather than overflow into a second implicit PR. No unresolved implementation history is inherited.

**Slice decision audit:** Source/release-note PR and its authorized release receipt are one delivery. Do not merge wiremux release into another repository PR.

**Stop conditions:** Stop on unsupported representation, second mutation owner, missing full reader/writer join where this slice requires it, a newly required public topology or capability bypass, untraced blast radius, or inability to fit the accepted outcome in this PR. Use the shared precise-root comparison before declaring repeated review roots. Missing native access blocks only dependent evidence; do not weaken required correctness or inflate repetitions.

## Operating discipline and validation

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern directly, composed with [this repository overlay](../REVIEW-LOOP.md). They own representation/artifact gates, finite evidence/review budgets, semantic-family closure and review/verification-aware approach stops. Use exact-head local certification, same-head applicable hosted CI, matched-head squash merge, then append-dev-journal and pointer reconciliation. Diagnose before any rerun. Keep audit history out of this current contract.

Fork code slices use affected tests/vet, applicable race/native gates and module tidiness; docs-only certification follows the overlay. Existing unit/integration/lint/cross-compilation/interop checks apply and run on drafts. Do not create `task preflight` or rename workflows.

Before any Go subcommand beyond approved environment/version auditing, enforce the repository installed-toolchain floor. Never treat skipped hosted jobs as required success. Each child updates only live state/pointers; no child closes a program parent. Z01 is the sole full-program closeout. The plans may be revised through scoped audit, but no mandatory outcome may disappear.
