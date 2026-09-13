# A13 architecture handoff audit evidence

This directory is traceability metadata, with frozen exploratory evidence. It is not a normative implementation contract. The [program index](../../adr/2026-09-13-architecture-deepening-program.md) points to the current plans.

## Provenance and limitations

Grilling inspected clean local and live remote main `fdb1af2a2c81e441ad3c855c355e136214e3fd77`, reverified before this handoff. The full report is archived as [2026-09-13 architecture improvement](x-devonthink-item://52B7FB36-ED4A-4354-9195-3939C74437D0); its technical decisions are captured in the current plans and N01–N20 ledger. Review-report rankings are historical discussion, not implementation requirements.

[manifest.json](manifest.json) records exact focused commands, platform and outcomes. The baseline focused root and HTTP/3 tests passed uncached. [diagnostic-results.txt](diagnostic-results.txt) records four intentionally failing subtests: Read/Peek wakeup after partial reset, and malformed batch counts leading to individual fallback writes. [diagnostic_test.go.txt](diagnostic_test.go.txt) is the original Go overlay source, saved as text so it cannot join the maintained test suite. The original overlay mapping used a temporary absolute file path; reproduce only by creating a fresh local overlay to this frozen source. No production or repository test changes occurred during diagnosis.

The wakeup probe uses a real ReceiveStream and mocked stream notifications. The batch probe uses real sendBatchEntries and a scripted existing fake adapter; it does not demonstrate a malformed production adapter or duplicate wire transmission. Server admission findings are source-supported schedules, not reproduced network incidents. Retained references/missed pool returns are not measured unbounded memory growth. No full suite, native Windows/Linux, performance or live-wire experiment was run during grilling.

[Minimal](design-minimal.md), [flexible](design-flexible.md) and [common-pattern](design-common.md) alternatives are frozen design exploration, not instructions. The current plans resolve their alternatives. No diagnostic, proof harness or additional performance campaign becomes a maintained blocking deliverable. Future implementation owns the finite behavioral evidence specified by its slice.

## Existing work and agent slice audit

[Existing work](existing-work.md) records open issue/PR/branch disposition and precise-root comparisons. No unmerged branch is a prerequisite; stale PRs remain untouched. [Receive/server audit](receive-server-audit.md) and [other-slice audit](other-slice-audit.md) record independent slice reviews and dispositions. The final docs PR records review and exact-head publication receipts separately from the normative contracts.

## Handoff acceptance boundary

- Capture the accepted technical decisions in current plan docs and the no-code ledger.
- Audit each one-PR slice for ownership, representation, artifacts, context fit, finite evidence, blast radius and genuine dependency edges before commitment.
- Merge only documentation and frozen evidence, then verify the exact reachable default-branch plan commit before publishing GitHub issues and the corrected OmniFocus mirror under cxMRcPXrCD9.
- Publish seven parent tracks, nine child slices and one tracker with pointer-only mirrors; no implementation or implementation-agent dispatch is authorized in this handoff.

Local documentation evidence is link/identity/graph validation and git diff whitespace checks at the pushed head, plus a fully briefed fresh review. Review budget: one initial review and at most one replacement, with shared low/nit handling. Hosted repository workflows apply on the exact head; no CI standardization or issue-specific historical waiver is authorized.

## Final slice-audit disposition

All substantive draft findings were accepted as fix-now within the existing contract: R1/B1 use ordinary focused evidence; S1 explicitly serializes owned-listener close completion without holding admission/completion locks; R2 permits bounded temporary release observation and uses separate reachable trim-copy success and first gap-limit failure cases. This corrects the earlier report’s overstatement of a copy-then-gap-error path without dropping the consuming handoff outcome. C1 source/normalization/Windows evidence, Q1 actual queue owner, H1 source path and M1 generation alias/artifact classification are corrected. Existing-work evidence is present. The other-slice reviewer verified all eight corrections; the parent independently checked R1–S1 F1–F4 against their audited source argument. All nine slice boundaries and the no-hard-edge graph pass. No stop-for-decision remains, and no new implementation or proof-harness scope was added.
