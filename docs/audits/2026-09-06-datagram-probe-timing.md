# Targeted DATAGRAM probe timing diagnostic

The diagnostic identifies substantial probe delay before send admission and on the return transport path in both variants. It does not establish a candidate-specific regression or clear the previous 4 Gbps p99 bound. PR 16 remains an experimental draft with unchanged production code.

## Campaign and measurement effect

Following the [longer-window comparison](2026-09-06-datagram-tail-followup.md), a frozen 16-attempt diagnostic completed at 4 Gbps: four original-binary controls, eight timing-only runs and four runs with scheduled five-second endpoint execution traces. Every attempt completed, with no replacements, exclusions or adaptive extension. Traffic remained native QUIC loopback, 1071-byte records and 100 sparse 64-byte echo probes/s, with 2 seconds warmup, 60 seconds measurement and 1 second drain. Each endpoint retained two dedicated physical cores and two Go processors; unused SMT siblings were reserved.

The instrumented variants used the same native Linux Go 1.27.1 toolchain and identical external measurement source, differing only in the two audited public parser files. Public baseline source remains `686bce6541438101c96732a023984e031faf6df7`, and candidate runtime remains `391b3df6932fec5f14fc0396947a0c2e4fd8e2a6`. Private diagnostic hooks do not enter this repository.

Calibration paired original and instrumented binaries twice per variant, reversing order in the second round. Timing-enabled p99 was 7.91% and 7.98% higher for baseline, and 7.37% and 3.04% higher for candidate. At the same time, p50 fell 15.6–29.9% and goodput rose 0.41–0.85%. This small calibration does not isolate the mechanism, but shows that these diagnostic measurements cannot substitute for the original acceptance campaign. Small timestamp-placement differences do not remove that limitation.

## Location of the delays

Within each timing-only run, the slowest 1% was selected using original successful RTT, then the stages of those same probes were examined. Medians across four run-level median shares per variant were:

| Portion of slow response | Baseline | Candidate |
| --- | ---: | ---: |
| Request begin to receiver observation | 42.0% | 44.7% |
| Receiver observation to reply begin | 0.0051% | 0.0053% |
| Reply begin to sender observation | 58.0% | 55.3% |
| Before send-lock acquisition, overlapping request leg | 39.8% | 42.1% |

The first three legs partition an individual measured response; the admission row overlaps them. Do not add overlapping spans or sum independent medians as though they describe one response. Observation timestamps follow application validation and are not kernel-arrival timestamps.

Eight endpoint traces decoded successfully. The four trace windows contained 1917 fully matched probes in total. Each window's slowest five probes formed a separate trace cohort; their median RTTs were 2.030–2.294 ms. Within these cohorts:

- Median time before send-lock acquisition was 1.020–1.231 ms, including 0.691–1.004 ms blocked and 0.228–0.404 ms runnable. Stacks locate almost all blocking at the measurement sender's shared send mutex, with a small context-setup contribution. Runnable means ready to execute but not yet resumed.
- The return observer span had medians of 0.793–0.984 ms, including 0.691–0.968 ms waiting for transport data and 0.024–0.100 ms runnable. This does not distinguish reply scheduling, transmission, packet processing and receive notification; packet-level boundaries remain missing.
- Individual stop-the-world overlap was at most 0.086 ms per endpoint. It was much smaller than the observed millisecond delays, and both endpoints had zero overlap with every probe in the second candidate cohort. This does not rule out all GC or scheduler effects.

These state medians are descriptive and need not add up to the median span. The same broad pattern appears in both variants. The trace-window p99s were 1.964–2.105 ms, while whole-run p99s for those captures were 2.195–2.386 ms; those different populations do not estimate an isolated tracing effect. Twenty traced tail probes provide attribution evidence, not a candidate-ranking confidence interval. [Aggregate results](datagram-probe-timing/aggregate-results.json) retain calibration and run-group counters; [trace summaries](datagram-probe-timing/trace-summaries.json) retain the cohort values without private stack/source contents.

## Validation and next decision

Normal and race tests passed for both native measurement variants, including probe identity/clock ordering and trace cancellation/cleanup. Independent ledgers, original RTT histograms, matched probe counts/order, binary identities and trace hashes/sizes validated, with no timing-record overflow or truncated traces. Timing-only baseline/candidate runs attempted 22530/22465 probes and missed 27/25 replies, including one local rejection each. Successful-response tails remain conditional on receipt.

Placement checks passed before warmup and throughout all captures. Reserved cores recorded zero guest/steal time and unused SMT siblings averaged 99.960% idle. The temporary partition was removed and all reserved cores released. Spare host cores and the configured two-processor endpoint budget are separate facts: a runnable goroutine cannot automatically use every idle host core beyond that budget.

The earlier 4 Gbps result remains +4.04% [−0.20%, +8.45%] against the unchanged 5% bound. A useful next bounded diagnostic is two versus four dedicated cores and Go processors per endpoint, matched across both variants, with admission delay and return delay reported separately. If return waiting persists, reply enqueue/arrival/notification boundaries are the next missing observation. This recommendation is not an executed test, a replacement acceptance gate, or evidence that additional cores will remove serialization. Independent review, full adoption certification, merge and completed D1/D2/program tracking remain outstanding or unchanged.

Raw evidence and measured source/binaries are retained privately on two hosts. The evidence archive SHA-256 is `8b47c066e6bd856a4b44005b7802ec58952cae6c00650e0b4cd3359c39638c1a`. Only aggregate evidence is public, so this repository alone cannot reproduce the private fixture. The experiment does not qualify physical-link, disk, FEC, file-completion or maximum-capacity behavior.

The subsequent [two-versus-four-core comparison](2026-09-07-datagram-core-budget.md) completed all 32 planned captures. More cores cut original-binary p99 by about two-thirds in both variants, with about 19% more CPU time per delivery; admission remains prominent in the residual tail and the candidate-specific acceptance question remains unresolved.
