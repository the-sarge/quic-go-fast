# Readiness of the merged BBRv3 production port on Linux

**Date:** October 16, 2026, America/New_York. **Scope:** slice 6 of [Port the corrected BBRv3 sender (r9) into main's experimental opt-in sender](https://github.com/the-sarge/quic-go-fast/issues/748), under its normative [port contract](https://github.com/the-sarge/quic-go-fast/blob/abfa5456/docs/agents/bbr-production-port.md#after-pr-5-readiness-and-advancement-disposition), step 1 of "After PR 5: readiness and advancement disposition". Measurement only: no code change, qualification, advancement decision, default-controller change, release, paid resource, host-setting change or ledger change.

Branch `codex/bbr-port-readiness`, based on `abfa5456`, the candidate itself. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666) record, this record stays off `main`. The [#739 re-demonstration of r9](https://github.com/the-sarge/quic-go-fast/blob/138223ac/docs/audits/2026-10-08-bbr-r9-linux-redemonstration/README.md) (`138223ac`, on `codex/bbr-r9-linux-redemonstration`) supplies the readiness stage, the S5 timeline, the preservation stages, the fixture, the host layout and the aids that this record reruns.

**Status: registration.** Everything below was written, with the aids adapted and the builds made, before any prerequisite, smoke, readiness or attribution observation of this record. Results, deviations and assets will be added afterwards, and these sections will stay unchanged.

## Question

On owned Linux hardware, what are the readiness flags of the merged port head (`main` at `abfa5456`), what does the S5 timeline show, and does any raised Reno-on-candidate preservation cell reproduce under its registered stage? Beside each cell: what did r9 (`81e9dc8c`) record in #739?

## The candidate

**`main` at `abfa5456d99a9299825d04454a2e7ed12dff3326`**, chosen by the operator on October 16, 2026. It is the merged port: slices 1–5 ([#749](https://github.com/the-sarge/quic-go-fast/pull/749), [#754](https://github.com/the-sarge/quic-go-fast/pull/754), [#757](https://github.com/the-sarge/quic-go-fast/pull/757), [#763](https://github.com/the-sarge/quic-go-fast/pull/763), [#766](https://github.com/the-sarge/quic-go-fast/pull/766)), plus [#752](https://github.com/the-sarge/quic-go-fast/issues/752)'s discovery-scratch release ([#770](https://github.com/the-sarge/quic-go-fast/pull/770), `6a587c12`; declared deviation 4). Beyond the journal commit `3821a61c` named in the slice brief, it adds only [#772](https://github.com/the-sarge/quic-go-fast/pull/772) (when closing a socket the transport created fails, `Transport.Close` still waits for the receive loop) and [#774](https://github.com/the-sarge/quic-go-fast/pull/774) (an integration-test change). Later `main` commits are not measured.

What the candidate's code is, relative to r9 and to frozen Reno (`e4f322cb`), checked with `git diff` before writing:

- **BBR packages.** `internal/congestion` and `internal/ackhandler` differ from r9 only by slices 1–4's race-mode test budget (test files) and by deviation 4 (`internal/ackhandler/congestion_dispatch.go`, 4 lines added and 1 removed, plus its regression test).
- **Root package, against r9.** 20 non-test root files differ: the three declared merge seams (`connection.go`, `packet_emission.go`, `send_queue.go`), and 17 files that `main` changed outside r9's lineage (`candidate_authority.go`, `setup_reservation.go`, `receive_phase.go`, `packet_policy.go`, `frame_sorter.go`, `receive_stream.go`, `stream.go`, `streams_map.go`, `datagram_queue.go`, `transport.go`, `server.go`, `client.go`, `closed_conn.go`, `external_packet_io.go`, `mtu_discoverer.go`, `send_conn.go`, `send_conn_sendmsg_x_darwin.go`). Every root file `main` has not changed since `e4f322cb` equals r9's.
- **Dependencies.** `main`'s `go.mod` requires newer `golang.org/x` modules than frozen Reno's. The fixture module's `replace` points at the exported tree, so the candidate fixture links `x/crypto` v0.58.0, `x/net` v0.61.0 and `x/sys` v0.49.0, where frozen Reno's links v0.57.0, v0.59.0 and v0.48.0. Each build receipt records `go version -m`.

The port is a new revision. **No readiness exception granted to r9 (decisions D2 and D3 of the [r9 advancement decision](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md)) covers it.**

## Baselines

- **Readiness baseline: frozen Reno, `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`.** Every ratio is the arm's value over the same block's frozen Reno value. The frozen Reno fixture is rebuilt byte-identical to #739's (`cda8feb1…`), so this record's ratios use the same denominator binary as r9's.
- **A/A: frozen Reno run twice per block.** Its per-block ratios are reported beside every ratio and never override the rule (#712's reporting, unchanged).
- **r9's recorded values, as context only.** Beside every cell, this record reports #739's value for r9 on the same cell (median, raised flag, blocks crossing), its absolute medians and its S5 timeline. They are taken mechanically from `138223ac`'s `summary.json` and `attribution.json` by [r9context.py](r9context.py). r9's values come from different runs on a different day (October 8). The comparison is descriptive: no rule compares this record with r9, and a difference from r9 neither raises nor clears a cell.

## How a Reno-on-candidate difference is reported

The Reno-on-candidate arm runs the candidate fixture with Reno selected (the default). Against frozen Reno, it measures **`main`'s Reno at `abfa5456` against frozen Reno**. That difference bundles two things this stage cannot separate:

1. the port's Reno-reachable merge seams, which the contract requires to keep Reno's values (an eight-entry queue, no pacing kick); and
2. every non-BBR change `main` carries since `e4f322cb`: the 17 root files and the module versions listed above.

So:

- **Rule, unchanged.** Reno on candidate is held to the readiness limits against frozen Reno, as #712 registered it: goodput 0.95, CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20, with the median of five per-block ratios.
- **A raised Reno-on-candidate cell stays raised.** It is reported with its value, blocks crossing, A/A range and absolute medians, as a **raised preservation flag, not attributed to the port or to `main`'s other changes**. Under the contract, a raised preservation flag blocks advancement until slice 7's operator disposition. This record never labels such a cell noise, explained or excepted.
- **Conditional stages as registered.** `presrss` and `preslat` run exactly as #739 registered them (below). They test whether a cell reproduces, not what causes it. Their outcome is reported beside the readiness value, which stays as recorded. #739 applied the [D3 mapping](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d3--the-reno-on-candidate-loopback-latency-cell-resolved-for-r8-on-linux-by-accepted-operator-interpretation), under which an adequate *not reproduced* `preslat` outcome ended the latency cell's blocking state for the named revision. Whether that mapping applies to the ported revision is left to slice 7, so **this record ends no cell's blocking state**.
- **Descriptive split, only for a raised cell.** For each raised Reno-on-candidate cell, [r9context.py](r9context.py) also reports the median (minimum, maximum) over blocks of candidate ÷ Reno on candidate for the same measure. Both arms are ratios against the same block's frozen Reno, so frozen Reno cancels, and the figure compares BBR with Reno on the candidate's own root code. It uses the readiness observations only and sets no flag.
- No arm measures `main` without the port, so the port's share of a Reno-on-candidate difference is **not examined** here.

The candidate's own ratios (BBRv3 on the candidate against frozen Reno) are read the same way: they measure `main`'s BBR, on `main`'s root code, against frozen Reno, as #739's measured r9's BBR on r9's root code against frozen Reno.

## What stays fixed and what changes

- **Fixed (port contract, step 1):** #739's readiness stage, unchanged except for the candidate revision: the same arms, paths, durations, seeds, rotation, flag limits, median rule, A/A reporting, contamination rule (the operator's #714 rerun rule, as #715 adopted it, with the cap of ten readiness block reruns), quiet-host gate (#738's `wait_quiet`, before every counted block) and core layout. Also fixed: frozen Reno; the fixture with the causal-diagnosis heap patch; the relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter); the launcher; the endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz); `minimax`'s core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings; `GOTOOLCHAIN=go1.27.0` for every build (the toolchain is part of the fixed fixture; `main`'s CI pin, Go 1.27.2, is not used here); #715's timeline rule; and the preservation rules as #739 registered them.
- **Changed, all recorded:**
  - *Candidate:* `cand` is `abfa5456`, the merged port head.
  - *Builds:* [build.py](build.py) is #739's with five recorded changes (its docstring): the candidate; this record's artifact directory (`.local/bbr-port-readiness`); #739's expected hash for r9's `cand` fixture dropped; a byte-identity check of every copied aid against #739's record; and `go version -m` in each build receipt. It asserts that every binary rebuilt from a revision and overlays a prior record already built is **byte-identical** to it. Frozen Reno (`cda8feb1…`), `reno-diag` (`36752495…`), both relays (`033d467c…`, `24ee941d…`), the calibration aid (`4218b433…`) and the launcher (`a6146d62…`) match #739's binaries. The candidate variants are new: `cand` (`d3f05ad6…`), `cand-diag` (`f53f2714…`, for `presrss` only) and `cand-timeline` (`19cd9ff6…`, for `s5timeline` only). The `-diag` and timeline overlays apply to the candidate unchanged: `build.py` asserts that each patch site occurs exactly once. `connection.go` is a merge seam, but both diag sites there occur once, unchanged. `packet_emission_bbr.go`, `internal/congestion/bbr_sender.go` and `flow_controller_connection.go` equal r9's.
  - *Aids:* every aid except four is copied **byte-identical** from #739's record and checked by `build.py`, including #739's adapted [matrix.py](matrix.py), [stages.py](stages.py) and [follow.py](follow.py). Those three are kept verbatim, so their docstrings still name #739 and r9; the candidate is set only by `build.py`'s binaries. Adapted, with only paths and their header comments changed: [art.py](art.py) (artifact directory), [sync.sh](sync.sh) (artifact and remote directories) and [driver.sh](driver.sh) (remote directory and log path). `rules_test.py` (43 tests), `harness_test.py` (10), `receiver_test.py` (11) and `localize_test.py` (14) pass.
  - *New, descriptive only:* [r9context.py](r9context.py), with [r9context_test.py](r9context_test.py) (6 tests, passing), writes `r9-context.json` from this record's `summary.json` and `attribution.json` and r9's at `138223ac`. It runs after `stages.py all`, sets no flag and reads no observation.
- **Gates.** No transport source changes here.
  - The candidate's tree is #774's merge `70ec8bb5` plus one journal entry (`docs/DEV-JOURNAL.md` only). `70ec8bb5`'s tree equals #774's pull-request head `bbaa5738`, whose 33 hosted checks (unit, integration, lint and cross-compile) passed.
  - Slice 5's Linux kick test passed on `minimax` under `taskset -c 1-3`.
  - No gate is rerun.

## Prerequisites

#739's checks (which are #736's), through [stages.py](stages.py) `prereq`, on a quiet host, each run excluded from every statistic: ECN calibration ([prereq.py](prereq.py)), ECN engagement (`smoke`, `perfsmoke`, `ecnsmoke`), native I/O (`perfsmoke`: one send syscall and one transmit per measured-window forward packet), resources (threads inside their CPU sets, 8 MiB effective socket buffers, no UDP errors), instruments (`perf stat` at ≥ 95% running; `perf record` resolves Go frames) and the timeline overlay (`xsmoke`). The criteria are [#736's](https://github.com/the-sarge/quic-go-fast/blob/3bf2245e/docs/audits/2026-10-06-bbr-r8-linux-redemonstration/README.md#prerequisites). The smoke runs use the candidate. A failure ends this record as a reported prerequisite gap. `matrix.py hostfacts` records the governor, EPP, boost, idle states and other host settings read-only at the start and end of every stage.

## Registration: readiness

#712's readiness stage as #715, #736 and #739 ran it, unchanged except for the candidate revision:

- **Arms:** Reno, A/A Reno, candidate (BBRv3) and Reno on candidate, on loopback and S5; Reno, A/A and candidate on S6.
- **Runs:** five blocks per path and workload; seeds 9001–9005 (S5) and 9101–9105 (S6); 110 observations through #712's unchanged `run_case` ([matrix.py](matrix.py) stages `loopback`, `s5`, `s6`).
- **Flags:** #712's ([analyze.py](analyze.py), unchanged). Per path, workload and measure, the median of five per-block ratios against the same block's frozen Reno, against these limits: goodput 0.95 (loopback, S5), CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20. On S6, the useful benefit is repeatable when the candidate beats Reno in all five pairs.
- **Preservation check:** Reno on candidate is held to the same limits; see [How a Reno-on-candidate difference is reported](#how-a-reno-on-candidate-difference-is-reported).

**Contamination.** #712's test ([localize.py](localize.py) `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean, or above 0.50 in any one-second sample), with #715's handling:

- A block holding a contaminated or unusable observation (a block is all arms of one path, workload and block number) is rerun once with the same seed, under `observations-rerun`.
- The rerun replaces the original only if it is usable and uncontaminated ([rules.py](rules.py) `choose_block`).
- `summary.json` applies that rule. `summary-original.json` applies #712's rule to the original blocks, and any flag that differs between the two is reported.
- At most ten readiness blocks are rerun. Beyond that, the remaining contaminated blocks are reported under #712's rule.

**Reporting.**

- **Every cell is reported:** its median ratio, its blocks crossing, its A/A range, its raised flag, its absolute medians, and r9's value from #739 beside it.
- **Every raised cell stays raised, with absolute values.** Nothing is pre-cleared: r9's readiness exceptions (eight memory cells; loopback STREAM control p95; S6 control p95; the Up policy) do not carry over. No cell here is labelled as covered by one, and the record states no advancement verdict.
- **Carried open costs.**
  - [#738](https://github.com/the-sarge/quic-go-fast/issues/738)'s S5 DATAGRAM sender CPU rise is a readiness cell, S5 DATAGRAM sender CPU per useful GiB. It is reported beside r9's 0.953 (#739) and r8's 0.909 (#736).
  - [#734](https://github.com/the-sarge/quic-go-fast/issues/734)'s run-loop wake increase is reported through the cost its guard protects: sender and receiver CPU per useful GiB on every path. The operator chose this on October 16, 2026. The wake count itself is not a readiness measure under #739 and is **not measured** here.
  - The open memory and loopback-latency questions are readiness cells (peak RSS, control p95). They are measured here on the candidate, and no exception covers them.
- A CPU flag that falls but stays above its limit without a cause remains unresolved.
- These are Linux results. The candidate's macOS and Windows behaviour stays unmeasured, and supported-platform correctness remains required.

## Registration: S5 timeline and receiver split

**Stage `s5timeline`.** #739's stage, unchanged ([#739's registration](https://github.com/the-sarge/quic-go-fast/blob/138223ac/docs/audits/2026-10-08-bbr-r9-linux-redemonstration/README.md#registration-s5-timeline-and-receiver-split), which is #736's and #715's):

- **Question:** what bounds S5 delivery. **Hypothesis:** model behaviour. **Competitor:** sender timing.
- **Instrument:** #715's measurement-only [timeline overlay](timeline/), in `cand-timeline`.
- **Runs:** S5, both workloads, four blocks, seeds 9601–9604.
- **Rule:** [rules.py](rules.py) `timeline_decomposition`, `timeline_verdict`, `workload_verdict` and `stage_verdict`; *no deficit* under 0.02; the unusable tests; the three-of-four majority.
- **Escalation:** a workload with exactly two of four agreeing usable blocks gets blocks 5–6 (seeds 9605–9606), once.
- **Ending:** inconclusive if no verdict.
- **Descriptive figures:** lateness events and total lateness per run, phase shares, connection-goroutine states, and instrumented ÷ readiness goodput. r9's values from #739 are reported beside them.

The stage describes S5 whether or not an S5 goodput cell is raised.

**Receiver CPU split.** #715's split, unchanged and with no new runs ([rules.py](rules.py) `cpu_split`, `receiver_localization`), applied to the readiness observations. A raised S5 receiver CPU cell is classified as **inside window**, **outside window** or **mixed**; other cells are reported descriptively. This is localization, not cause. A raised S5 receiver CPU cell has no causal stage here, so it is reported as unresolved, with its split.

## Registration: preservation (conditional)

#739's stages and rules, unchanged ([#715's registration](https://github.com/the-sarge/quic-go-fast/blob/36f7ce01/docs/audits/2026-10-05-bbr-linux-redemonstration/README.md#stage-presrss-reno-on-candidate-rss-conditional-d3)):

- **`presrss`**, for each raised Reno-on-candidate RSS cell (at most two): six blocks of `reno-diag` and `cand-diag` with Reno at both endpoints and heap series; seeds 9801–9806 (S5) or 9901–9906 (S6); [rules.py](rules.py) `rss_preservation` (*retention excess*, *RSS excess without retained heap*, *not reproduced*, *inconclusive*, *evidence gap*).
- **`preslat`**, for a raised Reno-on-candidate loopback control p95 cell (at most one): six blocks of `reno` and `cand` with Reno, a 120 s window, at least 100 replies per run and 500 per arm; [rules.py](rules.py) `latency_preservation` (pooled p95 ratio with a 95% block-bootstrap interval: *regression reproduced*, *not reproduced*, *inconclusive*, *evidence gap*). The readiness value stays as recorded whatever the stage finds. As registered above, this record ends no blocking state; slice 7 decides whether D3's mapping applies to the ported revision.
- A raised Reno-on-candidate p95 cell on S5 or S6 has no stage and is reported as an evidence gap. A raised Reno-on-candidate CPU or goodput cell has no stage either. It is reported as an unattributed preservation flag that keeps blocking, beside r9's Reno-on-candidate value for the same cell from #739, as context.

**Cap.** At most **60** Linux attribution observations, reruns and escalations included:

| Stage | Runs | Observations (maximum) |
| --- | --- | --- |
| `s5timeline` | S5; `cand-timeline`; both workloads; 4 blocks, seeds 9601–9604 | 8 (12 with escalation) |
| Receiver CPU split | Readiness observations only | 0 |
| `presrss` | Conditional: per raised Reno-on-candidate RSS cell, 6 blocks × 2 arms | 12 per cell, at most 2 cells |
| `preslat` | Conditional: per raised Reno-on-candidate loopback latency cell, 6 blocks × 2 arms, 120 s window | 12, at most 1 cell |
| Total | | ≤ 48 plus reruns, ≤ 60 |

Contaminated or unusable attribution blocks follow the readiness rerun rule, and reruns count toward the cap. If the cap binds, stages run in the order listed, and any that cannot run end as **not run (cap)**. Attribution never sets a flag value.

## Stages not run

- **Memory attribution (`timeline`, `heapsites`), #740's memory instrument, and #741's or #744's stages.** The contract's step 1 names #739's readiness, `s5timeline` and conditional preservation stages only. Raised memory and control p95 cells are reported as **raised**, for slice 7.
- **`recvharness`, #715's loopback-bottleneck stage and any loopback hand-off stage.** #739 did not run them, and the contract does not list them. Raised loopback goodput or CPU cells, and a raised S5 receiver CPU cell, are reported as unresolved.
- **A run-loop wake count.** Not a readiness measure under #739 (above).
- **A `main`-without-port arm.** Not part of #739's stage. The port's share of any Reno-on-candidate difference is not examined.
- **macOS and Windows.** The candidate's behaviour there is unmeasured, not inferred.

## Disclosures made before data

- No observation of this record existed when this registration was written: no prerequisite, smoke, readiness or attribution run. The builds above were made before writing it; building produces no observation.
- **The candidate has no prior readiness figure.** During slice 5, `minimax` ran the Linux kick test under `taskset -c 1-3`. That was a correctness test, and it produced no goodput, CPU, RSS or latency figure.
- **r9's #739 values are known**, and they are the context named above. Nothing in this registration depends on them: the stages, limits and rules are #739's, unchanged. Known in advance: on r9, every goodput and CPU cell passed, no Reno-on-candidate cell was raised, and eleven cells were raised. Those were the eight S5 and S6 memory and control p95 cells; loopback receiver RSS (STREAM, DATAGRAM); and loopback STREAM control p95.
- **Interpretation known in advance.** Loopback STREAM goodput is sensitive to code placement at about 3% (#738, #744). Small RSS cells cannot be reconciled with `ru_maxrss` within a few hundred KiB (#740). The candidate's root code and module versions differ from r9's (above). The readiness rule applies regardless.

## Time estimate

All aids exist and every binary is built, so no instrument development is expected. On a quiet host (#739's figures):

| Step | Estimate |
| --- | --- |
| Prerequisites and smoke checks | about 15 minutes |
| Readiness (110 observations) | about 70 minutes |
| Quiet-host gating (one minute before each of 15 blocks) | at least 15 minutes |
| `s5timeline` | about 10 minutes |
| Conditional `preslat` | about 30 minutes, if triggered |
| Conditional `presrss` | about 15 minutes per cell, if triggered |
| **Total** | **about 1 h 50 min without conditional stages; up to about 2 h 50 min with all of them** |

Foreign load extends the gate's waits and adds same-seed reruns. Each readiness block rerun costs about 5–9 minutes.

## Order

**On the Mac:**

1. `build.py` (done) and the four aid tests plus `r9context_test.py` (done).
2. `sync.sh`.

**On `minimax`,** sequentially under `taskset -c 1-3`, with no build or test running there while a stage measures, and `matrix.py hostfacts` before and after each stage:

1. `prereq.py` (imported after `art`), then `smoke`, `perfsmoke`, `ecnsmoke` and `xsmoke`.
2. The readiness stages: `loopback`, `s5`, `s6`; then any readiness reruns.
3. `s5timeline`, and any escalation.
4. The conditional `presrss` and `preslat` cells, as the readiness flags require; then attribution reruns.
5. `stages.py fold`.

**Then, anywhere:** `stages.py all`, `r9context.py`, `rules_test.py`, `receiver_test.py` and `harness_test.py`.

**Unattended run.** By operator choice (October 16, 2026), the order runs unattended from 05:30 America/New_York on October 16, 2026 (09:30 UTC), started by a systemd user timer on `minimax` that runs [driver.sh](driver.sh). This is #739's arrangement, unchanged.

- **What the driver runs:** the prerequisites and smoke checks, the readiness stages, `s5timeline` and the conditional stages, in the order above.
- **What [follow.py](follow.py) decides:** it applies the registered rules that would otherwise need a person between stages. These are same-seed reruns of contaminated or unusable blocks (the #714 rule, capped at ten readiness blocks and at the attribution cap), and the conditional preservation stages that the readiness flags trigger.
- **Where decisions go:** every decision `follow.py` takes is written to `follow-decisions.json`.
- **Dry run:** `follow.py` is byte-identical to #739's, which was dry-run before #739's data and then ran #739's order unattended.
- **What stays with the operator's session:**
  - The prerequisite review. The driver continues past the smoke checks unless one fails outright, so a review failure afterwards still ends the record as a prerequisite gap, and any later observations are reported as excluded.
  - The `s5timeline` escalation, which depends on that stage's verdicts.
