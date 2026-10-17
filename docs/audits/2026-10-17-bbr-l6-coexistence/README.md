# BBRv3 L6: coexistence of the production port with a competing Reno flow

**Date:** October 17, 2026, America/New_York. **Scope:** [#777](https://github.com/the-sarge/quic-go-fast/issues/777), the acceptance plan's L6, carried by D5 of the [BBR port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md) from [#747's D4](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md#d4--the-smallest-qualification-that-would-change-an-adoption-decision-l6-coexistence-named-and-not-started). Measurement only: no code change, adoption, default-controller change, release, CHANGELOG change, paid resource, host-setting change or ledger change.

Branch `codex/bbr-l6-coexistence`, based on `abfa5456`, the candidate itself. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666) and [#748](https://github.com/the-sarge/quic-go-fast/issues/748) record, this record stays off `main`; a dev-journal entry on `main` points to it.

**Status: decisions accepted; no fixture built and no observation made.** This commit records the operator's decisions before any fixture, calibration, development or counted run of this record exists. The registration follows in later commits, before any counted observation.

## Question

When BBRv3 shares a bottleneck with a competing QUIC Reno flow, does it displace that flow beyond a fairness criterion fixed before any data?

## Accepted decisions (before any data)

The agent presented one consolidated plan with six recommended decisions on October 17, 2026, at about 01:50 America/New_York. The operator answered "Approve, threshold 0.70": every recommendation stands except D2's threshold, which the operator set at 0.70 instead of the recommended 0.80.

- **D1, the revision: `main` at `abfa5456d99a9299825d04454a2e7ed12dff3326`**, the revision the [port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md) advanced on Linux. Later `main` commits are not measured. `main` has since gained [#779](https://github.com/the-sarge/quic-go-fast/pull/779) (`81a72481`), a production change to the handshake packet-size fallback, so a later commit would be a new revision needing its own readiness measurement and advancement disposition first.
- **D2, the fairness criterion, fixed before data.** For each workload (STREAM, DATAGRAM), pair and period, the ratio is the competitor's useful delivery when the focal flow is BBRv3 divided by its useful delivery when the focal flow is matched Reno in the same pair. The classified periods are the measured intervals t = 0–100 s (equal round trips), t = 100–200 s (the competitor on the 25 ms round trip) and t = 270–300 s (after the competitor resumes).
  - *Acceptable:* in every classified period of both workloads, at least four of five pairs are at or above **0.70**.
  - *Adverse:* in any classified period of either workload, at least four of five pairs are below 0.70.
  - *Inconclusive:* any other result, including a period with fewer than four usable pairs on either side of the threshold.
  - Reported beside the classification and never changing it: the competitor's control-response latency (flagged when more than 20% higher than with matched Reno, the acceptance plan's control-latency tolerance), and the focal flow's own useful delivery, control p95, CPU time per useful GiB and peak RSS against matched Reno under the unchanged readiness limits. No readiness exception covers any of them.
  - The registration owns the estimator details (period binning, the treatment of a too-small denominator, usability). It may not change the threshold, the periods or the three outcomes without the operator.
- **D3, an A/A arm.** Each pair also runs matched Reno a second time. Its ratios are reported beside each period and never override the classification.
- **D4, no L8.** It is not established that GridCast will run several BBR connections over one bottleneck.
- **D5, process.** This record branch with a pointer entry on `main`; one `ras consider` of the registration before any counted observation, with at most one replacement consideration after fixes; operator acceptance of the registration before the counted stage; the `minimax` quiet window (or an unattended run) requested only when the stages are ready.
- **D6, the competitor and matched Reno: frozen Reno, `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`**, the readiness baseline every readiness record used. No Reno-on-candidate arm runs, so this record measures no preservation cell.

## Known before these decisions

- The [Q1 campaign](../2026-09-25-bbrv3-q1-campaign/README.md) ran L6, L7 and L8 once each on cloud hardware as orchestration examples, with **Reno** as the focal flow, and judged them only by process exits and gateway gates. The agent saw per-period figures derived from those archived records: in the L6 example, the competitor delivered about 47, 63 and 53 Mbit/s in the three classified periods and the focal Reno about 49, 32 and 43 Mbit/s. The L8 example had the frozen (pre-correction) BBRv3 as the *competitor*. No run anywhere has had BBRv3 as the focal flow of a coexistence case.
- At this path's bandwidth-delay product (about 857 full-size packets), Reno recovers from a window reduction over minutes, so a Reno-against-Reno split within a 100 s period varies between runs. With five pairs, *inconclusive* is a realistic outcome.
