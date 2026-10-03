# Quantum-release BBR pacing and the STREAM receiver heap rise

Status: complete for [Adopt quantum-release BBR pacing and explain the STREAM receiver heap rise](https://github.com/the-sarge/quic-go-fast/issues/710), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Experimental candidate branch only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-quantum-pacing`, based on `b516b18e` (the ECN-correction record on top of `777ce739`).

## Answer

**D2 is adopted on the D1+D2 candidate `fc4c1bf1`.** Under D3's outcome table this is "genuine quantum effect with whole-run receiver peak RSS within the 10% flag". The measured-window heap evidence is disclosed below, and the existing RSS excess is left to D5.

The STREAM receiver heap rise is loss-driven reassembly. Releasing a full quantum per wakeup makes the S5 one-BDP drop-tail queue overflow about six times as often (0.13% to 0.84% of forward packets). Each overflow leaves a hole. Behind it the receiver must hold up to about 4 MiB of received STREAM frames until the retransmission arrives, and those frames come from the pooled 1,452-byte frame buffers. The receiver spends 3.4× as long holding such data, so garbage collections more often mark a large live heap, and the measured-window heap peak rises about 1.2× in every block. Neither the per-excursion maximum nor whole-run receiver RSS rises: quantum/per-datagram RSS is 1.009. No relay artifact was found. Sender CPU per useful GiB falls from 1.58× to 1.08× Reno in the same pairs.

## D2 build

Candidate revision `fc4c1bf1` implements D2 of the [WAN-rate correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md#d2-pacing-release-a-full-quantum-per-wake-amendment-to-d08) on the D1 (ECN-corrected) candidate `777ce739`.

**Change.** `bbrSendPolicy.deadline(size)` still returns zero when credit covers `size`. Otherwise it returns the exact time for credit to reach Q, rounded up to the nanosecond clock unit, instead of the time to cover `size`. Every caller passes `size ≤ Q` (ordinary sends pass M ≤ Q; MTU probes pass `min(probeSize, Q)`), so sub-Q probes adopt the Q deadline and probes above Q keep it. Admission is unchanged: an opportunity that arrives earlier for another reason still sends whatever credit covers. Q's formula, the token cap, initial credit, debit, clamping on rate or Q decreases, two-Q pending credit, isolated-probe admission and debt, and the ACK, PTO and path-validation exemptions are untouched.

**Design amendment.** In [bbrv3.md](../../designs/bbrv3.md), "Deadline uses exact deficit/rate rounded up to the monotonic clock unit" now reads "When credit cannot cover the next paced admission, the deadline is the exact time for credit to reach Q, rounded up to the monotonic clock unit; this includes DPLPMTUD probes below Q, while ACK-only, authorized PTO and path-validation exemptions do not use this deadline." The D08 row records the amendment, its measured motivation and the rejected alternatives.

**Tests** ([bbr_send_policy_test.go](../../../bbr_send_policy_test.go)):
- `TestBBRPacingQuantumAndDeadline` is rewritten as deterministic cases: floor Q = 2M (M = 1200 and 1300), rate-derived Q (9,900 bytes) and capped Q (65,536 bytes), each checking that covered admissions are unpaced, that a short admission's deadline is the fill-to-Q instant, the credit one nanosecond before it, and that delayed wakeups accumulate at most Q. Further cases cover fractional credit (the target stays Q, admission still happens once credit covers M, and retained fractional credit shortens the next wait by one nanosecond), rate and Q decreases (clamping, and the wait targeting the decreased Q), and probes below, equal to and above Q with their post-send credit or debt.
- `TestBBRPendingCreditMTUException/paced_probe` now pins the 1,300-byte probe's deadline at the Q instant (2,424,243 ns, formerly 1,313,132 ns) and 1,100 bytes of credit after it. The same test's oversized-probe tail, which also pinned the exact-deficit rule, now expects 7,070,708 ns (debt plus Q) and still shows 1,200 bytes available at the former instant.
- New `TestBBRQuantumDeadlineExemptions` drives emission with ordinary pacing credit spent, with local credit free and with exactly one packet of the 2Q local bound left. An ACK proceeds while the payload's deadline is the Q instant; an authorized PTO proceeds with no deadline; a client path-validation probe proceeds directly without touching pacing or local credit.
- Mutants of the new rule are all killed: the old exact-deficit deadline (the four deadline cases, fractional credit and probes fail), gating admission on Q instead of `size` (fractional credit and the MTU control test fail), and ignoring probe debt in the deadline (the above-Q probe and the MTU tail fail).

**Gates** on `fc4c1bf1` with the host Go 1.27.1: `go vet ./...` clean; `internal/ackhandler` and `internal/congestion` pass; the root package passes, and the BBR, emission, credit, MTU and ECN tests also pass with `-race` ([gates.log](gates.log)).

## D3 pre-registration

Written and committed before the main stage ran. Nothing below is changed after outcomes are seen; deviations are recorded as deviations.

**Arms.** Each built from an exported tree with the same measurement-only overlay ([build.py](build.py)):

| Arm | Source | Controller |
| --- | --- | --- |
| Reno | frozen `e4f322cb` | matched Reno reference |
| Per-datagram | `777ce739` (D1) | BBRv3, exact-deficit deadline |
| Quantum | `fc4c1bf1` (D1+D2) | BBRv3, deadline at Q |

**Path and workload.** S5 (100/100 Mbit/s, 100 ms RTT, one-BDP drop-tail queue, no injected loss), STREAM, 10 s warmup and 30 s measurement, the demonstration's endpoint contract and relay model. Eight blocks; each block runs the three arms in rotated order with one shared declared seed (8001–8008). If the primary heap comparison below is not separated after eight blocks, four more blocks (8009–8012) run once, and the test is evaluated at twelve. No run is excluded after its outcome is seen; a failed run is retained and reported.

**Instrumentation** (identical in every arm):
- Receiver and sender: a 10 ms series of Go runtime metrics (heap objects, live heap at last mark, heap goal, cumulative allocation, GC cycles, mapped/released/free memory) and `getrusage` peak RSS.
- Transport overlay: receive-queue length and its maximum per sample, and connection-level flow-control received and read offsets with the largest received-but-unread byte count per sample. For this one-connection fixture, connection-level unread bytes bound the stream's buffered reassembly data.
- Relay: the v1 model ([relay/main-v1.go.src](relay/main-v1.go.src)) plus observability only ([relay/main.go](relay/main.go)): per-10 ms forward packets written, largest single-wakeup batch, maximum lateness and cumulative overflow drops, and a whole-run histogram of packets written per wakeup. The queue model, admission order and timing are unchanged.
- A separate three-block heap-series stage (seeds 8101–8103) also writes a receiver heap profile every second without forcing GC, for allocation and retention sites at the peak. It is not used for the RSS or heap-peak statistics because profile writing perturbs the heap.

**Measures per observation.**
- M1: whole-run receiver peak RSS.
- M2: measured-window receiver heap peak, from the fixture's 1 Hz `HeapAlloc` samples as in the demonstration; also the 10 ms series peak of heap objects.
- M3: live heap and heap goal at the M2 series peak.
- M4: received-but-unread connection bytes: measured-window maximum and time integral.
- M5: receive-queue maximum length.
- M6: relay forward overflow drops, maximum lateness, `LateOver1ms`, the stall count (wakeups more than 5 ms late) and the share of forward packets written in multi-packet wakeups.

**Comparisons.** Paired by block. The primary comparison is quantum against per-datagram; each BBR arm is also compared with the same block's Reno. Report median, minimum and maximum of per-block ratios, the count of blocks in each direction, and an exact two-sided sign test. An effect is *separated* from GC-timing variance when the sign test gives p < 0.05 (8 of 8 blocks, or at least 10 of 12 after extension).

**Attribution rules.**
1. *Retention by buffered stream data* is supported when a separated quantum effect on M2 is accompanied by a separated effect in the same direction on M4, and, across observations, M3 live heap at the heap peak tracks M4 at that time.
2. *Loss-driven reassembly* is supported when M4 excursions above 0.5 MiB start within one RTT plus 50 ms after forward overflow drops in the relay timeline, and the quantum arm's overflow drops are separated from the per-datagram arm's in the same direction as M4.
3. *Relay artifact* needs positive evidence: M4 or heap excursions that start after relay stalls (forward lateness over 5 ms) without overflow drops in the preceding window, and an arm difference in stall exposure that explains the heap difference. Absence of another explanation is not artifact evidence.
4. *GC-pacing effect* (no retention): a separated M2 effect with no separated M3 or M4 effect. Classified as a genuine quantum effect on garbage high-water, not as an artifact.
5. Anything else, or rules that conflict, is *inconclusive*.

**Flag rule for the D3 table.** The readiness flag is whole-run receiver peak RSS more than 10% above matched Reno. Because the per-datagram arm already carries receiver RSS excess (D3, D5), the quantum-attributed part is judged incrementally: the quantum effect *keeps whole-run receiver peak RSS above the flag* if the paired median of quantum/per-datagram M1 exceeds 1.10, or if the quantum arm's median M1 ratio to Reno exceeds 1.10 while the per-datagram arm's does not. Absolute values and both Reno ratios are reported either way. Any per-datagram excess is left to D5 under the re-demonstration and is not classified here.

**Mapping to D3's outcome table.** A positive artifact attribution (rule 3) with no remaining quantum effect is "artifact". Rules 1, 2 or 4 identify a genuine quantum effect, which is adopted or stopped by the flag rule. An implementation defect is claimed only if a discriminating comparison shows that receive-side retention exceeds what the design requires; then a fix within existing contracts is built, and the affected stage is rerun on the fixed revision. Mixed causes follow the D3 table. Inconclusive stops.

## D3 results

Eight complete blocks (24 observations), all clean, none excluded. The extension was not needed, because the primary comparison separated at eight blocks. Every observation passed receiver integrity: zero corrupt or duplicate payload, per-second totals equal to useful bytes, and matching sender and receiver reports. The tables come from [analyze.py](analyze.py); [summary.json](summary.json) holds every per-observation value.

### Primary paired comparison: quantum against per-datagram

Ratios are per-block medians [min–max]. *Higher/lower* counts blocks where quantum exceeds per-datagram. Absolute values are medians in MiB unless noted.

| Measure | Q/PD ratio | Higher/lower, sign test | Reno | Per-datagram | Quantum |
| --- | --- | --- | --- | --- | --- |
| M1 whole-run receiver peak RSS | 1.009 [0.991–1.388] | 4/4, p = 1.0 | 24.3 | 33.3 | 33.5 |
| M2 measured-window heap peak (1 Hz) | 1.206 [1.020–1.705] | **8/0, p = 0.0078** | 3.4 | 10.3 | 12.6 |
| M2 same, 10 ms series peak | 1.096 [0.767–1.480] | 6/2, p = 0.29 | 3.6 | 12.8 | 15.2 |
| M3 live heap, measured-window max | 1.098 [1.005–1.470] | **8/0, p = 0.0078** | 1.6 | 6.8 | 8.0 |
| M4 received-but-unread, max | 1.101 [0.974–1.815] | 6/2, p = 0.29 | 0.03 | 3.3 | 4.2 |
| M4 received-but-unread, time integral (MiB·s) | 3.372 [2.146–6.084] | **8/0, p = 0.0078** | 0.00 | 1.4 | 4.9 |
| M5 receive-queue maximum (packets) | 1.505 [0.390–2.258] | 6/2, p = 0.29 | 26 | 37 | 40.5 |
| M6 forward overflow drops, whole run | 5.416 [3.138–11.518] | **8/0, p = 0.0078** | 234 | 448 | 2,869 |
| M6 forward overflow drops, measured window | 7.028 [3.653–13.841] | **8/0, p = 0.0078** | 0 | 257 | 2,237 |
| M6 relay stall intervals (> 5 ms late) | — | 3/2, p = 1.0 | 0 | 0 | 0.5 |
| M6 share of forward packets in multi-packet relay wakeups | 1.315 [0.503–6.386] | 5/3, p = 0.73 | 0.003 | 0.007 | 0.007 |

Relay timing per run, as median [min–max]:

| Measure | Reno | Per-datagram | Quantum |
| --- | --- | --- | --- |
| Forward maximum lateness (ms) | 4.2 [1.5–16.1] | 4.1 [1.4–8.3] | 6.2 [1.4–56.2] |
| Forward `LateOver1ms` | 68 [10–5,016] | 177 [9–1,199] | 284 [18–1,241] |
| Reverse `LateOver1ms` | 48 [11–2,345] | 121 [6–578] | 165 [18–672] |

The physical delivery pattern at the receiver did not change materially. In every arm at least 96.5% of forward packets left the relay one per wakeup.

### Attribution

- **Rule 1, retention by buffered stream data: supported.** M2's separated rise is matched by separated rises in the M4 integral and the M3 live-heap maximum. The pre-registered across-block statistic is degenerate, and was replaced post hoc: see Deviations. In that replacement, each GC mark's live heap tracks the unread bytes when the mark completed, with within-run Spearman medians of 0.42 [0.39–0.53] for quantum, 0.30 [0.03–0.37] for per-datagram and 0.04 for Reno. Marks taken with more than 1 MiB unread find 5.2/5.4 MiB live (quantum/per-datagram), against about 3.0 MiB otherwise. Quantum has 59 such marks, per-datagram 14 and Reno none.
- **Rule 2, loss-driven reassembly: supported.** Quantum's overflow drops are separated in the same direction as M4 (8/0). Of 94 quantum excursions above 0.5 MiB, 86 start within 150 ms after forward overflow drops, and none is preceded only by a relay stall. Per-datagram has 40 excursions: 13 fall inside that window and 27 follow a drop by 166–204 ms. The pre-registered window left out the roughly 45 ms needed to accumulate 0.5 MiB at line rate after the hole's arrival time, so it undercounts the slower per-datagram case; the quantum classification does not depend on this.
- **Rule 3, relay artifact: not supported.** No excursion in any arm is preceded only by a stall. Quantum's lateness is slightly higher, but stalls are rare in every arm and do not explain the heap or unread-data differences.
- **Heap sites at the peak** (heap-series stage, three blocks, [heapsites.json](heapsites.json)). At the profile closest to the measured-window peak, the largest in-use site in the quantum arm is `internal/wire` stream-frame pool allocation (`wire.init.0.func1`, 1,452-byte frame buffers): 3.0–4.6 MiB of 9.1–10.1 MiB. It reaches 3.6 MiB of 9.0 MiB in one per-datagram profile and is absent from Reno's top sites. These are received STREAM frames held in reassembly. The allocation sites in the second before each peak are ordinary receive-path churn (`ReadPacket`, timers, stream frames, `captureCongestionSend`) and differ between arms only in amount.
- **Why bursts overflow more** (descriptive). Both BBR arms keep a small median forward queue (3–6 KB) and probe up to the one-BDP cap. The quantum arm spends 5.9% [5.1–6.7%] of measured 10 ms samples within 12 KiB of the cap, against 1.7% [0.8–3.5%] per-datagram. A Q-sized release that arrives near the cap overflows where single datagrams would fit. The arms differ only by D2's deadline line, so the paired comparison attributes the drop increase to D2. Why the quantum arm reaches the cap more often is not decomposed further.

**Classification.** This is a genuine quantum effect, and it is not an implementation defect. Holding received data behind a hole is the reliable in-order delivery requirement, served by the receive path that Reno also uses, unchanged by the candidate. The 1,452-byte pooled buffer per frame is upstream receive-path behaviour, also unchanged.

**Flag rule.** Paired quantum/per-datagram receiver RSS is 1.009, which is at most 1.10, and the per-datagram arm is already 1.315× Reno, so the quantum effect does not keep whole-run receiver peak RSS above the flag. Absolute RSS: quantum/Reno 1.383 [1.244–1.618] and per-datagram/Reno 1.315 [1.135–1.391]. Both remain above the 10% flag. Receiver RSS reaches 99% of its final value at a median of 9.9 s (quantum) and 16.7 s (per-datagram), so the whole-run peak is set mostly during warmup, consistent with the Startup transient the demonstration reported. That excess belongs to D5 and the re-demonstration.

### Other observations in the same pairs

These come from instrumented builds and are reported only as context. The re-demonstration owns the flags.

| Ratio to the same block's Reno | Per-datagram | Quantum |
| --- | --- | --- |
| Sender CPU per useful GiB | 1.581 [1.443–1.725] | 1.079 [0.971–1.193] |
| Receiver CPU per useful GiB | 1.040 [0.949–1.114] | 1.006 [0.918–1.091] |
| Goodput | 0.976 [0.974–0.982] | 0.980 [0.969–0.985] |
| Control p95 | 1.012 [0.940–1.099] | 1.049 [0.960–1.224] |
| Sender peak RSS | 1.159 [1.061–1.222] | 1.199 [1.112–1.259] |

The higher overflow rate is a behaviour change in its own right: about 0.84% of forward packets are dropped at the S5 bottleneck, against 0.13%. Goodput is unaffected. The re-demonstration should report the loss rate. One quantum block exceeds the 20% control-p95 flag (1.224); the median does not.

### Deviations from the pre-registration

1. **Degenerate rule-1 statistic.** The pre-registered across-block correlation of live heap and unread bytes "at that time" is undefined for Reno and per-datagram, because unread bytes are 0 at every series heap peak. `/gc/heap/live` is the previous mark's value, and the heap-objects peak is the instant before the next GC, which follows the drain of a reassembly excursion. It is replaced post hoc by pairing each completed GC mark with the unread bytes just before it. Rule 1's separated-measure conditions are unaffected.
2. **Rule-2 window.** As noted above, the 150 ms window omits the fill time to the 0.5 MiB threshold. The drop-lag distribution is reported as descriptive evidence.
3. **Relay v1 identity.** The demonstration's claim that the relay "rebuilds byte-identically" depended on Go's VCS stamping of its own worktree (`vcs.revision=7387445d`). Its `relay/main-v1.go.txt` was never committed, because the repository ignores `*.txt`. This record pins the on-disk v1 source by SHA-256 (`19635ff6…`), commits it as [relay/main-v1.go.src](relay/main-v1.go.src), and builds every binary with `-buildvcs=false`.

## Preservation

- Default Reno, the Q formula, the token cap, initial credit, debit, clamping, two-Q pending credit, probe admission and debt, the exemptions, ECN ledger semantics and every recorded translation are unchanged. The only production-code change is the deadline line in `bbr_send_policy.go`.
- All 33 retained observations (24 main, 9 heap-series) exited cleanly with receiver integrity. Two smoke runs are excluded from every statistic. One used the relay before overflow sampling was added and is kept under `superseded-relay` in the raw archive.

## Limits

- One macOS host, loopback endpoints and a userspace relay, on a shared desktop with recorded background load (Lightroom, WindowServer and a concurrent test process). This is not a native two-host or kernel emulator.
- One path (S5) and one workload (STREAM); DATAGRAM and S6 are left to the re-demonstration. The floor-regime latency (Q = 2M at low rates) remains unmeasured.
- Instrumented builds: every arm carries the same occupancy overlay and 10 ms sampler. The heap-series stage also writes profiles, which perturb its own runs, so it is excluded from the statistics.
- Eight blocks: separation uses an exact sign test, and per-block ratios are reported with their ranges.

## Assets and reconstruction

- **Results:** [summary.json](summary.json), [heapsites.json](heapsites.json), [gates.log](gates.log).
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json). It holds every observation (configs, endpoint, series and relay records, commands, exits, host samples, heap profiles), stage logs and build receipts. Binaries and credentials are omitted.
- **Aids:** [build.py](build.py), [run.py](run.py), [matrix.py](matrix.py), [analyze.py](analyze.py), [heapsites.py](heapsites.py), [overlay/](overlay/), [relay/](relay/) and [model-overlay/next.go](model-overlay/next.go). These are frozen one-ticket aids, not a maintained benchmark framework. To reconstruct, in a fresh owned worktree of this branch, run `build.py`, then `matrix.py smoke`, `main` and `heapseries`, then `analyze.py` and `heapsites.py`. The runner refuses to overwrite a prior attempt.
