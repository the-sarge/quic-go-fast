# Native writer-yield applicability handoff

**Scope:** the owner's 2026-11-12 request authorizes only native contention and shutdown cases exercising the delivered writer yield, with the existing Linux venue, Go versions and acceptance bounds. The [R21 contract](../adr/2026-10-24-multipath-reno-plan.md#r21) is normative; this audit records source provenance, carried evidence and slice decisions. [R19 #1039](https://github.com/the-sarge/quic-go-fast/issues/1039) remains the separate activation slice. No new native campaign ran during this handoff.

## Source and ownership audit

The reviewed default-branch source is `08c6cf28420e78c75f69c7a7ee8268be615a3f88`, following [R18 product PR #1310](https://github.com/the-sarge/quic-go-fast/pull/1310) and [journal PR #1311](https://github.com/the-sarge/quic-go-fast/pull/1311). The source in the R18 measured candidate `56e011367705ce3adb31a70ce23f4b5ff29b5521` differs from this default-branch source only in documentation. The [R19 preflight receipt (#1039)](https://github.com/the-sarge/quic-go-fast/issues/1039#issuecomment-6838050370) identified the native applicability gap. No open R19 implementation PR was found. The separate deadline-adapter [PR #1295](https://github.com/the-sarge/quic-go-fast/pull/1295) is unmerged and supplies no baseline or prerequisite.

At this source, `packet_emission_software.go:27` records short software grouping, and `native_ordinary.go:377` transfers that mark only for software groups with more than one datagram. `native_software_segmentation_test.go:49–105` checks the actual producer and frozen writer mark. `native_endpoint.go:1388–1398` computes the Linux, successful-result, marked-group and timely-whole-borrow predicate before publishing completion; it notifies progress and yields only after publication, outside the endpoint lock. No token payload is accessed after publication.

`native_endpoint.go:997` retains independent queued expiry; `:1112` reconciles immutable completion and owns publication/release; `:1068` retains the terminal control path, with `:1081` waiting for writer exit before returning authority. The request-turn policy at `:1143` is unchanged. These facts preserve the ownership argument but do not prove elapsed scheduling bounds after a yield. `native_transport.go:97` joins under the caller's context; the native fixture's existing less-than-120-ms assertion is operational evidence, not a newly promised public API SLA.

## Native evidence applicability

The latest accepted native package precedes R18's yield. Its source is `4e115954e7e54e747a9f3292d1561c6447c299c8`; [PR #1306](https://github.com/the-sarge/quic-go-fast/pull/1306) delivered the identical tree as squash `1c1f48b846ee8e64369ae80817d2900ffb314b62`. Their shared tree is `b83b8be90b17567884f1ee816a21e1b93660d2ad`. The [native terminal receipt for repair #1305](https://github.com/the-sarge/quic-go-fast/issues/1305#issuecomment-6828604001) retains execution provenance.

The raw archive is `/Volumes/worktrees/r18-progress-delivery-native/evidence/native-package-v1/native-results.tar`, SHA-256 `c98bddda2ee6ab6d00a945b43d973b4eb28f83c458d80c54ab7126a425046248`. Independent read-only inspection verified this checksum and all twelve original case records. Root verification is `/Volumes/worktrees/r18-tradeoff-20261110/evidence/product-1305-native-terminal-root-verification.json`, SHA-256 `9a15489202f8a01be8b3e3128cf72b7aefe349fd6020df30e1df587b608d71d1`. The Go 1.26.11 binary hash is `d516b8488d0cdc5fbeaf5a9e8ba100bd60a1761da81c987add34335bb1891764`; the Go 1.27.4 binary hash is `09f4593131a332886a341a57f70ed51690f74c70971257c5591a05fa006d0747`. Carried observations retain these historical identities; they are not new-head executions.

The accepted venue is `minimax`, Linux amd64, Ubuntu 26.04.1 LTS, kernel `7.0.0-30-generic`, `#30-Ubuntu SMP PREEMPT_DYNAMIC Fri Jul 31 18:22:54 UTC 2026`. The native package records affinity `0–31` and no GOMAXPROCS, GOGC or GOMEMLIMIT override. Preserve that venue and the existing custody/contamination policy. The older E6 receipt's Go patch versions are historical; this scoped supplement uses the already adopted **Go 1.26.11 and 1.27.4** without platform expansion.

The native fixture and driver, `native_transport.go` and `native_response.go` are unchanged between that package source and the reviewed default branch. The native core changes are mark propagation and the success-only post-publication notify/yield. The following disposition closes applicability at the precise changed predicate:

| Existing evidence | Disposition | Why |
| --- | --- | --- |
| `expired-before-dispatch`, both Go versions | Carry eight unaffected observations in total, including these two | `native_qualification_test.go:313–342`: expiry occurs before native entry; the successor is an unmarked scalar request |
| `eagain-to-deadline`, both Go versions | Carry | `:344–363`: the native error excludes the yield predicate; error/deadline behavior is unchanged |
| `retry-fallback`, both Go versions | Carry | `:365–402`: the retry/fallback error path excludes the successful marked-group predicate; preserve simulated-trigger and injected-traversal qualifications |
| `queued-expiry`, both Go versions | Carry | `:404–430`: control expires queued work while the current unmarked request is in the poller; independent expiry owner is unchanged |
| `request-contention`, both Go versions | Retain as historical unmarked-workload evidence; supplement in R21 | `:82–88`, `:226` and `:256` construct unmarked singleton requests; historical contention does not reach the new yield |
| `shutdown-join`, both Go versions | Retain as historical drained STREAM/public-close evidence; supplement in R21 | `:432–493` closes after STREAM/public work, without successful marked-group activity |

The original contention receipts report public writes 53/53 and stateless admissions 53/53 on Go 1.26.11, and public writes 48/48 and stateless admissions 49/49 on Go 1.27.4. Publication maxima were 1.086977 ms and 0.685925 ms; pressure-publication maxima were 1.291881 ms and 1.781549 ms. Both pressure phases observed zero sampled native poll waits. Preserve that absence as an observation limitation, not evidence of actual kernel pressure traversal. These historical numbers are context, not substitute results for R21.

The old shutdown client/server joins were 0.087865/0.053881 ms on Go 1.26.11 and 0.074760/0.046668 ms on Go 1.27.4. The new cases retain the same B = 100 ms, 20 ms tolerance, per-field timing checks and join less than 120 ms. Join values belong in `facts`, where the Go fixture checks the 120 ms bound; the Python driver applies 20 ms to every `timing_ms` entry. Moving join values into that map would silently tighten acceptance and is excluded.

## Other carried evidence and limits

The [R18 delivery receipt (#1038)](2026-11-12-multipath-r18-delivery-receipt.md), its raw summary and owner disposition retain all 36 observations. The measured source's non-documentation content equals the delivered source. Four cells pass numerically; DATAGRAM/batching-on CPU 1.121686× and mixed-bulk CPU 1.108585× remain the two exact accepted exceptions, with strict numerical failure against 1.10 preserved. The CPU 1.10 and RSS 10% limits are unchanged. No performance cell is repeated or reclassified by this handoff.

The ordinary campaign's native close records use a successful `joined` boolean under a five-second context after workload drain. They support that campaign's close check, but cannot substitute for native shutdown timing during marked work. Likewise its mixed/stateless cells do not supply the native endpoint's new yield-contention timing case.

[R17 #1037](https://github.com/the-sarge/quic-go-fast/issues/1037) remains complete. The [independent-peer run for PR #1310](https://github.com/the-sarge/quic-go-fast/actions/runs/43319049861) succeeded on head `bcc0e527d30f93b9ce0d47ef44911635f1c8b503`, whose tree equals squash `fecc67b89f140d5f5481d2ae32c60647f555d44b`. Carry that delivered protocol evidence; R19 #1039 still requires its own activation tests and applicable exact-head hosted checks. Existing native constructor, ownership, expiry and join-order regression coverage stays in force.

## Scoped rebaseline and slice audit

Completed product and safety enforcement are retained. The new blocking maintained-aid work is only the two native cases and a minimal exact-inventory profile, with its payoff/domain/owner/retirement recorded in R21. The proof-only alternatives of replaying the full six-class native matrix, repeating the 36-observation ordinary campaign, forcing every scheduler interleaving or recursively proving the harness are excluded. Existing raw evidence, accepted failure classifications and operational ledger remain intact. No repeated-root or runtime-defect conclusion was found.

| Slice | Audited disposition | Owner, scope and dependency judgment |
| --- | --- | --- |
| R19 #1039 | Retain activation slice; add R21 as its sole new blocker | Configuration capability gate remains the activation owner. Existing R17/R18 blockers are fulfilled. One activation PR, existing test/context budget and same-head checks remain intact |
| R21 (issue pending) | New evidence-only slice | Existing test suite/driver own the two cases and receipt; runtime owners and private activation are unchanged. Completed R18 #1038 supplies the changed writer. Four native instances and two builds fit one fresh context and one intended PR |

The strongest split is one child for contention and one for shutdown; both share the predicate witness, venue, driver and applicability receipt, and a partial result would not unblock R19. The strongest merge is folding evidence into R19; it would join independently failing qualification with public activation. Both are rejected on merit. A production yield counter/hook adds an observation handoff and changes the measured product; input/result/source reachability through the existing seam is sufficient. If it proves insufficient, stop rather than invent instrumentation.

Two independent read-only source audits verified the applicability table and feasible fixture seams. They agreed that uninstrumented execution can establish successful marked requests and active-close timing, but cannot force `Close` inside one particular `Gosched` interval. They also identified the join-map bound distinction above. The normative contract incorporates both limits. The final independent graph/contract review accepted the two-slice boundary and carry-forward, with one in-scope clarification: require the existing marked-group witness separately in contention and pressure phases so a scalar-only pressure phase cannot satisfy the scoped claim. The contract incorporates that clarification without adding runs or duration. Review dispositions and docs validation are recorded in the handoff PR discussion.
