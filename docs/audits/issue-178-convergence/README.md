# Controlled MTU convergence characterization

The bounded investigation for [#178](https://github.com/the-sarge/quic-go-fast/issues/178) identifies premature fixture closure under tolerated probe loss. The original 50 MB echo can finish while discovery is below the required 1375-byte minimum and another probe is still scheduled for a later RTT interval. Keeping the stream active lets the unchanged finder progress; closing afterward preserves the MTU/DATAGRAM snapshot barrier. This is example-level evidence at the real UDP fixture, not a claim that the historical hosted packet-loss cause has been recovered.

## Captures

Base: `f4d13bd8d5edff5e57706499b2f7b7837a7bd4fe`. All three executions used `TIMESCALE_FACTOR=3`, QUIC v1, Ubuntu 24.04 Docker userspace and a Go 1.27.1 Linux arm64 binary cross-compiled on Darwin arm64 with `CGO_ENABLED=0`. Docker Desktop's kernel, architecture, scheduling and current suite order differ from the historical hosted runner. The recorded shuffle seed does not control those differences. [capture.json](capture.json) binds binaries, patches and logs by SHA-256; binaries and the full baseline log remain in the task's external evidence directory `/Volumes/worktrees/quic-go-fast/mtu-convergence-178-evidence`.

| Execution | Change | Observation | Exit |
| --- | --- | --- | --- |
| Baseline | Unmodified source, full self suite, original shuffle seed | MTU 1389; initial/final DATAGRAM 1163/1352; server 1234 | 0 |
| [Experiment 1](experiment1.log) | [Temporary probe logging and two early probe drops](experiment1.patch), original close-after-echo lifecycle | Echo completes at MTU 1263; final DATAGRAM 1226; discovery unfinished; assertion fails | 1 |
| [Experiment 2](experiment2.log) | [Same temporary drops/logging plus continued verified echo traffic](experiment2.patch) | Echo completes at MTU 1373; subsequent 1412-byte probe lost and 1392-byte probe ACKed; final DATAGRAM 1355; original assertions pass | 0 |

The controlled losses drop the first 1326-byte and 1357-byte client packets, observed in these captures as finder probes. They model two incidental losses, within the finder's stated loss tolerance. They do not alter production recovery, RTT spacing or probe sizes. Instrumentation reports the finder state at probe emission and after ACK/loss callbacks, the remaining interval at ACK, echo completion, and the closed snapshot. These exact-size controls and logs are frozen one-shot evidence, not maintained test helpers. Neither instrumentation nor injected losses remains in the correction.

In experiment 1, the second dropped probe is declared lost at `18:05:05.576376880Z`; the echo completes at `18:05:05.637939963Z`. The preceding send at `18:05:05.558794213Z` and approximately 16.751 ms smoothed RTT leave the next five-RTT eligibility interval beyond echo completion. The closed snapshot remains MTU 1263 / DATAGRAM 1226: a consistent but premature result.

In experiment 2, the 1373-byte probe is ACKed at `18:06:26.223062834Z`, with 63.696709 ms until the next eligibility interval. Echo completion follows at `18:06:26.223253375Z`. Continued echo traffic produces the next probe at `18:06:26.291174042Z`; it exceeds the 1400-byte path and is lost normally. The 1392-byte probe is then sent and ACKed. Closure at `18:06:26.413007584Z` observes MTU 1392 / DATAGRAM 1355 and all original assertions pass. No finder terminal event is assumed: the fixture asks for the existing 25-byte tolerance, and a final loss need not emit `MTUUpdated.Done`.

## Commands and limits

Each Linux binary was built with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o <binary> ./integrationtests/self`. The baseline command was `docker run --rm --name quic-mtu-178-baseline -e TIMESCALE_FACTOR=3 -v /Volumes/worktrees/quic-go-fast/mtu-convergence-178-evidence:/evidence:ro -w /tmp ubuntu:24.04 /evidence/self-linux-arm64.test -test.v -test.timeout=5m -test.shuffle=1789072237978249249 -version=1`. Experiment 1 and 2 used the same container options, their respective `experiment1.test` / `experiment2.test` binaries, and `-test.v -test.timeout=30s -test.run='^TestPathMTUDiscovery$' -version=1`. Each patch applies independently to the base above; experiment 2 is not stacked on experiment 1.

The investigation used the approved one replay and two controlled experiments, with no randomized retry campaign. The historical runs' particular incidental-loss or scheduling causes remain unknown. The captures demonstrate why echo completion is an insufficient convergence boundary and show the focused correction without requiring a production change. Candidate validation and review receipts belong in the PR; this capture is not itself hosted certification.

## Correction

Preserve the original verified 50 MB echo, but read that exact prefix before sending FIN. If the last MTU observation is below 1375, exchange and verify 16 KiB echo blocks until discovery reaches the existing threshold. A stream deadline applies the existing 20-second bound to writes and reads, including these extra exchanges. Send FIN, require the echo's EOF with no trailing bytes, close the connection, then sample final MTU and DATAGRAM values using the unchanged assertions. This changes only the test's traffic and observation lifecycle. The [approved contract](../../agents/mtu-convergence-178.md) owns the boundaries and evidence budget.
