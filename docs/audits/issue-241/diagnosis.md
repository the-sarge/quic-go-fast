# Issue 241: macOS DialAddr socket-rebind diagnosis

## Result

The historical `TestDial/DialAddr` socket-rebind failure was not reproduced. Its cause remains unresolved. One instrumented seeded root-package run and one isolated `TestDial` experiment passed on 2026-09-15 UTC. The original address-dial sockets reported closed, transport receive shutdown completed before cancellation returned, and each first rebind succeeded. A separate socket-ownership control demonstrated `EADDRINUSE` with an already-closed original socket and an explicitly held replacement socket. That control demonstrates the assertion's ambiguity, not the historical cause.

This completes the bounded diagnosis requested by [issue #241's authoritative brief](https://github.com/the-sarge/quic-go-fast/issues/241#issuecomment-5640794686), dispatched through OmniFocus task `jDiugS79fwb`. It does not fix or close #241. Budget consumed: one of one additional seeded root-package runs and two of three targeted experiment slots. There is no failure or concrete predecessor signal to guide a third experiment; that slot remains unused. The original failing hosted environment and contemporaneous socket observations are unavailable, so further unchanged attempts would not establish causality.

All temporary source instrumentation was removed. The final change contains only this report and frozen evidence. There are no product, fixture, timeout, quarantine, CI, API, wire, socket-ownership, or incoming-storage lifetime changes. No hosted run was retried and no broad stress campaign was performed. #151, #169, #188, and #317 remain separate. In particular, #317 fails the global transport-running assertion for caller-owned `Dial` on Ubuntu; it is not this address-owned socket-rebind failure.

## Preserved evidence and revision reconciliation

The [hosted failure](https://github.com/the-sarge/quic-go-fast/actions/runs/34647415855/job/103421413954) used push checkout `b0e09fedae109486550e541ad3ea3cb462864680`, Go 1.27.1 darwin/arm64, non-race, `TIMESCALE_FACTOR=10`, root shuffle seed `1789160702392383000`. The unit command was `go test -v -shuffle on -cover -coverprofile coverage.txt ./...`, after removing integration tests. Root-package result: FAIL 3.636s; `TestDial/DialAddr`: FAIL 2.00s at `client_test.go:82`, `Condition never satisfied`. The fixture uses numeric IPv4 loopback. The server had received a datagram, cancellation had been requested, and the dial result satisfied `context.Canceled`. The two seconds are the scaled 200ms rebind assertion budget, not a dial deadline. The bind error and socket addresses were not captured.

The [independent pull-request run](https://github.com/the-sarge/quic-go-fast/actions/runs/34647425290) passed with the same reported PR head; that metadata does not prove its possibly synthetic merge checkout was identical. The prior local exact-source attempts, both Go 1.27.1 darwin/arm64 and timescale 10, were:

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=10 go test -v -shuffle=1789160702392383000 -cover -count=1 -timeout=2m .
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=10 go test -v -run '^TestDial$' -shuffle=1789160702392383000 -cover -count=1 -timeout=30s .
```

They passed in 1.207s and 0.251s respectively; the target passed in 0.00s in each. They were not repeated at the historical checkout in this investigation. The original evidence directory `/Volumes/worktrees/quic-go-fast/triage-dial-241-evidence/` was left unchanged. Byte-identical copies are retained here:

| Artifact | SHA-256 |
| --- | --- |
| [Hosted log](prior-hosted-failure.log) | `df26dbc602b1e9b27b3eb4c5cc349a9f5867531db604b5a5b10288fa06146e4b` |
| [Prior seeded root](prior-seeded-root.log) | `0a1dd6f5c469c0b752be8fa2b0142eddd46c77653b5fb55d696f8a7b0781f597` |
| [Prior focused dial](prior-focused-dial.log) | `a826d79371ae102d3bc7a8179cdb0e102e7c315c65b0e45e47d4fe7606ccd41a` |

The new worktree is `/Volumes/worktrees/quic-go-fast/diagnose-dial-241`, branch `codex/diagnose-dial-241`, pinned base `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7` (verified current main before investigation). Local platform: macOS 26.6.2 build 25G83, Darwin arm64, Go 1.27.1, CGO enabled, non-race, timescale 10; no explicit `GOMAXPROCS`, `GODEBUG`, or `QUIC_GO_DISABLE_GSO` override. See [filtered build environment](environment.txt), [platform](platform.txt), and per-command receipts. Hosted cross-package concurrency, background processes, scheduling, ports, and random protocol inputs were not recreated.

[Source reconciliation](source-reconciliation.diff) confirms that `client.go`, `client_test.go`, `transport.go`, `quic_test.go`, and `sys_conn.go` are unchanged from the failing revision through the pinned base. Later changes include Darwin `sendmsg_x` batching (#257), send-queue batching work, coalesced receive delivery refactoring (#296), Windows offloads, and dependency upgrades (including `x/net` 0.56.0 to 0.59.0 and `x/sys` 0.47.0 to 0.48.0). Darwin still reports GRO disabled; the coalesced-delivery refactor does not make Darwin use GRO. No relevant change establishes a repair of this failure. This current-source execution is not an exact historical replay.

The [order comparison](order-comparison.json) finds 463 prior versus 503 current top-level root tests. The same shuffle seed therefore does not reproduce the historical order: the old immediate predecessors were `TestMTUDiscovererReset`, `TestHandshakeMTUFallbackPhaseAndPath`, and `TestEmissionResultRecoveryOutcome`; the current ones were `TestReceiveStreamReadData`, `TestConnectionMaxUnprocessedPackets`, and `TestConnectionIdleTimeout`. Passing either order cannot identify or exclude interference.

## Predictions and instrumentation

The ranked hypotheses were observation priorities, not established causes:

1. **Original owned socket remains open.** Predict an accessible captured original socket after cancellation, a missing or failed close, and failed rebinds. Correlate the socket factory's actual object and address with `Transport.Close`, receive-loop closure, and a read-only raw-control probe after the dial result.
2. **The original closes but the address is occupied again.** Predict a closed original handle alongside `EADDRINUSE`; proving another owner requires a contemporaneous owner observation. Experiment 2 changes only replacement ownership at a fixed address to test whether the assertion can distinguish those states.
3. **Other bind failures or stalled execution.** Predict a different concrete bind error, a slow bind, or missing/late retry callbacks. Record each bind's start, end, target, error type, and duration. The historical Boolean alone cannot separate these possibilities.

The [temporary patch](instrumentation.patch) plus [temporary helper](diag_241.go.txt) retain the existing factory's network/address request and the real socket implementation. They capture only address-owned cases, record the socket pointer, descriptor at creation, actual wildcard address, server/source addresses, transport identity, all relevant close results, and receive completion. They retain the original socket for observation and use `SyscallConn().Control` with an empty callback after `errChan` receipt to observe whether its Go handle is closed without duplicating its descriptor or modifying a deadline. Successful result-channel receipt synchronizes the captured pointer's use.

Instrumentation leaves assertions, deadlines, time scale, and cleanup unchanged. Logging, retained references, map lookups, and raw-control observation can perturb scheduling and finalizer eligibility, so a pass with probes cannot prove the uninstrumented path is fixed. The helper is archived as `.go.txt`; it is not compiled by normal builds. The patch is a frozen audit record, not permanent diagnostics or an approved future capture deployment.

## Bounded executions

### Seeded root package

Prediction: if the socket remains open or rebinding fails, the correlated original-socket and transport milestones will narrow the ownership boundary; callback entry/exit distinguishes concrete bind failures from absent or slow attempts. Changed variables versus the prior exact-source run are the reconciled current revision and the observation patch; this is not a single-variable comparison to the historical failure.

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=10 go test -v -shuffle=1789160702392383000 -cover -count=1 -timeout=2m .
```

Result: exit 0; root PASS 4.252s, coverage 91.6% of the instrumented statements; all `TestDial` subtests PASS 0.00s. [Complete log](instrumented-root.log), [receipt](instrumented-root-receipt.json).

| Observed event, UTC 2026-09-15 | DialAddr | DialAddrEarly |
| --- | --- | --- |
| Original socket; received source | `[::]:55174`; `127.0.0.1:55174` | `[::]:52375`; `127.0.0.1:52375` |
| Server | `127.0.0.1:63331` | `127.0.0.1:60409` |
| Cancellation requested | `00:11:54.202588` | `00:11:54.203448` |
| Receive channel closed | `00:11:54.202652` | `00:11:54.203498` |
| Successful owned close recorded | `00:11:54.202656` | `00:11:54.203502` |
| Dial result received; original raw control reports closed | `00:11:54.202684` | `00:11:54.203521` |
| First rebind succeeds | `00:11:54.202719` (27.041µs) | `00:11:54.203559` (28.916µs) |

These are probe timestamps, not kernel syscall timestamps. The receive goroutine can log its duplicate-close error before the first caller logs its successful close result. Creation observed an IPv6 wildcard socket for the `udp`/IPv4-zero request, while the datagram source and rebind target were IPv4 loopback with the same port. That is observed address mapping, not evidence of a DNS problem or a bind-family defect.

### Experiment 1: isolated dial

Changed variable relative to the new instrumented root run: test selection only; same source, probes, toolchain, seed, timescale, coverage, count, and process timeout. Prediction: reproduction without predecessor tests would show they are unnecessary; one isolated pass cannot establish or exclude a predecessor dependency.

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=10 go test -v -shuffle=1789160702392383000 -cover -count=1 -timeout=2m -run '^TestDial$' .
```

Result: exit 0; package PASS 0.239s; all four subtests PASS 0.00s. `DialAddr` used `[::]:59643` with server `127.0.0.1:57877`; `DialAddrEarly` used `[::]:62404` with server `127.0.0.1:56762`. Both original handles reported closed after receive completion, and first rebinds succeeded in 24µs and 12.708µs. There were no failed bind attempts in either QUIC execution. [Complete log](isolated-dial.log), [receipt](isolated-dial-receipt.json).

### Experiment 2: address-ownership control

This single-invocation standard-library harness does not modify or execute the repository fixture. Changed variable within the control: acquisition and release of a replacement socket at the same address while the original remains closed. Prediction: if rebind measures occupancy rather than original ownership, it fails with `EADDRINUSE` while that replacement is held and succeeds after its closure.

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=10 go run /Volumes/worktrees/quic-go-fast/diagnose-dial-241/diag241_control.go
```

Result: exit 0. Original `[::]:54249` was closed. Rebinding `127.0.0.1:54249` first succeeded; with explicit replacement owner `0x456734382038` held, the same bind failed with `*net.OpError`, errno 48 (`EADDRINUSE`), `listen udp 127.0.0.1:54249: bind: address already in use`. Original raw control still reported `net.ErrClosed`. After closing the replacement, rebind succeeded. [Source archive](socket-control.go.txt), [complete log](socket-control.log), [receipt](socket-control-receipt.json). The temporary executable source was removed.

This is causal evidence for the control's injected occupancy, not a reproduction of the historical two-second failure, proof of spontaneous address reuse, or evidence of another process's involvement. No third experiment was run.

## What the close trace means

At the pinned source, `Transport.doDial` cancels the connection and waits for its result channel. The single-use connection goroutine calls `Transport.Close` before sending that result. `DialAddr` then calls `Transport.Close` again on the returned dial error. Separately, the receive goroutine also closes its owned raw connection when listening stops. The passing observations show a successful original close, a receive-goroutine duplicate-close error, completed receive shutdown, and another duplicate-close error from the outer address helper.

Thus a captured `use of closed network connection` close error can follow successful cleanup and is not by itself a leak or the failure's cause. Conversely, `Transport.Close` can return early when `Conn.Close` errors, before its explicit wait for `listening`; that source path alone does not prove it happened before shutdown in the hosted failure. Our observed outer closes all saw `listening_closed=true`. No close-order repair is justified by these passing traces.

## Smallest useful future-failure capture and proposed correction boundary

The next useful evidence is one natural occurrence of the original failed assertion, with a small bounded record attached to that failure:

- Original socket identity, actual local network/address/port and descriptor at creation; server address and received datagram source. Verify source port correlation rather than assuming the first datagram came from this dial.
- Dial cancellation result and original-handle closed state, correlated with each socket-close result and the relevant transport's receive-channel completion. A process-wide goroutine Boolean is not an ownership identifier.
- Retry attempt count and first/last attempt times; concrete `net.OpError` fields, wrapped errno, address family/target, and bind duration. Record callback entry as well as exit to expose a stuck attempt.
- If `EADDRINUSE` persists despite a closed original, capture the owner of that exact UDP port while the conflict exists, where platform permissions allow. A late port/process snapshot or a reused descriptor number cannot identify the historical owner. If there are no callbacks or long gaps, capture contemporaneous scheduling/goroutine evidence before assigning a scheduling cause.

A capture-only follow-up should preserve the original assertion and all deadlines, record a finite amount of data, and have an explicit event/run cap. It requires separately agreed scope; this report does not install it.

The smallest correction depends on that evidence. If the original owned socket remains open, target the demonstrated cancellation/close boundary with a regression that retains the actual owned socket and observes its closure, while preserving caller-owned sockets. If a replacement owner is demonstrated, consider an ownership assertion against the original captured socket rather than exclusive access to a released port; require regression evidence that detects missing close even when finalizers run, and preserves `Dial`/`DialEarly` ownership. If a different bind error or callback stall is observed, address that demonstrated condition. No timeout increase, generic shutdown rewrite, or assumed infrastructure repair is proposed. Passing attempts do not resolve the bug.

## Validation and archive contract

The one full root-package execution and the isolated file-related execution both compiled and passed, including Go test's normal vet checks. The separate control passed. No additional `./...` or integration run was made: this diagnosis's explicit finite execution budget governs over the generic implementation workflow's full-suite advice, and no production/test code remains changed. Cleanup compared the active patch and helper to their archived bytes before restoring the two modified files from the pinned base. The final source diff is empty and no active `.go` source contains `diag241` or `[DEBUG-241]`.

All artifacts in [SHA256SUMS](SHA256SUMS) are frozen evidence. The manifest covers the captured inputs, patch/helper, commands, environments, history, order comparison, and complete outputs; this explanatory report and later review notes are excluded from that manifest. The archive is not a maintained tool or a test dependency. Review uses the pinned base as its comparison point and checks standards and brief compliance independently.
