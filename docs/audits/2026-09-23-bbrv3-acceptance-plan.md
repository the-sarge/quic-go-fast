# BBRv3 acceptance evidence and campaign budget

Accepted plan for [Choose acceptance criteria and an evidence budget](https://github.com/the-sarge/quic-go-fast/issues/557), under [Design opt-in BBRv3 for GridCast bulk transfers](https://github.com/the-sarge/quic-go-fast/issues/552). Repository inspection is pinned to `7e33f5ebd940f2fd05146ea4c81343199c6d15e1`, with earlier infrastructure observations pinned to `270dda4cf6a4ff66a873473e8dd68e1f12ea619f`. This is planning only. No controller, emulator or performance campaign has been implemented or run.

## Decision status

The owner accepted the scope, workloads, primary metric, human adoption decision, modeled-network approach, platform split, ordinary bulk-flow coexistence goal, repetitions, run lengths, four-core endpoint budget, advisory flags and three queue classes below. The owner accepted a 48-hour ceiling and clarified the accounting: count one complete experiment once regardless of how many machines participate; concurrent experiments each consume their own duration. The earlier “summed across machines” wording is superseded. Resource occupancy can be reported separately but does not multiply the experiment budget.

On 2026-09-23, the owner also accepted the concrete numbered scenario matrix, model parameters, topology contract, completion fixtures, eight-hour preparation allowance and 48-hour budget allocation below. The research note retains its candidate inputs and source uncertainty; accepting these model settings does not establish real-world medians.

## Accepted scope and evaluation policy

Required acceptance evidence is produced within quic-go-fast without GridCast or wiremux changes. GridCast informs workload shape and the consumer adoption contract. Actual GridCast transfers can supply supplemental real-world evidence; the owner cannot provide access to the named access networks, and obtaining those networks is not a prerequisite. No service purchases, cross-repository execution or production rollout are part of this map.

Run real QUIC endpoints over controlled network emulation, with deterministic simulations for specific controller events. A virtual-time model does not measure native CPU cost or real-time throughput. State conclusions as behavior under declared models rather than qualification on a real satellite service, cellular operator or country pair. Sources and uncertainty are retained in [Candidate real-world path profiles](2026-09-23-bbrv3-real-world-path-profiles.md).

Correctness and protocol compliance are hard gates. A 20% goodput improvement is an aspirational target, not a floor. The owner judges adoption after reviewing the data; neither a minimum improvement nor a statistical-significance threshold is an automatic performance gate. Report inconclusive, limited and missing evidence explicitly. A correctness failure cannot be offset by a throughput gain.

| Advisory flag, compared with matched Reno | Owner-accepted attention threshold |
| --- | ---: |
| Clean WAN or LAN receiver goodput | More than 5% lower |
| CPU time per delivered GiB | More than 10% higher |
| Peak memory | More than 10% higher |
| p95 control-stream response time | More than 20% higher |

These are review flags, not automatic rejection rules. Show absolute values and run variability alongside ratios. If a baseline denominator is zero or too small to produce a useful ratio, show the absolute difference and explain the limitation. Coexistence, packet loss, event recovery, startup and completion remain visible even where no numeric advisory flag has been chosen.

## Workloads and observation boundaries

Both reliable-stream bulk transfer and bulk DATAGRAM traffic alongside a reliable control stream are required and reported separately. The primary metric is unique useful payload consumed by the receiving test application during a fixed measurement interval after a predefined warm-up. Local send admission or a QUIC ACK does not establish that application observation. Protocol headers, repeated delivery and fixture metadata do not count in the numerator.

Fixture details: use deterministic content and sequence identities to detect corruption and duplication. For the DATAGRAM fixture, use a fixed negotiated payload size no larger than 1,200 bytes including the fixture identity; verify admission limits before the campaign. Count only useful content bytes in both fixtures. Use a reliable control stream with a 32-byte request and 32-byte response, offered once per second with at most one request outstanding; count skipped opportunities while a request is pending. Report response latency, unresolved requests and achieved cadence so slow paths cannot hide blocked requests. The same control fixture accompanies each bulk workload.

Record startup and handshake separately from the primary interval. Every scheduled slowdown and interruption within the measurement window stays in its denominator. Keep full-run and event-relative time series, not just a final average. Reliable completion uses receiver consumption and integrity verification, not a sender-side Write return. The DATAGRAM fixture does not pretend to provide reliable file completion or silently add GridCast's repair protocol.

A run record must identify the actual controller, candidate revision, BBR baseline and ECN policy, native platform, packet-I/O path, offload state, MTU, endpoints and emulator versions, workload settings and random seeds. Record sender demand, congestion/pacing constraints, flow-control stalls, application limitation, queue/drop/mark observations and CPU saturation sufficiently to distinguish controller behavior from a fixture or host limit. Retain limited runs rather than deleting them; do not present their throughput ceiling as network-controller capacity. A candidate-induced CPU bottleneck is itself relevant resource evidence.

## Accepted platform and resource coverage

Linux/amd64 runs the full selected modeled matrix. macOS/arm64 and Windows/amd64 each run a native LAN baseline, one long-RTT comparison and one changing-capacity/interruption comparison, covering both workloads. Correctness coverage continues across supported platforms. Native ECN claims are confined to the actual qualified platform and packet-I/O path; managed Windows capability does not establish ECN on GridCast's ordinary wrapper path.

Each endpoint receives four CPU cores, with identical allocations for Reno and BBRv3. Reserve emulator and background-traffic resources outside those allocations. Report CPU time, allocations, peak memory and saturation; record actual core type, affinity or scheduling controls and Go processor setting. Set GOMAXPROCS=4 for each focal endpoint process. Four Go processors alone do not prove four isolated physical cores. Host inventory and isolation must be demonstrated during preparation; current CI platform names are not evidence that suitable performance hosts are available.

**Operator amendment, 2026-09-25:** Mac endpoints may instead use otherwise-idle native hosts with `GOMAXPROCS=4`, identical Reno/BBRv3 settings, separate gateway resources and recorded CPU contention. Report this scheduling limitation explicitly; do not claim four isolated physical cores on macOS. Linux and Windows retain the resource contract above.

## Accepted repetition and timing plan

Use five paired Reno/BBRv3 runs per selected scenario and workload. Within a pair, match workload demand, emulator schedule and seed; randomize controller order. Different pairs may use different declared seeds. Report all observations, paired differences, median and spread. Any uncertainty estimates must describe their assumptions; no significance gate has been adopted. Additional repetitions need a concrete question and must fit the remaining budget.

Every row marked Standard in the matrix uses 30 seconds warm-up and 180 seconds measured. Every row marked Long (L0–L8) uses 60 seconds warm-up and 300 seconds measured. These durations are fixed for both controllers. Preserve startup observations and include outages in measured time. Calibrate the clocks and event alignment before comparison; do not combine endpoint timestamps without a declared synchronization/error contract.

## Accepted concrete matrix

This is the owner-accepted matrix. One row means both workloads, both controllers and five repetitions: 20 runs. Capacities below are decimal Mbps, forward/reverse, with the sender in the named source location. A named geography is only a model label. Each modeled RTT is the total base round trip, with an equal split of propagation delay across directions. Receiver access must support the stated forward cap; no hidden access bottleneck is assumed.

| ID | Linux scenario | Forward/reverse Mbps | Base RTT | Mode | Run length |
| --- | --- | ---: | ---: | --- | --- |
| S1 | Native LAN baseline | Measured link capacity | Measured | No injected impairment | Standard |
| S2 | UK to Los Angeles | 20 / 100 | 150 ms | Ordinary queue | Standard |
| S3 | Singapore to Los Angeles | 1,000 / 1,000 | 200 ms | Ordinary queue | Standard |
| S4 | Eastern Australia to New York | 20 / 100 | 250 ms | Ordinary queue | Standard |
| S5 | Clean WAN control | 100 / 100 | 100 ms | Ordinary queue | Standard |
| S6 | WAN loss sensitivity | 100 / 100 | 100 ms | S5 plus 0.1% independent forward loss | Standard |
| S7 | Deep-queue WAN | 100 / 100 | 100 ms | S5 with deep queues | Standard |
| S8 | Classic-ECN WAN | 100 / 100 | 100 ms | Mark-before-overflow queue | Standard |
| L0 | Starlink-inspired steady control | 20 / 100 | 40 ms | Same fixture and duration as L1/L2, no event | Long |
| L1 | Starlink-inspired periodic delay | 20 / 100 | 40 ms | Delay events every 15 seconds | Long |
| L2 | Starlink-inspired periodic loss | 20 / 100 | 40 ms | Loss events every 15 seconds | Long |
| L3 | Cellular capacity changes | Scripted below | Scripted below | LTE, 5G and weak-link phases with interruption | Long |
| L4 | Shared maritime VSAT | 4 / 4 | 1,000 ms | Increasing competing demand | Long |
| L5 | Maritime fallback | 0.256 / 0.256 | 1,000 ms | Separate scarce-capacity case | Long |
| L6 | Coexistence with QUIC Reno | 100 / 100 | 100 ms | Competing reliable Reno flow | Long |
| L7 | Coexistence with TCP CUBIC | 100 / 100 | 100 ms | Competing reliable TCP flow | Long |
| L8 | Coexistence with BBRv3 | 100 / 100 | 100 ms | Same pinned BBR candidate, independent state | Long |

On macOS and Windows repeat S1, S3 and L3. For each named platform, use two native endpoints of that platform and architecture, so each row exercises native send and receive behavior without duplicating an entire reverse-direction matrix. Prefer separate endpoint machines for the native LAN comparison; an all-loopback result cannot stand in for a LAN result. A calibrated impairment gateway may run Linux independently of endpoint OS. If suitable pairs are unavailable, report a prerequisite gap and propose a recounted alternative; do not silently substitute cross-compilation, a different receiver OS or shared CPU resources. The platform-pair availability is a preparation obligation, not a claim that hosts have been reserved.

The selected S2/S3/S4 capacities are one representative choice per route rather than the research note's full capacity sweep. L3 combines several cellular regimes into one explicitly changing-path scenario; it is not evidence for separate steady-state 4G and 5G runs. These reductions, plus any reserve-only sensitivities, must remain visible to the owner.

## Queue and impairment parameters

The accepted queue classes are ordinary loss-on-overflow buffering holding about one round-trip's traffic, deep buffering holding four round-trips' traffic, and classic ECN marking before overflow. Exact sizing: for each direction use its configured rate times base RTT divided by eight, round up to whole maximum-sized packets, and allow at least two packets; multiply by four for the deep queue. Count full modeled IP packet bytes consistently. Keep propagation delay outside the finite bottleneck queue. At a rate change, keep a declared fixed byte capacity rather than silently resizing or flushing the queue. For L3, preparation must select and record the rate/RTT sizing basis and resulting packet-rounded fixed byte capacity in each direction before calibration, then use that same basis and capacity for both controllers and all three native platforms. The changing phases do not select a new queue size implicitly; this preparation detail must be pinned in the runnable manifest.

For S8, a diagnostic queue marks eligible ECT(0) packets CE when occupancy exceeds 25% of the ordinary queue capacity and drops on overflow. Non-ECT traffic needs equivalent early-drop treatment at that threshold for a meaningful response comparison. This deliberately simple queue is a synthetic feedback fixture, not a claim to implement a deployed AQM or L4S. The controller's CE coefficient and cap release remain owned by [Settle the implementation-ready BBRv3 design](https://github.com/the-sarge/quic-go-fast/issues/558). Deterministic correctness cases must cover invalid/reordered ECN counters, validation failure/fallback, simultaneous CE/loss and mixed-path feedback independently of this performance row.

L1 events add 200 ms of propagation delay to forward packets that leave the bottleneck during a 50 ms window once per 15 seconds. Bottleneck service and finite-queue sizing stay unchanged; the extra delay is applied after bottleneck service, and resulting packet reordering is recorded. Thus the injected mechanism is delay rather than a service pause that would necessarily introduce queue overflow. Bound the pending-delay storage from configured maximum rate and delay and count any unintended overflow as an emulator failure. L2 instead discards forward packets arriving during a 50 ms window once per 15 seconds while leaving service unchanged. L0 is their separate, equally long steady-path control. Use five documented event phases, 0, 3, 6, 9 and 12 seconds, one per pair, with the same phase across compared controllers. Event magnitudes are chosen stress inputs, not measured handover percentiles.

L3 measured-time phases are 0–90 seconds at 5/30 Mbps and 50 ms RTT; 90–180 seconds at 20/150 Mbps and 30 ms; 180–240 seconds at 1/10 Mbps and 80 ms; and 240–300 seconds back at 20/150 Mbps and 30 ms. Include a 500 ms bidirectional interruption at t=210 seconds: discard new arrivals and packets already queued or pending propagation on both paths, then resume ordinary service without resetting endpoint controller state. The resulting full-window result describes this combined scripted scenario; per-phase observations cannot establish isolated causal effects for each transition. This changes the network conditions without assuming a new connection or seamless real-world link migration. Report per-phase results and recovery time in addition to the full-window metric.

For L4, provisionally interpret the owner-supplied 4 × 4 Mbps connection as 4 Mbps each way. Introduce no background bulk flow for the first 100 measured seconds, one continuously demanding TCP CUBIC flow for the next 100, and three for the final 100. The example's 150 people do not imply 150 active equal-demand flows. L5's symmetric capacity and RTT are explicit assumptions. The 800/1,800 ms maritime sensitivity cases, higher source-upload variants, 1% random loss and alternative event durations remain unallocated candidates for reserve time or a later campaign; they are not silently considered measured.

For L6–L8, keep the competitor implementation fixed across the paired focal-controller runs. Use equal base RTTs first, then an explicitly unequal RTT phase, then competitor departure/re-entry. The competitor sends in the same direction as the focal bulk flow. It is continuously backlogged during warm-up and measured t=0–240 seconds, absent during t=240–270 and backlogged again during t=270–300. Set competitor RTT to 100 ms during warm-up and t=0–100, 25 ms during t=100–200, and 100 ms afterward while retaining the common bottleneck and queue. Reserve separate background-flow CPU resources. Pin the TCP kernel/CUBIC configuration and QUIC peer implementation. Aggregate and per-phase reports must identify carryover state; these phases are not independent steady-state experiments. Report each participant's useful delivery and control latency so a focal throughput increase cannot conceal displaced traffic.

## Counted runtime and allocation

The machine-readable [planning manifest](2026-09-23-bbrv3-acceptance-manifest.json) enumerates the rows and arithmetic. It is a counted planning inventory with parameter-contract pointers, not an emulator implementation or runnable experiment configuration.

| Group | Scenario rows | Individual runs | Sequential experiment time |
| --- | ---: | ---: | ---: |
| Linux, standard | 8 | 160 | 9 h 20 min |
| Linux, long | 9 | 180 | 18 h |
| macOS, two standard and one long | 3 | 60 | 4 h 20 min |
| Windows, two standard and one long | 3 | 60 | 4 h 20 min |
| Total | 23 | 460 | 36 h |

One standard row costs `2 workloads × 2 controllers × 5 repetitions × 210 seconds = 70 minutes`. One long row costs the same 20 runs times 360 seconds, or 120 minutes. Warm-up is already included. Setup/teardown, calibration, correctness checks, fixed-payload completion measurements and diagnostic reruns are excluded from the core total and allocated separately below.

The owner clarified that the ceiling counts each complete experiment once, including its endpoint/emulator participants, while concurrent experiments count separately. The following accepted allocation totals 48 hours. Time is a ceiling, not a requirement to exhaust it; failed runs still consume it. One experiment's clock includes its launch, warm-up, measurement and teardown. The 36-hour table accounts for warm-up and measurement; charge launch/teardown time to the reserve below.

| Allocation | Maximum experiment time |
| --- | ---: |
| Counted core comparisons above | 36 h |
| Fixed-payload completion checks | 2 h |
| Calibration and correctness validation | 4 h |
| Launch/teardown, targeted sensitivities and ambiguous-result investigation | 6 h |
| Total ceiling | 48 h |

Maintain one ledger across platforms. The allocations guide scheduling; do not drop a required case to fund an optional sensitivity. Run validation before comparisons and stop collecting comparative data on an uncalibrated path. At the total ceiling, report completed, invalid, inconclusive and unmeasured cases. Request a new budget decision for additional runtime. Parallel execution is permitted only with isolated endpoint, gateway, link and background-traffic resources; it reduces elapsed waiting, not charged experiment time.

Completion coverage uses reliable streams on Linux S1, S6, L1 and L5, plus S3 on macOS and Windows. Each case has five paired runs, yielding 60 additional individual runs. Use 16 MiB of useful payload except 256 KiB on L5; use fresh connections and measure receiver-local admission through verified receipt and stream EOF, with handshake timing separately identified. Set the L1 event phase to two seconds after transfer start so it can affect this shorter fixture. A run ends at completion or a 60-second experiment timeout; a timeout is a censored completion observation, not automatically a protocol failure. The 60 run ceilings consume at most one hour; the second allocated hour covers fixture setup/cleanup and clock checks. These fixed-payload cases are distinct from the duration-limited sustained fixtures, and neither models GridCast file publication.

The four-hour validation allowance covers targeted controller/transport correctness, native integration smoke checks on the priority platforms, applicable supported-platform build/test checks, and rate/RTT/queue/loss/ECN calibration. Pin the actual command list and its expected cost during preparation rather than claiming these checks have run. If required validation cannot fit or a supported target cannot be exercised, stop and report the coverage gap; compile-only evidence is labeled as such and cannot certify native behavior. Correctness and protocol compliance do not become advisory when the time budget is exhausted.

Preparation allowance: one working day, capped at eight hours of engineering/preparation effort, outside the 48 experiment-hours, to establish host availability, emulator feasibility, fixture/instrumentation needs and a runnable manifest. This is not a promise to build missing infrastructure in eight hours and does not include implementing BBRv3. At that limit, report what is ready and any prerequisite work requiring a revised estimate. No test fleet or service purchase is authorized by this allowance.

**Current budget amendment, 2026-09-25:** The operator approved 16 cumulative preparation hours and then 56 total experiment-hours, retaining the $100 cloud ceiling and every prior reservation. The original allocation above is historical; the [approved current forecast](2026-09-25-bbrv3-q1-campaign/budget-proposal.md) accounts for 53.218 hours before contingency, with 2.782 hours remaining under the new ceiling. The accepted case inventory is unchanged.

## Infrastructure observations and prerequisites

At the pinned revisions, `integrationtests/self/benchmark_test.go` contains a localhost stream-transfer benchmark with payload integrity checks; it is not a WAN campaign harness. `testutils/simnet` provides in-process packet connections, MTU handling and configurable per-packet downlink latency, including deterministic testing. Those pieces do not by themselves establish a calibrated rate-limited finite-queue/ECN emulator or a native performance topology. Existing historical audit collectors are frozen evidence, not implicitly approved maintained dependencies. The earlier packet-emission budget confirms the owner's four-core deployment preference but does not establish BBR performance or current host reservations.

Preparation must identify usable native hosts, reserve the accepted resources, select and pin the emulator implementation, verify the intended network path and directional rate/RTT/queue/loss/CE behavior, and validate the receiver counters and event timestamps. Capture the source graph, tool/kernel versions and manifest before measurements. Do not provision a large new fleet or acquire paid services silently. If calibration or host isolation fails, report a prerequisite gap rather than collecting mislabeled qualification data.

The policy, budget unit, concrete matrix, diagnostic impairment parameters, two-native-endpoint topology, preparation allowance and allocation above are settled by owner approval. The final-design ticket still owns the BBR baseline, ECN controller response, API and transport implementation contracts. An accepted evidence plan does not close those design decisions or authorize campaign execution in this planning-only map.
