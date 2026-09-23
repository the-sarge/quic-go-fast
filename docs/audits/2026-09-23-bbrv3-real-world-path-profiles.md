# Candidate real-world path profiles for BBRv3 evaluation

Research supporting [Choose acceptance criteria and an evidence budget](https://github.com/the-sarge/quic-go-fast/issues/557), under [Design opt-in BBRv3 for GridCast bulk transfers](https://github.com/the-sarge/quic-go-fast/issues/552). Sources inspected on 2026-09-23; repository base `7e33f5ebd940f2fd05146ea4c81343199c6d15e1`. This note records source evidence and proposed experiment design, not accepted numerical gates, an executed campaign, or measured quic-go-fast performance.

The required workload implementations remain inside quic-go-fast: reliable-stream bulk transfer and bulk DATAGRAM traffic alongside a reliable control stream, measured separately. GridCast transfers can provide supplemental real-world validation. The discussed 20% improvement is an aspirational target; the user will judge performance, resource, latency, and coexistence trade-offs after seeing the data. Correctness and protocol compliance remain hard gates.

## Reading the numbers

Provider access-speed disclosures are not measurements between arbitrary homes on different continents. Keep access upload, access download, end-to-end RTT, and time-varying impairments separate. For an upload, the sender's upload limit and receiver's download limit are candidate bottlenecks; both directions need explicit capacity and delay settings because acknowledgements and control traffic traverse the reverse path. All proposed experiment latencies below are RTT, not one-way delay.

Published measurements have a location, date, service plan, device, and methodology. They motivate scenarios; they do not establish a universal typical packet-loss percentage. No baseline IP loss rate is assigned to a named access technology without measured evidence. Zero injected random loss means a control condition, not a claim that the real network never loses packets. Queue overflow, externally injected random loss, burst loss, reordering, and complete interruptions must remain distinguishable in the report.

## Starlink upload

Starlink's network update reports approximately 200 Mbps median US peak-hour download in July 2025, a lower service tier delivering 100 Mbps down and 20 Mbps up in most US states and territories, and 25.7 ms median US peak-hour RTT in June 2025. These are operator measurements and service descriptions for that period, not guaranteed rates to a distant Internet destination. The same update says fewer than 1% of US latency measurements exceeded 55 ms; its router-based sampling should not be read as ruling out brief sub-second impairment events. [Starlink network update](https://starlink.com/nr/updates/network-update)

Earlier published specifications list 5–20 Mbps typical upload and 25–60 ms land latency, with 100+ ms possible in remote locations. Because the page does not give a clear measurement date and describes older service-plan names, retain it as a conservative service envelope rather than treating it as the current fleet median. [Starlink specifications](https://www.starlink.com/legal/documents/DOC-1431-92252-65)

Mohan and colleagues' WWW 2024 study observes synchronized 15-second reconfiguration intervals with sub-second delay and throughput variation. In a restricted-view experiment, the effects persisted while only one satellite could serve the terminal. Therefore, a 15-second impairment profile should be called a reconfiguration-inspired scenario; the evidence does not support labeling every event a physical satellite handover. These are historical measurements, not confirmation that every current terminal exhibits the same magnitude. [A Multifaceted Look at Starlink Performance, sections 3 and 6.2](https://hendrikcech.com/documents/mohan2024.pdf)

Hammer and colleagues' LEO-NET 2024 study found periodic upload delivery changes even with constant UDP transmission toward the dish. Only 22 of 897,899 RTT observations exceeded 200 ms in their measurements, and they associated periodic TCP reductions primarily with packet losses rather than long-delay timeouts. A model consisting only of periodic long delays would therefore omit an observed mechanism. This result is tied to their hardware and location, not a current global packet-loss estimate. [Starlink Performance through the Edge Router Lens, sections 3.2–3.4](https://stygianet.cs.purdue.edu/papers/starlink-leonet2024.pdf)

A public dataset from Garcia, Sundberg, and Brunstrom supplies uplink and downlink one-way delays from an unobstructed terminal in Karlstad, Sweden, during April 2025, with hardware timestamps sharing a clock. It is a candidate source for replay after checking how its probe load and hardware map to this campaign. The authors warn that missing sequence numbers can reflect timestamp readout failures, so the trace cannot directly establish packet loss from sequence gaps. The 5.9 GB data archive was not downloaded or analyzed for this note. [LEO-NET 2025 dataset](https://zenodo.org/records/16275284)

## LTE and 5G phone tethering

T-Mobile's current US disclosures explicitly distinguish hotspot/tethering from on-device and fixed home Internet service. They provide the following ranges, subject to compatible devices, plans, coverage, network load, and prioritization; they are national operator expectations rather than worldwide population estimates. The disclosure also notes that a displayed 5G connection can temporarily carry traffic only over 4G. [T-Mobile performance disclosures](https://www.t-mobile.com/home-internet/policies/internet-service/network-speed-performance-metrics.html)

| T-Mobile hotspot/tethering access | Download | Upload | Disclosed latency |
| --- | ---: | ---: | ---: |
| 4G | 13–57 Mbps | 2–12 Mbps | 26–46 ms |
| 5G | 74–327 Mbps | 6–30 Mbps | 17–32 ms |

Another operator gives a broader comparison: Verizon's February 2026 reporting period lists 163–622 Mbps down, 9–48 Mbps up, and 35–55 ms RTT for 5G Ultra Wideband mobile service; its combined 5G/4G on-device category for most plans lists 35–148 Mbps down, 5–33 Mbps up, and 43–85 ms RTT. Those are approximately 25th–75th percentile ranges, and neither category is a pure LTE tethering measurement. They demonstrate why the T-Mobile ranges should not become universal labels. [Verizon network performance](https://www.verizon.com/about/our-company/network-performance)

Ofcom's October 2024–March 2025 UK phone measurements provide evidence for weak-upload cases: 33% of 4G upload tests and 18% of 5G upload tests were below 2 Mbps, while 14% and 30%, respectively, reached at least 20 Mbps. These are phone measurements, not tethering tests, and they should not be conflated with a US provider's expected ranges. [Mobile Matters 2025, upload speeds](https://www.ofcom.org.uk/siteassets/resources/documents/research-and-data/telecoms-research/mobile-matters/2025/mobile-matters-2025.pdf?v=400314)

## International home-user paths

No single typical end-to-end home-user transfer speed was substantiated for UK–Los Angeles, Singapore–Los Angeles, or Australia–New York. A nearby speed-test server measures a different path from an intercontinental peer; source upload, destination download, route, peering, and concurrent traffic all need explicit treatment in the experiment design.

A first-party backbone measurement provides one geographic reference: HATSNET reports 127.5 ms London–Los Angeles RTT from 50 probes spaced 100 ms apart at 2026-08-16T04:07:28Z. This short backbone observation is not a residential distribution or a prediction for a particular UK home. [HATSNET London latency measurements](https://hatsnet.io/docs/network/latency/lon-london)

M1's April 2026 disclosure reports local wired testing for multi-gigabit plans during January–March 2026; it does not establish Singapore-to-Los-Angeles throughput. Australian plan examples such as 50/20 and 100/40 illustrate access asymmetry but are not a population median. These sources support separating the access-plan limit from international-path delay rather than assigning one speed to a country pair. [M1 typical speeds](https://www.m1.com.sg/support/faq/all-topics/tablets-mobile-broadband/typical-speeds-1), [Aussie Broadband access-rate explanation](https://www.aussiebroadband.com.au/blog/what-is-symmetrical-internet-and-why-does-your-business-need-it/)

## Constrained maritime connectivity

The owner-supplied ship connectivity example describes a 256 kbps FleetBroadband fallback plus a maritime VSAT link described as 4 × 4 Mbps, shared by 150 people, with reported latency from 0.8 to 1.8 seconds. Preserve this as a specific constrained-ship scenario, not a claim about the current maritime industry average. Provisionally interpret 4 × 4 as 4 Mbps in each direction, not 16 Mbps aggregate. The supplied passage does not define latency as one-way or round-trip, so choosing 1 second RTT and 0.8/1.8-second RTT sensitivity cases is an explicit modeling assumption pending better path evidence.

Inmarsat distinguishes shared Standard IP service from guaranteed Streaming IP service. The fallback therefore needs a named service mode; the supplied 256 kbps cap must not be generalized to every FleetBroadband connection or mistaken for guaranteed usable application throughput. [Inmarsat FleetBroadband](https://www.inmarsat.com/narrowband-services/fleetbroadband/)

Model the VSAT and fallback separately before considering a transition between them. The example does not establish simultaneous bonding, multipath, or seamless session survival when changing links. For the shared VSAT condition, vary competing offered traffic and measure how bulk transfer affects a small reliable control stream. Do not automatically divide 4 Mbps by 150: people are not all continuously active identical flows, and traffic scheduling matters. These are proposed experiment choices, not facts established by the quotation.

## Candidate controlled experiments

These values are proposed simulator inputs, not claimed real-world medians and not yet an accepted matrix. They provide a compact starting point for discussion; parameters and evidence budget still need to be fixed before collecting comparative results.

| Scenario | Sender upload / reverse capacity | Base RTT | Distinct purpose |
| --- | ---: | ---: | --- |
| Starlink-style upload | 20 / 100 Mbps | 40 ms | Asymmetric access with periodic reconfiguration-inspired events |
| LTE tethering | 5 / 30 Mbps | 50 ms | Modest upload capacity and cellular delay |
| 5G tethering | 20 / 150 Mbps | 30 ms | Higher downlink capacity with a still constrained uplink |
| Weak or congested cellular upload | 1 / 10 Mbps | 80 ms | Sensitivity to much lower usable upload capacity |
| UK–Los Angeles | 20 or 100 Mbps upload; reverse and receiver caps to specify | 150 ms | Intercontinental delay with two source-access limits |
| Singapore–Los Angeles | 100 or 1,000 Mbps upload; reverse and receiver caps to specify | 200 ms | Fast source access across a long path |
| Eastern Australia–New York | 20 or 100 Mbps upload; reverse and receiver caps to specify | 250 ms | Longer intercontinental path with asymmetric home access |
| Constrained ship VSAT | 4 / 4 Mbps | 1,000 ms; 800/1,800 ms sensitivity | Long RTT plus bounded competing traffic |
| Ship fallback | 0.256 / 0.256 Mbps, provisional symmetric caps | 1,000 ms, provisional | Extremely scarce capacity; separate from VSAT |

The international and maritime RTTs above are chosen model inputs, not derived present-day residential or maritime medians. The fallback reverse cap and RTT are provisional because the supplied example specifies its rate without sufficient directional or delay detail. International receiver and reverse-path capacity remain explicit open parameters; these rows are not runnable specifications yet.

For Starlink, retain a constant-path control and separate periodic delay-only and loss/interruption variants. A candidate sensitivity sweep is a 15-second event period with 50, 200, and 500 ms event durations. These durations are deliberately chosen stress inputs, not derived percentiles of satellite handovers. Define whether an event delays queued packets, reduces service capacity, or discards packets; do not treat those as interchangeable. Prefer an eligible measured trace for the eventual representative replay, retaining the synthetic variants for diagnosis.

For cellular paths, pair each constant-rate control with a scripted capacity reduction and recovery, and evaluate a separate short interruption. Record the shape, duration, and timing of each event. A stationary tether and a moving phone are different scenarios. If physical tethering is later measured, capture operator, radio mode, plan state, phone, Wi-Fi versus USB tether, endpoint, and whether the test was stationary; otherwise changes in hotspot policy or local Wi-Fi can masquerade as congestion-controller effects.

Use a clean control with zero externally injected random loss and, if selected, 0.1% and 1% independent-loss sensitivity tests. These percentages are synthetic stress settings, not sourced typical rates for any named technology or route. Keep these tests separate from queue-overflow and event-correlated burst-loss cases.

Use identical impairment schedules and workload demand for Reno and BBRv3. Report sustained unique receiver payload, reliable-transfer completion, DATAGRAM loss, control-stream latency, queue delay, and post-event recovery separately. A full-run average can conceal repeated stalls, so retain time series around dynamic events. Bound measurement duration to cover multiple events and enough round trips for each scenario; do not transfer a short-LAN warm-up blindly to a high-latency maritime path. A fixed 10 GiB payload would take about 93 hours even at an ideal 256 kbps, before overhead, so the fallback needs a bounded-duration sustained test and appropriately sized completion fixture.

## Accepted specification

The owner subsequently accepted the selected matrix, required versus supplemental scenario families, directional capacities, queue models, impairment schedules, repetitions and evidence budget in the [acceptance plan](2026-09-23-bbrv3-acceptance-plan.md). This research note preserves the broader candidate inputs and their uncertainty; the acceptance plan defines the selected campaign. All families can be emulated within quic-go-fast; physical access to each service is not implied. This note does not authorize purchasing connectivity, provisioning an external test fleet, or expanding required acceptance gates into other repositories.
