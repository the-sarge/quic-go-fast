# Packet Emission Implementation Plan

**Date:** 2026-09-07. **Status:** Accepted design; not implemented. **Track:** E of QGF-PE-2026-09. **Depends on:** Nothing outside this track. **Related:** [Program](2026-09-07-packet-emission-program.md), [ADR 0004](0004-packet-emission-ownership.md), [compatibility](0001-upstream-compatibility.md), [module adoption](0002-adopt-through-module-replacement.md), [stable upstream](0003-follow-stable-upstream-releases.md). **Normative scope:** Current outcomes, boundaries, invariants, budgets, blockers and stops. **Audit history:** [Handoff audit](../audits/2026-09-07-packet-emission-handoff.md).

## Goal

One concrete private module consumes a send opportunity and returns a compact emission result. It owns capacity, packet construction through the existing packer, registration, send-related accounting/qlog and outgoing buffer handoff. The connection retains lifecycle, receive fairness and protocol-state execution on its goroutine. The asynchronous worker performs socket I/O and returns immutable failure feedback. Adoption requires clearer ownership and behavioral testing with measured performance preservation, not a speed gain.

## Current shape

Anchors are verified on the current fork baseline `d0baa96481ad27eaa7cff3f0fbca0d6a42ad726e`; source correspondence is refreshed against each implementation parent. `connection.go:568` owns the run loop, `:2474` dispatches send modes, `:2518` prepares probes/control data, `:2589` and `:2625` assemble ordinary/GSO output, `:2697` handles ACK-only and `:2724` handles PTO. `:2777` and `:2816` register packets before queueing; coalesced registration can discard Initial keys. `packet_packer.go:20` exposes the broad test substitution interface; `:118` is the real implementation. Both connection constructors and `preSetup` at `connection.go:514` bind send state.

`send_queue.go:90` requires callers to check capacity; `:118` runs asynchronous writes; `:152` joins shutdown. On a fatal write, the current return precedes the ordinary buffer release; stopped submissions and leftover queued buffers also need explicit lifetime characterization. This is evidence of incomplete pool-return paths, not a measured memory-leak claim. Queue capacity is eight batches. `send_conn.go` owns native write/GSO fallback behavior and is not being redesigned.

`connection.go:1283` constructs direct server probes, `:2518` includes direct client probes, `:919` replaces the send path, and the server migration path changes the destination in place. `:2461` owns handshake MTU fallback and consumes generation-tagged feedback. `:2878` returns close bytes; `transport.go:813` retains them for later retransmission. Releasing that construction buffer without preserving the retained bytes would be incorrect.

The [completed handshake contract](2026-09-06-handshake-mtu-plan.md) and shipped parser/receive improvements are retained. No existing packet-emission implementation PR or child issue is an adopted baseline. The earlier local transmission design and August packet-emission review are retained as rationale and re-sliced here; their undifferentiated full-extraction step is replaced. Relevant discovery and dispositions live in the audit artifact.

## Decision and ownership

Keep a concrete private emission value in package `quic`, with the queue lifecycle and real packet packer behind its interface. Conceptually `advance(now)` returns progress, blocked reason, optional pacing deadline, whether an immediate retry is warranted, a capacity wakeup when needed, and an error. Queue saturation, recovery hard-blocking, congestion, pacing and absence of data remain distinct. Progress can precede a blocked result. Do not add per-packet objects, goroutines, timers or completion round trips.

There is one real packer, recovery state and send queue for an active path, including during migration of call sites. Recovery continues to own packet history/congestion/loss and the connection feeds it received ACKs and timer events on the same goroutine. Producers of stream, crypto, ACK, flow-control and path intent keep their state. Path policy, MTU transitions, key lifetime and connection lifecycle remain connection-owned. Emission executes the send sequence and synchronously reports packet-registration effects to those owners at the original points.

Bind narrow typed policy/registration hooks once. They run directly on the connection goroutine and complete before the next dependent packet is built. Preserve first-send/idle timestamps, connection-ID notification frequency and Initial-key retirement. No mutable shadow connection state, unrestricted `*Conn` dependency inside a renamed wrapper, per-packet closure allocation or broad generic command/strategy interface is permitted. E1 produces the concrete field and hook map for this bounded shape; it does not select a different architecture.

### Capacity and accounting

The ordinary queue has one producer on the connection goroutine. Check capacity inside emission before destructive packing for a batch; only the worker can reduce occupancy before that producer submits, so a separate reservation counter is unnecessary. Worker termination is a distinct case. Preserve the initial full-queue check before handshake feedback application, then process eligible fallback before another flight, including busy iterations. Preserve subsequent recovery, pacing and anti-amplification decisions.

Register each packet at the existing pre-I/O point, interleaved with GSO per-packet recovery/pacing queries. Preserve packet numbers, encryption levels, sizes, frame callbacks, ECN and probe flags. Keep receive-before-send fairness between batches, existing ACK-only/PTO allowances, GSO segment/ECN boundaries and fallback. Do not roll back registration, reuse packet numbers or add immediate retransmission after a local error. Partially constructed fatal paths terminate under existing error policy; only byte-storage cleanup changes. This is a coordinated sequence, not a transaction promising rollback.

### Buffer lifetime and errors

For a successfully constructed queued batch, submission consumes the buffer exactly once: enqueue transfers it to the worker; a stopped-submission rejection releases it. The worker releases its current buffer after the last syscall on success and error. Terminal shutdown stops production, joins the worker and only then drains any remaining entries. This covers an enqueue racing worker termination without a new hot-path lock or per-write completion message. Normal close continues to drain queued work. No cleanup drain runs concurrently with the worker.

Internally allocating packers release private construction storage on error and transfer it on success; append-style packers leave caller-owned storage with their caller on error. Correct only lifetime paths crossed by the migrated constructors. Do not free protocol-frame data still owned by recovery. A deterministic failing lifetime regression precedes each correction. No public ownership contract changes.

Direct server/client path probes retain their direct execution, destinations, error handling and accounting; they are not forced through the ordinary queue. Their emission helper releases temporary storage after the call. Path replacement joins the old worker before replacing it. In-place rebinding retains its current queued-destination behavior. Preserve current path-generation capture, latest eligible feedback, stale/post-confirmation rejection and the monotone congestion packet-size floor.

Close emission produces immutable owned bytes for the closed-handler lifetime, then releases pooled construction storage. One bounded close-payload copy is allowed on this cold path. Preserve retained retransmission, close suppression, remote/immediate close, error precedence and retention duration. Do not register close-only output as ordinary data or close a shared application-owned socket to simplify cleanup.

### Rejected directions and non-goals

**Do not do this:** move registration to socket completion; treat emission as rollback/retry; introduce a second credit ledger; widen the queue; replace the channel with an unmeasured lock-free queue; add per-packet strategies; move the run loop or receive policy into emission; pass a raw connection into a cosmetic wrapper; delete mocks before outcome coverage; require a second production adapter; retire a retained close buffer early; let the worker decide MTU/keys/congestion; suppress a failed performance gate because another fork patch received an exception.

**Non-goals:** Full sans-I/O engine, application credit, receive refcounts/storage, new congestion/scheduling policy, per-path state reorganization, package/import changes, new qlog vocabulary, HTTP/3 changes, CI standardization, physical-link/application qualification or revival of completed D/H tracks. General packet-packer cleanup outside a migrated construction path is excluded.

## Representation, artifacts and evidence rules

The semantic domain is the existing private send modes and typed packet/frame metadata produced by the real packer, supported QUIC versions, queue occupancy 0..8, connection-owned path transitions and supported native send results. Existing `wire` encoders/parsers and TLS protection own protocol representations; this work does not implement or claim a new broad QUIC parser. The ownership guarantee is universal within the named internal constructors/handoffs and their lifecycle states, enforced centrally; wire/scheduling regression observations and performance claims are example-level in their recorded domains. Public compatibility remains the repository's existing support obligation.

Runtime extraction is **shipped behavior**. Ownership cleanup and protocol-state guards are **required safety enforcement**. Tests, the disposable prototype, fixtures and analyzers are **verification aids**. Plans, receipts, source maps and mirrors are **process/traceability metadata**. No verification aid is approved as a maintained deliverable. An equivalent bounded fixture may supply the accepted measurement; aid completeness, portability or future reuse is not a product acceptance criterion. E1's dependency is the feasibility disposition, not maturation of a benchmark framework.

The shared CONTRACT-CLOSURE.md baseline governs, without additional triggers. Outgoing ownership triggers closure because corruption/premature release and broken cleanup are material and the supported build/worker/direct/retained paths are independently reachable. Focused queue tests cannot exercise private construction failure, direct-send disposal or retained-close pool reuse, so the bounded matrix coordinates those separate ownership transitions. The following is the bounded family, partitioned by actual enforcement owner. It is a design census with implementation evidence pending, not a claim tests already pass. Routine file moves and hook spelling alone do not trigger another matrix.

| Semantic class | Disposition and enforcement owner | Responsible slice and finite evidence | Status |
| --- | --- | --- | --- |
| Queue writes successfully, is full, or drains normally | Transfer once; worker current-entry release and connection-side join/drain | E2; positive write/full-resume/graceful-drain tests | Required; pending |
| Worker errors before/during submission; queued entries remain | Preserve cause; stopped submit releases or queues; terminal join/drain reclaims leftovers | E2; fatal-current/stopped-submit/racing-enqueue tests | Required; pending |
| Caller-owned ordinary/GSO buffer; no data or partial fatal build | Caller retains/reclaims until successful handoff; registered protocol state is not refunded | E3; empty/partial-build/normal/GSO cases | Required; pending |
| Internally allocated coalesced, ACK or PTO construction errors | Constructor reclaims unreturned storage; synchronous registration hooks preserve key effects | E4; representative error per materially distinct allocation path and coalesced/key/PTO cases | Required; pending |
| Direct probe and queued MTU probe | Probe helper owns temporary bytes; preserve direct/queued distinctions | E5; server/client direct destination and MTU cases | Required; pending |
| Path replacement and stale feedback | Old worker quiesces; connection owns generation and MTU | E5; queue replacement/in-place change plus existing stale/current feedback tests | Required; pending |
| Close bytes retained after construction | Close helper transfers immutable owned payload to closed handler before pool reuse | E6; first close and retained retransmission after poisoned pool reuse | Required; pending |
| Arbitrary grammar, public borrowed buffers, post-confirmation MTU shrink, application multipath | Outside this work; existing owners/policies apply | No new tests or mutation program | Non-goal |

Use one representative positive per semantic behavior and one negative per materially different failure mode/owner. No guard mutation is required initially because newly written failing lifetime tests directly distinguish the old behavior. At most one targeted guard-bypass per owner may replace unclear inherited evidence; record the named guard and failing existing test. No mutation of external syntax, recursive closure of aids, or unbounded state/platform cross-product. Review/verification repeated-root judgments require the shared exact-invariant/central-owner/semantic-class side-by-side comparison, never just a common filename or lifecycle noun.

## Performance contract

The accepted 5% practical noninferiority margin is not a promise of identical timing. Compare exact candidate and parent with identical fixture sources, native Go patch release, flags, QUIC version, qlog setting, GOMAXPROCS and disjoint physical-core placement per endpoint. Linux qualifies GSO; verify capability is active. Record CPU/SMT placement and contention. Correctness on macOS/Windows is required through existing support gates; their performance is not claimed without measurement. Use the fork parent retaining its parser and handshake changes, not only a comparison with upstream v0.62.0.

The full matrix is P1: 1071-byte application DATAGRAM at 1 Gbps/two processors; P2: 4 Gbps/two; P3: 4 Gbps/four; P4: P2 with GSO disabled; P5: one continuous reliable stream/two; P6: sixteen equal-priority continuous streams/two. GSO is on unless specified. Each cell uses established QUIC v1, qlog off, 2 s warmup, 60 s measurement, 1 s drain and ten adjacent alternating base/candidate pairs. Echo 64-byte framed records at 100/s on a dedicated bidirectional stream on the same connection, one outstanding, with a 1 s deadline; count missed schedules/failures and end a timed-out run as a failure rather than misattribute a late reply. A run, not a packet, is the experimental unit.

Record goodput and offered/admitted/delivered/drop ledgers, probe p50/p99 and failures/missed attempts, process CPU and allocated bytes per delivered unit, and focused allocations per packet/batch. Invalid/duplicate/corrupt payloads fail correctness. Require one-sided 95% paired bounds excluding >5% worse CPU/unit, allocated bytes/unit and successful-probe p99 and >5% lower goodput. Use geometric mean paired ratios with 20000 whole-pair bootstrap resamples, percentile bounds and seed 20260907; failed/missed probe proportions use paired absolute differences with upper bound at most +0.1 percentage point. Insufficient counts, zero/undefined denominators, timeout failures or unqualified hosts do not become passing intervals. A first run failure is diagnosed; it is not silently discarded or replaced for an attractive result.

Require no added steady-state per-packet/batch allocation in focused checks. Setup/closure may add one fixed module allocation ≤512 bytes and one owned close-payload allocation bounded by serialized length; report both. Existing handshake/churn/transfer benchmarks use ten paired one-second samples and `-benchmem`, with a 5% time noninferiority gate. One qlog-on paired P2 smoke is diagnostic only. Deterministic loss/reordering tests are correctness evidence, not real-time latency qualification.

Full matrix runs belong to E1 and E6. E3 runs P2/P4; E4 runs the handshake benchmarks and P2; E5 runs P2 plus focused probe/path allocation checks. E2 uses ten paired one-second queue microbenchmarks for successful handoff/drain and capacity pressure with `-benchmem`, requiring no added success-path allocations and a 5% time noninferiority bound. This maps evidence to changed paths rather than rerunning the entire matrix per file move. Exact head/base and commands are recorded for each receipt. E6 validates the composed final path; earlier receipts cannot certify a changed final candidate.

Per slice, one declared matrix plus at most one complete replacement for documented host contamination or one in-contract candidate revision. Retain both, predeclare the replacement reason and do not pool changed candidates. Failed/inconclusive evidence ends with no runtime adoption or an in-contract rework fitting the one replacement; a third campaign requires a new decision. Existing historical collectors, CPU reservation tools and private fixtures are not implicit dependencies. Issue #18 is relevant only if deliberately reusing its archived collector; use a bounded equivalent fixture instead of silently inheriting unsafe setup/cleanup. No new maintained measurement framework is authorized.

## Slice graph

| Slice | Disposition | Delivers | Blocked by | Temporary interface removed |
| --- | --- | --- | --- | --- |
| E1 | New, evidence-only | Bounded feasibility answer for normal/GSO packet emission | None | Prototype remains inert/disposable |
| E2 | New prefactor | Complete queued outgoing-buffer lifetime on failure and close | None | None |
| E3 | New migration | Normal 1-RTT/GSO emission through concrete owner | E1 positive, E2 | Normal-send orchestration; remaining legacy arms retired by E4–E6 |
| E4 | New migration | Handshake/coalesced, ACK-only and PTO emission | E3 | Those legacy arms; close/probes remain bounded |
| E5 | New migration | Direct/MTU probes and path handoff through emission | E3 | Probe/path raw send access; other arms remain bounded |
| E6 | New contract step | Retained close emission, obsolete-interface deletion and composed qualification | E4, E5 | All remaining legacy packer/queue forwarding |

E1 and E2 are independent; E4 and E5 are independent after E3. Serialize same-file merges and revalidate, but do not add convenience blockers. Each runtime migration routes disjoint packet classes to exactly one orchestration owner; legacy forwarding borrows the same authoritative packer/recovery/queue and stores no shadow state. No class can be processed by both paths. E3 introduces the bounded legacy forwarding seam; E4/E5 narrow it and E6 removes it. One active worker per path remains true throughout.

## Implementation slices

### E1 — Establish packet-emission feasibility

**Delivers:** A finite experiment using a disposable established-1-RTT normal/GSO extraction, source/field/hook correspondence map, outcome characterization, paired results and a positive/no-change/inconclusive disposition. The PR retains only useful ordinary characterization and opt-in evidence; prototype runtime is an inert patch tied to its base, never selected by normal builds. Positive means the bounded concrete seam works without moving execution/fairness or adding hot-path coordination and meets the full performance contract. It does not certify later migrated modes.

**Existing work / blockers:** New; no blocker. Retain prior reviews as rationale only. E2 may land independently; a prototype can be evaluated against its frozen parent, and E3 later uses the integrated E2 parent. Do not present unmerged prototype code as established production.

**Owners / authority / temporary seams:** Shipped runtime ownership is unchanged. One experiment receipt owns its stated result; no persisted product fact becomes authoritative. Prototype routing exists only in frozen experimental sources and is not an alternate runtime path in the merged tree. No maintained-aid exception is granted.

**Blast radius / artifacts / representation:** Only characterization, opted-in fixture/evidence and inert patch are merged; they are verification aids and traceability metadata. Representation is decoded internal send behavior and the frozen workload/manifest, owned by production codecs and the fixture's explicit counters. Performance and feasibility evidence is example-level. General tooling completeness and private-host reproduction are excluded; no product behavior depends on aid robustness outside this capture.

**TDD / closure / budget:** First add normal/GSO output, full-queue/resume and receive-fairness characterization through a real packer/recovery and controlled send adapter. Freeze the exact manifest before capture. Full performance contract applies, with one replacement. No resource-failure correction or production ownership claim belongs here; E2–E6 own those. Contract closure is not triggered for the receipt/fixture: no new authority or production lifecycle is introduced. One independent review plus at most one replacement reviews the actual evidence claims, not recursive proof of the aid.

**Context budget:** ≤45k input tokens: this slice, shared decisions/performance rules, relevant `sendPackets*`/registration methods, packer/recovery interfaces, queue behavior, four characterization cases, current prototype diff and compact result receipt. Raw samples remain file artifacts; do not load complete historical reports/tests. This leaves room for implementation, disposition and verification in one fresh context; if the fixture requires a maintained tool or extensive new infrastructure, stop instead of building it here.

**Split/merge audit:** Splitting fixture creation from the experiment would create a horizontal tool prerequisite without a design answer. Merging E1 with production extraction would remove the inexpensive no-adoption outcome and exceed scope. E2 is not a blocker because E1 claims only its unchanged normal-path behavior; E3 needs a positive E1 answer to justify moving production ownership.

**Stops:** No concrete bounded hook/field shape, extra per-packet coordination, receive starvation, failed/inconclusive gate after the allowed replacement, or required execution-model change. A no-change E1 PR may merge evidence, but E3 stays blocked and descendants require scoped re-handoff rather than automatic dispatch.

### E2 — Close queued-buffer lifetimes

**Delivers:** Every supported queued-byte-buffer submission reaches one terminal ownership disposition on normal send, stopped submission, fatal worker error, racing enqueue and graceful close. Preserve current queue capacity, single producer, worker behavior, error cause and handshake feedback. This is a useful independently shippable prefactor even if E1 rejects the larger extraction.

**Existing work / blockers:** New; no blocker. Current `Send`/`Run`/`Close` and their failure tests are the baseline; a new failing release test must establish each intended correction. Retain public and protocol accounting behavior. No direct probe or packer constructor cleanup in this slice.

**Owners / authority / temporary seams:** `sendQueue.Send` transfers or releases a submitted buffer; the worker releases its current entry; connection-side `Close` first ends production, joins the worker and then drains leftovers. These are successive exclusive owners, not competing mutation owners. No durable state or restart schema is added. No new semaphore/credit ledger or permanent interface is introduced.

**Blast radius / artifacts / representation:** Queue lifecycle runtime is shipped behavior; releases and join-before-drain are safety enforcement. Tests and tiny queue benchmarks are verification aids; receipt is metadata. Domain is one connection producer, bounded queue, supported write success/error and termination order, owned by the queue operations. Universal lifetime contract within that domain; no claim about arbitrary concurrent producers or closing externally owned sockets.

**TDD / closure / budget:** Use the first two closure rows. Six bounded scenarios: success, full/resume, normal drain, fatal-current-buffer, stopped submit, and enqueue concurrent with worker termination. Verify both current and queued storage release; race-test the same lifecycle suite. Preserve existing handshake-message-size feedback tests. Queue microbenchmark contract applies. No required mutation; at most one join/drain guard bypass if inherited race evidence cannot distinguish the guard. One initial review plus at most one replacement.

**Context budget:** ≤25k input tokens: this slice, queue ownership/common failure contract, `send_queue.go`, its tests, `buffer_pool.go`, the connection startup/close/path-replacement call sites and focused governing diff. No other migration implementation is needed.

**Split/merge audit:** Splitting worker-current and leftover/stopped cleanup leaves a single queue lifecycle partly specified and makes its race hard to verify. Merging with E3 would entangle the safety prefactor with orchestration changes. E3 requires E2 because emission's unconditional handoff contract relies on complete queue ownership.

**Stops:** A second producer, cleanup requiring concurrent queue drain, a changed close/error policy, or inability to enforce the lifecycle without new hot-path synchronization. No adjacent refcount redesign.

### E3 — Own normal and GSO packet emission

**Delivers:** Established 1-RTT normal and GSO send opportunities pass through the concrete emission owner, including capacity, existing packer, registration, qlog/connection-ID effects, batching, receive fairness and queue transfer. Real-connection stream and DATAGRAM output exercises it. Preserve the registration point and recovery queries during batch assembly.

**Existing work / blockers:** New production implementation; E1 must have a positive disposition and E2 must be merged. E1's inert prototype is design evidence, not automatically cherry-picked production. Integrate both completed outcomes against the current parent and record the bounded governing diff.

**Owners / authority / temporary seams:** Emission alone orchestrates ordinary/GSO output; the connection still orchestrates unmigrated send classes. Both borrow one real packer and recovery instance on one goroutine, and E2's queue is the only queued-buffer owner. Introduce only typed legacy forwarding for the remaining classes; it stores no shadow state and is removed by E4/E5/E6. Explicitly list each retained legacy method. No persisted authority is added.

**Blast radius / artifacts / representation:** Runtime extraction and necessary caller-buffer error cleanup are shipped/safety code; moved outcome tests and performance captures are aids/metadata. Domain is constructed 1-RTT ordinary/GSO packets under supported recovery modes and queue capacity, represented by existing packer/recovery objects. Ownership/order invariant is universal in those entrypoints; outcome/performance evidence is example-level. Handshake/probe/close policy remains unchanged.

**TDD / closure / budget:** Third closure row: normal, full-queue/resume, GSO full/short final segment, ECN batch change, empty pack, partial fatal append and receive fairness. Extend E1's real-packer cases instead of duplicating mock choreography. Keep existing pacing/GSO/size/ECN and real DATAGRAM/stream tests. P2/P4 and focused allocation checks apply; one replacement. No guard mutation initially. One initial review plus at most one replacement.

**Context budget:** ≤40k input tokens: this slice/common ownership/performance contract, E1's compact accepted shape/receipt, E2's queue diff, normal/GSO send and registration code, relevant packer/recovery interfaces and touched tests. Legacy arms are bounded interface context, not a requirement to redesign them.

**Split/merge audit:** A non-GSO-only production split would create two owners for essentially the same per-packet registration/capacity loop and postpone the main hot-path evidence. GSO and ordinary loops plus their shared registration fit together. Combining all send classes does not fit. E1 and E2 edges are behavioral prerequisites, not ordering convenience.

**Stops:** Wrapper retaining unrestricted `*Conn`, repeated forwarding of the old coordination contract, shadow accounting, altered send timing/fairness, widening the legacy seam, or performance failure after the declared replacement.

### E4 — Own handshake, ACK and PTO emission

**Delivers:** Normal coalesced handshake/0-RTT output, ACK-only output and PTO emission use the concrete owner. The module interprets those send modes and packs/registers/hands off internally, preserving handshake key retirement and all ordinary-loop wakeups. The connection retains handshake state and applies typed registration effects synchronously.

**Existing work / blockers:** New; E3 is required for the common emission owner, registration bridge and queue contract. No dependency on E5: probe policy and handoff can remain legacy through typed forwarding. Refresh only the E3 governing diff and any integrated E5 call-site changes.

**Owners / authority / temporary seams:** Emission owns these additional send sequences; connection owns keys, first-send/idle facts and MTU fallback, recovery owns packet history. No delayed registration-effect list. Move coalesced registration into one emission helper; legacy callers may forward to it but never re-register. Remove those old packer calls/interaction tests after parity. Probe/close forwarding remains bounded until E5/E6. No persisted authority is introduced.

**Blast radius / artifacts / representation:** Runtime migration and constructor-error lifetime corrections are shipped/safety; tests/benchmarks/receipts are aids/metadata. Domain is supported coalesced encryption levels, ACK-only/PTO modes, empty-content outcomes and typed handshake feedback. Existing packer/TLS own encoding. Universal ordering/lifetime only in these paths; no new retry, keyspace or congestion policy.

**TDD / closure / budget:** Fourth row and existing feedback preservation: normal coalesced send with client Initial-key retirement, server handshake, ACK-only under pacing/congestion, each of Initial/Handshake/application PTO, no-data PTO/PING, and representative private-allocation failure for each distinct constructor path. Reuse real 0-RTT/rejection, key update and handshake-MTU busy/stale/disabled-discovery tests. Handshake benchmarks and P2 apply; one replacement. No mandatory mutation. One initial review plus at most one replacement.

**Context budget:** ≤40k input tokens: slice/common order rules, current E3 bridge, coalesced/ACK/PTO methods and constructor failure exits, key-drop/fallback owners and relevant tests. Do not import all historical handshake work; use current H1 preservation contract and its shipped methods only.

**Split/merge audit:** Splitting ACK/PTO from handshake duplicates the common coalesced registration/key lifetime bridge across encryption levels. This bounded family is coherent together. Combining with path/close adds independent lifetime/addressing owners and is rejected. E3 is necessary for the shared owner; E5 ordering is only integration convenience.

**Stops:** Deferring key effects past a dependent packet, registration-time changes, new MTU/recovery policy, a second handshake-state owner, or constructor cleanup requiring a broad frame-storage redesign.

### E5 — Own probe emission and path handoff

**Delivers:** Direct server responses, direct client alternate-transport probes and queued MTU probes use typed emission operations with complete temporary-byte ownership. Path replacement uses the queue lifecycle interface while path policy and generation/MTU state remain with the connection. Preserve both replacement and in-place migration behavior.

**Existing work / blockers:** New; E3 is required for short-header registration and the concrete owner's queue lifecycle. E4 is not required because these are separate packet classes using that existing registration bridge. Preserve H1 feedback rather than reimplementing it.

**Owners / authority / temporary seams:** Path managers/connection produce probe intent and own path transitions; emission constructs, records, addresses and disposes probe bytes. One active worker is joined before replacement. The connection owns generation capture and fallback, with immutable feedback from the queue. Probe/path legacy access is removed; handshake/close legacy forwarding is not widened and is retired by E4/E6. No durable schema/state becomes authoritative.

**Blast radius / artifacts / representation:** Runtime emission/path handoff and lifetime corrections are shipped/safety; tests/captures are aids/metadata. Domain is the two existing direct-probe paths, queued MTU probing and existing migration seams. Native socket representation/error classification is retained. Universal ownership only within these operations; no public multipath or path-policy claim.

**TDD / closure / budget:** Fifth/sixth rows: server destination response, client alternate transport, queued MTU probe success/error, replacement with pending queue, in-place destination change and stale/current-generation feedback. Preserve qlog checksum retention. Use existing real migration and MTU tests and native handshake fallback classifiers. P2 plus focused probe/path allocations apply; one replacement. Race-test ownership/feedback. No mandatory mutation. One initial review plus at most one replacement.

**Context budget:** ≤35k input tokens: this slice/common direct-send contract, E3 registration/queue interface, both probe entrypoints, two migration seams, current H1 owner and focused tests. Path-validation algorithms are read-only context; neither their redesign nor a full path-state census is required.

**Split/merge audit:** Direct probes and path handoff share their actual address/queue ownership seam; separating them would leave probe storage crossing old/new path lifetimes. Adding handshake or retained close lifetime would widen the family. E3 is a real dependency; E4 is parallel-safe with serialized same-file integration.

**Stops:** Requeueing currently direct probes, changing error handling/destinations, multiple workers per active path, widening generation semantics, or requiring migration-policy redesign.

### E6 — Own close emission and retire the legacy seam

**Delivers:** Close construction hands off immutable retained bytes safely, and the remaining connection/packer/queue forwarding interface is removed. The final connection requests emission progress and handles outcomes; it no longer selects a packet-shape method or coordinates raw output registration/handoff. Remove the broad `packer` interface, its generated mock and superseded interaction tests only after their mode-specific replacements exist. Preserve focused `packetPacker` conformance tests. Qualify the composed final implementation.

**Existing work / blockers:** New; E4 and E5 must be merged, transitively E3/E2 and positive E1. The deletion census includes packer construction/token updates in both client/server setup and Retry paths, any close/error test use and generation directives; adapt administrative packer calls through narrow typed operations without recreating the taxonomy interface. No other open work is assumed valid.

**Owners / authority / temporary seams:** Emission owns close construction and temporary storage; the closed-handler path owns immutable retained bytes through its existing lifetime. Runtime emission is the sole output orchestration owner, connection retains lifecycle/receive scheduling, recovery/handshake/path modules retain protocol facts, and E2 remains queue-lifetime owner. Remove all transitional aliases and legacy forwarding; none remain. No new persisted authority.

**Blast radius / artifacts / representation:** Close lifetime and final runtime wiring/deletion are shipped/safety; replacement conformance/outcome tests and full matrix are aids; source map and receipt are metadata. Domain is existing close conditions and the finite census of all migrated call sites and construction/token users. Universal no-legacy-caller and lifetime claims are scoped to that census and the typed module; protocol conformance remains codec-owned.

**TDD / closure / budget:** Seventh row: local close with queued output, retransmission after poisoned construction-pool reuse, and existing remote/immediate/never-sent-client suppression. Inspect generated-mock consumers, finish their already-required outcome migration and remove obsolete generation. Run the full preservation matrix and full performance contract, including cold-close allocations; one replacement. If remaining test migration cannot fit, scoped re-handoff splits the contract step instead of asking the implementer to decompose it. One initial review plus at most one replacement.

**Context budget:** ≤40k input tokens: this slice/common contracts, current emission interface, closed-handler methods, finite remaining-caller census, current mock-generation references and focused tests, plus compact E1–E5 receipts and final diff. Do not load complete historical plan versions or raw measurements. The earlier migrations must remove their own mock choreography so final deletion is bounded.

**Split/merge audit:** A separate pure certification PR would leave product adoption dependent on an unmerged successor; each prior migration already passes its local gate and E6's composed check belongs with final contraction. Close construction is the last packet taxonomy user and its retention contract fits this deletion. Both E4 and E5 are necessary because their legacy callers prevent removing the broad interface.

**Stops:** Remaining consumers exceed the context budget, an accepted mode lacks replacement coverage, a new generic interface replaces the old taxonomy, retention cannot be made safe locally, or composed qualification fails/inconclusive after the allowed replacement.

## Acceptance and operating discipline

Each slice meets its exact end-to-end outcome, named ownership and representation contract, bounded temporary seam, tests and applicable performance gate before its own merge. E1 may merge a no-change receipt without making E3 ready. E2 can stand alone. E3–E6 runtime changes cannot merge on a promise that later certification will fix their behavior. E6's source census proves the scoped negative requirement that no legacy orchestration remains; tests alone do not establish a universal code-deletion claim.

The shared REVIEW-LOOP.md and CONTRACT-CLOSURE.md baselines supplied by `$implement-architecture-slice` govern. No repo overlays are present or copied. Compose them with ADRs 0001–0004, the source/representation domains and finite slice budgets above. Accepted findings are independently dispositioned; actual repeated-root stops require the shared side-by-side comparison. No historical unresolved packet-emission findings are grandfathered; audit history is linked. Diagnose failures before reruns; do not mutate the task contract to satisfy a reviewer.

The fork uses inherited workflows (`unit.yml`, `integration.yml`, `lint.yml`, `cross-compile.yml`, interop), not portfolio `ci.yml`/`ci-*`, and these may run on draft PRs. Open a draft, finish bounded review and local exact-head certification, push, mark ready and inspect the actual latest checks on that live head. Respect live protection requirements and do not invent skipped-job success. All applicable triggered verification must pass or be explicitly nonapplicable under the declared docs/runtime scope; investigate failures, with same-head reruns only for diagnosed external infrastructure faults. Merge by `gh pr merge --squash --match-head-commit <head>`, then append the journal after merge and finally update the child task.

For runtime slices, local equivalents are ordinary package tests/static checks and the named focused race/integration regressions on the actual pushed head. Use existing real-QUIC v1/v2, 0-RTT, key update, close/migration/MTU, GSO-disabled and qlog-enabled coverage relevant to the changed classes. Linux is required for GSO/native error behavior; inherited Linux/macOS/Windows CI provides support coverage. Extra platform combinations, traffic sweeps or fuzzing campaigns are outside the finite plan. Verification aids stay opt-in; ordinary test/benchmark enumeration must not reserve CPUs or start the sustained capture.

For this docs handoff only, the artifact domain is these authored Markdown contracts and the six explicit slice decisions. One independent slice audit before commit, source/link/format inspection and whitespace verification terminate the local docs gate. No runtime test campaign, prototype or implementation agent runs during handoff. Hosted checks actually triggered on the docs PR are inspected to completion before head-guarded merge. Record the merged default-branch plan commit before creating issues or mirrors. Issues hold that exact plan commit; OmniFocus holds current state and pointers only. Dispatch is left to the operator.
