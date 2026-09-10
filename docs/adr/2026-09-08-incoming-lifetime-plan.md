# Incoming packet-buffer lifetime Implementation Plan

**Date:** 2026-09-08. **Status:** Implementation complete; I1–I8 implemented. **Track:** I in QGF-AD-2026-09. **Depends on:** No other track. **Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stops. **Audit history:** [Handoff receipt](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-architecture-handoff/README.md). **Related:** [Program](2026-09-08-architecture-deepening-program.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md).

## Goal

Make ownership transfer, retention and terminal disposal explicit across existing receive owners without creating a receive engine.

## Current Shape (verified 2026-09-08)

`buffer_pool.go:21` splits references, `:27` only decrements, `:36` conditionally returns and `:45` requires exclusive final release. `connection.go:1054` currently allows zero-count storage while parsing coalesced siblings; `:1391`/`:2654` can report retention after rejected admission. `:572`, `:624` and `:938` show ordinary drain, detached replay tail and handshake discard. `transport.go:564`, `server.go:395` and `closed_conn.go:34` are consuming routes; `server.go:813`/`:839`/`:845` cover failed Initial handoff. `sys_conn.go:117` and `sys_conn_oob.go:143` have distinct owned/cached reader storage.

## Decision

Keep successive concrete owners. A consuming handlePacket transfers or disposes responsibility on every outcome. Establish an explicit active parsing reference before dispatching coalesced views; successful retention transfers a counted view and reports actual admission. Dispose retained views individually, not deduplicated by buffer identity. Seal admission under the enqueue synchronization before final drain. Each worker cleans its own receive-backed pending storage after its producer barrier; raw-reader cleanup reclaims only untransferred storage without closing caller-owned sockets. See [ADR 0005](0005-incoming-packet-lifetime.md).

**Rejected directions — do not do this:** Do not globally replace Decrement with Release or eagerly return shared storage while parsing; do not infer exact pool returns from refCount zero or sync.Pool.Get identity. Do not add atomic refcounts, a global lease registry, new goroutines or a broad receive module. Do not change parsing, protocol fairness, keys, qlog/checksum, path policy, socket ownership or public Close waiting semantics. Do not claim permanent leak elimination, pool exhaustion or a throughput gain.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Temporary seam |
| --- | --- | --- | --- | --- |
| I1 | Complete | Consume connection processing input on every exit | None | None introduced |
| I2 | Complete | Close connection-owned retained storage and admission | I1 | None introduced |
| I3 | Complete | Dispose terminal transport routes and closed-handler input | None | None introduced |
| I4 | Complete | Retire transport-owned queued receive storage | I3 | None introduced |
| I5 | Complete | Retire server-held 0-RTT groups deliberately | None | None introduced |
| I6 | Complete | Seal server admission before worker drains | None | None introduced |
| I7 | Complete | Make raw-reader cached ownership explicit | None | None introduced |
| I8 | Complete | Complete failed Initial construction and publication | I2, I5 | None introduced |

## Implementation Slices

### Bounded pool-return observation

The runtime contract is not a claim of exhaustive proof. For the listed cells requiring actual return/no-premature-return observation, use one disposable diagnostic source overlay at the exact candidate head: instrument the existing packetBuffer.putBack entry to record the chosen input’s return events while preserving its original pool operation. Keep the overlay and diagnostic test outside Git; use Go’s build overlay mechanism, keyed to the exact inspected source hash/span, with a single unambiguous instrumentation site. Do not build a general Go source scanner. Track only the input acquisition under test so later reuse of the same address is not mistaken for duplicate return. The probe adds no semantic cases or repetitions; it supplies observations for the existing cell budget.

Ordinary reference-count, preserved-byte and protocol-outcome tests remain maintained regressions. The overlay is a disposable verification aid, not a permanent production hook, API, analyzer or maintained blocking deliverable; its generality/completeness is not a gate. Retain its exact head, mapping, observations and concise limitations in a receipt. Run ordinary certification on the unmodified source after the probe, and verify production tree bytes are unchanged by observation. Do not infer exact return from refCount zero or arbitrary sync.Pool.Get identity. If the bounded overlay cannot observe the required seam without changing the representation/behavior under test, re-audit the evidence method rather than add permanent instrumentation.


### I1 — Consume connection processing input on every exit

**Status:** Implementation complete. I2 is ready after this slice merges. **Size:** M; one intended PR. **Blocked by:** None.

**What it delivers:** Give handleOnePacket an explicit active parsing reference; count a separate view before each actual header-handler dispatch, and finalize the parsing reference on every return. Header handlers either finish their view or retain it with truthful bounded admission.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** handleOnePacket owns the active parsing hold and final pool-return opportunity. Header handlers own dispatched views until decrement or successful transfer into connection-owned deferred storage. tryQueueingUndecryptablePacket reports actual admission.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** The input reference is the parsing hold. Split before each actual header dispatch; existing handlers finish or transfer the view. A deferred outer Decrement plus conditional pool return closes the hold even on Version Negotiation, first-parse rejection and fatal errors. This intentionally replaces the old zero-count-but-still-parsing convention. Observe pool return and preserved bytes; refCount zero alone and nondeterministic pool identity are insufficient evidence.

**Blast radius:** Coalesced parsing, synchronous key/handshake events, shared references, timestamps/checksums/qlog, protocol errors and replay are traced. Adds one reference increment/decrement pair per processing call, not a heap object or atomic; no performance gain is claimed. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Pool-backed receivedPacket inputs, including retained views replayed through handleOnePacket. Existing wire parsers own QUIC syntax; this slice owns the internal counted-view representation. Universal internal lifetime contract; example-level malformed-packet coverage, not parser conformance.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows record the accepted obligations and maintained regression evidence. The bounded disposable pool-return probe supplements these tests; its receipt belongs to the product PR.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Short packet success | Release after processing. | Covered: `TestConnectionReceiveLifetimeViews/short_success`; named slice owner |
| Coalesced long/long | Keep storage live throughout both views. | Covered: `TestConnectionReceiveLifetimeViews/long_long`; named slice owner |
| Long/short siblings | Preserve both packet behaviors and final disposition. | Covered: `TestConnectionReceiveLifetimeViews/long_short`; named slice owner |
| Malformed first header | Consume initial hold even without header dispatch. | Covered: `TestConnectionReceiveLifetimeMalformedFirstHeader`; named slice owner |
| Malformed or mismatched suffix | Dispose consumed views while preserving established parser policy. | Covered: `TestConnectionReceiveLifetimeViews/{malformed,mismatched}_suffix`; named slice owner |
| Version Negotiation | Finalization also covers the early return. | Covered: `TestConnectionReceiveLifetimeVersionNegotiation`; named slice owner |
| Fatal handler error | Return original error and finish unretained storage. | Covered: `TestConnectionReceiveLifetimeViews/fatal_handler`; named slice owner |
| Retained view | Transfer only its counted view; final parsing hold ends separately. | Covered: `TestConnectionReceiveLifetimeViews/retained_replay`; named slice owner |
| Rejected retention | Report false and finish the rejected view. | Covered: `TestConnectionReceiveLifetimeRejectedRetention`; named slice owner |
| Mixed retained/processed siblings | Do not recycle storage while parsing or retention remains active. | Covered: `TestConnectionReceiveLifetimeViews/mixed_retained_processed`; named slice owner |

**Evidence budget:** 10 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 24k input tokens: I1/common rules, ADR 0005, buffer_pool.go, roughly 500 lines of processing/header/unpack/retention routines and selected existing fixtures. Include current bounded diff, not whole connection history.

**Slice decision audit:** The active hold and truthful retention form one processing-consumes contract. Splitting them retains an ambiguous reference model. I2 adds run-loop/admission concurrency and fits a separate context; do not merge it. There are no convenience-only blockers.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### I2 — Close connection-owned retained storage and admission

**Status:** Implementation complete. I8 is complete; no I-track successors remain. **Size:** M; one intended PR. **Blocked by:** I1.

**What it delivers:** Seal ordinary packet admission atomically with final queue drain, dispose pending/ready retained views and detached replay tails, and handle synchronous handshake discard without returning active parsing storage.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** The connection goroutine owns retained queues, replay and tail disposal. receivedPacketMx owns ordinary enqueue plus the terminal-admission flag and drain. One private admission/drain operation is reusable for an unstarted connection by its exclusive constructor owner; it performs no socket, routing or protocol teardown.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Set terminal admission under receivedPacketMx before draining ordinary input; cached router pointers cannot bypass it. Retained queues are drained by their exclusive connection owner. A replay batch detached into a local variable requires explicit unvisited-tail disposal on error. Dispose each retained view, even when views share one buffer. This slice’s startup failure is run() startup, not failed server publication; I8 owns that separate state.

**Blast radius:** Queue locking, terminal state, handshake callback timing, scheduled replay fairness and error precedence are traced. No shared refcount mutation from a new goroutine. I1 is needed for retained discard while parsing is active. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Ordinary input queue, pending undecryptable views, scheduled replay views, detached active replay batch and legitimate producers calling Conn.handlePacket. Universal internal disposition after the owner terminates; no new public close deadline.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows record the accepted obligations and maintained regression evidence. The bounded disposable pool-return probe supplements these tests; its receipt belongs to the product PR.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Admitted input and overflow | Transfer on admission; reject with exclusive disposition. | Covered: `TestConnectionRetainedLifetimeOverflow`; named slice owner |
| Admission after terminal drain | Reject rather than enqueue. | Covered: `TestConnectionRetainedLifetimeTermination`; named slice owner |
| Admission racing final drain | Serialize through the same mutex; no post-drain orphan. | Covered: `TestConnectionRetainedLifetimeAdmissionRace`; named slice owner |
| Handshake discard with shared active input | Drop each retained reference without recycling parsing storage. | Covered: `TestConnectionRetainedLifetimeHandshakeDiscard`; named slice owner |
| Normal close with pending and ready retained storage | Dispose each held view. | Covered: `TestConnectionRetainedLifetimeTermination/normal_close`; named slice owner |
| run startup failure | StartHandshake/event failure performs final owned cleanup. | Covered: `TestConnectionRetainedLifetimeTermination/{start_handshake,handshake_event}`; named slice owner |
| Replay success | Transfer scheduled ownership and preserve order/fairness. | Covered: `TestConnectionRetainedLifetimeReplay/success`; named slice owner |
| Early replay error | Dispose unvisited local tail and newly scheduled work, not just fields cleared earlier. | Covered: `TestConnectionRetainedLifetimeReplay/early_error`; named slice owner |

**Evidence budget:** 8 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 28k input tokens: I2/common rules, accepted I1 reference invariant, about 650 relevant run/handshake/admission lines, startup paths and selected lifecycle fixtures. Exclude unrelated protocol parsing and old plans.

**Slice decision audit:** Overflow alone could ship but does not close the accepted admission-terminal-drain family. Retained/replay exits share the connection owner and fit one bounded context. I8 handles unpublished server construction separately; do not absorb routing teardown here. The dependency is necessary because retained disposal requires I1’s active parsing hold even during synchronous handshake events.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### I3 — Dispose terminal transport routes and closed-handler input

**Status:** Implementation complete. I4 is ready after this slice merges. **Size:** S; one intended PR. **Blocked by:** None.

**What it delivers:** Complete exclusive input disposition for transport terminal routing, closed-connection handlers and successful non-QUIC copy-out, while preserving successful ownership transfers.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Transport owns each whole datagram until transfer; its empty-input branch preserves the existing bufferless no-progress sentinel as a no-op; each closed handler consumes its own input; the successful non-QUIC reader owns its dequeued packet through copy-out.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Blast radius:** Routing side effects, qlog/drop reasons, stateless reset, non-QUIC bytes/truncation and closed-handler response behavior are traced. No shared-reference mechanics or worker scheduling change. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Pool-backed exclusive datagrams before connection splitting and the named routing outcomes, plus the existing empty, bufferless no-progress sentinel forwarded by listen after an OOB zero-message read. The transport empty-input branch releases only when an input buffer exists; a bufferless sentinel owns nothing and remains a no-op. Existing connection-ID/wire parsing owns syntax. Universal disposition at these listed terminal owners; no new representation is introduced and nonempty bufferless packets remain unsupported. Reader retry/cache behavior belongs to I7.

**Contract closure:** Not triggered for this finite transition set: named guarded operations are reasonably covered by ordinary focused tests; multiple callers alone do not trigger closure. P1 uses the authoritative archive representation and a finite content comparison; T1 is a bounded test migration without a new shipped safety owner. Ordinary focused checks suffice; do not recursively impose closure on verification aids.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Empty input | Dispose an owned empty datagram; preserve the bufferless no-progress sentinel as a no-op. | Covered: `TestTransportTerminalLifetime/{empty,empty_without_buffer}`; named slice owner |
| Connection-ID parse rejection | Dispose current reference; MaybeRelease alone is insufficient. | Covered: `TestTransportTerminalLifetime/connection_id_rejection`; named slice owner |
| No server available | Dispose. | Covered: `TestTransportTerminalLifetime/no_server`; named slice owner |
| Recognized stateless reset | Process then dispose. | Covered: `TestTransportTerminalLifetimeReset`; named slice owner |
| Non-QUIC disabled | Dispose. | Covered: `TestTransportTerminalLifetimeNonQUIC/disabled`; named slice owner |
| Non-QUIC full queue | Dispose rejected packet. | Covered: `TestTransportTerminalLifetimeNonQUIC/full_queue`; named slice owner |
| Non-QUIC copy/truncation | Preserve returned bytes/address then dispose. | Covered: `TestTransportTerminalLifetimeNonQUICRead`; named slice owner |
| Local closed handler send/suppression | Dispose after needed input metadata use. | Covered: `TestClosedLocalConnection`; named slice owner |
| Remote closed handler | Dispose terminal input. | Covered: `TestClosedRemoteConnection`; named slice owner |
| Successful forward control | Transfer ownership without releasing the forwarded buffer. | Covered: `TestTransportTerminalLifetimeForward`; named slice owner |

**Evidence budget:** 10 semantic cells as listed; the empty-input cell includes owned-buffer and bufferless-sentinel subcases, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. The directly in-scope process-crash regression discovered during replacement permits one scoped central correction, exact-head verification of that triggering review, and one final fresh review under the shared critical-safety budget exception; no further expansion is approved. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 18k input tokens: roughly 300 transport routing/non-QUIC/closed-handler lines, packet-handler contract and selected routing tests. Include one successful-forward control and source receipt.

**Scoped preservation audit:** [Empty-input producer clarification](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-10-i3-empty-input.md). The accepted outcome, single owner and one-product-PR boundary remain intact. The finite empty-input transition has ordinary focused coverage; closure remains not triggered. No new authority, transitional seam, public behavior or reader scheduling change is admitted.

**Slice decision audit:** Individual scalar drop sites could be separate tiny PRs, but their exclusive consume contract and finite terminal census fit one context. I4 has a different producer-stop obligation and remains separate. There are no convenience-only blockers.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### I4 — Retire transport-owned queued receive storage

**Status:** Implementation complete. I8 is complete; no I-track successors remain. **Size:** M; one intended PR. **Blocked by:** I3.

**What it delivers:** Drain pending stateless-reset input when its sending owner exits and pending non-QUIC storage after the listener stops producing. Preserve concurrent non-QUIC consumers and caller-owned sockets.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Transport.initOnce creates and publishes one stable non-QUIC channel before starting the listener. Listener completion is the producer barrier. The transport send worker owns stateless-reset consumption/final drain. A listener-terminal cleanup owner drains that same non-QUIC channel; channel receives give a concurrent reader exclusive packet ownership.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Initialize the bounded non-QUIC channel alongside the existing transport queues under initOnce, before listener startup; ReadNonQUICPacket must never replace it. Keep readingNonQUICPackets as the atomic opt-in flag so packets arriving before the first reader still follow the existing drop policy. The one channel allocation occurs per initialized Transport, not per packet; no new pool or synchronization module is needed. Return pending storage when its owning worker terminates. Do not strengthen public Close to wait for a blocked write. Any producer that can survive the claimed listener barrier is an approach stop.

**Blast radius:** Producer cessation, worker/channel consumption, public Close completion and caller-owned socket lifetime are traced. Do not drain outgoing close payloads as received buffers or join previously asynchronous writes. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Receive-backed stateless-reset and non-QUIC queued packets and listener/worker terminal states. Excludes outgoing immutable closePacket payload. Universal queued disposition when the corresponding owner exits, not a stronger public Close wait guarantee.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows record the accepted obligations and maintained regression evidence. The bounded disposable pool-return probe supplements these tests; its receipt belongs to the product PR.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Reset send success | Retain existing consume behavior. | Covered: `TestTransportQueueLifetimeReset` and `TestTransportStatelessResetSending`; named slice owner |
| Reset queue pending at listener stop | Sending owner disposes pending input. | Covered: `TestTransportQueueLifetimeReset`; named slice owner |
| Non-QUIC queue pending at listener stop | Drain after producer barrier. | Covered: `TestTransportQueueLifetimeNonQUICPending`; named slice owner |
| Consumer versus terminal drain | Concurrent first readers share the single initialized channel; publication, readers and terminal drain cannot replace or lose its queue. Exactly one channel receiver disposes each packet. Include the concurrent first-reader interleaving within this cell. | Covered: `TestTransportQueueLifetimeConcurrentReaders`; named slice owner |
| Caller-owned socket | Cleanup does not close it or introduce a network-write join. | Covered: `TestTransportQueueLifetimeReset` and `TestTransportQueueLifetimeNonQUICPending`; named slice owner |

**Evidence budget:** 5 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 22k input tokens: approximately 450 transport init/listen/send/close/non-QUIC lines plus socket ownership comments and focused fixtures. No raw-reader internals beyond its stop boundary.

**Slice decision audit:** Reset and non-QUIC queues could split, but both are bounded transport queues with the same producer barrier. I4 requires I3 because a consumer winning the race must dispose its copied packet; that consuming path omits release at baseline. I7’s reader cache is another owner and remains separate. Its I3 edge is required for disposal by a concurrent non-QUIC consumer.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### I5 — Retire server-held 0-RTT groups deliberately

**Status:** Implementation complete. I8 is complete; no I-track successors remain. **Size:** S; one intended PR. **Blocked by:** None.

**What it delivers:** Centralize server-owned 0-RTT group retirement for expiry, Retry, refusal and collision, preserving transfer to a successfully registered connection and existing queue bounds.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Server receive goroutine is sole zeroRTTQueues owner. One concrete group-retirement operation removes a group and releases still-owned whole datagrams; successful transfer empties ownership before deletion.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Use a single concrete retirement operation for this map, also reusable by I8. I6 can independently establish or reuse the same operation for final map drain under the same server owner; branch integration resolves shared helper declarations without duplicate state machines.

**Blast radius:** 0-RTT timing, limits, successful handoff order, refusal/Retry and collision map disposition are traced. I5 corrects map ownership, not the pre-existing unpublished-connection wait; I8 closes that separate obligation. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Existing bounded map/queue keyed by connection ID. Universal map-retirement disposition within listed causes; current timing, admission and policy limits remain unchanged.

**Contract closure:** Not triggered for this finite transition set: named guarded operations are reasonably covered by ordinary focused tests; multiple callers alone do not trigger closure. P1 uses the authoritative archive representation and a finite content comparison; T1 is a bounded test migration without a new shipped safety owner. Ordinary focused checks suffice; do not recursively impose closure on verification aids.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Expiry | Retire storage. | Covered: `TestServerZeroRTTLifetimeExpiry`; named slice owner |
| Retry invalidation | Retire invalidated group. | Covered: `TestServerZeroRTTLifetimeRetry`; named slice owner |
| Refusal | Retire group still held by server. | Covered: `TestServerZeroRTTLifetimeRefusal`; named slice owner |
| Registration collision | Retire group; unpublished connection cleanup remains I8. | Covered: `TestServerZeroRTTLifetimeCollision`; named slice owner |
| Successful transfer | Transfer each datagram before deletion; no sender release. | Covered: `TestServerZeroRTTLifetimeTransfer`; named slice owner |
| Bounded admission rejection | Dispose rejected input; retain accepted group. | Covered: `TestServerZeroRTTLifetimeAdmission`; named slice owner |

**Evidence budget:** 6 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 20k input tokens: about 350 lines of 0-RTT handling, cleanup, Initial/refusal branches and selected fixtures. Include collision cleanup boundary without full I8 constructor mechanics.

**Slice decision audit:** Deleting each cause independently would scatter one map-retirement invariant. Keep the family together. Server shutdown and failed constructor cleanup have distinct producer/publication owners and remain I6/I8. There are no convenience-only blockers.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.


### I6 — Seal server admission before worker drains

**Status:** Implementation complete. I8 is complete; no I-track successors remain. **Size:** M; one intended PR. **Blocked by:** None.

**What it delivers:** Stop receive admission under the enqueue synchronization before draining server input, remaining 0-RTT storage and receive-backed response queues at their respective owner exits.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** A server admission seam owns accepting/terminal state and enqueue. Server run owns received queue and final 0-RTT drain. Response worker owns Version Negotiation, Retry, invalid-token and refusal queues after s.running proves response production has ended.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** The response worker observes s.running before terminal drain, rather than assuming the close request itself proves the producer has stopped. Final map disposal has the same server owner as ordinary 0-RTT retirement; do not add a competing map state machine.

**Blast radius:** Server admission synchronization, receive/response worker barriers, listener acceptance, established connection survival and response wrappers are traced. Do not hold admission mutex while waiting for workers or change public wait/socket semantics. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Server-owned whole input datagrams, rejectedPacket wrappers and producer/worker terminal states. Universal owner-exit disposal; established connections survive ordinary listener close and public close wait semantics are unchanged.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows record the accepted obligations and maintained regression evidence. The disposable pool-return probe supplies direct return observations in the product PR receipt.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Accepted input | Transfer to server consumer. | Covered: `TestServerAdmissionLifetime/accepted_and_full`; named slice owner |
| Full rejection | Dispose locally. | Covered: `TestServerAdmissionLifetime/accepted_and_full`; named slice owner |
| Closed rejection | Dispose locally. | Covered: `TestServerAdmissionLifetime/closed`; named slice owner |
| Enqueue versus close | Synchronize terminal state with enqueue. | Covered: `TestServerAdmissionLifetimeEnqueueClose and TestServerAdmissionLifetimeReceiveExit`; named slice owner |
| Receive queue at exit | Seal before final drain. | Covered: `TestServerAdmissionLifetimeReceiveExit`; named slice owner |
| Response queues at exit | Worker drains after producer barrier. | Covered: `TestServerAdmissionLifetimeResponseExit and TestServerAdmissionLifetimeRetryResponse`; named slice owner |
| Pending 0-RTT at exit | Server owner retires still-held map storage. | Covered: `TestServerAdmissionLifetimeReceiveExit and TestServerZeroRTTLifetimeAdmission`; named slice owner |

**Evidence budget:** 7 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 24k input tokens: about 500 server constructor/admission/run/response/close lines and focused queue/close fixtures. No complete handshake or packet-packer history.

**Slice decision audit:** Splitting admission from final drain leaves the terminal promise incomplete. Response cleanup is bounded by the same receive-worker producer lifetime. I5 may be reused when present but is not required for a coherent final server map drain; no convenience blocker. There are no convenience-only blockers.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### I7 — Make raw-reader cached ownership explicit

**Status:** Implementation complete. I8 is complete; no I-track successors remain. **Size:** M; one intended PR. **Blocked by:** None.

**What it delivers:** Reclaim basic read failures and optimized-reader untransferred buffers at error/termination without reclaiming already returned packets or closing caller-owned sockets. Use a private optional cleanup capability after listener reading stops.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Each raw reader owns its unreturned storage. In the OOB buffer array, nil means transferred/unowned and non-nil means reader-owned. Successful extraction clears ownership before returning; the listener invokes cached-storage cleanup after its read loop ends.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Clear transferred slots immediately. Reclaim only non-nil owned slots, and refill them through the same reader. Reader cleanup does not close the UDP socket. Custom internal adapters without cached storage need no new cleanup obligation; do not broaden a public interface.

**Blast radius:** Batch slot lifetime, n/error policy, ancillary parse errors, nil ownership, listener termination and platform adapter coverage are traced. Retain current discard behavior for n>0 with error; do not introduce partial delivery. No packet refcounts become concurrent. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Existing basic/OOB raw adapters, internal slot array and authoritative ReadBatch count/error and OS control-message parser. Universal slot ownership within existing adapters; no partial-error delivery policy expansion.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows record the accepted obligations and maintained regression evidence. The bounded disposable pool-return probe supplements these tests; its receipt belongs to the product PR.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Basic success/error | Transfer only on success and reclaim on failure. | Covered: `TestBasicConn`, `TestBasicConnReadFailure`; named slice owner |
| Short/full batch progression | Refill owned/unowned slots correctly. | Covered: `TestOOBReaderBatchProgressionAndCleanup` (Linux); named slice owner |
| Batch error then retry | Reset valid-message metadata; no stale replay. | Covered: `TestOOBReaderBatchErrorRetry`; named slice owner |
| Zero-progress batch | Read again or return actual error; do not fabricate a packet. | Covered: `TestOOBReaderZeroProgress`; named slice owner |
| Ancillary parse error | Dispose the extracted buffer if no packet is returned. | Covered: `TestOOBReaderAncillaryFailure`; named slice owner |
| Terminal unread/cached slots | Reclaim only reader-owned slots. | Covered: `TestOOBReaderListenerCleanup`, `TestListenerReleasesReadBuffers`; named slice owner |
| Previously transferred input | Survives reader cleanup. | Covered: `TestOOBReaderBatchProgressionAndCleanup`, `TestOOBReaderListenerCleanup`; named slice owner |
| Caller-owned socket | Storage cleanup leaves socket lifetime unchanged. | Covered: `TestOOBReaderBatchProgressionAndCleanup`, `TestOOBReaderListenerCleanup`; named slice owner |

**Evidence budget:** 8 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 22k input tokens: about 400 raw-interface/basic/OOB/platform/listener-finalization lines and selected adapter fixtures. Include OS control parser calls, not their whole implementation.

**Slice decision audit:** Basic failure alone is tiny, but raw success/error/termination spans both supported adapters and the cleanup caller. A slot-schema-only prefactor is horizontal and creates avoidable transient ownership. Keep this concrete protocol together; exclude I4 queue ownership. There are no convenience-only blockers.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### I8 — Complete failed Initial construction and publication

**Status:** Implementation complete. No I-track successors remain. **Size:** M; one intended PR. **Blocked by:** I2, I5.

**What it delivers:** Return from server Initial generation/registration failures without waiting for an unstarted connection loop, abandoning owned resources or disturbing the already registered winning connection.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Server owns construction/publication and Initial before enqueue. The constructed unpublished connection owns its queued Initial and a narrowly scoped abort-unstarted operation. I2 owns admission/drain; I5 owns server 0-RTT retirement; packet-handler map remains the sole routing mutation owner.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Keep Initial enqueue before AddWithConnID so later routed packets cannot precede it. Generate connection ID before creating the tracer; generation failure releases the still-server-owned Initial and cancels the construction context, retaining existing 0-RTT expiry policy. Failed registration invokes a private abort-unstarted operation whose precondition is never registered and never run, reuses I2 drain, closes unstarted crypto, cancels construction contexts, then closes its qlog producer and invokes I5 group retirement. TLS Close is safe before Start; standard qlog tracing starts its own worker independently. Do not promise arbitrary tracer Close latency. Cancellation uses the generation error or local ConnectionRefused transport error and preserves an already established cancellation cause; cleanup errors do not replace the construction failure. It must not wait for ctx.Done, run protocol processing, drain an unstarted send worker, close a shared socket or mutate routing/reset-token entries. Verify each cleanup API is safe before startup; an unstarted wait is an approach failure.

**Blast radius:** Construction contexts, qlog producer, crypto startup, Initial ordering, registration collisions, reset-token/routing authority and zero-RTT retention are traced. Never start run merely to make blocking close finish: its normal teardown can remove the winning connection IDs. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Accepted Initial packets reaching handleInitialImpl and concrete server connections constructed but never registered or run. Existing wire/TLS implementations own syntax. Universal internal ownership/publication invariant in this bounded path, not a general startup/shutdown proof.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows record the accepted obligations and maintained regression evidence. The bounded disposable pool-return probe supplements these tests; its receipt belongs to the product PR.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Connection-ID generation error | Release Initial and cancel construction contexts; do not create tracer first. | Covered: `TestServerInitialLifetimeGenerationError`; named slice owner |
| Real failed registration | Complete without starting run; reclaim queued Initial through I2. | Covered: `TestServerZeroRTTLifetimeCollision`; named slice owner |
| Winning routing and reset-token entries | Preserve them; abort never invokes ordinary routing teardown. | Covered: `TestServerZeroRTTLifetimeCollision`; named slice owner |
| Losing crypto/qlog/context | Clean unpublished resources without waiting for protocol run. | Covered: `TestServerZeroRTTLifetimeCollision`; named slice owner |
| Losing retained 0-RTT group | Retire via I5 owner. | Covered: `TestServerZeroRTTLifetimeCollision`; named slice owner |
| Successful publication | Preserve Initial-before-later-packet ordering and single run/accept start. | Covered: `TestServerZeroRTTLifetimeTransfer and TestServerCreateConnection`; named slice owner |

**Evidence budget:** 6 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the listed new regression cells first (demonstrate failure or characterize unchanged behavior), then the relevant existing receive/transport/server families, one focused `go test -race -count=1` selection for the synchronization owners changed by this slice, and normal local certification. I7 additionally uses one Linux OOB behavior cell and a non-OOB Windows compile because those are different existing adapters; no platform cross-product. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 26k input tokens: I8/common rules, I2 drain and I5 retirement contracts, about 650 constructor/Initial/crypto-close/qlog/map-registration/destructive-close lines and selected concrete construction fixtures. Include current source map and bounded diff.

**Slice decision audit:** Generation failure alone could be a tiny PR, but it and registration rejection are the two failure transitions of this bounded constructor owner. Merging with I2 mixes running and unpublished lifetimes; merging with I5 mixes routing authority with map retirement. I2/I5 blockers are necessary shared enforcement owners, not convenience-only helpers. Its I2/I5 edges reuse the accepted admission/drain and 0-RTT retirement enforcement owners.

**Acceptance criteria:**

- [x] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [x] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [x] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

## Validation Gates

The exact per-slice evidence tables and command families above are terminating. Preserve existing regression tests on changed surfaces. The source contract is normative; raw diagnostics and historical failure logs are evidence rather than reusable acceptance harnesses. Do not require a permanent verification aid before a shipped correction unless its explicit maintained-deliverable contract says so.

## Operating Discipline

The shared `REVIEW-LOOP.md` and `CONTRACT-CLOSURE.md` baselines supplied by `$implement-architecture-slice` govern, composed with ADRs 0001–0004 and any repository-specific overlays actually present at dispatch. There are currently no `docs/REVIEW-LOOP.md` or `docs/CONTRACT-CLOSURE.md` overlays; do not create or synchronize copies. Apply artifact and representation gates, semantic-family closure only when triggered, independent fix-now/defer/reject/stop-for-decision dispositions, finite evidence and one initial review plus at most one replacement. Diagnose failures before reruns; repeated-root conclusions require the shared side-by-side invariant/central-owner/semantic-class comparison across review and verification history.

The repository uses inherited `unit.yml`, `integration.yml`, `lint.yml`, cross-compilation and interop workflows, not portfolio `ci.yml`/`ci-*`; they can run on drafts. Open a draft, finish the bounded review and local exact-head certification, push and verify clean state, mark ready, then inspect the latest applicable actual hosted checks on that exact live head. Runtime local certification uses ordinary package tests/static checks plus the slice’s focused regressions; use existing required Linux/macOS/Windows support checks rather than adding a platform matrix. Documentation-only local certification is scope/anchors/links/Markdown structure and `git diff --check`. All applicable triggered checks must succeed, not be silently treated as skipped. A same-head rerun requires a diagnosed external infrastructure fault; unresolved repository failures do not become passing evidence.

Respect live branch protection and merge with `gh pr merge --squash --match-head-commit <exact-head>`. Only after the primary work merges, use `$append-dev-journal` without RAS through a separate docs PR; then revalidate pending deferred findings and complete the matching OmniFocus slice task. Keep parent issues and task notes pointer-based. Exact code-head certification is distinct from the accepted plan commit and administrative mirror state. Shared-file merges require integration/revalidation, not invented blocking edges.

No new verification framework, permanent analyzer, CPU reservation, sustained traffic experiment, security campaign, new support platform or release publication is authorized by these slices. Historical emission adoption uncertainty and the closed E1–E6 evidence budgets remain unchanged. No implementation is dispatched by this handoff.
