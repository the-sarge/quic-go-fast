# Issue #317: bounded transport-observer diagnosis

## Result

The seeded unit suite reproduced the specific `TestDial/Dial` assertion failure. The exact triggering profile identifies **transport 14 from the earlier `TestListenAddr`**, at `127.0.0.1:55526`, still executing `Transport.listen` during receive-buffer allocation. The just-cancelled dial transport was **15**, at `127.0.0.1:58722`; its receive loop and completion channel had finished before the caller received `context.Canceled`. This directly establishes cross-test transport overlap in this local reproduction, rather than a failed shutdown of that dial transport.

Source inspection identifies the boundary: `TestListenAddr` defers `Listener.Close`, which requests shutdown through `Transport.closeServer` without joining the transport's receive goroutine. The global observer in a later test can consequently still see that earlier transport. The same test ordering appears in the hosted log, but its original stack was not retained, so attribution of the historical failure remains an evidence-supported inference rather than a direct observation.

This is the bounded diagnosis deliverable for [issue #317](https://github.com/the-sarge/quic-go-fast/issues/317). No repair is implemented or validated. No timeout, assertion, socket ownership, production behavior, or CI policy is changed in the delivered source. The bug remains open for a separate confirmation and fixture-repair scope.

## Source and preserved evidence

The failing checkout `b1f16ae0edf3f23483e2009d8196912c39c2eac5` and this investigation's baseline `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7` have the same entire Git tree, `ad4ae2c7788f29de890c24ac18a522c5473b12fa`. There are therefore no intervening source differences to reconcile. The dedicated worktree is `/Volumes/worktrees/quic-go-fast/diagnose-317`, branch `codex/diagnose-317`; the initiating checkout was left unchanged. Each new execution used that baseline plus the patch specified below, copied into a disposable container runtime directory. Git metadata, documentation, and `integrationtests` were excluded from the runtime copy. Excluding integration tests matches the hosted unit workflow.

The hosted log `/Users/josh/diagnostics/http-idle-retry-2026-09-14/journal-failed-unit.log` still hashes to `03ca64cba38be836f008553c06cfb052ac9cf03bbfbb158cd7ee4b259bdc346f`. Every entry in the prior triage directory's `SHA256SUMS` was rechecked successfully; [prior-evidence-check.json](prior-evidence-check.json) retains the results. No previous evidence was edited. Original new logs remain at `/Users/josh/diagnostics/diagnose-dial-317-2026-09-15/`; compressed copies and diagnostic patches are archived here, with [SHA256SUMS](SHA256SUMS). These scripts and patches are frozen investigation evidence, not maintained tools or permanently deployed diagnostics.

## Environment and finite budget

All new test attempts used the already available image `sha256:20dfa9aeeb42795279e3e73318bdd425f1fe09e3cebed05484fce1a3ca9aef20`: Ubuntu 24.04.4 userland, Go 1.26.8 linux/amd64, `GOAMD64=v1`, `CGO_ENABLED=1`, `GOTOOLCHAIN=local`, `TIMESCALE_FACTOR=10`, non-race, UID 1001, and default `GOMAXPROCS`. The Docker host is macOS on ARM64, so amd64 execution is emulated; the container uses LinuxKit 7.0.12. This differs from the hosted Ubuntu 24.04.5 / Linux 6.17 runner and does not reproduce its scheduling, load, or services. The existing native-amd64 VM alias `dev-agent-imacpro` was checked once; SSH exited 255 because the Lima `dev-agent` instance did not exist. No infrastructure was provisioned or started.

The investigation consumed three targeted experiment slots, including the setup failure, and one seeded suite slot. Each Go command had an outer `timeout --signal=TERM --kill-after=5 595` deadline, bounding execution to ten minutes including forced termination, and an inner `-timeout=9m`. The environment log's displayed `timeout 600` prefix is a shorthand logging error; [run.sh](run.sh) preserves the exact executed timeout arguments. There were no unchanged hosted-check reruns or additional campaigns.

| Attempt | Declared purpose | Diagnostic patch | Result |
| --- | --- | --- | --- |
| E1 | Repeat the exact `Dial` subtest up to 1,000 times, stop on first failure, and retain any matching observer snapshot. | [observer.patch](observer.patch) | Exit 1 during setup: shared module/build caches were not writable by the non-root test user. No tests ran. This consumed a targeted slot. |
| E2 | Execute the same focused experiment with private writable container caches. | [observer.patch](observer.patch) | Exit 0; 1,000 `TestDial/Dial` repetitions passed. Root test time 0.679s, excluding compilation. No matching profile was captured. |
| E3 | Exercise all four dial variants 200 times with transport attribution and lifecycle evidence, stopping at the first failure. | [lifecycle.patch](lifecycle.patch) | Exit 0; 800 subtests passed. Root test time 0.635s, excluding compilation. No matching profile was captured. |
| Suite | Exercise shuffled unit-test interactions with the same lifecycle capture, once, at the recorded root seed. | [lifecycle.patch](lifecycle.patch) | Exit 1; reproduced `TestDial/Dial` at instrumented `client_test.go:106` (original line 104). Root package 4.785s; other dial variants and all other packages passed. Exact triggering profile retained. |

Exact Go invocations, in execution order:

```sh
go test -v -run '^TestDial$/^Dial$' -shuffle=1789428569339240250 -cover -count=1000 -failfast -timeout=9m .
go test -v -run '^TestDial$/^Dial$' -shuffle=1789428569339240250 -cover -count=1000 -failfast -timeout=9m .
go test -v -run '^TestDial$' -shuffle=1789428569339240250 -cover -count=200 -failfast -timeout=9m .
go test -v -shuffle=1789428569339240250 -cover -coverprofile=/evidence/coverage.txt -count=1 -timeout=9m ./...
```

Each attempt has its own environment/command log, complete output, and exit-status file. [launches.md](launches.md) records the Docker invocation details. The explicit shuffle seed applies to every package locally; the hosted `-shuffle on` generated independent package seeds, and only its root-package seed is being reproduced. These were coverage-enabled builds and thus also compiled/typechecked the exercised packages. No separate test campaign or integration suite was added beyond the issue's budget.

## What the instrumentation observes

Both patches preserve the existing `pprof.Lookup("goroutine").WriteTo(&b, 1)` snapshot and the exact `strings.Contains` predicate. When it is true, the observer prints that same buffer before returning the same Boolean. There is no replacement snapshot, polling, delay, or assertion relaxation. The suite exercised the true-output branch once in the root package. [observer-profile.txt](observer-profile.txt) is the exact emitted buffer extracted from that run, including its labeled matching stack and assertion caller. No retained profile from the original hosted failure exists.

The lifecycle patch assigns an atomic ID once during transport initialization, logs its pointer, socket address where the socket is a concrete `*net.UDPConn`, ownership flag, and creation stack, and labels the `listen` call with its ID and socket using `pprof.Do`. Unknown socket implementations are recorded by type without invoking mocked `LocalAddr` methods. Test markers correlate the test name and peer-observed client address with cancellation and receipt of the dial result. IDs are process-local and distinguish separate transport lifetimes even if pointer addresses are reused; wildcard local addresses on owned sockets can be correlated by port with the peer-observed loopback address.

Lifecycle markers surround the receive call, completion-channel close, and `Close` exit. `receive-returned` means `listen` and its deferred cleanup have returned. `Close-returning` is a deferred exit marker; it runs after the existing deadline-reset defer on the caller-owned socket path, but immediately before the function returns to its caller. `dial-result-received` is the stronger caller-side observation. Each event probes whether the completion channel is already closed without waiting. Marker writes and state probes are not an atomic trace: a receiver can run after channel closure but before the `completion-after-close` log is emitted. Log ordering alone must not be treated as a new synchronization guarantee.

In E3, [targeted-summary.json](targeted-summary.json) counts 800 initialized transports, 800 receive returns, 800 before/after completion-close pairs, and 1,600 `Close` exit markers, all observing completion. Each dial variant passed 200 times. Two closes per transport are consistent with the single-use connection worker closing after `Conn.run` and the convenience dial wrapper closing again on error. These are observations of successful instrumented executions, not evidence explaining the hosted failure.

Instrumentation changes scheduling: atomics, allocations, stack collection, profile labels, and synchronous stderr writes add work around cancellation and shutdown. The lifecycle wrapper also changes stack shape. Even observer-only instrumentation adds a Boolean branch, and a true result adds output before assertion failure. Passing instrumented runs cannot exclude a timing-sensitive failure that instrumentation suppresses, and the reproduced overlap may have been prolonged by diagnostic work. Instrumentation did not add a transport, remove a wait, or alter the Boolean predicate. Temporary Go changes are removed from the delivered source; only the patches and output are retained.

## Boundary established by source inspection

On the caller-owned-socket path, `Dial` calls `Transport.Close` before returning a dial error. `Close` initializes the transport, closes its handlers, sets the read deadline, waits for `listening`, then resets the deadline. The initialization goroutine closes `listening` after `listen` returns; `listen` also completes its deferred receive-buffer and non-QUIC packet cleanup before returning. There is no separate process-wide transport registry whose unregistration the observer checks. Connection-ID removal and closed-handler retirement are distinct from this receive-loop completion boundary.

The observer searches the process-wide profile for a function-name substring. Its original failed Boolean does not attribute a stack to the just-cancelled dial transport, another fixture, or a particular point in shutdown. Static control flow and the passing event sequences do not establish that the historical assertion was wrong, nor do they prove a transport leak. Issues #241, #151, #169, and #301 remain separate.

## Reproduction evidence and causal boundary

The [suite excerpt](suite-excerpt.txt) preserves the creation stack and events from `TestListenAddr` through the failing assertion. The matching profile labels are `transport317=14` and `socket317=127.0.0.1:55526`. Its stack is `Transport.listen` → `oobConn.ReadPacket` → `getCoalescedPacketBuffer` → `sync.Pool.Get` → the pool allocator. This is an actual receive-loop stack, not merely a similarly named function or a diagnostic label containing the observer's substring.

The initialization stack for transport 14 points to `TestListenAddr`, `server_test.go:229`. That test passed before `TestDial` started. Transport 15's cancellation markers, receive return, completion close, two `Close` exit markers, and caller-side result receipt all precede the observer call. No transport-15 listen stack appears in the failing snapshot. Transport 14's receive-return and completion-close events occur after the assertion failure is reported and before the next dial variant starts. The observed overlap is transient; it is not evidence of an indefinitely leaked transport.

At baseline, `TestListenAddr` creates a listener and defers `ln.Close()` (`server_test.go:219–232`). `Listener.Close` delegates to `baseServer.Close`; `baseServer.close` waits for its server worker and invokes `onClose`, which is `Transport.closeServer` (`server.go:146–147,408–429`; `transport.go:235,496–510`). With no handlers on this single-use transport, `closeServer` calls `maybeStopListening`, which sets the socket read deadline (`transport.go:588–592`). This path does **not** wait for `Transport.listening`. The transport's initialization goroutine separately waits for `listen` to return, closes its owned socket, and closes the completion channel (`transport.go:427–434`). Thus listener-worker completion and transport-receive completion are different boundaries. Connection-ID unregistration is not the missing observation here.

The hosted log has `TestListenAddr` followed by the same intervening tests and then `TestDial/Dial`. This correspondence and the identical source tree support the same explanation for the hosted symptom, but do not prove its unseen transport identity. The capture does not establish an allocator performance defect, leaked incoming storage, or a public `Listener.Close` contract violation.

## Ranked explanations and next scope

The reproduction occurred in the final allowed attempt. No experimental budget remained for repeated reproduction, a minimized execution, or a one-variable intervention; no additional tests were run. The following falsifiable ranking is a proposed follow-up, not a tested correction:

1. **Earlier listener fixture returns before its transport exits.** Directly observed in the local capture and supported by the shutdown call graph. Prediction: explicitly joining that fixture's own receive completion before it returns prevents transport 14's equivalent from appearing in the later assertion, without changing `Dial` or the observer.
2. **The failing dial's transport survives cancellation.** Contradicted for this capture by the matching ID and completion events. A future snapshot matching the failing dial's own ID before completion would reopen this explanation.
3. **Observer text/profile discrepancy.** Contradicted for this capture by the real labeled `listen` frame. A future failure without a real receive-loop frame in the same predicate-producing buffer would warrant investigating it.

The next narrowly scoped work is to confirm and repair `TestListenAddr`'s fixture-completion boundary. A candidate minimized scenario is `TestListenAddr` followed by `TestDial/Dial` at the recorded seed; this pair has not yet been executed in isolation. A useful regression seam is the real listener creation/closure path, with a controlled receive worker or an explicit per-transport completion observation demonstrating that fixture teardown has finished before the global assertion. Do not substitute sleeps or globally relax `areTransportsRunning`. Preserve `Listener.Close`'s established-connection semantics and the incoming-buffer disposal boundary; a change to the public close contract would require its own justification.

For confirmation, prefer an already available native Ubuntu amd64 environment with Go 1.26.8, coverage, time scale 10, and the original root seed. Declare a fresh finite budget. First minimize the identified fixture/dial sequence, then vary only the fixture completion synchronization. A repair must have a failing regression at that seam before implementation and must rerun the original seeded suite afterward. No deterministic regression has yet been demonstrated.

The smallest future-failure capture remains [observer.patch](observer.patch), which retains the exact predicate-producing profile rather than a later snapshot. [lifecycle.patch](lifecycle.patch) provides the attribution used here if another occurrence needs transport identity and caller correlation. These are archived proposals only; temporary instrumentation has been removed from working source. The bounded diagnosis is complete, with confirmation/minimization and any repair explicitly left to a separate scope.
