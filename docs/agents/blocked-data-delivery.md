# Blocked-data delivery regression

## Outcome and acceptance criteria

Issue [#222](https://github.com/the-sarge/quic-go-fast/issues/222) extends `TestDataBlockedDelayedDelivery` to the declared `TIMESCALE_FACTOR` examples 1, 3, 10 and 20. The regression caps its effective RTT at 15 ms, the existing factor-3 setting, and delays the final batch by three effective RTTs. Failure-wait budgets remain scaled. The original undelayed tests retain their existing timing. This bounds an injected fixture condition against fixed transport timers; it does not establish freedom from PTO under arbitrary scheduler stalls or support for arbitrary timing factors.

- Require accepted batches of 100/200/400 bytes, write-deadline errors, complete delivery, blocked offsets 100/300/700, exactly three relevant blocked frames, and existing bundling checks.
- Preserve the regression's ability to fail the old read-completion assumption in both stream- and connection-limited cases.
- Make frame-count checks fatal before indexing expected offsets, with observed frames included in failure output.
- Preserve existing factor-1/3 behavior. Do not filter or deduplicate retransmissions to satisfy assertions.

## Boundary and representation

The approved scope is `integrationtests/self/packetization_test.go` and this contract. The self-test helper owns the fixture; existing typed qlog frames and real UDP/TLS connections own representation and observation. The guarantee is example-level coverage of stream/connection limits, QUIC v1/v2 and the four declared factors. Exact counts describe these scenarios, not arbitrary QUIC traffic. No production transport changes, global timing changes, dependencies, CI expansion or arbitrary scheduler-stall guarantee are included. The cap changes simulated RTT, write deadlines and injected delivery delay only in the delayed regression; it does not change shared transport state, APIs or wire contracts.

The tests are maintained verification aids explicitly approved for this work. Their operational payoff is detecting recurrence of the accepted-bytes versus delivered-bytes assumption. Replace or retire the regression only when equivalent coverage is explicitly accepted. This contract, review history and certification receipts are process/traceability metadata; historical evidence remains unchanged. No shipped behavior or required safety enforcement is modified. Contract closure is not triggered for this bounded verification-aid repair; no semantic closure matrix is required.

## Terminating evidence and review

Use the existing integration fixture as the approved test seam. On local macOS arm64, reproduce the baseline factor-20 race failure, stopping at the first failure or 15 runs. Temporarily restore the old read-completion behavior and require the revised regression to fail for both limiting modes at factors 1 and 20; remove that mutation. Inject an excess frame once per limiting mode to confirm assertion failure without an indexing panic; remove those mutations. These are bounded diagnostic mutations, not maintained harnesses.

Run all three affected tests 25 times per factor/version combination, both normally and with race detection: factors 1, 3, 10, 20 and versions 1, 2. Run one uncached full local suite, `go vet ./...`, module-tidiness and formatting checks. Repetition addresses the prior instability and terminates at the declared counts; it is not a statistical reliability guarantee. Existing hosted workflows supply their configured platform coverage. Diagnose failures rather than retrying until green; expand evidence only for an accepted correction or changed decision.

Review permits one fully briefed initial RAS review, verification of independently accepted fixes, and at most one replacement review. The implementing agent judges and fixes; RAS supplies review evidence only. Stop if the timing cap cannot preserve the acceptance criteria, a production change is needed, or an accepted obligation exceeds the boundary or evidence/review budget. Apply the shared representation and repeated precise-root stops. Keep historical run IDs and receipts in the PR discussion instead of appending them to this normative contract.

Develop in a dedicated worktree and feature branch, open one draft PR, and certify the exact final head/base with the local checks above. This repository has no `task preflight`, `ci.yml` or draft-gated `ci-*` jobs. Require the applicable existing hosted checks to pass on the exact live head, mark ready, and squash merge with a pinned head. Reconcile a changed base and repeat required review/certification/CI gates. Append the dev journal only after merge, without RAS, then update the linked task and revalidate surviving deferred findings against the merged code.
