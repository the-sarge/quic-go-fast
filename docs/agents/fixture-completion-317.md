# TestListenAddr fixture completion (#317)

## Accepted contract

The maintainer approved this one-PR confirmation and fixture-repair scope in the planit conversation on 2026-09-15 UTC. The [completed diagnosis](https://github.com/the-sarge/quic-go-fast/issues/317#issuecomment-5673120889) is historical evidence, not a renewable experiment allowance. Its original hosted-failure attribution remains an inference.

Make `TestListenAddr` finish its own transport receive loop before returning, supported by a failing regression and a synchronization-only intervention.

Acceptance: observed regression failure without the join; success with it; successful bounded intervention; unchanged assertions and production source.

Preserve the real `ListenAddr` call and existing address-error checks, `TestDial` assertions and the process-wide goroutine observer, public listener behavior, caller-owned sockets, and incoming-storage ownership. Production changes, timeout increases, sleeps, observer weakening, CI changes, permanent diagnostics, and investigation of #241 or HTTP failures are outside scope.

## Implementation boundary

Maintained Go changes belong in `server_test.go`. Share only successful-listener fixture mechanics needed by `TestListenAddr` and its regression. Retain real `ListenAddr` construction, capture the transport through `ln.baseServer.tr`, close the listener, and join its existing `listening` channel before fixture cleanup returns. Use a bounded failure watchdog following test conventions; the channel is the synchronization mechanism.

The regression follows `TestServerFixtureStopsTransport`: create the real fixture in a child test, then require receive completion immediately after the child returns. This test-local cleanup owner is the approved seam. The wait can lengthen fixture cleanup, but introduces no production state, socket factory, dependency, public interface, or storage change. If this seam cannot safely enforce the boundary, stop before widening the implementation.

## Representation and artifact contract

The supported domain is real, successfully created, idle `ListenAddr` fixtures. `Transport.listening` owns the completion representation. The guarantee is example-level regression coverage, not universal absence of transport overlap. Contract closure is not triggered: focused evidence covers this single fixture boundary; no recursive harness-completeness obligation is accepted.

The fixture and regression are maintained verification aids owned by root-package tests. Their operational payoff is preventing cross-test teardown overlap; revise or retire them when this fixture or its completion boundary disappears. They are explicitly accepted deliverables, with no new shipped behavior or required safety enforcement. Temporary experimental patches and logs become frozen evidence, not maintained tools. This plan and validation receipts are process/traceability metadata.

## Finite evidence

Use a dedicated feature branch and worktree under `/Volumes/worktrees/quic-go-fast/fixture-completion-317`, reconciling remote main before editing. Prefer an already available native Ubuntu amd64 environment; otherwise use the existing Ubuntu amd64 Docker image and report emulation and other differences. Do not provision infrastructure.

Four targeted launches are authorized, each capped at ten minutes and 1,000 repetitions with early failure termination. Setup failures consume slots. Use Go 1.26.8, coverage, `TIMESCALE_FACTOR=10`, and recorded seed `1789428569339240250` where applicable. Retain source/diff, environment, commands, counts, and exit statuses. Temporary exact-predicate profile capture is permitted when needed.

| Launch | Evidence purpose |
| --- | --- |
| 1 | Explicit `TestListenAddr` then `TestDial/Dial` sequence, with original assertions and verified order. |
| 2 | Same sequence, changing only the earlier fixture's receive-completion join. |
| 3 | Regression without the join; an observed failure is mandatory. |
| 4 | Same regression with the join; must pass. |

The without-join regression is the single guard-omission control; no extra mutation campaign is required. Passing attempts alone do not establish resolution. If discriminating causal evidence cannot be obtained in this budget, preserve results, leave #317 open, and return for a scope decision.

After confirmation, run one seeded root-package pass, one root-package race pass, `go vet .`, `go mod tidy -diff`, formatting, and diff checks. Repeat affected checks only for changes or diagnosed failures. No extra platform or stress campaign is authorized. Results and limitations live in the linked [evidence report](../audits/fixture-completion-317/README.md).

## Review and delivery

Use one fully briefed RAS review, verification of independently accepted fixes, and at most one replacement review. Quote this acceptance criterion verbatim in the briefing. The implementing agent dispositions findings and performs fixes; RAS supplies evidence only. Apply the shared review-loop boundary, evidence-budget, representation, and precise-root stops. A second distinct counterexample at the same accepted invariant and concrete owner requires reconsideration, not another local patch.

The bounded implementation context is this contract, relevant source and preservation invariants, and the linked evidence report; historical diagnosis logs need not be duplicated. Keep this normative plan current and store review chronology/receipts separately.

Follow the [repository execution overlay](../REVIEW-LOOP.md): draft PR, exact-head local certification, successful applicable hosted checks, ready status, then squash merge that exact head. Diagnose failed checks before reruns. After default-branch advancement, reconcile and repeat the required review/certification/CI gates. Only after merge, append the dev journal without RAS, revalidate deferred findings against the merged code, and update #317 and OmniFocus. Preserve the distinction between the repaired fixture boundary and the historical hosted failure's inferred attribution.
