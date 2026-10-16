# BBRv3 advancement decision on the production port

Status: decision set for slice 7 of [Port the corrected BBRv3 sender (r9) into main's experimental opt-in sender](https://github.com/the-sarge/quic-go-fast/issues/748), under its normative [port contract](https://github.com/the-sarge/quic-go-fast/blob/abfa5456/docs/agents/bbr-production-port.md#after-pr-5-readiness-and-advancement-disposition), step 2 of "After PR 5: readiness and advancement disposition". The agent recommended keeping the raised preservation flag blocking and filing one re-measurement. The operator chose instead to advance the port by explicit decision, ruling that flag out for this revision. The operator confirmed that choice on 2026-10-16 after being shown that it overrides two standing preservation rules (D4). Planning only: no code change, qualification, release, default-controller change, paid resource or ledger change.

Branch `codex/bbr-port-advancement-decision`, based on `9b38f0ab`, the [port readiness record](../2026-10-16-bbr-port-readiness/README.md) (slice 6), whose tree's code is `main` at `abfa5456` plus records. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666) record, this record stays off `main`; a journal entry on `main` points to it. The candidate is **`main` at `abfa5456`**: slices 1–5 of the port plus [#752](https://github.com/the-sarge/quic-go-fast/issues/752)'s discovery-scratch release (declared deviation 4), with `main`'s other changes since the frozen component `e4f322cb`.

## Question

From the port contract, step 2: present each raised cell of the ported revision's readiness for a fresh operator decision under the unchanged criteria. A raised preservation flag blocks advancement, and no qualification (L6 or other) runs before this decision. Does the port advance to further qualification on Linux, and on what terms?

## Normative constraints (quoted)

[Port contract](https://github.com/the-sarge/quic-go-fast/blob/abfa5456/docs/agents/bbr-production-port.md), accepted outcome: "The ported revision is a **new revision**. No readiness exception granted to r9 (D2, D3) covers it. After the last code PR, its readiness is measured on `minimax` following #739's readiness stage, and its raised cells need an explicit operator advancement disposition before any qualification (L6 or other) runs on it." Step 2: "**Advancement disposition:** a decision record presenting each raised cell for a fresh operator decision under the unchanged criteria. A raised preservation flag blocks advancement. No qualification runs before this decision." Invariant: "**Default Reno:** unselected connections are unchanged. Every changed production line is reachable only for a BBR-selected connection, except the declared merge seams, which keep Reno's values (Reno's queue stays eight entries; Reno never creates a pacing kick)."

[Acceptance plan](https://github.com/the-sarge/quic-go-fast/blob/a4ecfc8de39eaa673755bf864fb7fdefbc109e80/docs/audits/2026-09-23-bbrv3-acceptance-plan.md), on the four limits: "These are review flags, not automatic rejection rules. Show absolute values and run variability alongside ratios." Also: "Correctness and protocol compliance are hard gates." "The owner judges adoption after reviewing the data; neither a minimum improvement nor a statistical-significance threshold is an automatic performance gate."

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1, the advancement condition: "1. the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload); 2. every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell (D2 sets the form); 3. no flag is raised and unresolved, and no preservation flag is raised".

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3: "D1's condition 3 is **unchanged**: a raised preservation flag blocks advancement. Instruction agreement and A/A overlap are recorded as **noise-compatible or inconclusive**, never as noise attribution, and they never clear or except a preservation flag." And: "Any later proposal to let a preservation flag be covered by an exception would be a separate, explicitly identified operator decision. It would need a demonstrated cause, and it could never cover protocol, payload, ownership, ECN/loss or other correctness contracts."

[r8 decision](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d3--the-reno-on-candidate-loopback-latency-cell-resolved-for-r8-on-linux-by-accepted-operator-interpretation), D3, prospectively: "Any new revision re-measures the cell in readiness. A raised value there runs `preslat` again, and only its **not reproduced** outcome ends blocking. It is not an exception and covers no other cell."

[r9 advancement decision](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md) (#747), D4: "**The revision whose adoption is in question.** Any new revision re-measures. If the production port (D5) lands first, the port effort measures its readiness and obtains an explicit advancement disposition from the operator before qualification runs on it; r9's amendment and exceptions do not transfer."

Glossary (`CONTEXT.md`), **Readiness exception**: "An explicit operator decision that one raised readiness flag does not block an advancement decision, for one candidate revision, platform and cell, covering the recorded evidence it names at no more than a recorded magnitude; the flag stays raised in every report, and the exception neither carries over to another revision or platform nor pre-clears later measurements. It is *attributed* when it rests on a registered cause attribution and *unattributed* when the operator grants it without a completed attribution or remedy test; neither kind changes the readiness limit."

## Evidence base

All figures come from the [port readiness record](../2026-10-16-bbr-port-readiness/README.md) at `9b38f0ab`: 110 readiness observations of `abfa5456` against frozen Reno `e4f322cb` on `minimax`, all clean, plus 8 `s5timeline` and 12 `preslat` observations. Ratios are medians of five per-block ratios against the same block's frozen Reno, with the number of blocks crossing the limit in parentheses. [raised_cells.py](raised_cells.py) reads that record's `summary.json`, `attribution.json` and `r9-context.json` and writes [raised-cells.json](raised-cells.json). Every decisive figure below was checked against `summary.json`, and every one matches the readiness README. Memory excesses in MiB are medians (maxima) of per-block differences, arm minus the same block's frozen Reno, as #747's `memory_excess.py` computed them for r9; latency differences are differences of arm medians. Both are descriptive and change no flag. r9's values come from #739 (`138223ac`) and #747, runs made on a different day; they are context, not a baseline.

### The unchanged advancement condition on the port (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 14.28× [12.32–15.61] STREAM, 14.42× [12.21–16.26] DATAGRAM, 5/5 pairs each (r9: 14.53×, 14.74×) | Yes |
| 2. Every flag passes, or is attributed and excepted | Nine raised candidate cells, none with a cause attribution registered on this revision, none excepted | No |
| 3. No unresolved flag, no preservation flag | Nine unresolved cells; one Reno-on-candidate preservation flag raised | No (both clauses) |
| 4. Mac records reviewed, platforms named | Linux only; the port's macOS and Windows readiness is unmeasured; #747's Mac review is unchanged | Reviewed |

Every goodput and CPU cell passes on every path. #738's S5 DATAGRAM sender CPU per useful GiB is 0.948 of frozen Reno's (r9: 0.953). #734's wake increase, read through CPU per useful GiB as the operator chose, passes everywhere; the wake count was not measured.

### The ten raised cells

| Cell | Port ratio | Per-block range | A/A range | Frozen Reno → arm (medians) | Excess, median [max] | r9 (#739) |
| --- | --- | --- | --- | --- | --- | --- |
| **Preservation:** Reno on candidate, loopback DATAGRAM control p95 | **1.504 (3)** | 0.58–2.49 | 0.69–5.51 | 0.422 → 0.364 ms | −0.058 ms | 0.744 (2), not raised |
| Loopback STREAM control p95 | 1.656 (4) | 0.86–1.80 | 0.43–1.05 | 0.145 → 0.213 ms | +0.069 ms | 1.485 (4) |
| S5 STREAM sender RSS | 1.217 (5) | 1.20–1.24 | 0.99–1.24 | 18.80 → 22.73 MiB | 4.11 [4.44] MiB | 1.235; 4.17 [4.58] |
| S5 STREAM receiver RSS | 1.482 (5) | 1.45–1.79 | 0.96–1.10 | 17.88 → 26.50 MiB | 8.62 [13.74] MiB | 1.518; 8.77 [9.44] |
| S5 DATAGRAM sender RSS | 1.157 (5) | 1.14–1.19 | 0.97–1.13 | 18.63 → 21.70 MiB | 2.93 [3.51] MiB | 1.159; 2.86 [3.65] |
| S6 STREAM sender RSS | 1.656 (5) | 1.33–1.74 | 0.95–1.04 | 13.88 → 22.98 MiB | 9.11 [10.11] MiB | 1.673; 8.92 [9.95] |
| S6 STREAM receiver RSS | 1.832 (5) | 1.51–1.90 | 0.98–1.02 | 14.63 → 26.89 MiB | 12.18 [12.84] MiB | 1.898; 12.28 [13.36] |
| S6 DATAGRAM sender RSS | 1.466 (5) | 1.39–1.61 | 0.99–1.01 | 14.63 → 21.75 MiB | 6.90 [8.97] MiB | 1.542; 7.46 [7.88] |
| S6 STREAM control p95 | 1.581 (5) | 1.46–1.62 | 0.93–1.00 | 125.5 → 199.3 ms | +73.7 ms | 1.562; +71.7 ms |
| S6 DATAGRAM control p95 | 1.461 (5) | 1.30–1.51 | 0.97–1.11 | 126.6 → 184.6 ms | +57.9 ms | 1.459; +61.5 ms |

The two loopback receiver RSS cells r9 raised are not raised here: 1.095 (STREAM) and 1.098 (DATAGRAM), with two of five blocks crossing in each. That is the rule's outcome on different runs, not a measured improvement, and they need no disposition.

### The nine candidate cells against r9's accepted costs (descriptive)

- **Memory.** The largest median excess is 12.18 MiB at the S6 STREAM receiver (r9: 12.28 MiB). Four of the six memory cells have a smaller ratio and median excess than r9's accepted magnitude. Two have a slightly larger median excess at a smaller ratio: S5 DATAGRAM sender (2.93 against 2.86 MiB) and S6 STREAM sender (9.11 against 8.92 MiB); frozen Reno's own peaks were higher on this day (18.63 against 17.88 MiB, and 13.88 against 13.25 MiB). One block stands out: an S5 STREAM receiver block at 31.12 MiB against frozen Reno's 17.38 MiB (+13.74 MiB, per-block ratio 1.79). That is the largest single-block excess in either record's readiness (r9's was 13.36 MiB, at the S6 STREAM receiver). The S6 DATAGRAM sender's largest block (+8.97 MiB) also exceeds r9's (+7.88 MiB). At the S5 STREAM sender, one A/A block (1.24) is above the cell's median; A/A is context and clears nothing.
- **S6 control p95.** STREAM is slightly above r9's accepted magnitude (1.581, +73.7 ms, against 1.562, +71.7 ms). DATAGRAM's ratio is about equal (1.461 against 1.459) at a smaller absolute rise (+57.9 against +61.5 ms). #740 attributed r9's S6 cells to the selected draft-06 ProbeBW Up policy. `internal/congestion` on `abfa5456` is byte-identical to r9's (`git diff 81e9dc8c abfa5456 -- internal/congestion` is empty), so that policy is the same code, but no attribution stage ran on this revision.
- **Loopback STREAM control p95.** Readiness reads 1.656, a rise of 0.069 ms (0.145 → 0.213 ms). r9's readiness read 1.485 (0.134 → 0.199 ms), and #747 accepted 1.849 (+0.118 ms) at #740's adequate sample. No adequate-sample stage ran on this revision. #744's finding stands as context: restoring depth 8 would not clear the cell (1.345 against frozen Reno), and the bytes- or time-ahead queue bound is an untested lead that the port contract did not implement.

### The preservation cell in detail

- **What it measures.** The Reno-on-candidate arm runs `abfa5456`'s fixture with Reno, the default. Against frozen Reno, it measures `main`'s Reno, which bundles the port's Reno-reachable merge seams, every non-BBR change `main` carries since `e4f322cb`, and code placement. No arm measured `main` without the port, so the port's share is not examined.
- **The reading.** 1.504, three of five blocks crossing; per-block ratios 0.58–2.49. Frozen Reno's per-block p95 ranged 0.170–0.630 ms, and the arm's 0.223–1.052 ms. The arm's median p95 (0.364 ms) is below frozen Reno's (0.422 ms).
- **The sample.** Loopback runs carry 20 control replies each, and the readiness p95 is the reply at sorted index ⌈0.95·n⌉−1 (`run.py`), which for 20 replies is the second slowest.
- **The registered follow-up.** `preslat` (six clean blocks, 120 s windows, at least 100 replies per run) is *inconclusive*: pooled p95 0.475 ms against 0.520 ms, ratio 0.913, with a 95% block-bootstrap interval of 0.611–1.494 whose upper end is above 1.20; per block 0.49–1.84. Under the r8 decision's D3 mapping, only *not reproduced* would end blocking, so no measurement-based route ends it today.
- **The cell's history** on `minimax` (each record's `summary.json`):

| Record | Frozen-Reno A/A, loopback DATAGRAM control p95 | Reno on candidate |
| --- | --- | --- |
| #715 (`36f7ce01`) | 1.615 (3), raised | 0.721 (1) |
| #736, r8 (`3bf2245e`) | 0.973 (2) | 2.588 (3), raised; `preslat` *not reproduced* 0.716 [0.487–1.180] |
| #739, r9 (`138223ac`) | 1.599 (3), raised | 0.744 (2) |
| This port (`9b38f0ab`) | 1.519 (3), raised | 1.504 (3), raised; `preslat` *inconclusive* |

- **Descriptive comparator.** Candidate ÷ Reno on candidate on this cell is 0.837 (0.28–1.64) over five blocks; it attributes nothing.
- **Code-level preservation.** The contract's default-Reno invariant is enforced by construction-time gating and the merge seams' recorded tests (PR 5's closure table: Reno's queue stays eight entries with its authority rebound). This decision neither changes nor relies on a new claim about it.

## Decisions

### D1 — Verdict under the unchanged condition: not advanced

Under the **unchanged** condition, the port (`abfa5456`) is **not advanced**. Condition 2 fails: nine raised candidate cells, none attributed on this revision or excepted. Condition 3 fails twice: nine cells are unresolved, and a preservation flag is raised, which on its own blocks advancement under the contract and the Linux decision's D3. That verdict stands and is not rewritten.

### D2 — Advance the port on Linux, by an explicit operator amendment

The operator then decides, explicitly and after reviewing the data, to **amend the advancement condition for `abfa5456` on Linux only**:
- condition 2's cause-attribution requirement is waived for the nine candidate cells in D3, and so is the remedy-before-exception rule for the loopback STREAM control p95 cell;
- condition 3's unresolved-flag clause is amended for those nine cells;
- condition 3's preservation clause, the contract's "A raised preservation flag blocks advancement", and the Linux decision's D3 rules quoted above are set aside for **one cell**, the Reno-on-candidate loopback DATAGRAM control p95, as D4 states. So is the r8 decision's D3 mapping, under which this cell's *inconclusive* outcome would keep blocking.

**What stays.**
- All ten cells remain **raised** in every report, with their values, labels and `preslat` outcome. Nothing is attributed by this decision.
- Every readiness limit, every correctness gate and the contract's default-Reno invariant are unchanged.
- The standing rules are unchanged for every other cell, revision and platform: a raised preservation flag blocks, A/A overlap never clears or excepts one, a preservation exception needs a demonstrated cause, and only an adequate *not reproduced* ends a raised Reno-on-candidate latency cell (r8 D3).

With D3's and D4's exceptions, every raised cell on Linux is passing or excepted. So the port is **advanced: `main` at `abfa5456` deserves further qualification, on Linux**.

**What advancement means.** The port is eligible for qualification. It is not a qualification, an adoption decision, a release, a README or CHANGELOG change, or a default-controller change. BBRv3 stays experimental, opt-in and unqualified on `main`, and Reno stays the default. It is an operator judgment about costs and one noisy reading, not a finding that they were explained.

**The operator weighed:**
- **The benefit.** 14.28× and 14.42× Reno's goodput on S6, 5/5 pairs each; every goodput and CPU cell passes; fifty Linux pairs above Reno across five revisions (#747's forty, plus this record's ten).
- **Sameness with r9.** The nine candidate cells are the costs the operator accepted for r9, at about r9's magnitudes, on byte-identical BBR packages.
- **The preservation reading's character.** It is sub-millisecond, it rests on the second-slowest of 20 replies per run, the arm's median p95 is below frozen Reno's, frozen Reno against itself raised the same cell in three of the four readiness records on `minimax`, r8's raised value on this cell did not reproduce, and the adequate-sample stage's point estimate is below 1.
- **The rules overridden.** The agent's recommendation stayed within them. The operator was shown both Linux-decision rules, and confirmed the choice.

**Is this a weakened gate?** The map rules out "weakening calibration/correctness gates". The preservation check holds `main`'s Reno to the four readiness limits, which the acceptance plan calls "review flags, not automatic rejection rules"; it is not a protocol, payload, ownership, ECN/loss or other correctness contract, which the Linux decision's D3 says no exception could cover. Still, it is the gate that protects default users, the Linux decision's D3 tightened it on purpose, and this is the first preservation flag set aside by judgment rather than ended by measurement. That is recorded here as an override, not a reinterpretation.

**Classification:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | None new. |
| Untested remedies | The bytes- or time-ahead queue bound for loopback STREAM control p95: untested, not disposed of. |
| Missing evidence | The causes of the six memory cells and the loopback STREAM control p95 on this revision; an attribution of the S6 control p95 on this revision; the port's share of the Reno-on-candidate reading; an adequate-sample answer for that reading. |
| Attributed behaviour | None on this revision. r9's Up-policy attribution of S6 control p95 is context (identical code). |
| Demonstrated contract incompatibility | None. |
| Algorithm suitability | No finding against BBRv3. |

### D3 — Unattributed readiness exceptions: the nine candidate cells

Granted for `abfa5456` on Linux only, covering slice 6's readiness evidence. Each cell stays **raised** in every report. None is attributed: no cause attribution was registered on this revision, including for S6 control p95, where r9's attribution is context only.

| Cell | Accepted magnitude |
| --- | --- |
| S5 STREAM sender RSS | 1.217; median excess 4.11 MiB |
| S5 STREAM receiver RSS | 1.482; median excess 8.62 MiB (largest block +13.74 MiB, disclosed above) |
| S5 DATAGRAM sender RSS | 1.157; median excess 2.93 MiB |
| S6 STREAM sender RSS | 1.656; median excess 9.11 MiB |
| S6 STREAM receiver RSS | 1.832; median excess 12.18 MiB |
| S6 DATAGRAM sender RSS | 1.466; median excess 6.90 MiB |
| S6 STREAM control p95 | 1.581; +73.7 ms (125.5 → 199.3 ms) |
| S6 DATAGRAM control p95 | 1.461; +57.9 ms (126.6 → 184.6 ms) |
| Loopback STREAM control p95 | 1.656; +0.069 ms (0.145 → 0.213 ms), a readiness reading at 20 replies per run |

**Scope.** These dispose of the recorded evidence named above, for this advancement decision. They pre-accept nothing in later measurements: not qualification runs on `abfa5456`, not any later `main` commit, and not any other platform. No standing memory budget and no absolute latency tolerance are adopted.

**Memory.** The reasons #747 recorded for r9 apply on identical code: #740 labeled no cell after 124 observations, #746 was not run because even full attribution would clear no cell, and the per-connection worst case is bounded by configuration (code reading). The STREAM receivers' excess stays unexplained. Consumer affordability and connection counts stay adoption questions.

**Loopback STREAM control p95.** The remedy-before-exception rule (the intervention decision's D2) is waived for this cell on this revision. The bound class stays **untested, not disposed of**. The 20% limit is unchanged.

### D4 — The preservation cell: an unattributed readiness exception by operator ruling

**Cell:** Reno on candidate, loopback DATAGRAM control p95, `abfa5456`, Linux.

**Evidence covered:** slice 6's readiness reading, 1.504 with three of five blocks crossing (per-block maximum 2.49; arm medians 0.364 against 0.422 ms), and its `preslat` stage, *inconclusive* at 0.913 [0.611–1.494]. **Accepted magnitude:** 1.504 in readiness.

**What the ruling is.** The operator rules that this reading does not block the port's advancement on Linux. It is a readiness exception in the glossary's sense, unattributed, and the first one granted on a preservation cell. To grant it, the operator overrides, for this one cell on this revision and platform only:
1. condition 3's preservation clause and the port contract's "A raised preservation flag blocks advancement";
2. the Linux decision's D3 rule that instruction agreement and A/A overlap "never clear or except a preservation flag", because the A/A history is among the operator's reasons;
3. the same D3's requirement that a preservation exception "would need a demonstrated cause": no cause is demonstrated;
4. the r8 decision's D3 mapping, under which only *not reproduced* ends blocking.

**What it is not.**
- Not a finding that `main`'s Reno is unchanged, and not a noise attribution: the reading stays raised and *inconclusive*.
- Not an attribution to the port or to `main`'s other changes.
- Not a change to any correctness contract or to the contract's default-Reno invariant.
- Not a standing rule. A raised preservation flag in any later measurement, on this revision or any other, blocks under the unchanged rules, and the overrides above do not recur without a new explicit decision.

**The agent's recommendation**, kept on record: keep the cell blocking and file one re-measurement (see [Rejected](#rejected)).

### D5 — What advancement leads to: L6, named and not started

#747's D4 stands: the smallest qualification that would change an adoption decision is **L6**, coexistence with a QUIC Reno flow on a 100/100 Mbps, 100 ms bottleneck, with L8 conditional on concurrent BBR Transfers. It runs when the operator wants an adoption call. Its required evidence, binding on whichever effort runs it, is #747's, unchanged, with these additions for this revision:
- **The qualified revision.** This advancement covers `abfa5456`. A qualification run on a later `main` commit is a new revision: it first needs its own readiness and an explicit advancement disposition.
- **No pre-clearance.** No exception in D3 or D4 covers any qualification measurement. Readiness limits, preservation rules and correctness gates apply unchanged, and a raised preservation flag there blocks under the standing rules.
- **Carried open costs:** #734's wake count, never measured; #738's S5 DATAGRAM sender CPU (0.948, passing); the loopback receiver RSS cells at the edge (two of five blocks crossing each); macOS and Windows unmeasured.

### D6 — The port effort closes

All seven slices of #748 are done, so #748 closes with this record. No follow-up measurement is filed: the operator chose none. The OmniFocus task for slice 7 is completed.

## Rejected

- **Keep the preservation cell blocking and file one re-measurement** (the agent's recommendation). The re-measurement would have run this one cell with an adequate sample and a third arm, `abfa5456` with the port removed, so a slowdown could be traced to the port or to `main`'s other changes. Its registration would have had to show before data that it could return *not reproduced* when nothing changed, and it would have stopped instead of running if it could not. That stays within every standing rule. The operator chose the ruling instead; the design can still be filed later.
- **Hold, filing nothing.** Costs nothing, but leaves the question about `main`'s Reno unchecked and needs a fresh round for any later advancement.
- **A standing rule change**, for example that a 20-reply loopback p95 reading cannot block on its own, or that an *inconclusive* adequate-sample outcome below 1 counts as not reproduced. It would change every future revision's check, written after seeing this result.
- **Deciding the nine candidate cells later.** It would need a second decision round.
- **Standing allowances**: a per-connection memory budget (for example 15 MiB, one default connection receive window) or an absolute latency tolerance (for example 1 ms on paths whose Reno p95 is below 1 ms). The operator declined both for r9 too.
- **Further investigation of the memory or loopback-latency cells, or testing the queue bound now.** #740 and #746 showed attribution clears no memory cell, and the port contract made the bound a lead only.
- **Carrying r9's exceptions over.** The contract forbids it; every cell here is decided fresh.
- **Any qualification, paid run, release change or default-controller change.**

## Actions on acceptance

- Push this branch; it stays off `main`.
- A resolution comment on #748, slice 7 checked, and #748 closed.
- A journal entry on `main` pointing to this record.
- The OmniFocus slice 7 task completed.

## Limits

This is a planning decision. It claims no qualification, no readiness on another platform and no adoption. It authorizes no release, paid resource, default-controller change or ledger change. Its figures come from the readiness record at `9b38f0ab` and the records cited. Byte identity of `internal/congestion` with r9 and the default-Reno invariant's enforcement come from source inspection and recorded tests, not from a measurement in this record. The exceptions and the preservation ruling record the operator's judgment, not an explanation of the costs or the reading they cover.
