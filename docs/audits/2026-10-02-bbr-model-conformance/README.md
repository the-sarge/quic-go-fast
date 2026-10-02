# BBRv3 model conformance: the full candidate (C1–C4)

**Date:** October 2, 2026, America/New_York. **Scope:** [Conform the four undispositioned BBRv3 model differences to the selected draft](https://github.com/the-sarge/quic-go-fast/issues/707), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666), implementing [C4 of the BBRv3 correction choice](https://github.com/the-sarge/quic-go-fast/blob/724b033612fb6d87b898b689abd034147f611218/docs/audits/2026-10-02-bbr-correction-choice/README.md#c4--conform-the-four-undispositioned-model-differences) on the [C1–C3 service-correction layer](https://github.com/the-sarge/quic-go-fast/blob/d785f3b82a82380e7b36c87dd29cd6291ae418a3/docs/audits/2026-10-02-bbr-recovery-service/README.md).

This record builds the experimental full candidate and its conformance evidence. It does not measure endpoint performance, tune controller parameters, switch controller versions or merge production code.

## Answer

Yes. All four differences conform to the selected draft-06 behavior, and none needed a recorded QUIC translation to change:

1. **All-spurious undo restores the loss-driven phase.** A loss-driven Startup exit returns to Startup; a loss-driven Up exit restarts at Refill.
2. **Startup, Drain and ProbeBW decide on the pre-update RTT minimum.** `UpdateMinRTT` now runs after those decisions and immediately before `CheckProbeRTT`, with the saved ProbeRTT cap and expiry result unchanged.
3. **Packet rounds advance on delivered-at-send evidence regardless of rate validity.** Missing history and invalid clocks still supply no round.
4. **Startup loss learns unquantized capacity**, `max(BDP, latest delivered volume)`.

Each item has focused oracles that failed on the service layer and pass on the full candidate, plus unchanged-behavior controls that pass on both. Each item is its own commit, and each commit turns exactly its own oracles green. Every pre-existing test passes at every commit unchanged. A 14-mutation sweep is fully caught.

An independent review found one new divergence in the first item-3 build: a rejected-rate round start in ProbeBW_UP consumed the round without raising the probe slope. It is fixed, with its own oracle. The review found nothing blocking.

One recorded translation bounds item 1 rather than conflicting with it: an active CE cap keeps precedence over phase restoration, as the design requires. One design sentence about undo reads more narrowly than the draft. It is treated as silent on phase, and its wording is returned to the map for the production change; see [Recorded translations](#recorded-translations).

## Provenance

| Name | Revision |
| --- | --- |
| Frozen component | `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` |
| C1–C3 service-correction layer | `c6bb13b72e2d5de55af36543aa27c8cf06812a11` on `codex/bbr-recovery-service` |
| Behavior authority | [draft-ietf-ccwg-bbr at `6db913f5ec575a8852bea926bd6001884e579496`](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md) |
| Recorded translations | [`docs/designs/bbrv3.md`](https://github.com/the-sarge/quic-go-fast/blob/a4ecfc8de39eaa673755bf864fb7fdefbc109e80/docs/designs/bbrv3.md) and [controller plan](https://github.com/the-sarge/quic-go-fast/blob/a4ecfc8de39eaa673755bf864fb7fdefbc109e80/docs/adr/2026-09-24-bbrv3-controller-plan.md) at `a4ecfc8de39eaa673755bf864fb7fdefbc109e80` |
| Full candidate (C1–C4) | `codex/bbr-model-conformance`; commits below |

| Commit | Content |
| --- | --- |
| `f9dad393` | Failing oracles and controls ([`bbr_conformance_test.go`](../../../internal/congestion/bbr_conformance_test.go)) |
| `fd0b90b2` | Item 4 |
| `1710d64f` | Item 2 |
| `f3383912` | Item 3 |
| `558d1729` | Item 1 |
| `3bf81a98` | Review follow-up: failing Up-slope oracle and closed test gaps |
| `8a0e0962` | Item 3 fix: the Up slope follows the round; the full candidate's code |
| `8c033da2` | Rejected-rate oracle keeps a nonzero rate; the full candidate's tests |

The two layers stay separately buildable: `c6bb13b7` is the service-correction layer and `8a0e0962` is the full candidate's code (`8c033da2` changes only a test). Only `internal/congestion` changes between them, so the C1–C3 equivalence and work-bound evidence carries over unchanged. Default Reno never constructs this reducer.

## The four conformances

### 1. Undo restores the loss-driven phase

Draft `HandleSpuriousLossDetection` ([lines 3368–3406](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md#L3368-L3406)) returns a flow to the probing state that loss ended. Its `undo_state` is set by `CheckStartupHighLoss` and by `HandleInflightTooHigh` in ProbeBW_UP, and cleared when a loss episode starts.

The undo record now carries that state. The Startup-loss exit and the loss-driven Up exit record it only while the record is valid, and a new episode's record starts without it. On all-spurious undo, after the existing numeric restoration:

- a recorded Startup exit returns to Startup, or, in ProbeRTT, marks the probe to return to Startup on exit (draft: `full_bw_reached = false` without leaving ProbeRTT);
- a recorded Up exit restarts the probe at Refill, except in ProbeRTT;
- an active CE cap blocks both. CE already owns a Startup exit, and no new Refill/Up is allowed under its caps.

Refill's round starts at the reducer's own delivered count, because an undo event can carry no ACK.

| Oracle | Before (C1–C3) | After |
| --- | --- | --- |
| Startup-loss exit, then all-spurious undo | Drain, 50,000 B/s | Startup, 277,258 B/s; stays in Startup on the next ACK |
| Up-loss exit, then all-spurious undo | Down, 90,000 B/s | Refill, 100,000 B/s; Up at the next round |
| Startup-loss exit, ProbeRTT, all-spurious undo, probe exit | Cruise | Stays in ProbeRTT through undo; exits to Startup |

Controls, unchanged on both layers: mixed (not all-spurious) undo from each exit; a plateau-driven Startup exit; undo from Cruise; a mixed-undo ProbeRTT exit; and CE active at undo, for both the Startup and the Refill restoration.

### 2. Pre-update RTT minimum for phase decisions

The draft's per-ACK order ([lines 1758–1774](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md#L1758-L1774)) runs `CheckStartupDone`, `CheckDrainDone` and `UpdateProbeBWCyclePhase` before `UpdateMinRTT`, then `CheckProbeRTT`. Control parameters follow, so the same ACK's window and pacing use the new minimum.

The reducer captured the old ProbeRTT cap, updated both RTT filters, then decided phases. It now captures the cap first as before, makes the phase decisions on the old minimum, then updates the filters and passes the identical expiry result to the ProbeRTT check. An ACK that returns before the phase decisions (no ACK, or a stale delivered count) still updates the filters, exactly as before.

| Oracle | Before | After |
| --- | --- | --- |
| Drain, 100 kB/s, 100 ms minimum; ACK with raw RTT 50 ms, flight 7,500 B | Stays in Drain: new target 5,000 B | Cruise on the same ACK: old target 10,000 B; minimum becomes 50 ms |
| ProbeBW Down after Up, same ACK | Stays in Down | Cruise: the old max-bandwidth BDP admits 7,500 B |
| Startup-loss exit at 1 MB/s with a 50 ms ACK after 100 ms | Learns 50,000 B | Learns 100,000 B |
| Drain, ten-second minimum expires and rises from 100 to 200 ms; flight 15,000 B | Exits Drain on the new 20,000 B target | Stays in Drain on the old 10,000 B target; minimum becomes 200 ms |

The expiring-minimum case uses an idle restart to keep the five-second ProbeRTT filter from entering ProbeRTT, a reachable path.

The unsampled path decides on the old minimum too: the Drain oracle with a rejected rate also reaches Cruise.

Controls: the same Drain, Down and Startup-loss ACKs with an unchanged RTT; a stale-delivered ACK that returns early still updates the minimum. Three same-ACK ProbeRTT entries cover a lower minimum, an expired minimum rising from 100 ms to 1 s, and a rejected rate. Each checks that entry still happens on that ACK and that the cap is the saved pre-update cap, lowered only by a smaller new target. The rising case would expose a cap saved after the update. The existing ProbeRTT tests, including the expired queued-sample cap, pass unchanged.

### 3. Packet rounds and rate validity

Draft `UpdateMaxBw` calls `UpdateRound` before its positive-rate test ([lines 2820–2918](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md#L2820-L2918)). The reducer returned through its unsampled path before advancing the round.

Round evidence is now the old anchor predicate without the rate terms: a valid clock, a present anchor with a valid delivered-at-send snapshot no greater than the event total, current path and sample generation, and not an MTU or path probe. When that evidence crosses the boundary but the rate is rejected (`Valid` false or a non-positive interval), the general round advances. So do the draft's round-start consumers:

- the Drain round count;
- the ProbeBW wait rounds;
- the ProbeBW ACK phases, including `AdvanceMaxBwFilter` at the end of a stopping round, which is gated on supply limitation as before;
- the ProbeBW_UP probe slope (`RaiseInflightLongtermSlope`), under its existing window-limited gate;
- Refill to Up, with a zero plateau baseline instead of an unmeasured rate;
- the ProbeRTT ACK-phase reset;
- window-usage rotation;
- the ACK-aggregation round slot.

The sampler still reports the anchor ordinal, interval and limitation for a rejected sample, withholding only the rate. A rejection therefore reaches this path in transport, for example for an interval shorter than the sampler's minimum RTT, which needs no loss.

| Oracle | Before | After |
| --- | --- | --- |
| Four rejected-rate ACKs in Drain, high flight | No rounds; stays in Drain | Four rounds; Down |
| One rejected-rate ACK at the Refill boundary, still reporting a nonzero rate beside `Valid` false | No round; stays in Refill | One round; Up, ACK phase Starting; plateau baseline zero, not the reported rate |
| Same, with a zero interval instead of `Valid` false | No round; stays in Refill | One round; Up |
| Rejected-rate ACK at the end of a stopping round | ACK phase stays Stopping; no filter-cycle advance | ACK phase Init; filter cycle advances |
| Rejected-rate round start in a window-limited Up | No round; slope unchanged | One round; slope raised; acknowledged growth not accumulated |

Controls: a valid rate advances identically; missing history (an invalid anchor snapshot), an absent anchor and an invalid clock advance nothing, in both Drain and Refill. A valid Up round raises the slope and accumulates growth; a rejected rate leaves the safe-flight bound raise alone. The existing rejected-rate and originless ProbeRTT round-gate tests pass unchanged.

The first item-3 build gated the slope raise with bound growth. A rejected boundary ACK then consumed the round and the slope raise was lost, where before it had only been delayed to the next valid ACK. [Independent review](#independent-review) reproduced this; `8a0e0962` fixes it.

### 4. Unquantized Startup-loss capacity

Draft `CheckStartupHighLoss` learns `max(BBR.bdp, BBR.inflight_latest)` ([lines 1922–1929](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md#L1922-L1929)). `BBR.bdp` is `bw * min_rtt` before quantization ([lines 3602–3606](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md#L3602-L3606)). The reducer learned the quantized `inflight(1)`, which includes the two-quantum and four-M floors. It now learns `max(bdp(1), latest volume)`; the quantized Drain and ACK output targets are unchanged.

Two offload quanta exceed the BDP only when the RTT is below about 5.5 ms at Startup gain, so the difference concentrates on low-RTT, high-rate paths such as loopback.

| Oracle | Before | After |
| --- | --- | --- |
| 100 MB/s, 1 ms: BDP 100,000 B, two quanta 131,072 B | Learns 131,072 B; Cruise headroom 111,412 B | Learns 100,000 B; Cruise headroom 85,000 B |
| 20 kB/s, 100 ms: BDP 2,000 B | Learns 4,800 B | Learns 2,000 B; the four-M floor still masks headroom at 4,800 B |

Controls: 1 MB/s at 100 ms, where the BDP dominates quantization, learns 100,000 B on both. A loss-round ACK delivering 6,000 B beyond its anchor learns the latest volume over a 2,000 B BDP on both.

The learned cap can now sit below two quanta, as in the first oracle. That matches the draft. The controller plan's two-quantum floor applies to the Drain and ACK window targets, which are unchanged, not to learned caps.

## Commit matrix and mutation sweep

[`commit-matrix.md`](commit-matrix.md) runs the final conformance test file against each code revision from the service layer to the full candidate, in clean `git archive` extracts. Every oracle fails on the service layer and turns green at exactly its own item's commit. The Up-slope oracle turns green only at the fix. Every control passes at every revision, and so do the package's pre-existing tests.

[`mutations.sh`](mutations.sh) applies 14 single-point mutations to a disposable extract of the full candidate. Each removes or reorders one conformance decision or one of its guards: every RTT-update site, the expiry result, the saved cap, each round-evidence term, the slope and bound gates, the CE and ProbeRTT restoration guards, and the learned-capacity terms. [All 14 are caught](mutations.txt). On the earlier tests, the review's mutations left six surviving and this sweep found a seventh (M14, a rejected sample's reported rate). The tests added in `3bf81a98` and `8c033da2` close each one.

## Recorded translations

No conformance changed a recorded translation:

- **Undo.** The design says undo "restores only the saved loss-derived bounds/cwnd according to draft maxima and resets the plateau detector; it does not rewind delivered counters, resend frames or remove CE limits." Read literally, "only" leaves out phase. Read in context, it is not a translation of phase:
  - The sentence gives no reason to differ from the draft, and its negative clause targets QUIC side effects: counters, frames and CE.
  - The controller plan frames undo as restoring proven loss-derived state, and a loss-driven exit is such state.
  - The design follows draft-06 unless it overrides it.
  - The [model-fidelity research](https://github.com/the-sarge/quic-go-fast/blob/ffaa4410c7e2b9982f31c9820ebdad0d4d42dece/docs/audits/2026-09-30-bbr-model-fidelity/README.md) and the [correction choice](https://github.com/the-sarge/quic-go-fast/blob/724b033612fb6d87b898b689abd034147f611218/docs/audits/2026-10-02-bbr-correction-choice/README.md) both treated phase restoration as undispositioned.

  The independent review reached the same reading. Restoration follows the draft, and the clause about CE limits is preserved. The design is frozen campaign history, so this record does not amend it. A production change that adopts item 1 should amend that sentence to name phase restoration; the question is returned to the map.
- **CE.** The design says a CE event exits Startup and that no new Refill/Up is allowed while CE caps are active. Phase restoration therefore does not run under an active CE cap. A CE response in the same event as an undo still applies after restoration, because the reducer composes CE after recovery.
- **ProbeRTT.** The saved pre-update cap, the five-second scheduling filter, the ten-second model filter and `CheckProbeRTT` after `UpdateMinRTT` are unchanged.
- **Sampling.** The design rejects an invalid chosen snapshot's rate and gives missing history no invented evidence. Item 3 uses only a valid snapshot's delivered-at-send value and invents no rate.
- **Quantized outputs.** The plan's accepted quantized Drain and ACK targets are unchanged; item 4 changes only learned loss capacity.

## Residual differences

Item 3 is scoped to general packet rounds. On a rejected-rate ACK these remain sample-owned, as before:

- loss-round boundaries and their latest-delivery signals (draft `UpdateLatestDeliverySignals` and `UpdateCongestionSignals`, [lines 3278–3313](https://github.com/ietf-wg-ccwg/draft-cardwell-ccwg-bbr/blob/6db913f5ec575a8852bea926bd6001884e579496/draft-ietf-ccwg-bbr.md#L3278-L3313));
- Startup and Up plateau evaluation, which compares the sample's rate (draft `CheckFullBWReached`);
- long-term bound growth from safe delivery (draft `AdaptLongTermModel`'s raise and `ProbeInflightLongtermUpward`).

Their effect is unmeasured. They are disclosed here, not dispositioned, because the selected correction scoped item 3 to general packet rounds. On a rejected-rate round start the draft would count a Startup plateau round at a zero rate, and in a window-limited Up it would reset the plateau baseline to that zero rate. The reducer does neither; the second predates this change.

Other differences the review identified, none introduced by C4 except where noted:

- **Undo timing.** The draft applies spurious-loss undo before the ACK's model update. The reducer applies it after, in the event's tail, so a same-ACK Startup-loss exit is taken and then reversed. The end state is the same phase.
- **Same-event CE and undo (introduced by item 1).** The CE guard reads the CE state before the event. If CE and an all-spurious undo arrive together, an Up undo enters Refill, which resets the short-term bounds to unlimited, and CE then aborts to Down. Had CE arrived one ACK earlier, the short-term bounds would have kept the restored maxima. Output stays under the CE caps, so this is not a safety issue.
- **Same-ACK CE and Startup-loss exit.** The loss exit wins, so CE does not mark the plateau. If the CE caps are later released and the episode then proves all-spurious, the flow returns to Startup despite the earlier CE. This needs CE release before undo, a narrow sequence.
- **Bandwidth timing.** The draft's `BBR.bdp` uses bandwidth as bounded at the end of the previous ACK. The reducer's phase decisions and the learned Startup-loss capacity use this ACK's updated maximum. Item 2 moves only the RTT side.
- **Snapshot validity.** Round evidence still requires a valid anchor snapshot, which also excludes packets registered before the current generation had a send origin. The draft's `P.delivered` has no such condition.
- **Filter cycle outside ProbeBW.** If a Stopping ACK phase survives ProbeRTT and a return to Startup, the reducer can advance the filter cycle in Drain. The draft advances it only in ProbeBW. Item 3 makes this less likely.

## Independent review

A read-only adversarial review checked the diff against the draft, the recorded translations and the reducer's call paths, and mutated a disposable extract. It found no blocking defect:

- **Item 1:** the undo phase is recorded only while an undo is possible, and always against the right episode, because recovery opens a new episode for any loss after the previous one ends.
- **Item 2:** the RTT filters still update exactly once on every path that updated them before, the expiry result is identical, and the old ProbeRTT cap is still saved before any filter update.
- **Item 3:** the round-evidence predicate is the old anchor predicate minus the rate terms. Every newly advanced round consumer conforms to the draft except the probe slope.

Its findings and their dispositions:

- **New divergence, the Up probe slope.** Fixed in `8a0e0962`, with a failing-first oracle.
- **Six mutations that survived the whole suite.** Closed in `3bf81a98`. The record's own sweep found a seventh, closed in `8c033da2`. All 14 are now caught.
- **Two controls weaker than their names.** The same-ACK ProbeRTT control now includes a rising minimum that exposes a post-update cap save. The CE Refill control stays as an unchanged-behavior control: Refill entry already refuses under CE, and the Startup CE control tests the restoration guard.
- **A comment left on the wrong function.** Fixed.
- **Same-event CE and undo ordering, low-RTT learned caps below two quanta, and the residual differences above.** Disclosed.
- **The undo sentence in the design.** Read as silent on phase, matching this record; returned to the map, as described above.

## Gates

[Gate log](gates.log), Go 1.27.1 darwin/arm64, at `8c033da2`:

- `internal/congestion` and `internal/ackhandler`, including the long histories, with and without `-race`
- the `bbrworkcount` counting build
- the full root package: 648 tests pass and 7 skip for platform capabilities (GSO and related)
- root BBR, emission, credit and pacing tests with `-race`
- `go vet`, including the counting build
- `golangci-lint` on both packages and the counting build
- `go mod tidy -diff`

## Limits

This is reducer-level evidence over constructed event sequences, not endpoint measurement. Whether and how often each item engages on a real path, and its performance effect, belong to [Demonstrate the BBRv3 correction in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/671). Items 2, 3 and 4 can engage without loss: item 2 on any falling RTT minimum, item 3 on any rejected rate interval, and item 4 on any low-RTT Startup loss. Item 1 needs an all-spurious loss episode. A shortfall attributable to C4 fails the applicable readiness criterion and triggers diagnosis; it neither rejects BBRv3 nor authorizes tuning away conformance.

## Reconstruction

At the full-candidate revision on `codex/bbr-model-conformance`:

```bash
go test -run 'TestBBRUndoRestoresLossDrivenPhase|TestBBRPhaseDecisionsUsePreUpdateMinRTT|TestBBRPacketRoundsIgnoreRateValidity|TestBBRStartupLossLearnsUnquantizedCapacity' -v ./internal/congestion/
```

For the commit matrix, extract each listed code revision with `git archive`, copy in the final `bbr_conformance_test.go` and run the same command with `GOWORK=off`. For the mutation sweep, run [`mutations.sh`](mutations.sh) from a disposable extract of the full candidate. No paid resource, campaign resumption, default-controller change or ledger change occurred.
