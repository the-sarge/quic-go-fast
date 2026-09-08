# HTTP/3 pooled exchange lifetime Implementation Plan

**Date:** 2026-09-08. **Status:** Accepted; not implemented. **Track:** H in QGF-AD-2026-09. **Depends on:** No other track. **Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stops. **Audit history:** [Handoff receipt](../audits/2026-09-08-architecture-handoff/README.md). **Related:** [Program](2026-09-08-architecture-deepening-program.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md).

## Goal

Keep pooled connections active through actual request/response use and make eviction conditional on cached-entry identity. This is a lifecycle correction in existing modules, not a pool rewrite.

## Current Shape (verified 2026-09-08)

`http3/transport.go:216` acquires before dial wait and installs the decrement only at `:235`; it releases after response headers. `:420` removes by hostname, and `:530` treats count zero as idle. `http3/client.go:306` creates request completion and `:319` keeps cancellation alive after headers; `:414` launches asynchronous uploads. `http3/body.go:97` owns raw body completion, while `http3/stream.go:407` and `http3/gzip_reader.go:23` establish an outer decoded body. `RequestStream.Context` is write-side, not complete-exchange lifetime.

## Decision

H1 has one per-attempt lifetime reporting response and upload completion; H2 has one identity-checked eviction owner. They are separate invariants and independent PRs. Headers do not end usage, raw compressed EOF does not end decoded consumption, and ordinary body Close does not silently abort an ongoing upload. Cancellation/connection teardown terminate owned activity and close request input once, with release following uploader exit. Preserve unmanaged ClientConn behavior and the separately tracked Extended CONNECT wait (#67).

**Rejected directions — do not do this:** Do not release every successful acquisition merely on RoundTrip return; do not create a new pool module; do not change retry policy, public response behavior, shared dial context or shutdown-lock ordering. Do not use ContentLength/status or write-side context as full lifetime. Do not duplicate #68 or absorb #67.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Temporary seam |
| --- | --- | --- | --- | --- |
| H1 | Rework #68 contract | Preserve usage through the complete HTTP/3 exchange | None | None introduced |
| H2 | New | Evict only the expected cached HTTP/3 connection | None | None introduced |

## Implementation Slices

### H1 — Preserve usage through the complete HTTP/3 exchange

**Status:** Accepted contract; implementation pending. **Size:** M; one intended PR. **Blocked by:** None.

**What it delivers:** One pooled acquisition remains counted while either its delivered response or asynchronous request upload still uses the connection. Balance pre-delivery failure and cancel paths, and retain the count after headers until terminal activity ends. Use the same request lifecycle for direct ClientConn calls without introducing pooled accounting there.

**Existing-work disposition:** Rework existing issue #68 in place and reuse OmniFocus task eeBsZFCZJNb. Its original report is retained in audit metadata. No open PR exists and no implementation is grandfathered.

**Single owner after merge:** One concrete private per-attempt lifetime owns response completion, uploader completion, abort, observer cleanup and the single pooled decrement. Transport, the user-visible body and uploader only report events. The transport mutex continues to own cache publication; stream tracking continues to own stream bookkeeping.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Install cleanup immediately after successful acquisition, before waiting for dialing. Carry the release callback through private client plumbing. Ordinary body EOF/error/Close ends the response half only and preserves an active upload; request cancel, connection termination or pre-response failure abort owned stream activity and close owned input through one close-once path. Stream-opening failure before any input consumption is the explicit exception: the failed attempt finishes its own usage obligation, while Transport retains the untouched input through its existing errConnUnusable retry decision. Reuse that input unchanged if the existing retry is taken; close it on a terminal decision, or let the eventual uploader close it. Direct ClientConn has no enclosing retry and closes input on terminal failure. Do not close untouched input in the failed client attempt before Transport makes this decision; do not transfer usage accounting to the successor. Keep the cancellation observer alive until the whole exchange finishes; no observer survives final completion. The existing raw reqDone signal cannot remain an independent pooled-release authority, particularly through gzip. A body never read or closed on a live context remains active. Do not redesign the #67 SETTINGS wait or claim its responsiveness fixed; if the chosen lifetime design cannot safely separate that existing wait, stop for a scoped re-audit rather than silently absorb it.

**Blast radius:** Shared dialing, atomic usage totals, asynchronous uploads, request/body cancellation, decompression, trailers, multiplexing, retries and idle close are traced. Preserve initiating dial context, tracing, OnlyCachedConn and body-replay eligibility. No public API, dependency, wire or pool-module change. Never use RequestStream.Context (write-side completion) as full-exchange completion. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Supported pooled Transport.RoundTrip/RoundTripOpt attempts and supported http.Request.Body implementations whose concurrent Close unblocks Read. Existing standard-library, HTTP/3 and QPACK parsers own external representations; this slice owns internal attempt state. Universal usage invariant within that domain, supported by finite examples rather than an exhaustive concurrency proof. The separate pre-stream Extended CONNECT SETTINGS-wait responsiveness defect is owned by issue #67 and excluded from the cancellation-response-time claim.

**Contract closure:** Triggered: the accepted lifecycle invariant has material cleanup/compatibility consequences across independently reachable states. The table is the bounded semantic census, not a proof by example. Its owner is the named single owner above; rows are accepted obligations with implementation evidence pending.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Validation/acquisition failure | No usage acquired; do not decrement. | Named slice owner; regression/characterization required before completion |
| Canceled shared-dial waiter or acquired dial failure | Finish the acquired obligation once before returning. | Named slice owner; regression/characterization required before completion |
| Stream opening/header/pre-response failure | Abort owned stream activity; finish after any started uploader exits. Preserve untouched input on stream-opening failure for Transport’s existing retry decision; terminal disposition closes it. | Named slice owner; regression/characterization required before completion |
| Headers with active body | Retain usage; idle cleanup must leave the response readable. | Named slice owner; regression/characterization required before completion |
| Outer-body EOF, terminal read error or explicit Close; upload complete | Finish the response half and release once. | Named slice owner; regression/characterization required before completion |
| Response terminal while request upload continues | Keep usage and cancellation alive until uploader exits after input cleanup, trailers and write closure. | Named slice owner; regression/characterization required before completion |
| Cancellation after stream creation without another body Read | Cancel both stream directions, close request input once and finish after uploader exit. | Named slice owner; regression/characterization required before completion |
| Connection termination with an unread body | Perform the same terminal cleanup without changing error propagation. | Named slice owner; regression/characterization required before completion |
| Compressed source EOF before outer consumption, or outer decoder error before raw EOF | Observe outer-body completion; on terminal decoding error close/cancel receive cleanup while preserving the returned error. | Named slice owner; regression/characterization required before completion |
| HEAD, 204, length zero and successful CONNECT | Use actual body EOF/Close/cancel; never infer completion solely from status or ContentLength. | Named slice owner; regression/characterization required before completion |
| Multiplexed responses | Finishing one attempt retains the remaining counts; idle cleanup waits until no attempt is active. | Named slice owner; regression/characterization required before completion |
| Overlapping EOF/Close/cancel/uploader termination | One decrement and completed observer cleanup. | Named slice owner; regression/characterization required before completion |
| Retry | Each attempt has a separate balanced obligation; no lease transfer to the successor. Preserve unread input through an eligible errConnUnusable retry; a started uploader owns its original input, and any replay uses the existing GetBody policy. Characterize a close-sensitive body without GetBody, both retry success and terminal failure, within this cell. | Named slice owner; regression/characterization required before completion |
| Direct ClientConn and low-level RequestStream | Preserve API behavior; introduce no pooled usage for unmanaged connections. | Named slice owner; regression/characterization required before completion |

**Evidence budget:** 14 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Before production edits, write failing cancellation-count and active-response regressions for H1, and characterize early uploads, gzip completion, cancellation and teardown before modifying their reporters. For H2, write current/stale entry and delayed dial/request failure regressions before editing the guard or callers. Use the listed cells without expanding their scope. Then run `go test -count=1 ./http3` and `go test -race -count=1 ./http3`, once each at the final tested head. H1 additionally uses one real streaming/multiplexed exchange and one deterministic upload/early-response rendezvous among its listed cells; H2 uses controlled failure interleavings rather than timing sleeps. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 30k dispatch input tokens: this H1 contract/common rules, issue #68 and the short correction receipt, http3/{transport,client,body,stream,gzip_reader}.go and only their relevant fixtures. Include the current bounded lifecycle diff, not full historical plans or other tracks. Reserve the remainder of one fresh context for implementation, review disposition and verification.

**Slice decision audit:** A cancellation-only patch is independently green but leaves one accounting fact under incompatible lifetimes. A body-only successor would still release early for gzip, active uploads or cancellation. Keep these reporters with their single owner in one PR. H2 owns cache identity and can ship separately, so merging it only enlarges the context. There are no convenience-only blockers.

**Acceptance criteria:**

- [ ] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [ ] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [ ] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

### H2 — Evict only the expected cached HTTP/3 connection

**Status:** Accepted contract; implementation pending. **Size:** S; one intended PR. **Blocked by:** None.

**What it delivers:** An old failed request or dial waiter cannot delete a replacement cached under the same hostname. Route those cleanup callers through one conditional eviction operation.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Transport.mutex and a concrete conditional-eviction operation compare clients[hostname] with the expected entry pointer. Existing locked map-wide Close/CloseIdleConnections retain their own disposal authority.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Blast radius:** Cache publication/removal and racing failure cleanup are traced. Keep locked getClient failed-entry deletion, idle close, explicit close, cancellation and retry policy. No proactive close of evicted connections, shutdown-lock restructuring or public behavior change. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Changed runtime lifetime guards are shipped behavior and required safety enforcement. Tests/fixtures are verification aids; no new maintained analyzer, observer framework or harness is approved as a blocking deliverable. Plans, closure matrices, receipts and journal are process/traceability metadata.

**Representation contract:** Internal hostname-keyed map and expected entry pointer identity; existing authorityAddr owns hostname normalization. Universal identity guard in this internal domain; five finite regression classes.

**Contract closure:** Not triggered for this finite transition set: named guarded operations are reasonably covered by ordinary focused tests; multiple callers alone do not trigger closure. P1 uses the authoritative archive representation and a finite content comparison; T1 is a bounded test migration without a new shipped safety owner. Ordinary focused checks suffice; do not recursively impose closure on verification aids.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Current expected entry | Remove it under the mutex. | Named slice owner; regression/characterization required before completion |
| Missing entry or nil map | No-op. | Named slice owner; regression/characterization required before completion |
| Replacement occupies hostname | Preserve it. | Named slice owner; regression/characterization required before completion |
| Delayed old dial failure | Preserve successor via the same conditional operation. | Named slice owner; regression/characterization required before completion |
| Delayed old request failure | Preserve successor; retry and cancellation eligibility remain unchanged. | Named slice owner; regression/characterization required before completion |

**Evidence budget:** 5 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Before production edits, write failing cancellation-count and active-response regressions for H1, and characterize early uploads, gzip completion, cancellation and teardown before modifying their reporters. For H2, write current/stale entry and delayed dial/request failure regressions before editing the guard or callers. Use the listed cells without expanding their scope. Then run `go test -count=1 ./http3` and `go test -race -count=1 ./http3`, once each at the final tested head. H1 additionally uses one real streaming/multiplexed exchange and one deterministic upload/early-response rendezvous among its listed cells; H2 uses controlled failure interleavings rather than timing sleeps. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 14k dispatch input tokens: H2/common rules, transport implementation, relevant eviction/dial/retry tests, stale-failure receipt and current diff. H1 and its full body lifecycle code are not required.

**Slice decision audit:** Splitting the helper from its failure callers creates an unused or incomplete seam. Merging with H1 is unnecessary because cache identity and usage completion are separate facts. Both slices are independently green; shared-file integration is coordination, not a blocking edge. There are no convenience-only blockers.

**Acceptance criteria:**

- [ ] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [ ] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [ ] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

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
