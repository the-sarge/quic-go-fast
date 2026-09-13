# HTTP/3 server admission implementation plan

**Date:** 2026-09-13
**Status:** Complete
**Track:** S in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

`http3/server.go:260–285` initializes contexts but replaces the done channel, and direct ServeQUICConn checks closure separately from incrementing the active count. The listener route (`:308–318`) similarly separates cancellation checking from count reservation. Completion (`:268–270`) can close the done channel at zero. Close (`:609–632`) and Shutdown (`:640–678`) have different listener-error and timeout behavior. `handleConn` includes request/GOAWAY and unidirectional-accept completion (`:509–595`). `NewRawServerConn` (`:457–462`) is outside server accounting.

## Decision

Keep private admit/complete/seal operations on existing Server and mutex. Initialize a stable done channel once; atomically reserve a connection before setup or goroutine launch and pair it with one deferred completion covering the full managed handleConn lifetime. Seal admission under the same mutex. Close the completion channel once when sealed and empty; never wait while holding the completion mutex. Snapshot owned listener work under lock, close and wait outside it while preserving current error precedence. Preserve cross-call completion of owned-listener shutdown with one Server-private listener-close serialization mutex around listener Close calls only. Acquire it after releasing Server.mutex and release it before waiting for managed connection completion. Repeated calls keep their existing per-call Close/error behavior; do not memoize results or introduce once-only error semantics. This is necessary because the underlying listener can return early from a second Close while the first is still finishing callbacks/handshakes (server.go:415–434). Direct rejected ServeQUICConn must not close the caller's connection. A listener-accepted connection rejected by admission must receive the existing H3_NO_ERROR shutdown disposition, without closing caller-owned listeners/sockets. Keep ConnContext after successful admission and raw connections unmanaged.

**Rejected alternatives — do not do this:** Do not add a generic lifecycle group, new goroutine or public API, reopen a closed Server, use a bare WaitGroup as admission control, or treat a stable channel alone as sufficient. Do not promise shutdown can force a blocked user handler to exit. Preserve first-error versus joined-listener-error and timeout behavior.

**Non-goals:** No wrappedConn test-seam refactor, new error/idempotency policy, handler force-stop, raw-connection enrollment, external socket ownership change or HTTP/3 framing change.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| S1 | Complete | Make HTTP/3 connection admission atomic with shutdown | None | None |

## Implementation slices

### S1 — Make HTTP/3 connection admission atomic with shutdown

**What it delivers:** Make both managed server entry routes reserve before setup and reject after sealing; wait for admitted work through one stable completion signal.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** Existing Server mutex owns admission state, count, seal and completion-channel closure. The separate listener-close mutex serializes owned-listener close work only; it never owns admission or connection count. Listener ownership stays Server-owned only where already owned.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Both serving entrypoints, constructor/setup failures, Close/Shutdown and handleConn completion. Trace listener/error ownership and ConnContext ordering. Preserve public API and protocol GOAWAY behavior; no untraced effects accepted.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal managed-connection accounting over ServeQUICConn and listener Serve routes. Existing Server methods own representation; raw NewRawServerConn is explicitly outside this domain. Finite interleaving cases demonstrate the invariant, not all possible Go schedules.

**Contract closure:** Triggered: the lifecycle/ownership or retry failure is material and independently reachable states cross the bounded enforcement seams below. Invariant: Make both managed server entry routes reserve before setup and reject after sealing; wait for admitted work through one stable completion signal. Actors are normal application calls and peer inputs already accepted by existing parsing/validation; no new hostile grammar domain.

| Semantic class | Disposition | Enforcement owner | Finite evidence | Status |
|---|---|---|---|---|
| Direct route checked just before shutdown / listener accepted just before seal | Either reserve before seal or reject; never start uncounted | Server admission | Deterministic cases for both routes | Covered: `TestServerCloseWaitsForAdmittedSetup`, `TestServerServeListenerRejectsAcceptedConnAfterClose`, `TestServerServeQUICConnRejectsWithoutTakingOwnership` |
| Last admitted connection completes while another attempts admission after seal | Close stable done once; reject late admission | Server seal/complete | Coordinated completion case | Covered: `TestServerServeCompletionSignal` |
| Unused server, repeated/concurrent Close and Shutdown, immediate escalation | Finish without channel replacement, double close or lock wait cycle; concurrent callers wait for earlier owned-listener close work | Server lifecycle | Bounded table including concurrent listener close completion | Covered: `TestServerUnusedConcurrentShutdown`, `TestServerConcurrentListenerCloseCompletion`; existing immediate/graceful shutdown tests |
| Setup failure and managed handler completion | Release reservation once across full managed scope | Server completion | Failure and request/uni-accept completion cases | Covered: `TestServerCloseAfterSetupFailure`, `TestServerCloseWaitsForManagedHandler` |
| Rejected direct connection / rejected accepted connection / external resources | Preserve ownership-specific disposition | Admission callers | One case each ownership class | Covered: Direct/listener rejection regressions and `TestServerListenerCloseErrors`; existing external-socket tests |
| Listener failures and shutdown timeout | Preserve existing return precedence and timeout ctx.Err | Existing Close/Shutdown error boundary | Characterization table | Covered: `TestServerListenerCloseErrors`; existing `TestServerGracefulShutdown` |

**Evidence budget:** At most 12 new cases, reuse existing ServerClosing, ConcurrentServeAndClose, ImmediateGracefulShutdown and GracefulShutdown. One HTTP/3 package run and one focused race run. Use private actual admission seam or temporary test-only coordination; no permanent production callback solely for testing. No repeated stress or deadline SLA. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Write deterministic failing admission/last-completion cases before moving count operations. Characterize listener-error precedence first. Run `go test ./http3 -count=1` and `go test -race ./http3 -run "TestServer.*(Clos|Shutdown|Serve)" -count=1`.

**Dispatch context budget:** This contract, http3/server.go, handleConn/server lifecycle tests and directly referenced listener ownership docs; no global connection refactor. One source file with existing methods and bounded tests. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Splitting stable completion from admission leaves the race intact; splitting entry routes leaves one bypass. Merging response finish is unrelated lifecycle ownership. No blockers: current Conn and listener contracts suffice.

**Stop conditions:** Stop if completion requires waiting while holding Server.mutex, closing a caller-owned resource, forcing user handlers, expanding raw management or changing error precedence. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

**Acceptance criteria:**

- [x] Deliver the behavior stated in this slice's What it delivers field at its named owner.
- [x] Preserve the explicitly listed existing behavior and satisfy the finite evidence budget.
- [x] Introduce no temporary second owner or unapproved public/API/storage representation change.

Universal wording in these criteria is bounded by this slice's Representation contract and semantic classes; no external syntax or unknown consumer census is implied.

## Validation gates

Use each slice’s named focused commands and finite evidence. Follow the execution overlay for exact-head local certification and applicable hosted CI. Record actual test names if current naming differs, and verify selectors execute tests. Do not claim native behavior from compilation alone.

## Operating Discipline

The shared [review-loop baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/REVIEW-LOOP.md) and [contract-closure baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/CONTRACT-CLOSURE.md), supplied by `$implement-architecture-slice`, govern directly. Apply the repository-specific [execution overlay](../REVIEW-LOOP.md), [maintained conventions](../agents/conventions.md), and ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md), and [0005](0005-incoming-packet-lifetime.md). Do not seed a repository copy of either shared baseline.

One fully briefed initial review and at most one replacement review; independently disposition findings before fixes. Stop on a representation mismatch, an established repeated precise root, or required boundary expansion. Compare exact invariant, concrete enforcement seam, semantic classes, and why the earlier accepted family owned the later case before calling findings one repeated root. No recursive proof obligations on tests or frozen diagnostics. No new performance campaign, random stress expansion, unsupported platform cross-product, or timing SLA. Ordinary focused regression tests remain required evidence of the accepted behavior; no new verification framework is a maintained blocking deliverable.

After the exact reviewed head passes local certification and applicable existing hosted checks, squash merge, append the development journal, reconcile issue/frontier pointers, and complete the corresponding OmniFocus slice task. Do not complete the program parent until its actual children are complete. Existing open investigations and completed programs keep their scopes.
