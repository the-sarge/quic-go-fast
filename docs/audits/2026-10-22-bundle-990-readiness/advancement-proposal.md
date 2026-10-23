# #990 fresh advancement proposal

**Status: proposed, not accepted.** This document requests an explicit operator decision for candidate **`8095c72244755f0c1e24f4158253bf09cfbe6e17` on Linux**, permitting progression toward experimental prerelease `v0.62.1-fast.8`. It grants no exception or advancement itself. The [checked result](result.md), raw archive and manifests in this record are the evidence being presented.

## Recommendation and alternatives

**Recommend A: advance with the precisely scoped decisions below.** S6 BBR useful throughput is 14.061× (12.153–15.622) for STREAM and 14.268× (11.784–15.582) for DATAGRAM, above the intended bundle Reno in all five pairs of each workload. The registered adequate default-Reno latency follow-up does not reproduce the raised short-run result. BBR remains experimental, opt-in and unqualified, and Reno remains default. These facts support accepting the recorded costs for this experimental bundle, not declaring the twelve flagged rows measured passes or claiming that the eleven BBR costs are unavoidable.

**B: hold publication under the unchanged advancement rules.** Grant no exceptions and leave all flags blocking. A separately registered cause/remedy investigation or an explicit later policy decision would be required before progression. No product behavior or default is changed by either choice. No additional campaign is launched by this proposal.

## Evidence and all raised cells

Complete observations: 140 readiness, eight S5 timeline and twelve longer latency observations; 44 clean original counted blocks, zero measurement failures/reruns, no timeline escalation and no conditional-selection omission. Seven prerequisite smokes are excluded. All source/build/controller/configuration/affinity, integrity and known-contamination checks passed. The full per-block ratios, absolute medians, A/A ranges, original-only view, historical comparison and DATAGRAM loss are in the mechanically checked result.

Ratios use Reno at this same intended release commit as the blocking reference, following [#887 option C](https://github.com/the-sarge/quic-go-fast/issues/887#issuecomment-6354232820). The independent Reno builds are byte-identical; this does not establish preservation against old Reno and does not automatically clear a random raised flag. Absolute values below are arm medians; their quotient is not necessarily the median of paired ratios.

| Path/workload | Arm | Raised metric | Paired median (min–max) | Crossing blocks | Absolute reference → arm | A/A ratio min–max |
| --- | --- | --- | --- | --- | --- | --- |
| loopback/stream | cand-bbrv3 | goodput | 0.896879 (0.892301–0.903444) | 5/5 | 6079.945978 → 5452.828973 Mbit/s | 0.992769–1.002367 |
| loopback/stream | cand-bbrv3 | send_cpu | 1.114559 (1.111970–1.122928) | 5/5 | 3.606388 → 4.019438 s/GiB | 0.995406–1.002634 |
| loopback/datagram | cand-bbrv3 | goodput | 0.897020 (0.891718–0.901687) | 5/5 | 5212.629619 → 4667.170765 Mbit/s | 0.992919–1.009118 |
| loopback/datagram | cand-reno | control_p95 | 1.274805 (0.252346–2.704924) | 3/5 | 0.341129 → 0.375263 ms | 0.622691–4.000195 |
| S5/stream | cand-bbrv3 | send_rss | 1.192647 (1.157039–1.223240) | 5/5 | 17.917969 → 21.667969 MiB | 0.956621–1.028123 |
| S5/stream | cand-bbrv3 | receive_rss | 1.247554 (1.098684–1.438950) | 4/5 | 16.917969 → 21.417969 MiB | 0.982532–1.014777 |
| S5/datagram | cand-bbrv3 | send_rss | 1.148574 (1.108590–1.168302) | 5/5 | 17.917969 → 20.500000 MiB | 0.959279–1.014103 |
| S6/stream | cand-bbrv3 | send_rss | 1.696704 (1.334999–1.716057) | 5/5 | 13.417969 → 21.917969 MiB | 0.969919–1.041921 |
| S6/stream | cand-bbrv3 | receive_rss | 1.450463 (1.388513–1.525898) | 5/5 | 14.167969 → 20.187500 MiB | 0.947064–1.017339 |
| S6/stream | cand-bbrv3 | control_p95 | 1.393585 (1.316045–1.587968) | 5/5 | 126.672987 → 176.529526 ms | 0.978961–1.036940 |
| S6/datagram | cand-bbrv3 | send_rss | 1.509143 (1.382436–1.547122) | 5/5 | 13.843750 → 20.832031 MiB | 0.987381–1.050857 |
| S6/datagram | cand-bbrv3 | control_p95 | 1.336182 (1.318518–1.371187) | 5/5 | 124.296321 → 166.930283 ms | 0.944200–1.059177 |

The eleven BBR rows are **unattributed** on this candidate. The CPU split places the raised loopback STREAM sender cost inside the measured window (ratio about 1.114), but that localization is not a mechanism or cause. S5 timeline is model behaviour (STREAM 4/4, DATAGRAM 3/4 with one no-deficit block); it does not attribute loopback costs, memory or S6 latency. No current-candidate memory or S6-latency cause-discrimination stage was registered. Missing attribution remains explicit.

## Default-Reno preservation follow-up

The short-run loopback DATAGRAM p95 flag is 1.274805, three of five pairs crossing 1.20; arm medians are 0.341129 → 0.375263 ms. A/A spans 0.622691–4.000195 and clears nothing. The registered longer plain-build stage has six clean paired blocks, **120 control replies in every run, 720 per arm**, satisfying ≥100/run and ≥500/arm. Pooled p95 is **0.920616 → 0.767539 ms**, ratio **0.833723**, 95% block-bootstrap interval **[0.658301, 1.127269]**, using the registered 2,000 resamples and seed715. Per-block ratios are 1.304667, 1.277574, 0.431990, 1.129703, 0.896738, 0.418510. Outcome: **not reproduced**, because the upper interval is below 1.20. No cause is inferred.

The original flag remains raised. There is no standing rule mapping this follow-up outcome to the end of blocking for this candidate. Accepting that mapping is an explicit, result-informed interpretation of D1 condition3 and [Linux D3](https://github.com/the-sarge/quic-go-fast/blob/efdac495549e3ac91deb1bf9971bc549558961f9/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md#d3--preservation-flags-stay-blocking-resolve-them-with-metric-specific-evidence), confined to this cell/revision/platform. It accepts no demonstrated regression, supplies no cause, and rests on the adequate registered follow-up, not A/A overlap or binary identity. Earlier such decisions are analogies, not authority.

## Required fresh decisions for option A

1. Grant **eleven unattributed readiness exceptions**, exactly the eleven `cand-bbrv3` cells in the table, for this candidate on Linux and this recorded evidence at the displayed magnitudes (full precision retained in summary.json). Each flag stays raised. Explicitly waive D1 condition2’s cause-attribution requirement and amend condition3’s unresolved-flag clause for these eleven cells only. Permit progression without further cause-discrimination or remedy experiments on these cells; to the extent the remedy-before-exception requirement would otherwise require them, waive that requirement for these named cells/revision/platform only. This is not a finding that their costs are algorithmically necessary or that a contract-preserving remedy is impossible.
2. End the **single default-Reno loopback DATAGRAM p95 flag’s blocking state** on the adequate registered not-reproduced outcome, by the candidate-specific result-informed interpretation described above. Do not grant a preservation regression exception or erase the original flag.
3. Explicitly **permit progression to publication of this pinned candidate** as the experimental opt-in BBR bundle under #851, after the remaining release-note, immutable-tag, real-tag consumer and tag-triggered workflow gates pass. This is neither Q2 qualification nor macOS/Windows advancement, and does not authorize substituting later main.

All other thresholds and preservation rules remain unchanged. No exception covers protocol/payload integrity, ownership, bounded resources, ECN/loss correctness, a different cell, platform or revision, or any later measurement. No historical exception transfers. The agent cannot grant any of these decisions.

## Historical context and coverage limits

Current BBR loopback goodput against the same-block **old descriptive Reno** is 1.155890× STREAM and 1.002416× DATAGRAM; current candidate Reno is 1.288064× and 1.117008×. Prior #808’s BBR ratios against its own old-reference observations were 1.160205× and 0.995817×, on a different day. This contextualizes the new 0.897× blocking-reference flags without proving a cause or establishing a controlled across-day BBR non-regression. The old reference remains descriptive and cannot clear new-reference flags.

The carried S5 DATAGRAM sender CPU and run-loop wake cost are reported in result.md’s full historical ratio table and CPU/GiB comparisons. Wake counts are not measured. Current DATAGRAM loss is descriptive: BBR S5 median0.0254%, S6 median0.1334%; these are not native gluing or batched-send results. The earlier L6 campaign remains adverse in all six cells; it has not been rerun or qualified here.

The preserved Mac flags were reviewed alongside this Linux evidence: the old `fc4c1bf1` macOS D2 exception covered S6 control p95, S6 STREAM receiver RSS and S5 sender RSS only. [#747’s Mac review and Linux-only scope](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md#mac-records-reviewed) remain historical and unchanged; no Mac exception is extended. No macOS or Windows readiness record measures this candidate. The fixture’s nil sendBatch callback means it exercises neither #858’s batched-send cap nor gluing; native offload/read-slot behavior, HTTP/3 workloads, consumer adoption opt-ins and direct wake counts remain outside its coverage. The old loopback STREAM queue-bound remedy remains untested and its old-reference latency comparison is descriptive under option C.

Consumer measured passes, amended readings, accepted failing rows and platform limitations remain the separate evidence linked in #851. BBRv3 stays experimental, opt-in and unqualified; Reno stays default. #599/Q2 remains a later separately dispatched campaign, #971 is excluded/deferred, and #962/#971 stay open.
