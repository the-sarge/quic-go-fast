# #808 side stage: #743's STREAM cost and the received-DATAGRAM discard counter

**Date:** October 19, 2026, America/New_York. **Scope:** campaign decision D8 of [#808](https://github.com/the-sarge/quic-go-fast/issues/808) ([campaign record](../2026-10-19-bbr-808-campaign/README.md)). Measurement only. It runs in the readiness window, right after the readiness order ([readiness record](../2026-10-19-bbr-808-readiness/README.md)), on `minimax` loopback.

**Status: complete.** The sections from [Questions](#questions) to [Order](#order) were written, with the harness built and one development round run, before any counted run; the [Answer](#answer) and [Results](#results) were added afterwards.

## Answer

- **K3: #743's STREAM cost is *not reproduced* on the #808 revision.** Reno STREAM from the revision's sender reads 0.997× the pre-#743 sender (range 0.992–1.006 over eight rounds), at 1.000× its sender CPU per GiB, against #743's 0.979. It is above #743's registered 0.97 guard and the consumer's 0.99 line. The `perf` diagnostic agrees: in three of four clean pairs, user instructions and cycles per GiB differ by under 1%. The fourth pair reads 0.82× on both, which is the perf coverage-mode artefact #743 described, not a difference in work.
- **`DatagramReceiveDropsV1`: zero by median in every DATAGRAM and mixed arm,** and nonzero in four of 40 runs, all Reno: three `D-reno` runs (192, 93 and 53 payloads, at most 0.0019%) and one `M-reno` run (73, 0.0013%). It matched #824's measurement counter at the same refusal site in every run. DATAGRAM loss is at most 0.017% by median, nearly all at the 256-packet unprocessed-packet queue, which the sender sees as loss.
- **#743 on BBRv3:** BBRv3 DATAGRAM sends 14.24 segments per `sendmsg` with #743 on and 1.00 with it off. With it on, BBRv3 carries 1.76× the throughput (median of paired ratios; arm medians 8,202 against 4,710 Mbit/s) at 0.44× the sender CPU per GiB. #743 measured only Reno; this is its first BBRv3 measurement.
- **Beside these, on this unbottlenecked loopback path:** BBRv3 reads 0.85× Reno's throughput for DATAGRAM, 0.71× for STREAM and 0.77× for mixed, at 1.20–1.22× Reno's sender CPU per GiB. Readiness's loopback cells measure the same comparison under their own rules. This is descriptive only.

## Results

Eight counted rounds, all 64 runs clean: foreign time was at most 0.061 s against the 0.3 s limit, so no round was rerun. Both endpoints reported their arm's controller in every run (`status` complete). Figures are from [summary.json](summary.json) (`analyze.py counted.jsonl rerun.jsonl perfdiag.jsonl`).

Arm medians (range over the eight rounds for throughput):

| Arm | Received Mbit/s | Sender CPU-s/GiB | Receiver CPU-s/GiB | Receiver peak RSS (MiB) | Segments per send | Smoothed / min RTT (µs) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `S-pre` | 15,223 (15,140–15,335) | 0.984 | 0.917 | 14.7 | 11.69 | 64 / 19 |
| `S-rev` | 15,203 (15,156–15,267) | 0.985 | 0.916 | 14.6 | 11.69 | 67 / 20 |
| `S-bbr` | 10,834 (10,084–11,089) | 1.176 | 0.990 | 15.1 | 11.34 | 53 / 12 |
| `D-reno` | 9,704 (9,685–9,730) | 1.256 | 1.310 | 16.8 | 14.82 | 109 / 16 |
| `D-bbr` | 8,202 (7,845–8,459) | 1.526 | 1.391 | 17.0 | 14.24 | 93 / 11 |
| `Dk-bbr` | 4,710 (4,651–4,762) | 3.429 | 2.264 | 15.4 | 1.00 | 88 / 8 |
| `M-reno` | 11,333 (11,324–11,376) | 1.172 | 1.201 | 15.1 | 12.57 | 94 / 20 |
| `M-bbr` | 8,695 (8,362–9,199) | 1.419 | 1.357 | 15.8 | 12.45 | 60 / 16 |

Per-round ratios, median (range):

| Ratio | Throughput | Sender CPU/GiB | Receiver CPU/GiB |
| --- | --- | --- | --- |
| `S-rev/S-pre` | 0.9971 (0.9921–1.0063) | 1.0001 (0.9938–1.0083) | 1.0017 (0.9843–1.0097) |
| `Dk-bbr/D-bbr` | 0.5687 (0.5581–0.6053) | 2.2493 (2.2178–2.2756) | 1.6287 (1.6040–1.6535) |
| `D-bbr/D-reno` | 0.8452 (0.8062–0.8712) | 1.2143 (1.1922–1.2326) | 1.0620 (1.0519–1.0705) |
| `S-bbr/S-rev` | 0.7115 (0.6652–0.7299) | 1.1969 (1.1835–1.2118) | 1.0798 (1.0664–1.0918) |
| `M-bbr/M-reno` | 0.7661 (0.7374–0.8106) | 1.2181 (1.1867–1.2282) | 1.1313 (1.1162–1.1438) |

DATAGRAM loss and the discard counter (medians; the counter's largest single run is in the Answer):

| Arm | DATAGRAM loss | `DatagramReceiveDropsV1` share | Counter minus #824 measurement counter (largest) | Unprocessed-queue drops (share of DATAGRAMs) | Residual (largest) |
| --- | ---: | ---: | ---: | ---: | ---: |
| `D-reno` | 0.0130% | 0.0000% | 0 | 0.0125% | 1 |
| `D-bbr` | 0.0077% | 0.0000% | 0 | 0.0077% | 0 |
| `Dk-bbr` | 0.0000% | 0.0000% | 0 | 0.0000% | 0 |
| `M-reno` | 0.0166% | 0.0000% | 0 | 0.0240% | 525 |
| `M-bbr` | 0.0000% | 0.0000% | 0 | 0.0000% | 23 |

In the mixed arms the residual is not a DATAGRAM count: the unprocessed-queue drops count packets, some of which carry only stream data. #743 did not report it for mixed arms either.

**The STREAM work diagnostic** (four alternating `S-pre`/`S-rev` pairs with `perf stat` on the sender, all clean), `S-rev` ÷ `S-pre`:

| Pair | Throughput | Sender CPU/GiB | User instructions/GiB | User cycles/GiB |
| --- | ---: | ---: | ---: | ---: |
| 1 | 0.993 | 1.000 | 1.008 | 1.002 |
| 2 | 1.003 | 0.993 | 1.008 | 0.992 |
| 3 | 0.999 | 0.985 | 0.822 | 0.814 |
| 4 | 0.999 | 1.002 | 1.008 | 1.001 |

**Deviations:** none. The stage ran 09:13–09:27 UTC, inside the readiness window, after `DRIVER-DONE`.

## Questions

1. **K3, #743's open STREAM cost.** [#743's record](../2026-10-18-equal-size-gso/README.md) read Reno loopback STREAM at 0.979× its pre-#743 `main`, with the same work per GiB at a lower IPC; the operator accepted it as an open cost for #808 to re-check (#743's K3). On the #808 revision, what is Reno STREAM throughput against the pre-#743 sender?
2. **The discard counter.** #824's comment on #808 asks the campaign to report `(*Conn).DatagramReceiveDropsV1()` beside DATAGRAM loss in the DATAGRAM and mixed arms. Readiness and L6 cannot: their frozen fixture does not read it. Here: DATAGRAM loss, the counter, #824's measurement counter at the same refusal site, and the unprocessed-packet queue's drops, for Reno and BBRv3, in DATAGRAM and mixed arms.
3. **#743 on BBRv3.** Readiness and L6 cannot see #743 (their fixture never GSO-batches; readiness record, [The candidate](../2026-10-19-bbr-808-readiness/README.md#the-candidate)). Here: BBRv3 DATAGRAM with #743 on and off (`QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true`), segments per send and throughput. #743 measured only Reno.

## The harness

[bench808.go.src](bench808.go.src) is #824's `rdq824bench` (`rdq824bench.go.src` at `2f550727`) with one recorded change: a `-cc` flag selecting the built-in controller (`reno` or `bbrv3`) at both endpoints through `Config.SetCongestionControlV1`, and each role reports its connection's selected controller. Everything else is as #824 measured it: one process per role, the counting `sendmsg` wrapper that keeps batched receive, 1,190-byte DATAGRAMs, 64 KiB stream writes, the receiver reading `DatagramReceiveDropsV1` by interface assertion after close, and the receive-drop counters of [recvcount-cand.patch](recvcount-cand.patch) (#824's, unchanged, applied cleanly to `d94b3ec2`) behind the `dropcount` tag ([drops_on.go.src](drops_on.go.src), [drops_off.go.src](drops_off.go.src)). The sources stay `.go.src` so the archived-evidence module never compiles them.

[build.sh](build.sh) (#824's, adapted) exports three trees and builds one binary from each, `GOTOOLCHAIN=local` (Go 1.27.2), `GOPROXY=off`, linux/amd64:

| Binary | Tree | SHA-256 |
| --- | --- | --- |
| `bench808-send-pre` | `main` at `c8dbe35e` (post-#742, #800, #803; before #743), unpatched | `de84214542b62b7a…` |
| `bench808-send` | the #808 revision, `d94b3ec2`, unpatched | `1d0193bebb4b9d94…` |
| `bench808-recv` | `d94b3ec2` plus `recvcount-cand.patch`, `dropcount` tag | `f6e75a1f04f3b95e…` |

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

One round of all eight arms on October 19, 2026, 03:45–03:47 UTC, on `e7eb3f10`'s binaries before the re-pin (`dev1`, excluded from every statistic, kept with the raw data), while a GridCast GARM Windows VM was running on `minimax`. Every run was contaminated (foreign time up to 6.6 s). Seen by the agent: both endpoints reported the arm's controller in every run; BBRv3 DATAGRAM ran at 8,256 Mbit/s with 14.36 segments per send, and 4,325 Mbit/s with 1.00 when the switch was set; `DatagramReceiveDropsV1` read 0 in every DATAGRAM and mixed run; `S-rev` ÷ `S-pre` read 0.902 under the contamination. Nothing in this registration changed after it.

## Order

[stage.sh](stage.sh), run by the readiness driver after `DRIVER-DONE`, under `taskset -c 1-3`: a check that no counted, rerun or diagnostic output exists; `runner.py` for eight counted rounds; one rerun of each contaminated round; `perfdiag.py` for four pairs. Then, anywhere: `analyze.py counted.jsonl rerun.jsonl perfdiag.jsonl > summary.json` (`stage.sh` creates `rerun.jsonl` empty before any rerun).

Estimated host time: 64 runs of about 12 s plus quiet gates, about 15 minutes; reruns about 2 minutes per round; the diagnostic about 3 minutes.

## Registration review

Reviewed with the whole #808 registration; see the [readiness record](../2026-10-19-bbr-808-readiness/README.md#registration-review).
