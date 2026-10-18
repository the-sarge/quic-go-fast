# BBRv3 ECN ledger exhaustion under ordinary loss

This record answers [issue #793](https://github.com/the-sarge/quic-go-fast/issues/793): what disables ECN on the BBRv3 connection mid-run on a codepoint-preserving path, whether that is correct, and whether Reno would hit it. It is a diagnosis. It changes no runtime behavior; a remedy is a separate decision.

Inspected revision: `main` at `7a84a914`. The ECN path (`internal/ackhandler/bbr_ecn.go`, `ecn.go`, `congestion_dispatch.go`, both `bbr_diagnostics.go` files) is identical to `abfa5456`, the revision [#777](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md) and [#792](https://github.com/the-sarge/quic-go-fast/blob/89e71a60/docs/audits/2026-10-18-bbr-l6-diagnosis/README.md) measured; `sent_packet_handler.go` differs only by #786's timeout outcome, which does not touch ECN.

## Answer

- **The trigger is the BBR ECN validator's range budget.** Its marking ledger reaches `maxECNMarkRanges` (4,096 records), `failEvidence` sets `evidenceLost`, and marking falls back to Not-ECT for the rest of the connection. None of the tracker failure paths that #793 lists from `ecn.go` is involved: `EnableBBRECN` replaces that tracker on BBR connections.
- **Ordinary loss fills the budget.** A packet that is really lost is never acknowledged, so its record is never accounted for. Prefix compaction stops at the first such record, and every later loss event adds two records: the hole and the ACKed run after it. The budget therefore counts loss events over the connection's life, not packets in flight. It runs out after at most 2,048 loss events.
- **On L6 this took 3,131–3,995 drops**, at 16.8–200.7 s into the run, in all 31 BBRv3 runs that got that far. The one run that never switched was a 45 s development run with 1,628 drops in total.
- **It is a defect, in the design rather than the code.** The code does what the design and the [#612 investigation](../issue-612-ecn-ledger-lifetime/README.md) accepted. #612 kept the fallback after reproducing exhaustion only with alternating codepoints, and it recorded how often exhaustion happens in practice as unknown. Ordinary loss is enough, and BBRv3's CE response ([design D03](../../designs/bbrv3.md#decision-and-deviation-register)) stops working on any long connection that loses packets.
- **Reno would not hit it.** Default Reno uses the legacy validator, which keeps no per-packet ledger and ignores loss once the path is validated. Under the identical loss and acknowledgment pattern, 4,096 loss events (twice BBR's exhaustion point) leave it at ECT(0). The L6 Reno arms dropped only 324–835 packets per run, so they cannot settle this on their own; the code and the test below do.

## Mechanism

References are to `7a84a914`.

- [`sentPacket`](../../../internal/ackhandler/bbr_ecn.go) (`bbr_ecn.go:102`) appends one record per registered send through `appendRange` (`bbr_ecn.go:339`). A send merges into the last record only when `extendsRange` (`bbr_ecn.go:361`) finds the same codepoint, path generation and ACK status, a consecutive packet number and an affine ordinal. A fresh unACKed send after an ACKed record always starts a new record.
- [`feedback`](../../../internal/ackhandler/bbr_ecn.go) (`bbr_ecn.go:129`) splits records at ACK-range boundaries. The acknowledged parts become ACKed records; an unacknowledged part, such as a lost packet, stays as its own unACKed record.
- `compact` (`bbr_ecn.go:278`) removes only the leading run of ACKed records. A lost packet is never ACKed, so after the first loss nothing behind it is ever compacted.
- Recovery's loss declaration does not reach the ledger record. `lostPacket` (`bbr_ecn.go:311`) only updates testing state, and the sampler's tombstone expiry does not touch the ledger either (#612 explains why).
- When a registration needs a 4,097th record, `appendRange` calls `failEvidence` (`bbr_ecn.go:347`); an ACK split over budget does the same (`bbr_ecn.go:216`). The state becomes failed, and `mode` (`bbr_ecn.go:69`) then returns Not-ECT. `resetPath` (`bbr_ecn.go:325`) keeps lost evidence failed, so even a new path cannot restore ECN.

After *k* isolated loss events the ledger holds 2*k* records: hole, ACKed run, hole, ACKed run. A burst of consecutive lost packets is one hole, so a loss event costs two records whatever its size. Other distinct records lower that count: a random packet-number skip adds one, as do Not-ECT holes from path probes or coalesced short-header packets. Skips are rare, because the skip period doubles from 256 packets up to its cap. The sender traces below put the exhaustion at 3,271–3,794 declared losses, so L6's loss events averaged roughly 1.6–1.9 packets each. That is an inference; the traces do not record burst sizes.

**Observing it.** With the BBR validator installed, no `ECNStateUpdated` qlog events are emitted: those come from the legacy tracker that `EnableBBRECN` (`bbr_ecn.go:15`) removes. The BBR connection's once-per-second `congestion_control` qlog debug event ([`bbr_diagnostics.go`](../../../bbr_diagnostics.go)) carries the validator state ([`internal/ackhandler/bbr_diagnostics.go`](../../../internal/ackhandler/bbr_diagnostics.go)). It reads `ecn=missing-marking-evidence` once `evidenceLost` is set, which covers both range exhaustion and a missing ordinal anchor, as #612's diagnostics table records, and `ecn=invalid-counters` after a counter failure. The string alone therefore does not separate exhaustion from a missing anchor; the argument under the cross-run tables does.

## Evidence

### Deterministic test

`TestBBRECNOrdinaryLossExhaustsRangeBudget` in [`bbr_ecn_test.go`](../../../internal/ackhandler/bbr_ecn_test.go) drives real `SentPacket` registration, decoded ACK frames through `ReceivedAck`, and real loss detection. After ECN validation, it sends blocks of 64 consecutive packet numbers (registered directly, to exclude random skips) and never acknowledges each block's leading packets. Every ACK also repeats the previous block's range, so each hole lies inside the acknowledged span and recovery declares it lost.

```sh
go test -count=1 -v -run '^TestBBRECNOrdinaryLossExhaustsRangeBudget$' ./internal/ackhandler
```

| Case | Loss events at fallback | Lost packets at fallback | Result |
| --- | ---: | ---: | --- |
| Isolated loss (one packet per event) | 2,048 | 2,048 | Two records per event; ECT(0) until the ledger holds 4,096 records; the next block's first registration fails evidence (`evidenceLost`, not `counterFailed`), marking becomes Not-ECT, diagnostics read `ecn=missing-marking-evidence`, accepted counters stay unchanged, and the fallback persists |
| Paired loss (two consecutive packets per event) | 2,048 | 4,096 | The same, at twice the lost packets |
| Legacy validator, isolated loss | none after 4,096 events | none after 4,096 lost packets | ECT(0) throughout; every hole was declared lost |

The L6 runs' 3,271–3,794 declared losses lie between the isolated and paired cases. The test characterizes current behavior: a remedy that retires such records changes its BBR expectations.

### Cross-run tables

[`crossrun.py`](crossrun.py) regenerates the tables below from the two pinned raw archives (`python3 -I crossrun.py`; `--check` compares them with this file). The archives are on the record branches `codex/bbr-l6-coexistence` (`71e88789`) and `codex/bbr-l6-diagnosis` (`89e71a60`). Its docstring defines each column. In short:

- **Sender trace columns** come from #792's trace overlay, which records the BBR sender's own feedback events. "First ECN failure" is the first event whose feedback reports a failed validator. "Declared lost" sums the per-round lost packets of every round completed before it, so it can undercount by the round in progress.
- **Relay columns** come from relay2's counters. The switch is estimated from the 100 ms samples: the first sample at which the flow's arrivals, reconstructed from delivered bytes and drops, reach its ECT(0) arrival count. Every ECT(0) packet precedes the switch.

The relay counts run 50–279 drops above the sender's at the same moment: the round in progress, packets not yet declared lost, and the 100 ms sample step.

<!-- crossrun:begin -->
**Sender traces.** Every #792 observation with a focal trace:

| Observation | First ECN failure (s) | Packets the sender declared lost before it | Relay drops by then | Relay drops, whole run | Not-ECT share |
| --- | ---: | ---: | ---: | ---: | ---: |
| #792 `dev-L6-datagram-p0-trace-dev4` | 177.6 | 3,368 | 3,431 | 5,193 | 35.7% |
| #792 `dev-L6-datagram-p0-trace-dev6` | 176.8 | 3,366 | 3,416 | 5,472 | 36.3% |
| #792 `dev-L6-stream-p0-trace-dev3` | 54.3 | 3,650 | 3,838 | 13,218 | 69.8% |
| #792 `dev-L6-stream-p0-trace-dev5` | 86.6 | 3,794 | 3,987 | 12,236 | 61.1% |
| #792 `main-L6-datagram-p1-trace` | 166.8 | 3,422 | 3,501 | 5,462 | 38.7% |
| #792 `main-L6-datagram-p2-trace` | 161.2 | 3,388 | 3,463 | 5,626 | 40.0% |
| #792 `main-L6-datagram-p3-trace` | 166.4 | 3,462 | 3,516 | 5,444 | 38.6% |
| #792 `main-L6-stream-p1-trace` | 76.8 | 3,583 | 3,748 | 11,989 | 63.4% |
| #792 `main-L6-stream-p2-trace` | 16.5 | 3,572 | 3,851 | 22,198 | 80.7% |
| #792 `main-L6-stream-p3-trace` | 146.5 | 3,271 | 3,373 | 7,283 | 44.1% |
| #792 `smoke-L6-stream-p0-trace` | 97.8 | 3,597 | 3,745 | 9,787 | 57.2% |

Range: failure at 16.5–177.6 s, after 3,271–3,794 declared losses (11 observations).

**Relay counters.** Every observation of a BBRv3 focal flow in either archive:

| Observation | Forward packets | ECT(0) | Not-ECT | Drops | Drop share | Not-ECT share | Estimated switch (s) | Drops before the switch (est.) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| #777 `dev-L6-datagram-p0-cand-dev3` | 3,085,709 | 1,976,529 | 1,109,180 | 5,178 | 0.17% | 35.9% | 177.6 | 3,464 |
| #777 `dev-L6-stream-p0-cand-dev1` | 2,791,964 | 1,059,368 | 1,732,596 | 11,909 | 0.43% | 62.1% | 81.7 | 3,875 |
| #777 `main-L6-datagram-p1-cand` | 3,076,131 | 1,928,509 | 1,147,622 | 5,578 | 0.18% | 37.3% | 173.5 | 3,497 |
| #777 `main-L6-datagram-p2-cand` | 3,072,215 | 2,153,994 | 918,221 | 5,012 | 0.16% | 29.9% | 200.7 | 3,569 |
| #777 `main-L6-datagram-p3-cand` | 3,094,031 | 1,693,721 | 1,400,310 | 5,987 | 0.19% | 45.3% | 141.8 | 3,516 |
| #777 `main-L6-datagram-p4-cand` | 3,066,206 | 2,135,678 | 930,528 | 5,068 | 0.17% | 30.3% | 199.2 | 3,416 |
| #777 `main-L6-datagram-p5-cand` | 3,104,865 | 1,787,277 | 1,317,588 | 5,807 | 0.19% | 42.4% | 151.1 | 3,593 |
| #777 `main-L6-stream-p1-cand` | 2,836,354 | 646,317 | 2,190,037 | 17,511 | 0.62% | 77.2% | 28.9 | 3,947 |
| #777 `main-L6-stream-p2-cand` | 2,801,286 | 934,940 | 1,866,346 | 13,333 | 0.48% | 66.6% | 65.5 | 3,846 |
| #777 `main-L6-stream-p3-cand` | 2,776,426 | 969,233 | 1,807,193 | 10,663 | 0.38% | 65.1% | 71.1 | 3,556 |
| #777 `main-L6-stream-p4-cand` | 2,782,669 | 1,069,153 | 1,713,516 | 10,143 | 0.36% | 61.6% | 82.3 | 3,659 |
| #777 `main-L6-stream-p5-cand` | 2,727,822 | 1,480,987 | 1,246,835 | 7,629 | 0.28% | 45.7% | 141.2 | 3,381 |
| #777 `smoke-L6-stream-p0-cand` | 2,781,358 | 967,962 | 1,813,396 | 12,240 | 0.44% | 65.2% | 71.3 | 3,782 |
| #792 `dev-L6-datagram-p0-trace-dev2` | 875,549 | 875,545 | 4 | 1,628 | 0.19% | 0.0% | no switch | — |
| #792 `dev-L6-datagram-p0-trace-dev4` | 3,077,403 | 1,979,968 | 1,097,435 | 5,193 | 0.17% | 35.7% | 177.9 | 3,431 |
| #792 `dev-L6-datagram-p0-trace-dev6` | 3,077,069 | 1,958,669 | 1,118,400 | 5,472 | 0.18% | 36.3% | 177.1 | 3,416 |
| #792 `dev-L6-stream-p0-trace-dev1` | 2,734,722 | 1,529,118 | 1,205,604 | 7,066 | 0.26% | 44.1% | 147.1 | 3,131 |
| #792 `dev-L6-stream-p0-trace-dev3` | 2,790,603 | 842,781 | 1,947,822 | 13,218 | 0.47% | 69.8% | 54.6 | 3,838 |
| #792 `dev-L6-stream-p0-trace-dev5` | 2,777,353 | 1,079,524 | 1,697,829 | 12,236 | 0.44% | 61.1% | 87.0 | 3,987 |
| #792 `main-L6-datagram-p1-cand` | 3,089,627 | 1,730,736 | 1,358,891 | 5,922 | 0.19% | 44.0% | 145.8 | 3,657 |
| #792 `main-L6-datagram-p1-trace` | 3,104,700 | 1,902,899 | 1,201,801 | 5,462 | 0.18% | 38.7% | 167.1 | 3,501 |
| #792 `main-L6-datagram-p2-cand` | 3,075,972 | 1,926,070 | 1,149,902 | 5,597 | 0.18% | 37.4% | 172.2 | 3,580 |
| #792 `main-L6-datagram-p2-trace` | 3,111,699 | 1,867,846 | 1,243,853 | 5,626 | 0.18% | 40.0% | 161.5 | 3,463 |
| #792 `main-L6-datagram-p3-cand` | 3,062,641 | 1,987,949 | 1,074,692 | 4,961 | 0.16% | 35.1% | 181.8 | 3,382 |
| #792 `main-L6-datagram-p3-trace` | 3,083,319 | 1,892,940 | 1,190,379 | 5,444 | 0.18% | 38.6% | 166.7 | 3,516 |
| #792 `main-L6-stream-p1-cand` | 2,831,955 | 655,637 | 2,176,318 | 17,635 | 0.62% | 76.8% | 29.5 | 3,863 |
| #792 `main-L6-stream-p1-trace` | 2,796,237 | 1,024,266 | 1,771,971 | 11,989 | 0.43% | 63.4% | 77.2 | 3,748 |
| #792 `main-L6-stream-p2-cand` | 2,798,014 | 984,408 | 1,813,606 | 11,987 | 0.43% | 64.8% | 71.4 | 3,995 |
| #792 `main-L6-stream-p2-trace` | 2,871,400 | 555,009 | 2,316,391 | 22,198 | 0.77% | 80.7% | 16.8 | 3,851 |
| #792 `main-L6-stream-p3-cand` | 2,726,687 | 1,533,854 | 1,192,833 | 7,424 | 0.27% | 43.7% | 148.6 | 3,254 |
| #792 `main-L6-stream-p3-trace` | 2,748,260 | 1,535,640 | 1,212,620 | 7,283 | 0.27% | 44.1% | 146.9 | 3,373 |
| #792 `smoke-L6-stream-p0-trace` | 2,774,293 | 1,187,764 | 1,586,529 | 9,787 | 0.35% | 57.2% | 98.1 | 3,745 |

Range over the 31 observations that switched: drop share 0.16–0.77%, switch at 16.8–200.7 s, after 3,131–3,995 drops. Observations that never switched dropped 1,628 packets in total.

**Other focal flows** (frozen Reno), whole runs:

| Archive and arm | Observations | Drops | Most Not-ECT packets |
| --- | ---: | ---: | ---: |
| #777 `aa` | 10 | 386–775 | 3 |
| #777 `reno` | 13 | 324–835 | 3 |
<!-- crossrun:end -->

The failure time varies by a factor of twelve across runs, and the drop rate by a factor of five, while the number of losses at the failure stays within about ±12% of its midpoint. A fixed budget consumed by loss events produces exactly that. The validator's other failure paths do not explain it for valid feedback on one path, which is what these runs had:

- **Counter checks** (`bbr_ecn.go:198` and `bbr_ecn.go:209`: counts that decrease, exceed what was sent, or rise by less than the newly acknowledged marks) respond to peer counting errors or bleaching, which a codepoint-preserving relay does not produce. #777 records that relay2 passed `calibrate2`'s per-flow, per-direction ECN check.
- **A missing ordinal anchor** (`bbr_ecn.go:203`) needs an advancing ACK whose largest packet has no retained record. Every application-space send is registered (`congestion_dispatch.go:71`). An ACK of an unsent or skipped packet number is rejected as a protocol violation before ECN feedback runs (`sent_packet_handler.go:410` and `sent_packet_handler.go:597`, both before `captureBBRECN`), and such a rejection closes the connection, while every BBRv3 connection here kept sending until its run ended. Compaction removes only ACKed records, which lie at or below the feedback watermark, so the record of an advancing largest ACK is always retained.
- **Testing-state failures** (`failTesting` and `lostPacket`, `bbr_ecn.go:305`–`323`) do depend on loss, but they act only in the testing and unknown states, before validation. Those states allow at most ten ECT(0) packets before marking stops, so a flow that sent hundreds of thousands of ECT(0) packets had been validated. Only connection migration (`MigratedPath`, through `resetPath`) restarts testing, and the L6 fixture never migrates.

That leaves the range budget, the one failure path that depends on loss accumulated after validation. The deterministic test reaches it with `counterFailed` clear.

### Slice 6's single-flow runs

[Slice 6](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md)'s 40 s S5 runs kept ECT(0). From that record's medians, a BBRv3 S5 STREAM run carried about 330,000 forward packets at 0.217% overflow, about 720 drops; S5 DATAGRAM about 380,000 at 0.025%, about 100. Both are far below the roughly 3,100 at which every L6 run switched. These counts are estimates from medians, not per-run counters.

## Correct behavior or a defect

**The code matches its design.** The [classic ECN response](../../designs/bbrv3.md#classic-ecn-response) section caps the ledger at 4,096 records, compacts "only such a prefix" as is "completely accounted for by individual ACK evidence and accepted cumulative counts", and on overflow says to "fail marking to Not-ECT before losing evidence". [T4's contract](../../adr/2026-09-24-bbrv3-transport-plan.md#t4) lists budget overflow with a Not-ECT fallback as covered behavior. #612 examined the budget and retained it.

**The design's assumption fails.** #612's reproduction needed a suffix of alternating codepoints, and its record says it "does not claim an ordinary sender alternates them at this rate"; it recorded production incidence and time to exhaustion as unknown. They are now known for L6: every BBRv3 run that reached about 3,100 drops lost ECN, between 17 s and 201 s in. Ordinary loss makes records distinct by ACK status, which the alternating-codepoint case did not show.

**Why it is a defect.** The design's bounds are memory and accuracy trade-offs. Because never-acknowledged records are never retired, this one is a lifetime budget of about 2,048 loss events instead. D03 chose a classic CE response and rejected disabling ECT; exhaustion disables ECT anyway on any long connection that loses packets, so the CE response (and the S8 qualification row that would measure it) goes inert exactly where it is meant to act. This is the "ECN … silently disabled under ordinary loss" case #793 named.

**Out of scope.** #777's L6 classification is unaffected: its queue never marks. BBRv3 remains experimental and opt-in, and default Reno is unaffected.

## Inputs for a remedy decision

No remedy is adopted here. Any accepted alternative must still meet the five obligations #612 listed: actual-codepoint authority, late feedback, ordinal anchors, cumulative counter fences and bounded storage.

| Option | What it changes | What it leads to |
| --- | --- | --- |
| Retire records of packets that will not be acknowledged | The ledger's representation and the design's compaction rule (a design amendment, then implementation). A retired hole would leave the ledger; a late ACK covering it would raise the peer's counters without adding newly acknowledged marks, which loosens the "too few counts" check by that packet and can attribute a late CE mark to a later ACK. | A bounded ledger whose size tracks unresolved packets in flight. Needs a retirement rule that answers #612's objection that recovery loss is "a scheduling/accounting judgment, not evidence that the receiver can never acknowledge that transmission", a reviewed design amendment, and a fresh measurement. |
| Raise `maxECNMarkRanges` | A bound change, which the [port contract](../../agents/bbr-production-port.md) leaves to named evidence or an explicit operator amendment | The same failure later, in proportion to the bound (16× the bound is roughly 4.5–53 minutes on L6), with a larger worst-case ledger and ACK-split work |
| Keep the fallback and document it | Documentation only | BBRv3 is ECN-capable only for about its first 2,048 loss events per connection; a CE-response qualification would have to stay inside that window |
| Use the legacy validator's inference for BBR | Replaces the representation contract | Conflicts with the design's rule that numeric membership in a capable epoch is not evidence of marking; rejected by T4's contract |

## Not examined

- No new host run. The evidence is the code, the deterministic test, and the existing #777 and #792 archives. The `ecn=missing-marking-evidence` state was not observed in a live run's qlog; the traces time the failure, and the argument under the cross-run tables excludes the other failure paths (which the string alone would not distinguish from a missing anchor).
- Burst sizes of the L6 drops, other paths and platforms, and other revisions than `abfa5456`/`7a84a914`.
- Whether a remedy can meet all five #612 obligations. That is the remedy's design work.

## Operator decision

Pending.
