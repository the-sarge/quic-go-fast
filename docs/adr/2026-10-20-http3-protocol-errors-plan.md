# HTTP/3 Detected Protocol Error Dispositions Implementation Plan

**Date:** 2026-10-20
**Status:** Accepted after independent slice audit; not implemented
**Track:** A20-P — 3 of 5 in A20
**Depends on:** Nothing; independent slices as stated below
**Related:** [Program index](2026-10-20-architecture-deepening-program.md), [no-code decisions](2026-10-20-architecture-decisions.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Source capture and slice audit](../audits/2026-10-20-architecture-handoff/README.md)

## Goal

Correct two independently shippable wire dispositions after the existing HTTP/3 parser detects invalid body framing or malformed trailer semantics. The stream module owns actions on its current stream/connection; parsers continue to own representation.

## Current Shape (verified 2026-10-20)

[`http3/stream.go:94`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/stream.go#L94) returns ordinary errors for DATA or HEADERS after trailers; the shared frame parser already handles selected forbidden frame types and ignores unknown extension types. [`http3/headers.go:193`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/headers.go#L193) distinguishes QPACK decode failure, field-size failure and malformed trailer semantics internally, but [`http3/stream.go:105`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/stream.go#L105) currently returns trailer failure without the required semantic disposition. Initial header QPACK errors are separately reset at stream scope in [`http3/server_conn.go:246`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/server_conn.go#L246) and [`http3/stream.go:423`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/stream.go#L423). qpack v0.6.0, also the latest published upstream release at capture, exposes neither invalidIndexError nor errVarintOverflow for reliable consumer classification.

## Decision

P1 handles detected HEADERS/DATA-after-trailers at Stream.Read by closing the connection with H3_FRAME_UNEXPECTED and returning the existing descriptive failure. Preserve parser-owned unknown-extension behavior and the first effective close. P2 distinguishes malformed trailer semantics with a private typed semantic error created only at the semantic validation exits inside parseTrailers (headers.go:213–220) and consumed at Stream.Read; call the existing tracked CancelRead/CancelWrite operations with H3_MESSAGE_ERROR before returning the failure, without adding open-half flags or inspecting private stream state. Do not infer semantics from string text or a catch-all non-QPACK error, because read, size and cancellation errors have different owners. The same stream owner serves request, response and public stream use. Keep shared validator contracts, decodeTrailers qlog placement, initial header behavior and trailer budget unchanged. Decoder failures, encoded/decoded size refusals and read/cancellation failures remain outside the new semantic type.

**Rejected alternative (do not do this): Close the connection for every frame after trailers.** Unknown extension frames can be legal; use the parsed HEADERS/DATA classes only. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Reset every trailer error with H3_MESSAGE_ERROR.** Compression, size, truncated reads and cancellation are not the same semantic failure. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Close the connection for every qpackError.** RFC 9204 §7.4 keeps decoder-limit errors on request streams at stream scope. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Classify private decoder errors by text, reflection or probing sentinel identity.** Borrows an unsupported dependency representation and cannot provide the accepted semantic guarantee. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Vendor or replace the decoder to finish this program.** Adds a maintained protocol parser owner and upgrade obligations disproportionate to this correction; the accepted report did not choose that ownership. **Revisit if:** explicit acceptance of local decoder ownership or an upstream typed-error release.

**Owner's design:** Adopt stream-owned dispositions driven by existing authoritative parsed frame/semantic results. The small typed semantic error communicates a fact; it does not carry a new caller state machine or a partial QPACK parser. For QPACK, the proper owner’s design is typed error classification in the upstream decoder. Wait for that contract, then re-audit its adoption; do not add a parallel byte scanner beside it.

**Non-goals:** No complete HTTP/3 conformance sweep, QPACK parser fork, dependency replacement, error-string classification, aggregate trailer budget, retransmission/retry change, generic error-policy callback interface or public-stream ownership change. Preserve N01/N05/N14 in the existing [architecture dispositions](2026-09-13-architecture-decisions.md).

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| P1 | new | Close on detected HEADERS or DATA after trailers | None | n/a |
| P2 | new | Reset streams for detected malformed trailer fields | None | n/a |

## Implementation Slices

### Slice P1 — Close on detected HEADERS or DATA after trailers

**What it delivers:** Correct detected post-trailer frame-sequence violations end to end, from the existing frameParser result through Stream.Read to peer-visible connection closure.

**Existing-work disposition:** New slice. The open-PR and issue census found no implementation PR or prior child for this accepted outcome; the audit record names the current source and search receipt. Retain the merged implementation as the behavior baseline except the explicit change here. Historical temporary diagnostics are evidence, not an implementation branch or maintained dependency.

**Blocked by:** None.

**Single owner after merge:** Stream.Read owns this framing disposition; rawConn/QUIC retain connection-close publication and first-close semantics.

**Authority completeness:** No persisted fact, schema, public configuration or restart format becomes authoritative. Existing constructors and QUIC/HTTP parsers retain their contracts; this slice changes only the named runtime owner and its supported consumers.

**Transitional-seam budget:** None introduced. No compatibility adapter, duplicate mutable representation or double-open lifetime is permitted; there is no successor cleanup slice.

**Blast radius:** Wire-visible failure disposition changes for the named detected class, including sibling streams when connection closure is mandated. Trace request, response and public Stream.Read, qlog invalid/parsed events, body completion, first-close behavior and already-finished halves. No external grammar, schema, decoder dependency or budget change. Existing malformed input parsing remains owned by authoritative parsers. Untraced new error categories stop the slice instead of entering a catch-all.

**Artifact classification:** Runtime behavior is shipped behavior; its selected guard/cleanup is required safety enforcement. Regression tests and temporary diagnostics are verification aids, with no general maintained harness approved. This plan, its issue/task pointers and review receipts are process metadata.

**Representation contract:** Parsed HEADERS/DATA after trailing HEADERS in supported request/response/public HTTP/3 streams. frameParser is the trusted representation owner; the guard owns a universal disposition over those two parsed classes. Unknown extensions remain parser-owned and outside the prohibited class.

**Contract closure:** Not triggered: the accepted parser/validator result classes and their common stream enforcement owner are covered by ordinary bounded focused tests. Multiple request/response/public consumers alone do not trigger formal closure. The table below is a semantic test plan, not an expanded closure obligation; do not cross syntax aliases or unrelated protocol errors.

**Evidence budget:** Valid trailers plus FIN; DATA after trailers; additional HEADERS after trailers; allowed unknown frame after trailers; preserve an already-applied parser connection failure. At most five semantic rows; exercise request and response consumers only where cleanup differs, plus one public-stream path. One focused/package/race pass; no mutations or repeated timing. One fresh review plus at most one replacement.

**TDD and preservation evidence:** Add failing real-peer code/scope assertions for both detected classes, a legal unknown-extension control and public Read path, then keep descriptive returned errors and logging assertions.

**Dispatch context budget:** Current P1, Stream.Read, frameParser.ParseNext, rawConn.CloseWithError, real HTTP/3 stream fixtures and N05. At most 12k context tokens; bounded governing diff in stream.go and relevant tests. No dependency edit.

**Slice decision audit:** Separating HEADERS and DATA would split one guard family and duplicate the same owner review; reject, none: rejected on merit. Merging P2 is unnecessary because semantic-field errors have a distinct representation and can ship green alone. No blockers; shared file overlap is a scheduling caution, not a dependency.

**Stop conditions:** Stop if the typed semantic distinction requires parsing error text or external QPACK bytes, public interface change, broadening header-size or cancellation policy, a new parser owner, or moving resets into multiple independent callers. A first-close or EOF half-completion conflict must be resolved within the same supported scope or returned for decision.

### Slice P2 — Reset streams for detected malformed trailer fields

**What it delivers:** Carry semantic trailer validation failure to the common stream owner and emit H3_MESSAGE_ERROR without terminating unrelated requests.

**Existing-work disposition:** New slice. The open-PR and issue census found no implementation PR or prior child for this accepted outcome; the audit record names the current source and search receipt. Retain the merged implementation as the behavior baseline except the explicit change here. Historical temporary diagnostics are evidence, not an implementation branch or maintained dependency.

**Blocked by:** None.

**Single owner after merge:** The existing trailer semantic validator creates the typed error fact; Stream.Read alone applies the reset. Existing QUIC stream halves own cancellation completion. CancelWrite after Close can still abort outstanding delivery; preserve that behavior, and require peer-visible reset only where the direction has not already completed at the peer.

**Authority completeness:** No persisted fact, schema, public configuration or restart format becomes authoritative. Existing constructors and QUIC/HTTP parsers retain their contracts; this slice changes only the named runtime owner and its supported consumers.

**Transitional-seam budget:** None introduced. No compatibility adapter, duplicate mutable representation or double-open lifetime is permitted; there is no successor cleanup slice.

**Blast radius:** Wire-visible stream failure disposition changes for the named malformed trailer class; the connection and independent streams remain usable. Trace request, response and public Stream.Read, qlog invalid/parsed events, body completion, first-close behavior and already-finished halves. No external grammar, schema, decoder dependency or budget change. Existing malformed input parsing remains owned by authoritative parsers. Untraced new error categories stop the slice instead of entering a catch-all.

**Artifact classification:** Runtime behavior is shipped behavior; its selected guard/cleanup is required safety enforcement. Regression tests and temporary diagnostics are verification aids, with no general maintained harness approved. This plan, its issue/task pointers and review receipts are process metadata.

**Representation contract:** Trailer field lists produced by the pinned QPACK decoder and rejected by the existing semantic validators. qpack owns encoded representation; parseTrailers/validateTrailerHeaderField own field semantics. Universal disposition for that typed semantic category; read, size and compression errors are deliberately separate.

**Contract closure:** Not triggered: the accepted parser/validator result classes and their common stream enforcement owner are covered by ordinary bounded focused tests. Multiple request/response/public consumers alone do not trigger formal closure. The table below is a semantic test plan, not an expanded closure obligation; do not cross syntax aliases or unrelated protocol errors.

**Evidence budget:** Valid trailer/FIN; prohibited pseudo-field; malformed field name/value; prohibited trailer field; read failure; size refusal; QPACK failure retains existing behavior; independent-stream survival. At most eight semantic cases; request and response routes where ownership differs, plus a public Read assertion. Reuse logging tests; one focused/package/race pass and no mutations. One fresh review plus at most one replacement.

**TDD and preservation evidence:** First demonstrate malformed trailer Read returning without the required stream error using a real peer. Add the private semantic classification at the validator and consume it at the stream owner. Characterize nonsemantic branches before wiring cancellation.

**Dispatch context budget:** Current P2, parseTrailers/validateHeaderFieldNameAndValue/validateTrailerHeaderField, decodeTrailers, Stream.Read, client/server trailer callbacks, body cleanup, qlog role tests. At most 16k context tokens. No full historical plan or decoder internals beyond the documented error limitation.

**Slice decision audit:** Splitting error typing from its consumer is an inert horizontal slice; keep both plus wire evidence together. Splitting request/response would duplicate a shared owner and leave family closure incomplete. Merging with P1 or header extraction increases unrelated outcome scope; reject, none: rejected on merit. No blockers; P1 is convenient priority only.

**Stop conditions:** Stop if the typed semantic distinction requires parsing error text or external QPACK bytes, public interface change, broadening header-size or cancellation policy, a new parser owner, or moving resets into multiple independent callers. A first-close or EOF half-completion conflict must be resolved within the same supported scope or returned for decision.


## Finite Semantic Coverage

| Semantic class | Disposition | Enforcement owner | Evidence |
| --- | --- | --- | --- |
| P1 parsed DATA / HEADERS after trailers | Connection H3_FRAME_UNEXPECTED | Stream.Read | Real peer sees code and sibling connection termination |
| Unknown extension frame | Existing permitted parser handling | frameParser | Positive extension control |
| P2 decoded malformed trailer fields | Stream H3_MESSAGE_ERROR | Stream.Read on typed semantic fact | Request/response/public paths and sibling survival |
| Trailer read/cancel/size errors | Preserve existing role result | Existing read/limit owners | One representative per differing failure category |
| QPACK decoder failures | Current behavior retained; new correction deferred | Existing QPACK/role path | No blanket connection/reset rule; dependency disposition below |
| Valid trailers and completed halves | Existing normal completion and documented cancellation semantics: CancelWrite after Close can abort outstanding delivery, while an already-observed FIN can precede a later reset | Existing stream lifetime owners | Valid FIN and body completion coverage |

Evidence status is planned, not yet run for an implementation. Completion means satisfying this terminating plan, not enumerating arbitrary syntax or schedules.

## QPACK dependency disposition — not a dispatchable slice

The QPACK part of the v7 recommendation cannot safely be specified as a small consumer-only change against v0.6.0. Invalid static-table references require a connection error, while decoder-limit overflow on a request stream requires a stream error. The needed error categories are private in the dependency, including on its inspected default branch. The accepted owner’s design is a typed classification contract exposed by that authoritative decoder, then one consumer change across initial requests, responses and trailers. No supported released contract exists at this capture.

**Disposition:** Defer the QPACK correction until an upstream typed-error contract is available; do not publish an implementation child for an unauditable future interface. It is not a blocker for P1/P2, the retry repair, simulator repair or request-header ownership. Record this limit in the program, not as an implementer task to invent a parser. A later scoped architecture-handoff must inspect the actual dependency release, choose the supported categories, re-audit the one-PR consumer migration and publish its exact contract before dispatch. Revisit trigger: a released typed-error contract or explicit acceptance of maintaining a local decoder fork. No upstream message or PR is sent by this handoff.

The RFC sources are [HTTP/3 framing](https://www.rfc-editor.org/rfc/rfc9114.html#section-4.1), [malformed HTTP messages](https://www.rfc-editor.org/rfc/rfc9114.html#section-4.1.2), [QPACK invalid static references](https://www.rfc-editor.org/rfc/rfc9204.html#section-3.1) and [decoder limits](https://www.rfc-editor.org/rfc/rfc9204.html#section-7.4). This plan deliberately does not certify complete protocol conformance.

## Acceptance Criteria

- [ ] P1: detected HEADERS or DATA following a successfully parsed trailing HEADERS causes H3_FRAME_UNEXPECTED at connection scope through Stream.Read; legal unknown extension frames retain parser-owned behavior.
- [ ] P2: trailer fields rejected by the current semantic trailer validator cause H3_MESSAGE_ERROR on the affected stream through Stream.Read, while an independent stream on that connection remains usable. Peer-visible reset assertions use directions whose completion has not already been observed; completed halves retain the existing cancellation behavior.
- [ ] P1/P2 preserve qlog event conventions, existing transport read/cancellation errors, header-size policy and the response/body/public-stream ownership on their changed paths.
- [ ] No broad QPACK conformance claim, handwritten representation classifier or silent dependency fork is introduced. Initial/QPACK-only dispositions remain explicitly deferred to the upstream typed-error prerequisite.

Each universal statement is limited to the slice’s declared domain and owner, with the guarantee and terminating evidence in that slice. No broader protocol, concurrency or platform certification is implied.

## Validation Gates

P1: write TestHTTP3PostTrailerFrameDisposition first and test peer-visible connection code plus continued acceptance of unknown frames. P2: write TestHTTP3MalformedTrailerDisposition first and test stream code, sibling-stream survival and nonsemantic error preservation. For each PR run its focused test once, `go test ./http3 -count=1`, `go test -race ./http3 -count=1`, `go vet ./http3`, and `go mod tidy -diff`, plus applicable existing hosted checks. Do not enumerate alternate wire encodings owned by qpack/frameParser or launch additional fuzz/security campaigns.

## Operating Discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern, composed with the existing [repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). The shared files are `REVIEW-LOOP.md`, `CONTRACT-CLOSURE.md`, and `DESIGN-COST.md` in the active architecture skill package’s sibling `_shared/` directory. Use those baselines directly; do not copy them into this repository. No repository-specific contract-closure overlay exists or is needed for this program.

Apply representation and artifact gates, independently disposition findings, and use one initial fresh review plus at most one replacement after accepted fixes. Verification is limited to accepted source findings. Apply the shared precise-root comparison and approach stops; a common package or lifecycle topic does not establish a repeated root. Do not grow the evidence plan to exhaust reviewer suggestions or recursively certify fixtures. Any replacement design must be compared by obligations, contract changes, then footprint.

Use a dedicated worktree and feature branch. Certify the exact pushed head using the focused gates below and the repository overlay; require applicable existing hosted checks on that head, then mark ready and squash merge with a matching head. This repository has no `task preflight` or `ci-*` workflow. Diagnose failed checks before reruns; only demonstrated infrastructure failure permits a same-head rerun. After the primary work reaches remote main, run `append-dev-journal` without RAS, revalidate deferred findings against the merge, and reconcile issue and OmniFocus pointers. Plan/mirror state does not substitute for code-head certification.

This document packages future work. The handoff run does not implement or dispatch it. Changes to representation ownership, accepted outcome, topology, adjacent scope, irreversible authority, or the one-PR boundary require a decision or a new scoped architecture handoff.
