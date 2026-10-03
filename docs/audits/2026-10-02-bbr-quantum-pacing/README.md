# Quantum-release BBR pacing and the STREAM receiver heap rise

Status: in progress for [Adopt quantum-release BBR pacing and explain the STREAM receiver heap rise](https://github.com/the-sarge/quic-go-fast/issues/710), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Experimental candidate branch only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-quantum-pacing`, based on `b516b18e` (the ECN-correction record on top of `777ce739`).

## D2 build

Candidate revision `fc4c1bf1` implements D2 of the [WAN-rate correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md#d2-pacing-release-a-full-quantum-per-wake-amendment-to-d08) on the D1 (ECN-corrected) candidate `777ce739`.

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
