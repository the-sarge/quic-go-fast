# Blocked-data delivery regression

## Outcome and acceptance criteria

The blocked-data fixture exercises stream- and connection-level flow control with declared `TIMESCALE_FACTOR` examples 1, 3, 10 and 20. It runs real QUIC/TLS connections over the existing simulated packet network inside `testing/synctest`, so packet delivery and protocol timers share virtual time. Host scheduling delays must not become undeclared network delays that provoke PTO retransmissions and invalidate exact frame counts. This is a controlled protocol fixture, not native UDP qualification or a guarantee about arbitrary network delays.

- Require accepted batches of 100/200/400 bytes, write-deadline errors, complete delivery, blocked offsets 100/300/700, exactly three relevant blocked frames, and existing bundling checks.
- Preserve the delayed-delivery regression's ability to fail the old read-completion assumption in both stream- and connection-limited cases.
- Retain the existing logical RTTs: five milliseconds times the factor for the original scenarios; cap the delayed regression's RTT at 15 ms and delay its final client-to-server batch by three effective RTTs. Failure-wait budgets remain scaled.
- Keep frame-count checks fatal before indexing expected offsets, with observed frames included in failure output. Do not filter or deduplicate retransmissions to satisfy assertions.

## Boundary and representation

The approved CI repair in [PR #363](https://github.com/the-sarge/quic-go-fast/pull/363) is limited to `integrationtests/self/packetization_test.go` and this contract. The self-test helper owns the fixture; existing simnet endpoints own scheduled packet delivery, the Go runtime owns virtual time, and production QUIC/TLS plus typed qlog frames own protocol processing and observation. The guarantee is example-level coverage of stream/connection limits, QUIC v1/v2 and the four declared factors. Exact counts describe those scenarios, not arbitrary QUIC traffic. No production flow-control changes, global timing changes, dependencies, CI expansion, retransmission suppression or packet filtering are included. Other integration tests retain their native UDP boundaries.

These tests are maintained verification aids. Their operational payoff is detecting recurrence of the accepted-bytes versus delivered-bytes assumption and checking blocked-frame emission and bundling at known flow-control limits. The simulated fixture replaces wall-clock UDP delivery for these assertions without replacing protocol processing. Replace or retire the regression only when equivalent coverage is explicitly accepted. This contract and review/certification receipts are process metadata; historical evidence remains unchanged. No shipped flow-control behavior or required safety enforcement is modified. Contract closure is not triggered for this bounded verification-aid repair.

## Terminating evidence and review

Run all three affected tests once per factor/version combination, both normally and with race detection: factors 1, 3, 10, 20 and versions 1, 2. The fixture now controls scheduling with virtual time; repeated wall-clock runs are not its correctness argument. Temporarily restore the old read-completion behavior and require the delayed regression to fail for both limiting modes at factors 1 and 20, then remove that mutation. Existing exact-count assertions remain unchanged; no new mutation framework is required.

Run affected-package tests, `go vet` on changed packages, module-tidiness, formatting and diff checks. Existing hosted workflows supply their configured native platform coverage. Diagnose failures rather than retrying until green; expand evidence only for an accepted correction or changed decision. The controlled diagnosis in PR #363 demonstrates that delayed responses can produce the same duplicated offset-100 frames through PTO; it does not identify the exact scheduling event in the historical CI failure.

The retry implementation's initial review and replacement review remain historical evidence for that unchanged production code. The separately approved CI repair requires a fresh, fully briefed review of the final combined candidate, verification of accepted in-scope findings, and at most one replacement after such fixes. The implementing agent judges and fixes; RAS supplies evidence only. Stop if preserving these assertions requires production changes or expansion beyond the approved fixture boundary. Keep run IDs, dispositions and historical receipts in the PR discussion instead of appending them here.

Develop in a dedicated worktree and feature branch and certify the exact final head/base. This repository has no `task preflight`, `ci.yml` or draft-gated `ci-*` jobs. Require applicable hosted checks to pass on the exact live head, mark ready, and squash merge with a pinned head. Reconcile a changed base and repeat required review/certification/CI gates. Append the dev journal only after merge, without RAS, then update the linked task and revalidate surviving deferred findings against the merged code.

The earlier real-UDP fixture repairs and their completed evidence remain recorded in [PR #221](https://github.com/the-sarge/quic-go-fast/pull/221) and [PR #263](https://github.com/the-sarge/quic-go-fast/pull/263).
