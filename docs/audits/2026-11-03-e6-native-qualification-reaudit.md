# E6 #1020 native qualification re-audit

## Trigger and preserved state

Execution preflight for [E6 #1020](https://github.com/the-sarge/quic-go-fast/issues/1020) at `1fd1ed9b7b31c559884dd1e453c4fdfeaa240ce3` found that the slice's named location can't host its fixture. No implementation code exists. Child #1020 stays open and this changes no product code, completion state or frontier.

| Accepted assumption | Current source fact | Why it cannot be avoided |
| --- | --- | --- |
| The fixture lives in `integrationtests/self` | That package is `self_test`, outside `quic`. `Transport.configureNativeIO` and `waitNativeIOStopped` are unexported (`native_transport.go:25`, `:97`), and R19 owns their public activation | Exporting them is the production hook the slice forbids. A `go:linkname` reaches the two methods but no request-level state |
| Each native class is reachable from the public write path | `Transport.WriteTo` always takes admission+B (`native_response.go:52-64`, `native_endpoint.go:451-452`). Expired-before-dispatch and queued expiry while another request is current need an earlier per-request deadline, which only `endpointRequest.deadline` carries (`native_endpoint.go:68`) | Expired-before-dispatch has no public route at all. Queued expiry behind a stalled current request can occur through class turns or a race, but not deterministically in a fixed run budget |
| strace can sequence injected errors on one syscall | A second `inject=sendmsg` rule replaces the first. Probe: rules `EIO:when=1` and `EAGAIN:when=2+` gave a successful first call | A fallback trigger followed by EAGAIN can't both be injected |
| Injection counts are process-wide | strace counts `when=` per tracee, and the writer goroutine isn't bound to a thread | A numbered injection needs one known thread |
| A kernel GSO `EIO` can trigger the fallback | Three probes on 7.0.0-30-generic returned success for a segmented send: `lo` refused `tx off`, a dummy device with `tx off`, and an ESP transport transform | No real trigger for that branch exists on the selected image |

The binding rule makes an effect outside the named files a slice stop until a scoped re-audit establishes that it fits the existing outcome and one-PR budget. This one changes where a test lives and where one trigger comes from, not who owns anything. The transport test suite still owns the fixture, and no production file changes.

## Method probes

A throwaway program, not the fixture, checked the method on `minimax` (Ubuntu 26.04.1, Linux 7.0.0-30-generic x86_64, strace 6.19, Go 1.27.3). It used a real UDP socket and `strace -f -qq --seccomp-bpf -e trace=sendmsg`. These seven process invocations charge the single program ledger as setup attempts, reconciled in E6's receipt.

| Case | Observed |
| --- | --- |
| `inject=sendmsg:error=EAGAIN` on a 200 ms deadline | One injected `sendmsg`; a goroutine stack sampled mid-wait contained `internal/poll.runtime_pollWait`, `(*pollDesc).waitWrite`, `(*FD).WriteMsgInet4` and `net.(*UDPConn).WriteMsgUDPAddrPort`; the write returned `os.ErrDeadlineExceeded` 1.4 ms after the deadline |
| nftables `meta l4proto udp drop` on output, in a disposable network namespace | Real `sendmsg` returned `EPERM` without injection |
| Locked OS thread, `inject=sendmsg:error=EAGAIN:when=3+` | Calls one and two reached the kernel and returned byte counts, and call three was injected. The count follows the thread |

Polling and deadline traversal is therefore observable without production hooks, which clears the slice's first stop condition on Go 1.27.3. Go 1.26.10 is checked by its own runs.

## Accepted design

Keep child #1020, one fixture PR, 12 native instances and the single ledger.

**The fixture is a build-tagged root-package test.** `native_qualification_test.go` is `package quic` under `//go:build nativequal && linux && amd64`, so ordinary test runs never compile it. It uses the real endpoint on real UDP sockets, which is the "production endpoint retains lifetime authority" the slice requires: `newNativeEndpoint` and `Transport.configureNativeIO` for every class but the retry class. That class needs a test wrapper around the exact socket, which only `startNativeEndpoint`'s `endpointNativeIO` parameter accepts, and it supplies its own `writeState`. No file outside tests and the driver changes.

**The driver and accounting live beside the R1 peer control.** `interop/multipath/native_qualification.py` builds one test binary per Go version, runs each class in its own process, and fails on a missing prerequisite, a skip, a missing or duplicate class, or missing traversal evidence. Accounting regressions run with the existing `test_*.py` discovery, without a native run. The receipt records the kernel, both toolchains, a hash of each toolchain's `internal/poll/fd_unix.go`, the strace version and every charge.

**One class per process, as the ledger counts it.** Each class is an instance on each of Go 1.26.10 and 1.27.3, at B = 100 ms:

| Class | Injection | Observation |
| --- | --- | --- |
| Expired-before-dispatch | None | A request with a past deadline completes unsubmitted without reaching the socket, and a successor with a future deadline reaches the receiver |
| EAGAIN-to-deadline | `EAGAIN` on every `sendmsg` | Injected trace line, mid-wait stack through `writeEndpointRequest` and `waitWrite`, deadline error and an uncertain result |
| Retry and fallback preserving the absolute deadline | Real `EPERM` from nftables in a disposable namespace, then `EAGAIN:when=2+`. The GSO `EIO` trigger is simulated by the test wrapper | Two requests in one process. The first-send permission retry reaches the poll wait and returns at its original deadline. Then the GSO attempt gets a wrapper-returned `EIO` with no syscall, and its fallback segment reaches the same poll wait and returns at its own request's deadline. Numbered injection needs one thread, so the wrapper locks the writer goroutine to its OS thread on the first `SetWriteDeadline` and otherwise forwards to the exact socket |
| Independent queued expiry | `EAGAIN` on every `sendmsg` | A queued request with an earlier deadline completes unsubmitted while the stalled current request's stack is still in `waitWrite` |
| Shutdown and join | None | Real ordinary QUIC transfer on a granted client and server, then Close, join and a direct write on the returned socket; a post-close `WriteTo` returns `net.ErrClosed` |
| Shared request-class contention | None | Data participants on path and control lanes, plus stateless and public classes, drive the real writer. Every participant progresses with no fault. A pressure phase records sampled `waitWrite` hits and the `Udp` `SndbufErrors` delta |

**Timing is separate from traversal.** Injected and non-injected timings are recorded apart. Only non-injected observations are compared to the 20 ms tolerance. A pressure phase with no observed wait is recorded as that, and is not pressure evidence.

**The GSO `EIO` trigger is the one simulated input.** This kernel gave no real trigger, so the wrapper returns `EIO` for the GSO attempt without a syscall. Everything after it is native: the fallback in `writeEndpointRequest` (`native_endpoint.go:1094-1116`), the segment's `sendmsg`, the injected `EAGAIN`, the poll wait and the deadline return. The claim is that a fallback stays inside the absolute deadline, not that this kernel produces `EIO`, and the receipt labels the trigger as simulated.

## Gates and alternatives

Representation stays example-level over the fixed scenario and toolchain domain. Runtime closure isn't imposed on the fixture, and no public, wire or persistent surface changes. Ownership and authority are unchanged. There is no transitional seam: the test wrapper exists only in the tagged test. Blast radius is one tagged test file, one driver with its regressions, and the receipt. The budget stays at 12 native instances, with probes and smoke runs charged, one fresh review and at most one replacement.

| Alternative | Obligation and contract cost | Disposition |
| --- | --- | --- |
| Export test accessors or a tagged non-test file in the root package | Adds a production-package surface the slice forbids | Rejected on merit |
| `go:linkname` from `integrationtests/self` | Reaches the grant methods but no per-request deadline, so expired-before-dispatch can't be reached and queued expiry isn't deterministic | Rejected on merit |
| `go test -overlay` adding exports | Evidence would depend on files outside the tested package | Rejected on merit |
| Run only the permission retry natively and leave GSO fallback deterministic | Drops the fallback half of a class the ledger and slice name | Rejected: a labeled simulated trigger keeps the native traversal after it. Replace the trigger if a later kernel or venue gives a real one |
| IPsec, device or module work to chase a real `EIO` further | Unbounded venue work for one branch that shares its deadline with a covered path | Rejected under the evidence budget |
| Run the fixture on hosted CI | The named venue must allow ptrace and namespaces and record one pinned image; `minimax` does and matches the plan's host history | Not selected; the receipt pins `minimax` |

A required class that skips, unobservable traversal on Go 1.26.10, or a method needing a production hook still stops the slice. Implementation starts from main after this reaches it, on a new branch for #1020.
