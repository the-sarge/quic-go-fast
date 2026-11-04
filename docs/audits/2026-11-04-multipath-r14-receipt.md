# R14 two-path smoke receipt

[R14 #1034](https://github.com/the-sarge/quic-go-fast/issues/1034), under [Reno parent #1013](https://github.com/the-sarge/quic-go-fast/issues/1013) and [program #646](https://github.com/the-sarge/quic-go-fast/issues/646), runs the first independent two-path transfer. Contract: [R14](../adr/2026-10-24-multipath-reno-plan.md#r14) and its [re-audit](2026-11-04-r14-close-reaudit.md).

## Fixed evidence

`TestPicoquicMultipathSmoke/client` and `TestPicoquicMultipathSmoke/server` each executed once and passed on candidate `69361026f`, image `sha256:aab40be682a3d9565df1ce2611629fa6726440165594eb6dd53eac9de1addc1d`. See [Go JSON](2026-11-04-multipath-r14/go-test.jsonl), [client-role peer output](2026-11-04-multipath-r14/client-peer.log) and [server-role peer output](2026-11-04-multipath-r14/server-peer.log).

| Role | Our STREAM bytes on path 0 / 1 | Our CONNECTION_CLOSE on path 0 / 1 | Peer |
| --- | --- | --- | --- |
| Client, against `picoquicdemo -M` | 20 / 20 (one request per path) | 1 / 1 | Server exit code 0 |
| Server, against `picoquicdemo -M -A 127.0.0.2/0` | 425,984 / 425,984 (the response, split by a primary switch) | 0 / 0 (the peer closed) | Client exit code 0, 94 and 93 packets sent on its two paths |

Each receiver compared the whole 851,968-byte fixed workload. Each role also reset one stream and finished with the connection still usable. This is the pinned peer of [R1](2026-11-01-multipath-r1-receipt.md): picoquic `01e124ebdf9fc50df307e01e71f2744471ddc470` and PicoTLS `bfa67875982afc4c24f21e146cef4747fa189c2f`, with Go 1.27.3, the same image base and the same Debian snapshot.

Execution used Docker Desktop on LinuxKit with amd64 emulation on a Darwin arm64 host, `--network none` and loopback UDP over `127.0.0.1` and `127.0.0.2`. This is example-level evidence for one fixed scenario. It doesn't replace [R17 #1037](https://github.com/the-sarge/quic-go-fast/issues/1037)'s matrix, native qualification or any performance claim, and public negotiation stays disabled.

## Applicability to the merged candidate

Commits after `69361026f` change tests, move `setLocalUsePrivate` to an untagged file and wrap the fault check for other platforms. They also merge main, which brings `ClearProbeCredit` from [PR #1166](https://github.com/the-sarge/quic-go-fast/pull/1166), and that affects only a recovered suspect path, which the smoke doesn't exercise. None changes the smoke's close, activation or transfer path, so this fixed evidence stays applicable. A later change to those owners requires a recorded reason and another charge.

## Operational ledger

The [ledger](2026-11-04-multipath-r14/ledger.json) charges 5 instances and 120 host-seconds: 2 regular and 3 from the reserve. The reserve charges are two image builds and a [failed client-role run](2026-11-04-multipath-r14/failed-attempt-go-test.jsonl), whose cause was the test's uninitialized target transport. Cumulative use is 73 of 180 instances, leaving 107 instances and 28,138 host-seconds. No other receipt has been published since [E6](2026-11-03-multipath-e6-receipt.md).

The reserve was already overdrawn by 14, so it is now overdrawn by 17, and the 124 remaining scheduled instances exceed remaining capacity by 17. The owner said on 2026-11-03 that the ceiling can be raised if needed. This receipt doesn't change it, and a later preflight has to record that raise before capacity runs out.

The integration suite owns the smoke as part of the maintained peer aid. Equivalent independent coverage or feature removal is required for retirement.
