# Windows managed ECN successor handoff audit

Baseline: `260a51f6c76129e6972b223e42d6c52c464fa442`. Scoped parent: [#459](https://github.com/the-sarge/quic-go-fast/issues/459); program: [#354](https://github.com/the-sarge/quic-go-fast/issues/354). W1 remains complete through #534 with its frozen native receipt unchanged. No open implementation PR existed at preflight. Other platform contracts are unchanged.

## Scope and disposition

The user authorized the Windows handoff and added Windows 11 to qualification. Retain shipped Windows datapath, L1/D1/F1 implementations and W1 evidence. The program already uses bounded shared policies; no legacy proof obligation requires removal. Add W2 for desktop/server and dual-stack native qualification, then W3 for one complete managed implementation. Older Windows releases are explicitly unqualified rather than called broken. No runtime change or implementation dispatch is part of this publication.

## Slice audit

Independent read-only agent audit passed before publication against the pinned baseline. W2 is independently complete as a finite four-cell native decision receipt, with no runtime authority. W3 is one vertical capability through existing endpoint, Windows representation, adapter correlation and checked-result owners. No new temporary runtime seam or untraced effect was accepted.

The W2→W3 dependency is required by unresolved Windows 11/dual-stack facts; L1 is already merged. Splitting W3 into dormant encoding/parsing or a temporary single-family capability was rejected; merging W2 discovery into W3 would exceed the declared uncertainty/context boundary. The audited source manifest is approximately 59,700 bytes (14,926 characters/4 token proxy), consistent with the 30,000-token dispatch ceiling including selected tests and current contract. Hosted Windows jobs cover Go 1.26 and 1.27.

The audit requested one clarification, incorporated before commit: port platform-neutral shared-adapter guard tests into Windows coverage, keeping the existing x/sys/unix permission and oobConn decoder cases Unix-only. No other blocking finding or unresolved product choice remained. The current plan owns the substantive contract; no implementation agent was dispatched.
