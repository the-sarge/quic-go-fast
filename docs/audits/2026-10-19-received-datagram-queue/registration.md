# #824 measurement registration

Registered 2026-10-19 (UTC) before the counted stage, after one development round (`dev1`, not counted). The gate is the one in #824's agent brief (issue comment 6256404128), with decision D2 from the approved plan (operator, 2026-10-18). The operator's decisions bind; these rules decide pass or stop under the brief.

## Builds

- **send:** `main` at `a345c236`, unpatched, harness binary `rdq824bench-send`. The sender in every arm. It includes #743's equal-size GSO batching, on by default.
- **main:** `main` at `a345c236` plus [recvcount-main.patch](recvcount-main.patch) (#743's `recvcount.patch`, byte for byte: measurement-only counters at the received-DATAGRAM queue's refusal site and the unprocessed-packet queue's drop site).
- **cand:** the PR #829 head `9a0e2cc8` plus [recvcount-cand.patch](recvcount-cand.patch) (the same two measurement-only counters on the candidate's code).
- **c128:** `cand` plus [depth128.patch](depth128.patch), which sets `maxDatagramRcvQueueLen` back to 128. Measurement-only; never merged.
- The three receivers are built with the `dropcount` tag. All four binaries are cross-compiled on the Mac by [build.sh](build.sh) with Go 1.27.2 (`-trimpath`, `-buildvcs=false`, `CGO_ENABLED=0`, linux/amd64). The README records the binary hashes.
- `9a0e2cc8` is the head under code review when this stage runs. If the review changes shipped code (`datagram_queue.go` or `DatagramReceiveDropsV1`), the stage reruns on the final head.

## Fixture

#743's single-connection DATAGRAM fixture ([its registration](../2026-10-18-equal-size-gso/registration.md)), with the receiver as the variable:
- Host minimax (Linux 7.0, loopback MTU 65536, powersave governor). One QUIC connection over IPv4 loopback per run, port 47430.
- The receiver is pinned to cores 12–15 and the sender to cores 8–11, both with `GOMAXPROCS=4`. SMT siblings 24–31 stay idle. The runner is pinned to cores 1–3.
- Default congestion control, default MTU discovery, default initial packet size.
- Each run sends for 10 s: `SendDatagram` with 1,190-byte payloads in a tight loop. The sender's socket wrapper counts `sendmsg` calls and segments, as in #743.
- The harness, [rdq824bench.go.src](rdq824bench.go.src), is #743's with three receiver additions: it reads `DatagramReceiveDropsV1` through `interface{ DatagramReceiveDropsV1() uint64 }` after its connection has closed (absent on `main`), and it reports `VmHWM`.
- Receiver peak RSS: the runner starts the receiver under GNU `time`, which forks it, so the receiver's `ru_maxrss` is its own and not the runner's (Linux `ru_maxrss` survives `execve`). `VmHWM` and `wait4`'s `ru_maxrss` are reported beside it.

## Arms and order

- `R-main`, `R-cand`, `R-128`: the `main`, `cand` and `c128` receivers against the same sender.
- Eight rounds. Round *r* runs the arms in that list order rotated left by *r*−1.

## Metrics

- **DATAGRAM loss:** 1 − received ÷ sent.
- **Discard share:** `DatagramReceiveDropsV1` ÷ sent (candidate arms). The measurement-only queue counter ÷ sent is reported in every arm.
- **Derived queue loss:** sent − received − unprocessed-queue drops − socket drops. #743's packer residual is zero here: it measured none in any DATAGRAM run, and this change does not touch the sender.
- **Unprocessed drops not declared lost:** unprocessed-queue drops minus the sender's lost-packet count. A DATAGRAM packet is ack-eliciting, so the sender declares it lost when the receiver drops it unacknowledged. A dropped packet the sender never declares lost was not ack-eliciting (for example an ACK-only packet) and carried no DATAGRAM. `dev1`'s `R-main` run had one.
- **Reported against `R-main`, per-round ratios:** received throughput, receiver CPU per GiB, sender CPU per GiB, receiver peak RSS, and the sender's min and smoothed RTT.

## Rules

Each rule uses the median over counted rounds unless it says every run.

0. **Fixture.** `R-main` discards ≥ 0.5% at its queue (measurement counter ÷ sent) with ≥ 2 segments per send. Otherwise the fixture does not reproduce #743's overload: report and stop.
1. **Pass.** `R-cand`'s discard share is ≤ 0.01%, and its DATAGRAM loss is ≤ 0.01%. Under D2, loss above 0.01% still passes when loss minus the unprocessed-queue and socket shares is ≤ 0.01%: the excess is then drops at queues the sender already sees, out of #824's scope (GridSwarm/wiremux#1871), and is reported. Otherwise: record the numbers and stop for the owner. No other depth is tried.
2. **Exact.** In every counted `R-128` run, `DatagramReceiveDropsV1` − derived queue loss is 0, or equals that run's unprocessed drops not declared lost. Anything else is a counter defect until shown otherwise.
3. **Correctness.** Any sender or receiver error stops the runner. In any run, a residual (sent − received − measurement queue counter − unprocessed − socket drops) beyond ±2 DATAGRAMs is reported as an accounting defect.

## Host state

As #743:
- Before each run, the runner waits until every fixture CPU (8–15, 24–31) is under 5% busy over a 2 s sample, and while `~/agents/quic-824/YIELD-824.request` exists.
- After each run, it computes foreign time on the fixture CPUs: `/proc/schedstat` task running time minus both harness processes' CPU (`wait4`) minus the fixture `ksoftirqd` threads. A run is contaminated when foreign time exceeds 0.3 s.
- A round with any contaminated run is rerun once at the end, in the same order. The rerun replaces the original only if it is clean. Contamination alone never decides an outcome.
- wiremux 1869 released minimax before the stage (its campaign finished and its Windows guest is shut off); wiremux 1870 was told of the stage. GARM CI is not covered by any yield.
