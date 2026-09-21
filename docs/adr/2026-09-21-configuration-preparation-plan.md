# Configuration numeric preparation implementation plan

**Date:** 2026-09-21
**Status:** Accepted; not yet implemented
**Track:** C, 2 of 3 in QGF-ARCH-20260921
**Depends on:** Nothing; this slice is on the independent frontier.
**Related:** [Program index](2026-09-21-architecture-deepening-program.md), [ADR 0001](0001-upstream-compatibility.md), [ADR 0003](0003-follow-stable-upstream-releases.md), [ADR 0006](0006-explicit-external-packet-io.md), [N01–N20](2026-09-13-architecture-decisions.md).
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers, and stop conditions.
**Audit history:** [Slice audit and source/history dispositions](../audits/2026-09-21-architecture-handoff/README.md).

## Goal

Applications can supply settings when starting a connection/listener or return settings for an individual incoming connection. Share the numeric rules, while preserving the meaningful differences between those routes.

## Current shape

InitialStreamReceiveWindow and InitialConnectionReceiveWindow promise clipping to the largest encodable value, but both pass through validation and defaulting unchanged. A value of quicvarint.Max + 1 reaches transport-parameter encoding, whose length calculation panics. The client handshake constructor calls that encoding directly. This source trace concerns extreme application-supplied settings; runtime reproduction is an implementation acceptance gate.

Verified source anchors: `interface.go:125`; `config.go:25`; `config.go:75`; `connection.go:467`; `internal/handshake/crypto_setup.go:98`; `internal/wire/transport_parameters.go:479`; `quicvarint/varint.go:164`.

## Grilled decisions

### C1 · Is this still just tidying two helpers?

No. Keep the original callback clipping fix and include the newly traced initial-window gap. Refactoring alone could preserve both errors; the completion criteria must prove the promised bounds reach connection construction and encoding.

### C2 · Which limits should be shared?

Both initial and maximum receive windows, both incoming stream-count caps, and existing initial packet-size bounds/defaults. Preserve zero-as-default and negative-stream-count-as-disabled. Do not introduce new timeout restrictions or an initial-window-versus-maximum-window relationship in this change.

### C3 · Clamp oversized values or reject the connection?

Clamp where the existing comments promise clipping. Preserve the established initial packet-size normalization. Avoid new error categories for these numeric values; restoring the published behavior is smaller and more compatible than introducing rejection.

### C4 · Should every caller use exactly one identical operation?

Use two named private entry points in the existing configuration module: one for Dial/Listen, one for callback results. Share the numeric implementation beneath them. Avoid a boolean validation flag, a policy registry, or a new exported configuration representation.

### C5 · May preparation modify the application’s settings?

Preserve the existing in-place writes to direct Dial/Listen inputs, including their ordering before an unsupported-version error. Apply the newly required initial-window caps to the effective prepared copy; do not add new writes to those caller fields. The compatibility step mirrors only the shared clipping-stage result for MaxIncomingStreams, MaxIncomingUniStreams, MaxStreamReceiveWindow, MaxConnectionReceiveWindow and InitialPacketSize, before defaults or negative-stream-count conversion. Raw zero values and negative stream counts remain unchanged in the caller, including before an unsupported-version error; only the effective copy receives defaults and negative-to-zero conversion. The compatibility step reuses the shared clipping rules rather than duplicating bounds. Callback preparation works on a shallow local copy because it currently leaves the returned object unchanged; applications may reuse that object.

### C6 · Should callback Versions be validated like listener Versions?

No new callback rejection or renegotiation. The listener checks the packet version before invoking the callback and passes the selected header version to the new connection. Keep root version validation, preserve callback version-list behavior, and do not invent a second version-selection gate.

### C7 · What do nil inputs and callback errors mean?

Keep existing behavior: nil root settings use defaults; a successful callback returning nil uses defaults rather than inheriting every listener setting; a callback error refuses the connection. Do not recursively invoke a callback found in a returned Config.

### C8 · Should all nested values become independently owned?

No. Keep public Config.Clone shallow and preserve existing Versions references, callbacks, and TokenStore identity. A broad copy/immutability change is not necessary to fix numeric correctness and would add compatibility questions.

### C9 · When should preparation happen?

Dial still initializes Transport before validating configuration. Listen still reports missing TLS first and validates configuration before closed/duplicate-listener checks. Callback execution and refusal stay in the server. Moving pure default calculation earlier is acceptable only if it changes no visible error or initialization order.

### C10 · Can preparation safely run twice?

Do not assume so. A negative stream count becomes effective zero to disable streams; treating that effective zero as raw input again would restore defaults. Prepare raw settings once per entry route. Keep defaults and normalization together and test sentinel behavior explicitly.

### C11 · Should HTTP/3 preparation be included?

No. HTTP/3 has separate client/server defaults, ownership behavior, and custom dialing. Retain those tests and keep the root QUIC change independent.

### C12 · How much new architecture is justified?

One shared numeric-rule owner and two small role operations are enough. Reject a new immutable Config type, a constructor overhaul, general policy injection, and deep-copying everything. If the diff grows across unrelated construction paths, split out the numeric repair and reduce the refactor.

### Private interface direction

```go
// Private intent, not an implementation.
prepareConfig(input *Config) (*Config, error)
prepareConfigForClient(input *Config) *Config
// Both share numeric limits/defaults.
// Startup retains version validation and existing caller mutation.
// Callback preparation uses a local copy; no new version gate.
```

Names are illustrative private names. The behavior, ownership and ordering above are normative. No new public interface is authorized.

### Rejected directions — do not do this

- **Rejected: insert validateConfig directly into the callback path.** Would newly mutate callback-owned objects and reject versions that were not previously callback admission criteria. It also misses the two initial-window clamps unless the rules are repaired.
- **Rejected for this work: immutable effective-config type plus role policies.** Can prevent some misuse but broadens constructor changes, copying semantics, and maintenance surface beyond the evidence. No new type is needed to repair published numeric behavior.

**Non-goals:** No generalized lifecycle, activation, configuration-policy, or transaction framework; no new exported types, raw-socket authority, HTTP/3/root configuration merger, performance claims, or unrelated repairs. Preserve the decisions above and the accepted source owners.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| C1 | New; not yet implemented | Configuration numeric preparation through its real entrypoints and tests | None | None introduced |

## Implementation slices

### Slice C1 — Configuration numeric preparation

**Stable identity:** `QGF-ARCH-20260921/C1`. One intended PR; GitHub child: pending.

**What it delivers:** Applications can supply settings when starting a connection/listener or return settings for an individual incoming connection. Share the numeric rules, while preserving the meaningful differences between those routes. Acceptance criteria below define the complete one-PR outcome.

**Existing-work disposition:** New slice. No open implementation PR supplies a dependency. Preserve merged behavior at the cited source; related closed repairs and open adjacent investigations are dispositioned in the linked audit. No local or unmerged branch is an accepted baseline.

**Blocked by:** None. Recommended program order is priority, not a dependency.

**Single owner after merge:** The configuration module owns shared numeric limits/defaults. Its two named role operations own startup compatibility versus callback nonmutation/version applicability. The direct-input compatibility step mirrors only the five legacy fields from the shared clipping-stage result, before defaults or negative-stream conversion; it does not copy effective defaults back, duplicate bounds, or become a second rule engine. Existing listener version admission and callback refusal remain server-owned.

**Authority completeness:** No new persisted fact or restart representation. Typed in-memory constructors, validation, consumers and terminal cleanup on the changed seam are included. Durable storage/restart round-trip gates are not applicable; do not invent persistence tests.

**Transitional-seam budget:** Zero new temporary seams. No dual authority, temporary adapter, migration branch, or double-open lifetime is allowed. Existing compatibility behavior is permanent supported behavior, not a temporary seam needing a later slice.

**Blast radius:** Config mutation/alias compatibility, nil/default sentinels, error precedence, callback object reuse, listener version choice, and values passed into handshake/transport-parameter encoding. Shared references and TokenStore remain shared; HTTP/3 ownership/defaults stay separate. Only documented numeric bounds are repaired. No new parser, public type, rejection gate, dependencies, persistence, or allocation/throughput promise. Any constructor or public ownership redesign is outside the traced surface. No identified untraced effect is accepted. A newly discovered effect outside this traced surface invokes the stop conditions.

**Artifact classification:** Runtime behavior and private refactoring are shipped behavior; the accepted admission/bounds/authority checks are required safety enforcement. Tests, fixtures and existing CI are verification aids; no new analyzer, mutation harness or maintained verification product is approved. Plans, review receipts, journal and tracking pointers are process/traceability metadata. Required proportionate regression evidence does not authorize recursive completeness requirements for its aids.

**Representation contract:** Public Go Config values with their declared scalar field types, nil input, and callback nil/error/result outcomes. All seven numeric fields described in C2 use the existing public bounds and defaults. Existing protocol version validation owns root Versions; the already-selected listener version remains authoritative for callback admission. No external text grammar is added. The enforcement guarantee is universal within this supported typed domain; finite tests are representative evidence, not an exhaustive proof. Each quantified acceptance criterion is scoped to this domain and named owner.

**Contract closure:** Not triggered. Failure has material consequences, but the changed operations and supported semantic distinctions can reasonably be covered by the ordinary focused tests and finite budget below. Multiple callers or concurrent states alone do not satisfy the shared second trigger. The table below is a focused preservation plan, not a new closure policy. If source/review evidence establishes both shared triggers, apply the shared policy within this same outcome; a required larger family is stop-for-decision.

**Evidence budget:** At most 36 scalar boundary/default table rows, 9 route/compatibility cases, and 1 real transport-parameter encoding regression. Use representative semantic distinctions, not the Cartesian product of every field and entry route. Existing tests may discharge rows. No mutation required; at most one optional bypass of the shared upper-bound owner if needed to establish inherited coverage. One fully briefed fresh review, at most one replacement after accepted fixes. One exact-head local certification and the applicable existing hosted check suite per candidate. No extra repetitions, timing thresholds, fuzzing, platform cross-products or new hosted experiments are approved. Diagnose failure before reruns; stop when the bounded evidence passes and dispositions permit completion.

**TDD and preservation evidence:** Write the missing focused regression/characterization cases first, observe their pre-change behavior, then repair/refactor and retain relevant existing tests. Ship tests and implementation together in one green PR. No separately landed failing-test PR.

| Semantic case | Required disposition | Evidence |
| --- | --- | --- |
| Seven numeric fields and sentinels | Shared bounds/defaults; negative stream count remains disabled | Budgeted scalar boundary rows |
| Dial/Listen mutation and version errors | Preserve legacy mutation/error timing; initial caps affect effective copy only | Direct route cases include raw zero and -1 unchanged while effective values differ, plus the invalid-version error path |
| Callback reused/nil/error/version list | Copy without mutation; defaults/refusal/selected version preserved | Callback route cases |
| Initial window above encodable maximum | Effective value clipped; real encoding does not panic | Encoding regression |
| Shallow Clone and HTTP/3 custom role behavior | Unchanged aliases and role-specific semantics | Existing Clone/HTTP3 tests |

**Dispatch context budget:** One fresh agent context. Supply this current plan (including its sole slice), program binding rules, referenced ADR clauses, shared baselines and repository overlay; then only these source ranges/files: config.go; config_test.go; interface.go:103–199; transport.go:203–280; server.go:525–541,873–952; server_test.go:879–927; connection.go:337–342,467–473; internal/handshake/crypto_setup.go:80–100,245–258; internal/wire/transport_parameters.go:372–385,479–482; quicvarint/varint.go:161–179; http3/transport_test.go:219–295. Target at most 24,000 input tokens (including focused test extracts), leaving the rest of the fresh context for implementation, review disposition and verification. Read only the relevant existing-work row in the linked audit; there is no unresolved prior implementation review or governing contract diff. Do not replay the five report rounds or duplicate historical plans. If additional owners or an input bundle beyond this budget become necessary, re-slice before implementation.

**Slice decision audit:** The strongest split is numeric correction first and general preparation later. Here the accepted architecture is only the two small role entrypoints plus a shared rule owner; bounds and route integration fit one context. Splitting by field or route leaves supported settings paths inconsistent; introducing a separate effective-config representation merely for a prefactor is rejected. If a broader constructor change is required, stop and re-slice rather than enlarge this PR. Merging the other tracks would add unrelated lifetimes. There are no blocking edges: priority and shared-package test execution are not code dependencies.

**Stop conditions:** Stop for a decision if the accepted outcome, representation owner, authority, public contract, native capability scope, or one-PR/context boundary must change; if a required invariant cannot fit the declared finite evidence budget; or if the shared precise-root review/verification stop triggers after its required side-by-side comparison. No repeated-root conclusion may be based solely on a shared module or lifecycle. A failing existing test requires diagnosis; do not rerun, quarantine, relax timeouts, or absorb unrelated repairs automatically.

## Acceptance criteria

- [ ] C1: No. Keep the original callback clipping fix and include the newly traced initial-window gap. Refactoring alone could preserve both errors; the completion criteria must prove the promised bounds reach connection construction and encoding.
- [ ] C2: Both initial and maximum receive windows, both incoming stream-count caps, and existing initial packet-size bounds/defaults. Preserve zero-as-default and negative-stream-count-as-disabled. Do not introduce new timeout restrictions or an initial-window-versus-maximum-window relationship in this change.
- [ ] C3: Clamp where the existing comments promise clipping. Preserve the established initial packet-size normalization. Avoid new error categories for these numeric values; restoring the published behavior is smaller and more compatible than introducing rejection.
- [ ] C4: Use two named private entry points in the existing configuration module: one for Dial/Listen, one for callback results. Share the numeric implementation beneath them. Avoid a boolean validation flag, a policy registry, or a new exported configuration representation.
- [ ] C5: Preserve the existing in-place writes to direct Dial/Listen inputs, including their ordering before an unsupported-version error. Apply the newly required initial-window caps to the effective prepared copy; do not add new writes to those caller fields. The compatibility step mirrors only the shared clipping-stage result for MaxIncomingStreams, MaxIncomingUniStreams, MaxStreamReceiveWindow, MaxConnectionReceiveWindow and InitialPacketSize, before defaults or negative-stream-count conversion. Raw zero values and negative stream counts remain unchanged in the caller, including before an unsupported-version error; only the effective copy receives defaults and negative-to-zero conversion. The compatibility step reuses the shared clipping rules rather than duplicating bounds. Callback preparation works on a shallow local copy because it currently leaves the returned object unchanged; applications may reuse that object.
- [ ] C6: No new callback rejection or renegotiation. The listener checks the packet version before invoking the callback and passes the selected header version to the new connection. Keep root version validation, preserve callback version-list behavior, and do not invent a second version-selection gate.
- [ ] C7: Keep existing behavior: nil root settings use defaults; a successful callback returning nil uses defaults rather than inheriting every listener setting; a callback error refuses the connection. Do not recursively invoke a callback found in a returned Config.
- [ ] C8: No. Keep public Config.Clone shallow and preserve existing Versions references, callbacks, and TokenStore identity. A broad copy/immutability change is not necessary to fix numeric correctness and would add compatibility questions.
- [ ] C9: Dial still initializes Transport before validating configuration. Listen still reports missing TLS first and validates configuration before closed/duplicate-listener checks. Callback execution and refusal stay in the server. Moving pure default calculation earlier is acceptable only if it changes no visible error or initialization order.
- [ ] C10: Do not assume so. A negative stream count becomes effective zero to disable streams; treating that effective zero as raw input again would restore defaults. Prepare raw settings once per entry route. Keep defaults and normalization together and test sentinel behavior explicitly.
- [ ] C11: No. HTTP/3 has separate client/server defaults, ownership behavior, and custom dialing. Retain those tests and keep the root QUIC change independent.
- [ ] C12: One shared numeric-rule owner and two small role operations are enough. Reject a new immutable Config type, a constructor overhaul, general policy injection, and deep-copying everything. If the diff grows across unrelated construction paths, split out the numeric repair and reduce the refactor.
- [ ] Callers stop deciding which numeric preparation half to run. The module retains one implementation of each bound while hiding the direct-versus-callback ownership differences.
- [ ] Focused evidence and affected-package validation pass at the exact pushed head, with successful applicable hosted checks on that same head and no unresolved stop-for-decision disposition.

The typed-domain representation contract above bounds every “all,” “no,” “only,” “each” and “exactly” claim in these criteria. The case table and evidence budget terminate verification; tests do not claim complete schedule enumeration.

## Validation gates

```sh
go test . -run 'TestConfig|TestServerGetConfigForClient|TestTransport.*Config' -count=1
go test -race . -run 'TestConfig|TestServerGetConfigForClient|TestTransport.*Config' -count=1
go test . ./http3 ./internal/wire ./internal/handshake ./quicvarint -count=1
go vet .
go mod tidy -diff
```

Include the newly named regression tests in the focused selections; the commands above select existing families, not permission to omit a newly added case. The full applicable existing hosted suite remains required by the repository overlay, including native operating-system coverage and existing benchmarks; it is not a new performance claim or an extra measurement campaign. Run each local gate once on the final candidate unless changes or diagnosed failure require fresh certification. Additional package vet gates apply if another package is actually changed within the accepted contract.

## Operating discipline

The shared `../_shared/REVIEW-LOOP.md` review-loop and `../_shared/CONTRACT-CLOSURE.md` contract-closure baselines supplied by the active architecture skills govern, composed with [the repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). These resource paths resolve relative to the architecture-handoff skill, not this repository; implementing agents use the installed skill copies. No repository contract-closure overlay exists or is needed.

The composed policy covers representation/artifact gates, finite evidence and review budgets, semantic-family closure where triggered, review-and-verification-aware precise-root stops, exact-head local certification, same-head hosted CI, squash merge, post-merge journal and pointer-based tracking. This repository runs checks for drafts and uses its existing workflows, not a portfolio ci.yml/preflight fiction. Follow the overlay and diagnose before reruns. After merge, append-dev-journal, reconcile child/parent/tracker and complete the slice task; do not complete an unimplemented parent.
