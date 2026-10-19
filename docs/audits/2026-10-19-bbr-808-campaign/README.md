# #808: one readiness and L6 campaign on the combined BBRv3 revision

**Date:** October 19, 2026, America/New_York. **Scope:** [#808](https://github.com/the-sarge/quic-go-fast/issues/808). Measurement only: no product code change, adoption, default-controller change, release, CHANGELOG change, paid resource, host-setting change or ledger change. Q2 ([#599](https://github.com/the-sarge/quic-go-fast/issues/599)) is not started here.

**Status: readiness and the side stage complete; the advancement disposition is drafted and awaits the operator. L6 has not run.** This record holds the campaign's decisions and, after each stage, its answer, the operator's advancement disposition and the requirements the campaign discharges. The stage records hold the registrations and results:

- [Readiness](../2026-10-19-bbr-808-readiness/README.md): slice 6's stage on the combined revision.
- [Side stage](../2026-10-19-bbr-808-gso-receive/README.md): #743's STREAM cost, the received-DATAGRAM discard counter, and #743 on BBRv3, on loopback.
- [L6](../2026-10-19-bbr-808-l6/README.md): #777's registration with #792's trace arm; runs only after an advancing disposition.

## Why one campaign

Every change to BBRv3's code makes a new revision, and each new revision needs its own readiness measurement and an operator advancement disposition before L6 or qualification (the [port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md#d5--what-advancement-leads-to-l6-named-and-not-started), D5). On October 18, 2026, the operator chose to measure once, on the revision that has #742, #800 and #803, instead of separately for each; #743 was decided and merged first, and #824 followed. The revision is `main` at `d94b3ec2` (re-pinned from `e7eb3f10` before any data; D1).

## Accepted decisions (before any data)

The agent presented one plan with eleven recommended decisions on October 18, 2026, and the operator answered "Approve all (Recommended)" at about 23:20 America/New_York. At about 23:50 the same evening, after a development pair, the operator amended D3 (below).

| # | Decision |
| --- | --- |
| D1 | **The revision:** `main` as of the registration commit, `e7eb3f10`; a BBR or send/receive-path code change on `main` before the readiness run stops the campaign for a decision. **Applied:** #836 and #844 changed the shared send path before any data, and the operator chose on October 19 at about 02:30 to re-pin the candidate to `main` at `d94b3ec2`. Later commits are not measured. |
| D2 | **Readiness:** slice 6's stage unchanged except the candidate, with slice 6's values beside each cell as context; nothing pre-cleared; slice 7's exceptions do not carry over. |
| D3 | **Amended: no extra arm.** The plan pre-registered a conditional Reno-on-candidate arm with #743's batching switched off. A development pair then showed that the frozen fixture's managed packet-I/O lease never GSO-batches (one `sendmsg` per packet with and without the switch), so #742 and #743 cannot reach readiness or L6 and the arm could isolate nothing. The operator chose "Drop the arm (Recommended)": readiness runs slice 6's stage exactly, and a raised Reno-on-candidate cell goes to the disposition with #824 named as a known, unattributed contributor. |
| D4 | **The disposition stop:** after readiness the agent stops and gives the operator a plain summary with one recommended advancement disposition, checked against every standing rule and the cell's history in every readiness record; if it recommends an exception or override, one `ras consider` reviews it first. L6 runs only after an advancing disposition. |
| D5 | **L6:** #777's registration unchanged plus #792's trace arm in pairs 1–3, with #800's one-line overlay update; the trace never enters the classification. |
| D6 | **#800's requirement:** STREAM's displacement beside #777's and #792's, and the trace's per-packet application-limited marking, judged from `Delivery.Limited`, not qlog `limitation=`. |
| D7 | **#803's requirement:** ECN stays on if no BBRv3 flow stops marking ECT(0) in any usable L6 observation, from relay2's per-flow ECN counts. |
| D8 | **#743's STREAM cost and #824's counter:** a short loopback side stage in the readiness window on #743/#824's harness with a controller flag; readiness and L6 keep their plain frozen-fixture binaries and report DATAGRAM loss from the fixture's counts. |
| D9 | **The record:** on `main` by one docs-only pull request, four sibling directories; raw archives on a pinned branch off `main`, their checksums on `main`. |
| D10 | **Reviews:** one `ras consider` of the whole registration before any counted data, at most one replacement; no separate registration-acceptance stop (a finding that would change a decision here stops the campaign for one); after results, planit's bounded RAS review of the pull request. |
| D11 | **Running:** unattended night windows by systemd timer, after the wiremux yield handshake and with GridCast's GARM CI idle; contamination triggers one rerun and never decides an outcome. |

## What the campaign must discharge

| Requirement | Source | Where |
| --- | --- | --- |
| Readiness on the fixed revision and its advancement disposition | #800, #803 (moved to #808 by the operator), the port advancement rule | Readiness; the disposition below |
| L6 again, with STREAM's displacement compared against #777 and #792 | #800 | L6, D6 |
| ECN stays on through an L6-length BBRv3 run | #803 | L6, D7 |
| Re-check #743's open STREAM cost (0.979×) | #743's K3 | Side stage, K3 |
| `DatagramReceiveDropsV1` beside DATAGRAM loss in DATAGRAM and mixed arms | #824's comment on #808 | Side stage; DATAGRAM loss also in readiness and L6 |

## Known before any data

- **Coverage.** The readiness and L6 fixture cannot exercise #742's or #743's send-path changes (D3's amendment); only the side stage measures #743 on BBRv3. #800, #803 and #824 do reach the fixture.
- **The agent's expectations** (stated in the plan, not results):
  - readiness probably raises the nine memory and latency cells that slice 6 raised on the port, so advancing would again need explicit operator exceptions;
  - a Reno-on-candidate loopback DATAGRAM cell may cross its limit, with #824's deeper queue a known contributor;
  - L6 very likely stays adverse overall, because #792 found DATAGRAM's displacement to be draft-06's own behaviour on this path, which nothing merged changes; what L6 can show is whether STREAM improved with #800 and whether ECN now stays on.

## Answer

**Readiness (complete).** On `minimax`, the combined revision (`main` at `d94b3ec2`) raises ten cells. Nine are the candidate memory and control p95 cells that slice 6 raised on the port and slice 7 excepted for `abfa5456` only: six now read lower and three higher. The tenth is a Reno-on-candidate preservation cell, S5 DATAGRAM sender RSS, at 1.157. It is raised on the registered clean blocks (four of five runs high, including both blocks never contaminated); the original blocks read 1.009, but there the contaminated runs were this arm's own, so that reading is context only. Its registered `presrss` stage, on the `-diag` builds, reads *not reproduced*. Every goodput and CPU cell passes; the S6 benefit holds (14.14× STREAM and 13.74× DATAGRAM, 5/5 pairs each; slice 6: 14.28× and 14.42×); the Reno-on-candidate loopback DATAGRAM control p95 cell that slice 7 ruled on is not raised (0.720). [Readiness record](../2026-10-19-bbr-808-readiness/README.md).

**Side stage (complete).** #743's STREAM cost is *not reproduced* (0.997×, against 0.979); `DatagramReceiveDropsV1` is zero by median in every DATAGRAM and mixed arm (nonzero in four Reno runs, at most 0.0019%); and #743 gives BBRv3 DATAGRAM 1.76× its throughput at 0.44× its sender CPU per GiB. [Side-stage record](../2026-10-19-bbr-808-gso-receive/README.md).

**L6:** not run; it waits for the disposition.

## Advancement disposition

**Status: draft, for the operator's decision; revised after its one `ras consider` review** (run `20261019T093631-c07a79d7c71f86724b446f85`; [dispositions](#disposition-review) below). The verdict under the unchanged condition is mechanical. The options and the recommendation follow the form of slice 7's [port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md). Every figure is from the readiness record's `summary.json`, `summary-original.json`, `attribution.json` and `port-context.json`; slice 7's accepted magnitudes are from its [`raised-cells.json`](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/raised-cells.json) at `ebfd9339`; each cell's history across #712, #715, #736, #739 and slice 6, and slice 7's accepted magnitudes, are in [cell-history.json](cell-history.json) ([cellhistory.py](cellhistory.py)).

### Normative constraints (quoted)

- **The first qualification decision's D1, the advancement condition** (`b5f759c6`): "1. the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload); 2. every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell (D2 sets the form); 3. no flag is raised and unresolved, and no preservation flag is raised; and 4. the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers."
- **The intervention qualification decision's D2** (`42e40111`), the remedy-before-exception rule: an implementation-integration cost is "a candidate for remedy, not for exception, while a contract-preserving remedy remains untested". For loopback STREAM control p95 the untested remedy is the bytes- or time-ahead queue bound.
- **The Linux qualification decision's D3** (`efdac495`), "Preservation flags stay blocking; resolve them with metric-specific evidence": "a raised preservation flag blocks advancement. Instruction agreement and A/A overlap are recorded as noise-compatible or inconclusive, never as noise attribution, and they never clear or except a preservation flag." It required each later readiness record to register "metric-specific measurement-adequacy and discrimination rules for any raised preservation cell", for RSS "a bounded heap-site and retention comparison of Reno on candidate against frozen Reno, together with the frequency of high-memory outcomes across blocks". And: "Any later proposal to let a preservation flag be covered by an exception would be a separate, explicitly identified operator decision. It would need a demonstrated cause."
- **The r8 decision's D3** (`232a6b76`), for the Reno-on-candidate loopback DATAGRAM control p95 cell only: "Any new revision re-measures the cell in readiness. A raised value there runs `preslat` again, and only its **not reproduced** outcome ends blocking. It is not an exception and covers no other cell." It called itself "an operator interpretation, proposed with r8's result already known".
- **The port advancement decision** (`ebfd9339`): its D2 waived condition 2's cause-attribution requirement, amended condition 3's unresolved-flag clause and waived the remedy-before-exception rule for loopback STREAM control p95, for `abfa5456` on Linux only. D5: "A qualification run on a later `main` commit is a new revision: it first needs its own readiness and an explicit advancement disposition." "No pre-clearance. No exception in D3 or D4 covers any qualification measurement." D4's preservation ruling "is not a standing rule".
- **The readiness registration** ([Registration: preservation](../2026-10-19-bbr-808-readiness/README.md)): "The readiness value stays as recorded whatever a stage finds, and **this record ends no cell's blocking state**: the advancement disposition decides."
- **Glossary, readiness exception:** "An explicit operator decision that one raised readiness flag does not block an advancement decision, for one candidate revision, platform and cell, covering the recorded evidence it names at no more than a recorded magnitude; the flag stays raised in every report, and the exception neither carries over to another revision or platform nor pre-clears later measurements. It is *attributed* when it rests on a registered cause attribution and *unattributed* when the operator grants it without a completed attribution or remedy test; neither kind changes the readiness limit."

### Verdict under the unchanged condition: not advanced

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 14.14× [12.06–15.69] STREAM, 13.74× [12.29–15.75] DATAGRAM, 5/5 pairs each | Yes |
| 2. Every flag passes, or is attributed and excepted | Ten raised cells, none attributed on this revision, none excepted | No |
| 3. No unresolved flag, no preservation flag | Ten unresolved cells; one raised Reno-on-candidate preservation flag | No (both clauses) |
| 4. Mac records reviewed, platforms named | Linux only. [#747's Mac review](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md#mac-records-reviewed) stands unchanged: the first decision's D2 exception covers `fc4c1bf1` on macOS only (S6 control p95, S6 STREAM receiver RSS, S5 sender RSS) and is not extended. No macOS or Windows record measures `d94b3ec2`; #747's D7 requires no Mac readiness for a Linux-only advancement. | Reviewed |

### The nine candidate cells against slice 7's accepted magnitudes

Slice 7 excepted these nine cells for `abfa5456` on Linux at slice 6's magnitudes; those exceptions do not carry over. Excesses are medians [largest] of per-block arm minus the same block's frozen Reno; latency differences are differences of arm medians.

| Cell | This revision | Slice 7 accepted (port) | Higher? |
| --- | --- | --- | --- |
| S5 STREAM sender RSS | 1.192 (4); 3.63 [4.38] MiB | 1.217; 4.11 MiB | No |
| S5 STREAM receiver RSS | 1.417 (5); 7.35 [7.63] MiB | 1.482; 8.62 MiB | No |
| S5 DATAGRAM sender RSS | 1.181 (4); 3.33 [4.02] MiB | 1.157; 2.93 MiB | Yes: +0.024, +0.40 MiB |
| S6 STREAM sender RSS | 1.640 (5); 8.88 [9.13] MiB | 1.656; 9.11 MiB | No |
| S6 STREAM receiver RSS | 1.481 (5); 6.92 [11.35] MiB | 1.832; 12.18 MiB | No |
| S6 DATAGRAM sender RSS | 1.481 (5); 7.02 [7.48] MiB | 1.466; 6.90 MiB | Yes: +0.015, +0.12 MiB |
| S6 STREAM control p95 | 1.414 (5); +55.2 ms (124.2 → 179.4 ms) | 1.581; +73.7 ms | No |
| S6 DATAGRAM control p95 | 1.415 (5); +57.5 ms (124.6 → 182.0 ms) | 1.461; +57.9 ms | No |
| Loopback STREAM control p95 | 1.981 (5); +0.099 ms (0.115 → 0.215 ms) | 1.656 (4); +0.069 ms | Yes, by more: 1.981 against 1.656, the highest of the cell's six records (#712 1.063, #715 1.035, #736 1.171, #739 1.485, slice 6 1.656); +0.099 against +0.069 ms. The candidate's own p95 is nearly unchanged (0.215 against 0.213 ms); frozen Reno's read lower (0.115 against 0.145 ms). The A/A arm also crosses this cell (1.222), which clears nothing. The queue-bound remedy stays untested. |

### The preservation cell in detail

**Reno on candidate, S5 DATAGRAM sender RSS, `d94b3ec2`, Linux: 1.157, three of five blocks crossing.**

- **What it measures.** The Reno-on-candidate arm runs the revision's fixture with Reno, the default, against frozen Reno. It bundles the port's Reno-reachable merge seams, every non-BBR change `main` carries since `e4f322cb` (since slice 6, among others #824's received-DATAGRAM queue, which campaign D3 names as a known, unattributed contributor, and #844's sent-packet history), and code placement. No arm separates them.
- **The reading.** Arm medians 18.38 → 21.26 MiB; median per-block excess 2.88 MiB, largest 3.63 MiB; per-block ratios 1.061–1.203. The A/A range is 0.914–1.070, so the median is above it. High-memory outcomes in readiness (peak RSS above 1.10× frozen Reno's median, 20.21 MiB): Reno on candidate 4 of 5, frozen Reno 1, A/A 0.
- **The registered reading and the original blocks.** GridCast CI contaminated three of the cell's five blocks; the registered rule reran each whole block with the same seed, and the reruns replaced them. The clean reading above is the registered one. On the original blocks (`summary-original.json`, #712's rule) the cell reads 1.009, but that is context only: in blocks 1–3 the contaminated runs were this arm's own (18.51, 19.76 and 18.76 MiB under foreign load; their clean reruns read 19.51, 21.76 and 21.26 MiB), and `summary-original.json` marks the cell contamination-sensitive with two clean blocks. Those two, blocks 4 and 5, were never contaminated and cross in both summaries (1.203 and 1.159).
- **The registered stage.** `presrss`, the RSS-specific discrimination rule the Linux decision's D3 required (registered in #715 and unchanged since): six clean blocks of `reno-diag` and `cand-diag` with Reno at both endpoints, with heap series. **Not reproduced:** RSS ratio 1.001, retained-heap ratio 1.020, no high-memory outcome in either arm. Two differences from readiness matter for reading it. It runs the `-diag` builds, as #715 registered it, whose frozen-Reno peak RSS is about 2 MiB higher than the plain build's, so its high-memory threshold is 22.47 MiB against readiness's 20.21 MiB (each 1.10× its own stage's frozen-Reno median). And its retained-heap comparison reads the measured window, while its RSS comparison reads the whole run's peak. In readiness's plain builds, Reno on candidate's sender heap peak is unchanged in the measured window (1.019) and higher during warm-up (1.240; arm medians 8.07 against 6.42 MiB; A/A range 0.832–1.144; slice 6 read 1.004, #712's raised case 1.251). That is descriptive: it neither locates nor causes the RSS difference, and it changes no outcome.
- **The cell's history** (Reno on candidate; the frozen-Reno A/A arm's median on the same cell in brackets): #712 **1.152** [1.055], #715 1.002 [1.036], #736 1.066 [0.986], #739 1.023 [0.990], slice 6 1.006 [1.067], this revision **1.157** [1.014]. Raised twice in six records; the Linux decision noted #712's reading lay above the A/A range and that nothing explained it.
- **Since slice 6.** The candidate's own S5 DATAGRAM sender RSS is unchanged (arm median 21.70 MiB in both records); Reno on candidate rose from 18.98 to 21.26 MiB (+2.28 MiB), to within 0.44 MiB of the candidate. Today their median excesses over frozen Reno are similar (candidate 3.33 MiB, Reno on candidate 2.88 MiB). This attributes nothing.
- **Default Reno.** The cell measures `main`'s default Reno at `d94b3ec2`, on this path and host. No option below changes Reno's code on `main`; they decide only whether BBRv3 is eligible for qualification. Whether `main`'s Reno uses more sender memory here stays open under every option unless it is tracked separately.
- **No rule yet decides this outcome.** The readiness registration leaves the cell's blocking state to this disposition. The r8 decision ended blocking on an adequate *not reproduced* outcome for the latency cell only ("covers no other cell"), by an operator interpretation proposed with its result known; its rule rested on `preslat`'s pooled p95 and bootstrap interval, while `presrss`'s rests on medians and high-memory counts. Ending this cell's blocking state on the `presrss` outcome would be a new, result-informed operator interpretation of condition 3 and the Linux decision's D3, for this cell, revision and platform only. r8 supplies the mechanism as an analogy, not as authority. It would not be an exception (it accepts no regression and attributes nothing), and it would not rest on A/A overlap. An actual exception for this cell would still need a demonstrated cause.

### Options

| Option | What it is | What it leads to | Rules it departs from |
| --- | --- | --- | --- |
| **A: advance on Linux now** | (1) Nine unattributed readiness exceptions for `d94b3ec2` on Linux, at the magnitudes above; each flag stays raised. (2) The preservation cell's blocking state ends on its registered `presrss` outcome (*not reproduced*), by a new operator interpretation for this cell, revision and platform only. Carried open: the default-Reno S5 DATAGRAM sender RSS reading (raised on the clean blocks; *not reproduced* by `presrss` on the `-diag` builds), with or without its own tracking issue, as you choose. | L6 runs next on `d94b3ec2` on `minimax`, unattended: about 4 h 20 min, up to about 6 h with reruns. Then the L6 result and #800's and #803's discharges come back to you. | For the nine cells, fresh for `d94b3ec2` on Linux, as slice 7's D2 did for `abfa5456`: condition 2's cause-attribution requirement waived; condition 3's unresolved-flag clause amended; the remedy-before-exception rule waived for loopback STREAM control p95. For the preservation cell: a new interpretation of condition 3 and the Linux decision's D3, made after the result was known (no standing rule maps a `presrss` outcome to the end of blocking). Kept: A/A overlap clears nothing; no preservation exception; no correctness contract touched. |
| **B (recommended): grant the nine now; hold the preservation cell for one plain-build re-measurement** | (1) The same nine candidate exceptions as A, granted now. (2) The preservation cell stays blocking. One re-measurement of S5 DATAGRAM sender RSS on readiness's plain builds, registered and reviewed before data: Reno on candidate against frozen Reno as in readiness, plus `abfa5456`'s Reno as a third arm (slice 6 read it at 1.006), so a rise since the port would be visible; peak RSS, high-memory outcomes and the warm-up heap peak reported. Its registration states before data which outcome the agent would recommend as ending the block. | About 1 h on `minimax` plus about 2 h of registration, review and analysis; then a second, short disposition of the preservation cell alone, still your decision. If the raise does not repeat, the agent would recommend ending the block on the registered outcome; if it repeats, the cell stays blocking and you choose again between advancing by decision and C, knowing whether the rise came after the port. L6 runs only after an advancing disposition. | For the nine cells, the same three departures as A. None for the preservation cell now. |
| **C: do not advance** | Nothing excepted. | The campaign stops after the record lands; #800's and #803's L6 requirements stay open; Q2 (#599) waits for a later revision. | None. |

**The agent's recommendation: B.** BBRv3's benefit holds and every goodput and CPU cell passes. The nine BBRv3 cost cells are the costs slice 7 accepted for the port: six read lower; two DATAGRAM sender-memory cells read slightly higher (+0.40 and +0.12 MiB); and loopback STREAM control p95 is higher in ratio (1.981 against 1.656, the highest of its six records), though the candidate's own p95 is nearly unchanged (0.215 against 0.213 ms) and frozen Reno read lower this time. Granting those nine needs the same three rule departures slice 7 made, fresh for this revision; its queue-bound remedy stays untested.

The preservation cell is different. On the clean runs, `main`'s default Reno used about 2.9 MiB (about 16%) more peak memory than frozen Reno on this path in four of five runs, including both blocks that were never disturbed; the A/A arm never did. The one check that reads *not reproduced* ran differently built programs (about 2 MiB larger to begin with) and compared retained memory in a later part of the run than where the plain builds differ. Ending the block on that check would be a rule made after seeing the result, and the second revision running whose preservation flag ends by an operator decision rather than by a rule fixed before data. About an hour of host time on the plain programs either supports ending it on a rule fixed beforehand or confirms the reading, and shows whether it appeared after the port.

**For A instead:** the registered check for this cell does read *not reproduced*; no option changes Reno's code on `main`, so holding BBRv3 back protects no default user today; and L6 starts within the hour rather than about three hours later, at best. **Against B:** with four of five readiness runs high, the re-measurement is more likely to repeat the raise than not; you would then face the same choice with a firmer reading.

**What advancement means** (as in slice 7): the revision is eligible for qualification (L6, then Q2). It is not a qualification, an adoption decision, a release or a default-controller change; BBRv3 stays experimental and opt-in, and Reno stays the default. No exception pre-clears any L6 measurement. Scope stays as measured: readiness and L6 do not exercise #742's and #743's send paths (the fixture never GSO-batches; the side stage measured #743 on loopback); the run-loop wake count, macOS and Windows stay unmeasured; the loopback STREAM queue bound stays untested.

### Disposition review

One `ras consider` of this section (`20261019T093631-c07a79d7c71f86724b446f85`; six reviewers, 40 findings, synthesis by codex-astra; one adjudicator, agy-argon, failed in round 2). Every consolidated fix was checked against the JSON and applied; no replacement review, as registered.

| Fix | Disposition | Change |
| --- | --- | --- |
| F-01 (high): A's waivers for nine unattributed exceptions | Fixed | A and B name the three departures, fresh for `d94b3ec2`; the remedy-before-exception rule, slice 7's D2 and the full glossary entry are quoted. |
| F-02 (high): contaminated originals used for A | Fixed | 1.009 is context only; the contaminated runs were this arm's own; blocks 4 and 5 cross in both summaries. The Answer is corrected too. |
| F-03 (medium): the `presrss` mapping called "within D3" | Fixed | Described as a new, result-informed interpretation; r8 as analogy, not authority; the two outcome rules distinguished. |
| F-04 (medium): B promised to close the rule gap and to start L6 | Fixed | B gathers plain-build evidence; L6 only after an advancing disposition; the outcome to recommend is stated before data. |
| F-05 (medium): "moved by a similar amount" | Fixed | The absolute changes since slice 6 and today's excesses are stated separately. |
| F-06 (medium): limits of the `presrss` evidence | Fixed | Both thresholds, the build difference, the measured-window retention and the warm-up heap figure (descriptive) are stated. |
| F-07 (medium): condition 4 marked reviewed | Fixed | #747's Mac review and the macOS D2 exception are cited. |
| F-08 (medium): loopback STREAM control p95 called small | Fixed | Ratio, history and the absolute p95 are given in the table and the recommendation. |
| F-09 (low): `cellhistory.py` key | Fixed | `excess_median_mib` and `excess_max_mib`; `cell-history.json` regenerated (byte-identical on a second run); `raised-cells.json` named. |
| F-10 (low): L6 duration and scope | Fixed | 4 h 20 min, up to about 6 h; scope reminder; tracking the default-Reno reading offered as optional. |

After these fixes the agent changed its recommendation from A to B: with the original-blocks reading removed from A's case and the `presrss` caveats stated, A rests on a result-informed rule against a clean, repeated reading.

### Operator decision

To be recorded.

## Discharges

To be added after L6.
