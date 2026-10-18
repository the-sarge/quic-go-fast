# #742 measurement: sending a lone GSO packet without a segment size

**Result: the gap closes.**

| Sender | DATAGRAM throughput vs GSO off | Sender CPU per GiB vs GSO off |
| --- | --- | --- |
| Before #742 (`main` `f97ceb75`) | 4.5% lower (0.955) | 4.0% higher (1.040) |
| After #742 (candidate `dc7d387b`) | 0.995 | 1.004 |

The STREAM guard did not regress: post-#742 STREAM read 1.019× pre-#742 throughput at 0.985× sender CPU per GiB. All 40 counted runs were clean, and none was rerun.

## Question

Issue #742's measurement gate asks whether a Linux A/B of a single-connection DATAGRAM workload, with ~1,190-byte payloads and GSO on, closes the before/after gap to within noise of the `QUIC_GO_DISABLE_GSO=true` arm, with a reliable-stream transfer as a guard that must not regress.

## Fixture and rules

[registration.md](registration.md) holds the fixture, arms, metrics and decision rules. They were registered before the counted stage, after two development rounds; the copy here is byte-identical to the one placed beside the counted data on minimax at launch.

In short:
- Host minimax, IPv4 loopback, one QUIC connection per run, 10 s per run.
- The receiver is always the pre build; only the sender build differs.
- Arms: `D-pre`, `D-post`, `D-off` (post build, `QUIC_GO_DISABLE_GSO=true`), `S-pre`, `S-post`.
- Eight rounds, with the arm order rotated each round. Ratios are taken within a round, and each decision uses the median across rounds.

Operator decisions (2026-10-18): loopback surface, these rules, and no #743 prototype arm.

## Engagement

[engagement-shape.txt](engagement-shape.txt) records one strace-traced 1 s DATAGRAM run per sender build. It is shape only and not counted.
- **Pre-#742:** 77,768 of 77,787 `sendmsg` calls carried a `UDP_SEGMENT` control message, almost all with segment size 1,441, the path-MTU-discovered maximum. Each call held a single 1,216-byte datagram.
- **Post-#742:** none of the 86,116 calls carried one.

So on this surface every pre-#742 DATAGRAM send was a one-packet GSO send, which is the case #742 targets.

## Results

Arm medians (range over 8 rounds):

| Arm | Received Mbit/s | Sender CPU-s/GiB | Receiver CPU-s/GiB |
| --- | ---: | ---: | ---: |
| D-pre | 4,046 (4,038–4,069) | 3.700 (3.691–3.708) | 2.470 |
| D-post | 4,221 (4,203–4,231) | 3.572 (3.569–3.582) | 2.379 |
| D-off | 4,240 (4,227–4,252) | 3.560 (3.550–3.565) | 2.377 |
| S-pre | 14,972 (14,898–15,081) | 0.993 (0.989–0.996) | 0.933 |
| S-post | 15,234 (15,162–15,303) | 0.978 (0.977–0.988) | 0.928 |

DATAGRAM loss was at most 0.0022% in any run.

Per-round ratios, median (range):

| Ratio | Throughput | Sender CPU/GiB | Receiver CPU/GiB |
| --- | --- | --- | --- |
| D-pre / D-off | 0.9551 (0.9532–0.9583) | 1.0400 (1.0375–1.0417) | 1.0386 |
| D-post / D-off | 0.9951 (0.9930–0.9976) | 1.0043 (1.0025–1.0058) | 1.0035 |
| D-post / D-pre | 1.0426 (1.0373–1.0451) | 0.9662 (0.9636–0.9692) | 0.9642 |
| S-post / S-pre | 1.0185 (1.0081–1.0220) | 0.9849 (0.9823–0.9990) | 0.9959 |

Per-round DATAGRAM and STREAM throughput:

| Round | D-pre | D-post | D-off | post/off | pre/off | S-pre | S-post | S post/pre |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 4,069 | 4,228 | 4,252 | 0.9945 | 0.9571 | 15,081 | 15,271 | 1.0126 |
| 2 | 4,050 | 4,231 | 4,242 | 0.9975 | 0.9547 | 15,040 | 15,162 | 1.0081 |
| 3 | 4,046 | 4,222 | 4,235 | 0.9971 | 0.9554 | 14,964 | 15,227 | 1.0176 |
| 4 | 4,068 | 4,219 | 4,245 | 0.9940 | 0.9583 | 14,950 | 15,240 | 1.0194 |
| 5 | 4,045 | 4,214 | 4,244 | 0.9930 | 0.9532 | 14,898 | 15,218 | 1.0215 |
| 6 | 4,041 | 4,217 | 4,235 | 0.9957 | 0.9543 | 14,980 | 15,303 | 1.0216 |
| 7 | 4,038 | 4,203 | 4,227 | 0.9944 | 0.9554 | 14,947 | 15,276 | 1.0220 |
| 8 | 4,044 | 4,227 | 4,237 | 0.9976 | 0.9545 | 15,027 | 15,171 | 1.0096 |

## Decisions under the registered rules

1. **The pre-#742 gap reproduces.** `D-pre`/`D-off` throughput is 0.955, at or below 0.985, and sender CPU per GiB is 1.040, at or above 1.015. The loopback gap (4.5%) is larger than wiremux's Docker-bridge cell (~3%).
2. **The gap closes.** `D-post`/`D-off` throughput is 0.995, at or above 0.985, and sender CPU per GiB is 1.004, at or below 1.015. This is not a favorable deviation.
3. **The STREAM guard passes.** `S-post`/`S-pre` throughput is 1.019, not below 0.97, and sender CPU per GiB is 0.985, not above 1.03.

## Observations outside the rules

- **A small residual in every round:** `D-post` reads 0.2–0.7% below `D-off`. It is inside the registered band. The only remaining difference on this path is the buffer tier: post-#742 lone packets still use the large buffer, by design. This record does not test that, and the brief keeps the tier out of scope.
- **The STREAM gain is consistent but not attributed.** Every round favors `S-post`. Lone packets also occur in stream traffic, for example when pacing or the window cuts a batch to one packet, so the change plausibly contributes. But rebuilds alone have moved loopback STREAM by about ±3% on this host before, and the guard only asks for no regression.
- **The BBR bounded path was not measured.** It is covered by the focused test `TestBBRBoundedGSOSegmentSize`. BBR performance is left to the planned shared BBR readiness campaign.

## Host state

- Every run passed the quiet gate on its first sample.
- Foreign task time on the fixture CPUs was 0.007–0.055 s per run, against the 0.3 s contamination limit.
- The fixture `ksoftirqd` threads used no measurable time.
- Booked with wiremux 1869, which held its minimax campaigns until `counted.DONE`; the counted stage ran 17:50–17:58 UTC.

## Files

- `registration.md`: the registered fixture and rules.
- `gso742bench.go.txt`: harness source (`main.go`). It is archived as text so the `docs/audits` module never builds it.
- `runner.py`, `analyze.py`: the minimax runner and the rule script. `python3 analyze.py counted.jsonl` regenerates `summary.json` byte for byte.
- `counted.jsonl`, `counted.log`: the 40 counted runs and their log.
- `summary.json`: per-round metrics, ratios and decisions.
- `development.jsonl`: development rounds `dev1` and `dev2`. These are not counted, and `dev1`'s `foreign_s` comes from the superseded `/proc/stat` method, which under-reads on this tickless kernel.
- `engagement-shape.txt`: the strace engagement check.

## Reproduction

1. Build the harness once per source tree. Use a module containing `gso742bench.go.txt` as `main.go`, with `require github.com/quic-go/quic-go v0.0.0` and `replace github.com/quic-go/quic-go => <tree>`, where `<tree>` is a checkout of `f97ceb75` (pre) or `dc7d387b` (post).
2. Compile with `GOPROXY=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false` using Go 1.27.2. These commands reproduce the counted binaries byte for byte:
   - pre: sha256 `5960f1726b9573f2a2af2226f004a2ac5b0170eedf30a8c5e7832d624096f443`
   - post: sha256 `a6281a2f100590bd4235b29628b235ea412f52360fa0228c74d6329f3bb28e81`
3. Copy them to `~/agents/quic-742/bin/gso742bench-{pre,post}` on minimax.
4. Run `taskset -c 1-3 python3 runner.py counted 8 --tag=counted --duration=10`.
