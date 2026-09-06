# Development Journal

**Append-only. New entries go at the END of this file.**

Oldest entry first, most recent entry last.

---

## First milestone architecture handoff - 2026-09-06 12:28 EDT

**Main:** `cf32368f3bd5`
**Actor:** Codex

### Completed

Merged the audited first-milestone plans in [PR #1](https://github.com/the-sarge/quic-go-fast/pull/1). Published two track issues and three implementation children, with corresponding task-manager pointers. No implementation slice has started.

### Decisions

Preserve existing API/wire contracts, adopt through application-owned pinned module replacements, and base fork releases on validated stable upstream releases. [QGF-2026-09](adr/2026-09-06-fork-program.md) links the accepted policies and plans. The first milestone separates receive overflow allocation removal, receive storage reuse and explicit handshake message-size recovery; application integration and the existing external performance sweep remain externally owned.

### Validation

One independent slice audit passed after scoped corrections to healthy-path MTU preservation and ring-test evidence attribution. Authored Markdown links, slice identities and whitespace were checked on docs head `5c8c70c3c9a95e6902fee238f5919de482c14012`; PR #1 was squash-merged as `cf32368f3bd5896eb3c0f88f78dea160304fec3b`, and its audited document contents matched the fetched default branch. No runtime test, benchmark or physical-path result is claimed. The docs PR had no hosted check runs or required branch checks.

### Next

D1, D2 and H1 are ready but undispatched; D1 is the recommended starter. Serialize D1/D2 integration and allow H1 alongside either. [Program tracker #7](https://github.com/the-sarge/quic-go-fast/issues/7) owns live progress.

---

## D1 receive overflow allocation removal landed - 2026-09-06 13:09 EDT

**Main:** `67f7fdea9b9f`
**Actor:** Codex

### Completed

Merged [D1 PR #9](https://github.com/the-sarge/quic-go-fast/pull/9) as `67f7fdea9b9fa94d9d935af5dcb5b83a1a22fcc8`, closing [#4](https://github.com/the-sarge/quic-go-fast/issues/4). Receive capacity admission now precedes payload allocation/copy under the existing mutex, preserving queue limits, FIFO, payload ownership, notifications, and cancellation/close behavior. The product PR includes D1 completion and the D2/H1 frontier in the committed plans.

### Validation

The overflow allocation regression was red before the patch (1 allocation/drop) and green afterward (zero). Four bounded cases, focused datagram/race/ring checks, the whole suite including local integration tests, and documentation checks passed on certified head `99ec81c2b31c5475709336ef0c7f95d7e37d0766`. [Certification receipt](https://github.com/the-sarge/quic-go-fast/pull/9#issuecomment-5560802022).

One paired benchmark set on Go 1.27.0, darwin/arm64, Apple M4 Max, GOMAXPROCS=4, with 1071-byte frames and 10 × 200 ms samples removed overflow's 1152 B/op and 1 alloc/op. No >5% preserved-case timing regression was observed, with base-side drift qualifying confidence; the SteadyDrain timing difference is not an attributable improvement. [Bounded evidence and raw samples](audits/2026-09-06-d1-overflow.md). No application throughput or physical-platform result is claimed.

RAS review `20260906T165157-43dca5c8f574e5c8650c73fe` completed with one low receipt-qualification finding, independently accepted and corrected under the shared docs-only no-rerun policy. All seven initial reviewers completed; one adjudicator failed its CLI capability preflight, while remaining adjudication and synthesis completed. No deferred follow-ups or unresolved stops remain. [Disposition receipt](https://github.com/the-sarge/quic-go-fast/pull/9#issuecomment-5560796416). No Actions runs or required branch checks were reported on the matched merge head; hosted evidence remains unqualified rather than passed. [Hosted inspection](https://github.com/the-sarge/quic-go-fast/pull/9#issuecomment-5560804385).

### Next

D2 and H1 remain independently ready with no blockers; no new dependency was introduced by D1. [Program tracker #7](https://github.com/the-sarge/quic-go-fast/issues/7) owns live progress. Application integration and physical qualification remain externally owned.
