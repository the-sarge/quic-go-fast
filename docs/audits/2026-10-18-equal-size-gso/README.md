# #743 measurement: GSO-batching equal-size short DATAGRAM packets

**Result: a large DATAGRAM gain, a small STREAM cost, and receive-side loss.**

| Sender vs post-#742 `main` | Throughput | Sender CPU per GiB |
| --- | --- | --- |
| DATAGRAM, #743 candidate | 2.28× (4,163 → 9,493 Mbit/s) | 0.36× (3.614 → 1.288 s) |
| DATAGRAM, candidate with the kill switch | 0.996× | 1.008× |
| STREAM, candidate | 0.979× | 1.016× |
| Mixed STREAM + DATAGRAM, candidate | 1.015× (STREAM 1.001×, DATAGRAM 1.028×) | 0.991× |

Every registered rule passed: the candidate engages, the kill switch restores today's behaviour, the benefit is material, and the STREAM and mixed guards hold. Packer discards were zero. The STREAM ratio (0.979) passes the registered 0.97 guard but not the downstream consumer's 0.99 line. A diagnostic outside the rules (below) points to code placement rather than extra work. The faster sender raises receive-side DATAGRAM loss from 0.0005% to 1.66%, nearly all of it at the receiver's 128-entry received-DATAGRAM queue.

## Question

Issue #743 asks whether GSO emission should keep batching after a short first packet, using that packet's size as the segment size. Its gate is a Linux A/B on minimax of a single-connection DATAGRAM workload (~1,190-byte payloads), max-size-only segmenting (post-#742 `main`) against first-packet-size segmenting, with a reliable-stream guard and a mixed workload, reporting throughput, sender CPU per GiB, segments per send, and receive-side DATAGRAM loss by drop point.

## Fixture and rules

[registration.md](registration.md) holds the fixture, arms, metrics and rules. They were registered after one development round (`dev1`) and before the counted stage. The copy here is byte-identical to the one placed beside the counted data on minimax at launch.

In short, it is #742's fixture on minimax loopback, with three changes:
- The receiver is `main` plus two drop counters ([recvcount.patch](recvcount.patch)), the same in every arm.
- There is a mixed mode: STREAM and DATAGRAM at once on one connection.
- The sender's socket is wrapped to count segments per `sendmsg`.

Arms: `D-main`, `D-proto`, `D-kill` (the candidate with `QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true`), `S-main`, `S-proto`, `M-main`, `M-proto`. Each run sends for 10 s. Ratios are taken within a round, and each rule uses the median across rounds.

Builds:
- **main:** `c8dbe35e` (post-#742 `main`).
- **proto:** the PR #818 candidate `2c2c2db7`, after its code review.
- **recv:** `c8dbe35e` plus the patch.

Binary sha256:
- main: `4344828b656af4c0a0348f7ee0ec7082445125c7e9677b01cddc110e0b7a3742`
- proto: `1949f6fbddb601775a162b74eb2555dd3951726259372662bc6dcef5caaecd6c`
- recv: `88cde787c0123cb9e8b79aa4c4b7b7b1db7a082bafc1cf14245c514b4e8285ca`

Operator decisions (2026-10-18): the plan's D1–D7, including this fixture, these rules and measuring the reviewed candidate. The keep-or-reject decision is the operator's, below.

## Rounds used

Five of eight rounds count: 1, 2, 3, 5 and 8.
- **First pass.** GridCast's self-hosted CI started on minimax at about 21:58 UTC, as round 2 began: an unpinned Windows Server 2025 GARM VM and a Linux runner. Rounds 2–8 were contaminated, with foreign time of up to 18.5 s against the 0.3 s limit. Round 1 was clean.
- **Reruns.** Once the CI had been idle for a minute, rounds 2–8 were rerun once each (22:24–22:36 UTC). Rounds 2, 3, 5 and 8 came back clean and replace their originals. Rounds 4, 6 and 7 were contaminated again (the CI returned), so the registered rule excludes them.

The rule outcomes don't depend on the excluded rounds: every first-pass round, contaminated or not, read `D-proto`/`D-main` between 2.27 and 2.46.

## Engagement

| Arm | Segments per `sendmsg` | Multi-segment sends | Largest |
| --- | ---: | ---: | ---: |
| D-main | 1.00 | 0.0% | 1 |
| D-proto | 14.78 | 98.6% | 16 |
| D-kill | 1.00 | 0.0% | 1 |
| S-main / S-proto | 11.69 / 11.70 | 99.8% / 99.8% | 16 / 16 |
| M-main / M-proto | 10.99 / 12.58 | 85.4% / 99.0% | 14 / 16 |

`D-proto`'s batches are limited by the 20 KiB GSO buffer (16 packets of about 1.2 KB), not by the 64-segment cap. STREAM batching is unchanged.

## Results

Arm medians (range over the five rounds):

| Arm | Received Mbit/s | Sender CPU-s/GiB | Receiver CPU-s/GiB |
| --- | ---: | ---: | ---: |
| D-main | 4,163 (4,151–4,182) | 3.614 (3.605–3.633) | 2.394 |
| D-proto | 9,493 (9,459–9,518) | 1.288 (1.288–1.301) | 1.316 |
| D-kill | 4,152 (4,127–4,167) | 3.642 (3.628–3.660) | 2.393 |
| S-main | 15,275 (15,255–15,304) | 0.976 (0.975–0.979) | 0.921 |
| S-proto | 14,939 (14,804–14,973) | 0.992 (0.990–1.002) | 0.923 |
| M-main | 11,208 (11,198–11,265) | 1.178 (1.173–1.180) | 1.204 |
| M-proto | 11,372 (11,313–11,435) | 1.166 (1.157–1.168) | 1.193 |

In the mixed arms, `M-main` carried 5,798 Mbit/s of STREAM and 5,411 of DATAGRAM; `M-proto` carried 5,796 and 5,577.

Per-round ratios, median (range):

| Ratio | Throughput | Sender CPU/GiB | Receiver CPU/GiB |
| --- | --- | --- | --- |
| D-proto/D-main | 2.2803 (2.2688–2.2863) | 0.3573 (0.3565–0.3581) | 0.5480 |
| D-kill/D-main | 0.9962 (0.9943–0.9998) | 1.0078 (1.0037–1.0090) | 1.0007 |
| D-proto/D-kill | 2.2889 (2.2775–2.2919) | 0.3547 (0.3536–0.3555) | 0.5491 |
| S-proto/S-main | 0.9793 (0.9685–0.9802) | 1.0161 (1.0153–1.0231) | 1.0008 |
| M-proto/M-main | 1.0146 (1.0094–1.0156) | 0.9909 (0.9855–0.9914) | 0.9929 |

Mixed, per flow: STREAM 1.0010 (0.9918–1.0037), DATAGRAM 1.0282 (1.0279–1.0330).

Per-round throughput (Mbit/s):

| Round | D-main | D-proto | D-kill | D proto/main | S-main | S-proto | S proto/main | M-main | M-proto |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 4,182 | 9,489 | 4,167 | 2.269 | 15,275 | 14,973 | 0.9802 | 11,209 | 11,372 |
| 2 | 4,163 | 9,518 | 4,162 | 2.286 | 15,255 | 14,939 | 0.9793 | 11,265 | 11,435 |
| 3 | 4,164 | 9,503 | 4,152 | 2.282 | 15,304 | 14,893 | 0.9731 | 11,208 | 11,313 |
| 5 | 4,151 | 9,459 | 4,127 | 2.279 | 15,286 | 14,804 | 0.9685 | 11,198 | 11,373 |
| 8 | 4,163 | 9,493 | 4,145 | 2.280 | 15,271 | 14,969 | 0.9802 | 11,198 | 11,355 |

## DATAGRAM loss and where it happens

| Arm | DATAGRAM loss | At the received-DATAGRAM queue | Unprocessed-queue drops | Socket drops | Packer residual (max) |
| --- | ---: | ---: | ---: | ---: | ---: |
| D-main | 0.0005% | 0.0005% | 0 | 0 | 0 |
| D-proto | 1.656% (1.618–1.663%) | 1.645% | 1,135 packets | 0 | 0 |
| D-kill | 0.0010% | 0.0010% | 0 | 0 | 0 |
| M-main | 0.052% | 0.038% | 1,196 packets | 0 | — |
| M-proto | 0.088% | 0.069% | 1,393 packets | 0 | — |

- **Loss rises sharply in the DATAGRAM arm.** At 2.3× the rate, the receiver's 128-entry received-DATAGRAM queue (`maxDatagramRcvQueueLen`) discards about 1.6% of DATAGRAMs. It does so after the packets were acknowledged, so congestion control never sees the loss.
- **The unprocessed-packet queue drops the rest.** Its drops equal the sender's lost-packet count in every arm.
- **The socket dropped nothing.**
- **The packer discarded nothing.** In every DATAGRAM-only run, sent − received − counted drops = 0. #743's no-discard safeguard holds; the probe this replaces discarded a median of 258 per run.
- **Upstream has the same queue.** This is inherited behaviour that #743 makes easier to reach. Receive queues are out of #743's scope (operator decision D7).

## Decisions under the registered rules

1. **Engagement: passes.** `D-proto` carries 14.78 segments per send, with 98.6% of sends multi-segment. `D-main` and `D-kill` carry none.
2. **Kill switch: passes.** `D-kill`/`D-main` throughput is 0.996, and sender CPU per GiB is 1.008.
3. **Benefit: material.** Throughput is 2.28×, at or above 1.10, and sender CPU per GiB is 0.357×, at or below 0.90.
4. **STREAM guard: passes.** Throughput is 0.979, not below 0.97, and sender CPU per GiB is 1.016, not above 1.03. The consumer's 0.99 line, reported beside it, is not met. One round, round 5, read 0.9685.
5. **Mixed guard: passes.** STREAM is 1.001 and DATAGRAM 1.028, both at or above 0.97.
6. **Correctness: passes.** No sender or receiver error. The largest packer residual was 0.
7. **Loss: reported above.**

## Observations outside the rules

- **The STREAM cost is cycles, not work.** [perfdiag.py](perfdiag.py) ran four alternating `S-main`/`S-proto` pairs right after the reruns, all clean.
  - Throughput: `S-proto` read 1.4–2.1% lower in every pair.
  - Unchanged: sends per GiB (65.8k), segments per send (11.70), and whole-process sender CPU per GiB (`getrusage`, within ±1.5%, mean about +0.3%).
  - `perf stat -p` caught the Go runtime's threads in one of two coverage modes (about 1.5 or 1.9 CPUs), so compare only pairs in the same mode. There, user-space instructions per GiB differ by −0.6% and +0.6%, while user cycles per GiB are 1.6% and 2.3% higher.

  The candidate does the same work at a lower IPC, which is consistent with code placement in the send loop. Placement and run state have moved loopback STREAM by about ±3% on this host before (#738, #744). This record can't separate placement from the change's own layout. The full-size path runs the same packing rule, with only the batch helper's method calls added.
- **Round-trip time rises with the faster DATAGRAM sender.** `D-proto`'s smoothed RTT is 107 µs against `D-main`'s 23 µs, and min RTT 14 µs against 7 µs. A 16-packet batch is delivered as one burst, and the receive queue runs deeper.
- **The counting wrapper costs nothing measurable.** `D-main` (4,163 Mbit/s) matches #742's unwrapped post-#742 arm (4,221) within the host's run-to-run spread.
- **BBR's bounded path was not measured.** Focused tests cover it (`TestGSOEqualSize*`, `TestBBRBoundedEqualSizeCredit`). BBR performance belongs to the shared campaign #808.
- **Windows was not measured** (Human Brief). Hosted CI runs a 64-short-segment USO socket test.

## Host state

- Every counted run that was used passed the quiet gate, with foreign time of at most 0.125 s on the fixture CPUs; the fixture `ksoftirqd` threads used none.
- The development round ran 21:42–21:44 UTC. The first pass ran 21:55–22:18 UTC and the reruns 22:24–22:36 UTC, all under yields from wiremux 1869, whose idle Windows guest stayed on cores 4–7 and 18–19.
- GridCast's GARM CI is not covered by the yield; it caused the exclusions above.

## Files

- `registration.md`: the registered fixture, arms, metrics and rules. Byte-identical to the copy beside the counted data on minimax (sha256 `70cc3d4b1574ab338966d2c826e1509d8648243d6e1a7fa94213d12899813603`).
- `gso743bench.go.txt`, `drops_on.go.txt`, `drops_off.go.txt`: harness source (`main.go` and its two build-tagged drop reporters). They're archived as text so the `docs/audits` module never builds them.
- `recvcount.patch`: the receiver tree's drop counters, applied to `main` `c8dbe35e`. Measurement-only; never merged.
- `build.sh`: exports the three trees and builds the binaries.
- `runner.py`, `analyze.py`: the minimax runner and the rule script. `python3 analyze.py counted.jsonl rerun.jsonl` regenerates `summary.json` byte for byte.
- `counted.jsonl`, `counted.log`: the 56 first-pass runs and their log.
- `rerun.jsonl`, `rerun.log`: the 49 rerun runs (rounds 2–8) and their log.
- `summary.json`: per-round metrics, ratios and rule outcomes.
- `development.jsonl`: development round `dev1`. Not counted.
- `perfdiag.py`, `perfdiag.jsonl`: the STREAM work diagnostic, outside the rules.

## Reproduction

1. Run `build.sh c8dbe35e 2c2c2db7 <outdir>` on a Mac with Go 1.27.2 and the module cache populated. It needs no network (`GOPROXY=off`).
2. Copy `<outdir>/bin/gso743bench-{main,proto,recv}` to `~/agents/quic-743/bin/` on minimax, with `runner.py` beside them.
3. Run `taskset -c 1-3 python3 runner.py counted 8 --tag=counted --duration=10`.
4. Rerun the contaminated rounds once, with `runner.py counted 7 --tag=rerun --first=2 --duration=10` (or `counted 1 --first=N` per round).
5. Run `python3 analyze.py counted.jsonl rerun.jsonl > summary.json`.

The binaries' build info embeds the absolute `replace` directory, so a rebuild elsewhere produces different hashes; the hashes above identify the counted binaries.
