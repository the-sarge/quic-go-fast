# #743 measurement registration

Registered 2026-10-18 before the counted stage, after one development round (`dev1`, not counted). Operator decisions on #743 (2026-10-18): the plan's D1–D7, including this fixture and the rules below (D5) and measuring the code-reviewed candidate (D6). The keep-or-reject decision stays with the operator; these rules shape the recommendation and do not bind it.

## Builds

- **main:** `main` at `c8dbe35e` (includes #742 `592e98cb`, #800 and #803), harness binary `gso743bench-main`.
- **proto:** the PR #818 candidate, harness binary `gso743bench-proto`.
- **recv:** `main` at `c8dbe35e` plus [recvcount.patch](recvcount.patch) (two drop counters and an exported reader), harness built with the `dropcount` tag. Used as the receiver in every arm.
- All three are cross-compiled on the Mac by [build.sh](build.sh) with Go 1.27.2 (`-trimpath`, `-buildvcs=false`, `CGO_ENABLED=0`, linux/amd64). The README records the counted SHAs and binary hashes.

## Fixture

As #742's record, with three changes: the receiver build, the mixed mode, and the sender's counting socket wrapper.
- Host minimax (Linux 7.0, loopback MTU 65536, powersave governor). One QUIC connection over IPv4 loopback per run, port 47430.
- The receiver (`recv`) is pinned to cores 12–15, the sender (by arm) to cores 8–11, both with `GOMAXPROCS=4`. SMT siblings 24–31 stay idle. The runner is pinned to cores 1–3.
- Default congestion control (not BBR), default MTU discovery, default initial packet size.
- Each run sends for 10 s.
  - **DATAGRAM:** `SendDatagram` with 1,190-byte payloads in a tight loop.
  - **STREAM:** 64 KiB writes to one unidirectional stream.
  - **Mixed:** both at once on one connection.
- The sender's UDP socket is wrapped to count `sendmsg` calls and the segments each carries (from its `UDP_SEGMENT` control message). The wrapper doesn't allocate and keeps quic-go's batched receive. It runs in every arm. `dev1`'s `D-main` read 4,185 Mbit/s, against 4,221 for #742's unwrapped post-#742 arm.

## Arms and order

- `D-main`, `D-proto`, `D-kill` (proto with `QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true` on the sender), `S-main`, `S-proto`, `M-main`, `M-proto`.
- Eight rounds. Round *r* runs the arms in that list order rotated left by *r*−1.

## Metrics

- **Received throughput:** payload bytes the receiver got, × 8, over first-to-last receipt. In mixed runs, also each flow's own.
- **Sender CPU per GiB:** the sender's CPU seconds (user + system, `getrusage`) divided by received GiB. DATAGRAM runs measure over the send loop; STREAM and mixed runs measure until the receiver closes, as in #742.
- **Receiver CPU per GiB,** the same way.
- **Segments per send:** segments ÷ `sendmsg` calls, the share of calls with ≥ 2 segments, and the largest.
- **DATAGRAM loss:** 1 − received ÷ sent, split into:
  - received-DATAGRAM-queue drops and unprocessed-queue drops (receiver counters);
  - socket drops (`/proc/self/net/udp`);
  - in DATAGRAM-only arms, a residual (sent − received − the three), which is packer discards plus anything still queued at close.
- **Reported, not used in any rule:** the sender's packets lost and min and smoothed RTT.

## Rules

Each per-round ratio is computed within one round; each rule uses the median over rounds.

1. **Engagement.** `D-proto` carries ≥ 2 segments per send, with ≥ 50% of sends multi-segment. `D-main` and `D-kill` have ≤ 1% multi-segment sends. Otherwise the prototype does not engage on this surface: report and stop.
2. **Kill switch.** `D-kill`/`D-main` throughput and sender CPU per GiB both fall within 0.985–1.015. Otherwise report it as a kill-switch or code-placement finding.
3. **Benefit.** `D-proto`/`D-main` throughput ≥ 1.10 or sender CPU per GiB ≤ 0.90 is a material benefit. Without it, the recommendation is reject.
4. **STREAM guard.** Passes unless `S-proto`/`S-main` throughput < 0.97 or sender CPU per GiB > 1.03 (#742's guard). The consumer's ≥ 0.99 line is reported beside it.
5. **Mixed guard.** Passes when `M-proto`/`M-main` stream throughput and DATAGRAM throughput are each ≥ 0.97.
6. **Correctness.** Any sender or receiver error stops the runner. In any DATAGRAM-only run, a residual above 2 DATAGRAMs is an implementation defect. Either one stops the stage: report it.
7. **Loss.** Reported with its split, not ruled. The receive queues are out of scope (D7). A recommendation to keep must state the loss and the follow-up it needs.

## Host state

As #742.
- Before each run, the runner waits until every fixture CPU (8–15, 24–31) is under 5% busy over a 2 s sample.
- After each run, it computes foreign time on the fixture CPUs: `/proc/schedstat` task running time minus both harness processes' CPU (`wait4`) minus the fixture `ksoftirqd` threads. A run is contaminated when foreign time exceeds 0.3 s.
- A round with any contaminated run is rerun once at the end, in the same order. The rerun replaces the original only if it is clean. Contamination alone never decides an outcome.
- The stage runs inside a yield from wiremux 1869. Its idle Windows guest stays booted on cores 4–7 and 18–19, outside the fixture.
