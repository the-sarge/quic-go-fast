# Packet-pacing fixture diagnosis (#103)

The behavioral owner was the recovery input fixture in `TestConnectionPacketPacing`, not the transport pacer. `datagramQueue.Add` already queues a sending notification. When `Conn.run` consumes it before the test's explicit `scheduleSending`, the latter causes a second dispatch at the same controlled time. Dispatch correctly asks recovery for its allowance. The old ordered mock returned its next `SendAny` regardless of time, permitting the third packet before its own deadline. Production `sentPacketHandler.SendMode(now)` consults `HasPacingBudget(now)` on every dispatch; no runtime change is needed.

The secondary deadlock had a separate owner: test teardown followed fatal assertions. A timing failure left the connection and TLS goroutines inside the synctest bubble. Teardown now uses a defer inside that bubble. The early-exit subtest exercises destruction with two packets sent, a pending pacing timer, and one queued DATAGRAM. Review exposed a second teardown path: an over-count send mock could abort the connection goroutine before it reported completion. Packet-count assertions now run on the test goroutine, and the send callback records and releases each buffer. Restoring the old call-count allowance across all three scenarios now produces ordinary assertion failures (three packets instead of two), with no teardown deadlock.

The corrected fixture grants two initial sends, retains a deadline established by the second send, and grants the third only when the supplied clock reaches that deadline. A repeated recovery query cannot consume or postpone the allowance. All packet construction and recovery registration remain real. The test still checks equal first/second timestamps and an exact 50 ms third interval, and now also checks that only two packets exist after the forced early wakeup. After packet three, another fixed 50 ms pacing deadline preserves the original final timer-to-`SendNone` check. This happens after the exact third-packet interval assertion.

## Baseline and diagnostic evidence

Source baseline: `534a10f71dec1def08df09811116bb948a51606b`. Local host: Go 1.27.0 darwin/arm64. Linux reproduction: Go 1.27.1 linux/amd64, Docker image `golang:1.27.1` at digest `sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa`, Linux x86_64 kernel 7.0. The image uses Debian and runs under amd64 emulation on an arm64 Docker host; it matches CI's Go version and architecture but does not claim to be the hosted Ubuntu runner. All following pacing runs set `TIMESCALE_FACTOR=10`.

- Unchanged macOS baseline: `go test -run '^TestConnectionPacketPacing$' -count=10000 -cover -shuffle=1788889588824808975 -timeout=90s .` passed 10,000 repetitions.
- Unchanged Linux baseline with the same command: failed before the 10,000-repetition bound with the exact missing 50 ms interval and teardown deadlock. A second run with `-json` recorded 5,643 passes and one failure among 5,644 started repetitions, then the deadlock terminated the process. This stopped sample is an observed count, not an estimate of a stable failure probability.
- The retained `force-wakeup.patch` adds only `synctest.Wait()` before the explicit wakeup, making the competing goroutine order deterministic without advancing fake time. Five separately launched Linux test processes all failed (5/5) with the missing interval and deadlock; a macOS launch also failed.
- Temporary tracing of the forced order recorded `SendAny`, `SendAny`, pacing deadline `t+50ms`, then `SendAny` at `t`, followed by the missing interval. This distinguishes incorrect fixture input from runtime bypass or premature timer advancement. Tracing was removed.
- Red/green: restoring the old sequential allowance in the new `wakeup while paced` subtest failed with `should have 2 item(s), but has 3`. Deferred teardown completed without a deadlock for that named subtest. Review then exercised the same old-allowance mutation across all three scenarios: before moving count assertions it exposed a deadlock in the early-exit scenario; afterward, both affected scenarios failed cleanly without any deadlock. The clock-based allowance passed. This is the single bounded mutation; it is diagnostic evidence, not a maintained mutation framework.
- Corrected Linux test: the original bounded command passed 10,000 repetitions, each running all three scenarios (30,000 subtests). Corrected macOS race check: `go test -race -run '^TestConnectionPacketPacing$' -count=1000 -shuffle=1788887233505157651 -timeout=90s .` passed all 3,000 subtests.

## Replay the baseline failure

Use a dedicated worktree at the baseline above to keep the rest of the source pinned. First run the unchanged command above. To force the wakeup order, apply the retained one-line patch in that disposable worktree, then run `TIMESCALE_FACTOR=10 go test -run '^TestConnectionPacketPacing$' -count=1 -timeout=10s .`. A failing run exits nonzero and reports the exact 50 ms mismatch followed by the original deadlock. Running five separate processes retains a bounded failure count despite the panic terminating each process.

For Linux, mount that worktree read-only and run:

```sh
docker run --rm --platform linux/amd64 \
  -v "$PWD:/src:ro" -w /src -e TIMESCALE_FACTOR=10 \
  golang:1.27.1@sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa \
  go test -run '^TestConnectionPacketPacing$' -count=1 -timeout=10s .
```

Module download and compilation are outside the test timeout. Reusable module/build cache volumes can reduce startup cost. The patch deliberately targets the baseline test; the final test incorporates the same forced order as a named regression.

## Review and certification contract

The normative contract is the [issue #103 agent brief](https://github.com/the-sarge/quic-go-fast/issues/103#issuecomment-5589687925). The representation domain is the existing connection scheduling fixture with a controlled clock and recovery inputs. The guarantee is example-level coverage of the specified packet sequence across concurrent and forced non-timer wakeups, plus controlled early-exit cleanup. These are verification aids and diagnostic documentation; production code, congestion policy, APIs, and the architecture program are outside this change.

The terminating plan is baseline/forced reproduction, the single old-allowance red/green regression, focused scheduling/pacing race checks, final-candidate Linux and macOS bounded repetition, local full tests and static checks, and the repository's existing hosted gates. Review has one initial RAS round and at most one replacement following accepted fixes. This repository has neither `task preflight` nor a draft-gated `ci.yml`; its existing unit, integration, lint, and cross-compilation workflows provide the hosted gates. Exact candidate SHA, final local results, review dispositions, and hosted run results are recorded in the PR before merge. No benchmark campaign or additional mutation/platform cross-product is required for this test-only correction.
