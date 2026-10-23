# Windows startup fixture investigation #987

The later [#1002 contract](windows-startup-port-selection-1002.md) supersedes this investigation's TCP port-zero selection strategy after a native recurrence showed TCP-allocated ports denied for UDP. This document retains the original bounded investigation and decision; frozen evidence remains unchanged.

## Accepted outcome

Deliver bounded diagnosis, fixture cleanup and the evidence-supported correction for [issue #987](https://github.com/the-sarge/quic-go-fast/issues/987). Apply a port-selection correction only if native Windows evidence supports it. The initial inconclusive diagnostic PR #991 is merged. A natural recurrence during journal CI now triggers the originally approved conditional correction in a separate fixture-only PR; keep the journal documentation-only and hold its merge until the correction lands. This additional PR is an execution consequence of when native evidence arrived, with no expanded production boundary or investigation budget. Leave #987 open until native validation and merge establish the supported fixture correction. The user approved this contract in the planit chat on October 22, 2026.

## Acceptance criteria

- [ ] Exhausted fixture setup reports bounded per-attempt addresses and complete bind errors; temporary sockets are closed on every exit, including assertion failures.
- [ ] Record a finite native Windows investigation on a pinned revision, beginning with the named test and using the captured HTTP/3 shuffle order or original suite conditions if isolation does not reproduce it. State the attempt/time budget before running; do not rerun unchanged CI until it happens to pass.
- [ ] If evidence identifies a cause, explain the causal link to the selected fixture correction and validate it on native Windows. If evidence remains inconclusive, publish that result and the precise next evidence needed without claiming the issue fixed or closing it as resolved.
- [ ] Any correction retains actual sockets and verifies UDP-first helper startup, the test-owned TCP conflict, a TCP `listen` error, hooks not running, and successful UDP rebind after return. Preserve cleanup of the TCP reservation and all temporary UDP sockets.
- [ ] Any correction passes the focused test and relevant combined-server lifecycle tests; record platform, Go version, revision, command, and outcome. Passing macOS/Linux or Windows cross-compilation alone cannot substitute for native Windows evidence.

## Boundary and representation

The maintained surface is `TestListenAndServeTLSStartupFailureReleasesUDP` in `http3/combined_server_test.go`. Preserve ten selection attempts. Reserve TCP on `127.0.0.1:0` first and probe UDP on that held address. Close rejected TCP candidates immediately, and close successful UDP probes before calling the unchanged UDP-first helper. Register socket cleanup immediately after acquisition, before an assertion can abort; close temporary UDP sockets before proceeding. Log bounded attempt numbers, requested/selected addresses, both bind stages, complete errors and available underlying OS error codes.

The test owns fixture-local socket selection, reservation and cleanup. Go's networking/error types and Windows own error representation. This is example-level diagnostic and regression coverage of loopback IPv4 real sockets, not a guarantee that port allocation always succeeds or that every environmental failure is classified. Claims about all sockets and attempts are bounded to this test's ten synchronous attempts and final rebind. The trusted representation is typed Go errors and OS-assigned socket addresses; no grammar scanner is introduced.

Preserve real UDP-first helper startup, the held TCP address as the deliberate conflict, the TCP `listen` error, unused hooks, UDP rebind and reservation cleanup. Exclude production server/API/wire behavior, recovery/controller work, dependencies, global synchronization, increased retries, Windows skips, mocked replacement of the real-socket regression, and broad fixture refactoring. A correction must fit this fixture; a broader seam requires a new decision.

The test and temporary branch-only diagnostic job are verification aids. The operational payoff is actionable future failures and attributable native investigation. User approval makes their declared checks gates for this PR, without adding a permanent product gate. Remove the temporary workflow before the final candidate. Retain bounded fixture diagnostics while the fixture is maintained; reconsider them when the fixture retires or the investigation resolves. This contract and linked receipts are process metadata. No shipped behavior or required safety enforcement changes.

Contract closure is not triggered: ordinary focused tests and ownership inspection cover the small synchronous lifecycle. No recursive harness closure, mutation campaign, cross-product, fuzzing or extra platforms are authorized.

## Design and blast radius

Keep decisions in the existing fixture owner with synchronous logging and immediate cleanup registration. This adds bounded test output and cleanup callbacks but no cross-goroutine handoff, mirrored state, public contract, production performance or security surface. Cleanup callbacks close their own acquired resources; immediate closes retain existing ordering. The demonstrated recurrence justifies moving candidate selection to TCP's allocator; the helper startup order is unchanged. No cross-protocol availability guarantee is inferred: UDP is explicitly probed while TCP remains held.

A generic port-allocation framework is rejected on merit: it adds interfaces without serving the single fixture. Changing the production socket owner is outside this contract; revisit only if evidence establishes a production defect and the user approves expanded scope. Increasing retries or skipping Windows is rejected on merit. The specific Windows host policy behind access-denied binds remains unknown; the fixture's invalid inference from UDP allocation to TCP eligibility is demonstrated. See the [recurrence evidence and causal decision](../audits/issue-987/windows-bind-denial/diagnosis.md).

## Finite evidence

One controlled local real-socket exhaustion probe checks all ten attempt diagnostics and underlying errors; it is not historical reproduction and is removed before the candidate. Native diagnosis uses Windows Server 2025, Go 1.27.3, `GOTOOLCHAIN=local`, `TIMESCALE_FACTOR=10`, actual pinned HEAD and recorded image/build. Budget one diagnostic job and one validation job only if evidence supports a correction, each at most 30 minutes including setup. Start with at most 100 focused executions, fail-fast and a 60-second Go test timeout. If isolation does not reproduce, run HTTP/3 once with shuffle seed `1792682603248206600`, coverage and a five-minute stage cap; then, only if needed, the original unit package scope/coverage/random-shuffle settings once with a ten-minute stage cap and actual seed retained. Do not rerun unchanged failing CI. Existing required CI certification is separate from this diagnosis budget.

Use existing capture tooling, preserving command, environment, source revision/patch, logs, checksums and completeness limits. The one remaining conditional validation job runs the corrected focused test at most 100 times, combined lifecycle tests once, HTTP/3 once with recurrence seed `1792692485838992000`, and unit scope once, with the same five-/ten-/30-minute stage/job caps. The initial diagnostic job and controlled exhaustion probe have already been consumed; no new campaign is authorized. If evidence remains inconclusive, name precisely what next native failure evidence is needed and leave the issue unresolved.

Final local certification: focused test, combined lifecycle tests once with race detection, `go test -count=1 ./http3`, `go vet ./http3`, `go mod tidy -diff`, formatting/lint and diff checks on the exact pushed candidate. Record commands, platform, Go version, SHA and outcomes. macOS/Linux or cross-compilation does not substitute for native Windows evidence.

## Review and completion

Run one initial fully briefed RAS review, independently disposition substantive findings, verify accepted fixes and allow at most one replacement review. Supply the criteria above verbatim, this representation domain, artifact classes, boundaries, terminating evidence and review budgets. No unresolved stop-for-decision finding may remain. Stop for material scope expansion, repeated precise-root failure, exhausted investigation budget requiring additional work, or evidence that safe completion needs production changes.

Follow [the repository execution overlay](../REVIEW-LOOP.md): draft PR with existing CI running, local certification, successful applicable existing PR checks on the exact live head, readiness, and squash merge with `--match-head-commit`. The repository has no `task preflight` or `ci-*` gates. Revalidate and repeat required gates if main advances. Append the journal only after remote main contains the merge, then revalidate deferred findings and reconcile issue/task pointers. Never close #987 as resolved on inconclusive evidence.

The bounded implementation context is this contract, the issue's agent brief, the changed fixture and helper's UDP-first startup seam, and the historical [initial diagnosis](../audits/issue-987/diagnosis.md) and current [recurrence/correction evidence](../audits/issue-987/windows-bind-denial/diagnosis.md). Review chronology, run IDs and historical receipts belong in PR discussion rather than this normative contract.
