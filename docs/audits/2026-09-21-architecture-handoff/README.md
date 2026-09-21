# Architecture handoff audit — 2026-09-21

**Program:** QGF-ARCH-20260921
**Status:** Passed independent pre-commit slice audit; accepted graph contains three independent slices. Implementation not started.
**Source revision:** `cf6fd5fcfa51b6fbbe0a5e14d8767f541e78f600`

## Provenance and authority

The user explicitly selected architecture-handoff after delegating the technical grilling decisions. The target OmniFocus parent is `gjr4K2ByhS5` (2026-09-21 - architecture improvement); it was empty at preflight. The accepted outcomes are captured in the three linked current plans; none depends on temporary HTML or conversation context. No implementation is authorized by this packaging run.

The preceding review compared Sol/Astra/Fable v1–v4 and RAS, then resolved 34 grilling questions. The current plans contain all those decisions. Historical ranking commentary is not a dispatch dependency. Source inspection found the initial-window clipping-to-encoder failure and concurrent-Close failure; neither was runtime-reproduced during the review.

## Existing-work census

GitHub open-PR query returned no open PRs at preflight. Open issues were inspected for overlap; no existing child implements these three scoped contracts. Existing local branches are not treated as baseline. Remote main at the pinned revision supplies merged behavior only.

| Existing work | Disposition and precise relation |
| --- | --- |
| Closed #74 repeated successful path validation | Retain merged behavior/tests. Its invariant is that a matching response completes the current successful reprobe while prior switch eligibility survives; its enforcement is validation-channel completion. New P1 owns terminal admission and channel closure under manager synchronization. The prior response-completion family does not establish post-close admission correctness. No repeated-root stop is inferred from sharing path_manager_outgoing.go. |
| Open #434 migrated old-path packet-count investigation | Separate, unchanged. Its observation concerns counter timing/packet origin during a switched transfer; it does not establish closed-path resurrection. No dependency, expansion or closure. |
| Merged #480 / closed #464 HTTP/3 configuration snapshot coverage | Retain. Test snapshot aliasing in http3/transport_test.go is different from root numeric bounds and callback preparation. No new copy semantics or repeated-root assertion. |
| Open #353 DF/PMTU, #354 and children #455–459 managed ECN | Separate capability programs. L1 does not advertise a capability, expose metadata or alter native setup. Their unmerged proposals are not accepted prerequisites. |
| Open #473 fork fuzzing workflow restoration | Separate CI work; no workflow modification is authorized here. Existing applicable hosted checks still govern. |

## Audited graph and slice decisions

P1, C1 and L1 each have no blockers. They are independent one-PR slices with zero new temporary seams. The priority P → C → L is not represented as dependency edges. Each plan records delivery, owner, authority completeness, domain/guarantee, artifact classes, finite evidence, context budget, blast radius, split/merge challenge and stop conditions.

The strongest further split for C1 is numeric repair versus broader preparation. The current proposal accepts only a small common numeric owner and two role entrypoints; no constructor-wide representation is permitted. P1 cannot split admission/retry/Close without fragmenting the invariant; L1 gains nothing from an unused descriptor-only precursor. Merging any tracks introduces unrelated obligations with no correctness dependency.

## Audit result

The independent read-only reviewer inspected the three draft contracts, program graph and source under the shared baselines. The reviewer accepted each one-PR boundary, context budget, owner, representation domain, zero-seam intermediate state, finite evidence and absence of blockers. The author independently concurred. No implementation or runtime testing occurred in this audit.

| Finding | Evidence and obligation | Disposition and verification |
| --- | --- | --- |
| C1 could accidentally mirror effective defaults into caller inputs | config.go:25–55 clips five raw fields, while :59–128 later applies defaults and negative-stream conversion; C5 requires historical mutation compatibility | fix-now, process metadata. Named all five fields and the pre-default clipping stage in decision/owner/acceptance; explicit raw zero and -1 preservation, including invalid-version ordering, fits the existing nine route cases. Local comparison confirmed the named fields exactly match current assignments. |
| L1 error table promised availability for already-occupied slots | ConfigureManagedPacketIOV1 rejects already registered/initialized transports; existing managed tests preserve those states | fix-now, process metadata. Decision/acceptance/table now require unchanged prior slot/lease state and no additional consumption; no new behavior or evidence scope. |
| P1 evidence did not explicitly name the accepted overlapping-waiter obligation | P10 requires current-signal re-fetch and all waiters waking on Close without a new validation-success policy | fix-now, process metadata. Allocated one deterministic overlapping-waiters/Close case inside the existing ten-case budget and focused race gate; no attempt epochs, extra budget or new success guarantee. |

These are cheap, high-confidence documentation clarifications within the accepted graph. The reviewer explicitly allowed local verification without a second broad review; the shared docs-only policy applies. No defer, reject or stop-for-decision finding remains. Further split and merge alternatives were rejected for the reasons recorded in each plan. P1, C1 and L1 are independently green frontier candidates after verified default-branch publication; priority P → C → L creates no blocking edge.

Local documentation verification checks the 34 decision records, three unique slice identities, zero blocking edges, all added relative file links, valid source-file anchors, and documentation-only diff. Exact pushed-head certification, hosted check and merge receipts belong in the docs PR discussion; this audit is not code certification.
