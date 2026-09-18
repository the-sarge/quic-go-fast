# Test-quality audit contract — issue 357

## Accepted outcome

Audit all maintained tests and shared infrastructure at source commit `07d8716ddd00b193f2c331d443a123c62a516cd6`, producing a traceable initial assessment, ranked evidence-backed findings, explicit deeper-inspection gaps, and focused repair recommendations. The operator approved this audit-only, one-documentation-PR plan on 2026-09-18. [Issue #357](https://github.com/the-sarge/quic-go-fast/issues/357) is the originating specification; [the report](../audits/test-quality-357/README.md) and [inventory](../audits/test-quality-357/inventory.json) are its deliverables.

## Acceptance criteria

The following criteria are quoted verbatim from the originating issue:

- Record the audited commit and a complete inventory of maintained tests and shared infrastructure, including explicitly justified exclusions.
- Assign every inventoried test or scenario group an assessment status and document any deferred deep inspection.
- Produce a ranked findings report with source references, affected tests/helpers, protected behavior, evidence, confidence, likely impact, existing issue links, and recommended next verification or repair.
- Identify recurring patterns separately from claims of a shared causal root.
- Propose focused repair batches and their dependencies, prioritizing shared fixture defects and invalid mandatory assertions.
- Summarize remaining coverage gaps and uncertainty without claiming that the audit proves the suite flake-free.

## Boundary and preservation

The work adds this contract and the new `docs/audits/test-quality-357/` report, inventory, and evidence documents. Existing tests, production source, public API and wire behavior, assertions, timeouts, dependencies, CI policy, and frozen evidence are preserved. No repair implementation, framework, persistent analyzer, or CI gate is authorized. Any source-confirmed defect in an audited file remains a finding for a separately scoped follow-up, not an instruction to edit it in this PR.

Review priority is shared helpers/cleanup; real-network HTTP, MTU, counts, loss and retries; fork-modified production paths; then other unit tests. Every maintained entry receives initial screening; deeper source inspection is selective and explicitly labeled. Assess meaning, defect detection, assertion validity, synchronization, isolation, determinism, and diagnostic value. Neither assertion counts nor naming patterns establish correctness. Preserve synctest's virtual-time semantics and exact counts that protect a declared controlled-scenario contract.

## Representation and artifacts

The finite domain is the pinned Git tree, including build-tagged platform variants, nested modules, testdata tests, fixtures, generated mock infrastructure, fuzz/benchmark entrypoints, and relevant workflow/scripts. Git owns membership; Go's standard parser owns declarations and source spans. Top-level test functions are scenario groups: their nested `Run` sites, loop domains, and local helper references retain traceability without executing dynamically generated test cases. Source spans are authoritative when an inventory preview is shortened. Platform suffixes and build constraints are recorded, not inferred from the host's executable package set.

The inventory guarantee is complete accounting of this finite source domain. Assessment conclusions are example-level, with explicit initial-screen versus inspected distinctions; there is no universal defect-detection or flake-free guarantee. The contract, inventory, report and receipts are process/traceability metadata. Temporary extraction and consistency tools are verification aids for this audit only, are not shipped, and retire after certification. No new verification aid blocks shipped product work. Contract closure is not triggered for this documentation change; no recursive harness closure or mutation matrix is required.

## Terminating evidence and review

Completion requires each maintained entry to have an initial assessment and source identity; justified exclusions; explicit deferred deeper questions; findings linked to source/evidence and current issue states; recommended repair scopes/dependencies; and a reconciled inventory and valid source/relative links. Preserve prior evidence and exhausted campaign budgets. Zero new product-test invocations, causal experiments, mutations, stress runs, statistical repetition or optional platform campaigns are budgeted for the audit. Required automatic hosted checks for publication remain applicable; they are not new causal investigations and passing runs establish no diagnosis.

Local certification on the exact pushed SHA/base consists of clean-tree identity, `git diff --check`, reconciliation of the finite inventory against the pinned tree/parser output, hashes/source anchors, and relative links. One fully briefed RAS review is allowed, verification of accepted report corrections if needed, and at most one replacement review. The shared finding-disposition and evidence ceilings apply. Supply this contract verbatim to reviewers with relevant unresolved history; keep run chronology and receipts in the PR discussion rather than expanding this contract.

Follow [the repository execution overlay](../REVIEW-LOOP.md): existing checks run on drafts; there is no `task preflight`, `ci.yml`, or draft-gated `ci-*` workflow. Require applicable hosted PR checks successful on the exact live head, then mark ready and squash merge with a matching head. Diagnose failures before any rerun. A repository test failure cannot be cleared by an unchanged-head retry or a historical exception; an unresolved publication blocker requires a maintainer decision. If the base advances, update the branch and repeat required review/certification/checks while keeping the audited source identity explicit.

After the report merges to remote main, append the development journal without RAS, revalidate proposed follow-ups against merged source, file only worthwhile surviving repair batches, and reconcile issue/OmniFocus pointers. Do not close unresolved historical investigations on the strength of this audit. Stop for a decision if completion requires widening the audit, changing product/tests/CI, reopening an exhausted campaign, or exceeding the accepted review/evidence budget.
