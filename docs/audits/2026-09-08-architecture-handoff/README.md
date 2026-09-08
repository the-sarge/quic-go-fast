# Architecture handoff evidence and disposition

This directory is process/traceability metadata and frozen diagnostic evidence, not production code or an approved maintained verification framework. The current [program](../../adr/2026-09-08-architecture-deepening-program.md) and its exact plan slices own dispatch contracts. Diagnostic source is stored as .go.txt so ordinary build/test enumeration does not execute known-failing baseline probes. Raw module zip archives are intentionally not committed; the authoritative comparator source and exact result summary are retained.

## Source and scrutiny

Inspected baseline: a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf, clean default branch on 2026-09-08. Architecture run: 20260908T140201-379bc9b4a91f2859f270b80e. The [grilled assessment](grilled-assessment.md) captures all seven Strong groups and the final retain/defer rationale. The revised HTML copy is in [DEVONthink](x-devonthink-item://655C4856-8B01-4A15-BE46-FCC5DC8A5E35).

Baseline [receive/HTTP diagnostics](diagnostic-tests.txt) reproduce five missed terminal buffer dispositions, a false retention result, canceled-waiter usage retention and stale cache eviction. The [active-response diagnostic](response-lifetime-diagnostic.txt) additionally shows idle cleanup closing a real QUIC connection after mocked headers return with an unfinished controlled body. These are controlled regressions, not measured production incidence or full streaming acceptance evidence. Their source is [receive](ingress-diagnostic.go.txt), [initial HTTP](http3-diagnostic.go.txt), and [response lifetime](response-lifetime-diagnostic.go.txt). [Focused existing emission tests](existing-emission-tests.txt) pass; this does not certify an implementation that has not been written.

[Module zip results](module-zip-results.json) record a 22,410,202-byte baseline and 1,118,190-byte nested-module projection, saving 95.01035287410618%. The [probe](module-zip-probe.go.txt) uses golang.org/x/mod/zip v0.37.0 with Go 1.27.0. It is a local archive projection, not a release/download or runtime benchmark. Go’s module-zip implementation owns exclusion semantics. P1 must measure its actual candidate and preserve historical evidence.

## Precise-root correction record

| Observation | Exact invariant | Concrete enforcement seam | Semantic class | Relationship |
| --- | --- | --- | --- | --- |
| Canceled waiter retains usage | An acquired attempt is released once at actual termination | Proposed per-attempt lifetime replacing transport function-exit decrement | Pre-response cancellation | H1 requires this class |
| Headers release an active response | An acquired attempt is released once at actual termination | Same proposed lifetime with body/upload reporters | Successful headers before response completion | Initial recommendation was narrower than the complete accepted request-lifetime family; H1 is re-grilled to cover both |
| Old failure deletes successor | Eviction may remove only the expected current cached entry | Conditional map removal under Transport.mutex | Stale pointer after replacement | H2 is a distinct owner and invariant; sharing transport.go does not make it the H1 root |

These were analysis/design counterexamples before any implementation or PR review/fix loop, not two failed attempts at one central fix. Do not invent a repeated-root stop from their count. The corrected H1 accepted family includes outer gzip completion, upload completion, cancel/teardown and multiplexing; it does not convert #67’s separate pre-stream wait into this work.

## Existing-work disposition

GitHub issue #68 (audit-20260908-T5) and OmniFocus eeBsZFCZJNb are reworked/reused for H1. Its existing [audit](https://quic-go-fast-audit-a5bb7f9-20260908.sargent-joshua.chatgpt.site), [archived report](x-devonthink-item://BF1C5186-F4A3-4996-A873-70C6564EF321) and [conventions survey](x-devonthink-item://562E78F3-42C7-48B4-998E-52263ADC7874) remain provenance. No open PRs were present. #67 and #73/#72 are adjacent, independently owned scopes. Existing closed emission work and its accepted uncertainty remain intact; dormant old experimental branches are not dependencies.

The ingress audit reverses the minimum-delta zero-count parsing convention in favor of an explicit active reference. This local representation avoids premature pool reuse during synchronous handshake discard without a global module. It also adds I8 after inspecting failed Initial publication: calling closeWithTransportError before run waits indefinitely, while starting run as a workaround can remove the winning connection’s routing entries. I8 uses a bounded unpublished abort and depends on the I2/I5 owners.

## Slice audit

PASS before commit: three independent read-only audits covered I1–I8, H1/H2, and P1/T1 plus program/publication gates. Required corrections were dispositioned fix-now: add I3 → I4 for the accepted consumer-versus-drain outcome, specify a bounded actual-pool-return overlay, and explicitly require H1/H2 tests before production edits. The affected auditors rechecked and passed those corrections. The root audit confirms twelve one-PR/context slices, four genuine blocking edges, nine frontier slices, no implementation agent, and no remaining stop-for-decision finding. The H2/I3/I5 finite guarded-operation census uses ordinary focused evidence; multiple callers alone do not trigger closure. The original [issue #68 contract](existing-issue-68.json) is retained before its pointer-based update.

## Documentation publication evidence

Substantive docs follow the inherited workflow path recorded in the existing emission plan. No Taskfile/ci.yml/ci-* or copied policy overlay is introduced. Recent journal PR #63 is a same-head hosted-CI precedent, not a waiver of substantive architectural review. Final docs review, exact-head local certification, hosted run IDs and guarded merge receipt belong in the docs PR and final handoff report; child issues are created only after the accepted plan tree is verified reachable on default.

## HTTP request-input retry correction

Initial docs review 20260908T163636-8a0a3828a3a9b07c1593b6c9 at 3b5d1d7cffb8c30b79c1500912e6deb2f375c185 identified CODEXASTRA-F-001. Disposition: fix-now. `canRetryRequest` in http3/transport.go returns the original request for errConnUnusable, and the existing stream-opening retry test supplies no GetBody. The former blanket input-close requirement would break close-sensitive unread bodies before that supported retry. H1 now leaves untouched input with Transport’s retry decision while the failed attempt independently releases its usage; terminal decisions close input, and a successful successor’s uploader owns eventual cleanup. Direct ClientConn has terminal cleanup without an enclosing retry. The existing Retry evidence cell includes close-sensitive input without GetBody for successful retry and terminal failure.

This is a process-contract correction preserving runtime compatibility, not a production change. Its precise invariant is that unread retryable input remains open until the retry owner decides its disposition; the concrete enforcement seam is Transport’s retry decision and client-to-Transport ownership handoff. The earlier cancellation/header observations concern the distinct usage decrement owner and acquired-connection lifetime, not request-input reuse; a shared HTTP lifecycle topic does not establish a repeated root. A scoped independent H1 audit passed this correction before commit: unchanged accepted outcome, representation domain, one-PR/context boundary and finite evidence budget. The shared cheap docs-only correction policy permits skipping a further RAS review/verify cycle after lightweight checks and renewed exact-head certification; the initial review still must finish and all findings must be dispositioned.
