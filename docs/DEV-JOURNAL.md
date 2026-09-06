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
