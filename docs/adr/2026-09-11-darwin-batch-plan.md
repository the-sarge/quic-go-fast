# Darwin Batch Send and Receive-Batching Experiment Implementation Plan

**Date:** 2026-09-11
**Status:** Accepted; D1 complete (#257), D2 frontier
**Track:** D, 3 of 3 in the 2026-09-11 datapath offload program
**Depends on:** Nothing technically — recommended after receive-side slices by the program's evidence-per-effort ordering; D2 requires D1
**Related:** [Datapath offload plan](2026-09-11-datapath-offload-plan.md); ADRs [0001](0001-upstream-compatibility.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers, and stop conditions
**Audit history:** [Handoff receipt](../audits/2026-09-11-datapath-handoff/README.md); [KeibiSoft discovery](../audits/2026-09-11-keibisoft-gso-discovery.md)

## Goal

macOS sends batches of UDP datagrams per syscall through XNU's private `sendmsg_x`, with fail-closed ABI qualification, and a bounded experiment decides whether `recvmsg_x` receive batching earns adoption. No production QUIC stack batches UDP on macOS; this track owns all of its own evidence.

## Current Shape (verified 2026-09-11)

Darwin sends and receives one datagram per syscall (`sys_conn_helper_darwin.go:22` fixes `batchSize = 1`). The send worker attributes message-size errors and handshake MTU feedback per queue entry (`send_queue.go:135-147`). The KeibiSoft reference (commit `24ecf34c`, MIT) implements the send half with correct v4-mapped dual-stack destinations and partial-acceptance resend, ships no tests, and leaves its receive half unable to batch; details in the [discovery audit](../audits/2026-09-11-keibisoft-gso-discovery.md).

## Decision

Port the send half under the exact build, qualification, and error-attribution contract in the [datapath offload plan](2026-09-11-datapath-offload-plan.md): active path `darwin && !ios && !quic_go_no_private_syscalls`, fallback stub, Darwin-kernel-major allowlist with recorded product-version mapping, production-shape startup self-check (unconnected `msg_name` destinations, shared ECN control buffer, semantic payload verification), accepted-count bounds, process-latched fallback, engaged/fallback counters. Run `recvmsg_x` receive batching as a separate bounded experiment with its own gate. That plan is binding; this document adds only the slice decomposition.

**Rejected alternative (do not do this):** Porting the KeibiSoft receive half as-is (it has never batched; its commit leaves `batchSize = 1`). Presenting a tested version ceiling as future compatibility. Justifying the opt-out tag by symbol scanning (raw syscalls import no symbol). Folding the receive experiment into the send slice's gate.

**Non-goals:** iOS builds ever invoking private syscalls; changing the Linux or Windows paths; any public API change.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
|---|---|---|---|---|
| D1 | Complete (#257) | macOS `sendmsg_x` batch send with fail-closed qualification and adoption evidence | None | n/a |
| D2 | new | Bounded `recvmsg_x` receive-batching experiment; adopt or retire | None (D1 complete) | n/a (experiment; retires cleanly) |

## Implementation Slices

### Slice D1 — macOS `sendmsg_x` batch send

**What it delivers:** The batch send path end to end: raw-syscall wrapper with ABI struct-layout tests, the build-tag pair (active and stub), the kernel-major allowlist and production-shape startup self-check, batch submission integrated into the send queue under ADR 0004 with the plan's partial-acceptance algorithm (never resend accepted entries; first unaccepted entry retried through the per-packet path so `isSendMsgSizeErr` and handshake MTU feedback attach to the correct entry; tail without loss or duplication), `QUIC_GO_DISABLE_SENDMSG_X`, engaged/fallback counters, and the adoption protocol/results pair (engagement metric: packets per batch submission).

**Existing-work disposition:** Adaptation of external reference KeibiSoft `24ecf34c` (MIT, attributed in file comments). It is a reference, not a baseline: no code merges unreviewed, its missing tests are written here, and its receive half is explicitly excluded.

**Blocked by:** None. The program recommends starting after G and W frontier work purely by evidence-per-effort ordering; there is no technical edge and this slice may run in parallel.

**Single owner after merge:** The send worker (`send_queue.go`) remains the single owner of send ordering and error attribution; the Darwin conn owns batch encoding and syscall submission. No second send policy owner.

**Authority completeness:** The batch capability (allowlist + self-check result): constructed at startup, validated fail-closed, process lifetime, consumers (send path only), latched off on structural failure. No persistence, no destructive consumer.

**Transitional-seam budget:** None. The fallback stub is a permanent platform-policy artifact, not a transitional seam.

**Blast radius:** `send_queue.go`/`send_conn.go` are shared across platforms — batch submission is reached only behind the Darwin capability, verified by the full suite on the CI matrix; MTU-discovery feedback is the named risk and has closure classes below. Cross-builds (darwin amd64/arm64, ios arm64, opt-out tag) gate the constraint claims, and adding the ios and opt-out-tag build checks (currently skipped by `cross-compile.sh:13`) to CI or the slice's validation gates is a traced deliverable of this slice. Untraced: none remaining.

**Artifact classification:** Batch path, tags, self-check, counters: shipped behavior. ABI layout tests, cross-build checks, fake-batch-sender tests: verification aid. Protocol/results: process metadata per the adoption rule.

**Representation contract:** The `sendmsg_x` message-array ABI; owned by the wrapper plus the startup self-check (the semantic authority that the running kernel matches); guarantee is universal only within qualified kernel majors, fallback elsewhere; terminating evidence: self-check, layout tests, closure classes.

**Contract closure:** Triggered — silent send corruption or misattributed MTU feedback is material across reachable paths. Invariant: every queue entry is either sent exactly once with correct error attribution or fails through the per-packet path with correct metadata. Owner: the send worker's partial-acceptance algorithm. Classes: full acceptance; partial acceptance then success; partial then `EMSGSIZE` (MTU feedback to correct entry); partial then non-size error; `ENOSYS`/structural failure (latch + fallback); kill switch off; unqualified kernel major. Each tested with a fake batch sender; guard mutation: resend an accepted entry and observe the duplication test fail.

**Evidence budget:** Closure tests; cross-build matrix (darwin amd64/arm64, ios arm64, opt-out tag); self-check qualification on each protocol host; protocol cells with predeclared statistical bounds. No further platform or repetition scope.

**TDD and preservation evidence:** Closure tests first against a fake sender. Preservation: with the capability off (switch, tag, unqualified, latched), send behavior is byte-identical — disabled protocol cell plus full suite.

**Dispatch context budget:** This slice contract, the Darwin rules in the program plan, `send_queue.go` (~200 lines), `send_conn.go`, the discovery audit's ABI notes, and the reference commit diff (bounded, ~576 lines). Fits one fresh context.

**Slice decision audit:** Strongest split: syscall wrapper + self-check separate from send-queue integration — rejected, the wrapper without a consumer is horizontal and the self-check is only meaningful with the production call shape the integration defines. Strongest merge: with D2 — rejected, D2 is an experiment with permission to fail and must not share D1's gate. Blocking edges: none claimed.

**Stop conditions:** Self-check fails on a host the matrix intends to qualify (ABI assumption wrong — operator decision); partial-acceptance closure cannot hold without changing send-queue ownership beyond ADR 0004's boundary; engagement zero across protocol cells.

### Slice D2 — `recvmsg_x` receive-batching experiment

**What it delivers:** A bounded experiment per the program plan: raise the Darwin receive batch size behind a capability sharing D1's allowlist/self-check/latch infrastructure, receive via `recvmsg_x`, declare batch size and per-connection memory budget up front, measure receive syscalls per delivered datagram and loopback throughput against the D1-era baseline on the unmerged PR head, and dispose in one PR: adopt means the PR merges on predeclared-positive results; retire means the PR closes unmerged with zero residue in the tree. An inconclusive outcome retires; D1 is untouched either way, and no removal PR exists because nothing experimental merges before its result.

**Existing-work disposition:** New development. The KeibiSoft receive half is documented prior art that never batched; it is not ported.

**Blocked by:** D1 (shares the syscall wrapper pattern, qualification/allowlist/latch infrastructure, and counters).

**Single owner after merge:** The Darwin conn owns receive batching; the buffer pool owns the ordinary per-datagram buffers (no slab — receive batching delivers separate datagrams, not a coalesced buffer).

**Authority completeness:** The receive-batch capability mirrors D1's: constructed, validated, latched, consumed only by the read path — all in this slice.

**Transitional-seam budget:** The experimental path exists only on this slice's unmerged PR branch; nothing transitional merges. On adoption the merged path is permanent capability-gated behavior; on retirement the PR closes and the tree never contained it.

**Blast radius:** Darwin `batchSize` affects the shared OOB read loop — gated so the constant changes only with the capability; per-connection receive memory multiplies by the declared batch size and is budgeted in the protocol. Untraced: none identified.

**Artifact classification:** Experimental path: shipped behavior only if adopted (merged); otherwise it never enters the tree. Tests: verification aid. Protocol/results: process metadata; the adopt-or-retire disposition is the accepted gate.

**Representation contract:** The `recvmsg_x` message-array ABI; wrapper plus self-check own it; universal within qualified kernel majors; terminating evidence: self-check, layout tests, protocol cells.

**Contract closure:** Triggered — receive-path corruption is material. Invariant: every delivered datagram surfaces exactly once with its own ancillary data, regardless of batch fill. Classes: full batch; partial batch; single datagram; ancillary-data attribution per message; structural failure latch. Tested with loopback and fake-syscall cases.

**Evidence budget:** Closure tests; protocol cells with predeclared batch size, memory budget, statistical bounds, and the explicit adopt/retire rule. One review plus at most one replacement.

**TDD and preservation evidence:** Closure tests first. Preservation: capability off → `batchSize` 1 behavior byte-identical; disabled cell plus full suite.

**Dispatch context budget:** This slice contract, D1's merged qualification infrastructure, the Darwin receive path (`sys_conn_oob.go`, `sys_conn_helper_darwin.go`), and the program plan's experiment rules. Fits one fresh context; governing diff is D1's PR.

**Slice decision audit:** Strongest split: none — the experiment is already minimal. Strongest merge: with D1 — rejected; independent gates are the accepted design and an inconclusive receive result must not block send adoption. Blocking edge D1: genuine — the qualification and latch infrastructure are D1 deliverables.

**Stop conditions:** Inconclusive or negative protocol result → retire (not an operator escalation; the retirement disposition is pre-accepted). Self-check failure on an intended host, or any need to change shared receive-path ownership → operator decision.

## Acceptance Criteria

- [x] On qualified Darwin kernel majors, batch send engages (packets per submission > 1 reported) and meets its protocol's predeclared thresholds; on unqualified majors or the kill switch the path is inert (fallback counters observed), and for iOS builds and the opt-out tag it is absent at compile time — the terminating mechanism is that the active and stub files define the same symbols so exactly one compiles per tag set, verified by the cross-build matrix. Delivered by #257: 7.99 packets per submission ([results](../audits/2026-09-12-d1-sendmsgx-results.md)), and `cross-compile.sh` now builds the ios library packages and the opt-out tag instead of skipping them.
- [x] MTU-discovery and handshake feedback attribution is preserved under every partial-acceptance class (#257 closure suite, `send_queue_batch_test.go`).
- [ ] D2 ends in exactly one of the pre-accepted dispositions: adopted with positive predeclared results, or retired with the experimental path removed.

Universal criteria carry the per-slice domains, owners, guarantee levels, and terminating evidence above.

## Validation Gates

`go test ./...` and focused closure tests on macOS CI; the cross-build matrix (`GOOS=darwin` amd64/arm64, `GOOS=ios` arm64, opt-out tag); each slice's precommitted protocol on the qualified Darwin hosts.

## Operating Discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` for every slice/PR; no repository-specific overlay exists. Track-specific stops: the program plan's build-tag, allowlist, and self-check contract is binding; any weakening of fail-closed qualification, any iOS exposure, or any send-queue ownership change beyond ADR 0004 returns for decision. Vocabulary per the root `CONTEXT.md`.
