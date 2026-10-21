# Architecture decisions requiring no standalone implementation — A20

**Status:** Accepted after independent audit
**Normative scope:** Current dispositions and guardrails from the final grilled review; not a dispatch backlog
**Related:** [Program](2026-10-20-architecture-deepening-program.md), [audit history](../audits/2026-10-20-architecture-handoff/README.md)

The five plans own selected outcomes. This register preserves decisions that otherwise lived only in temporary reports, including the Sol/Opus refinements. Each “do not do this” records its trigger or rejection on merit. No new domain term or standalone ADR is warranted; existing terms and settled ADRs remain.

## Selected decisions and reversals

Use final client admission after tracking (R1), not a shared qlog-admission change. Use network-owned delivery cancellation before joins (S1), not only endpoint-first fixture cleanup. Correct detected post-trailer frames and malformed trailer semantics (P1/P2), while holding QPACK scope correction for upstream typed errors. Deepen only the initial request-header resource lifetime (H1), keeping the outer timer activation gate. Correct/document the installed all-generation BBR observation without runtime refactoring (M1). Mandatory send-interface migration is no longer selected.

## Deferred and rejected directions

| Direction | Current decision and reason | Revisit trigger |
| --- | --- | --- |
| Mandatory permit-aware send interface | Do not migrate the mocks now; production sconn already checks submissions. Optional compile-time `authorizedWriter` conformance check and accurate comment may accompany a real send-module change. It does not prove semantic checks. | Second production sender/wrapper, new direct-write path, relevant fallback defect or already-required mock migration |
| Routing lifetime facts | Do not add a registry or duplicate map metadata. Prefer a required read-only handler accessor when needed; closed handlers retain no live setup reservation and Transport retains closing receipts. | New handler or routing-lifetime change |
| Named byte-credit observations | Do not perform a generic snapshot/view migration now. Keep all-generation controller debt, current-generation supply, ECN drain and diagnostics distinct; M1 is only documentation/coverage. | Real consumer or accounting-rule change |
| Full retained-evidence store | Do not extract now. Mutation is centralized and #752 is fixed. Copied evidence is a future option; allocation/work claims need evidence. | Real recovery/ACK operation change or reuse, not merely an already-owed campaign |
| Narrow retained ACK operation | Keep as guidance: existing destination values, indexed discovery/removal and scratch lifetime behind one operation; caller keeps policy and sorting. | Same real recovery/ACK change |
| Retained representation/bounds | Do not replace algorithms, alter bounds or rewrite frozen oracles to ease refactoring. | New measured representation problem under an accepted contract |
| Packet-size owner | Do not introduce a coordinator. Existing reset/start/lowering helpers own most behavior; ACK growth is the narrow future operation. Preserve client/server ordering and first-path settlement. | Multipath, black-hole behavior or a concrete ACK growth change |
| Universal path epoch | Do not merge path, sample and MTU callback identity; lifetimes differ. | none: rejected on merit |
| BBR adapter/feedback rewrite | Do not combine unrelated allowance, recovery, delivery and diagnostic contracts or erase Reno’s path. | Real integration change with demonstrated duplicated obligation |
| BBR generation rewrites | Keep path/sample conventions and the purpose of the second size-dependent reset; no assertion-removal project. | Changed generation contract |
| Pacing/quantum helpers | Do not unify on resemblance alone; preserve units, overflow rounding and reverse ECN conversion. | Concrete same-contract duplication changed together |
| Constructor installer | Two constructors, not path replacement. Do not create a shallow wrapper over short role-specific sequencing. | Next Transport-scoped per-connection extension |
| Reader receive modes | Do not launch a native setter refactor. Endpoint owns qualification/retention; a future reader operation must own genuine storage transitions. Preserve sticky normalization and platform errors. | Real storage/cursor transition change |
| Managed capability snapshot | Do not conflate captured setup, current endpoint and live lease authority or infer nested locking from sequential locks. | A concrete incorrect accessor-lifetime contract |
| Probe bookkeeping | No demonstrated synchronous stall; retain identity, allowance and amplification guards. A full blocked challenge/resend test is useful when touched. Candidate clients cannot add paths, so the proposed bypass is refuted. | Real path-probe change |
| Setup completion policy | Do not split built-in and caller settlement into delegating no-op interfaces. Existing reservation owns exactly-once settlement. | Concrete duplicated policy obligation |
| Darwin batch preparation | Do not normalize observable callback control bytes as cleanup. Preserve route/native ordering and progress rules. | Accepted callback/batch contract change |
| Teardown consolidation | Do not unify unstarted abort with worker-joining close; another connection can own the CID. | Real duplicated idempotent resource operation |
| Handshake phase owner | Do not infer multiple production owners from fixtures. | Changed production confirmation ownership |
| Incoming queues | Keep Conn admission/seal/drain and I2; exposing locks/budgets through a queue abstraction adds shallow coordination. | Accepted incoming-lifetime change |
| Stream completion latch | Do not add another latch; both halves already own completion under locks. | none: rejected on merit |
| Universal HTTP/3 idle predicate | Do not conflate pool idle, GOAWAY drain and server inactivity. Pending initial headers do not suspend inactivity. | none: rejected on merit |
| Typed diagnostics | No reachable panic or schema obligation established. Do not create a module to remove assertions alone. | Schema or consumer change |
| Common HEADERS reader | Keep role differences. H1 is not a callback-based shared quota/error reader. | Concrete fourth cross-role change with enough caller obligation removal |
| Body/stream forwarding types | Keep method promotion, content-length, gzip and upload/receive lifetimes. | A demonstrated redundant responsibility, not forwarding count |
| Dead reqDone | Optional when maintaining the relevant path; keep hijackableBody and exchange completion. No standalone child. | Real body/client maintenance |
| Stale N09 slab citation | Update a maintained pointer when that decision is touched; do not rewrite historical evidence. | Maintenance of that ADR |
| Remote-address headroom/comment | Append can allocate; no demonstrated correctness bug. Preserve immutable snapshots and ancillary bytes. | Next send_conn change |
| Field/lock hiding methods | Do not hide syntax while leaving ordering obligations or add callbacks under locks. | A concrete operation removes a caller invariant |
| Delete deliveryStreams (withdrawn RAS candidate) | Do not delete: flow-blocked streams still supply observations. | none: rejected on merit |
| Queued 64 KiB native buffers (withdrawn RAS candidate) | The reviewed managed path copies into caller storage and releases native storage. Do not redesign on the disproved premise. | New evidence on a different retained path |
| Universal simnet callback cancellation | Do not change exported callbacks or close caller endpoints; S1 guarantees only owned endpoint delivery. | Accepted arbitrary-receiver shutdown contract |
| Generic no-op-after-close qlog wrapper | Do not replace scoped recording; admitted final events still must finish. R1 leaves rawConn ownership intact. | A separately accepted recorder contract change |
| QPACK consumer-only scope correction | Do not classify errors by string/reflection/private sentinel probes or own a fork implicitly. | Released typed error contract, or explicit local-parser ownership decision and scoped handoff |

## Policy guardrails

Keep the shared header capacity quota limited to pending initial request headers; charging trailers changes mid-body rejection and is outside this program. Correcting trailer error scope is separate from adding a quota. Preserve role-specific partial-field logging and error ownership.

The header timer also gates activation after decoding; it does not only fail reads. H1 retains that outer check. R1 preserves safe retry restrictions, request cancellation and GOAWAY; no error after sending becomes proof of unprocessed input.

Admission callbacks can receive additional control messages such as ECN according to route; consumers find source/interface metadata by type. Do not normalize callback buffers or reorder assembly as incidental cleanup.

Keep current Config/Transport placements and recorded consumer exceptions. Future per-connection behavior ordinarily follows Config; physical endpoint, lease and shared authority follow Transport. Do not move existing opt-ins merely for uniformity.

Keep Windows reported GRO state after the deliberate #860 fallback while retaining normalization. Configured/qualified, currently enabled and retained decoder are different facts; do not silently redefine the boolean.

For a future Reno preservation reference, prefer a released-bundle reference plus the prior descriptive arm after the separate #871 investigation and #887 decision. Do not absorb unexplained memory cost by changing the reference. Those live decisions and performance campaigns are not owned or advanced here.

Protect measured commits, but do not impose a blanket tag wait or shared-code measurement exemption. Check current per-change obligations. Documentation/test-only M1 does not modify the production binary; other shared-path changes are not pre-certified by that fact.
