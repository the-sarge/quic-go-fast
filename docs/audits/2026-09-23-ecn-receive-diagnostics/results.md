# ECN receiver investigation (#528)

## Status

Diagnosis unresolved; no corrective behavior change is established. The approved [contract and replay budget](../../agents/ecn-receive-diagnostics.md) precede collection. This candidate adds test-only error and endpoint reporting plus joined receiver cleanup. Production code, packet assertions and existing waits are unchanged.

## Existing evidence

The [original CI failure](https://github.com/the-sarge/quic-go-fast/actions/runs/35793423396/job/106967046958) reports a ten-second IPv4 wait timeout under Go 1.26.8 and TIMESCALE_FACTOR=10, seed 1790116749139531000, candidate c1b6daa04fce055ffc9276256badf8d189c5d306. The issue records Darwin 25.6.0 arm64 / macOS 26.6.2 build 25G83. Earlier matching-host scratch-overlay and Darwin 27 triage passes did not reproduce or resolve it; the scratch overlay needed corrected absolute certificate fixture paths. No infrastructure classification is established.

## Instrumentation validation

On the Darwin 27 development host with Go 1.27.1, the receiver-error regression first failed against the silent-return worker: closing the UDP socket produced only `timeout waiting for packet`, not an error wrapping `net.ErrClosed`. The same regression passed after the worker published the actual error in the ordered result handoff. An initial asynchronous execution's output was not retained; the observed red execution was repeated once to capture the diagnostic assertion. These development checks are not incident replays.

The subsequent focused suite covers error-before-delivery, queued-packet-before-error, no-delivery timeout, success followed by cleanup, and pending-result cleanup, alongside existing ECN and packet-info tests. It passed once in 0.243 seconds before final sender-context polish. Final certification and native replay receipts will be recorded below.

## Collection

Collection pending for this instrumentation source. The reference host is reachable and its observed identity matches Darwin 25.6.0 arm64 / macOS 26.6.2 build 25G83. Go 1.26.8 is available through GOTOOLCHAIN. No replay has yet consumed the six-invocation/twenty-minute budget.
