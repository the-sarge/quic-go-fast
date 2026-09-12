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

---

## Handshake MTU recovery landed - 2026-09-06 23:35 EDT

**Main:** `38c5d07357aa`
**Actor:** Codex

### Completed

Merged [H1 / PR #20](https://github.com/the-sarge/quic-go-fast/pull/20) as `38c5d07357aa1c9f7d86a57ad61be0e85d5c68d8`, closing [#6](https://github.com/the-sarge/quic-go-fast/issues/6). Classified native message-size failures from oversized Initial/Handshake flights now reach a bounded mailbox and a connection-owned 1200-byte fallback. Existing loss/PTO recovery, key lifetime, path/phase guards, disabled discovery, and congestion accounting remain intact. The product PR also records H1 completion and an empty implementation frontier in the normative plan and program index.

### Decisions

The review found that standalone 0-RTT application packets were initially misclassified as handshake flights. The existing enqueue owner now checks Initial/Handshake encryption levels; the eighth permitted focused test covers application-only and mixed datagrams. Optional fallback logging was rejected as a new merge obligation. See the [independent dispositions](https://github.com/the-sarge/quic-go-fast/pull/20#issuecomment-5564574400) and [fix receipt](https://github.com/the-sarge/quic-go-fast/pull/20#issuecomment-5564583946) for the bounded family and evidence.

### Validation

Both native-error UDP regressions timed out on base `c16a990872b1e8f443045240f7e4840f255c58e9` and established real handshakes with stream exchange on the candidate. Final reviewed head `0c511bde83b2b3a9bda80b09eb67cc5fd492714e` passed the full Go suite, focused race and integration gates, and all 17 same-head hosted jobs across unit, integration, lint, cross-compilation, and interop workflows. Initial RAS run `20260907T030645-20896f083382415f0f9b5fde` was fixed and exact-head verified; replacement run `20260907T032535-78377477dbd2215ac812a1df` was clean. No deferred findings remain. See the [local certification receipt](https://github.com/the-sarge/quic-go-fast/pull/20#issuecomment-5564629773) and [hosted certification receipt](https://github.com/the-sarge/quic-go-fast/pull/20#issuecomment-5564647938).

### Next

The merged [program index](adr/2026-09-06-fork-program.md) records D and H complete, with no successor slice to dispatch. Journal publication and mutable GitHub/OmniFocus closeout remain at this timestamp; [program tracker #7](https://github.com/the-sarge/quic-go-fast/issues/7) is the live view. Physical-path qualification remains externally owned and was not claimed by H1.

---

## Packet emission architecture handoff - 2026-09-07 00:58 EDT

**Main:** `75ce9dc16186`
**Actor:** Codex

Published the packet-emission architecture handoff in [PR #23](https://github.com/the-sarge/quic-go-fast/pull/23), with the accepted plan on main at `75ce9dc161864af241863afdf59a94f19e2e8136`. An independent source-backed audit accepted six bounded slices; the design preserves the connection goroutine and send worker and requires performance preservation before extraction. The earlier DEVONthink review is linked as rationale, and the completed datagram/handshake program remains closed.

Validation: the slice audit, source anchors, relative links, Markdown/whitespace checks and all 33 triggered hosted checks passed for the documentation PR. No runtime implementation, prototype or performance capture was performed during this handoff.

Next snapshot: E1 feasibility [#25](https://github.com/the-sarge/quic-go-fast/issues/25) and E2 queue lifetime [#26](https://github.com/the-sarge/quic-go-fast/issues/26) are independently ready. Six child issues have native parent/blocking links and matching OmniFocus tasks under the supplied parent. [Program tracker #31](https://github.com/the-sarge/quic-go-fast/issues/31) owns the live frontier and mapping. No implementation agent has been dispatched.

---

## Packet emission feasibility concluded inconclusively - 2026-09-07 05:05 EDT

**Main:** `1692d1739809`
**Actor:** Codex

### Completed

Merged [E1 / PR #33](https://github.com/the-sarge/quic-go-fast/pull/33) as `1692d1739809dfd086477f60698cf0755eba3a35`, closing [#25](https://github.com/the-sarge/quic-go-fast/issues/25). The product contains characterization tests and archived feasibility evidence, with E1 completion and the resulting frontier recorded in the plan and program index. The runtime prototype remains unadopted.

### Decisions

E1 concluded inconclusively under the accepted performance contract: all 120 matrix samples were valid, but P2 failed probe latency and missed-probe margins, P5 narrowly failed the probe latency margin, and stream churn failed its benchmark time bound. P1, P3, P4 and P6 passed all matrix margins. Four physical cores per endpoint improved delivered DATAGRAM throughput from about 2.93 to 3.72 Gbps at 4 Gbps offered load. This was one initial campaign on minimax using same-host loopback UDP; it does not qualify a physical link or other platforms. The partial prototype result interface and incomplete historical allocation invocation provenance are disclosed in the [feasibility receipt](audits/e1-emission/README.md). All temporary CPU isolation settings were restored.

### Validation

Independent RAS review `20260907T081528-7e4b074c7cec739bb0356bfc` completed, with every finding independently dispositioned in the [review receipt](audits/e1-emission/review.md). Accepted corrections changed documentation only; the shared review policy permitted skipping another RAS review or verification. No deferred findings survived closure. Final head `6a6da8977e6fefa06f29697938709ef6bd064ee7` passed focused emission tests, focused race checks, opt-in fixture compilation, the full Go suite and vet with Go 1.27.1 on darwin/arm64. Archived samples reanalyzed to identical summaries. All 33 hosted checks passed on that head, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34102647743), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34102647816), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34102647746), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34102647757), and [interop build](https://github.com/the-sarge/quic-go-fast/actions/runs/34102647749). Historical CI failures and reproduction limits remain recorded separately; no runtime fix is claimed for them.

### Next

E2 queue lifetime [#26](https://github.com/the-sarge/quic-go-fast/issues/26) remains independently ready. E3–E6 remain blocked because an inconclusive E1 does not authorize extraction; migration requires scoped re-handoff. Journal publication and mutable GitHub/OmniFocus closure remain at this timestamp. [Program tracker #31](https://github.com/the-sarge/quic-go-fast/issues/31) is the live frontier.

---

## Four-core packet-emission feasibility completed - 2026-09-07 13:07 EDT

**Main:** `ad60bc438bb1`
**Actor:** Codex

### Completed

Merged [E1 replacement evidence PR #37](https://github.com/the-sarge/quic-go-fast/pull/37) at `ad60bc438bb1d9143cf0323baf85aaf9237ef0d9`. The revised established-1-RTT normal/GSO prototype consumes progress, distinct stop reasons, deadlines and queue wakeup while retaining the authoritative packer/recovery/queue and existing execution model. It remains an inert patch; production runtime is unchanged. The [replacement receipt](audits/e1-emission-four-core/README.md) retains the complete minimax four-core campaign, bounded churn diagnosis, full allocation provenance and raw archives.

### Decisions

The [four-core deployment decision](audits/2026-09-07-emission-deployment-budget.md) removes two-core performance from acceptance. Under the [accepted resumption](audits/2026-09-07-emission-four-core-resumption.md), the remaining campaign and review allowances are now exhausted. The final result is inconclusive: P3/P4/P5/P6 meet the numerical margins, P1 p99 and stream churn exceed their numerical upper-bound limits, and capture tooling shared the endpoint cpuset, so dedicated-core qualification is unestablished. No runtime adoption or additional campaign is implied; historical receipts remain unchanged.

### Validation

All 100 matrix samples and 20 existing-benchmark invocations completed successfully; temporary CPU restrictions were restored. Focused allocations matched at 37 ordinary and 104 GSO, with a 48-byte embedded connection-size increase. The revised prototype passed full suite/vet/focused race on macOS and native Linux; the baseline Linux randomized-corruption Dial timeout remains disclosed and was not rerun. Replacement RAS review completed with five successful reviewers, one accepted placement-disclosure correction and one rejected missing-drain claim; both Claude subprocess failures and an AGY adjudication source-token rejection are retained. The docs-only correction policy skipped an additional RAS cycle. Final product head `7309630a12739dc0eef0003724dc1f6fe02c02c7` passed local full suite/vet/focused race/opt-in compile checks and all 33 hosted checks; see the [certification receipt](https://github.com/the-sarge/quic-go-fast/pull/37#issuecomment-5573759929).

### Next

E2 remains independently ready; E3–E6 remain blocked pending a new feasibility decision and their existing dependencies. The product PR already committed this frontier. [Tracking issue #31](https://github.com/the-sarge/quic-go-fast/issues/31) is the live view; issue and OmniFocus closure is being reconciled after this journal merge. Physical-link and non-Linux performance remain untraced.

---

## Queued-buffer lifetimes closed; staged adoption accepted - 2026-09-07 14:18 EDT

**Main:** `21b45f4615c7`
**Actor:** Codex

### Completed

Merged [E2 / PR #39](https://github.com/the-sarge/quic-go-fast/pull/39): fatal writes and stopped submissions now release packet buffers, and shutdown joins the worker before reclaiming queued leftovers. This preserves queue capacity, the successful send path, error causes and handshake-MTU feedback. [Issue #26](https://github.com/the-sarge/quic-go-fast/issues/26) is closed.

### Decisions

The owner [authorized staged packet-emission adoption with E1's recorded uncertainty accepted](audits/2026-09-07-emission-adoption-decision.md). E1 remains inconclusive, its prototype remains inert and no further E1 campaign is authorized. Later slice correctness and bounded performance checks remain in place.

### Validation

Final product head `ddcbde7090d8bc2cb8bcfd9185a541eb117fa8d5` passed the full Go suite, vet and focused queue/handshake-MTU race checks with Go 1.27.1 on macOS and minimax Linux, plus all 33 hosted checks. The [queue comparison](audits/e2-buffer-lifetimes/README.md) passed its 5% time margins with zero allocations per operation. One failed fixture capture is retained; the corrected bounded replacement completed all twenty invocations. The source/build provenance correction reproduces the recorded binary hashes without another benchmark run. Independent review completed with five reviewers and two provider failures; its one metadata finding was resolved under the shared docs-only correction policy. [Certification](https://github.com/the-sarge/quic-go-fast/pull/39#issuecomment-5574341697).

### Next

E3, ordinary/GSO packet emission, is the current implementation frontier; E4/E5 still depend on E3 and E6 on E4/E5. [Live program tracker](https://github.com/the-sarge/quic-go-fast/issues/31).

---

## Complete ordinary and GSO packet emission - 2026-09-07 17:40 EDT

**Main:** `f271602adabd`
**Actor:** Codex

E3's ordinary/GSO packet-emission owner and caller-buffer cleanup merged in [PR #41](https://github.com/the-sarge/quic-go-fast/pull/41), closing [#27](https://github.com/the-sarge/quic-go-fast/issues/27). The product PR includes its committed completion/frontier transition: E4 and E5 have their dependency satisfied; E6 still requires both. Their mutable tracking mirrors are being closed out after this journal, with the [program tracker](https://github.com/the-sarge/quic-go-fast/issues/31) as the live view.

The operator accepted uncertainty around one unexplained historical macOS randomized-loss timeout through [decision #43](https://github.com/the-sarge/quic-go-fast/pull/43). [Investigation #44](https://github.com/the-sarge/quic-go-fast/issues/44), labeled `needs-triage` and mirrored in OmniFocus, retains that nonblocking follow-up. The original failure remains failed and unexplained; no runtime, test, payload, loss or timeout behavior changed during certification continuation.

Final candidate `5bfed6994be0fb3d6dbf7c8250b152b0ecac1050` passed exactly one fresh uncached macOS Go 1.27.0 full suite, focused races, vet, golangci-lint and gcassert. All 33 applicable hosted checks passed on that head, including [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34163338141), [unit tests](https://github.com/the-sarge/quic-go-fast/actions/runs/34163338109), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34163338138), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34163338067), and [interop](https://github.com/the-sarge/quic-go-fast/actions/runs/34163338013). The [local certification receipt](https://github.com/the-sarge/quic-go-fast/pull/41#issuecomment-5575813442) binds commands, source and base. The prior initial review and bounded P3/P4/allocation evidence remain applicable because Go/test/module sources are unchanged from the reviewed candidate. No replacement review, performance campaign or green-seeking rerun was used.

Untraced effects remain the historical timeout's cause, physical-link/application performance and the ownership families assigned to E4–E6. There were no deferred review findings to file. E3's exact OmniFocus completion and successor readiness updates follow this post-merge journal; no routine frontier PR is needed.

---

## Complete handshake, ACK and PTO emission - 2026-09-07 19:56 EDT

**Main:** `67771b9fca25`
**Actor:** Codex

E4 merged in [PR #47](https://github.com/the-sarge/quic-go-fast/pull/47), closing [#28](https://github.com/the-sarge/quic-go-fast/issues/28). The concrete emission owner now interprets handshake/coalesced, ACK-only and PTO opportunities and performs construction, recovery registration and queue handoff. Connection-owned Initial-key retirement and handshake-MTU feedback remain synchronous. Migrated constructors reclaim unreturned byte storage on errors without refunding protocol state. The product PR also committed E4 completion and the resulting frontier.

The [E4 performance receipt](audits/e4-handshake-emission/README.md) retains both bounded campaigns. The initial P3 probe-p99 comparison failed; the single permitted revision restored a direct ordinary result return and the existing bounded PTO continuation. The replacement passed all margins, including probe-p99 upper ratio 0.968297 and handshake-time upper ratio 1.009273, with unchanged focused allocations. This does not establish a sole cause for the initial latency difference. The performance budget is exhausted.

Final head `2b4a4709f5026fe4d5783ade193fbedf6f31ea9f` passed uncached full suites, focused races, vet, gcassert and Go fix checks on macOS Go 1.27.0 and native Linux Go 1.27.1, plus local lint and Linux qlog coverage. All 33 hosted checks passed, including [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34170838030). The [certification receipt](https://github.com/the-sarge/quic-go-fast/pull/47#issuecomment-5576943933) records the exact source/base, commands and hosted runs. A hosted Go formatting failure was corrected with a test-only range loop; production sources remain identical to the passing performance candidate.

The bounded initial and replacement reviews completed with no accepted fix-now or stop-for-decision finding. One initial adjudicator timed out; all seven initial reviewers, the other six adjudicators and synthesis completed. The three-reviewer replacement completed fully. Independent [initial](https://github.com/the-sarge/quic-go-fast/pull/47#issuecomment-5576819627) and [replacement](https://github.com/the-sarge/quic-go-fast/pull/47#issuecomment-5576928027) dispositions retain rejected aid-strengthening requests. Remaining probe-constructor cleanup is already assigned to [E5 #29](https://github.com/the-sarge/quic-go-fast/issues/29); no duplicate follow-up or added contract is needed.

E5 remains the ready frontier; E6 is now blocked only by E5. The [live program tracker](https://github.com/the-sarge/quic-go-fast/issues/31) and exact OmniFocus task are being reconciled after this journal. Direct probes, retained close, receive storage and physical-link/application qualification remain outside E4’s traced effects.

---

## Complete probe emission and path handoff - 2026-09-07 21:35 EDT

**Main:** `52c5321db04c`
**Actor:** Codex

E5 merged in [PR #49](https://github.com/the-sarge/quic-go-fast/pull/49) as `52c5321db04cdbf1671249878dde9db633866ab6`, closing [#29](https://github.com/the-sarge/quic-go-fast/issues/29). The emission owner now constructs, registers and disposes direct server/client probes, queues MTU probes and owns path handoff through the authoritative active send-connection slot. Connection policy, MTU lifecycle, worker execution and protocol behavior remain with their existing owners. Probe constructor errors release unreturned storage without refunding packet numbers. The product PR committed E5 completion and E6 readiness.

The [bounded performance receipt](audits/e5-probe-emission/README.md) preserves both campaigns. The initial P3 probe-p99 upper ratio 1.070867 failed its 1.05 margin. The single permitted replacement, after binding the active send-connection slot, passed all margins across ten valid alternating pairs: goodput lower ratio 0.997506, CPU/unit upper 1.002985, allocated bytes/unit upper 1.000953 and probe-p99 upper 1.026829, with no failed/missed probes or payload errors. Focused allocations were unchanged; the embedded connection grew by eight bytes. No causal latency claim is made, and the performance budget is exhausted.

Final head `933bbff047feb6d7954bfa6e43d2e592353269d8` passed one uncached full suite, focused ownership/path/feedback races, vet, golangci-lint and gcassert on macOS arm64 Go 1.27.0. Native Linux Go 1.27.1 full tests, focused races, vet and performance qualification cover byte-identical Go/module/fixture sources. All 33 hosted checks passed, including [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34175825987); the [certification receipt](https://github.com/the-sarge/quic-go-fast/pull/49#issuecomment-5577720975) binds the exact head, base and commands. A Windows/non-OOB test-fixture compile failure was diagnosed and corrected before these passing checks.

The initial review, verification and single replacement review completed. Independent dispositions accepted the authoritative-slot correction despite the initial synthesis rejecting that architectural finding; receipt provenance was completed under the cheap documentation correction policy. The [final review receipt](https://github.com/the-sarge/quic-go-fast/pull/49#issuecomment-5577711945) records no remaining fix, decision stop or deferred follow-up.

E6 is now the ready frontier: E4 and E5 are closed. The [live program tracker](https://github.com/the-sarge/quic-go-fast/issues/31) and exact OmniFocus tasks are being reconciled after this journal. Receive storage, retained-close lifetime assigned to E6, path-policy algorithms and physical-link/application performance remain outside E5's traced effects.

---

## E6a handshake lifecycle consumers migrated - 2026-09-07 22:54 EDT

**Main:** `a62dc4eee670`
**Actor:** Codex

### Completed

Merged [PR #55](https://github.com/the-sarge/quic-go-fast/pull/55), closing [E6a #52](https://github.com/the-sarge/quic-go-fast/issues/52). Nine handshake/lifecycle consumers now use real packet construction. Server post-handshake frames are decoded at socket output; client synchronization observes a real Handshake flight and settled lifecycle state. Timeout, 0-RTT limit rejection, buffering/replay, version negotiation and pre-packing MTU fallback observations are preserved. The [preservation mapping](audits/e6a-handshake-consumers/README.md) records each replacement. Production, module and CI files are unchanged.

### Validation

Exact candidate `41f6e54e67780edfe54e7678a602233842433316` passed the full package suite, focused race family, vet, gcassert, golangci-lint and inherited local lint steps. All 33 hosted checks passed. [The review and certification receipt](https://github.com/the-sarge/quic-go-fast/pull/55#issuecomment-5578386517) records RAS run `20260908T024017-2042c2d6a4f8a495c48a2888`, two rejected low findings, zero required fixes or follow-ups, and same-head hosted workflow links. The MTU output regression distinguished 1,452 bytes without feedback from 1,200 with eligible feedback. No performance campaign or untraced runtime effects were introduced.

### Next

The product PR committed E6a completion and the remaining E6b/E6c frontier. Both remain independent and ready; E6 remains blocked by both. [Program tracker #31](https://github.com/the-sarge/quic-go-fast/issues/31) is the live view.

---

## E6b scheduling and batching consumer migration - 2026-09-07 23:54 EDT

**Main:** `eb9b3ed8f562`
**Actor:** Codex

### Summary

Merged [E6b / PR #57](https://github.com/the-sarge/quic-go-fast/pull/57), retiring nine scheduling/batching packer-mock consumers and the caller-buffer observation wrapper. Tests now use real packet construction, decoded output, timer/queue observations and real recovery state. Production Go, module files and CI configuration are unchanged. The [preservation mapping](audits/e6b-scheduling-consumers/README.md) records the retained assertions.

### Validation

The initial RAS review and its allowed replacement completed with no required fixes or follow-ups. Final exact-head certification at `c50f700dc16b84cd0686d9ae1aa7d5c5474c02c5` passed the full Go suite, the prescribed focused race family, vet, gcassert and lint. All 33 inherited hosted checks succeeded. CI exposed two fixture defects—worker-start synchronization and call-count-based capacity assumptions—which were reproduced locally and corrected before the replacement review. The [validation receipt](https://github.com/the-sarge/quic-go-fast/pull/57#issuecomment-5578902773) links the review dispositions and hosted runs.

### Next

[Child #53](https://github.com/the-sarge/quic-go-fast/issues/53) is closed. E6c remains the sole ready frontier; final E6 remains blocked by E6c. The product PR owns this committed transition. See the [program tracker](https://github.com/the-sarge/quic-go-fast/issues/31) for the live state. No new review follow-up or untraced runtime effect was admitted.

---

## E6c path consumers and version-negotiation correction landed - 2026-09-08 00:48 EDT

**Main:** `4e99c6725483`
**Actor:** Codex

### Completed

Merged [E6c PR #59](https://github.com/the-sarge/quic-go-fast/pull/59) as `4e99c672548303aaaca496e8e86977fe5cb13db5`, closing [child #54](https://github.com/the-sarge/quic-go-fast/issues/54). The three assigned connection-ID/path tests now use real packet construction and decoded output; the [preservation mapping](audits/e6c-path-consumers/README.md) records the retained Retry, destination, path-validation, migration and storage/handoff evidence. Only the two declared close/error consumers remain for E6. The product PR owns the committed completion/frontier transition.

The inherited hosted unit suite exposed an ordering-sensitive version-negotiation defect on the unchanged base. [Prerequisite PR #60](https://github.com/the-sarge/quic-go-fast/pull/60), merged as `d7d2b295f50b6c7ff78a47c2c78c57136c541846`, makes the existing close handler suppress CONNECTION_CLOSE during version recreation even after the first Initial. Deterministic before/after-Initial regressions preserve the typed error, selected version, connection-ID removal and qlog observations. E6c then resumed on the corrected base with its test-only diff intact.

### Validation

Both PRs completed RAS review with no findings or follow-ups, exact-head local certification, all 33 applicable hosted checks and head-guarded squash merge. The [correction receipt](https://github.com/the-sarge/quic-go-fast/pull/60#issuecomment-5579257820) and [E6c receipt](https://github.com/the-sarge/quic-go-fast/pull/59#issuecomment-5579396957) record exact heads and evidence. E6c passed the full suite, its prescribed focused race family, vet, lint, gcassert, whitespace and production/module/CI byte-identity checks against the corrected base. No performance campaign or new verification infrastructure was introduced. The first E6c review was interrupted before synthesis at the CI failure; the resumed initial review completed cleanly.

### Next

[E6 #30](https://github.com/the-sarge/quic-go-fast/issues/30) is the sole ready successor; E4, E5 and all three test prefactors are complete. The [program tracker #31](https://github.com/the-sarge/quic-go-fast/issues/31) is the live frontier view. E6 retains close ownership, final interface contraction and its existing qualification obligations. No review follow-ups or untraced runtime effects remain from E6c.

---

## E6 packet-emission ownership completed - 2026-09-08 02:40 EDT

**Main:** `86bba30bb7b6`
**Actor:** Codex

### Summary

Completed E6 in [PR #62](https://github.com/the-sarge/quic-go-fast/pull/62), merged as `86bba30bb7b6b2ccb96566ce3aafa799fbe53037`. Close emission now retains an immutable owned payload and releases temporary construction storage after the synchronous write and on constructor failures. Packet emission owns the concrete packer, queue lifecycle/capacity and packet-shape dispatch; the legacy interface, mock and connection forwarding are removed.

### Validation

The initial review identified a remaining generator-only alias; it was removed and exact-head RAS verification passed. The final replacement review found no issues or follow-ups. Uncached full tests, focused ownership/close/feedback races, vet, lint, gcassert, tagged builds and generation checks passed on reviewed head `78420a048e5edfa19c5796dbec6923ffb9923bf1`, together with all 33 applicable hosted checks. The [PR certification receipt](https://github.com/the-sarge/quic-go-fast/pull/62#issuecomment-5580461735) links review and hosted evidence.

### Decisions

The owner directed completion after unsuccessful final performance qualification. The [normative E6 decision](adr/2026-09-07-packet-emission-plan.md#e6--own-close-emission-and-retire-the-legacy-seam) accepts the recorded uncertainty without changing margins or authorizing another campaign. P1 probe-p99 upper ratio remains 1.2358177429874126 against the 1.05 bound; P3/P4/P5/P6 remain unqualified. The [E6 receipt](audits/e6-close-emission/README.md) preserves both campaigns and all failed evidence; no cause or performance pass is claimed.

### Next

All nine packet-emission slices are complete in the merged plan and program index; #30 is closed. Finish this journal and reconcile the parent/OmniFocus completion state. The [program tracker #31](https://github.com/the-sarge/quic-go-fast/issues/31) remains the live tracking view; no successor slice is newly ready.

---

## Architecture improvement handoff published - 2026-09-08 13:06 EDT

**Main:** `f43f8d65a43f`
**Actor:** Codex

### Completed

Merged the audited architecture handoff in [PR #85](https://github.com/the-sarge/quic-go-fast/pull/85), publishing four normative track plans, the incoming packet-lifetime ADR, domain terms, and frozen supporting evidence. The [program index](adr/2026-09-08-architecture-deepening-program.md) retains HTTP/3 exchange lifetime, incoming packet-buffer lifetime, module payload reduction, and maintained emission entrypoint tests as twelve bounded implementation slices. No production implementation was performed or dispatched.

Published four GitHub parent issues and twelve child issues, reusing #68 for H1, with exact merged plan commit references and four native blocking edges. [Program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) owns live progress and the current frontier.

### Decisions

The [HTTP/3 plan](adr/2026-09-08-http3-lifetime-plan.md) separates per-attempt usage from unread request-input ownership during supported retries and retains usage through active response/upload lifetime. The [incoming lifetime plan](adr/2026-09-08-incoming-lifetime-plan.md) gives non-QUIC queue publication one initialization owner and includes a bounded abort for failed unpublished Initial construction. Broader shutdown-lock restructuring, path/MTU coordination, emission construction, and readiness consolidation remain deferred as recorded in the program index.

### Validation

Three independent pre-commit slice audits passed. The bounded two-agent RAS review identified two distinct contract gaps; both were independently accepted, corrected, and passed scoped pre-commit re-audits. The shared cheap docs-only correction policy allowed skipping another RAS cycle. Final local certification covered documentation scope, tracked links, slice headings and graph, Markdown/JSON structure, whitespace, and clean worktree. All 33 inherited hosted checks passed on the exact reviewed-and-corrected candidate before guarded squash merge; the merged tree matched that candidate and the accepted plan commit was verified reachable on remote main.

### Next

At this entry, H1, H2, I1, I3, I5, I6, I7, P1, and T1 form the nine-slice frontier; I2, I4, and I8 await their named predecessors. Prefer H1 first; packaging can proceed independently. Implementation remains undispatched. Use [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) as the live view and the [OmniFocus program](omnifocus:///task/jTmzcx-2f8I) as its task-manager mirror.

---

## 32-bit close-reason parser hardening landed - 2026-09-08 13:45 EDT

**Main:** `24fb7b565d9d`
**Actor:** Codex

[PR #104](https://github.com/the-sarge/quic-go-fast/pull/104) closed [issue #64](https://github.com/the-sarge/quic-go-fast/issues/64). CONNECTION_CLOSE parsing now compares the decoded reason length with remaining bytes before narrowing, rejecting malformed oversized lengths with `io.EOF` on 32-bit targets. Valid application and transport close decoding, empty/nonempty reasons and consumed-length behavior remain preserved.

A Linux/386 executable under emulation reproduced the baseline allocation panic and passed after the fix, including `1<<31`, `1<<32` and maximum QUIC varint rejection for both close types. Reviewed head `7ac589bf9ad5634034f80fd7698afff63041b534` passed the full native suite, focused close race tests, vet, lint, gcassert and inherited local checks. Both RAS reviewers completed without findings or follow-ups, and all 33 hosted checks passed before head-guarded squash merge. The [local certification receipt](https://github.com/the-sarge/quic-go-fast/pull/104#issuecomment-5589320256) retains an initial lint/generation overlap and an unrelated STREAM allocation assertion from an overbroad race invocation; neither is relabeled as passing. The [hosted receipt](https://github.com/the-sarge/quic-go-fast/pull/104#issuecomment-5589390828) records the exact-head checks. No public API, module identity, packet-emission ownership or CI behavior changed.

---

## Emission construction handoff published - 2026-09-08 14:00 EDT

**Main:** `0d9ef762117d`
**Actor:** Codex

### Summary

Published the scoped packet-emission construction track C in the existing QGF-AD-2026-09 program through [docs PR #105](https://github.com/the-sarge/quic-go-fast/pull/105), merged at `0d9ef762117d12cc704d820bd7a5b0b8f34d8a0d`. The [plan](adr/2026-09-08-emission-construction-plan.md) adds constructor-owned regression coverage before replacing staged initialization with one complete assignment. Recovery/path bindings and packet-time behavior remain unchanged; no initialization defect or performance gain is claimed.

### Completed

The durable [investigation and audit receipt](audits/2026-09-08-emission-construction/README.md) preserve evidence and scope decisions. [Track parent #107](https://github.com/the-sarge/quic-go-fast/issues/107) contains [C1 #108](https://github.com/the-sarge/quic-go-fast/issues/108) and [C2 #109](https://github.com/the-sarge/quic-go-fast/issues/109), with a native C1 blocker on C2. Both child issues pin the verified reachable default-branch plan commit. [Program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) was updated without recreating the other tracks. OmniFocus parent `fsNvXyZ3aDd` contains sequential track `nQ6ddTP0zNg`, C1 `l7l2JEzj8AW`, and C2 `d-KfNeYG5KL`.

### Validation

The independent pre-commit slice audit passed. RAS review `20260908T173827-1f8fe92822c77e314724ec3e` completed on docs head `a59dfdae510d420bed0d7da65d1f796fdec1ddcd`; both reviewers approved with no findings and no fix verification required. Exact-head local documentation checks and all 33 applicable hosted checks passed before guarded squash merge. Production Go/module/workflow files were unchanged by the handoff. Preserved baseline tests are historical observations, not certification of future implementation.

### Next

At this entry, C1 is the scoped frontier and C2 is blocked by C1; T1 is independent. [Tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) owns live progress. Dispatch is the operator's decision through `$implement-architecture-slice` in a fresh context. No implementation or performance campaign was launched, and the other tracks and completed Track E retain their existing contracts.

---

## Connection pacing fixture diagnosis completed - 2026-09-08 15:26 EDT

**Main:** `a5c7ec43b7e3`
**Actor:** Codex

[PR #116](https://github.com/the-sarge/quic-go-fast/pull/116) closed [issue #103](https://github.com/the-sarge/quic-go-fast/issues/103). The missing 50 ms packet-pacing interval came from the connection test's call-ordered recovery allowance: DATAGRAM admission and the explicit scheduling call could produce separate wakeups at the same controlled time, consuming the next `SendAny` too early. The fixture now uses sent count and fixed deadlines, preserves the exact two-immediate/third-after-50-ms guarantee and final pacing wakeup, and covers the competing wakeup order. Production pacing, APIs, and architecture contracts are unchanged.

The separate teardown defect is corrected by deferring destruction inside the synctest bubble, asserting send counts on the test goroutine, and collecting completion without blocking after destruction. The old-allowance regression now fails cleanly in both forced-wakeup scenarios; controlled early exit also drains the connection with a live timer and queued data. The [diagnostic record](audits/pacing-ci-103/README.md) retains the baseline source, Linux environment differences, bounded failure counts, deterministic 5/5 reproduction, and red/green evidence.

Reviewed head `4afc62b13a46300931f24633ee9c886f31e269cc` passed the full local suite, vet, module-tidy check, lint, focused race checks, 10,000 pacing repetitions on both Linux/amd64 and macOS/arm64, and all 33 hosted checks. Independent Standards and Spec reviews were clean. Initial RAS review `20260908T185254-2f998620c5bce16804ca91af` led to the bounded teardown and coverage corrections; exact-head verification resolved those findings. Replacement review `20260908T191846-5664579bbc888d7588944405` found no issues or follow-ups. The [final local receipt](https://github.com/the-sarge/quic-go-fast/pull/116#issuecomment-5590423887), [finding dispositions](https://github.com/the-sarge/quic-go-fast/pull/116#issuecomment-5590404063), and [merge receipt](https://github.com/the-sarge/quic-go-fast/pull/116#issuecomment-5590608427) preserve certification and review history. The prior journal CI failures remain historical failures; this correction was separately certified and merged.

---

## Unit race instrumentation enabled - 2026-09-08 16:03 EDT

**Main:** `45fffa2e0d38`
**Actor:** Codex

### Summary

Merged [PR #118](https://github.com/the-sarge/quic-go-fast/pull/118), closing [issue #71](https://github.com/the-sarge/quic-go-fast/issues/71). The existing Ubuntu unit race step now passes `-race`, retaining `TIMESCALE_FACTOR=20`, verbosity, shuffle, package scope, and the workflow matrix.

### Decisions

The first instrumented run exposed a zero-allocation assertion incompatible with race-mode `sync.Pool` drops. Relocated the unchanged parser allocation test and helper into a `!race` file; normal unit CI retains both STREAM and ACK allocation assertions, and functional parser tests remain race-enabled. The [PR review record](https://github.com/the-sarge/quic-go-fast/pull/118) records the rationale and rejection of an added requirement to measure ACK allocations under race instrumentation.

### Validation

Certified candidate `3d1c6e4717c7f816e831bf57e77fe1d2f8c73cb7`: full 31-package local unit scope passed with `TIMESCALE_FACTOR=20` and race instrumentation; normal parser allocation tests and the wire race suite passed. Workflow validation and whitespace checks passed; default actionlint's unrelated SC2086 warning was unchanged from the base. Standards and Spec reviews reported zero findings. Initial RAS run `20260908T193951-214de8c5fd4e189830fae73f` identified one allocation-test defect, with duplicate reports; verification resolved all four reports on the candidate. Five initial reviewers completed and two provider-output failures were retained in the audit record. Replacement RAS run `20260908T195750-06148e85f32028558d62dfe2` completed with no findings or follow-ups.

All existing pull-request workflows passed on the candidate: unit, integration, lint, cross-compilation, and interop Docker. Both Ubuntu Go 1.26 and 1.27 jobs completed the race, FIPS140, and benchmark steps successfully. Squash merge `45fffa2e0d38a431480f18096003c01a2d43e460` was guarded by the exact reviewed head.

---

## Self-suite QUIC version selection corrected - 2026-09-08 18:40 EDT

**Main:** `6a3e66fa00e4`
**Actor:** Codex

Merged [PR #120](https://github.com/the-sarge/quic-go-fast/pull/120), closing [issue #69](https://github.com/the-sarge/quic-go-fast/issues/69). The self-suite now applies its selected QUIC version through `getQuicConfig`, preserving explicit version lists. Seven HTTP/3 dialing fixtures now use that helper, and representative handshakes assert the actual negotiated version on both peers. Existing suite entry points, qlog behavior, the separate version-negotiation suite, public API, and module identity remain intact.

The initial regression reproduced `-version=2` logging v2 while negotiating v1. Applying the helper default then exposed HTTP clients still using v1; the seven fixture assignments resolved that in-scope regression. Certified head `dfbd3e43821b547aef489f9f2ffadfae1d6a3d19` passed full repository tests, the full shuffled v2 self-suite, package vet, and whitespace checks. Focused handshake and affected HTTP tests passed for both flags; explicit overrides and qlog were also exercised. All 33 hosted checks passed with none skipped, including unit, integration, lint, cross-compilation, and interop image workflows.

Standards and Spec reviews found no actionable issues. Initial RAS review `20260908T221543-60b137673c7efed2aeda5895` identified the same HTTP client mismatch; exact-head verification resolved it and its duplicate reports. An added HTTP assertion requirement was rejected as beyond the accepted representative coverage. Replacement review `20260908T223512-6e14684b70f6cd6e0fb59e70` returned no fixes or follow-ups. The [PR review record](https://github.com/the-sarge/quic-go-fast/pull/120) preserves scope and dispositions. Merge `6a3e66fa00e400f7256bbd2bf574d4bdcbebf636` was guarded by the exact reviewed head.

---

## Active send-connection publication fixed - 2026-09-08 20:16 EDT

**Main:** `79dd6834d826`
**Actor:** Codex

### Completed

Merged [PR #125](https://github.com/the-sarge/quic-go-fast/pull/125), closing [issue #123](https://github.com/the-sarge/quic-go-fast/issues/123). Packet emission now synchronizes active send-connection publication with public state and address inspection. The replacement object is safely initialized, GSO fallback remains live, and queue drain, worker lifecycle, protocol ownership, public API, and wire behavior are preserved. The ownership audit and real UDP migration regressions landed with the fix.

### Validation

Both concurrent migration regressions reproduced the unfixed baseline race. At reviewed head `f8810a3a4ff1bf4e27ee5a8018a82b279dee6dae`, three independent fixed Linux/arm64 Go 1.27.1 race runs passed, as did focused migration/rebinding/queue/GSO race checks, the full macOS/arm64 Go 1.27.0 suite, vet, and all 33 hosted checks. The baseline production SHA was `5e9ae2ab3e46067335ea409a689b638a82877bd3`. See the [exact-head certification receipt](https://github.com/the-sarge/quic-go-fast/pull/125#issuecomment-5593756181).

### Decisions

Initial RAS review `20260908T234415-517558168d6ac9d54a1f563d` identified a vet failure in the address regression, corrected with explicit result discards and independently verified. Speculative contention and constructor-rebinding comments did not establish required changes; the verifier's additional 100-run macOS stress campaign passed. Final replacement review `20260909T001011-ae36863bcb6c624f9b094857` returned zero findings. No deferred follow-up remains. [Review dispositions](https://github.com/the-sarge/quic-go-fast/pull/125#issuecomment-5593604592) and [verification dispositions](https://github.com/the-sarge/quic-go-fast/pull/125#issuecomment-5593708546) preserve the evidence and scope decisions.

---

## Address helper socket cleanup merged - 2026-09-08 23:55 EDT

**Main:** `c79b5a386bdd`
**Actor:** Codex

### Completed

Merged [PR #127](https://github.com/the-sarge/quic-go-fast/pull/127), closing [issue #66](https://github.com/the-sarge/quic-go-fast/issues/66). DialAddr, DialAddrEarly, ListenAddr, and ListenAddrEarly now promptly close internally allocated UDP sockets on setup failure. Successful setup, caller-owned PacketConn lifetimes, public APIs, module identity, and packet-emission ownership remain intact.

### Validation

Retained real-socket regressions failed before the fix for destination resolution and TLS/configuration errors and passed afterward; listener failures also permit immediate port reuse. Caller-owned sockets still deliver packets after configuration errors, and normal/early address-helper handshakes succeed. Full `go test ./...`, focused race tests, `go vet ./...`, and golangci-lint passed on reviewed head `aac9f97271b580b8f07531dd3816879424322610`. All 33 reported hosted checks passed, including unit, integration, lint, cross-compilation, and interop builds. Independent Standards and Spec reviews each reported zero findings.

RAS run `20260909T033733-23022bfd71cf61ee19029a29` completed with no required fixes or follow-ups. Its sole observation requested stronger read-deadline restoration testing in unchanged transport teardown; the [PR disposition](https://github.com/the-sarge/quic-go-fast/pull/127) records why that verification-aid strengthening is outside this bounded ownership fix. No fix verification or replacement review was required. Product merge: `c79b5a386bdd504b978d57871d075d9967c8f7eb`.

---

## Extended CONNECT SETTINGS cancellation fixed - 2026-09-09 00:11 EDT

**Main:** `32446154e729`
**Actor:** Codex

Merged [PR #129](https://github.com/the-sarge/quic-go-fast/pull/129), closing [issue #67](https://github.com/the-sarge/quic-go-fast/issues/67). Extended CONNECT now observes request cancellation while waiting for peer SETTINGS. The live-peer regression withholds SETTINGS, verifies bounded cancellation while both peers remain connected, and completes a subsequent Extended CONNECT on the same connection. Public APIs, module identity, ordinary negotiation, and packet-emission ownership remain intact.

The regression failed before the fix and passed afterward. Certified head `e5573685b9b6dfe5e502279b9f5bcbe4c0974a4d` passed the full local `go test ./...` suite, focused HTTP/3 cancellation/CONNECT race tests, vet, golangci-lint, and whitespace checks on Go 1.27.0 macOS/arm64. All 33 reported hosted checks passed with none skipped, covering unit, integration, lint, cross-compilation, and interop image workflows.

Independent Standards and Spec reviews each found zero issues. RAS initial review `20260909T040120-b5ba2759af3045385bfdc11c` completed with all seven reviewers successful and no findings or follow-ups; no fix verification or replacement review was needed. The [PR validation record](https://github.com/the-sarge/quic-go-fast/pull/129) preserves the bounded contract and review evidence. Squash merge `32446154e7290a2d1d1007ab879a95413427432e` was guarded by the exact reviewed head. The linked OmniFocus task is complete.

---

## Packet-loss failure diagnostics merged - 2026-09-09 03:43 EDT

**Main:** `61278443eb3e`
**Actor:** Codex

Merged [PR #132](https://github.com/the-sarge/quic-go-fast/pull/132) at `61278443eb3e88d834777c800b182ffa8910e266`. The packet-loss fixture now emits its exact scenario, latest helper and connection-close states, and a bounded tail of router decisions and qlog events on test failure. Normal passing cases emit no diagnostic dump. Timeout, payload, loss decisions, existing subtest names, production transport behavior, and timeout-helper ownership are preserved.

The certified head `956823576885c769cab32a777f66e83a2871b93b` passed deterministic collector/callback/cleanup regressions, a focused race check, one full uncached local suite, vet, go-fix and module-tidy checks, golangci-lint, and all 33 inherited hosted checks. Standards and Spec reviews had no findings. RAS run `20260909T071827-6a28f99d53caeab7df5edc5f` completed all seven initial reviews and adjudications. Three low recommendations for future writer encapsulation, more close-state tests, and explicit unset labels were independently deferred as contract strengthening; the asserted current buffer panic was rejected because the native encoder uses bounded Write calls. No fix/verify/replacement cycle was needed under the scoped low/nit policy. The [PR record](https://github.com/the-sarge/quic-go-fast/pull/132) preserves those dispositions.

The [accepted diagnostic scope](agents/packet-loss-diagnostics.md) excludes complete histories, deterministic replay, process-termination capture, and scheduler-neutral observation. Client transport-level drops before connection dispatch are not traced; the retained tail can include teardown. Those limitations were revalidated against the merged code. [Issue #44](https://github.com/the-sarge/quic-go-fast/issues/44) remains the live investigation: the preceding 200-run campaign captured no failure, no timeout cause is established, and its original OmniFocus task remains open. Direction correction remains separate in [issue #79](https://github.com/the-sarge/quic-go-fast/issues/79).

---

## Timeout test worker ownership landed - 2026-09-09 04:39 EDT

**Main:** `7607293b93da`
**Actor:** Codex

### Completed

Merged [PR #134](https://github.com/the-sarge/quic-go-fast/pull/134), closing [issue #70](https://github.com/the-sarge/quic-go-fast/issues/70). Timeout wrappers retain caller-buffer ownership through synchronous I/O and joined interruption. Cancellation workers report errors during bounded joining and finish pre-canceled accepts before stream production. Deadline fixtures deliberately block ten operations and join asynchronous setters before checking recovery.

### Validation

Full Go tests, focused race tests, vet, lint, and Go modernization checks passed on `b52da1761eb6603be989cd7a4fa0b1e7f7844aed`; all 33 hosted checks were green before the exact-head squash merge. Original timeout helpers fail the buffer-reuse regressions with races. Controlled connection-error and producer-gate-bypass checks exercised the repaired fixture error paths without a randomized campaign. Initial RAS review `20260909T080552-1ab6192f9849f3b6fa754df0` produced two accepted findings, both verified resolved; replacement review `20260909T083140-f50240c4d4cceb216484ba03` was clean. No deferred findings remain.

---

## HTTP/3 exchange lifetime landed - 2026-09-09 10:37 EDT

**Main:** `28922b8a0025`
**Actor:** Codex

Merged [PR #136](https://github.com/the-sarge/quic-go-fast/pull/136) as `28922b8a002591dec05024d067d3844afe8d8590`, closing [H1 / issue #68](https://github.com/the-sarge/quic-go-fast/issues/68). Each HTTP/3 request attempt now has one lifetime owner that retains pooled usage through both outer response consumption and asynchronous upload cleanup. Cancellation and connection teardown close input once; stream-opening retries preserve untouched input. Direct ClientConn requests use the same lifecycle without pooled accounting. The product PR also records H1 completion and the resulting program frontier.

The two required baseline regressions failed before implementation. The accepted 14-cell evidence includes real multiplexed responses, early-response/upload rendezvous, compressed-source EOF before decoded completion, cancellation, connection teardown, terminal read errors, and close-sensitive retry success/failure. Exact candidate `6e2acee9f8f678ed810a639b543964a6ff5e01bd` passed HTTP/3 normal/race suites, ordinary non-integration package tests, vet, scoped lint, modernization and whitespace checks. All 33 inherited hosted checks passed, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34362641316) and [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34362641406) workflows. The [certification receipt](https://github.com/the-sarge/quic-go-fast/pull/136#issuecomment-5603639567) records the remaining hosted runs and exact base.

Initial RAS review `20260909T140950-8e756c65b4e2419e390bafcf` completed seven reviews and adjudication. Independent [dispositions](https://github.com/the-sarge/quic-go-fast/pull/136#issuecomment-5603553176) rejected unreachable runtime claims and non-required cleanup; dormant raw reqDone plumbing has no pooled-release authority. Replacement review `20260909T142941-c39df2e73d432e288b51903b` was clean on the final candidate. No accepted product finding required fix verification, and no worthwhile deferred findings or required untraced effects remain.

H2 remains independently ready; H1's merge does not newly unblock another slice. The [program tracker](https://github.com/the-sarge/quic-go-fast/issues/101) is the live frontier view, and the [H plan](adr/2026-09-08-http3-lifetime-plan.md) retains the accepted boundaries. Cache-identity eviction, shared-dial policy, public API and wire behavior remain under their existing contracts.

---

## Conditional HTTP/3 eviction landed - 2026-09-09 11:28 EDT

**Main:** `9018906dc1e0`
**Actor:** Codex

### Summary

Merged [PR #138](https://github.com/the-sarge/quic-go-fast/pull/138) as `9018906dc1e03782426818b96444d63e15019384`, completing [H2 / #90](https://github.com/the-sarge/quic-go-fast/issues/90). Delayed dial and request failures now remove a hostname's cached HTTP/3 entry only when it still matches the failed attempt's pointer. Replacement connections remain available to cached requests and eligible retries.

### Completed

Added deterministic current/missing/replacement and delayed-failure regressions, with terminal and retryable request cases. The product PR records H2 completion, its five evidence classes, and the resulting program frontier. Both H slices are complete; H2 unlocks no successor. Retry eligibility, cancellation, shared dial context, idle/explicit close authority, public API and wire behavior are preserved.

### Validation

Observed stale-entry and delayed-failure regressions fail before the guard/caller fix and pass afterward. Final local certification at `7fbfd1b576b98498b7a1dd090d636554f3916199` against base `89f4491a55080e56d1067aef1b66dd4743c3950a` passed HTTP/3 tests and race tests, all remaining package tests, vet, golangci-lint, HTTP/3 go-fix inspection, documentation checks and `git diff --check` with a clean worktree.

RAS review `20260909T145737-87e99fdb0de429153c25a71c` completed with eight successful reviewers; two Cursor reports failed parsing and did not count as clean reviews. No runtime defect was found. The accepted documentation count correction distinguishes fourteen total slices from twelve remaining; the optional helper comment was rejected. [Finding dispositions](https://github.com/the-sarge/quic-go-fast/pull/138#issuecomment-5604262335) record why the shared docs-only policy skipped another RAS cycle. No deferred findings or untraced effects remain.

All 33 hosted checks passed on the exact certified head before guarded squash merge: [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34369225954), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34369225830), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34369225947), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34369225774) and [interop](https://github.com/the-sarge/quic-go-fast/actions/runs/34369225943). No same-head CI rerun or additional stress campaign was needed.

### Next

The remaining frontier is I1, I3, I5, I6, I7, P1, T1 and C1; [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) is the live view. Existing blocked slices retain their accepted edges. No routine frontier PR is needed because the product PR owns the committed transition.

---

## HTTP/3 fixture completion synchronized - 2026-09-09 12:51 EDT

**Main:** `229011cae6fa`
**Actor:** Codex

### Summary

Merged [H3 / PR #142](https://github.com/the-sarge/quic-go-fast/pull/142) as `eeabc39b534080385182d2a5e85fff2215cc8173`, closing [#141](https://github.com/the-sarge/quic-go-fast/issues/141). Six HTTP/3 fixture observations now join their own attempt’s existing completion barrier before asserting released pooled counts. The controlled empty-upload case preserves count one and open input after response EOF, then verifies count zero and one input close after upload completion. Production behavior is unchanged. The product PR owns H3’s committed completion/frontier transition.

### Validation

A temporary premature-zero assertion failed deterministically with count one; the retained characterization passed before adding the six joins. The five focused fixture families passed. Final HTTP/3 normal/race tests, vet, module tidiness, document links and whitespace checks passed on `01d16cb4959a72b8268752ae4983b5a3201ded58` against base `45954f54fcca3a6906ea3a0c3b0a11f2b2aad56c`. All 33 applicable hosted checks passed on that head before guarded squash merge; [local certification](https://github.com/the-sarge/quic-go-fast/pull/142#issuecomment-5605275980) and [hosted receipt](https://github.com/the-sarge/quic-go-fast/pull/142#issuecomment-5605389213) retain the evidence.

RAS review `20260909T162120-742f220e0bcc502559853c72` completed with nine successful R1 reviewers; cursor-glm failed structured output and did not count as a successful review. Its one accepted documentation finding corrected the stale current-shape sentence; the second cluster duplicated that observation. The shared docs-only policy skipped a RAS rerun after the correction, with new exact-head local and hosted certification. No deferred review findings or required untraced effects remain. [Dispositions](https://github.com/the-sarge/quic-go-fast/pull/142#issuecomment-5605266481) record the boundary.

### Decisions

The full local suite observed the separately tracked randomized-corruption Dial timeout in [#46](https://github.com/the-sarge/quic-go-fast/issues/46#issuecomment-5605125412), at the same test and assertion location; an identical underlying cause was not established. All other packages passed. The operator explicitly accepted keeping that failure separate from H3 and proceeding with remaining checks. The failed local check was disclosed, not rerun until green or reported as passing; no deadline or corruption behavior changed, and no new architecture handoff was required for the existing investigation.

Reconciled and merged the retained [H2 journal / PR #139](https://github.com/the-sarge/quic-go-fast/pull/139) as `229011cae6fa6d2e62f9d15bdcbc89c691865fc6` after H3. Its prior journal bytes were preserved exactly, its main diff remained one EOF entry, and all 33 new-head hosted checks passed.

### Next

All H implementations are complete. The remaining implementation frontier is I1, I3, I5, I6, I7, P1, T1 and C1; [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) owns live state. No new implementation successor is unlocked and no separate frontier PR is needed.

---

## Captured packet-loss diagnosis and CI fixtures landed - 2026-09-09 19:10 EDT

**Main:** `4084ae7b4f96`
**Actor:** Codex

### Completed

[PR #145](https://github.com/the-sarge/quic-go-fast/pull/145) merged the diagnosis of the fresh PR #144 packet-loss capture, deterministic mandatory loss cases, the opt-in historical random stress path, missing-range/ACK-blackout regression and recovery controls, and the separately approved MTU fixture snapshot repair. The reconstructed capture identified a missing 1253-byte stream range and absent application ACKs, with recovery backoff extending beyond the existing read deadline. No transport defect was established by that capture.

### Decisions

Use the explicit deterministic loss corpus in mandatory CI while preserving the historical random callback behind `QUIC_GO_TEST_RANDOM_LOSS=1`; retain production recovery, payloads and deadlines. The [accepted packet-loss contract](agents/packet-loss-ci.md) records the operator's approval and the bounded model's guarantees. The [MTU diagnostic addendum](audits/mtu-snapshot-race.md) records the separate approval and controlled ACK-publication experiment; closing the connection before final measurements makes the MTU event and DATAGRAM estimate consistent without relaxing the test tolerance.

### Validation

The merged PR head `c0cf3ba9084daa11ce8e28bdeb047f6e9a9bb702` passed the uncached full module suite, QUIC v2 self suite, focused packet-loss/MTU race tests, vet, lint, module tidy including FIPS, generator consistency, go-fix diff and gcassert. All 33 applicable hosted checks passed on that head. Standards and Spec reviews found no findings. The packet-loss RAS finding about receive-trace completion was fixed and verified before its clean replacement review; the final integrated review `20260909T225952-9893ed0c3f0715ed03d8233d` completed with both reviewers and synthesis reporting no findings. No same-head CI rerun or new random campaign was requested, and passing duplicate runs were not used as diagnosis.

### Next

The original historical server-first timeout remains unexplained; [issue #44 is the live investigation](https://github.com/the-sarge/quic-go-fast/issues/44). Its OmniFocus task stays open. [PR #144](https://github.com/the-sarge/quic-go-fast/pull/144) and the #46 workflow remain independently owned and were not modified, rerun or merged by this work.

---

## Corruption recovery tests and failure diagnostics landed - 2026-09-09 19:54 EDT

**Main:** `b0cdb08bb7a4`
**Actor:** Codex

### Completed

Merged [PR #144](https://github.com/the-sarge/quic-go-fast/pull/144) as `b0cdb08bb7a498d0cfe835709ae0dd8b856bb4e7`. The original randomized corruption fixture now emits bounded failure diagnostics with connection/endpoint events, proxy mutation decisions and write results, plus the latest Dial/Accept/transfer phases. Five permanent simulated-network cases cover bounded Initial/Handshake corruption recovery, undamaged controls and cancellation under continuous corruption. Both prior investigations and their raw evidence are published with the tests.

The maintainer directed “finish 144” after PR #145 addressed the captured packet-loss failure that had stopped publication. A new dedicated worktree integrated main without changing the original PR's Go files or immutable evidence. The original #46 worktree and its local branch were untouched. The [publication contract](audits/issue-46-recovery/MERGE-CONTRACT.md) records this resumption, preservation of corruption decisions/deadlines and the bounded evidence plan.

### Validation

Reviewed and merged head `2a8513cfead75b6018235feb6248f676c40bdafd` against main `6786a562a07175f612ac3b6f7a876e62d3b41f86`. The uncached native full suite, focused corruption/diagnostic race tests, vet, lint, changed-Go formatting, root/FIPS module tidiness, go-fix diff, generator consistency and gcassert passed. RAS review `20260909T234404-647230c63d31c6e15fa61bba` completed with both reviewers and synthesis reporting zero findings; no fix verification or replacement round was needed. All 33 applicable hosted checks passed on that exact head before guarded squash merge. No same-head CI rerun, new random campaign or production change was performed.

### Next

[Issue #46](https://github.com/the-sarge/quic-go-fast/issues/46) remains the live investigation into the historical randomized-corruption Dial timeout; its tests and diagnostics are now delivered. An actual failing corruption capture is still needed to establish that failure's cause. [Issue #44](https://github.com/the-sarge/quic-go-fast/issues/44) retains its separate unresolved historical question, which did not prevent this publication. The investigation tasks stay open; no additional diagnostic campaign is started by this journal entry.

---

## Stronger corruption capture published - 2026-09-09 23:45 EDT

**Main:** `fcbaa187f105`
**Actor:** Codex

### Completed

[PR #148](https://github.com/the-sarge/quic-go-fast/pull/148) merged stronger corruption-handshake evidence: append-only JSONL through Dial return, original datagrams and packet boundaries, qlog, selected mutations, and actual endpoint/proxy socket results including receive-batch identity. Failure and interrupted-process evidence survives ordinary cleanup loss; successful fixtures remove captures, and CI uploads retained files independently of debug mode. The existing corruption choices, deadlines and UDP/OOB capabilities remain unchanged. The capture has explicit storage limits and a record-by-record finalization cutoff; it does not promise complete causation, deterministic replay or a historical timeout fix.

### Validation

Initial RAS review `20260910T030709-f2e1b2306d111dc77967b61c` found missing receive-batch identity/result and a truncation-marker size error. Both were fixed manually and verified resolved (2/2, clear blocking projection) at `0a70aba55268c83766ca6005cd6c77fc583a75b1`. Final two-reviewer review `20260910T033219-717f424aed57a1f57f3db455` completed; its atomic-batch suggestion was independently deferred under the documented recording boundary and finite review budget. Exact-head focused race tests, full proxy race tests, uncached native full suite, vet, lint, go-fix, formatting, root/FIPS module tidy, workflow syntax and diff checks passed. Corrected Linux batch race evidence and Windows build evidence are in the [audit receipts](audits/issue-46-strengthening/README.md). All 33 applicable hosted checks passed before squash merge `fcbaa187f10591544df90bbf68a9bb55a5281236`.

### Decisions

The [capture contract](agents/corruption-capture.md) bounds interpretation to records appended before finalization, with explicit truncation/overflow/error limits and accepted scheduling effects. A batch result can precede finalization while indexed member records fall after the cutoff. Operation-group atomicity is a worthwhile strengthening beyond this boundary, revalidated against the merged commit and tracked in [#149](https://github.com/the-sarge/quic-go-fast/issues/149).

### Next

The historical randomized-corruption timeout remains unresolved; [#46](https://github.com/the-sarge/quic-go-fast/issues/46) is the live investigation view and stays open. The previously approved 100-attempt capture window passed without reproducing it; publication did not authorize another random investigation campaign. Retrieve the strengthened capture and complete job log from a future failure, respecting the recorded completeness limits. The separate packet-loss investigation [#44](https://github.com/the-sarge/quic-go-fast/issues/44) is unchanged.

---

## I1 receive lifetime and CI fixture repairs merged - 2026-09-10 01:10 EDT

**Main:** `21d70f758e03`
**Actor:** Codex

### Completed

Merged [I1, PR #152](https://github.com/the-sarge/quic-go-fast/pull/152) as `21d70f758e0346b4c57e3e1fb7d207e5e5bbd2a3`, closing #91. Connection processing now retains an active parsing reference through every exit, grants each dispatched view its own reference, and reports rejected undecryptable admission truthfully. The product PR includes I1 completion and the I2 frontier transition; queue teardown remains I2's responsibility.

Two separately reviewed fixture repairs preceded integration: [#153](https://github.com/the-sarge/quic-go-fast/pull/153), merged as `691b8a0e8dcb0bf054a8b445794a4a11fa7a9a5d`, establishes compressed-source EOF before gzip lifetime assertions; [#154](https://github.com/the-sarge/quic-go-fast/pull/154), merged as `303fbff8cf9dd850bcd156b13a77a49f139b96a0`, restores continuing ACK feedback in the captured-loss recovery control and preserves a deterministic delayed-feedback regression. Both fixture defects originated in this fork (#136 and #145); I1's faulty receive cleanup/admission behavior is present in upstream common ancestor `793f74d8e03368c5aded128af6f48d21dbb47f73`.

### Decisions

The maintainer directed ordinary diagnosis and separate bounded repairs for the CI failures. No architecture decision or slice contract needed reopening; the [resumption disposition](https://github.com/the-sarge/quic-go-fast/pull/152#issuecomment-5613362519) supersedes the earlier cross-scope stop. No retained deadlines, sleeps, payload assertions or loss bounds were weakened.

### Validation

I1's reviewed head `bc335595b2cb912b3d03f486ec2f50f4ba14dbcb` passed local suite, focused race and preservation tests, and vet. A disposable source overlay observed actual final pool returns across all ten semantic cells without a maintained production hook. RAS `20260910T050053-d808bdc6a025114952f21051` completed with codex-astra and codex-sol and no findings. All 33 applicable hosted checks passed before exact-head squash merge. [Certification](https://github.com/the-sarge/quic-go-fast/pull/152#issuecomment-5613490701) and [probe receipt](https://github.com/the-sarge/quic-go-fast/pull/152#issuecomment-5613443774) retain the evidence. Each supporting repair also passed its bounded review, local certification and all 33 hosted checks.

An additional earlier QUIC-v2 randomized-corruption run failed at the existing Dial timeout. Its complete capture is retained at `/Volumes/worktrees/quic-go-fast/i1-ci-evidence/issue46-capture-76c6fb97.jsonl` with an adjacent SHA-256 receipt. It was not rerun until green. Historical cause and upstream-versus-fork provenance remain unresolved under existing [#46](https://github.com/the-sarge/quic-go-fast/issues/46); no new random campaign or causal claim is implied.

### Next

I2 #92 is newly eligible after I1; the other accepted dependency edges remain unchanged. The [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) is the live frontier. Continue surviving corruption evidence work under existing #46 rather than opening a duplicate investigation.

---

## I2 retained receive storage and admission merged - 2026-09-10 02:38 EDT

**Main:** `4574917af20c`
**Actor:** Codex

### Completed

Merged [I2, PR #156](https://github.com/the-sarge/quic-go-fast/pull/156) as `4574917af20cb104c6aa5bc3d9d3d91b16fe8c20`, closing #92. Connection admission is sealed under the queue mutex before final drain, overflow and late input are disposed, retained views are released individually, and early replay errors dispose the detached tail. Synchronous handshake cleanup preserves I1's active parsing hold. The product PR records I2 completion and the resulting frontier; failed server publication remains I8's scope.

### Validation

Reviewed head `5c231567aa02524fa51995abc5c62269888f8774` passed all eight semantic-cell regressions, the focused race selection, an uncached full suite including integration packages, vet, repository-specific go-fix checks, tidy, lint and whitespace checks. The [disposable pool-return probe](https://github.com/the-sarge/quic-go-fast/pull/156#issuecomment-5613977403) observed one actual return per tested acquisition and no premature return during active parsing; production source hashes remained unchanged. RAS initial round `20260910T061928-839b7ce1d3de84eac4c6cf8c` completed with eight successful reviewers, one structured-output failure, and zero Fix First clusters. No code fix verification or replacement review was needed. The [final certification receipt](https://github.com/the-sarge/quic-go-fast/pull/156#issuecomment-5614265145) records the exact head and independent dispositions.

### Decisions

The maintainer explicitly directed continuation after the Linux Go 1.27 race integration job failed in `TestMITCorruptPackets/towards_the_client` with a three-second Dial timeout. The [PR-specific exception](https://github.com/the-sarge/quic-go-fast/pull/156#issuecomment-5614094598) accepted that disclosed occurrence; the other 32 hosted checks passed. The failed check was not rerun or reclassified as passing or infrastructure, and no corruption behavior or deadline was relaxed. The capture remains with [#46](https://github.com/the-sarge/quic-go-fast/issues/46); an identical cause or absence of an I2 contribution is not established.

### Next

I8 #98 remains blocked by I5 #95; I2's merge makes no new successor ready. I3, I5, I6, I7, P1, T1 and C1 remain the recorded frontier. The [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) is the live view. Continue the existing #46 investigation without creating a duplicate issue or authorizing another randomized campaign.

---

## Captured corruption timeout explained and deterministic CI merged - 2026-09-10 03:18 EDT

**Main:** `71b930ecd731`
**Actor:** Codex

### Completed

Merged [PR #157](https://github.com/the-sarge/quic-go-fast/pull/157) as `71b930ecd731dc0a829f4f4891b7adf1376a9ea0`, closing [#46](https://github.com/the-sarge/quic-go-fast/issues/46). The I1 and I2 captures establish that every server Initial carrying CRYPTO was corrupted before the existing Dial deadline, while ACK-only Initials arrived and the client never derived Handshake keys. Byte-level mutation/write/read/authentication-rejection chains and the next Initial retry timers explain these two captured failures; they do not establish the cause of every earlier unrecorded occurrence. The [audit](audits/issue-46-captured-corruption/README.md) retains raw captures, hashes, provenance, analysis and limits.

Mandatory corruption CI now covers six real-UDP finite-damage cases followed by reliable delivery, with three simulated Initial-starvation, retransmission-release and undamaged cases. The original random callback remains available through `QUIC_GO_TEST_RANDOM_CORRUPTION=1`. Rebased Windows CI also exposed a token-expiry fixture assumption; the approved test-only repair waits for the decoded token timestamp to exceed its configured maximum age before retaining the original rejection assertions. Production recovery, token validation, corruption deadlines and payload assertions are unchanged.

### Decisions

The maintainer approved replacing mandatory random-success testing with deterministic mechanism and recovery examples, retaining the completed RAS review through the clean rebase onto merged I2 and the bounded token-expiry amendment. The [current contract](agents/corruption-ci.md) defines the fixture scope and evidence budget. No additional RAS review, randomized campaign or unchanged-head hosted rerun was performed.

### Validation

Final candidate `43ae5fbca67bad2cde18b74bf6def277ae9c9a47`, based on `6cc25c236659b4b86594f558916a9cabf29f1d7f`, passed focused corruption v1/v2/race tests, proxy race tests, an uncached full native suite, vet, lint, formatting, root/FIPS module tidy, repository go-fix and source/document whitespace checks. Token validation passed once natively and once with race detection. All 33 applicable hosted checks passed before pinned-head squash merge, including both Windows Go 1.27 unit jobs.

Independent Standards and Spec reviews found no issues. Initial RAS lost one reviewer to a process failure; the single allowed replacement `20260910T064327-6c901b3e97a808ecdcba75ca` completed both reviewers and synthesis. The stale capture-guide finding was fixed; the proposed test-selection omission was rejected after checking that the unanchored suffix includes the intended test families. No findings remain deferred from this review.

---

## I3 terminal incoming disposal merged - 2026-09-10 04:32 EDT

**Main:** `0e4600452e7c`
**Actor:** Codex

Merged [I3 / PR #160](https://github.com/the-sarge/quic-go-fast/pull/160) as `0e4600452e7cd8b7a2edc0afdaa6008d3aaf9e96`, closing [#93](https://github.com/the-sarge/quic-go-fast/issues/93). Terminal transport drops, recognized stateless resets, closed handlers and successful non-QUIC copy-out now dispose their owned input buffers while successful transfers retain ownership. The product PR includes I3 completion and I4 readiness in the normative plan and program index.

The [scoped preservation audit](adr/2026-09-08-incoming-lifetime-plan.md#i3--dispose-terminal-transport-routes-and-closed-handler-input), published through [#161](https://github.com/the-sarge/quic-go-fast/pull/161), preserved the existing bufferless empty sentinel from supported caller-supplied batch readers at the same transport guard. I7 retains reader retry/cache behavior. No public API, wire policy, socket lifetime or new runtime representation changed.

Validation on reviewed head `8a7b3b4857bb1bc1cce4f8e368bb07851ebcdc9c`: red-before-green lifetime and bufferless-sentinel regressions; full uncached package suite, vet, golangci-lint, focused race tests, disposable pool-return observations and unchanged production source hashes. Both accepted review findings were fixed and exact-head verified; final review `20260910T082412-a7eb70e95f52d4fe8509e945` had no findings or follow-ups, with reviewer quorum met despite adapter failures. All 33 applicable hosted checks passed, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34454698223), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34454698243), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34454698385), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34454698307) and [interop](https://github.com/the-sarge/quic-go-fast/actions/runs/34454698255). [Full certification receipt](https://github.com/the-sarge/quic-go-fast/pull/160#issuecomment-5615592355).

At this timestamp, I4's sole blocker #93 is closed; the committed frontier is I4, I5, I6, I7, P1, T1 and C1. The [program tracker](https://github.com/the-sarge/quic-go-fast/issues/101) remains the live progress view. No deferred review follow-ups or untraced effects remain in I3.

---

## Transport queued receive storage retired - 2026-09-10 10:04 EDT

**Main:** `419a43f5e262`
**Actor:** Codex

### Completed

Merged [I4 / PR #163](https://github.com/the-sarge/quic-go-fast/pull/163) as `419a43f5e262b7c9e368e10d6e18a2b87e9f7f99`, closing [#94](https://github.com/the-sarge/quic-go-fast/issues/94). Transport initialization now publishes one stable non-QUIC channel; listener termination drains pending non-QUIC input, and the send worker drains pending stateless-reset input after the listener barrier. Concurrent readers own their dequeued packets exclusively. Caller-owned sockets and public Close's asynchronous-write behavior are preserved. The product PR includes I4's committed completion and frontier transition.

### Validation

The bounded regressions demonstrated the original pending-storage and channel-publication failures. The final candidate `dd115a2d625dd96ea30746a8b2c6c608688070f4` passed focused preservation and race tests, the ordinary package suite, go vet, golangci-lint, and the disposable pool-return probe; nine selected acquisitions each returned once, with no premature return during the blocked reset write. Production source bytes remained unchanged by the probe. Initial review `20260910T133530-a55c46f54fa80f50975a582a`, exact-head verification of its accepted reader-assertion finding, and replacement review `20260910T135534-c8c8c8d44f96dbb028d29855` completed; the replacement had no findings. Eight reviewers completed each review; one adapter failed candidate recovery and quorum was satisfied. All 33 jobs across the final candidate's applicable push/PR workflows succeeded, including [hosted integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34485390600).

### Decisions

A failed existing HTTP/3 timing assertion did not establish an architecture or scope change. The premature architecture-handoff routing was retracted, and I4 continued under its accepted contract; see the [routing correction](https://github.com/the-sarge/quic-go-fast/pull/163#issuecomment-5619533525). The earlier exact candidate had discordant independently triggered integration runs; no same-head rerun or timing-threshold weakening was used. Detailed findings and evidence remain in the [PR discussion](https://github.com/the-sarge/quic-go-fast/pull/163).

### Next

I4 unlocks no additional child. I5, I6 and I7 remain ready; I8 remains blocked by I5. The [program tracker](https://github.com/the-sarge/quic-go-fast/issues/101) is the live frontier view.

---

## I5 server 0-RTT retirement landed - 2026-09-10 10:52 EDT

**Main:** `e3cad9517aed`
**Actor:** Codex

### Summary

Merged [I5 product PR #166](https://github.com/the-sarge/quic-go-fast/pull/166) as `e3cad9517aed9241802d9235db48ca8cc10df876`, closing [slice #95](https://github.com/the-sarge/quic-go-fast/issues/95). One server receive-owner operation now retires retained 0-RTT groups on expiry, Retry, refusal and registration collision. Successful connection handoff empties ownership before group deletion; queue bounds and Initial-first ordering are preserved.

### Validation

Retry, refusal and collision regressions failed before the fix. The six accepted semantic cells, existing server/transport/closed-handler tests, focused race selection, ordinary non-integration package suite and `go vet ./...` passed. A disposable source overlay observed 75 selected input acquisitions returning to the pool exactly once, with no early return in the transfer/admission controls; no permanent observer was added.

RAS run `20260910T142415-e16736fd346cac9cdf60dfa7` completed with eight successful reviewers, adjudication and synthesis; one reviewer failed structured-output parsing. The only accepted finding corrected two stale plan-status sentences. Duplicate and cosmetic findings were rejected, with no deferred follow-ups. The shared docs-only policy allowed skipping another RAS cycle after those sentence edits. Final certification at `f0aac7f4fffef570e9f7e4f2bcc443d2466f5ba3`, base `534d34fc1d6ba1304f2dd7af57fe0784d9ad0077`, passed with a clean tree. All 33 same-head hosted checks passed, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34490622978), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34490622857), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34490622910), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34490622976), and [interop](https://github.com/the-sarge/quic-go-fast/actions/runs/34490622810). See the [validation receipt](https://github.com/the-sarge/quic-go-fast/pull/166#issuecomment-5620499527).

### Next

I2 and I5 are complete, making I8 ready. I6/I7 retain their independent scopes; server shutdown and unpublished-connection abort remain separately owned, with no additional untraced effects discovered. The product PR already includes the committed completion/frontier transition. [Track #87](https://github.com/the-sarge/quic-go-fast/issues/87) and [program #101](https://github.com/the-sarge/quic-go-fast/issues/101) are the live progress views.

---

## Test fixture cleanup and HTTP diagnostics landed - 2026-09-10 11:38 EDT

**Main:** `e61f3a541dab`
**Actor:** Codex

### Summary

Merged [PR #168](https://github.com/the-sarge/quic-go-fast/pull/168), a test-only repair discovered while closing the I5 journal: server fixtures now join their transport listeners, and HTTP/0.9 fixtures close their transport and UDP socket. HTTP request failures now print bounded client/server event tails.

### Validation

The fixture listener completion and socket leak were demonstrated before the fix. Exact reviewed head `1ff2be5eff7137cd96ecfe77a66ff212d3dda4c7` passed the 100-run CPU-8 fixture gate, focused server/dial and race checks, HTTP/0.9 race checks, the ordinary non-integration package suite, vet, and all 33 hosted checks. Eight RAS reviewers completed with no required fixes; see the [certification and review receipts](https://github.com/the-sarge/quic-go-fast/pull/168#issuecomment-5621294280).

### Decisions

The observed TestDial assertion is distinct from [#44](https://github.com/the-sarge/quic-go-fast/issues/44) and [#151](https://github.com/the-sarge/quic-go-fast/issues/151). The HTTP/0.9 initial-request timeout resembles #151 only at symptom level; neither the cleanup defects nor a shared root are established as its cause. See the [comparison](https://github.com/the-sarge/quic-go-fast/pull/168#issuecomment-5621039721).

### Next

The unresolved HTTP/0.9 timeout is tracked in [#169](https://github.com/the-sarge/quic-go-fast/issues/169) for evidence from the next natural failure. The architecture frontier remains in the [live program tracker](https://github.com/the-sarge/quic-go-fast/issues/101); I5 product work is complete and I8 is ready.

---

## Server admission and owner-exit cleanup landed - 2026-09-10 13:09 EDT

**Main:** `fba5aaa1f55f`
**Actor:** Codex

### Summary

Merged [PR #170](https://github.com/the-sarge/quic-go-fast/pull/170) as `fba5aaa1f55f89db0a1cb9fe2f34fe99b1c64f3e`, completing the I6 implementation and closing [issue #96](https://github.com/the-sarge/quic-go-fast/issues/96). Server admission now synchronizes enqueue with closure and disposes rejected input. The receive worker drains queued datagrams and retained 0-RTT groups; the response worker drains its queues after the receive producer exits. Active Retry responses also release their input. Public close waits, established connections, and caller-owned sockets are preserved.

### Validation

Rejection, receive-exit, response-exit, and active-Retry regressions failed before their fixes. The I5 admission fixture preserves its pre-shutdown retention check and now checks disposal after owner exit. Exact candidate `c827e302c4af0f240dabcf8cb66f3d5f76b32a52` passed the focused race selection, `go test -count=1 ./...`, `go vet ./...`, golangci-lint, module-tidiness checks, and all 33 applicable hosted checks. Focused listener-close integration tests also passed.

The initial RAS review identified import formatting and a local test timeout; both were independently accepted and fixed, and exact-head verification resolved every cluster. The final replacement review completed with nine reviewers and zero findings. A disposable Go build overlay observed exactly one pool return for each of thirteen tracked I6 inputs and no premature return during a blocked response write. Source hashes stayed unchanged, and ordinary certification ran without the overlay. See the [complete review, observation, and hosted-validation receipt](https://github.com/the-sarge/quic-go-fast/pull/170#issuecomment-5622493452).

### Decisions

The accepted [I6 contract](adr/2026-09-08-incoming-lifetime-plan.md#i6--seal-server-admission-before-worker-drains) retains successive concrete owners, one terminal admission signal, and the seven-cell evidence budget. There is no new receive engine, public wait guarantee, maintained observer, performance claim, or newly untraced effect. No deferred review finding remains.

### Next

I7 and I8 remain ready; I6 adds no newly unblocked successor. The product PR already contains its committed completion and frontier transition. [Track #87](https://github.com/the-sarge/quic-go-fast/issues/87) and [program #101](https://github.com/the-sarge/quic-go-fast/issues/101) are the live progress views.

---

## I7 raw-reader ownership completed - 2026-09-10 14:05 EDT

**Main:** `781dca6cfc3c`
**Actor:** Codex

### Completed

Merged [I7 product PR #172](https://github.com/the-sarge/quic-go-fast/pull/172) as `781dca6cfc3cdda8b46d3273449b8edf504d242f`, closing [#97](https://github.com/the-sarge/quic-go-fast/issues/97). Basic read failures release their acquired packet buffer. OOB readers explicitly transfer slot ownership, discard failed batches without stale replay, retry zero-progress reads, and reclaim unread/cached storage after listener termination. Previously returned packets and caller-owned sockets retain their lifetime. The product PR includes I7 completion and the committed frontier transition; I8 remains ready.

### Decisions

Executed the existing [I7 contract](adr/2026-09-08-incoming-lifetime-plan.md#i7--make-raw-reader-cached-ownership-explicit) without re-audit or expanded ownership. RAS review `20260910T174013-4e9d9403d93ba9b8a1749c64` produced one accepted comment-only correction and the scheduled certification-receipt obligation. Duplicate guard removal and speculative fixture hardening were rejected; no deferred follow-ups remain. The shared documentation-only policy permitted skipping another RAS review/verification cycle. Nine initial reviewers completed; one adjudicator failed, and synthesis completed from the remaining results. [Independent dispositions](https://github.com/the-sarge/quic-go-fast/pull/172#issuecomment-5623077987) retain the details.

### Validation

Reproduced missing basic pool return, stale batch replay, zero-progress empty return, and missing listener cleanup before their fixes. Exact candidate `03f2df18a42e699c737fb622cd9d32ae6cf35799` passed disposable Darwin/Linux pool-return observations, `go test -count=1 ./...`, focused race tests, vet, golangci-lint, gcassert, module-tidy verification, Windows/amd64 test compilation, and diff checks. Every tracked source file remained unchanged by observation/certification. The [certification receipt](https://github.com/the-sarge/quic-go-fast/pull/172#issuecomment-5623103852) records the bounded evidence, overlay mapping, source hash, observations, and limitations; no permanent instrumentation was added.

All nine triggered workflows and 33 jobs passed on that exact head, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34511095071), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34511095048), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34511095037), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34511095028), and [interop image](https://github.com/the-sarge/quic-go-fast/actions/runs/34511095119). The separate push lint run required one same-head failed-job retry after an HTTP 500 while downloading the linter, before repository analysis; the [diagnosis](https://github.com/the-sarge/quic-go-fast/pull/172#issuecomment-5623129565) distinguishes this external fault from a code failure. No untraced effects remain within I7's accepted scope.

### Next

At this entry, I8, P1, T1, and C1 remain ready; I7 unlocks no new successor. [Program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) is the live frontier view, and [incoming-lifetime parent #87](https://github.com/the-sarge/quic-go-fast/issues/87) tracks the remaining I work.

---

## I8 unpublished Initial cleanup landed - 2026-09-10 15:22 EDT

**Main:** `aa879ce77e17`
**Actor:** Codex

### Summary

Merged [I8 product PR #174](https://github.com/the-sarge/quic-go-fast/pull/174) as `aa879ce77e17f095217e8fe3562347c04063f7cc`, closing [#98](https://github.com/the-sarge/quic-go-fast/issues/98). Connection-ID generation failures now release the Initial and cancel construction before allocating a tracer. Registration failures abort the real unpublished connection without starting or waiting for the protocol loop, reclaim its queued Initial and unstarted TLS/qlog resources, and retire the server-held 0-RTT group while preserving the winning routing and reset-token entries.

### Decisions

Preserved the accepted [I8 ownership and publication contract](adr/2026-09-08-incoming-lifetime-plan.md#i8--complete-failed-initial-construction-and-publication). Abort reuses I2 admission/drain and I5 retirement, retains Initial-before-publication ordering, preserves cancellation causes, and leaves shared sockets and routing authority untouched. The product PR owns I8 completion and the committed frontier transition; no separate status PR is needed. No new public API, receive engine, maintained observer framework, or tracer-close latency guarantee was introduced.

### Validation

Generation and real collision regressions failed before the runtime fixes. Initial RAS review `20260910T183643-63cd85f4c9df8d538400984e`, source verification, and replacement review `20260910T190316-6111b75042cce642b68307d5` completed with no unresolved accepted findings. The replacement had eight successful reviewers and one invalid structured result, meeting configured quorum. Independent dispositions and the rejected hypothetical mock-lifecycle extension are recorded in the [PR review receipts](https://github.com/the-sarge/quic-go-fast/pull/174#issuecomment-5624166162); no follow-up items survived.

At certified head `30e1b65a5649b34c41391fa789de256e5ed776d0`, the bounded disposable pool-return overlay observed each losing Initial return once, retained 0-RTT return once on collision, and no premature 0-RTT return on generation failure. Production source hashes were unchanged. A focused race selection, `go test ./... -count=1`, `go vet ./...`, local golangci-lint, formatting and clean-tree checks passed. All 33 same-head hosted checks passed, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34517590760), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34517590736), and [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34517591141). The [final certification receipt](https://github.com/the-sarge/quic-go-fast/pull/174#issuecomment-5624180730) records the exact head, base, observations, and hosted runs.

### Next

The incoming-lifetime implementations I1–I8 are complete. P1, T1, and C1 remain the implementation frontier; C2 remains blocked by C1. No incoming-track successor or remaining untraced effect was identified. This entry is the post-merge journal snapshot; [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) is the live view for task closure and subsequent dispatch.

---

## P1 module payload boundary merged - 2026-09-10 16:29 EDT

**Main:** `2d5cd37b3449`
**Actor:** Codex

[PR #176](https://github.com/the-sarge/quic-go-fast/pull/176) completed P1 and closed [#99](https://github.com/the-sarge/quic-go-fast/issues/99). An inert nested module excludes `docs/audits/` from the published parent module while all existing audit and historical journal bytes remain unchanged in Git. The shipped [evidence index](audit-evidence.md) links 65 archived Markdown documents at an immutable commit; current normative audit pointers use pinned repository links. The product PR owns P1's completion/frontier transition, with no P-track successor.

The authoritative `golang.org/x/mod/zip` comparison reduced the actual archive from 23,534,240 to 1,277,464 compressed bytes (94.5719%). All 516 audit files were excluded and preserved in Git; 547 non-documentation archive entries, root module/dependency bytes and the package list were unchanged. The final archive passed compilation, a local replacement-consumer build, 66 pinned Git-target checks, and documentation checks. The ordinary test suite and `go vet ./...` passed before the final checkbox/link-only correction. RAS `20260910T195342-72b238199c360bcfbd91f765` produced two accepted docs-only corrections; unsupported process claims and duplicate work items were rejected. The shared low/nit policy skipped another RAS cycle after the corrections. [Exact-head certification](https://github.com/the-sarge/quic-go-fast/pull/176#issuecomment-5624882928) records final head `3e3e5365622dc1ceea23ae3a59e6fbc3b0037229`; all 33 hosted checks passed before its guarded squash merge, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34525527465), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34525527472), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34525527482), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34525527473) and [interop](https://github.com/the-sarge/quic-go-fast/actions/runs/34525527502).

No deferred review finding or untraced effect remains. Git clone size and runtime behavior are unchanged; historical checkout-relative journal links use the shipped index when read from the module cache. The remaining implementation frontier is C1 #108 and T1 #100; C2 remains blocked by C1. [Program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) owns the live view.

---

## T1 maintained emission entrypoints merged - 2026-09-10 17:16 EDT

**Main:** `d4bde69fedb5`
**Actor:** Codex

[PR #179](https://github.com/the-sarge/quic-go-fast/pull/179) completed T1 and closed [#100](https://github.com/the-sarge/quic-go-fast/issues/100), merging as `d4bde69fedb5e8cb72559d8498096c50d036dd4e`. Eight maintained emission, handshake and MTU-probe callers now use `Conn.triggerSending` and the shipped packet-emission composition. Their real packer, recovery, queue and scenario-local assertions remain intact. The historical helper moved unchanged behind `emission_experiment` for its two frozen callers. The product PR includes the T1 completion and program-frontier transition.

The full-queue blocked-state assertion failed through the historical helper and passed through the shipped owner. Baseline and migrated emission/handshake/probe families passed, along with one focused race invocation and compile-only experiment validation. All 237 production Go, module and workflow files are byte-identical to the base. The full local suite and `go vet ./...` passed again at final head `e042d4b1a1b82233512ca105163728d1bff2021b`, with base `6c3030ca82f6c3a7ccf14b9cc5104fdc66fe3e66`; see the [exact-head receipt](https://github.com/the-sarge/quic-go-fast/pull/179#issuecomment-5625451132).

RAS review `20260910T205322-cf0f9b22daf63bbf4e668d81` completed with six successful reviewers and three failed Claude processes, meeting quorum. Its one low-severity finding was independently accepted and resolved by reconciling the plan's delivered-evidence cells and first three acceptance checkboxes. The [disposition](https://github.com/the-sarge/quic-go-fast/pull/179#issuecomment-5625430691) records the shared docs-only policy for skipping another RAS cycle. All 33 hosted checks passed on the final head, including [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34530258802), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34530258792), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34530258774), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34530258761), and [interop](https://github.com/the-sarge/quic-go-fast/actions/runs/34530258810), before the guarded squash merge.

No deferred review finding, newly ready T-track successor, or untraced effect remains. C1 #108 is the remaining implementation frontier; C2 #109 remains blocked by C1. The [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) owns the live view after this post-merge snapshot.

---

## C1 constructor-owned emission coverage landed - 2026-09-10 18:04 EDT

**Main:** `79db4ae39b5a`
**Actor:** Codex

C1 merged in [PR #181](https://github.com/the-sarge/quic-go-fast/pull/181), closing [#108](https://github.com/the-sarge/quic-go-fast/issues/108). Four constructor-owned regression cells cover server Initial emission, client Initial emission without/with a stored token, and the StartHandshake-error worker boundary. Real packer, sealing, recovery and initial queue remain intact; the tests observe header values, stored RTT, packet registration before I/O, feedback binding, joined worker teardown and cleanup-neutral startup failure. Production, module, workflow and existing test bytes are unchanged.

The bounded review completed with two accepted fixes, exact-head verification and a replacement review with no required fixes or follow-ups. Two marginal polish suggestions remain intentionally untracked. Reviewer/adjudicator failures did not prevent quorum or synthesis; details and dispositions are in the [validation receipt](https://github.com/the-sarge/quic-go-fast/pull/181#issuecomment-5626053361).

Candidate `3ca55c5880de92ac5345a8aa7a6084f69cacaf51` passed post-review full tests, vet, focused preservation/race selections, experiment-tag compilation and source identity checks on Go 1.27.0 darwin/arm64. All hosted unit, integration, lint, cross-compilation and interop checks succeeded on that head before guarded squash merge. Evidence remains example-level across the four accepted cells; no new performance, protocol-conformance or platform campaign was undertaken.

The product PR owns the committed C1-complete/C2-ready transition. C2 is the next frontier, with its C1 blocker now closed; [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) is the live view. Dispatch C2 in a fresh architecture-slice context. This journal append changes no frontier contract.

---

## C2 complete initial emission assembly landed - 2026-09-10 19:03 EDT

**Main:** `3700e9e1eb82`
**Actor:** Codex

C2 merged in [PR #183](https://github.com/the-sarge/quic-go-fast/pull/183) as `3700e9e1eb8278eddc881ba1d56a0056411d35af`, closing [#109](https://github.com/the-sarge/quic-go-fast/issues/109). Both production constructors now pass a local packet packer to `Conn.initPacketEmission`, which creates the queue and installs the complete emission once. The staged packer/queue fields and self-reading binder are removed. Packet-time source, layout, live recovery/path bindings, token/publication/worker ordering and all existing tests remain unchanged.

Initial RAS review `20260910T225223-a1bde93eada2b96b80d4959a` completed with all reviewers/adjudicators successful and no required fixes. The sole stale line-reference finding is pre-existing marginal documentation polish and remains untracked after revalidation at the merged head (`connection.go:512`, plan paragraph at line 13). No fix verification or replacement review was required.

Candidate `e03118bfb966192e4ebf35f3e2a28e6bcba3a30e`, based on `ee71e7a59e4a7c4a6b55ee162f9c79c1ec93ec15`, passed exact-head full tests, vet, focused preservation/race selections, experiment-tag compilation and source/compiler checks on Go 1.27.0 darwin/arm64. Constructor coverage passed unchanged before and after the refactor. The initializer and queue construction still inline; one queue and four channels remain per production route, with no extra setup object or closure. All 33 hosted checks and nine triggered workflows passed before the guarded squash merge. See the [review and certification receipt](https://github.com/the-sarge/quic-go-fast/pull/183#issuecomment-5626588918).

The product PR records C2 and all fifteen program implementation slices complete, with an empty implementation frontier and no newly ready successors. No new follow-up or untraced effect was introduced; broader performance/protocol guarantees and unrelated cleanup retain their declared non-goals. Post-merge task/mirror reconciliation is the remaining administrative closure at this timestamp; [program tracker #101](https://github.com/the-sarge/quic-go-fast/issues/101) owns the live view.

---

## Optional qlog resource ownership repaired - 2026-09-10 20:50 EDT

**Main:** `2a204483e154`
**Actor:** Codex

### Completed

Merged [PR #185](https://github.com/the-sarge/quic-go-fast/pull/185), closing [issue #76](https://github.com/the-sarge/quic-go-fast/issues/76). The HTTP/3 server preserves the rawConn identity captured by cancellation so recorder shutdown waits on its producer group. Optional qlog directory failures log and return nil, and buffered sink closure always attempts the underlying Close while joining flush and close errors. Public API/module identity and packet-emission ownership are unchanged.

### Validation

Each focused regression failed before its corresponding fix and passed afterward. Affected packages, focused race checks, go vet, module tidiness and formatting checks passed. The uncached full-suite run passed all packages except integrationtests/self when invoked with the unit workflow's 10× timing factor; that package passed with its documented 3× factor after diagnosing the resulting context-deadline versus idle-timeout mismatch. No source change was made for the command correction.

Standards and Spec reviews found no issues. RAS run `20260911T004140-42568e5d10429dd6350f4ef3` completed with two successful reviewers, no findings and no follow-ups. All 33 hosted checks passed on `119d33be55b0cde3d84a0107c410edc44b6c3503` before guarded squash merge. The bounded contract is [optional tracing ownership](agents/optional-tracing-ownership.md).

---

## Repeated successful path probes repaired - 2026-09-10 21:23 EDT

**Main:** `eb83d2ff29d1`
**Actor:** Codex

Merged [PR #187](https://github.com/the-sarge/quic-go-fast/pull/187) as `eb83d2ff29d1be6b80222921ea6fc8072e135d3e`, closing [issue #74](https://github.com/the-sarge/quic-go-fast/issues/74). Matching PATH_RESPONSE frames now complete the current probe independently of previous path validation. Starting another sequential probe clears old challenge membership and queued retries without revoking switch eligibility. The public API and packet-emission ownership remain unchanged.

Regressions failed before their fixes and passed afterward: two consecutive successful probes, stale queued retries, and stale sent retry responses. Existing cancellation, retransmission, switching and close tests pass. The final candidate `dfd03fc85ead6fa2f33b12cd0a660dea5e5d721a` passed the uncached full suite, focused race tests, vet, repository lint, formatting, root/FIPS module tidiness and diff checks. Standards and Spec reviews found no issues.

Initial RAS review `20260911T005629-4aa822173def58e4f9b859af` identified one accepted retry-boundary defect. The agent repaired it; exact-head verification resolved C-001 with no new findings. Replacement review `20260911T010606-a0df2bb505a6670e2219442f` had zero findings. No automated fixer was used.

Hosted CI passed 32 checks. The macOS PR integration job failed the unrelated `TestHTTP3ServerHotswap` replacement-server request; the same-head push integration job and one focused local reproduction passed. The cause remains unestablished. Under the maintainer's explicit instruction to file unrelated intermittent failures and move on unless a simple fast fix was supported, [issue #188](https://github.com/the-sarge/quic-go-fast/issues/188) records the failure, logs and investigation boundary. The reviewed head was squash-merged with a matching-head guard. No failed hosted job was rerun, no CI rules or timeouts were changed, and the failed check is not represented as successful.

---

## Large accepted stream writes scale linearly - 2026-09-10 22:07 EDT

**Main:** `9245232d4909`
**Actor:** Codex

Merged [PR #190](https://github.com/the-sarge/quic-go-fast/pull/190) as `9245232d49095e0d792990a2b81e13deb1de5428`, closing [issue #75](https://github.com/the-sarge/quic-go-fast/issues/75). Large `TryWriteAll` admissions now grow pending storage geometrically and segment by copying packet-sized prefixes while advancing the large unsent tail. Recovery retains bounded packet storage. Reliable-prefix cancellation copies the retained prefix once to release discarded suffix storage. Atomic admission, caller ownership, public API/module identity and transport accounting are preserved; no application throughput gain is claimed.

Bounded checks cover 32, 128 and 512 KiB single and repeated admissions, caller reuse, atomic rejection, partial-drain appends, retransmission splitting, and zero/short/large reliable-prefix cancellation. Recovery-capacity and cancellation-detachment regressions failed before their fixes and passed afterward. Final head `b31ca7b6dc7a03a01bcbfbd03b9b119733231639` passed the uncached full suite with the documented CI timing scale (`TIMESCALE_FACTOR=3`), focused stream race tests, vet, module tidiness and formatting checks. An earlier scale-1 full run failed `TestStreamDataBlocked` at its 5 ms deadline; ten focused base and candidate controls passed, and coverage showed neither changed storage path executed. The failed run remains disclosed in the PR validation record.

Standards review found no issues. Spec review identified a partial-drain append assertion gap, which was fixed and independently verified. Initial RAS `20260911T014623-7332fc45af633db44697075f` accepted that evidence correction and a cancellation storage-lifetime fix. Exact-head verification resolved both findings. Replacement RAS `20260911T015812-34d6949c9b16cb1c17a71729` completed with two successful reviewers and zero findings or follow-ups. All 33 applicable hosted checks passed unskipped before guarded squash merge. No automated fixer or archived performance campaign was used. The [bounded contract](agents/large-accepted-stream-writes.md) and [PR review/validation record](https://github.com/the-sarge/quic-go-fast/pull/190) retain the scope and evidence.

---

## Qlog fork provenance corrected - 2026-09-10 23:41 EDT

**Main:** `05050af18220`
**Actor:** Codex

### Completed

Merged [PR #192](https://github.com/the-sarge/quic-go-fast/pull/192), closing [issue #77](https://github.com/the-sarge/quic-go-fast/issues/77). Qlog provenance selects the supplying fork replacement version, marks local replacements including Go’s `(devel)` metadata, and preserves explicit linker overrides. Both interop linker flags now target qlogwriter. Module identity and ADR 0002 adoption remain unchanged.

### Validation

Real consumer regressions cover ordinary modules, versioned and local replacements, and linker revisions with and without replacement. Focused tests, race tests, both interop builds, go vet, module tidiness and focused lint passed. The uncached full suite passed with the integration workflow’s TIMESCALE_FACTOR=3; an earlier invocation with the unit-only factor 10 reversed two existing corruption-fixture timeout expectations and was corrected without changing product code. Independent Standards and Spec reviews and RAS run `20260911T033047-c68f2968882468d5e2db1e3b` reported zero findings. All 33 hosted checks passed on reviewed head `33610b1d956edabc8a358f3a37b4081897dd0d59` before the guarded squash merge.

---

## Short HTTP/0.9 request rejection landed - 2026-09-11 00:05 EDT

**Main:** `a591f863483d`
**Actor:** Codex

Merged [PR #194](https://github.com/the-sarge/quic-go-fast/pull/194), closing [issue #78](https://github.com/the-sarge/quic-go-fast/issues/78). The HTTP/0.9 interop server now uses a length-safe prefix check, retaining reset code 42 for invalid requests. Real QUIC stream regressions cover empty, truncated, invalid, and valid lines, including successful requests after rejections on the same connection.

Validation reproduced the empty-stream process panic before the fix. Package tests, a focused race check, go vet, and the uncached full suite passed on reviewed head `031d766ecf0f2779cc1520b1ae0e9417ab6c1e25`; all 33 hosted checks succeeded before the guarded squash merge. The full suite used the integration workflow multiplier 3. An initial invocation with unit-test multiplier 10 caused two unrelated corruption-diagnostics assertions to observe the QUIC idle timeout before the context deadline; the corrected configuration passed without code changes.

Standards and spec reviews found no issues. RAS run `20260911T035557-80b0f784515a4876a68c5fe5` completed with no required fixes; no fix-verification or replacement review was needed. Its sole observation concerned the unchanged malformed-URL error branch, independently deferred for separate tracking after merged-head revalidation.

---

## Optional HTTP/3 tracing-header collection landed - 2026-09-11 00:42 EDT

**Main:** `488fcd487e89`
**Actor:** Codex

Merged [PR #197](https://github.com/the-sarge/quic-go-fast/pull/197), closing [issue #82](https://github.com/the-sarge/quic-go-fast/issues/82). HTTP/3 request and response decoding now collects tracing-only header fields only when a recorder exists, following the trailer decoder convention. Decoded headers, complete logged fields including duplicates, public API/module identity and packet-emission ownership remain unchanged. Isolated request/response allocation fixtures measure 10 versus 14 and 9 versus 12 allocations respectively when collection is omitted; throughput remains unmeasured.

Validation on reviewed head `290837e421319c9cda60a0c91c990902e064856a`: HTTP/3 tests, focused race checks, the uncached full suite with the integration workflow's `TIMESCALE_FACTOR=3`, vet, module tidiness, HTTP/3 go-fix diff, golangci-lint and gcassert passed. All 33 applicable hosted checks succeeded before the guarded squash merge. An unscaled `TestConnDataBlocked` read-deadline failure also reproduced on the unchanged base; the write accepted the expected bytes but the server read returned zero. Its fixture was revalidated unchanged against the merged commit and tracked in [issue #198](https://github.com/the-sarge/quic-go-fast/issues/198).

Standards and spec reviews found no issues. Initial RAS `20260911T041810-39d66d5a85bbb0dde1f1428f` identified one duplicated test-goroutine finding, which was fixed and verified at the exact pushed head. Replacement RAS `20260911T043243-7ee86710e66c41b7d08a5a38` required no further fixes or follow-ups; seven reviewers completed and one failed structured delivery, with quorum and synthesis complete. Two low-value verification-aid nits were independently deferred without separate busywork after merged-head revalidation. [Review and certification receipt](https://github.com/the-sarge/quic-go-fast/pull/197#issuecomment-5629565827) and [hosted run receipt](https://github.com/the-sarge/quic-go-fast/pull/197#issuecomment-5629573162) retain the evidence and dispositions.

---

## Suite leak detection and TLS fixture cleanup landed - 2026-09-11 01:02 EDT

**Main:** `113633840bfb`
**Actor:** Codex

Merged [PR #200](https://github.com/the-sarge/quic-go-fast/pull/200) as `113633840bfb4ae933965fdf20ff895d2e37d45e`, closing [issue #80](https://github.com/the-sarge/quic-go-fast/issues/80). The root suite leak guard now recognizes `Conn.run`, with a real pending-dial positive control. Both ClientHello helpers copy handshake data before closing and joining their TLS clients. Both certificate testdata packages now use ephemeral loopback ports, close their listeners and client sockets, and join bounded server workers. The change is confined to test fixtures and cleanup verification.

The red-stage controls reproduced the retired guard's missed live loop, ten retained TLS workers per helper, and repeated certificate bind failures. On final head `f407d2c3e445d97972c7366b987333effbe6b218`, focused checks passed ten repetitions normally and under race, explicitly including both `testdata` packages omitted by `./...`. The uncached full suite with `TIMESCALE_FACTOR=3`, vet, module tidiness, formatting/go-fix checks, golangci-lint and gcassert passed. All 33 applicable hosted checks succeeded without skips or reruns before the guarded squash merge.

Standards and Spec reviews each found no issues. RAS run `20260911T044916-d14e03ad3fd41ddb0be31d76` completed with all eight reviewers and adjudicators, zero required fixes and zero follow-ups. The implementing agent rejected a request to strengthen the copy-observation test beyond its behavioral sanity check and deferred optional secondary-error suppression in certificate cleanup as low-value polish without a separate tracking task. The latter was revalidated at both certificate fixtures' line 20 on merged commit `113633840bfb4ae933965fdf20ff895d2e37d45e`; resource cleanup remains correct. No fix-verification or replacement review was needed. The [review and certification receipt](https://github.com/the-sarge/quic-go-fast/pull/200#issuecomment-5629726498) retains the evidence and dispositions.

---

## Packet-loss directions corrected - 2026-09-11 02:10 EDT

**Main:** `724b605bb969`
**Actor:** Codex

### Completed

Merged [PR #202](https://github.com/the-sarge/quic-go-fast/pull/202) as `724b605bb969d65ce4fdd82cf5f561b22446fc3e`, closing [issue #79](https://github.com/the-sarge/quic-go-fast/issues/79). The one-third-loss fixture now filters by the requested direction, while the explicit `both` case preserves historical bidirectional Bernoulli loss and independent ten-consecutive-drop limits. Handshake leaf names include direction, Retry, speaking order, post-quantum setting and certificate-chain dimensions. Current fixture documentation distinguishes the correction from historical diagnostics; archived #44 evidence is unchanged, its root cause remains unresolved by this correction, and #46 remains separate.

### Validation

The controlled direction regression failed before the fix and passed afterward. Controlled direction/decision and streak/reset tests, the deterministic v1/v2 corpus, focused v2 race tests, all 225 unique dimensioned leaf names, the uncached full local suite, complete v2 self suite, `go vet ./...`, `golangci-lint run --timeout=3m`, `go mod tidy -diff` and diff checks passed. No random campaign was run.

Standards and Spec reviews had zero findings. RAS run `20260911T055146-cab0ebab533179405f8b8c57` reviewed `7d969137fa9d9cf070d36d70ab3679edfada523c` with seven successful reviewers and one unusable reviewer response; quorum and synthesis completed. Its duplicate stale-documentation findings were accepted and fixed; marginal pre-existing dead-condition cleanup was deferred without follow-up busywork. The documentation-only correction used the shared no-rerun policy, with renewed local certification at `19af34b27edbfb4fd643211ac435bc39ea06085e` and unchanged tested Go/dependency files. No unresolved design decision remained.

All 17 applicable hosted jobs succeeded on that final head: [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34568053509), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34568053593), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34568053479), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34568053538), and [interop image](https://github.com/the-sarge/quic-go-fast/actions/runs/34568053606). The merge was guarded with the exact head SHA.

---

## Integration outcome assertions repaired - 2026-09-11 02:28 EDT

**Main:** `e611e4a97843`
**Actor:** Codex

Merged [PR #204](https://github.com/the-sarge/quic-go-fast/pull/204) as `e611e4a97843601b9fcef26b1ce8b20071bb952a`, closing [issue #81](https://github.com/the-sarge/quic-go-fast/issues/81). Both multiplex servers now serve data and both receive results must succeed. Migration measures old-path client traffic before switching through receiver EOF, explicitly permitting server ACKs. Listener lifetime checks send from the original caller-owned socket to an observer that validates source and payload, avoiding competition with transport reads during draining. Transport closure checks the awaited second connection's own cause. Production behavior, timeout durations, Linux skip, and historical investigations remain unchanged.

Five temporary negative controls failed at the intended assertions: each absent multiplex server, omitted migration switch, closed original socket, and a distinct second-connection closure cause. Restored macOS QUIC v1 tests and focused race QUIC v2 tests passed. The uncached full suite, vet, lint, root/FIPS module-tidy and formatting checks passed on reviewed head `106cf26c44cdf2b1c3a677f3ccbe96ff5499a421`. All 33 hosted checks succeeded without skips or reruns before the guarded squash merge. The [audit note](audits/issue-81-integration-outcomes.md) records the controls and the competing-reader fixture correction discovered by the initial race run.

Standards and Spec reviews each reported zero findings. RAS run `20260911T061825-1ad174bba1879dfc67636fb7` completed both configured reviewers and synthesis with zero findings; no fix-verification, replacement review, or deferred follow-up was required. The PR description retains review and certification details.

---

## Maintained-code conventions documented - 2026-09-11 02:49 EDT

**Main:** `ce3f1a785119`
**Actor:** Codex

### Completed

Merged [PR #206](https://github.com/the-sarge/quic-go-fast/pull/206), closing [issue #72](https://github.com/the-sarge/quic-go-fast/issues/72). Added the maintained-code and test-helper conventions guide and its AGENTS.md pointer. The guide distinguishes existing enforcement from observed practices and package differences, preserves API/module/emission and real-packer contracts, and links the archived survey while excluding frozen evidence from maintenance sweeps. Only Markdown changed.

### Validation

At reviewed head `47fbb943db6b966c6e68f5c1b729471d153e53fc`, relative documentation links and diff checks passed, as did one uncached full native Go test suite, `go vet ./...` and `go mod tidy -diff`. Independent Standards and Spec reviews found no issues. All 33 applicable hosted checks succeeded before the guarded squash merge.

RAS run `20260911T064100-6fffc7303bbb80f95d6037bd` used the full configured eight-reviewer default panel and completed with zero findings or required fixes. Seven reviewers succeeded; `cursor-kimik3` failed structured-output parsing. Configured quorum was met and synthesis completed; no verification or replacement review was needed. An earlier narrowed attempt failed quorum, and its narrowed retry was interrupted after the operator required preserving configured defaults without prior permission. No RAS review was posted to GitHub.

---

## Core packet and transport assertions made observable - 2026-09-11 03:32 EDT

**Main:** `7b1764833536`
**Actor:** Codex

### Completed

Merged [PR #209](https://github.com/the-sarge/quic-go-fast/pull/209) as `7b1764833536f695b02bf605887912ee9747eb44`, closing [issue #73](https://github.com/the-sarge/quic-go-fast/issues/73). The three GSO outcome tests share narrow setup, payload and decoding mechanics while retaining real packing/recovery, scenario-local segment-size/ECN assertions and explicit teardown. Randomized MTU tests now assert reachable final updates, and deterministic zero/two-loss cases check completion and size bounds. Regular and early transport dialing must return success before teardown. Only tests changed.

### Validation

Affected tests, focused race tests, the full native suite, vet, lint and formatting/diff checks passed. Temporary Go overlays confirmed that suppressed MTU completion flags fail both deterministic cases and hidden dial readiness fails both dial variants. Final local certification passed at `5047ad3dfd9598cb4ab0cacd82c22ad18faa845c`.

Standards and Spec reviews each returned zero findings. Initial RAS run `20260911T070828-f3ef910cfdf2ec9f4653b95e` used the configured eight-reviewer panel: seven succeeded, while `cursor-kimik3` returned no supported JSON candidate; quorum, adjudication and synthesis completed. The accepted diagnostic-attribution finding was fixed by removing two newly added `t.Helper()` markers from full MTU scenario bodies, and source verification resolved it and its duplicate at the final head. The proposed wider deterministic MTU bound was rejected as outside the fixed example-level contract. Replacement run `20260911T072127-d872cc9bf1b3e538a85930d2` used `codex-astra` and `codex-sol` and completed with zero findings. No deferred follow-ups remain; no RAS review was posted to GitHub.

Every job in all five final-head PR workflows succeeded: [unit](https://github.com/the-sarge/quic-go-fast/actions/runs/34573853581), [integration](https://github.com/the-sarge/quic-go-fast/actions/runs/34573853558), [lint](https://github.com/the-sarge/quic-go-fast/actions/runs/34573853590), [cross-compilation](https://github.com/the-sarge/quic-go-fast/actions/runs/34573853565) and [interop image](https://github.com/the-sarge/quic-go-fast/actions/runs/34573853559). Interop attempt 1 failed during Ubuntu package downloads with connection refusal/timeouts; unchanged-head attempt 2 passed. This repository uses its existing workflows rather than a `ci-*` gate. The squash merge was guarded by the reviewed head SHA.

---

## HTTP/3 tests observe priority and asynchronous completion - 2026-09-11 03:49 EDT

**Main:** `45b9fa840fc5`
**Actor:** Codex

### Completed

Merged [PR #211](https://github.com/the-sarge/quic-go-fast/pull/211) as `45b9fa840fc53dfa53efe475e36214ba585b3a9a`, closing [issue #83](https://github.com/the-sarge/quic-go-fast/issues/83). Request-priority tests now send real requests through `RawServerConn.HandleRequestStream` and observe the transport's applied-priority events, covering connection defaults, priority-aware transitions, malformed headers and multiple field lines. Datagram tests wait for parsing and a delivery barrier before registering the missing stream. Alt-Svc tests wait for protected listener registration and close/join serving workers during cleanup. Only tests changed; parser tables, valid virtual-time sleeps and production/API/module/emission contracts remain intact.

### Validation

Removing the production priority application call caused all seven replacement scenarios to fail; the temporary mutation was restored before committing. Focused tests and their race check, one uncached full native suite including self integration, lint, vet, module tidiness, compiler assertions, generation and diff checks passed. Certification covered reviewed head `3b74c07d65527206aa78994f29c4c261818d2bbd` against `bad8cd6ebf36745222571f54fc6a7cff90a590ed`, with a clean worktree. All 33 hosted checks succeeded without reruns before the guarded squash merge.

Independent Standards and Spec reviews each returned zero findings. RAS run `20260911T073947-5b96cbaa1a225c17cab9439c` used the configured `codex-astra` and `codex-sol` reviewers and completed with zero findings or follow-ups. No fix verification or replacement review was required, and no RAS review was posted to GitHub. Review and certification details are retained in the PR description.

---

## Fork automation validates staged inputs safely - 2026-09-11 04:17 EDT

**Main:** `d1b4292807ca`
**Actor:** Codex

### Completed

Merged [PR #213](https://github.com/the-sarge/quic-go-fast/pull/213) as `d1b4292807cac595eb289b417c326f172663e5a6`, closing [issue #84](https://github.com/the-sarge/quic-go-fast/issues/84). Benchmark and interop push filters and cross-compile cache saving now target the verified fork default branch, `main`. Interop validation remains build-only, with publication disabled and upstream registry login/namespace references removed. The optional hook validates an isolated checkout of the index, checks tool exits explicitly, uses staged Go content and safe filenames, and runs non-mutating vendor-module validation. Git index modes enforce rejection of staged Go symlinks. Documentation follows the accepted standard-test and helper conventions.

### Validation

The hook fixtures passed on macOS and in a Linux container. Red/green regressions covered an empty-output formatter failure, staged content with unusual filenames, and symlink materialization with `core.symlinks=false`. A hosted Linux fixture failure was reproduced and corrected by using literal Git pathspecs during fixture staging. The final-head full native suite, hook suite, vet, Bash syntax/shellcheck, formatting and diff checks passed; workflow syntax checks excluded existing shellcheck warnings on unchanged lines and the known CodSpeed runner label. A real-tool hook invocation passed without changing caller files or staging.

Standards and Spec reviews each found zero issues. Initial RAS run `20260911T075907-fc6eb5d3bf13f05b53a56fbb` used `codex-astra` and `codex-sol`; its three findings were independently accepted and fixed. Exact-head verification resolved all three. Replacement run `20260911T081036-e2f8543b5ed7bab0663888bf` used the same reviewers and returned zero findings. No deferred findings remain and no RAS review was posted. All 33 hosted checks succeeded on reviewed head `6fbb1f33381767a28ad3b6de86cd7d7234fe37d9` before the guarded squash merge. Superseded CI runs were cancelled; the final-head checks required no reruns.

---

## HTTP/0.9 malformed URL rejection landed - 2026-09-11 10:18 EDT

**Main:** `9bfeb1c5c23c`
**Actor:** Codex

Merged [PR #215](https://github.com/the-sarge/quic-go-fast/pull/215), closing [issue #196](https://github.com/the-sarge/quic-go-fast/issues/196). Completed HTTP/0.9 requests whose URL fails parsing now reset the response stream with application error code 42, consistent with invalid-prefix rejection, while preserving the parse error for logging. The real-QUIC validation regression checks an empty remote-reset response, excludes handler invocation, and exercises subsequent valid requests on the same connection.

Validation: the regression failed before the fix with a deadline error and passed afterward. The HTTP/0.9 package, focused race regression, uncached full local suite, repository-wide vet, module tidiness, compiler assertions, package lint and diff checks passed. Independent Standards and Spec reviews had zero findings. RAS run `20260911T140851-853d8884e373d61fd223d6cd` completed with two successful reviewers and zero findings; no fix verification or replacement review was needed. All 33 applicable hosted checks passed on reviewed head `6c1b56c23864d7eba2f580c4e9054c15d28a66e8` before guarded squash merge to `9bfeb1c5c23c483ad4e9ce91c4fdefd708c4adb8`. The non-required CodSpeed job remained queued without a runner; it benchmarks `integrationtests/self`, outside this harness change and its performance obligations. No deferred findings.

---

## Archived burst collector cleanup repaired - 2026-09-11 10:50 EDT

**Main:** `8060887ce6bb`
**Actor:** Codex

Merged [PR #217](https://github.com/the-sarge/quic-go-fast/pull/217) as `8060887ce6bbb59dae9e9aecde73366511492a04`, closing [issue #18](https://github.com/the-sarge/quic-go-fast/issues/18). The retained D2 burst collector now owns one cleanup attempt after successful setup, continues bounded best-effort teardown for every tracked process, defers handled signals across cleanup ownership and preserves initiating, teardown, restoration and late-interruption diagnostics. A failed cleanup never produces a restoration receipt. The [reuse contract](audits/d2-burst/collector-reuse.md) retains the recorded host and experiment scope and requires separate authorization for new collection. Historical samples, hashes, profiles, receipts and conclusions remain unchanged; this work performed no live isolation or collection.

Validation: 19 fully mocked lifecycle tests pass, including red/green regressions for initial receipt failure, receiver teardown timeout, all three handled signals at setup transition and combined initiating/finalization failures. Python compilation, the full uncached native Go suite, repository-wide vet and diff checks passed on certified head `1743072a9793f4443d346ecf104969e7f7460def` against base `60e2eb4b748844ba8a105ce525488c0ab3aba62d`. All 33 applicable hosted checks passed on that head before guarded squash merge. The non-required CodSpeed job remained queued without a runner; it was not counted as passing evidence and performance measurement is outside this repair.

Independent Standards and Spec reviews found no issues in the initial implementation and signal amendment. Initial RAS run `20260911T142648-9073cbc897cff366da6ca001` found the post-setup signal gap; exact-head verification confirmed its fix. Replacement run `20260911T143810-63e5813288b57c8c144b37f8` found omission of a later interruption after an initiating error. That small diagnostic correction was accepted within the existing contract and verified resolved on the certified head, without a third fresh review. The two findings have distinct enforcement owners: asynchronous cleanup-ownership protection and final diagnostic aggregation. Both RAS rounds used `codex-astra` and `codex-sol`. No unresolved or deferred findings remain; the PR description retains dispositions and certification history.

---

## HTTP/3 shutdown deadline fixture repaired - 2026-09-11 11:13 EDT

**Main:** `fefe59fecae8`
**Actor:** Codex

Merged [PR #219](https://github.com/the-sarge/quic-go-fast/pull/219) as `fefe59fecae8a2ffc1583537d1e21071611b6c85`, closing [issue #165](https://github.com/the-sarge/quic-go-fast/issues/165). The long-lived HTTP/3 shutdown fixture now establishes an unfinished exchange before starting the shutdown deadline, records terminal observations in client/shutdown workers and the handler, preserves `context.DeadlineExceeded` and `H3_NO_ERROR`, and bounds observation and cleanup. It covers zero setup delay and one controlled delayed dial. Production code is unchanged; the original intermittent CI failure's cause remains unproven. The [evidence record](audits/issue-165-shutdown-fixture/evidence.md) retains the contract and finite diagnostic scope.

Validation: a controlled setup delay failed the original duration assertion while the expected shutdown, client error and handler timing assertions passed. The repaired cases pass. Two removed fault probes detected immediate closure and omitted deadline closure with bounded cleanup. Neighboring shutdown coverage passed with timing scale 3 for QUIC v1 and v2 on Go 1.27.0, darwin/arm64. The focused race test, uncached full root-module suite, repository-wide vet/lint, module tidiness, changed-file formatting and diff checks passed on reviewed head `7b88a1198d370024524fc56e56d50f06da58c584`. Broad formatter inspection found only an existing blank line in a frozen, unchanged experiment artifact, which was preserved under repository conventions.

Independent Standards and Spec reviews found zero issues. RAS run `20260911T150605-82ac348e42b7d247746ec0e4` completed with two successful reviewers (`codex-astra` and `codex-sol`) and zero findings; no verification, replacement review or review posting was needed. All 33 applicable hosted checks passed unskipped on the exact reviewed head against unchanged base `420d6bad9b65f7b5a3645e27b77d783180b357cd` before guarded squash merge. The optional CodSpeed benchmark remained queued without an assigned runner; it is unavailable, not passing evidence, and performance measurement is outside this fixture repair. No deferred findings remain.

---

## Blocked-data fixture delivery synchronization - 2026-09-11 11:51 EDT

**Main:** `091daab635d3`
**Actor:** Codex

Merged [PR #221](https://github.com/the-sarge/quic-go-fast/pull/221), closing [issue #198](https://github.com/the-sarge/quic-go-fast/issues/198). The shared connection/stream blocked-data fixture now reads the exact accepted batch and explicitly waits for auto-tuned receive credit before starting subsequent write deadlines. The 100/200/400-byte batches, deadline behavior, offsets 100/300/700, frame counts, and bundling assertions remain intact; production APIs and wire behavior are unchanged.

A controlled final-batch forwarding delay reproduced accepted400/read0 in both variants on the base plus regression. The repaired regression passes under QUIC v1/v2. This establishes the fixture’s invalid delivery-deadline assumption without claiming the exact historical scheduling event was reproduced. On reviewed head `e0a87ed87c7d0ba8701778e9a0febf28bbf18c3e`, both original tests passed 1,000 unscaled repetitions each under both QUIC versions; the unscaled full suite, focused flow-control tests and vet passed. All 33 applicable hosted checks succeeded; hosted integration uses factor 3. Optional CodSpeed remained queued at merge.

Standards and Spec reviews found no issues. RAS run `20260911T153332-3899166c8c34f1ee0095e7c5` completed with eight reviewers, full adjudication and no immediate fixes; no fix verification or replacement review was required. [The review receipt](https://github.com/the-sarge/quic-go-fast/pull/221#issuecomment-5637026414) records dispositions and certification. [Issue #222](https://github.com/the-sarge/quic-go-fast/issues/222) retains the source-revalidated, out-of-scope factor-10/20 PTO/duplicate-frame limitation and pre-existing excess-frame diagnostic panic for separate triage.

---

## Coalesced-storage contract landed - 2026-09-11 14:48 EDT

**Main:** `f9bbf13d791a`
**Actor:** Claude

Merged [PR #236](https://github.com/the-sarge/quic-go-fast/pull/236) as `f9bbf13d791a`, closing [issue #228](https://github.com/the-sarge/quic-go-fast/issues/228) — Slice G1 of the [Linux GRO plan](adr/2026-09-11-linux-gro-plan.md), Track G of datapath offload program QGF-DP-2026-09. The platform-neutral coalesced-storage contract landed behaviorally inert: an atomically reference-counted `coalescedSlab` confined to the segments of one coalesced read under the 2026-09-11 amendment to ADR 0005; a third 65535-byte buffer-pool tier; a split helper yielding per-datagram `packetBuffer`-compatible views that keep the ordinary non-atomic refCount; retention-queue copy logic wired dormant into the undecryptable and unprocessed queues, with oversized views charged at full slab size against the new per-connection `MaxConnRetainedCoalescedBytes` budget (32 slabs ≈ 2 MiB) and released with the view; and shared coalesced-read fixtures for the G2/W3 producer slices. No runtime path can create a slab view until a producer exists; the nil-hook release path preserves existing behavior exactly.

Validation: closure classes 1–5 pass under the race detector — sequential and concurrent exactly-once slab recycling, retention-copy independence with slab-count decrement, oversized-retention budget charge/eviction release and exhaustion refusal, and the preserved double-release panic. The guard mutation (deleting the recycle condition in `coalescedSlab.decrement`) failed classes 1–2 and was restored. Dormant queue wiring is evidenced against an unrun connection for both queues. The full suite, full race suite, and golangci-lint passed on certified head `1672ba6eb16f`; 33 applicable hosted checks passed on that exact head before guarded squash merge. The non-required CodSpeed benchmark remained queued without a runner (the fork has no `codspeed-macro` runner registered) and was not counted as passing evidence.

RAS run `20260911T180348-98726c836eb85af49a85454d` completed with zero findings from six reviewers (`codex-astra`, `codex-sol` twice, `agy-flash`, `cursor-kimik3`, `grok`); three Claude-family reviewer adapters failed at process level and contributed nothing to quorum. No fix, verification, or replacement review was required, and no deferred findings remain. The PR's own docs commit marks G1 complete and advances the Track G frontier to G2 in the [program index](adr/2026-09-11-datapath-offload-program.md); W3's cross-track G1 edge is satisfied.

Next: G2 ([issue #231](https://github.com/the-sarge/quic-go-fast/issues/231), Linux UDP_GRO receive end to end) is dispatchable; W1 and D1 remain frontier. The [program index](adr/2026-09-11-datapath-offload-program.md) is the live frontier view.

---

## Linux UDP_GRO receive landed; Track G complete - 2026-09-11 17:03 EDT

**Main:** `14b8832b600c`
**Actor:** Claude

Merged [PR #239](https://github.com/the-sarge/quic-go-fast/pull/239) as `14b8832b600c`, closing [issue #231](https://github.com/the-sarge/quic-go-fast/issues/231) — Slice G2 of the [Linux GRO plan](adr/2026-09-11-linux-gro-plan.md), completing Track G of datapath offload program QGF-DP-2026-09. Linux coalesced receive is live: UDP_GRO is enabled by a setsockopt probe on transport-owned sockets only (`wrapConn`/`newConn` gained a socket-ownership parameter; the negative criterion — no `UDP_GRO` setsockopt on caller-supplied sockets — is unit-asserted), gated on kernel ≥ 5 and the `QUIC_GO_DISABLE_GRO` kill switch. A GRO socket preposts 8 × 64 KiB coalesced-tier buffers; every non-empty read is wrapped in G1's `coalescedSlab` and split per the `UDP_GRO` segment-size control message (single datagrams included, keeping retention copies uniform), with sibling views inheriting the read's address, receive time, ECN, and packet info, returned one per `ReadPacket()` call. Activating the producer made every `receivedPacket` hold past the routing pass a potential slab pin, so the retention rule was extended at its single owner `retainForRetentionQueue` to the server receive, version-negotiation, and 0-RTT queues and the transport's stateless-reset and non-QUIC queues, each owner with its own oversized-view retained-bytes budget.

Adoption evidence (protocol precommitted at `7306e2c6`, [results](audits/2026-09-11-g2-gro-results.md), disposition pass): on minimax, engaged-cell engagement ~100% (11–14 segments per kernel message), receive syscalls per delivered datagram ratio 0.263 [0.2595, 0.2663] against the 0.75 gate, throughput 1.259× [1.245, 1.272] against the 0.95 noninferiority gate, peak-RSS delta 1.7 MiB inside the 16 MiB budget; recorded deviations cover three unmeasured memory dimensions and one failed (mis-specified) predeclared context check with a labeled post-collection diagnostic. Closure classes — mixed-CID concurrent release, Initial-path sibling with the short-header-remainder rule, unknown-CID/stateless-reset, rejected sibling beside a budget-charged view, queue overflow with a sibling in flight, short tail — pass under the race detector; the guard mutation (skipping the retention copy) failed ten tests and was restored.

Review: initial RAS run `20260911T195639-f10c4a69140cb4d161356236` (nine substantive clusters); fix-now items landed at `8b446108` (cross-owner oversized-charge transfer with destination-reserved-then-source-refunded semantics; cause-accurate server drop diagnostics; capability-injected synthetic tests; evidence-record deviations), C-011's untraced retention-copy amplification closed by a scoped re-audit that traced and bounded it in the plan's G2 blast radius (receipt and disposition table in PR discussion), and exact-head verification reported the blocking projection clear. The replacement review `20260911T203047-d4d29902630235f78b84e300` returned zero fix-first/follow-up findings; its one non-blocking provenance note was fixed docs-only under policy. Local certification (full suite, race suite, focused closure gates, golangci-lint on Darwin and Linux) and all 33 hosted checks passed on merged head `c246cd8b` (one `go fix` gate failure was satisfied by the mandated mechanical `wg.Go` edit). No deferred findings survive to follow-ups.

The PR's docs commits mark G2 complete and Track G done in the [program index](adr/2026-09-11-datapath-offload-program.md); the frontier is W1 and D1 (parallel-safe). Next: W1 ([#229](https://github.com/the-sarge/quic-go-fast/issues/229), Windows message-I/O foundation) and D1 ([#230](https://github.com/the-sarge/quic-go-fast/issues/230), macOS sendmsg_x batch send) remain dispatchable; the program index is the live frontier view.

---

## Windows message-I/O foundation landed (W1) - 2026-09-11 22:17 EDT

**Main:** `fc97e4639260`
**Actor:** Claude

Merged [PR #242](https://github.com/the-sarge/quic-go-fast/pull/242) as `fc97e463`, closing [issue #229](https://github.com/the-sarge/quic-go-fast/issues/229) — Slice W1 of the [Windows datapath plan](adr/2026-09-11-windows-datapath-plan.md), Track W of datapath offload program QGF-DP-2026-09. Windows moves off the featureless plain-socket path: `windowsConn` replaces `basicConn` on Windows, reading and writing through `net.UDPConn.ReadMsgUDP`/`WriteMsgUDP` (`WSARecvMsg`/`WSASendMsg` on the runtime IOCP poller) and owning Windows control-message encoding and parsing. Sockets bound to an unspecified address request `IP_PKTINFO`/`IPV6_PKTINFO` and gain packet-info parity with the OOB platforms; replies encode the control-message family the socket actually speaks (dual-stack IPv4 traffic stays v4-mapped at the `IPPROTO_IPV6` level while `packetInfo.addr` stays unmapped), control buffers are padded to `WSA_CMSG_SPACE` (Windows CI caught the unpadded-IPv6 `WSAEINVAL` the aligned IPv4 message masked), and `WritePacket` preserves `basicConn`'s error behavior for nil or non-UDP destinations instead of panicking. Capabilities are unchanged — no GSO/ECN/GRO; those are W2/W3 — and the caller-supplied non-OOB `basicConn` fallback is preserved on every platform.

Validation: the plan's deadline/close closure classes — blocked read observing a preset deadline, a deadline set mid-block, and close; concurrent close against racing reads; sustained transfer through the poller; write after close — run platform-neutrally against both the platform conn and the `basicConn` fallback across the whole CI matrix, Windows included, and were written first as characterization. Windows-tagged tests cover packet-info parity end to end (IPv4, IPv6, dual-stack with IPv4 client, each including the reply path), the encode/parse round trip, a hand-built `ws2def.h` layout literal, malformed-control-buffer fallback, and invalid-destination errors. Local certification (full suite, race, windows/amd64+386+arm64 builds and vets, lint on both GOOS) and all hosted checks passed on certified head `57330ac8`, squash-merged with `--match-head-commit`. One pre-existing macOS integration flake (`TestPacketReordering`, `TestConnectionMigration`) failed one of two same-head runs, was diagnosed as untouched by this Windows-only diff, passed on rerun, and is tracked separately.

Adoption evidence ([protocol](audits/2026-09-11-w1-foundation-protocol.md) precommitted at `ec34df32`, amended to v2 at `206ac1e9`; [results](audits/2026-09-11-w1-foundation-results.md), disposition Pass): with no dedicated Windows host, the qualified surface is the GitHub-hosted `windows-latest` runner with a paired same-round design. The official v2 collection (run 34665708932) measured the swap family — both datapaths doing identical work — at throughput ratio 0.9894, 95 % CI [0.9798, 0.9996] against the 0.95 noninferiority gate, and the packet-info-active parity family at 0.9538, CI [0.9472, 0.9617] against the 0.85 stop floor, memory within budget. The v1 wildcard-only collection (0.932, mechanically inconclusive) and the decomposing diagnostic (swap-only 0.9859) are published as history.

Decisions: the v1 inconclusive result triggered a scoped re-audit ([receipt](https://github.com/the-sarge/quic-go-fast/pull/242#issuecomment-5642620643)) splitting the evidence contract into the gated swap family and the reported parity family; exact-head verification flagged the post-result amendment as weakening the production-path gate, and the loop stopped for decision. The operator explicitly accepted the amended gate — packet-info parity's ~5 % bulk-loopback cost on wildcard-bound sockets (the OOB platforms' per-packet cost model) is reported against the stop floor, not gated at 0.95 — recorded in the plan's W1 gate sentence and PR discussion.

Review: initial RAS run `20260911T214307-2294e25e077baf87194085d1` at `af4fdf6e` produced four accepted fix-now clusters (results-before-merge gate; throughput unit; measurement bypassing the packet-info path; `WritePacket` panic on invalid destinations), fixed at `412c383e` with the results artifact completing the set; one cluster was rejected as unsupported. Exact-head verification at `9bd7c181` resolved every accepted cluster. The replacement review `20260912T015952-bff70151aef2a4c7009d9499` returned zero fix-first and zero follow-up clusters. One deferred finding survived revalidation against the merged head and is filed as [issue #243](https://github.com/the-sarge/quic-go-fast/issues/243) (stale Windows skip in `TestSendConnOOB`).

Next: the PR's docs commits mark W1 complete and advance the frontier to W2 ([#232](https://github.com/the-sarge/quic-go-fast/issues/232), segmented send/USO) and W3 ([#233](https://github.com/the-sarge/quic-go-fast/issues/233), coalesced receive/URO — its G1 edge was already satisfied), which share Windows conn files and serialize in worktrees; D1 ([#230](https://github.com/the-sarge/quic-go-fast/issues/230)) remains frontier and parallel-safe. The [program index](adr/2026-09-11-datapath-offload-program.md) is the live frontier view.

---

## Windows segmented send landed (W2) - 2026-09-12 00:02 EDT

**Main:** `69d70161222b`
**Actor:** Claude

Merged [PR #245](https://github.com/the-sarge/quic-go-fast/pull/245) as `69d70161`, closing [issue #232](https://github.com/the-sarge/quic-go-fast/issues/232) — Slice W2 of the [Windows datapath plan](adr/2026-09-11-windows-datapath-plan.md), Track W of datapath offload program QGF-DP-2026-09. Windows transport-owned sockets gain segmented send (USO): `isUSOEnabled` probes `UDP_SEND_MSG_SIZE` with a mutation-free getsockopt (mirroring the Linux `UDP_SEGMENT` probe) on transport-owned sockets only and honors the existing `QUIC_GO_DISABLE_GSO` kill switch; `newConn` wires the result into the platform-neutral GSO capability flag, so the emission path batches exactly as Linux GSO does with no batching-policy changes; and `windowsConn.WritePacket` encodes the per-send segment-size control message (a DWORD, unlike Linux's uint16) through W1's `appendCmsg` encoder, replacing the W1 `gsoSize` panic. Caller-supplied sockets are never probed and keep the W1 foundation behavior. One platform quirk surfaced by TDD: Winsock reports zero transferred bytes on send completions carrying `UDP_SEND_MSG_SIZE`, for multi-segment and single-packet submissions alike; since a UDP send is all-or-nothing, a successful segmented send is normalized to the full buffer length to preserve the `rawConn` byte-count contract. With W2 first to land, the four-combination USO/URO interaction protocol is now W3's obligation as the W track's second lander.

Validation: closure classes on Windows CI — full acceptance (segmented submission delivered as per-segment datagrams with remainder tail, plus the single-packet-below-segment-size case), end-to-end `WSAEMSGSIZE` classification through the message-I/O path, deterministic probe-failure branches (Control error and getsockopt error via a fake `syscall.RawConn`), kill-switch and caller-supplied fallbacks — plus an encoding test against a hand-built `ws2def.h` layout literal and an append-after-packet-info ordering check. Host posture: on hosted CI the positive capability assertion is a strict tripwire (a runner that cannot exercise the USO closure classes fails visibly rather than certifying with them silently skipped); off CI, probe-false hosts skip loudly. The closure guard mutation ran on hosted Windows: `isSendMsgSizeErr` forced false failed `TestSendQueueHandshakeMTUEligibility`, then was reverted. Local certification (gofmt, vet on host and windows including both bench tags, full local suite) and all 33 hosted checks passed on head `75386c03`, squash-merged with `--match-head-commit`. The pre-existing nondeterministic `TestPathMTUDiscovery` (ubuntu, QUIC v2) failed one push-event run on the final head while the identical pull_request-event job on the same head passed; it was diagnosed as uncorrelated with this Windows-build-tagged diff, passed on one diagnosed rerun, and the observation is recorded on [issue #178](https://github.com/the-sarge/quic-go-fast/issues/178).

Adoption evidence ([protocol](audits/2026-09-11-w2-uso-protocol.md) precommitted at `e8852e35`; [results](audits/2026-09-11-w2-uso-results.md), disposition Pass): official collection run 34669428064 on the qualified `windows-latest` surface (build 10.0.26100, 4 vCPUs, go1.27.1, amd64), eleven rounds of four paired same-round cells with round 0 discarded. Primary — server send submissions per transmitted packet, engaged vs disabled — geometric mean 0.0815, 95 % CI [0.0813, 0.0817] against the ≤ 0.50 gate: a median 12.27 packets per submission, with the batch distribution dominated by 14- and 11-packet submissions. Engagement 99.96 % of packets in multi-packet submissions; the paced available-but-unexercised cell reported exactly 1.0 packets per submission in every round. Throughput improved 3.01× [2.93, 3.10] against the 0.95 noninferiority gate (~407 vs ~134 MB/s on loopback); memory within budget; the unavailable cell tracked disabled within 10 %; no contamination. Two earlier workflow invocations failed in the guard-mutation step's mechanics after their collection steps; their data was never inspected, and the official run was declared before its data was seen. The measured candidate is `1af918b1`; later PR commits are review-driven test additions and documents only, leaving shipped code and harness byte-identical to the merge.

Review: initial RAS run `20260912T031234-2ec84310316f70aff077545c` at `c292a89a` produced one accepted fix-now cluster — the probe's failure branches were untested — fixed at `41236d59` with deterministic branch tests; exact-head verifications at `41236d59` and `c8376e79` reported the blocking projection clear. The verifier's suggestion to skip the capability assertion on operator-overridden CI runners was rejected with recorded rationale (a CI runner unable to run the USO closure classes must fail visibly, not certify green-but-vacuous), and the strict-CI/graceful-dev posture helper landed at `c8376e79`. The replacement review `20260912T034022-e4d56f3aab3aea2e5abb2203` returned zero fix-first clusters; its provenance note was fixed docs-only under policy at `75386c03`. Rejected findings: layout-literal constant sharing (the imported x/sys constant is authoritative), a combined packet-info+USO fixture cross-product (outside the accepted evidence budget), and a `Transport.Conn` comment claim (unsupported). One deferred finding survived revalidation against the merged head and is filed as [issue #246](https://github.com/the-sarge/quic-go-fast/issues/246) (pin the ambient kill switch off inside `TestWindowsUSOProbeFailure` and assert the probe reaches `Control`); the duplicate `SyscallConn` acquisition in `newConn` was left as optional cleanup for W3's probe work.

Next: the PR's docs commits mark W2 complete in the [program index](adr/2026-09-11-datapath-offload-program.md); the frontier is W3 ([#233](https://github.com/the-sarge/quic-go-fast/issues/233), Windows coalesced receive/URO — dispatchable, all edges satisfied) and D1 ([#230](https://github.com/the-sarge/quic-go-fast/issues/230), macOS sendmsg_x batch send), which touch disjoint files and are parallel-safe. The program index is the live frontier view.
