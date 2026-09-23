# ECN receiver investigation (#528)

## Status

Diagnosis unresolved; no corrective behavior change is established. The approved [contract and replay budget](../../agents/ecn-receive-diagnostics.md) precede collection. This candidate adds test-only error and endpoint reporting plus joined receiver cleanup. Production code, packet assertions and existing waits are unchanged.

## Existing evidence

The [original CI failure](https://github.com/the-sarge/quic-go-fast/actions/runs/35793423396/job/106967046958) reports a ten-second IPv4 wait timeout under Go 1.26.8 and TIMESCALE_FACTOR=10, seed 1790116749139531000, candidate c1b6daa04fce055ffc9276256badf8d189c5d306. The issue records Darwin 25.6.0 arm64 / macOS 26.6.2 build 25G83. Earlier matching-host scratch-overlay and Darwin 27 triage passes did not reproduce or resolve it; the scratch overlay needed corrected absolute certificate fixture paths. No infrastructure classification is established.

## Instrumentation validation

On the Darwin 27 development host with Go 1.27.1, the receiver-error regression first failed against the silent-return worker: closing the UDP socket produced only `timeout waiting for packet`, not an error wrapping `net.ErrClosed`. The same regression passed after the worker published the actual error in the ordered result handoff. An initial asynchronous execution's output was not retained; the observed red execution was repeated once to capture the diagnostic assertion. These development checks are not incident replays.

The subsequent focused suite covers error-before-delivery, queued-packet-before-error, no-delivery timeout, success followed by cleanup, and pending-result cleanup, alongside existing ECN and packet-info tests. It passed once in 0.243 seconds before final sender-context polish. Native replay receipts are below; final-head certification and review dispositions are retained in [PR #544](https://github.com/the-sarge/quic-go-fast/pull/544).

## Collection and conclusion

**Unresolved.** All four planned attempts passed on matching Darwin 25.6.0 arm64 / macOS 26.6.2 build 25G83 with Go 1.26.8 and TIMESCALE_FACTOR=10. Source was clean commit `2162ba68cb39ab8bec5e4fcdfd6ccef594af3464`, based on `d1f35cdb10d547fef8dd7eb6c2c84bc986bce0af`, with exactly the committed test instrumentation and no overlay. The complete source and certificate fixtures were built together on the host. The [environment receipt](receipts/darwin25-environment.json) includes source-file hashes; the [preparation receipt](receipts/darwin25-prepare.json) records successful native compilation before the replay budget began.

| Attempt | Case | Wall seconds | Package seconds | Outcome |
| --- | --- | --- | --- | --- |
| [darwin25-isolated-1](receipts/darwin25-isolated-1.json) | isolated | 0.814 | 0.383s | pass |
| [darwin25-seeded-1](receipts/darwin25-seeded-1.json) | original-seed root package | 5.849 | 3.688s | pass |
| [darwin25-isolated-2](receipts/darwin25-isolated-2.json) | isolated | 0.666 | 0.261s | pass |
| [darwin25-seeded-2](receipts/darwin25-seeded-2.json) | original-seed root package | 4.091 | 3.649s | pass |

Collection used four of six permitted invocations and 11.420 execution seconds of the twenty-minute budget. Two optional evidence-directed probe slots were not used: no failure supplied a discriminating order/shared-state observation. There were no failed attempts, retries, target-test skips or instrumentation changes during collection; unrelated root-package platform exclusions remain unchanged. The isolated receipts show wildcard IPv6 listeners receiving first IPv4 loopback and then IPv6 loopback with the original ECN assertions intact. Root-package passes also exercised the focused helper lifecycle tests. Every receipt preserves exact command, source SHA, clean status, time limits, timestamps, stdout/stderr, exit status and environment. These receipts are a finite investigation record, not a new maintained collector.

The original historical candidate was not replayed. The same seed on current source (which includes additional tests) does not guarantee identical test order to the original revision, and root-package replay does not recreate hosted cross-package load. Local validation on Darwin 27 is separately labeled. No packet capture or new failure distinguished missing delivery from receive/decode termination; neither a cause nor infrastructure classification is supported.

The next missing observation is an instrumented failing Darwin 25 run: whether the IPv4 phase returns an actual read error (including type/wrapped identity) or expires without one, together with sender/listener/destination identity and that run's source, seed and environment. Keep #528 and OmniFocus task `nvTh86kEphl` open. Do not treat diagnostics, successful replays or later green CI as resolution.

## Local validation

At instrumentation commit `2162ba68cb39ab8bec5e4fcdfd6ccef594af3464`, Go 1.26.8 on [Darwin 27 arm64](receipts/local-environment.json): [focused race tests](receipts/local-focused-race.json), [root package](receipts/local-root.json), [go vet](receipts/local-vet.json) and [module tidiness](receipts/local-tidy.json) all passed. Native source digests agree across the two hosts. No production file or dependency changed. Exact final-head certification and review dispositions are retained in the PR discussion so administrative evidence does not recursively change the certified code head.
