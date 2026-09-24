# Issue 612 investigation contract

Status: approved by the user for one investigation PR. The [agent brief](https://github.com/the-sarge/quic-go-fast/issues/612#issuecomment-5820559296) owns acceptance; the [report](README.md) supplies the result. Review/certification history belongs in the PR discussion.

## Outcome and acceptance

Publish a revision-pinned decision on the bounded ECN ledger lifetime trade-off. Reproduce genuinely distinct retained history behind an unresolved prefix reaching conservative fallback, contrast equivalent suffix recompression, explain marking/late-feedback/ordinal/counter-fence/storage obligations, inventory current versus planned B6 diagnostics, and recommend retaining the trade-off or separately budgeting a future change. Report occupancy, marking and feedback eligibility without claiming production frequency. Publish a durable report link on #612.

## Boundaries and representation

The approved blast radius is this documentation directory and `internal/ackhandler/bbr_ecn_test.go`: one new deterministic characterization case and a small observation/assertion in the existing recompression case. Runtime, public APIs, dependencies, concurrency ownership, ECN policy, budget, retirement and diagnostics implementation remain unchanged. Existing actual-codepoint authority, late feedback, ordinal anchors, cumulative fences and bounded storage are preserved. Recovery loss, PTO, sampler expiry and migration do not acquire retirement authority. ACK performance belongs to #613; B6 owns planned diagnostics; T4 certification and the audited implementation frontier remain closed to this investigation.

Supported input domain: typed packet registrations and decoded ACK fixtures through the existing recovery test helpers, with explicit contiguous packet-number suffixes. `bbrECNTracker` owns ledger semantics; existing wire parsing is unchanged and parser conformance is not under test. Guarantee level: example-level regression evidence, with no universal protocol or lifetime claim. The work has no new external actor or threat model; synthetic fixture inputs isolate representation behavior.

Material artifacts: report, contract and receipts are process/traceability metadata; the test is a verification aid. No shipped behavior or required safety enforcement changes. No maintained-aid exception or authority to block unrelated shipped work is granted. Contract closure is not triggered: this investigation changes no enforcement owner or risky runtime invariant, and focused cases answer the bounded question. No semantic cross-product or recursive harness validation is required.

## Evidence and review budget

Use the four existing `TestBBRECNRangeBudgetFallback` cases and at most one additional `TestBBRECNDistinctACKedSuffixPinsRangeBudget` case. Run affected-package tests, `go vet ./internal/ackhandler`, and `go mod tidy -diff` for the Go test change. Finish with exact-head clean-tree, diff and relative-link checks and the repository's applicable existing hosted PR workflows. No statistical repetition, benchmarks, guard mutations, new platform gates or stress campaign is authorized.

Review receives this contract, the issue's verbatim acceptance criteria, the current report/test diff, relevant ledger/dispatch/design source, and unresolved findings only. Budget: one fully briefed initial RAS review and at most one replacement after accepted fixes, with source-review verification as needed. The implementing agent dispositions findings; RAS never fixes. Cheap docs-only corrections use the shared lightweight polish policy. Stop for any accepted obligation that cannot fit this boundary, evidence budget or one-PR shape, or a shared representation/precise-root approach stop. Adjacent findings do not silently expand the contract.

Completion requires the report and finite evidence, independent finding disposition, exact-head local and hosted certification, and squash merge. Only after merge, append the dev journal without RAS, publish the durable issue pointer, revalidate worthwhile deferred findings against merged code, and complete OmniFocus task `ndg-yo3ePu0`. There are no currently known blockers or untraced implementation effects.
