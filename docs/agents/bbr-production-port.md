# BBRv3 production port

Status: **approved by the operator on 2026-10-14** (Planit). This is the normative contract for the port, updated in place; [#748](https://github.com/the-sarge/quic-go-fast/issues/748) tracks slice state, and review chronology stays in each PR's discussion.

## Why

The BBR Wayfinder map ([#666](https://github.com/the-sarge/quic-go-fast/issues/666)) explained the BBRv3 regression and built a corrected revision, **r9 (`81e9dc8c`)**, on record branches. The [advancement decision](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md) ([#747](https://github.com/the-sarge/quic-go-fast/issues/747)) advanced r9 on Linux by operator amendment and chose, in D5, a separate production port into `main`'s experimental opt-in sender so that opt-in users get the corrected sender instead of the frozen component (`e4f322cb`). When the port began (`main` at `d3648918`), `main`'s `internal/congestion` and `internal/ackhandler` were identical to the frozen component.

## Accepted outcome (whole effort)

`Config.SetCongestionControlV1("bbrv3")` on `main` selects a sender whose behavior is r9's: C1–C4, bounded ECN feedback, quantum-release pacing (D08 amendment), the kept per-packet changes, the Linux-only netpoller kick and the 32-entry BBR send queue. The sender stays experimental and **unqualified**. Reno remains the default and unselected connections are unchanged. This effort makes no release, adoption, qualification or default-controller change; README and CHANGELOG are not edited (the next release's changelog entry must keep the "unqualified" label).

The ported revision is a **new revision**. No readiness exception granted to r9 (D2, D3) covers it. After the last code PR, its readiness is measured on `minimax` following #739's readiness stage, and its raised cells need an explicit operator advancement disposition before any qualification (L6 or other) runs on it.

## Source of truth and port method

The port reproduces r9's recorded lineage, in order. Each PR lands one recorded state for the packages `main` has not changed since the frozen component:

| PR | Lineage range | Recorded state after the PR |
| --- | --- | --- |
| 1 | `e4f322cb..c6bb13b7` | C1–C3 recovery-service correction layer |
| 2 | `c6bb13b7..8a0e0962` | C4, the four draft-06 conformance items |
| 3 | `8a0e0962..fc4c1bf1` | bounded ECN feedback (D1) and quantum-release pacing (D2): the "D1+D2 candidate" |
| 4 | `fc4c1bf1..d0fabc4d` | the kept per-packet changes (#714 interventions 1, 2, 3, 5 and r6) |
| 5 | `d0fabc4d..81e9dc8c` | r8's Linux netpoller kick and r9's 32-entry BBR send queue |

Method: apply the range's diff (excluding `docs/audits` and `CONTEXT.md`) to `main`. **Identity invariant:** after each PR, `git diff <recorded state> HEAD` over `internal/congestion`, `internal/ackhandler` and every root-package file `main` has not changed since `e4f322cb` shows only the declared deviations below. Reverted lineage leads (#714 m4's first attempt, #735 leads 1–3, #734's timerfd wake) net out of the ranges and are not ported.

**Declared deviations** (the only permitted differences from the recorded state):

1. **Merge seams** in the three root files that both `main` and r9's lineage have changed since the frozen component: `send_queue.go` and `packet_emission.go` (candidate authority, network-admission probes) and `connection.go` (candidate authority, setup reservation, receive phase). r9's hunks are applied to `main`'s versions; `main`'s behavior there is preserved.
2. **Lint:** the one staticcheck finding in r9's tests (`bbr_ecn_equivalence_test.go:317`, QF1001 De Morgan) is fixed in PR 3, and any further finding the pinned linter reports on ported files is fixed minimally and listed in its PR.
3. **Race-mode test budget:** in `internal/ackhandler`, a test-only `raceEnabled` constant (`race_on_test.go` / `race_off_test.go`, mirroring `service_work_on.go` / `service_work_off.go`) and `reducedEquivalenceBudget()` (`equivalence_budget_test.go`) make each randomized frozen-equivalence oracle use its own existing reduced (`testing.Short()`) budget under the race detector: each ported `testing.Short()` budget check in those oracles becomes `reducedEquivalenceBudget()`, and nothing else in them changes. Ordinary runs keep the full budget. Reason: at r9 these oracles take `internal/ackhandler` from 5.5 s to 333 s under `-race` (0.4 s to 22 s plain), which roughly tripled hosted unit CI on r9 heads; they are single-goroutine comparisons that the race detector adds little to.

Everything else is ported verbatim, including all recorded tests, the frozen-equivalence and reference oracles, and the `bbrworkcount` work counters (inert, compiled away in ordinary builds; their tag-gated tests are local evidence, not hosted CI).

## Invariants every PR preserves

- **Default Reno:** unselected connections are unchanged. Every changed production line is reachable only for a BBR-selected connection, except the declared merge seams, which keep Reno's values (Reno's queue stays eight entries; Reno never creates a pacing kick).
- **Protocol and payload integrity; packet and buffer ownership** ([ADR 0004](../adr/0004-packet-emission-ownership.md)): construction, registration and output handoff ownership are unchanged; each queued entry's buffer and local send credit are released exactly once.
- **ECN and loss correctness:** ECN validation, the ECN counter fence, CE response, loss detection, persistent congestion and frame retransmission behave as recorded at the lineage state.
- **Candidate authority, setup admission and receive phase** (on `main`, not in r9): every BBR send path keeps per-submission authorization and fencing.
- **Bookkeeping bounds unchanged:** `maxRecoveryOutcomes` 32,768, `maxDeliveryLive` 25,000, `maxDeliveryRetained` 4,096 and `maxECNMarkRanges` 4,096 (identical on `main` and r9). The bookkeeping representation remedy and any bound change are **untriggered conditional leads**; they need their named evidence or an explicit operator amendment.
- **Supported platforms:** every platform builds; the netpoller kick is Linux-only behind build tags with a no-op elsewhere; the 32-entry queue applies to BBR on every platform (r9's macOS and Windows behavior is unmeasured and stays so).
- **Public API and wire format:** unchanged. Any contract change is a separate explicit design decision.

## Non-goals and ceilings

No tuning, no new BBR behavior beyond r9, no bound or limit changes, no bytes- or time-ahead queue bound (an untested lead only), no new benchmark or measurement framework, no CI redesign, no README or CHANGELOG edit, no release, no qualification run. A review finding cannot widen a PR beyond its lineage range and declared deviations.

## Per-PR review contracts

Common to every PR:

- **Artifact classes.** Shipped behavior: BBR sender, BBR recovery/ECN adapter, BBR emission and queue paths. Required safety enforcement: the default-Reno gating and (PR 4–5) the authority and credit seams. Verification aids: ported tests and oracles, `bbrworkcount` counters, any seam test added here. Process metadata: this file, glossary, design-doc amendments, journal.
- **Representation domain and owner.** The recorded lineage commit owns the ported code; the identity invariant is a universal, mechanically checked guarantee over the ported files. Behavioral guarantees are example-level: the recorded tests, plus the seam tests named below. No new universal behavioral claim is made.
- **Evidence (terminating).** The identity diff; `go build ./...`; `go test` of the affected packages and the root package; `go test -race` of the affected packages; `go test -tags bbrworkcount ./internal/ackhandler` where counters changed; `go vet` on changed packages; `go mod tidy -diff`; the pinned golangci-lint where available locally; one cross-compile spot check (`GOOS=windows`, `GOOS=freebsd`) for PRs touching root files; then the repository's hosted unit, integration, lint and cross-compile workflows on the exact head.
- **Contract closure.** Not triggered except PR 5's authority seam (below): the ported behavior is owned by its recorded tests, and default-Reno gating is construction-time.
- **Review budget.** One fully briefed RAS review, verification of accepted fixes, at most one replacement review.

**PR 1 — C1–C3 recovery-service correction layer.** Ports `e4f322cb..c6bb13b7` (`internal/ackhandler`, plus 15 lines in `internal/congestion`). Also adds this contract file and the five glossary entries that live on record branches (**Matched Reno**, **Controller bookkeeping**, **Retained application data**, **Packet-tracking state**, **Readiness exception**), verbatim. Carries the fixes for [#630](https://github.com/the-sarge/quic-go-fast/issues/630) (initial-window arithmetic without constructing a sender) and [#636](https://github.com/the-sarge/quic-go-fast/issues/636) (genuine-idle pacing keeps the unmeasured fallback, with the regression test #636 asked for); both close after merge revalidation.

**PR 2 — C4 draft-06 conformance.** Ports `c6bb13b7..8a0e0962` (`internal/congestion` only). Adopts spurious-undo phase restoration, so it amends the design's undo sentence ("Undo restores only the saved loss-derived bounds/cwnd …") to state that an all-spurious undo also restores the loss-driven probing phase, as `558d1729` implements.

**PR 3 — Bounded ECN feedback and quantum-release pacing.** Ports `8a0e0962..fc4c1bf1`. Carries the D08 amendment to `docs/designs/bbrv3.md` verbatim from `fc4c1bf1`, and adds the drop-tail overflow disclosure from the [#710 record](https://github.com/the-sarge/quic-go-fast/blob/a59a9779/docs/audits/2026-10-02-bbr-quantum-pacing/README.md): releasing a full quantum per wakeup made the S5 one-BDP drop-tail queue overflow about six times as often (0.13% to 0.84% of forward packets) without a goodput loss. Fixes the De Morgan lint finding. After merge, [#613](https://github.com/the-sarge/quic-go-fast/issues/613) is revalidated against the bounded feedback and its recorded work and allocation tests, and closed or left open with that evidence.

**PR 4 — Kept per-packet changes.** Ports `fc4c1bf1..d0fabc4d`: loop continuation and lazy credit reads, the slab-backed delivery-record table, bracketed recovery searches, recycled send reservations and the per-opportunity capability snapshot. These remove both BBR-only per-packet allocation sites the map named (delivery-record map churn in `captureCongestionSend`; `&sendReservation{}` in `localSendCredit.reserve`), with their recorded `AllocsPerRun` tests. Merge seam: three fields in `packet_emission.go`. Seam evidence: one focused test that a fenced candidate's queued BBR entries return all local credit exactly once (reservations are now recycled, so a double completion would corrupt a live reservation).

**PR 5 — Linux netpoller kick and 32-entry BBR send queue.** Ports `d0fabc4d..81e9dc8c`. Merge seams in `connection.go`, `packet_emission.go` and `send_queue.go`. The authority seam triggers closure: `newSendQueue` delegates to one constructor, `newSendQueueWithCapacity`, which owns the `authorizedWriter` capture, and `replacePath` keeps the connection's depth and rebinds authority.

| Case | Expected | Owner | Evidence |
| --- | --- | --- | --- |
| Reno, initial queue | 8 entries; authorized writer captured; authority bound by `bindCandidate` | `newSendQueueWithCapacity` | existing authority tests |
| BBR, initial queue | 32 entries; same capture and binding | same | new seam test: fenced BBR entries are not submitted |
| Reno, `replacePath` | 8 entries; authority rebound | same + `bindAuthority` | existing tests |
| BBR, `replacePath` | 32 entries; authority rebound | same | ported depth test + new authority assertion |

Disclosed open costs, recorded here and in the PR: [#734](https://github.com/the-sarge/quic-go-fast/issues/734)'s run-loop wake increase (6–16% per useful GiB on S5) and [#738](https://github.com/the-sarge/quic-go-fast/issues/738)'s S5 DATAGRAM sender CPU rise (+5% against r8; 0.953 of frozen Reno in r9's readiness, passing). The Linux kick test runs on hosted Ubuntu CI and on `minimax` under `taskset -c 1-3`.

## After PR 5: readiness and advancement disposition

1. **Readiness measurement** on `minimax`, on a record branch (records stay off `main`, as for every #666 record): #739's readiness stage unchanged except the candidate revision (the merged port head), with its `s5timeline` and conditional preservation stages. The registration names its baselines and states how a Reno-on-candidate difference is reported, given that `main` carries non-BBR changes since `e4f322cb`. r9's recorded values are reported beside each cell, descriptively. Every raised cell stays raised with absolute values; nothing is pre-cleared. Needs an operator-booked quiet window: about 1.5–2.5 h of host time, about 5–8 h for the whole step.
2. **Advancement disposition:** a decision record presenting each raised cell for a fresh operator decision under the unchanged criteria. A raised preservation flag blocks advancement. No qualification runs before this decision.

## Carried inputs from D5

| Input | Where it lands |
| --- | --- |
| D08 quantum-release amendment and drop-tail disclosure | PR 3 |
| Undo-sentence amendment (phase restoration adopted) | PR 2 |
| Two BBR-only per-packet allocation sites | PR 4 removes both |
| #630, #636 | PR 1 (already fixed in `c6bb13b7`) |
| #613 | revalidated after PR 3 |
| #734 wake increase, #738 +5% S5 DATAGRAM CPU | disclosed in PR 5; re-measured by readiness |
| Bytes- or time-ahead send-queue bound | lead only; not implemented |
| Glossary entries on record branches | PR 1 |
| Open memory and loopback-latency questions | readiness step; no exception carries over |
| Bookkeeping remedy and bound changes | untriggered conditional leads; bounds unchanged |
| Discovery-scratch retention found in PR 1's review (inherited from `c6bb13b7` through r9) | deferred: bounded metadata retention, no functional effect; filed as [#752](https://github.com/the-sarge/quic-go-fast/issues/752) after PR 1 merged. **Operator decision (2026-10-14):** #752 lands as its own PR after PR 5 and before the readiness measurement, so the measured revision is the one that would ship |

## Stop conditions

Stop and return to the operator if: a ported test fails on `main` for a reason outside the declared merge seams; a fix would change BBR behavior beyond r9 or touch a Reno-observable path; a deviation beyond the three declared kinds is needed; a review finding is `stop-for-decision`; or a hosted check fails for a reason that is not a demonstrated external infrastructure failure.

## Resolved blocker outside this effort

PR 1 found the lint workflow's pull-request `check` job failing on every pull request: its govulncheck step, run on the pinned Go 1.27.1 toolchain, reported standard-library vulnerabilities that Go 1.27.2 fixes, and moving the primary Go pin also needed a golangci-lint release newer than v2.13.2, which cannot read go1.27.2 export data. Per the operator's 2026-10-14 decision a separate CI-repair PR landed first: [#750](https://github.com/the-sarge/quic-go-fast/pull/750) (`b6588e13`) moves the primary pin to Go 1.27.2 and golangci-lint to v2.14.0; the 1.26.8 compatibility lanes are unchanged. Later port PRs branch from a `main` that includes it.
