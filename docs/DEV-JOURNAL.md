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
