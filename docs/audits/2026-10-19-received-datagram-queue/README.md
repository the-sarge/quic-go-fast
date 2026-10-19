# #824 measurement: the 1,024-entry received-DATAGRAM queue

**Result: the gate passes, with decision D2 applied and one registered rule failing by one DATAGRAM in one run.**

| Receiver, against the same post-#743 sender | DATAGRAM loss | Discarded at the received-DATAGRAM queue | Received throughput |
| --- | ---: | ---: | ---: |
| `main` (128 entries) | 1.895% | 1.883% | 9,540 Mbit/s |
| candidate (1,024 entries) | 0.0122% | 0% (max 0.0094%) | 9,720 Mbit/s (1.021×) |
| candidate set back to 128 | 1.727% | 1.717% | 9,605 Mbit/s |

At 1,024 entries the received-DATAGRAM queue discarded nothing in five of eight runs, and at most 0.0094% in the others. All of the candidate's remaining loss (median 0.0122%) is at the unprocessed-packet queue. That queue is out of #824's scope, and the sender already sees its drops. Under decision D2, that passes. `DatagramReceiveDropsV1` matched an independent measurement counter in every run. In seven of the eight 128-entry runs, it also matched the loss derived from the harness's own counts exactly. In the eighth it was one higher, because one dropped packet carried no DATAGRAM (below).

## Question

Issue #824 asks the fork to raise the received-DATAGRAM queue from 128 to a fixed 1,024 entries, to count its full-queue discards behind `(*Conn).DatagramReceiveDropsV1()`, and to stop it pinning delivered payloads. The cause is #743's equal-size GSO batching: with it on, the 128-entry queue discarded about 1.6% of DATAGRAMs after their packets were acknowledged ([#743's record](../2026-10-18-equal-size-gso/README.md)). The agent brief (issue comment 6256404128) sets the gate:
- rerun #743's single-connection DATAGRAM harness with three arms;
- pass when the candidate's DATAGRAM loss and discard share are each ≤ 0.01%;
- require the counter, in every run of the arm set back to 128, to equal the derived queue loss.

## Fixture and rules

[registration.md](registration.md) holds the fixture, arms, metrics and rules. They were registered after one development round (`dev1`) and before the counted stage. The copy here is byte-identical to the one placed beside the counted data on minimax at launch (sha256 `763644d993120ea842dde088fd711bf5923d6a2a3c1088804eaf09058e58492c`).

In short, it is #743's fixture on minimax loopback with the receiver as the variable:
- **Sender:** unpatched `main` in every arm, with #743's batching on by default (14.8 segments per `sendmsg`).
- **Receivers:**
  - `R-main`: `main` plus #743's measurement-only counters.
  - `R-cand`: the candidate plus the same counters.
  - `R-128`: `R-cand` with `maxDatagramRcvQueueLen` set back to 128.
- **Runs:** 1,190-byte payloads, 10 s per run, eight rounds in rotated order.
- **Counter reading:** the receiver reads `DatagramReceiveDropsV1` by interface assertion after its connection has closed.
- **Peak RSS:** the runner starts the receiver under GNU `time`, which forks it, so the reading is the receiver's own.

Operator decisions (2026-10-18):
- **The plan.** The operator approved it. It registered rules 0–3 and named the gate's three arms.
- **D2.** If the candidate meets the line at its own queue but total loss exceeds 0.01% only through unprocessed-queue drops, record it and merge.

Builds:
- **send** and **main:** `main` at `a345c236`.
- **cand** and **c128:** the PR #829 head `9a0e2cc8`, which was under code review when the stage ran. The review's only change, `ab193db0`, edits a benchmark comment in a test file, so the shipped code is the code measured.

Binary sha256:
- send: `72a99d50210b3eb4e9f8ec9b90a0b6eb45039d54fa4c13781d7dca5f4fb15fd3`
- main: `c6499bcd86348b88c02941d020c029ec50bac1285356415df39f13b229d28842`
- cand: `434f67b020b024541df951ac94cc77c59b135ada0ab4235f25a4d41f54891e62`
- c128: `a5ae381ddf443167e04abaedb0908dcb3c893092c2b875bbfdd422e822f6261f`

## Rounds used

All eight rounds count. Every run was clean: foreign time on the fixture CPUs was 0.011–0.029 s against the 0.3 s limit. No round was rerun.

## Results

Arm medians (range over the eight rounds):

| Arm | Received Mbit/s | DATAGRAM loss | Receiver CPU-s/GiB | Receiver peak RSS | Smoothed / min RTT |
| --- | ---: | ---: | ---: | ---: | ---: |
| R-main | 9,540 (9,472–9,580) | 1.895% (1.777–2.105%) | 1.339 | 16.2 MiB (15.1–18.7) | 113 / 13.5 µs |
| R-cand | 9,720 (9,699–9,765) | 0.0122% (0.0101–0.0217%) | 1.303 | 17.9 MiB (15.7–18.9) | 103 / 15 µs |
| R-128 | 9,605 (9,561–9,645) | 1.727% (1.684–1.782%) | 1.310 | 16.2 MiB (15.0–17.7) | 128 / 15 µs |

Per-round ratios against `R-main`, median (range):

| Ratio | Throughput | Receiver CPU/GiB | Receiver peak RSS | Smoothed RTT | Min RTT |
| --- | --- | --- | --- | --- | --- |
| R-cand/R-main | 1.0206 (1.0139–1.0246) | 0.9738 (0.9641–0.9810) | 1.084 (0.912–1.236) | 0.887 (0.620–1.779) | 1.113 (0.813–1.333) |
| R-128/R-main | 1.0052 (1.0024–1.0182) | 0.9793 (0.9742–0.9841) | 1.019 (0.800–1.132) | 1.109 (0.888–1.515) | 0.971 (0.563–1.333) |

Sender CPU per GiB is 0.980× for `R-cand`, which is the same work spread over 2% more delivered bytes.

Per round:

| Round | R-main Mbit/s | R-cand Mbit/s | R-128 Mbit/s | R-cand loss | R-cand queue discards | R-128 `V1` count | `V1` − derived (R-128) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 9,567 | 9,765 | 9,615 | 0.0106% | 69 | 175,421 | +1 |
| 2 | 9,513 | 9,708 | 9,561 | 0.0118% | 0 | 180,874 | 0 |
| 3 | 9,566 | 9,699 | 9,631 | 0.0101% | 0 | 172,158 | 0 |
| 4 | 9,472 | 9,705 | 9,645 | 0.0125% | 0 | 172,725 | 0 |
| 5 | 9,537 | 9,755 | 9,605 | 0.0217% | 968 | 177,762 | 0 |
| 6 | 9,580 | 9,732 | 9,606 | 0.0154% | 368 | 177,250 | 0 |
| 7 | 9,542 | 9,706 | 9,565 | 0.0158% | 0 | 170,926 | 0 |
| 8 | 9,533 | 9,735 | 9,583 | 0.0114% | 0 | 177,372 | 0 |

## DATAGRAM loss and where it happens

| Arm | DATAGRAM loss | At the received-DATAGRAM queue | At the unprocessed-packet queue | Socket drops | Residual |
| --- | ---: | ---: | ---: | ---: | ---: |
| R-main | 1.895% | 1.883% | 0.0108% | 0 | −1 to 0 |
| R-cand | 0.0122% | 0% (max 0.0094%) | 0.0118% (0.0099–0.0158%) | 0 | −1 to 0 |
| R-128 | 1.727% | 1.717% | 0.0116% | 0 | −1 to 0 |

- **The deeper queue removes the loss it was meant to remove.** `R-cand`'s queue discarded nothing in five runs and 69, 368 and 968 DATAGRAMs (at most 0.0094%) in the other three.
- **What remains is at the unprocessed-packet queue.** It is the same size in every arm (0.011–0.012%), so the candidate doesn't add to it. Those drops equal the packets the receiver never processed (packets sent − packets received) in every run, and the sender declares nearly all of them lost.
- **The socket dropped nothing.**

## Decisions under the registered rules

0. **Fixture: reproduces.** `R-main` discards 1.883% at its queue, with 14.82 segments per send.
1. **Pass: passes under D2.** `R-cand`'s discard share (`DatagramReceiveDropsV1` ÷ sent) is 0% by median, at or below 0.01%. Its DATAGRAM loss is 0.0122% by median, above 0.01%. With the unprocessed-queue and socket shares taken out, it is 0.0000%, so D2 applies: the excess is entirely at a queue whose drops the sender already sees, which belongs to GridSwarm/wiremux#1871. The stop rule is not triggered.
2. **Exact: fails as registered in one run, by one DATAGRAM.** In seven of eight `R-128` runs, `DatagramReceiveDropsV1` equals the derived queue loss (sent − received − unprocessed-queue drops − socket drops) exactly. In round 1 it is one higher (175,421 against 175,420), and the registered explanation doesn't cover it: that run's unprocessed drops (1,034) equal the sender's lost packets (1,034). The registration calls any other difference "a counter defect until shown otherwise". The diagnosis below shows that the derived loss was off, not the counter.
3. **Correctness: passes.** No sender or receiver error. The largest residual was one DATAGRAM.

### Rule 2 diagnosis

The registered derived loss assumes every packet dropped at the unprocessed-packet queue carried a DATAGRAM. That isn't always true:
- **Every lost packet was dropped at that queue.** In every run, packets sent − packets received equals the unprocessed-queue drops.
- **A residual of −1 means one dropped packet carried no DATAGRAM.** The residual is sent − received − queue discards − unprocessed drops. It was −1 in five counted runs (`R-main` rounds 2 and 4, `R-cand` rounds 2 and 4, `R-128` round 1) and in `dev1`'s `R-main` run. `R-main` has no `DatagramReceiveDropsV1` at all, so the −1 comes from the packet accounting, not from the new counter.
- **Ack-eliciting and non-ack-eliciting packets are both involved.** In four of those six runs, the sender declared one fewer packet lost than the receiver dropped: an ACK-only packet. In the other two (`R-128` round 1 and `R-cand` round 2), the counts match, so the dropped packet was ack-eliciting but carried no DATAGRAM. A PING or a path-MTU probe would fit; the harness can't tell which.
- **The counter matches an independent instrument.** `DatagramReceiveDropsV1` equals #743's measurement-only counter, a process-wide atomic added at the same refusal site, in all 16 `R-cand` and `R-128` runs.
- **The brief's own formula balances in every run.** Its last term, the packer residual, is computed in #743's record from that counter. With it, the formula reduces to the comparison above (socket drops were 0), so the count is exact in all eight `R-128` runs.

## Observations outside the rules

- **1,024 is close to the line on this host.** `R-cand`'s worst run discarded 0.0094% at the queue, just under 0.01%. The median is 0. A faster sender or a slower receiver would cross it. #824's stop rule forbids trying another depth here.
- **Receiver peak RSS rises 8% (16.2 → 17.9 MiB by median).** A deeper queue can hold more payloads at once, up to 1,024 × 1,190 bytes (about 1.2 MB). The ranges overlap. Slot clearing means a drained queue holds none of them; `TestDatagramReceiveReleasesDelivered` covers that, and this harness doesn't measure it.
- **The receiver does slightly less CPU work per GiB** (0.974×). It no longer pays for payloads it copies and then can't deliver, and it delivers 2% more bytes.
- **The candidate at 128 loses a little less than `main` at 128** (1.727% against 1.895%), with receiver CPU per GiB at 0.979×. The two differ only in slot clearing and the counter's atomic add. This record doesn't separate a garbage-collection effect from code placement.
- **RTT barely moves.** `R-cand`'s smoothed RTT is 0.89× and its min RTT 1.11×, with wide ranges. A deeper application queue doesn't delay ACKs.
- **The three RSS readings agree.** GNU `time`'s `%M`, `wait4`'s `ru_maxrss` on the `time` process, and the receiver's own `VmHWM` were equal in every run.

## Host state

- The development round ran at 02:19 UTC on 2026-10-19 and the counted stage at 02:21–02:27 UTC.
- minimax was idle beforehand: load 0.00, no GARM CI, no VMs. wiremux 1869 had finished its campaign and shut its Windows guest off; it and wiremux 1870 were told of the stage.
- The runner was pinned to cores 1–3.

## Files

- `registration.md`: the registered fixture, arms, metrics and rules.
- `rdq824bench.go.src`, `drops_on.go.src`, `drops_off.go.src`: harness source (`main.go` and its two build-tagged drop reporters), archived as `.src` so the `docs/audits` module never builds them.
- `recvcount-main.patch`: #743's `recvcount.patch`, byte for byte, applied to `main`.
- `recvcount-cand.patch`: the same measurement-only counters on the candidate.
- `depth128.patch`: sets the candidate back to 128. Measurement-only; never merged.
- `build.sh`: exports the four trees and builds the binaries.
- `runner.py`, `analyze.py`: the minimax runner and the rule script. `python3 -B analyze.py counted.jsonl > summary.json` regenerates `summary.json` byte for byte. Its `outside_rules` block was added after the stage for the rule 2 diagnosis; the registered rules are unchanged.
- `counted.jsonl`, `counted.log`: the 24 counted runs and their log.
- `development.jsonl`, `development.log`: development round `dev1`. Not counted.
- `summary.json`: per-round metrics, ratios and rule outcomes.

## Reproduction

1. Run `build.sh a345c236 9a0e2cc8 <outdir>` on a Mac with Go 1.27.2 and the module cache populated. It needs no network (`GOPROXY=off`).
2. Copy `<outdir>/bin/rdq824bench-{send,main,cand,c128}` to `~/agents/quic-824/bin/` on minimax, with `runner.py` beside them.
3. Run `taskset -c 1-3 python3 runner.py counted 8 --tag=counted --duration=10` from `~/agents/quic-824`.
4. Run `python3 -B analyze.py counted.jsonl > summary.json`.

The binaries' build info embeds the absolute `replace` directory, so a rebuild elsewhere produces different hashes; the hashes above identify the counted binaries.
