# HTTP/3 response completion implementation plan

**Date:** 2026-09-13
**Status:** Accepted; not implemented
**Track:** H in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

`http3/server_conn.go:247–269` owns panic/hijack handling, reads responseWriter private header fields, infers Content-Length, flushes data and trailers, then cancels reading and closes the stream. `http3/response_writer.go:265–284` separates FlushError from logging Flush and independently logs trailer flush errors. The decodeHeaders test helper calls flushes without representing response completion and some tests continue writing afterward.

## Decision

Move normal non-hijacked response finalization behind one private void responseWriter.finishResponse() called once from server_conn. Preserve the exact headerWritten predicate, header-key presence semantics (present empty/nil Content-Length is explicit), and current byte count. Call existing Flush and then flushTrailers even if flushing logged an error. Server retains panic/hijack disposition, handler lifetime, CancelRead and stream Close.

**Rejected alternatives — do not do this:** Do not use headerComplete as the predicate, return early on flush failure, alter logging, or replace generic test decodeHeaders calls with finalization. One real caller is sufficient for private-field locality; do not claim a broad framework payoff.

**Non-goals:** No HTTP header parser consolidation, stream teardown move, new error policy, public API or response buffering change.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| H1 | New; frontier | Finish normal responses inside responseWriter | None | None |

## Implementation slices

### H1 — Finish normal responses inside responseWriter

**What it delivers:** Replace server-side private-field coordination with one private response completion operation while preserving headers, data/trailer attempts and teardown ordering.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** responseWriter owns response header/body/trailer finalization; server_conn owns handler and stream lifetime.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Normal response header decisions and failure ordering; preserve panic/hijack bypass, early flush and explicit Content-Length. No concurrency, public API, schema or allocation contract change.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Example-level characterization over existing responseWriter states; existing HTTP header and QPACK owners retain syntax validation. Universal method-local ordering of Flush then trailer attempt is an explicit two-operation finite sequence.

**Contract closure:** Not triggered: a single synchronous operation/deletion is covered by ordinary focused evidence; multiple independently reachable materially risky lifecycle paths are not being redesigned.

**Evidence budget:** At most eight characterization rows: empty/body, explicit key presence including nil, early flush, informational response, HEAD/bodyless, trailers, flush failure followed by trailer attempt, panic/hijack bypass. Reuse existing cases. One HTTP/3 package run; no race/platform expansion for a synchronous private method. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Characterize current normal completion through real server response behavior and failure ordering; run `go test ./http3 -count=1`. Keep decodeHeaders helper semantics intact.

**Dispatch context budget:** This slice, server_conn.go normal completion block, response_writer.go and relevant tests. One method extraction across two files, no connection history. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Splitting inference from flushing leaves coordination in the caller; merging Server admission adds independent concurrency risk. No blockers.

**Stop conditions:** Stop if the extraction changes error handling, needs new public interface methods or makes tests that keep writing call finalization. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

**Acceptance criteria:**

- [ ] Deliver the behavior stated in this slice's What it delivers field at its named owner.
- [ ] Preserve the explicitly listed existing behavior and satisfy the finite evidence budget.
- [ ] Introduce no temporary second owner or unapproved public/API/storage representation change.

Universal wording in these criteria is bounded by this slice's Representation contract and semantic classes; no external syntax or unknown consumer census is implied.

## Validation gates

Use each slice’s named focused commands and finite evidence. Follow the execution overlay for exact-head local certification and applicable hosted CI. Record actual test names if current naming differs, and verify selectors execute tests. Do not claim native behavior from compilation alone.

## Operating Discipline

The shared [review-loop baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/REVIEW-LOOP.md) and [contract-closure baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/CONTRACT-CLOSURE.md), supplied by `$implement-architecture-slice`, govern directly. Apply the repository-specific [execution overlay](../REVIEW-LOOP.md), [maintained conventions](../agents/conventions.md), and ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md), and [0005](0005-incoming-packet-lifetime.md). Do not seed a repository copy of either shared baseline.

One fully briefed initial review and at most one replacement review; independently disposition findings before fixes. Stop on a representation mismatch, an established repeated precise root, or required boundary expansion. Compare exact invariant, concrete enforcement seam, semantic classes, and why the earlier accepted family owned the later case before calling findings one repeated root. No recursive proof obligations on tests or frozen diagnostics. No new performance campaign, random stress expansion, unsupported platform cross-product, or timing SLA. Ordinary focused regression tests remain required evidence of the accepted behavior; no new verification framework is a maintained blocking deliverable.

After the exact reviewed head passes local certification and applicable existing hosted checks, squash merge, append the development journal, reconcile issue/frontier pointers, and complete the corresponding OmniFocus slice task. Do not complete the program parent until its actual children are complete. Existing open investigations and completed programs keep their scopes.
