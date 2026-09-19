# Deterministic RNG conversion tests

The [accepted brief for issue #402](https://github.com/the-sarge/quic-go-fast/issues/402#issuecomment-5735108352) owns the 14 literal conversion cases in `TestRandomNumberConversions`. The outcome is deterministic conversion regression coverage in place of sample-average acceptance. `TestRandomNumbers` retains 1,000 real-randomness draws at bound 12345678 and both per-value range assertions. Trust `crypto/rand` for entropy quality.

## Contract and ownership

The supported representation is the brief's finite corpus of four-byte big-endian words, positive `Int31n` bounds, and literal expected outputs. The actual production `Rand.Int31` and `Rand.Int31n` methods own conversion semantics. The guarantee is example-level coverage, including exact input consumption; it is neither exhaustive correctness nor entropy-source certification.

The tests and their child-process fixture are maintained verification aids owned by `internal/utils`. Their operational payoff is deterministic coverage of byte order, high-bit clearing, masking, modulo conversion, rejection boundaries and a valid extreme sample. Retain them while these conversion methods exist; revise or retire the fixture if the methods or supported reader-substitution contract changes. This approved deliverable is a merge gate for #402. This document is process and traceability metadata. No shipped behavior or production safety enforcement changes.

Fixed readers are finite and sequential, substituted and restored only in the dedicated child process. The parent invokes the same test executable with an anchored selector, bounded test/process lifetimes, child-mode recursion prevention, captured diagnostics, and a completion marker emitted only after all 14 cases pass. Exhaustion, timeout, failed assertions, and missing completion fail the test. No parent-suite reader substitution, production hooks, dependencies or Go requirement changes are permitted.

The approved blast radius is RNG tests and focused policy/evidence documentation. Non-goals are invalid bounds, entropy certification, statistical thresholds, greasing and packet-number skip-period policies, global reseeding, generic randomness frameworks, performance work, frozen-evidence edits, and new platform or random-repetition campaigns. Contract closure is not triggered: this finite verification change does not establish a material-risk obligation requiring a semantic closure matrix.

## Terminating evidence and review

Use the following finite local gates, with caching disabled for tests. The repository minimum is Go 1.26.0; current CI uses 1.26.x and 1.27.x. Run the focused corpus on the minimum and current CI toolchain, affected-package tests and vet, one focused race check, module tidiness, formatting and diff checks. Existing hosted CI remains authoritative for its established platform matrix; no new matrix is introduced.

```sh
GOTOOLCHAIN=go1.26.0 go test -count=1 ./internal/utils -run '^TestRandomNumber'
go test -count=1 ./internal/utils
go test -race -count=1 ./internal/utils -run '^TestRandomNumber'
go vet ./internal/utils
go mod tidy -diff
git diff --check
```

On the implementation based on `c9203d6756c80db823ca11752b74bfab882f2fe9`, local Darwin/arm64 validation passed with Go 1.26.0 and Go 1.27.1. The full 14-case corpus, including 1,000 all-zero words producing 1,000 zeros, passed against unmodified production methods. A temporary `Int31n` mutation consuming one `Int31` draw and returning `n/2` passed the retained range test and failed nine exact-output corpus cases; the parent propagated the child failure and diagnostics. Production source was restored byte-for-byte afterward. No mutation machinery ships. These are finite assertion-sensitivity controls, not a measured flake rate or evidence of a production RNG defect.

The review budget is one fully briefed initial RAS review, verification of independently accepted fixes, and at most one fresh replacement review. Findings cannot expand the approved boundary or evidence budget. A required production change, representation mismatch, repeated precise semantic root, or boundary expansion stops for a decision. Exact-head certification, applicable hosted checks, squash merge, subsequent journal and task reconciliation follow the [repository execution overlay](../REVIEW-LOOP.md). PR discussion and RAS artifacts retain review chronology and certification receipts separately from this current contract.
