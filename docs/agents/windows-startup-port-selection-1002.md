# Distributed combined-server fixture port selection

Status: approved by the operator through Planit on 2026-10-23 for [#1002](https://github.com/the-sarge/quic-go-fast/issues/1002), a prerequisite to #971. This is the current execution contract; review history and exact candidate receipts belong in PR discussion.

## Outcome

Repair the existing `TestListenAndServeTLSStartupFailureReleasesUDP` fixture blocking #971 / PR #999, in a separate prerequisite PR approved by the operator in the Planit chat on 2026-10-23. Select at most ten independently distributed unprivileged loopback IPv4 port candidates, then require actual TCP and UDP binds on the same address before invoking the unchanged production helper.

## Evidence

[Windows Go 1.26.10 job](https://github.com/the-sarge/quic-go-fast/actions/runs/39918053217/job/120296276491) failed only this test. All ten TCP-selected ports 63336–63345 succeeded for TCP and failed UDP with WSAEACCES/10013. The helper never started. The previous #987 correction observed the reverse direction. Changing which protocol allocates port zero does not remove the correlation between adjacent candidates and protocol-specific restrictions. The exact expired runner reservation/security policy remains unknown. This is a fixture-selection failure, not demonstrated external infrastructure failure; no unchanged-head rerun is authorized.

## Acceptance criteria

- Retain at most ten candidate attempts, real sockets, bounded address/error-chain diagnostics and immediate cleanup. An unavailable TCP or UDP candidate is rejected and released before the next attempt.
- Keep the successful TCP reservation held, close its UDP probe, invoke the real UDP-first helper and retain assertions for a TCP listen error, unused serving hooks and successful UDP rebind afterward.
- Sample independent port numbers in 1024–65535; success requires both binds. Do not infer cross-protocol availability, increase retries, skip Windows, change production server behavior or alter global network configuration.
- Validate the candidate in one temporary native Windows Go 1.26.10 job, at most 30 minutes: at most 100 focused executions with fail-fast/60-second timeout, combined lifecycle tests once, and HTTP/3 once with captured shuffle seed 1792740625500411700 and five-minute cap. Capture revision/platform/commands/output/checksums and contemporaneous TCP/UDP dynamic/excluded ranges. Remove the workflow before the final candidate; ordinary exact-head PR CI remains required.
- Run focused and combined race tests, HTTP/3 tests, vet, module tidiness, formatting/pinned lint and diff checks. Preserve frozen #987 evidence. Record the limits: finite real-socket evidence, no statistical flake-rate or universal port-availability guarantee.

## Review contract

Verification-aid-only changes: the single fixture, its execution contract and new evidence. The fixture owner keeps synchronous port selection and socket cleanup; Go networking and typed OS errors own representation. Guarantee is example-level over the bounded loopback IPv4 fixture, with no production behavior or dependencies changed. Contract closure is not triggered; focused tests and ownership inspection cover this small synchronous lifecycle. No recursive harness, mutation campaign or expanded platform/repetition budget.

One initial fully briefed RAS review, independent dispositions, accepted-fix verification and at most one replacement review. Exact-head local/hosted certification and squash merge under the repository overlay, then journal without RAS. Once merged, update #999 to current main and repeat its required review, certification and CI. No new performance campaign is authorized.
