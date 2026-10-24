# quic-go-fast

A fork of [quic-go](https://github.com/quic-go/quic-go) for efficient bulk DATAGRAM and stream transfers, with lower allocation and copying costs, platform I/O optimizations, and transport/HTTP/3 reliability fixes. Based on **quic-go v0.62.0**, retaining its module path and QUIC/HTTP/3 wire compatibility. Requires **Go 1.26.0+**.

**Latest fork release: [v0.62.1-fast.8](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8)**, published October 23, 2026. The highlights below ship in this prerelease; [Unreleased](CHANGELOG.md#unreleased) tracks subsequent work.

## Improvements over upstream

- **Less copying and allocation.** DATAGRAM overflow is rejected before allocation, the parser avoids an intermediate payload copy, and large stream writes use linear copying. HTTP/3 tracing and receive cleanup reduce unnecessary allocation and retained memory.
- **More data per socket call.** Linux coalesced receive (GRO), Windows segmented send/coalesced receive (USO/URO), and qualified macOS batch sends. Equal-size short DATAGRAM packets can share a segmented send. Host capability checks and ordinary socket fallbacks govern activation.
- **Better handling of bursts and connection lifetimes.** Deeper bounded receive queues expose drop counters. Fixes cover handshake packet sizing, path validation, cancellation, socket/buffer cleanup and HTTP/3 connection reuse and shutdown. Reno can undo a cutback when all supporting losses are proved spurious.
- **Integration with application-owned transports.** Opt-in APIs provide external packet I/O, managed socket leases, fixed-peer filtering, bounded connection setup, candidate selection and receive-phase control. See the [integration guide](docs/usage.md) for ownership requirements and platform controls.

**BBRv3 is available as an experimental opt-in:** `Config.SetCongestionControlV1("bbrv3")`. Fast.8 includes the corrected sender, but it remains **unqualified**, with unresolved performance and coexistence limitations. **Reno remains the default.** See the [release's BBR status](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8#corrected-bbr-and-subsequent-recovery).

## Performance evidence

Selected measurements from individual changes:

| Change | Recorded result | Workload and reference |
| --- | --- | --- |
| Short DATAGRAM batching | **2.48× throughput**, sender/receiver CPU per GiB **−57%/−53%** | Downstream Linux network namespaces; batching enabled versus disabled |
| Linux GRO | **1.26× throughput**, receive syscalls/datagram **×0.263** | Paired Linux loopback bulk transfers at GRO adoption |
| Windows USO | **3.01× throughput**, send submissions/packet **×0.0815** | Paired hosted Windows loopback transfers at USO adoption |
| macOS batch send | **1.17× throughput**, send syscalls/packet **×0.126** | Paired Darwin 25 arm64 loopback transfers at batch-send adoption |

These are historical, workload-specific results; they do not establish an aggregate speedup for the current fork against current upstream. The short-DATAGRAM result also raised mean RTT by 22.7 µs and lifetime minimum RTT by 9.8 µs, accepted under the [recorded decision](https://github.com/the-sarge/quic-go-fast/issues/743#issuecomment-6331925155). The [full measurement table](CHANGELOG.md#recorded-measurements) and [release notes](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8) retain the baselines, accepted costs and limitations.

## Use the fork

Keep `github.com/quic-go/quic-go` imports. Select the fork in your application's main module:

```sh
go mod edit -replace=github.com/quic-go/quic-go=github.com/the-sarge/quic-go-fast@v0.62.1-fast.8
go mod tidy
go list -m github.com/quic-go/quic-go
```

The version after `=>` identifies the selected fork. Commit the resulting `go.mod` and `go.sum`. Each application needs its own replacement; dependency replacements do not propagate. Use an exact published fork tag: `@latest` can select inherited upstream tags.

Use keyed `Config` literals; positional literals no longer compile. Applications inspecting HTTP/3 errors should follow the [error-handling migration guide](http3/README.md#error-handling). Check compatibility when another dependency expects APIs newer than the v0.62.0 baseline. [Integration and rollback instructions](docs/usage.md) cover fork-only APIs and socket ownership.

## Support and development

Unit and integration CI run on Linux, macOS and Windows, with additional Linux race coverage. Other cross-compiled targets are build-only; native offload support depends on the host and packet-I/O path. Review the [release limitations](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8) and evaluate this prerelease against your workloads.

- [Upstream usage documentation](https://quic-go.net/docs/) · [Fork changelog](CHANGELOG.md) · [Report a fork issue](https://github.com/the-sarge/quic-go-fast/issues)
- [Development toolchains](docs/usage.md#development-toolchains) · [Development journal](docs/DEV-JOURNAL.md) · [Evidence index](docs/audit-evidence.md) · [Release runbook](docs/runbooks/release.md)

Built on the work of the quic-go authors and contributors. [MIT license](LICENSE); upstream logo and brand assets have a [separate usage policy](assets/LICENSE.md).
