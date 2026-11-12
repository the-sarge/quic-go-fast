# R18 delivery receipt (#1038)

[R18 (#1038)](https://github.com/the-sarge/quic-go-fast/issues/1038) completed 36 eligible observations on exact source `56e011367705ce3adb31a70ce23f4b5ff29b5521`, using the same Go 1.27.4 binary in both roles. The [raw numerical summary](2026-11-12-multipath-r18-delivery-raw-summary.json) records a strict failure of the 1.10 CPU limit in two cells. The [owner's exact decision](2026-11-12-multipath-r18-delivery-disposition.json) accepts those two measured exceptions for delivery. The 1.10 CPU and 10% native RSS growth limits remain in effect. [Owner authorization for R18 (#1038)](https://github.com/the-sarge/quic-go-fast/issues/1038#issuecomment-6837253248) · [Full qualification result (#1038)](https://github.com/the-sarge/quic-go-fast/issues/1038#issuecomment-6837136807)

The product change groups compatible short DATAGRAMs in the native software send path while preserving individual UDP writes. The copying API reuses library-owned payload backing after serialization and preserves frame metadata and queue ownership. The Linux writer yields after publishing a successful, timely grouped request, outside the endpoint lock.

| Cell | Native / ordinary CPU per useful byte | Native / ordinary goodput | Median echo p99 margin | Median send-call p99 margin | Worst native VmHWM growth | Verdict |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| STREAM, GSO on | 1.081192 | 1.069406 | -0.705 ms | -0.912 ms | 0.00% | Pass |
| STREAM, GSO off | 0.911210 | 1.071699 | -0.082 ms | -0.844 ms | 3.52% | Pass |
| DATAGRAM, batching on | 1.121686 | 0.994596 | -0.803 ms | -1.022 ms | 0.32% | Fail (CPU) |
| DATAGRAM, batching off | 0.999309 | 1.033159 | -1.524 ms | -0.998 ms | 2.57% | Pass |
| Mixed bulk | 1.108585 | 1.039968 | -0.771 ms | -0.904 ms | 0.00% | Fail (CPU) |
| Stateless pressure | 1.093247 | 1.049702 | -0.738 ms | -0.904 ms | 0.00% | Pass |

Negative p99 margins are below the existing limits. All cells passed the 0.95 goodput floor and original latency, progress, write and close checks. Every native row passed the 10% VmHWM growth limit. Mixed and stateless workload CPU density uses useful bytes across all flows; the goodput ratio uses the ordinary flow. The table uses the maintained per-cell paired medians.

- DATAGRAM, batching on missed only CPU per useful byte. Pair ratios 1.115841, 1.123318, 1.121686.
- Mixed bulk missed only CPU per useful byte. Pair ratios 1.100474, 1.116511, 1.108585.

| Cell | Ordinary / native median ordinary-flow Gbit/s | Ordinary / native median server CPU-s | Ordinary / native median server CPU-s/GB | Maximum native ending VmHWM KiB |
| --- | ---: | ---: | ---: | ---: |
| STREAM, GSO on | 13.199 / 14.115 | 101.071 / 116.992 | 1.021 / 1.105 | 30212 |
| STREAM, GSO off | 6.084 / 6.517 | 118.169 / 115.194 | 2.591 / 2.357 | 30468 |
| DATAGRAM, batching on | 11.567 / 11.489 | 93.752 / 104.593 | 1.081 / 1.212 | 21812 |
| DATAGRAM, batching off | 5.460 / 5.641 | 117.426 / 121.273 | 2.868 / 2.866 | 22216 |
| Mixed bulk | 13.171 / 13.720 | 103.269 / 119.159 | 1.020 / 1.130 | 34848 |
| Stateless pressure | 13.158 / 13.812 | 102.117 / 117.332 | 1.036 / 1.132 | 31100 |

| Cell / profile | Echo count | Server packets lost | Client app DATAGRAM drops | Client unprocessed drops |
| --- | ---: | ---: | ---: | ---: |
| STREAM, GSO on / ordinary | 18000 | 2744 | 0 | 2744 |
| STREAM, GSO on / native | 18000 | 2887 | 0 | 2887 |
| STREAM, GSO off / ordinary | 18000 | 0 | 0 | 0 |
| STREAM, GSO off / native | 18000 | 0 | 0 | 0 |
| DATAGRAM, batching on / ordinary | 18000 | 17853 | 7 | 17853 |
| DATAGRAM, batching on / native | 18000 | 33493 | 11 | 33493 |
| DATAGRAM, batching off / ordinary | 18000 | 173 | 0 | 173 |
| DATAGRAM, batching off / native | 18000 | 1290 | 0 | 1290 |
| Mixed bulk / ordinary | 18000 | 2736 | 0 | 2736 |
| Mixed bulk / native | 18000 | 4155 | 0 | 4155 |
| Stateless pressure / ordinary | 18000 | 2317 | 0 | 2317 |
| Stateless pressure / native | 18000 | 2556 | 0 | 2556 |

All 18 native RSS rows passed. Every cell passed the existing goodput, p99, progress, write and close checks. Each mixed-bulk row delivered 2,516,582,400 background bytes. In stateless pressure, the client observed 403,447–411,709 resets in the ordinary profile and 384,907–387,389 in the native profile. The raw summary preserves offered counts and reset shares. These are peer-observed counts for the fixture, separate from local native endpoint outcomes.

The [previous stopped campaign](2026-11-12-multipath-r18-final-receipt.md) retains its own outcomes and source identity. This campaign ran all six cells and used zero replacements. Every custody was released, fresh absence and restoration were verified, and the 36 observations occupied 2773.45554357301443 seconds.
