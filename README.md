# quic-go-fast

A fork of [quic-go](https://github.com/quic-go/quic-go) for efficient bulk DATAGRAM and stream transfers, with lower allocation and copying costs, platform I/O optimizations, and transport/HTTP/3 reliability fixes. Based on **quic-go v0.62.0**, retaining its module path and QUIC/HTTP/3 wire compatibility. Requires **Go 1.26.0+**.

**Latest fork release: [v0.62.1-fast.9](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.9)**, published October 25, 2026. This maintenance prerelease contains fast.8 plus the idle path-MTU discovery timer fix. The highlights below remain part of its shipped feature set.

**Fast.9 and `main` differ:** fast.9 was released from a maintenance branch and excludes the adaptive application-loss thresholds in [#971](https://github.com/the-sarge/quic-go-fast/issues/971). That policy is implemented on `main` but remains unreleased, with no measured performance benefit or consumer qualification claimed. See [release and main selection](docs/usage.md#release-and-main-selection) and [Unreleased](CHANGELOG.md#unreleased).

## Improvements over the v0.62.0 baseline

Shipped capabilities combine fork-specific work and selected upstream fixes. See the [integration guide](docs/usage.md) for API ownership and platform requirements.

- **Lower allocation and linear stream copying.** Reject DATAGRAM overflow before allocation and remove an intermediate parser copy. Large `TryWriteAll` transfers replace quadratic tail copying with linear copying. HTTP/3 tracing and receive cleanup reduce allocation and retained memory.
- **Native batching across platforms.** Linux GRO, Windows USO/URO and qualified macOS batch sends move more packets per socket call. Equal-size short DATAGRAM packets share segmented sends, with capability checks and ordinary socket fallbacks.
- **Burst capacity and ACK control.** Bounded 1,024-entry received-DATAGRAM queues expose drop counters. Opt-in local ACK thresholds of 2–32 packets support known compatible peer controllers; negotiated ACK Frequency remains unimplemented.
- **Application-owned transport integration.** External packet I/O, reusable managed socket leases, fixed-peer filtering, bounded connection setup, candidate selection and receive-phase control give applications explicit resource and traffic ownership.
- **Managed ECN and path-MTU discovery.** Qualified managed paths preserve ECN on Linux, macOS, FreeBSD and Windows, with path-MTU discovery on qualified Linux and macOS leases.
- **Recovery and connection lifecycle fixes.** Correct handshake sizing, path validation, cancellation, socket cleanup and HTTP/3 reuse/shutdown. Reno restores a cutback when all supporting losses are proved spurious; fast.9 also keeps idle path-MTU discovery progressing.

**Experimental BBRv3 for lossy bulk transfers.** Opt in with `Config.SetCongestionControlV1("bbrv3")`. The measured benefit below is against fast.8 Reno. BBR remains **unqualified**: higher memory use, control-message latency and adverse Reno coexistence remain limitations. **Reno remains the default.** See the [release's BBR status](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8#corrected-bbr-and-subsequent-recovery).

## Performance evidence

Historical measurements of individual changes, using the explicit baselines below:

| Change | Recorded result | Comparison baseline and workload |
| --- | --- | --- |
| [Short DATAGRAM batching](https://github.com/the-sarge/quic-go-fast/issues/743#issuecomment-6331858628) | **2.48× throughput**, sender/receiver CPU per GiB **−57%/−53%** | On versus off at `642f014e`; downstream Linux network namespaces |
| [Linux GRO](https://github.com/the-sarge/quic-go-fast/blob/23c83b9896980511286ca8b81cd632f7ec56afed/docs/audits/2026-09-11-g2-gro-results.md) | **1.26× throughput**, receive syscalls/datagram **×0.263** | GRO on versus off in the adoption build, peer GSO on in both; paired Linux loopback transfers |
| [Windows USO](https://github.com/the-sarge/quic-go-fast/blob/23c83b9896980511286ca8b81cd632f7ec56afed/docs/audits/2026-09-11-w2-uso-results.md) | **3.01× throughput**, send submissions/packet **×0.0815** | USO on versus off in the same Windows message-I/O build; paired hosted Windows loopback transfers |
| [macOS batch send](https://github.com/the-sarge/quic-go-fast/blob/23c83b9896980511286ca8b81cd632f7ec56afed/docs/audits/2026-09-12-d1-sendmsgx-results.md) | **1.17× throughput**, send syscalls/packet **×0.126** | Batch versus per-packet sends in the adoption build; paired Darwin 25 arm64 loopback transfers |
| [Experimental BBRv3](https://github.com/the-sarge/quic-go-fast/blob/563785cf44a6be8b36f360bdf0e80acdf59df8ae/docs/audits/2026-10-22-bundle-990-readiness/result.md) | **14.06× STREAM / 14.27× DATAGRAM useful throughput** | Fast.8 BBR versus fast.8 Reno; Linux emulation, 100 Mbps each way, 100 ms base RTT, 0.1% random forward loss; median of five paired ratios per traffic type |

These are historical, workload-specific results; they do not establish an aggregate speedup for the current fork against current upstream. The short-DATAGRAM result also raised mean RTT by 22.7 µs and lifetime minimum RTT by 9.8 µs, accepted under the [recorded decision](https://github.com/the-sarge/quic-go-fast/issues/743#issuecomment-6331925155). The [fast.1 measurements](CHANGELOG.md#recorded-measurements) and [fast.8 release notes](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8) retain the baselines, accepted costs and limitations.

## Use the fork

Keep `github.com/quic-go/quic-go` imports. Select the fork in your application's main module:

```sh
go mod edit -replace=github.com/quic-go/quic-go=github.com/the-sarge/quic-go-fast@v0.62.1-fast.9
go mod tidy
go list -m github.com/quic-go/quic-go
```

The version after `=>` identifies the selected fork. Commit the resulting `go.mod` and `go.sum`. Each application needs its own replacement; dependency replacements do not propagate. Use an exact published fork tag: `@latest` can select inherited upstream tags.

Use keyed `Config` literals; positional literals no longer compile. Applications inspecting HTTP/3 errors should follow the [error-handling migration guide](http3/README.md#error-handling). Check compatibility when another dependency expects APIs newer than the v0.62.0 baseline. [Integration and rollback instructions](docs/usage.md) cover fork-only APIs and socket ownership.

## Support and development

Unit and integration CI run on Linux, macOS and Windows, with additional Linux race coverage. Other cross-compiled targets are build-only; native offload support depends on the host and packet-I/O path. Review the [fast.9 release notes](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.9) and inherited [fast.8 limitations](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8) and evaluate this prerelease against your workloads. Consumer certification and adoption remain separate decisions.

- [Upstream usage documentation](https://quic-go.net/docs/) · [Fork changelog](CHANGELOG.md) · [Report a fork issue](https://github.com/the-sarge/quic-go-fast/issues)
- [Development toolchains](docs/usage.md#development-toolchains) · [Development journal](docs/DEV-JOURNAL.md) · [Evidence index](docs/audit-evidence.md) · [Release runbook](docs/runbooks/release.md)

Built on the work of the quic-go authors and contributors. [MIT license](LICENSE); upstream logo and brand assets have a [separate usage policy](assets/LICENSE.md).
