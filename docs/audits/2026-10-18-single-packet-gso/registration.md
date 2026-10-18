# #742 measurement registration

Registered 2026-10-18 before the counted stage, after two development rounds (`dev1`, `dev2`, not counted). Operator decisions on #742 (2026-10-18): loopback surface on minimax, the rules below, and no #743 prototype arm.

## Builds

- **pre:** `main` at `f97ceb75`, harness binary `gso742bench-pre` (sha256 `5960f172…f443`).
- **post:** candidate `dc7d387b` (PR #805), harness binary `gso742bench-post` (sha256 `a6281a2f…8e81`).
- Both cross-compiled on the Mac with Go 1.27.2 (`-trimpath`, `CGO_ENABLED=0`, linux/amd64) from the same harness source. The harness uses only the public API.

## Fixture

- Host minimax (Linux 7.0, loopback MTU 65536, powersave governor). One QUIC connection over IPv4 loopback per run.
- The receiver is always the pre build, pinned to cores 12–15. The sender build varies by arm, pinned to cores 8–11. Both run with `GOMAXPROCS=4`. SMT siblings 24–31 stay idle. The runner is pinned to cores 1–3.
- Default congestion control (not BBR), default MTU discovery, default initial packet size.
- Each run sends for 10 s. DATAGRAM runs call `SendDatagram` with 1,190-byte payloads in a tight loop; STREAM runs write 64 KiB chunks to one unidirectional stream.

## Arms and order

- `D-pre`, `D-post`, `D-off` (post build with `QUIC_GO_DISABLE_GSO=true` on the sender), `S-pre`, `S-post`.
- Eight rounds. Round *r* runs the arms in that list order rotated left by *r*−1.

## Metrics

- Received throughput: payload bytes the receiver got, times 8, over the interval from first to last receipt.
- Sender CPU per GiB: the sender's CPU seconds (user + system, `getrusage` over the send window) divided by received GiB.
- Also reported: receiver CPU per GiB, DATAGRAM loss (1 − received ÷ sent), and the strace engagement check (not counted).

## Decision rules

The per-round ratio is computed within one round; each rule uses the median over the eight rounds.

1. **The pre-#742 gap reproduces** when `D-pre`/`D-off` throughput is ≤ 0.985 or sender CPU per GiB is ≥ 1.015. If neither holds, the surface cannot show the effect: report it and stop for an operator decision.
2. **The gap closes** when `D-post`/`D-off` throughput is ≥ 0.985 and sender CPU per GiB is ≤ 1.015. Being better than the GSO-off arm by more than 1.5% also counts as closed, reported as a favorable deviation. If the gap does not close, report it and stop (issue #742's brief); the buffer-tier hypothesis is a separate step.
3. **The STREAM guard passes** unless `S-post`/`S-pre` throughput is < 0.97 or sender CPU per GiB is > 1.03.

## Host state

- Before each run, the runner waits until every fixture CPU (8–15, 24–31) is under 5% busy over a 2 s sample.
- After each run, it computes foreign time: task running time on the fixture CPUs (`/proc/schedstat`), minus both harness processes' CPU (`wait4`), minus the fixture `ksoftirqd` threads. A run is contaminated when foreign time exceeds 0.3 s. Development runs read 0.009–0.016 s.
- A round with any contaminated run is rerun once at the end with the same order. It replaces the original only if the rerun is clean. Contamination alone never decides an outcome.
