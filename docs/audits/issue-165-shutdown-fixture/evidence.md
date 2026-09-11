# HTTP/3 shutdown fixture repair (#165)

The normative source is the [latest issue #165 triage brief](https://github.com/the-sarge/quic-go-fast/issues/165#issuecomment-5631303461).

## Acceptance criteria (verbatim)

- [ ] Record evidence distinguishing fixture/setup/scheduling effects from a protocol defect; preserve uncertainty about the historical failure unless new evidence resolves it.
- [ ] Establish an unfinished exchange and verify it survives until the intended shutdown boundary, then terminates following deadline expiration.
- [ ] Preserve `context.DeadlineExceeded` from shutdown and `http3.ErrCodeNoError` on the client, and observe handler termination.
- [ ] Demonstrate with a controlled setup-delay case that setup time does not invalidate the repaired shutdown observation; preserve sensitivity to premature closure and failure to terminate.
- [ ] Bound observation waits and clean up response bodies, tickers, and owned workers even on assertion failure.
- [ ] Run the focused test and relevant neighboring graceful-shutdown coverage with the repository's timing scale and QUIC version selection; report environment and outcomes. Use existing required checks for the resulting change, without an open-ended stress or rerun campaign.

## Boundary and evidence budget

The owner is `TestGracefulShutdownLongLivedRequest` and its local helper. The domain is an established, unfinished response using the real HTTP/3 server and transport, with zero setup delay and one controlled 75 ms setup delay (both scaled by the repository timing factor). Standard Go contexts and monotonic timestamps own deadline representation; QUIC and HTTP/3 own connection and stream errors. This is example-level verification, not a universal scheduling or public latency guarantee. The changed test is a verification aid; this document is evidence metadata. Production behavior and wire/API contracts remain unchanged.

The terminating plan is the controlled setup-delay red/green check; two temporary fault probes (immediate closure at shutdown entry and omitted closure at deadline expiry); focused neighboring shutdown coverage for QUIC v1 and v2 with `TIMESCALE_FACTOR=3`; one focused race run; one uncached full local suite; and repository static checks. Both fault probes must fail for the intended assertion and exit within the process timeout, and are removed before commit. No open-ended stress, mutation, or platform campaign is authorized. Existing hosted workflows supply platform coverage. Review comprises independent Standards/Spec reviews, one initial fully briefed RAS review, verification of accepted fixes if needed, and at most one replacement review. Review history and final receipts stay outside this document.

This repository has upstream-style workflows that run on push and pull request, including drafts, and no Taskfile, preflight command, or `ci.yml` / `ci-*` gate. Use its applicable existing hosted checks, successful and unskipped on the live head; mark ready after review and local certification, then squash merge with a pinned head. Do not change CI or rerun successful draft checks to manufacture portfolio gate names.

## Diagnostic evidence

Initial base: `420d6bad9b65f7b5a3645e27b77d783180b357cd`. Environment: Go 1.27.0, darwin/arm64, `TIMESCALE_FACTOR=3`, QUIC v1, caching disabled, 30-second process timeout for focused probes.

The old fixture started the client clock before request/connection setup and created the shutdown timeout later in a handler goroutine. The handler had a third start point and an unstopped ticker. A temporary 225 ms (scaled 75 ms) delay in the existing transport dial callback produced 306.698334 ms client elapsed time against the 75 ms target and 37.5 ms tolerance. The client timing assertion was made nonfatal only to observe subsequent assertions: the expected HTTP/3 error, `context.DeadlineExceeded`, and original handler-duration bound all passed. This demonstrates setup sensitivity; it does not identify the cause of the historical CI failure or establish an I4 regression.

The repaired fixture receives response headers before creating the shutdown context. The handler leaves the response unfinished until its request context ends, with a separate failure-cleanup cancellation path. Client-body and shutdown workers timestamp their own terminal observations; handler completion publishes its timestamp. Every terminal observation must be at or after the actual shutdown deadline, and bounded watchdogs detect missing termination. No narrow upper latency assertion remains. Cleanup cancels the request and handler escape context, closes the response body and server, and joins owned workers and the started handler with a bounded wait. The ticker is no longer needed.

The zero-delay and controlled-delay cases passed. Replacing graceful cancellation with immediate server cancellation failed with `response terminated before the shutdown deadline` and exited in 0.422 seconds. Omitting the deadline-triggered `Close` failed with `response did not terminate after the shutdown deadline` and exited in 3.441 seconds, including bounded cleanup. Both probes were removed; `http3/server.go` is unchanged.

Focused command: `TIMESCALE_FACTOR=3 go test -count=1 -timeout=30s ./integrationtests/self -run 'Test(GracefulShutdown|HTTPShutdown|HTTP3Listener.*(Closing|Shutdown))' -version=1` (repeat once with `-version=2`). Both versions passed. Final race, full-suite, static and hosted outcomes are recorded in the PR receipt after review.
