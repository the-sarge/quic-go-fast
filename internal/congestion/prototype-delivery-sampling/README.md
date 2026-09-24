# Disposable delivery-sampling prototype

Can packet-registration sampling plus a send quantum separate path delivery from local queueing and ACK compression, or does the design need bounded send feedback? This model answers the timing question with fixed path capacity and propagation. It is a planning artifact for [Test delivery sampling across queued and batched sends](https://github.com/the-sarge/quic-go-fast/issues/566), not a production BBR controller.

From the repository root, run:

```sh
go run ./internal/congestion/prototype-delivery-sampling
```

The terminal shows registration, acceptance and modeled-departure samplers side by side. Press Enter to advance an event, `a` for the next ACK, `j 480` to jump to 480ms, `r` to reset, or `q` to quit. Packet facts include future oracle knowledge and are explicitly diagnostic; the sampler only sees events already processed. The model is pure; the viewer owns terminal/file I/O.

Use `-list` to list scenarios; for example:

```sh
go run ./internal/congestion/prototype-delivery-sampling -scenario worker-stall-gso16 -quantum 4 -credit
go run ./internal/congestion/prototype-delivery-sampling -export /tmp/delivery-sampling-replay
```

`-credit` subtracts bytes still in the modeled send queue from the next opportunity's allowance. It does not account for kernel/NIC backlog. Export is explicit; the viewer writes no state. There is no existing repository Taskfile or package script to extend.

See the [evidence report](../../../docs/audits/2026-09-24-delivery-sampling-prototype/README.md) for assumptions, source correspondence, results and pending owner reaction. After that reaction, preserve the answer and frozen evidence, and retire this disposable model/viewer. Do not promote it into maintained transport code.
