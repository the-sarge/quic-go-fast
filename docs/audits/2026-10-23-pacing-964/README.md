# Slow-start pacing assessment — #964

**Registered screening recommendation: reject this candidate on this native fixture.** The clean 100 ms RTT / 10 Mbit/s forward bottleneck / 1 MiB response cell passed the registered completion-time and packet-delay sensitivity gates but exceeded the registered overflow and transport-loss limits. Four of five pairs increased both rates. The noise gate does not test variability of overflow or transport-loss rates, so this screening classification does not independently establish a causal material regression. No clean 100 ms cell reached the required startup-benefit threshold. The rejection label follows the frozen analyzer rule explained below.

The operator authorized the assessment and evidence merge. Production pacing, defaults, BBR, APIs and ownership are unchanged by this PR. The candidate is retained only as an archived patch. The [approved contract](contract.md) and [verbatim issue brief](issue-brief.md) define the scope.

## Budget and provenance

Completed 292/292 scheduled attempts: 4 smoke, 48 control/control calibration and 240 comparison attempts. All payloads passed size and SHA-256 verification. There were no connection failures, exclusions, replacements or unrun cells. Four smoke and eight replacement slots were unused. Preparation took 771.9 seconds of 7,200; the complete network stage took 191.676 seconds of 3,600. The longest attempt took 4.036 seconds of 10. See [reconciliation](reconciliation.json) and the append-before-spawn [attempt ledger](raw/ledger.jsonl).

The immutable [registration](registration.json) and its [SHA-256](registration.sha256) were committed as `9d90e930` before the first network attempt. It records every planned attempt, reserves, impairment algorithm, capacities/rounding, host activity, Go/Python/OS/CPU identities, effective configuration and binary/source/fixture hashes. The host was a shared Mac16,5 / Apple M4 Max, Darwin arm64, Go 1.27.3, with `GOMAXPROCS=4` per endpoint. Local scheduler and loopback observations apply only to this fixture; no host settings were changed.

Both arms derive from default Reno at `1c4a28b7bc000bbc521332f5d7145e6493b42709`. The [common prerequisite and instrumentation](common.patch) produces control `43d358cee14934fdfea880ee59b36658cde7ac9c`; [the candidate-only patch](candidate-only.patch) produces `418e94ee53375879c24a4858e5811d9dc4a58106`. [Source tree identities](source-trees.json) and [build identities](build-identities.json) permit reconstruction from the pinned base. The common callback retains the exact byte conversion, gain expression, constructor budget initialization and token operations; the candidate alone selects 2× while the sender reports slow start.

## Deciding screening breach

| Measure, forward direction | Control median | Candidate median | Median paired increase | Allowed increase |
| --- | ---: | ---: | ---: | ---: |
| Queue overflow | 11.4017% | 11.8503% | 0.4486 percentage point | 0.1 percentage point |
| Transport-declared loss | 11.3898% | 11.8380% | 0.4482 percentage point | 0.1 percentage point |

The frozen `fixture/analyze.py` marks a cell usable using calibration completion ratios, FIFO wait and ingress-to-delivery latency; it does not noise-gate overflow or transport-declared loss. It maps any usable cell’s median screening breach to `reject`. The registered 0.4486 percentage-point paired overflow increase is approximately five extra overflow datagrams per roughly 960-datagram transfer. The comparison controls themselves ranged from 109 to 120 overflow datagrams, or 11.3779%–12.3967%. This descriptive observed range is not a confidence interval and does not establish that the paired effect is noise or a stable population distribution.

The deciding-cell smoke observations were control 120/968 and candidate 115/962. The separate calibration pair was control 109/956 versus control 109/957. The five comparison-control observations were 120/968, 109/958, 109/956, 115/964 and 109/956; the matched candidate observations are in the table below. Smoke observations are excluded from calibration and comparison summaries.

The issue brief says a reproducible correctness defect or material regression yields rejection. Here, the retained `reject` is the preregistered analyzer’s operational screening label: the experiment supplies no independent materiality or loss-variability argument establishing a stronger causal reject-versus-defer judgment. Those broader questions remain inconclusive. The observed screening breach and absent qualifying startup benefit do not support production adoption; this report preserves the registered result without upgrading its causal certainty.

The same cell’s median request-to-verified-EOF ratio was 1.00064 (range 0.99225–1.03687), so the measured extra loss coincided with no qualifying startup improvement. Forward p95 FIFO wait was 97.963 ms control and 98.058 ms candidate; the paired increase was 0.095 ms and passed its separate limit. Control/control completion ratios were 1.00131 for request-to-EOF and 1.00250 for dial-to-EOF. The table below retains the deciding pairs rather than pooling them into a campaign average.

| Seed | Control / candidate IDs | Control / candidate completion (ms) | Relay overflow: count / ingress datagrams | Transport loss: count / sent QUIC packets |
| --- | --- | --- | --- | --- |
| 96401 | 081 / 082 | 1085.922 / 1077.502 | 120/968 / 114/962 | 120/969 / 114/963 |
| 96402 | 128 / 127 | 1081.089 / 1081.784 | 109/958 / 114/963 | 109/959 / 114/964 |
| 96403 | 173 / 174 | 1077.863 / 1086.718 | 109/956 / 120/969 | 109/957 / 120/970 |
| 96404 | 220 / 219 | 1080.813 / 1120.661 | 115/964 / 118/967 | 115/965 / 118/968 |
| 96405 | 265 / 266 | 1079.543 / 1079.238 | 109/956 / 114/962 | 109/957 / 114/963 |

## Complete cell results

Completion values below are medians across the five comparison runs for each arm. Ratios are medians of the five matched candidate/control ratios, followed by their full range. They are not ratios of arm medians. A transfer-time p95 or p99 is not estimated from five samples. Each cell’s complete paired observations and both directions’ absolute delay/loss/burst summaries are in [complete summary](summary.json.gz), with [cell-level detail](cells.json).

| RTT (ms) | Forward (Mbit/s) | Payload | Impairment | Request-to-EOF control / candidate (ms) | Request ratio, median [range] | Dial ratio, median [range] | Limits / limitations |
| ---: | ---: | --- | --- | --- | --- | --- | --- |
| 10 | 10 | 64 KiB | clean | 65.861 / 65.758 | 1.0000 [0.9970–1.0151] | 0.9932 [0.9877–1.0568] | screen limits passed |
| 10 | 10 | 64 KiB | loss | 65.631 / 65.681 | 1.0008 [0.9992–1.0123] | 1.0024 [0.9853–1.0485] | screen limits passed |
| 10 | 10 | 64 KiB | reorder | 65.734 / 65.818 | 1.0003 [0.9894–1.0048] | 1.0001 [0.9796–1.0158] | screen limits passed |
| 10 | 10 | 1 MiB | clean | 871.586 / 871.254 | 1.0001 [0.9995–1.0008] | 1.0000 [0.9992–1.0021] | screen limits passed |
| 10 | 10 | 1 MiB | loss | 871.301 / 871.427 | 1.0000 [0.9991–1.0020] | 0.9999 [0.9986–1.0026] | screen limits passed |
| 10 | 10 | 1 MiB | reorder | 883.739 / 881.479 | 0.9994 [0.9909–0.9999] | 0.9990 [0.9910–1.0003] | screen limits passed |
| 10 | 100 | 64 KiB | clean | 25.558 / 24.557 | 0.9806 [0.9359–1.0042] | 0.9940 [0.9714–1.0181] | noisy calibration dial_ns |
| 10 | 100 | 64 KiB | loss | 25.374 / 24.632 | 0.9671 [0.9602–0.9751] | 0.9722 [0.9544–0.9835] | screen limits passed |
| 10 | 100 | 64 KiB | reorder | 25.652 / 24.576 | 0.9680 [0.7888–0.9912] | 0.9807 [0.7565–0.9925] | insufficient comparison d1 wait_ns; insufficient comparison d1 latency_ns |
| 10 | 100 | 1 MiB | clean | 109.475 / 110.483 | 1.0032 [0.9538–1.0320] | 1.0062 [0.9607–1.0315] | screen limits passed |
| 10 | 100 | 1 MiB | loss | 110.313 / 109.536 | 1.0021 [0.9558–1.0683] | 0.9987 [0.9590–1.0886] | screen limits passed |
| 10 | 100 | 1 MiB | reorder | 130.826 / 129.942 | 1.0019 [0.9838–1.0118] | 0.9982 [0.9870–1.0080] | noisy calibration request_ns; noisy calibration dial_ns |
| 100 | 10 | 64 KiB | clean | 226.492 / 224.903 | 0.9962 [0.9881–0.9978] | 0.9986 [0.9934–1.0011] | screen limits passed |
| 100 | 10 | 64 KiB | loss | 226.698 / 226.045 | 0.9955 [0.9909–1.0071] | 0.9976 [0.9945–1.0109] | screen limits passed |
| 100 | 10 | 64 KiB | reorder | 227.580 / 225.525 | 0.9929 [0.9829–0.9953] | 0.9947 [0.9818–1.0003] | screen limits passed |
| 100 | 10 | 1 MiB | clean | 1080.813 / 1081.784 | 1.0006 [0.9922–1.0369] | 1.0004 [0.9921–1.0319] | d0 overflow_rate regression; d0 transport_loss_rate regression |
| 100 | 10 | 1 MiB | loss | 1069.026 / 1069.408 | 0.9991 [0.9930–1.0087] | 0.9994 [0.9942–1.0069] | screen limits passed |
| 100 | 10 | 1 MiB | reorder | 1276.079 / 1277.628 | 0.9983 [0.9943–1.0012] | 0.9984 [0.9949–1.0004] | screen limits passed |
| 100 | 100 | 64 KiB | clean | 223.840 / 217.671 | 0.9756 [0.9713–1.0192] | 0.9850 [0.9809–1.0157] | screen limits passed |
| 100 | 100 | 64 KiB | loss | 223.284 / 217.449 | 0.9719 [0.9658–0.9831] | 0.9806 [0.9765–0.9903] | screen limits passed |
| 100 | 100 | 64 KiB | reorder | 223.592 / 216.842 | 0.9703 [0.9662–0.9965] | 0.9805 [0.9754–0.9959] | screen limits passed |
| 100 | 100 | 1 MiB | clean | 589.718 / 560.689 | 0.9508 [0.9499–0.9516] | 0.9574 [0.9563–0.9585] | screen limits passed |
| 100 | 100 | 1 MiB | loss | 719.495 / 707.013 | 0.9827 [0.9511–1.0086] | 0.9862 [0.9553–1.0085] | screen limits passed |
| 100 | 100 | 1 MiB | reorder | 1143.162 / 1144.682 | 0.9996 [0.9912–1.0043] | 0.9998 [0.9915–1.0053] | screen limits passed |

Two calibration cells were noisy: clean 10 ms / 100 Mbit/s / 64 KiB (dial completion), and reordered 10 ms / 100 Mbit/s / 1 MiB (both completion intervals). One reordered 10 ms / 100 Mbit/s / 64 KiB cell had fewer than 20 delivered reverse datagrams in a comparison run, so the corresponding packet quantiles are insufficient. Those three cells cannot support a favorable conclusion. No independently evidenced contamination justified replacements. Their limitations remain separate from the deciding cell’s screening breach; that cell’s completion/delay usability does not establish a noise bound for its loss metrics.

## Timer, burst and preservation evidence

The sender-level regression first failed against unchanged pacing at 2.5 ms and passed at 1.5625 ms for 100 ms RTT, 32 packets and 1280-byte packets. Reno and Cubic modes are covered. Additional tests cover the 1 ms floor at 64 ms RTT, the rounded 1.015627 ms deadline at 65 ms, 1 ms RTT with a 163,840-byte candidate pacing allowance, a nondecreasing sender deadline after datagram-size increase, loss and HyStart exit, timeout/migration re-entry, constructor states, and congestion-window admission. The frozen sender packet-size assertion accepts equality, so it would not detect a no-op size update; correct propagation in the 100 ms above-floor case would give 3.125 ms. Existing `TestPacerUpdatePacketSize` independently checks exact deadline doubling and burst resizing at the pacer component. Existing rounding and overflow tests remain intact. The byte-conversion-before-gain order also bounds 2× multiplication for the uint64 Bandwidth representation; no wider arithmetic claim is introduced.

In native runs the server initially receives a zero restored RTT (`SetInitialRTT(0)` on the pinned base), exercising the existing 1 ms fallback. The first observed server rates were 51,200,000 bytes/s in control and 81,920,000 in candidate. Token allowances and socket bursts remain distinct: in the deciding cell both arms’ largest observed 100 µs-gap socket burst was 10 datagrams despite larger initial token allowances. Subsequent measured RTTs and phase/rate changes are retained in raw endpoint events. This shows why the algebraic gain alone does not predict emitted bursts or startup benefit.

The execution session reported congestion/ackhandler tests in both arms, candidate race tests, focused `Test(BBRPacing|ConnectionPacketPacing|Emission)` preservation tests, package vet and module tidiness. The retained Go logs contain bare package-success output and do not independently establish the invocation flags or focused selection. [Validation provenance](validation-provenance.md) maps the reported commands and their portable-evidence limits; root package vet/tidiness lacks an archived invocation receipt. The fixture build script explicitly contains its endpoint vet/tidiness commands. The archived sender tests and [local logs](raw/sender-boundaries.log) record the exact boundaries. Six deterministic fixture tests cover serializer timing, in-service queue capacity/overflow, impairment reproducibility, stream-offset retransmission accounting, quantile/zero-baseline handling and the complete schedule. All four native smoke connections verified size/hash, sender observations, socket writes and clean process shutdown before the comparison stage.

## Evidence semantics and limitations

Forward means server-to-client response traffic; reverse means client-to-server request/ACK traffic. Each direction serializes actual UDP payload bytes and includes in-service bytes in its finite FIFO capacity. Injected loss, queue overflow and transport-declared loss are distinct recorded counters. Reordering adds one base RTT after serialization and adds no injected loss. Queue waiting, serialization, propagation, injected reordering and late-delivery scheduler error are separate per-datagram fields. Delivered-datagram nearest-rank p50/p95/max values and sample counts are reported separately in each direction; occupancy is sampled at ingress, not time-weighted. Datagrams still pending at teardown remain explicit in raw relay results and are excluded from delivered-latency quantiles.

Endpoint writes record successful ordinary UDP `WriteTo` submissions; bursts use a registered 100 µs inter-write start gap and nonempty 1 ms bins. This is socket-emission evidence on loopback, not hardware wire timing. Counts include handshake and bounded teardown as well as application transfer. Transport loss is loss-event count divided by sent QUIC packet-event count; relay overflow uses ingress UDP datagrams. Repeated STREAM byte intervals are separately reported with sent-STREAM-byte denominators and are not equated with lost packets. Endpoints use real TLS 1.3 and verified application EOF; write completion and transport acknowledgments never substitute for payload verification.

The ordinary `PacketConn` wrapper exposes neither `SetReadBuffer` nor `SetWriteBuffer`, so quic-go’s interface-based automatic socket-buffer enlargement was unavailable in both arms. Retained client/server logs contain the receive-buffer warning. Actual socket-buffer sizes were not recorded. Identical wrappers do not establish immunity from interactions between socket buffering, the candidate’s changed gain and the userspace relay.

The host was shared, and one control/control pair is only a sensitivity check, not a confidence interval. Five comparison pairs cannot establish population tails, multi-flow fairness, sustained-load behavior, DATAGRAM performance or cross-platform qualification. The frozen screen labels the observed overflow/loss breach as rejection, with the metric-specific uncertainty stated above. A revised gain/burst approach would need a new proposal and budget; no tuning or second campaign occurred.

## Reproduce analysis and inspect sources

This directory is frozen assessment evidence, not maintained runtime or CI tooling. [Raw attempt archive](raw/attempts.tar.gz) contains every process command, client/server/relay result, ready file and log. [Per-file hashes](raw/attempt-files.json) cover its contents. [Checksums](SHA256SUMS) cover publication artifacts. Extract only the archive into this directory’s `raw/` to reproduce summaries without network access:

```sh
tar -xzf raw/attempts.tar.gz -C raw
python3 -m unittest discover -s fixture
python3 fixture/analyze.py
```

The registered runner refuses campaign restart. Reconstruction commands and exact local module replacement paths are in `fixture/build.py` and `build-identities.json`; apply `common.patch` to the pinned source and then `candidate-only.patch` for the candidate. The fixture certificate is a local test certificate with its recorded validity and is not a credential for any service. Analysis requires Python’s standard library; rebuilding uses the pinned Go toolchain and module sums. Re-execution of network experiments requires a separately approved registration.

## Relation to BBR preservation

The assessment control is default Reno at `1c4a28b7`, not historical frozen Reno `e4f322cb`. Since the brief was written, [#887 closed with accepted option C](https://github.com/the-sarge/quic-go-fast/issues/887#issuecomment-6355291699): `8095c72244755f0c1e24f4158253bf09cfbe6e17` is the blocking BBR preservation reference, and `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` is descriptive. These references, BBR readiness rules and historical evidence are unchanged.

## Review disposition

[Review and independent dispositions](review.md) record the five proposed findings, four accepted documentation corrections, offline checks and the docs-only review exemption. Measured artifacts and the registered result are unchanged.
