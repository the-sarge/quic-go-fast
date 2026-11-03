# E6 native qualification receipt

This receipt records [E6 #1020](https://github.com/the-sarge/quic-go-fast/issues/1020), under [endpoint parent #1012](https://github.com/the-sarge/quic-go-fast/issues/1012) and [program #646](https://github.com/the-sarge/quic-go-fast/issues/646). Contract: [E6](../adr/2026-10-24-multipath-endpoint-plan.md#e6), plan commit `151f0f5b6893b7908ee2484eb673b858a5ba3132`, with the [scoped re-audit](2026-11-03-e6-native-qualification-reaudit.md) owning the method.

## Result

**Qualified, for this fixed example-level domain.** All 12 instances passed once on candidate `0a9f72fe55b0ed559c72a13246d9ed3b63811f3e`, each class in its own process at B = 100 ms. No production file changes. This is no public support claim: the route stays private until R19, and no negotiated multipath traffic ran, which R17 and R18 own.

| Identity | Value |
| --- | --- |
| Host and image | `minimax`, Ubuntu 26.04.1 LTS, `7.0.0-30-generic #30-Ubuntu SMP PREEMPT_DYNAMIC Fri Jul 31 18:22:54 UTC 2026 x86_64` |
| Go 1.26.10 `internal/poll/fd_unix.go` | `689fb78bb32b8e810fa1dd2ded475a79574f24a8f40260af2d4a3273392bc2cc` |
| Go 1.27.3 `internal/poll/fd_unix.go` | `675f74e8cbfc73170e08b43947c131706cd59e498d722555fe3b793618f5f162` |
| Injection | strace 6.19, `--seccomp-bpf`, `sendmsg` |

[`results.json`](2026-11-03-multipath-e6/results.json) holds each command, exit, duration and load. Each class directory holds its evidence, test log and, where instrumented, its strace log.

## Injected traversal

Timing here is attribution only and isn't compared to the tolerance. In all three classes a goroutine stack sampled mid-wait showed the writer in `internal/poll.(*pollDesc).waitWrite` under `writeEndpointRequest`, and the trace shows the injected `sendmsg`.

| Class | Go 1.26.10 | Go 1.27.3 | Observed |
| --- | --- | --- | --- |
| EAGAIN-to-deadline | return +0.27 ms | return +0.70 ms | Deadline error, one uncertain datagram, no fault |
| Retry and fallback | retry +0.72 ms, fallback +1.22 ms | retry +0.33 ms, fallback +0.98 ms | Real `EPERM`, then the retry's injected `EAGAIN`. Fallback: one uncertain segment and two unsubmitted, GSO disabled |
| Independent queued expiry | expiry +0.92 ms, current +1.23 ms | expiry +0.61 ms, current +0.17 ms | The queued request completed unsubmitted while the current one was still in the poller |

Times are after each request's own deadline.

**One input is simulated.** The fallback's GSO `EIO` comes from a test wrapper with no syscall, because this kernel gave no real trigger. The wrapper counted exactly one. The fallback, the segment's `sendmsg`, the injected `EAGAIN`, the poll wait and the deadline return are native. So this shows a fallback staying inside the absolute deadline, not that the kernel produces `EIO`.

## Uninstrumented timing

Every value is within the 20 ms tolerance. These classes ran without a tracer.

| Class | Measure | Go 1.26.10 | Go 1.27.3 |
| --- | --- | --- | --- |
| Expired-before-dispatch | Expiry published | 0.06 ms | 0.06 ms |
| Expired-before-dispatch | Successor publication | 0.09 ms | 0.08 ms |
| Shutdown and join | Public write return | 0.10 ms | 0.11 ms |
| Request-class contention | Native return past deadline | none | none |
| Request-class contention | Slowest publication | 2.85 ms | 3.02 ms |
| Pressure phase | Native return past deadline | none | none |
| Pressure phase | Slowest publication | 2.72 ms | 3.87 ms |

Join isn't a tolerance measure. It took 0.05–0.13 ms, against the budget plus tolerance that the fixture asserts. After join, a direct write and a write deadline on the caller's socket both behaved normally, and a post-close `WriteTo` returned `net.ErrClosed`.

A sink that never returned dropped about 6,000 diagnostics in each contention run without faulting the endpoint, so publication didn't backpressure the writer. An unrelated GridCast Windows CI VM ran on the host throughout, at a load average near 1.4. Nothing reserved cores for this campaign.

## Pressure and contention

**No real pressure wait was observed.** About 190 stack samples during 1 second of traffic with a 2 KiB send buffer found the writer in the poller zero times, and `SndbufErrors` didn't move. Loopback delivers synchronously, so this is recorded as no observed wait and isn't pressure evidence. Native poll traversal rests on injection alone.

**Data lanes contended and progressed.** 32 participants each moved 64 datagrams per lane, drawing about 14,500 refusals with a peak of 32 endpoint requests, and no request expired.

**Stateless and public classes were mostly refused.** Over about 120 ms, public writes succeeded 1 time in about 110 attempts on each toolchain, and stateless replies 2 in 113 and 1 in 110. The endpoint held a third of its slots at most, and each class had room. One grant exists at a time (`native_endpoint.go:863`), and a refused caller of these classes withdraws (`native_response.go:33-35`, `:67-70`), so it only gets in when it arrives while no grant is held. The contract allows this: both routes are best-effort or return backpressure with no hidden retry. It does mean a public `WriteTo` can see `ErrMultipathBackpressureV1` almost every time under saturated data traffic, even with free capacity. This class shows the routes share the real writer without a fault. It doesn't show that they get a useful share, and R18's stateless-pressure cell and the public activation decision should take that into account.

## Operational ledger

The [ledger](2026-11-03-multipath-e6/ledger.json) charges 45 instances and 60 host-seconds: 12 regular and 33 from the reserve. Cumulative use is 55 of 180 instances, leaving 125 instances and 28,288 host-seconds. Only R1's receipt preceded this one.

The reserve had 32 left, so it is overdrawn by one, and the 126 remaining scheduled instances exceed remaining capacity by one. The owner said on 2026-11-03 that the ceiling can be raised if needed. This receipt doesn't change it. R17 or a later preflight has to record that raise before spending the last instance.

The superseded campaign on `4b6e00c0c618bd08ad9a29c9608ee8b949b84ddb` passed all 12. It was rerun for the attempt counts above, not to obtain a pass.

## Completion and applicability

The transport test suite owns the fixture, and retirement requires equivalent replacement or feature removal. Later commits in this PR add this receipt and its status transitions, and don't change the tested files. A toolchain or kernel change requires reassessing this evidence, as the qualification boundary states.

On merge, the endpoint track is complete. R16 #1036 still requires R11, R13, R14 and R15, so E6 makes no successor newly ready, and R9 #1029 remains the frontier.
