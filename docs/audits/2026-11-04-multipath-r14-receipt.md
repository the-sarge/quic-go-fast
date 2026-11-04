# R14 two-path smoke receipt

[R14 #1034](https://github.com/the-sarge/quic-go-fast/issues/1034), under [Reno parent #1013](https://github.com/the-sarge/quic-go-fast/issues/1013) and [program #646](https://github.com/the-sarge/quic-go-fast/issues/646), runs the first independent two-path transfer. Contract: [R14](../adr/2026-10-24-multipath-reno-plan.md#r14) and its [re-audit](2026-11-04-r14-close-reaudit.md).

## Fixed evidence

`TestPicoquicMultipathSmoke/client` and `TestPicoquicMultipathSmoke/server` each executed once and passed on candidate `abdd55927`, image `sha256:7e20acaea78dc0f72e6399732c6c2800db484dd3e39a76db5887a298694fa922`. See [Go JSON](2026-11-04-multipath-r14/go-test.jsonl), [client-role peer output](2026-11-04-multipath-r14/client-peer.log) and [server-role peer output](2026-11-04-multipath-r14/server-peer.log).

| Role | Our STREAM bytes on path 0 / 1 | Our CONNECTION_CLOSE on path 0 / 1 | Peer |
| --- | --- | --- | --- |
| Client, against `picoquicdemo -M` | 20 / 60 (one request on path 0, three on path 1) | 1 / 1 | Server exit code 0 |
| Server, against `picoquicdemo -M -A 127.0.0.2/0` | 425,984 / 425,984 (the response, split by a primary switch) | 0 / 0 (the peer closed) | Client exit code 0 |

Each receiver compared the whole 851,968-byte fixed workload. In the client role, we cancel reading a transfer in progress and then complete another full transfer. That shows the connection survives the abort, not that the peer observed it. In the server role, we reset the peer's first request, the peer's log reports the reset, and the next request completes. This is the pinned peer of [R1](2026-11-01-multipath-r1-receipt.md): picoquic `01e124ebdf9fc50df307e01e71f2744471ddc470` and PicoTLS `bfa67875982afc4c24f21e146cef4747fa189c2f`, with Go 1.27.3, the same image base and the same Debian snapshot.

Execution used Docker Desktop on LinuxKit with amd64 emulation on a Darwin arm64 host, `--network none` and loopback UDP over `127.0.0.1` and `127.0.0.2`. This is example-level evidence for one fixed scenario. It doesn't replace [R17 #1037](https://github.com/the-sarge/quic-go-fast/issues/1037)'s matrix, native qualification or any performance claim, and public negotiation stays disabled.

## Applicability to the merged candidate

Commits after `abdd55927` change no runtime owner on the smoke's path. A later change to its close, activation or transfer path requires a recorded reason and another charge.

## Operational ledger

The [ledger](2026-11-04-multipath-r14/ledger.json) charges 8 instances and 180 host-seconds: 2 regular and 6 from the reserve. The reserve charges are three image builds, a [failed client-role run](2026-11-04-multipath-r14/failed-attempt-go-test.jsonl) whose cause was the test's uninitialized target transport, and two passing role runs on an earlier candidate. Those were repeated because review changed path zero's cached source metadata and found the abort evidence too weak. Cumulative use is 76 of 180 instances, leaving 104 instances and 28,078 host-seconds. No other receipt has been published since [E6](2026-11-03-multipath-e6-receipt.md).

The reserve was already overdrawn by 14, so it is now overdrawn by 20, and the 124 remaining scheduled instances exceed remaining capacity by 20. The owner said on 2026-11-03 that the ceiling can be raised if needed. This receipt doesn't change it, and a later preflight has to record that raise before capacity runs out.

The integration suite owns the smoke as part of the maintained peer aid. Equivalent independent coverage or feature removal is required for retirement.
