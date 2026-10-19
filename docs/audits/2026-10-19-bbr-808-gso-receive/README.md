# #808 side stage: #743's STREAM cost and the received-DATAGRAM discard counter

**Date:** October 19, 2026, America/New_York. **Scope:** campaign decision D8 of [#808](https://github.com/the-sarge/quic-go-fast/issues/808) ([campaign record](../2026-10-19-bbr-808-campaign/README.md)). Measurement only. It runs in the readiness window, right after the readiness order ([readiness record](../2026-10-19-bbr-808-readiness/README.md)), on `minimax` loopback.

**Status: registration.** The sections from [Questions](#questions) to [Order](#order) were written, with the harness built and one development round run, before any counted run. Results are added below them afterwards.

## Questions

1. **K3, #743's open STREAM cost.** [#743's record](../2026-10-18-equal-size-gso/README.md) read Reno loopback STREAM at 0.979× its pre-#743 `main`, with the same work per GiB at a lower IPC; the operator accepted it as an open cost for #808 to re-check (#743's K3). On the #808 revision, what is Reno STREAM throughput against the pre-#743 sender?
2. **The discard counter.** #824's comment on #808 asks the campaign to report `(*Conn).DatagramReceiveDropsV1()` beside DATAGRAM loss in the DATAGRAM and mixed arms. Readiness and L6 cannot: their frozen fixture does not read it. Here: DATAGRAM loss, the counter, #824's measurement counter at the same refusal site, and the unprocessed-packet queue's drops, for Reno and BBRv3, in DATAGRAM and mixed arms.
3. **#743 on BBRv3.** Readiness and L6 cannot see #743 (their fixture never GSO-batches; readiness record, [The candidate](../2026-10-19-bbr-808-readiness/README.md#the-candidate)). Here: BBRv3 DATAGRAM with #743 on and off (`QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true`), segments per send and throughput. #743 measured only Reno.

## The harness

[bench808.go.src](bench808.go.src) is #824's `rdq824bench` (`rdq824bench.go.src` at `2f550727`) with one recorded change: a `-cc` flag selecting the built-in controller (`reno` or `bbrv3`) at both endpoints through `Config.SetCongestionControlV1`, and each role reports its connection's selected controller. Everything else is as #824 measured it: one process per role, the counting `sendmsg` wrapper that keeps batched receive, 1,190-byte DATAGRAMs, 64 KiB stream writes, the receiver reading `DatagramReceiveDropsV1` by interface assertion after close, and the receive-drop counters of [recvcount-cand.patch](recvcount-cand.patch) (#824's, unchanged, applied cleanly to `e7eb3f10`) behind the `dropcount` tag ([drops_on.go.src](drops_on.go.src), [drops_off.go.src](drops_off.go.src)). The sources stay `.go.src` so the archived-evidence module never compiles them.

[build.sh](build.sh) (#824's, adapted) exports three trees and builds one binary from each, `GOTOOLCHAIN=local` (Go 1.27.2), `GOPROXY=off`, linux/amd64:

| Binary | Tree | SHA-256 |
| --- | --- | --- |
| `bench808-send-pre` | `main` at `c8dbe35e` (post-#742, #800, #803; before #743), unpatched | `de84214542b62b7a…` |
| `bench808-send` | the #808 revision, `e7eb3f10`, unpatched | `a24e1b7360499546…` |
| `bench808-recv` | `e7eb3f10` plus `recvcount-cand.patch`, `dropcount` tag | `66cc542575f05b1c…` |

The binaries embed their build directory through the module `replace`, so these hashes identify the counted binaries; a rebuild elsewhere differs. `build.sh` records them in `build.json`, which is packed with the raw data.

## Arms

The receiver is `bench808-recv` in every arm. Each arm sets the controller at both endpoints.

| Arm | Sender | Mode | Controller | Sender environment |
| --- | --- | --- | --- | --- |
| `S-pre` | `send-pre` | stream | Reno | — |
| `S-rev` | `send` | stream | Reno | — |
| `D-reno` | `send` | datagram | Reno | — |
| `M-reno` | `send` | mixed | Reno | — |
| `D-bbr` | `send` | datagram | BBRv3 | — |
| `Dk-bbr` | `send` | datagram | BBRv3 | `QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true` |
| `S-bbr` | `send` | stream | BBRv3 | — |
| `M-bbr` | `send` | mixed | BBRv3 | — |

## Runs, host and contamination

- **Rounds:** eight, each running every arm once in an order rotated by round ([runner.py](runner.py), #824's runner adapted), 10 s per run.
- **Host:** `minimax`, sender on cores 8–11, receiver on 12–15 (under GNU `time`, which forks it, so its peak RSS is its own), runner on 1–3; `GOMAXPROCS=4`. Host settings unchanged.
- **Quiet gate:** before every run, the fixture cores and their SMT siblings below 5% busy in a 2 s sample (#824's gate), and the runner pauses while `~/agents/quic-808/YIELD-808.request` exists.
- **Contamination:** #742's foreign-time measure (task running time on the fixture CPUs from `/proc/schedstat`, less the harness's own CPU and `ksoftirqd`), contaminated above 0.3 s per run. A round holding a contaminated run is rerun once, all arms, with the same round number; the rerun replaces it only if it holds every arm and every run is clean; otherwise the round is excluded ([analyze.py](analyze.py), #824's rule, with a round counting only when it holds all eight arms). If listing the contaminated rounds fails, the stage stops rather than skipping the reruns.
- **Correctness:** a run whose endpoints report a controller other than its arm's, in any counted or rerun round whether used or not, makes the stage incorrect (`status` "incorrect", no reading reported), never a contaminated run; a failed process stops the stage, and the readiness driver records the failure in `STAGE.DONE`. With no usable round, or when the counted order did not finish (no `counted.DONE`), `status` is "incomplete" and no reading is reported.

## Readings (fixed before any counted run)

- **K3.** Per round, `S-rev` ÷ `S-pre` throughput; the median over used rounds is **"not reproduced"** when at least 0.99 (the downstream consumer's line) and **"persists"** below 0.99. Reported beside #743's 0.979, its registered 0.97 guard, and the sender CPU per GiB ratio. Because `S-pre` and `S-rev` are different builds, code placement is not separated from the change (#743's own caveat).
- **The STREAM work diagnostic.** After the counted rounds and reruns, four alternating `S-pre`/`S-rev` pairs with `sudo -n perf stat` on the sender (instructions, cycles, task-clock) — #743's [perfdiag.py](perfdiag.py), adapted — tell more work per GiB from the same work at lower IPC. Descriptive; it always runs, so the unattended order needs no decision.
- **The discard counter (descriptive).** For `D-reno`, `M-reno`, `D-bbr`, `Dk-bbr` and `M-bbr`: DATAGRAM loss (1 − received ÷ sent), the `DatagramReceiveDropsV1` share, #824's measurement counter at the same refusal site and their difference (expected 0, as in every #824 run), the unprocessed-queue drops, and the residual (sent − received − counted drops; #824 saw −1 when the unprocessed queue drops a packet carrying no DATAGRAM, and #743 did not report it for mixed arms, where the unprocessed drops count packets that may carry only stream data).
- **#743 on BBRv3 (descriptive).** `D-bbr` and `Dk-bbr` segments per `sendmsg`, and their throughput ratio; beside them `S-bbr` against `S-rev` and `M-bbr` against `M-reno`.
- Throughput, sender and receiver CPU per GiB, receiver peak RSS, and smoothed and minimum RTT for every arm; ratios are taken within a round.

No reading here changes a readiness flag, the advancement disposition or the L6 classification. [analyze_test.py](analyze_test.py) (11 tests) covers the incorrect-stage rule, round selection, partial rounds, an unfinished order and the empty cases. The STREAM diagnostic's contamination flag uses the registered measure (foreign time less the fixture CPUs' `ksoftirqd`).

## Development round (before this registration)

One round of all eight arms on October 19, 2026, 03:45–03:47 UTC (`dev1`, excluded from every statistic, kept with the raw data), while a GridCast GARM Windows VM was running on `minimax`. Every run was contaminated (foreign time up to 6.6 s). Seen by the agent: both endpoints reported the arm's controller in every run; BBRv3 DATAGRAM ran at 8,256 Mbit/s with 14.36 segments per send, and 4,325 Mbit/s with 1.00 when the switch was set; `DatagramReceiveDropsV1` read 0 in every DATAGRAM and mixed run; `S-rev` ÷ `S-pre` read 0.902 under the contamination. Nothing in this registration changed after it.

## Order

[stage.sh](stage.sh), run by the readiness driver after `DRIVER-DONE`, under `taskset -c 1-3`: a check that no counted, rerun or diagnostic output exists; `runner.py` for eight counted rounds; one rerun of each contaminated round; `perfdiag.py` for four pairs. Then, anywhere: `analyze.py counted.jsonl rerun.jsonl perfdiag.jsonl > summary.json` (`stage.sh` creates `rerun.jsonl` empty before any rerun).

Estimated host time: 64 runs of about 12 s plus quiet gates, about 15 minutes; reruns about 2 minutes per round; the diagnostic about 3 minutes.

## Registration review

Reviewed with the whole #808 registration; see the [readiness record](../2026-10-19-bbr-808-readiness/README.md#registration-review).
