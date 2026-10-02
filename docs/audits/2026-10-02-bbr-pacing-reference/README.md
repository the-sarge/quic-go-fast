# Google BBR pacing granularity and burst behavior — reference research for #708

Primary-source research for [Choose the correction for BBRv3's remaining WAN-rate transport costs](https://github.com/the-sarge/quic-go-fast/issues/708), dated 2026-10-02. Local source is pinned at `55ea687d68c9c6f33785a1875374ec6a0d7570e8`. References are pinned to Linux mainline `v7.2` (`8d3ae59288f1e7d58d76558a6ee96d533bc5019f`), Google's BBRv3 tree branch `v3` (`90210de4b779d40496dee0b89081780eeddf2a60`, tag `bbrv3-2025-03-18`, a 6.13.7 base), QUICHE `main` (`f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6`), the IETF draft [draft-ietf-ccwg-bbr-06][d-06] and quic-go `v0.62.0` (`793f74d8e03368c5aded128af6f48d21dbb47f73`). This is a source reading, not a measurement. It informs the pacing part of #708; it does not choose a correction.

Every statement marked **verified** cites pinned source or a draft section. Statements marked **inference** are this note's reading of how the cited code behaves at run time; none was executed or traced.

## Short answer

None of the three Google references wakes the sender once per MTU-sized packet at 100 Mbit/s. All of them release roughly one millisecond of data per scheduling event, and all of them floor the aggregate at two segments.

- **Linux TCP (BBRv1 mainline and the BBRv3 tree)** sizes each TSO/GSO skb to about `pacing_rate >> 10` bytes (≈0.98 ms of data, at least 2 segments, at most 64 KB minus headers). It then paces **per skb**: the next departure time advances by `skb->len / pacing_rate`. At 100 Mbit/s that is one ≈8-segment (≈11.6 KB) aggregate about every 0.94 ms. Departure is enforced by either the `fq` qdisc or TCP's internal hrtimer. **Verified** sizing and per-skb accounting; the numeric example is **inference**.
- **QUICHE (`PacingSender`, used for both BBR and BBRv2)** computes an ideal send time per packet. However, `TimeUntilSend` sends immediately whenever that time is within `kAlarmGranularity` = **1 ms**. "Lumpy pacing" also allows up to `min(2, cwnd/4 ÷ 1460)` extra unpaced packets per pacing interval. Each send-alarm wakeup therefore drains about 1 ms of data. **Verified** constants and conditions; the per-wakeup batch size is **inference**.
- **draft-ietf-ccwg-bbr-06** defines `send_quantum = clamp(pacing_rate × 1 ms, 2·SMSS, 64 KB)`. It describes pacing as spacing **quanta** (aggregates), with `next_send_time += packet.size / pacing_rate`, where a "packet" may be an offload aggregate. **Verified.**

quic-go-fast's BBR pacer uses the same draft quantum as its credit **cap**, but its deadline is set by the **one-datagram** deficit. In steady state it therefore wakes about every `M / rate` (≈112–117 µs at 100 Mbit/s) and releases about one datagram per wakeup. That cadence matches none of the references at this rate. In Linux, a 1-segment aggregate occurs only below 1.2 Mbit/s (mainline BBR); a 2-segment floor applies elsewhere.

## Linux TCP BBR

### Send quantum (TSO/GSO aggregate size)

Mainline `v7.2` `net/ipv4/tcp_bbr.c` is still the BBRv1 algorithm (for example, `bbr_high_gain = 2885/1000` at [L155][l-bbr-highgain]). BBRv3 is not upstream in this tag.

**Verified, mainline v7.2:**

- `sk_pacing_shift` defaults to **10** for every socket ([`sock.c` L3800][l-sock-shift]). Therefore `pacing_rate >> 10` is the bytes for ≈1/1024 s ≈ 0.977 ms.
- Transmit-time sizing is `tcp_tso_autosize()` ([L2242–L2271][l-autosize]):
  - `bytes = pacing_rate >> sk_pacing_shift`.
  - Plus an RTT allowance, `sk_gso_max_size >> (min_rtt_us >> tcp_tso_rtt_log)`.
  - Capped at `sk_gso_max_size`.
  - Result: `max(bytes / mss, min_tso_segs)` segments.
- `tcp_tso_segs()` uses the congestion module's `min_tso_segs` hook when present and clamps to `sk_gso_max_segs` ([L2276–L2287][l-tso-segs]). Defaults are `tcp_min_tso_segs = 2` and `tcp_tso_rtt_log = 9` (512 µs) ([`tcp_ipv4.c` L3496–L3497][l-sysctl]).
- The comment above `tcp_tso_autosize()` explains the RTT allowance: it halves every 512 µs of `min_rtt`, so it falls below 1500 bytes by about 3 ms ([L2242–L2255][l-autosize]). At a 100 ms path it contributes nothing.
- BBR overrides only the minimum: `bbr_min_tso_segs()` returns **1** when `sk_pacing_rate < bbr_min_tso_rate >> 3`, otherwise **2**. `bbr_min_tso_rate = 1200000` bits/s, that is 150,000 bytes/s ([L140][l-bbr-mintso-rate], [L299–L303][l-bbr-mintso]).
- `bbr_tso_segs_goal()` is a cwnd-budget estimate ("Sort of tcp_tso_autosize() but ignoring driver provided sk_gso_max_size"). It computes `min(pacing_rate >> shift, GSO_LEGACY_MAX_SIZE - 1 - MAX_TCP_HEADER) / mss_cache`, raises it to at least `bbr_min_tso_segs`, and caps it at `0x7F` segments ([L305–L319][l-bbr-goal]). `GSO_LEGACY_MAX_SIZE` is **65536** ([`netdevice.h` L2474][l-gso-legacy]).
- `bbr_quantization_budget()` adds `3 * bbr_tso_segs_goal` to the cwnd target. This covers one skb in the qdisc, one in TSO/GSO and one in the receiver's LRO/GRO/delayed-ACK engine ([L385–L411][l-bbr-qbudget]).
- BBR paces at 1% below the estimate (`bbr_pacing_margin_percent = 1`, [L148][l-bbr-margin], applied at [L245–L254][l-bbr-rate]). It requests pacing with `SK_PACING_NEEDED` ([L1079][l-bbr-needed]).

**Verified, BBRv3 tree (`google/bbr` `v3`):**

- BBR supplies its own `tso_segs` hook, and `tcp_tso_segs()` calls it in place of `tcp_tso_autosize` ([`tcp_output.c` L2065–L2075][b-tso-segs]; hook registered at [`tcp_bbr.c` L2354][b-hook]).
- `bbr_tso_segs_generic()` ([L476–L504][b-generic]) works as follows:
  - `bytes = pacing_rate >> sk_pacing_shift`.
  - Plus `GSO_LEGACY_MAX_SIZE >> (min_rtt_us >> bbr_tso_rtt_shift)`, with `bbr_tso_rtt_shift = 9` ([L195–L200][b-rttshift]).
  - Capped at `gso_max_size - 1 - MAX_TCP_HEADER`.
  - `segs = max(bytes / mss, sysctl_tcp_min_tso_segs)`, where the sysctl default is 2 ([`tcp_ipv4.c` L3489–L3490][b-sysctl]).
- There is **no** 1-segment low-rate rule and **no** `0x7F` cap in v3. The transmit path uses `sk_gso_max_size`; the cwnd goal uses `GSO_LEGACY_MAX_SIZE` ([L506–L518][b-generic]).
- `bbr_quantization_budget()` uses `max(cwnd, 3 * tso_segs_goal)`, not `+=` ([L607–L622][b-qbudget]). The pacing margin is still 1% ([L208][b-margin]). `sk_pacing_shift` still defaults to 10 ([`sock.c` L3615][b-sock-shift]).

**Inference, 100 Mbit/s and 100 ms, MSS 1448:**

- Cruise pacing rate ≈ 12.375 MB/s, so `rate >> 10` ≈ 12,085 B, which is **8 segments** (≈11.6 KB) per skb in both trees.
- The 64 KB ceiling is reached at about 537 Mbit/s, where `rate >> 10 ≥ 65536`.
- The 2-segment floor governs below about 23.7 Mbit/s. There, aggregates are 2 segments and are spaced more than 1 ms apart.
- At sub-3 ms RTT, the `min_rtt` allowance enlarges aggregates toward 64 KB even at modest rates. This matters when interpreting loopback runs; it is irrelevant to the 100 ms path.

### How pacing releases data

**Verified (mainline v7.2; the BBRv3 tree has the same code at the cited lines):**

- **Departure time per skb (EDT).**
  - `__tcp_transmit_skb` stamps each skb with `tcp_wstamp_ns = max(tcp_wstamp_ns, tcp_clock_cache)` ([L1553–L1555][l-wstamp]).
  - `tcp_update_skb_after_send()` then advances `tcp_wstamp_ns += skb->len * NSEC_PER_SEC / pacing_rate`. It subtracts up to half of that interval as credit for "OS jitter" (the lateness between the previous stamp and this one) ([L1446–L1468][l-after-send]; BBRv3 tree [L1254–L1276][b-after-send]).
  - The interval is computed from `skb->len`, the whole TSO/GSO aggregate, so pacing is **per aggregate, not per MTU packet**.
- **Initial unpaced burst.** The same function skips pacing until `tp->data_segs_out >= 10` ("Original sch_fq does not pace first 10 MSS") ([L1453–L1458][l-after-send]).
- **TCP internal pacing (no fq).** `tcp_write_xmit()` computes `max_segs = tcp_tso_segs()` once, and each loop iteration first calls `tcp_pacing_check()` ([L2992][l-xmit-maxsegs], [L3006][l-xmit-check], [L3017][l-xmit-quota]). `tcp_pacing_check()` returns false if `tcp_wstamp_ns <= tcp_clock_cache`. Otherwise it arms the per-socket hrtimer `pacing_timer` at absolute `tcp_wstamp_ns` (`HRTIMER_MODE_ABS_PINNED_SOFT`) and stops sending ([L2815–L2832][l-pacing-check]). `tcp_pace_kick()` re-enters the TSQ handler when the timer fires ([L1435–L1444][l-pace-kick]). No explicit slack range is passed.
- **fq qdisc with EDT skbs.**
  - Enqueue copies `skb->tstamp` into `time_to_send`. Timestamps beyond `horizon` (default **10 s**) are dropped or capped ([L608–L626][l-fq-enq]; [L1250][l-fq-init]).
  - Dequeue throttles a flow while `now + offload_horizon < max(time_to_send, f->time_next_packet)` ([L759–L770][l-fq-deq]).
  - fq's own rate computation runs only when the skb has **no** timestamp, or when `flow_max_rate` is set ([L795–L834][l-fq-rate]). For TCP EDT skbs, the qdisc enforces TCP's per-aggregate stamps.
  - When fq is installed, socket pacing status becomes `SK_PACING_FQ`, so TCP's internal timer is not used ([L397–L401][l-fq-fast]).
- **fq timers and defaults.**
  - The watchdog is armed with `qdisc_watchdog_schedule_range_ns(..., timer_slack)`; `timer_slack` defaults to **10 µs** ([L744–L747][l-fq-watchdog], [L1248][l-fq-init]).
  - `quantum = 2 * psched_mtu` and `initial_quantum = 10 * psched_mtu` are DRR credits ([L1229–L1230][l-fq-init]).
  - For **un-timestamped** skbs, rate pacing charges `max(plen, quantum)` and inserts no delay while the flow still has positive credit. Below `low_rate_threshold` = 550 kbit/s, credit is zeroed so every packet is paced ([L803–L812][l-fq-rate], [L1246][l-fq-init]).
  - fq also discounts up to half an interval for timer lateness ([L827–L833][l-fq-rate]).

**Inference:**

- With BBR at 100 Mbit/s, Linux wakes (fq watchdog or TCP hrtimer) about once per ≈11.6 KB aggregate, roughly every 0.94 ms, and emits that aggregate as one TSO/GSO burst.
- The 64 KB ceiling bounds the burst; above about 537 Mbit/s the wake interval shrinks below 1 ms (for example, ≈52 µs at 10 Gbit/s).
- Late wakeups can shorten the next interval by up to half, but they cannot accumulate unbounded catch-up.

## QUICHE

**Verified (`google/quiche` `f9e75eb8`):**

- **Granularity.** `kAlarmGranularity = 1 ms` ([`quic_constants.h` L309–L311][q-gran]).
  - `PacingSender::TimeUntilSend()` returns zero, meaning send now, when `ideal_next_packet_send_time_ <= now + kAlarmGranularity`. Otherwise it returns the remaining delay ([L168–L177][q-tus]).
  - `QuicConnection::CanWrite()` arms the send alarm with `send_alarm().Update(now + delay, kAlarmGranularity)` ([L3505–L3521][q-canwrite]). `QuicAlarm::Update` ignores changes smaller than the granularity ([`quic_alarm.cc` L55–L78][q-alarm]).
- **Per-packet ideal time.**
  - `OnPacketSent()` computes `delay = PacingRate(bytes_in_flight + bytes).TransferTime(bytes)` for each packet.
  - While pacing-limited, it accumulates `ideal_next_packet_send_time_ += delay` ("Make up for lost time"). Otherwise it uses `max(ideal + delay, sent_time + delay)` ([L92–L126][q-sent]).
  - `OnApplicationLimited()` stops the catch-up ([L129–L132][q-applimited]).
- **Lumpy pacing.** When `lumpy_tokens_` is exhausted or the sender was not pacing-limited, the sender resets `lumpy_tokens_ = max(1, min(quic_lumpy_pacing_size, cwnd × quic_lumpy_pacing_cwnd_fraction / kDefaultTCPMSS))`. It forces 1 when the bandwidth estimate is below `quic_lumpy_pacing_min_bandwidth_kbps`, or when `bytes_in_flight + bytes >= cwnd` (cwnd-limited) ([L96–L117][q-lumpy]). `TimeUntilSend` returns zero while `lumpy_tokens_ > 0` ([L150–L166][q-tus]).
  - Defaults are `quic_lumpy_pacing_size = 2`, `quic_lumpy_pacing_cwnd_fraction = 0.25` and `quic_lumpy_pacing_min_bandwidth_kbps = 1200` ([`quiche_protocol_flags_list.h` L80–L94][q-flags-lumpy]).
  - `kDefaultTCPMSS = 1460` ([L66][q-mss]).
- **Initial and quiescence bursts.**
  - `kInitialUnpacedBurst = 10` packets ([L17–L19][q-burst]).
  - When a retransmittable packet is sent with `bytes_in_flight == 0` and the sender is not in recovery, `burst_tokens_ = min(initial_burst_size_, cwnd / kDefaultTCPMSS)`. In the default path `TimeUntilSend` also returns zero whenever `bytes_in_flight == 0` ([L70–L90][q-quiescence], [L158–L165][q-tus]).
  - Burst tokens are cleared on loss ([L48–L51][q-loss]).
  - The non-initial burst is removed only by the `RNIB` connection option ([`quic_sent_packet_manager.cc` L235–L237][q-rnib]; [`crypto_protocol.h` L330][q-tags]) or by reloadable flag `quic_pacing_remove_non_initial_burst`, which defaults to false ([`quiche_feature_flags_list.h` L58][q-ff]). Open-source defaults take the `external_value` column ([`quiche_flags_impl.cc` L7–L8][q-flagimpl]).
  - `quic_conservative_bursts` (default false, [L27][q-ff]) would set burst tokens to `kConservativeUnpacedBurst = 2` on cwnd bootstrapping ([`quic_sent_packet_manager.cc` L67–L70, L326–L330][q-conservative]).
- **Pacing applies to all controllers.** `QuicSentPacketManager` routes `TimeUntilSend` and `GetNextReleaseTime` through the single `PacingSender` unless `quic_disable_pacing_for_perf_tests` is set ([L206][q-usingpacing], [L1082–L1096][q-spm-tus], [L1562–L1568][q-spm-release]). Neither `bbr_sender.cc` nor the BBRv2 files define a send quantum.
  - BBRv1 pacing gains are `{1.25, 0.75, 1, 1, 1, 1, 1, 1}` ([L43–L52][q-bbr1]).
  - BBRv2 sets `pacing_rate_ = pacing_gain × BandwidthEstimate()` ([L367–L400][q-bbr2-rate]), with ProbeBW gains 1.25 / 0.91 / 1.0 ([`bbr2_misc.h` L153–L155][q-bbr2-gains]).
  - A search of `bbr_sender.cc`, `bbr2_sender.cc` and `bbr2_misc.{h,cc}` found no 1% pacing margin. This is absence of evidence in those files only.
- **Offload coupling.**
  - `QuicGsoBatchWriter::CanBatch` limits a batch to `MaxSegments` = **45** (16 for `gso_size <= 2`) and `kMaxGsoPacketSize = 65535 - 40 - 8` = **65,487** bytes ([L63–L111][q-canbatch], [`quic_gso_batch_writer.h` L52–L58][q-maxseg], [`quic_constants.h` L57][q-gsomax]).
  - A packet can join a batch only if it can burst (no release delay, or `allow_burst` from burst/lumpy tokens) or has the same release time.
  - Release-time (SO_TXTIME) support requires restart flag `quic_support_release_time_for_gso`, which defaults to false ([L72][q-ff]; [L41–L51][q-gso-ctor]). The `NPCO` connection option disables it ([`quic_connection.cc` L646–L648][q-npco]).
  - When enabled, `CanWrite` sends immediately if the pacing delay is at most `release_time_into_future_ = max(1 ms, min(quic_max_pace_time_into_future_ms = 10 ms, srtt × 0.125))`. It stamps the packet with the ideal release time ([L3512–L3516][q-canwrite], [L3527–L3542][q-calc-release], [L5967–L5980][q-update-release], [L114][q-minrelease]; flags at [`quiche_protocol_flags_list.h` L96–L103][q-flags-release]).

**Inference:**

- Once a send alarm fires at about `ideal`, the connection keeps writing while `ideal + k·delay <= now + 1 ms`. One wakeup therefore releases about 1 ms of data, plus up to two lumpy packets: at 100 Mbit/s that is ≈12.5 KB, or ≈9 packets of 1,350–1,460 B. The next alarm lands about 1 ms later.
- Unlike Linux, this batch is **not capped at 64 KB**. At high rates 1 ms is larger than 64 KB, and the catch-up path can add the alarm's lateness on top. Only cwnd and the GSO writer's 45-segment / 65,487-byte batch limit bound a single write.
- With release time enabled, QUICHE hands the kernel up to 10 ms of future-stamped packets per wakeup on a 100 ms path, and the qdisc enforces departure.

## IETF draft-ietf-ccwg-bbr-06

**Verified:**

- `C.send_quantum` is "the maximum size of a data aggregate scheduled and transmitted together as a unit" ([§2.4][d-2.4]).
- `SetSendQuantum()` (renamed from `BBRSetSendQuantum()` in draft-05) is `pacing_rate * 1ms`, then `min(·, 64 KBytes)`, then `max(·, 2 * SMSS)`. It runs on each ACK ([§5.6.3][d-5.6.3]).
- §5.6.3 says an implementation "MAY use alternate approaches" to choose the quantum, but "SHOULD attempt to use the smallest feasible quanta".
- Pacing "enforces a maximum rate at which BBR schedules quanta of packets for transmission" by "maintaining inter-quantum spacing". The pseudocode is `next_send_time = max(Now(), next_send_time); P.send_time = next_send_time; next_send_time += packet.size / pacing_rate` ([§5.6.2][d-5.6.2]).
- The draft says that with TSO/GSO the per-packet state SHOULD be tracked per aggregate ([§4.1.3.1][d-4.1.3.1]). Elsewhere it calls an offload burst a "packet" in the pseudocode ([§5.5.10.2][d-5.5.10.2]).
- The pacing rate carries a 1% margin (`PacingMarginPercent`, [§5.6.2][d-5.6.2]).
- The offload budget is `3 * send_quantum` for TCP and `send_quantum` for QUIC. QUIC adds the data scheduled by any pacing offload and may add a delayed-ACK allowance ([§5.5.8][d-5.5.8]).

**Inference:** the draft's pacing model is departure-time debt per aggregate. A sender that forms quantum-sized aggregates gets one scheduling event per quantum; one that forms single-datagram "packets" gets one per datagram. The draft does not mandate either, and it says nothing about OS timer precision or a minimum delay.

## quic-go (upstream) and quic-go-fast Reno pacer

**Verified:**

- quic-go-fast's `internal/congestion/pacer.go` and the `MinPacingDelay` / `TimerGranularity` constants are byte-identical to upstream `v0.62.0` (`git diff v0.62.0` is empty for `pacer.go`; `params.go` differs only in unrelated additions).
- The pacer paces at `bw * 5/4` ([L21–L36][g-pacer-new]).
- Its burst cap is `max(rate·(MinPacingDelay + TimerGranularity), 10·maxDatagramSize)`, that is 2 ms of 1.25× rate or ten datagrams ([L11][g-pacer-const], [L64–L69][g-pacer-burst]).
- `TimeUntilSend` returns `lastSentTime + max(MinPacingDelay, deficit/rate)` with `MinPacingDelay = 1 ms` ([L90–L106][g-pacer-tus], [`params.go` L125–L128][g-params]). The constant's comment gives the intended effect: at a 200 µs packet interval, "we would send 5 packets at once, wait for 1ms".
- Upstream `master` (`7c3a98ee`) restructured the API so that the rate is passed in, with the 5/4 gain applied in `cubicSender.pacingRate()` and 2× in slow start. It kept the 1 ms minimum and the same burst cap ([`pacer.go`][g-master-pacer], [`cubic_sender.go` L287–L294][g-master-rate]).

**Inference:** this is QUICHE's 1 ms granularity expressed as a floor rather than an early-send window. quic-go has no BBR, so its upstream offers no BBR-specific pacing precedent.

## Comparison

At 100 Mbit/s with ~1.4 KB packets unless stated. "Inference" rows apply this note's arithmetic to verified expressions.

| Implementation | Wakeup granularity | Max burst per wakeup | Min-delay / early-send tolerance | Initial burst | Offload coupling |
| --- | --- | --- | --- | --- | --- |
| Linux mainline v7.2 TCP BBR (v1) | One per TSO/GSO skb; interval `skb->len / pacing_rate` (≈0.94 ms; inference) | One skb: `max(pacing_rate >> 10, …)` bytes, ≥2 segs (1 below 1.2 Mbit/s), ≤ `sk_gso_max_size` (≈8 segs; inference) | No minimum delay; up to ½ interval of lateness credit (TCP and fq); fq watchdog slack 10 µs | First 10 segments unpaced; fq `initial_quantum` 10 MTU of DRR credit | Quantum is the TSO/GSO skb; cwnd += 3 × quantum |
| google/bbr v3 TCP BBRv3 | Same per-skb EDT | `pacing_rate >> 10` + `64 KB >> (min_rtt/512 µs)`, ≥ `tcp_min_tso_segs` (2), ≤ `gso_max_size − 1 − MAX_TCP_HEADER` | Same as mainline | Same | Quantum is the TSO/GSO skb; cwnd ≥ 3 × quantum |
| QUICHE `PacingSender` (BBR, BBRv2, Cubic) | Send alarm about every 1 ms (inference from 1 ms tolerance) | ≈1 ms of data + ≤2 lumpy packets (inference); not 64 KB capped; GSO batch ≤45 segs / 65,487 B | Sends if ideal time ≤ now + 1 ms; alarm updates ignored if < 1 ms change; catch-up while pacing-limited | 10 packets (≤ cwnd/1460), re-granted on quiescence unless `RNIB`/flag; cleared on loss | Optional SO_TXTIME: up to max(1 ms, min(10 ms, srtt/8)) scheduled ahead; flag default off |
| draft-ietf-ccwg-bbr-06 | One per aggregate (`next_send_time += size / rate`) | `send_quantum` = clamp(rate × 1 ms, 2·SMSS, 64 KB) | None specified | None specified | Offload budget 3 × quantum (TCP), 1 × quantum + pacing-offload data (QUIC) |
| quic-go v0.62.0 / quic-go-fast Reno pacer | ≥1 ms after last send | max(2 ms × 1.25 × bw, 10 datagrams) (≈31 KB) | `MinPacingDelay` 1 ms floor | Full burst cap (≥10 datagrams) | None in pacer |
| quic-go-fast `bbrSendPolicy` (D08) | Per-datagram deficit (≈112–117 µs) | Credit cap Q = max(2M, min(64 KiB, rate × 1 ms)) (≈12.4 KB); steady state ≈1 datagram | None (exact deficit, by design) | Q (tokens start at Q) | GSO group ≤ min(budget, allowance, max large buffer); 2Q pending |

The last row is from the local source: [`bbr_send_policy.go` L23–L38, L62–L67][f-policy] and [`packet_emission_bbr.go` L136–L146][f-emission]. The design text is [`bbrv3.md` L141–L147][f-design]; D08 is at [L225][f-d08].

## Implications for #708

The issue frames the pacing question as "whether, and how, to change pacing wakeup granularity", treated as a contract change. In reference terms, D08 combines two separable choices:

- **The quantum is the draft's.** Q matches §5.6.3, including the 2·SMSS floor (the same floor as Linux's `tcp_min_tso_segs = 2`) and the 64 KB ceiling (≈ Linux's `GSO_LEGACY_MAX_SIZE` bound). Rejecting Reno's 5/4 gain and ten-packet minimum is also consistent with Linux and the draft, neither of which has them. QUICHE has a ten-packet *initial* burst, but no ongoing 10-packet floor.
- **Deadline = one-datagram deficit has no reference precedent at this rate.** Linux charges whole aggregates sized ≈ rate × 0.98 ms and schedules the next aggregate after its transfer time. QUICHE releases everything due within 1 ms. The draft spaces "quanta". In none of them does a sender with ample data and a 12 KB quantum wake every ≈115 µs to send one packet. The design's own text anticipated this: "At high rates the 64KiB quantum needs sub-millisecond opportunities". The references use sub-millisecond intervals only once the 64 KB ceiling binds (above about 0.5 Gbit/s), not at 100 Mbit/s.

Options, from closest-to-reference to novel. These are options only; the operator decides.

1. **Per-quantum release (follows Linux TCP and the draft's §5.6.2 model).** Charge and schedule in aggregates up to Q: either wake when the full Q of credit is available (the diagnostic), or send a ≤Q group immediately and set the next departure to `group_bytes / rate` (EDT debt, Linux-style). Linux additionally forgives up to half an interval of lateness. Either form cuts wakeups by about Q/M (≈8–9× at 100 Mbit/s) and matches Linux's burst size at this rate almost exactly (≈12 KB vs ≈11.6 KB). It changes D08's "exact deficit" contract and the two tests that pin it. One difference matters at low rates: Linux's aggregate is never smaller than 2 segments, and Q has the same floor, so the per-datagram cadence would disappear entirely.
2. **QUICHE-style 1 ms early-send tolerance.** Keep per-datagram deadlines, but send whenever the deadline is within 1 ms. Reference-backed (QUICHE, for BBR and BBRv2), and the same idea as quic-go's Reno `MinPacingDelay` lineage that D08 explicitly rejected. Unlike QUICHE, quic-go-fast would keep Q as a hard cap; at rates where rate × 1 ms exceeds 64 KiB the cap would bind, which QUICHE does not do. Reversing D08 means reversing a recorded rejection, not just tuning.
3. **Lumpy pacing alone (QUICHE).** Allow `min(2, cwnd/4 ÷ 1460)` unpaced packets per interval above 1.2 Mbit/s when not cwnd-limited. This is reference-backed, but it only roughly halves or thirds wakeups. In QUICHE it is layered on the 1 ms tolerance, not used instead of it.
4. **A fixed fraction of Q (for example Q/2) or a tuned minimum delay below 1 ms.** No reference does this. It would be a novel profile choice requiring its own evidence, per the design's rule that changes need "evidence and a recorded profile change".
5. **Keep D08 and accept the cost.** This is consistent with the draft's "smallest feasible quanta" SHOULD in spirit. It is not what any examined reference implementation does at 100 Mbit/s.

**Burst size and receiver memory.**

- **What the references say (verified/inference above).** At 100 Mbit/s every reference emits bursts of ≈1 ms of data, ≈12 KB, so a Q-sized burst is reference-typical, not aggressive. Linux budgets cwnd explicitly for one aggregate sitting in the receiver's LRO/GRO/delayed-ACK engine, and the draft budgets one quantum for QUIC. The references treat a quantum per RTT-scale ACK cycle as expected receiver load.
- **What they do not say.** None of the references addresses a user-space QUIC receiver's heap. The ≈3× STREAM receiver heap-peak increase was observed on macOS loopback through a user-space relay, with no GRO/receive offload in play. Nothing in these sources explains it or predicts it.
- **Inference only:** a 12 KB burst arriving at once may change how many packets a receive loop or relay drains per wakeup, and therefore how much it buffers. Whether that is the mechanism, and whether it reproduces on Linux with GRO or on a real path, is unmeasured. The reference evidence supports the burst size being acceptable *for the network*; it does not establish that the receiver-memory effect is benign or an artifact.

## Not verified

- The effective precision and slack of TCP's internal `pacing_timer` (no explicit slack argument is passed; kernel hrtimer slack defaults were not inspected), and real fq watchdog lateness. No kernel was run.
- QUICHE's per-wakeup batch size and send-alarm cadence. These are inferred from `PacingSender` and `CanWrite`; `QuicConnection::OnCanWrite`'s write loop and platform alarm implementations were not traced.
- Whether Google production (GFE, Chromium) runs with the open-source flag defaults: release time, lumpy sizes, `RNIB`, `quic_conservative_bursts`. Only the `external_value` defaults were read.
- The absence of a pacing margin or a send quantum in QUICHE BBR/BBRv2. Only the named files were searched.
- The newer `google/bbr` branch `bbr-v3-2026-09-16-01` (`674859761ded9f32690c7bdaf22ef585531452ed`). Its `tcp_bbr.c` is a 1,200-line file without `bbr_tso_segs_generic`, which looks like the v1 file, but the branch was not examined further. This note relies on `v3`.
- The numeric value of `MAX_TCP_HEADER` (configuration-dependent) and device `sk_gso_max_size` (BIG TCP can exceed 64 KB).
- Any reference measurement of sender CPU versus pacing granularity. The references justify their quanta by CPU cost in comments and prose, not with cited data.

## Sources

Linux mainline `v7.2`, commit `8d3ae59288f1e7d58d76558a6ee96d533bc5019f`:

- [`net/ipv4/tcp_bbr.c`](https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c)
- [`net/ipv4/tcp_output.c`](https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c)
- [`net/sched/sch_fq.c`](https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c)
- [`net/core/sock.c`](https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/core/sock.c)
- [`net/ipv4/tcp_ipv4.c`](https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_ipv4.c)
- [`include/linux/netdevice.h`](https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/include/linux/netdevice.h)

Google BBRv3 tree, branch `v3` / tag `bbrv3-2025-03-18`, commit `90210de4b779d40496dee0b89081780eeddf2a60`:

- [`net/ipv4/tcp_bbr.c`](https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c)
- [`net/ipv4/tcp_output.c`](https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_output.c)
- [`net/core/sock.c`](https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/core/sock.c)
- [`net/ipv4/tcp_ipv4.c`](https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_ipv4.c)

QUICHE, commit `f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6`:

- [`congestion_control/pacing_sender.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc)
- [`congestion_control/pacing_sender.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.h)
- [`quic_sent_packet_manager.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_sent_packet_manager.cc)
- [`quic_connection.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_connection.cc)
- [`quic_alarm.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_alarm.cc)
- [`quic_constants.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_constants.h)
- [`quiche_protocol_flags_list.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/quiche_protocol_flags_list.h)
- [`quiche_feature_flags_list.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/quiche_feature_flags_list.h)
- [`quiche_flags_impl.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/platform/default/quiche_platform_impl/quiche_flags_impl.cc)
- [`crypto_protocol.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/crypto/crypto_protocol.h)
- [`batch_writer/quic_gso_batch_writer.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/batch_writer/quic_gso_batch_writer.cc)
- [`batch_writer/quic_gso_batch_writer.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/batch_writer/quic_gso_batch_writer.h)
- [`congestion_control/bbr_sender.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/bbr_sender.cc)
- [`congestion_control/bbr2_sender.cc`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/bbr2_sender.cc)
- [`congestion_control/bbr2_misc.h`](https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/bbr2_misc.h)

IETF:

- [draft-ietf-ccwg-bbr-06][d-06] (6 July 2026), versioned archive URL; plain text at <https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.txt>.
- [draft-ietf-ccwg-bbr-05](https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-05.txt), compared only for the `BBRSetSendQuantum` → `SetSendQuantum` rename.

quic-go:

- `v0.62.0` (`793f74d8e03368c5aded128af6f48d21dbb47f73`): [`internal/congestion/pacer.go`](https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/congestion/pacer.go) and [`internal/protocol/params.go`](https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/protocol/params.go).
- `master` (`7c3a98eeb4144e77876ba77c9292da9b5b27a0e1`): [`pacer.go`][g-master-pacer] and [`cubic_sender.go`][g-master-rate].

quic-go-fast, `55ea687d68c9c6f33785a1875374ec6a0d7570e8`:

- [`bbr_send_policy.go`][f-policy]
- [`packet_emission_bbr.go`][f-emission]
- [`docs/designs/bbrv3.md`][f-design]

[l-bbr-highgain]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L155
[l-bbr-mintso-rate]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L139-L140
[l-bbr-margin]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L142-L148
[l-bbr-rate]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L245-L254
[l-bbr-mintso]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L299-L303
[l-bbr-goal]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L305-L319
[l-bbr-qbudget]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L385-L411
[l-bbr-needed]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_bbr.c#L1079
[l-sock-shift]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/core/sock.c#L3800
[l-sysctl]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_ipv4.c#L3496-L3497
[l-gso-legacy]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/include/linux/netdevice.h#L2474
[l-autosize]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L2242-L2271
[l-tso-segs]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L2276-L2287
[l-pace-kick]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L1435-L1444
[l-after-send]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L1446-L1468
[l-wstamp]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L1553-L1555
[l-pacing-check]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L2815-L2832
[l-xmit-maxsegs]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L2992
[l-xmit-check]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L3006-L3007
[l-xmit-quota]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/tcp_output.c#L3017
[l-fq-fast]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c#L397-L401
[l-fq-enq]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c#L608-L626
[l-fq-watchdog]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c#L744-L747
[l-fq-deq]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c#L759-L770
[l-fq-rate]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c#L795-L834
[l-fq-init]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/sched/sch_fq.c#L1221-L1250
[b-rttshift]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L195-L200
[b-margin]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L202-L208
[b-generic]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L476-L518
[b-qbudget]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L607-L622
[b-hook]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_bbr.c#L2354
[b-tso-segs]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_output.c#L2065-L2075
[b-after-send]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_output.c#L1254-L1276
[b-sock-shift]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/core/sock.c#L3615
[b-sysctl]: https://github.com/google/bbr/blob/90210de4b779d40496dee0b89081780eeddf2a60/net/ipv4/tcp_ipv4.c#L3489-L3490
[q-burst]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L17-L19
[q-loss]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L48-L51
[q-quiescence]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L70-L90
[q-sent]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L92-L126
[q-lumpy]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L96-L117
[q-applimited]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L129-L132
[q-tus]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/pacing_sender.cc#L141-L178
[q-gran]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_constants.h#L309-L311
[q-mss]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_constants.h#L66
[q-gsomax]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_constants.h#L57
[q-flags-lumpy]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/quiche_protocol_flags_list.h#L80-L94
[q-flags-release]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/quiche_protocol_flags_list.h#L96-L103
[q-ff]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/quiche_feature_flags_list.h#L27-L72
[q-flagimpl]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/common/platform/default/quiche_platform_impl/quiche_flags_impl.cc#L7-L8
[q-tags]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/crypto/crypto_protocol.h#L329-L330
[q-conservative]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_sent_packet_manager.cc#L67-L70
[q-usingpacing]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_sent_packet_manager.cc#L206
[q-rnib]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_sent_packet_manager.cc#L235-L237
[q-spm-tus]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_sent_packet_manager.cc#L1082-L1096
[q-spm-release]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_sent_packet_manager.cc#L1562-L1568
[q-minrelease]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_connection.cc#L114
[q-npco]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_connection.cc#L646-L652
[q-canwrite]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_connection.cc#L3500-L3524
[q-calc-release]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_connection.cc#L3527-L3542
[q-update-release]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_connection.cc#L5967-L5980
[q-alarm]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/quic_alarm.cc#L55-L78
[q-gso-ctor]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/batch_writer/quic_gso_batch_writer.cc#L41-L51
[q-canbatch]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/batch_writer/quic_gso_batch_writer.cc#L63-L111
[q-maxseg]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/batch_writer/quic_gso_batch_writer.h#L52-L58
[q-bbr1]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/bbr_sender.cc#L43-L52
[q-bbr2-rate]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/bbr2_sender.cc#L367-L400
[q-bbr2-gains]: https://github.com/google/quiche/blob/f9e75eb8ab1de3ea1c422deb561abbaa84ef2df6/quiche/quic/core/congestion_control/bbr2_misc.h#L153-L155
[d-06]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html
[d-2.4]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-2.4
[d-4.1.3.1]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-4.1.3.1
[d-5.5.8]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-5.5.8
[d-5.5.10.2]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-5.5.10.2
[d-5.6.2]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-5.6.2
[d-5.6.3]: https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-5.6.3
[g-pacer-const]: https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/congestion/pacer.go#L11
[g-pacer-new]: https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/congestion/pacer.go#L21-L36
[g-pacer-burst]: https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/congestion/pacer.go#L64-L69
[g-pacer-tus]: https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/congestion/pacer.go#L90-L106
[g-params]: https://github.com/quic-go/quic-go/blob/793f74d8e03368c5aded128af6f48d21dbb47f73/internal/protocol/params.go#L125-L128
[g-master-pacer]: https://github.com/quic-go/quic-go/blob/7c3a98eeb4144e77876ba77c9292da9b5b27a0e1/internal/congestion/pacer.go
[g-master-rate]: https://github.com/quic-go/quic-go/blob/7c3a98eeb4144e77876ba77c9292da9b5b27a0e1/internal/congestion/cubic_sender.go#L287-L294
[f-policy]: https://github.com/the-sarge/quic-go-fast/blob/55ea687d68c9c6f33785a1875374ec6a0d7570e8/bbr_send_policy.go#L11-L67
[f-emission]: https://github.com/the-sarge/quic-go-fast/blob/55ea687d68c9c6f33785a1875374ec6a0d7570e8/packet_emission_bbr.go#L136-L146
[f-design]: https://github.com/the-sarge/quic-go-fast/blob/55ea687d68c9c6f33785a1875374ec6a0d7570e8/docs/designs/bbrv3.md#L141-L147
[f-d08]: https://github.com/the-sarge/quic-go-fast/blob/55ea687d68c9c6f33785a1875374ec6a0d7570e8/docs/designs/bbrv3.md#L225
