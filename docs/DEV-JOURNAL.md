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

---

## D2 receive storage experiment closed without runtime change - 2026-09-06 13:43 EDT

**Main:** `9cb9eb139353`
**Actor:** Codex

### Completed

Merged [D2 PR #11](https://github.com/the-sarge/quic-go-fast/pull/11) as `9cb9eb139353943ca2a09cea5972d1f4dcd9f2bd`, closing [#5](https://github.com/the-sarge/quic-go-fast/issues/5). Retained a 16-cycle refill/drain FIFO and overflow characterization plus the bounded experiment receipt. The runtime ring substitution was evaluated and removed; runtime behavior remains identical to the D1 comparison base. The product PR records D2 completion and the H1-only frontier.

### Decisions

Close D2 with the authorized bounded no-change disposition. Metadata savings were consistent (BurstDrain: 153600 → 147456 B/op and 130 → 128 allocs/op for 128 admitted messages), but timing reversed under observed host contention and remained inconclusive after the one allowed replacement pair. No additional campaign or runtime optimization is implied. [Evidence, exact versions, workload and raw samples](audits/2026-09-06-d2-receive-storage.md).

### Validation

Characterization passed on the base and evaluated candidate. Focused datagram tests, datagram race tests, inherited ring tests, the complete local suite including integration tests, and documentation checks passed on final certified head `8427902cce22c8d7e67adc13b75ec90e23c83565`. [Certification receipt](https://github.com/the-sarge/quic-go-fast/pull/11#issuecomment-5560989804).

RAS review `20260906T172521-9855c057dbd6e3d996b72c79` completed with all seven reviewers, adjudication and synthesis. Two accepted wording corrections made the retained test comment storage-neutral and aligned the D2 summary table with the no-change outcome. RAS rerun was skipped under the shared documentation-polish policy; remaining observations were rejected or duplicate reports. No deferred findings or unresolved stops remain. [Independent dispositions](https://github.com/the-sarge/quic-go-fast/pull/11#issuecomment-5560982631).

No Actions runs, check runs, branch protection or required status checks were reported after ready on the matched merge head. Hosted-platform evidence is unavailable, not passed. [Hosted inspection](https://github.com/the-sarge/quic-go-fast/pull/11#issuecomment-5560993772). No application-throughput, heap-size or physical-network claim is made.

### Next

Track D is complete: D1 shipped its overflow improvement; D2 closed with a bounded no-change result. H1 is the sole ready slice and has no blockers. [Program tracker #7](https://github.com/the-sarge/quic-go-fast/issues/7) owns live progress. Application integration and physical qualification remain externally owned.

---

## DATAGRAM parser copy removal adopted - 2026-09-06 21:30 EDT

**Main:** `fd8e5ee109b9`
**Actor:** Codex

### Completed

Merged [PR #16](https://github.com/the-sarge/quic-go-fast/pull/16) as `fd8e5ee109b9fda31d6ace644dd9914aa705fd30`. DATAGRAM parsing now borrows the bounded packet payload during synchronous handling; the queue retains its owning copy before admitted bytes escape to applications. Queue representation, FIFO, capacity, overflow, cancellation, close, public API and wire behavior are unchanged. This standalone follow-up does not reopen D1/D2 or their tracking.

### Decisions

Adopt for the consistent roughly 47% receiver-allocation savings and smaller CPU savings, accepting unresolved tail-latency risk. The original 4 Gbps 5% noninferiority bound was not established and has not been relaxed or relabeled as passing. More endpoint cores substantially improved measured system latency for both variants; that does not establish a parser-specific speedup. The [adoption contract](audits/2026-09-07-datagram-adoption.md) records the owner's decision and preserved scope; the [core-budget receipt](audits/2026-09-07-datagram-core-budget.md) records the last diagnostic. No further performance runs were added for adoption.

### Validation

Certified head `c184ad02337bd25a6f5ba5578d36e5ecd8a65e7d` passed ordinary package tests, focused DATAGRAM race checks, tools/version negotiation, full QUIC v1/v2 integration, FIPS, vet, gcassert and module-tidiness checks on macOS Go 1.27.0 and minimax Linux Go 1.27.1. The final 20-second, four-worker parser fuzz run passed 554,524 executions. Linux full package race validation skips only `TestFrameParserAllocs/STREAM`, after identical failure on base `686bce65`: race-instrumented `sync.Pool` deliberately discards entries, invalidating its zero-allocation assertion. That test passes normally; all remaining race tests pass and no DATAGRAM test is excluded.

Initial RAS review `20260907T010639-2739819f5c674b6682bcc541` completed with seven reviewers and found one required formatter correction in a test fixture. The implementing agent fixed it; exact-head verification cleared the defect. Replacement review `20260907T012359-a8aa15ab1dbd59fb5b2926d6` completed with three reviewers and no required fixes or follow-ups. Optional documentation and qlog-test strengthening did not demonstrate an unmet adoption obligation. No unresolved ownership or compatibility finding remains.

Enabled GitHub's fork workflow execution gate, which had prevented earlier Actions runs despite active workflow metadata. All 33 checks passed on the certified head before exact-head squash merge: [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34072522113), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34072522088), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34072522118), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34072522163) and [interop Docker build](https://github.com/the-sarge/quic-go-fast/actions/runs/34072522109). The inherited workflow layout remains in place.

---

## Receive-queue evidence merged - 2026-09-06 22:48 EDT

**Main:** `8272ec74344e`
**Actor:** Codex

Merged the completed receive-queue investigations as evidence: [Linux candidates #13](https://github.com/the-sarge/quic-go-fast/pull/13), [paced traffic #14](https://github.com/the-sarge/quic-go-fast/pull/14), and [isolated burst diagnosis #15](https://github.com/the-sarge/quic-go-fast/pull/15). Their original samples, profiles, recorded hashes and candidate patches are preserved. Production retains the current queue and the separately adopted parser-copy optimization; the head-index candidate remains experimental. The [closeout contract](audits/2026-09-07-d2-evidence-closeout.md) records the bounded archival scope.

Independent three-reviewer RAS reviews and final local certification completed. PR #15's initial review captured stale GitHub metadata; explicit verification and the replacement review used the correct pushed head. Accepted documentation corrections cover collector provenance, phase-specific power settings, zero-context patch application, the historical offered-rate summary omission, causal wording and CPU rounding. PR #15's final documentation-only corrections used the shared no-rerun policy. All hosted checks passed on each exact merged PR head: 33 for #13, 50 for #14 and 33 for #15. Root tests, DATAGRAM race coverage, lint/vet and bounded tagged Linux correctness checks passed as recorded in the PR receipts; all saved analyses reproduce exactly. No new performance campaign ran during closeout.

The retained Go measurement aids require the experimental build tag and explicit environment opt-in; the external pacer requires both flags. [Issue #18](https://github.com/the-sarge/quic-go-fast/issues/18) records a prerequisite to make the archived burst collector's failure cleanup unconditional before any future authorized reuse. The recorded run restored all CPUs, and no collector or CPU reservation ran during closeout. Completed D2 tracking remains closed. Review artifacts, certificates, merge receipts and a verified history bundle are retained at `/Users/josh/Documents/quic-go-fast-validation/d2-evidence-closeout-20260907`.
