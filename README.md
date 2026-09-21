# quic-go-fast

A fork of [quic-go](https://github.com/quic-go/quic-go) focused on lower allocation and copying costs, efficient platform I/O, and reliable transport lifetimes. The upstream baseline is **quic-go v0.62.0**. Existing public APIs, import paths, and QUIC/HTTP/3 wire compatibility are preserved; Go **1.26.0 or newer** is required.

The fork began with bulk DATAGRAM transfer and now includes stream, handshake, HTTP/3, and shutdown improvements. The themes below summarize what ships here. See the [detailed changelog](CHANGELOG.md) and [GitHub Releases](https://github.com/the-sarge/quic-go-fast/releases) for individual changes, validation boundaries, and known limitations. General protocol and API usage is covered by the [upstream documentation](https://quic-go.net/docs/).

## Improvements over upstream

### Less allocation, copying, and retained memory

DATAGRAM receive overflow is rejected before allocating, and the parser no longer makes an intermediate payload copy. Large stream writes use linear copying rather than repeatedly copying the remaining payload. Consumed HTTP/3 datagrams and abandoned receive-stream data release their references promptly. Optional HTTP/3 tracing avoids building header collections when no recorder exists. Existing APIs keep their caller-buffer ownership contracts.

### More efficient platform I/O

Linux gains UDP coalesced receive (GRO). Windows gains message I/O with packet metadata, segmented send (USO), and coalesced receive (URO). Qualified macOS hosts can batch sends with `sendmsg_x`. Capability checks, runtime controls, and ordinary socket fallbacks constrain activation; availability depends on the host.

macOS batching uses a private syscall guarded by kernel qualification and a startup self-check. Set `QUIC_GO_DISABLE_SENDMSG_X=true` or build with `-tags quic_go_no_private_syscalls` to disable it. `QUIC_GO_DISABLE_GSO=true` disables segmented send and `QUIC_GO_DISABLE_GRO=true` disables coalesced receive. Receive coalescing is enabled on transport-created sockets and on supported external Linux and Windows sockets explicitly registered through `ConfigureExternalPacketIOV1`. External permission requires exclusive packet I/O and terminal socket disposal by the existing owner; `Transport.Close` does not restore ordinary raw reads, so borrowed sockets intended for reuse must not grant it. Managed Linux and Windows endpoints instead retain a normalizing receiver across leases: `ConfigureManagedPacketIOV1` enables coalescing after establishing that owner, and endpoint/lease `ReadFrom` continues returning one UDP datagram. Lease return discards already consumed QUIC storage without draining kernel-queued traffic. The macOS receive-batching experiment and alternative DATAGRAM receive-queue representations were evaluated and retired; they do not ship.

### Explicit packet and stream lifetimes

A private emission component owns packet construction, recovery registration, and handoff to socket I/O. Receive processing and coalesced reads track storage ownership through delivery and cleanup. The associated repairs release buffers on errors, cancellation, connection shutdown, and rejected admission, close internally owned sockets after setup failures, and wake readers canceled after partial stream resets. Invalid batch-send progress stops the send path instead of risking an ambiguous retry.

### More reliable handshakes and HTTP/3 lifecycles

Eligible local message-size errors reduce oversized handshake flights and enter the existing recovery path. Repeated successful path probes complete their waiters, and send-path publication is synchronized. HTTP/3 keeps pooled connections busy through complete uploads and response consumption, preserves replacement connections during stale failure cleanup, honors cancellation while waiting for SETTINGS, and makes server admission atomic with shutdown. Response completion has one owner for buffered output, lengths, and trailers.

`ConfigureFixedPeerV1` fixes a transport's permitted UDP peer before initialization, guarding receive, ordinary/batched/stateless output and alternate paths. It does not authenticate identity or enable migration. Wiremux retains its selected-peer wrapper.

### Stronger validation and useful diagnostics

CI actually enables the race detector, integration fixtures honor the selected QUIC version, and tests assert observable transfer and lifecycle outcomes. Deterministic loss/corruption cases and retained failure diagnostics make recovery behavior easier to assess. Qlog reports the selected fork revision and handles optional logging failures without terminating the process. Archived audit data remains accessible in Git but is excluded from Go module downloads.

## Performance evidence and current limits

Individual measurements establish improvements in specific workloads, rather than an overall speedup for every application:

| Change | Recorded result | Measurement scope |
| --- | --- | --- |
| DATAGRAM overflow admission | One allocation per rejected record → zero | Queue microbenchmark with 1071-byte records |
| DATAGRAM parser copy removal | About 47% less receiver allocation per delivered record | Native Linux QUIC loopback workload |
| Linux GRO | Receive syscalls ×0.263; throughput ×1.26 | Paired loopback bulk transfers |
| Windows USO | Send submissions ×0.0815; throughput ×3.01 | Paired hosted Windows loopback transfers |
| Windows URO | Receive syscalls ×0.098; throughput ×1.34 | Two-endpoint KVM virtual-NIC transfers |
| macOS batch send | Send syscalls ×0.126; throughput ×1.17 | Paired Darwin 25 arm64 loopback transfers |

These results were recorded at individual adoption commits, not remeasured as an aggregate comparison of the current fork. The DATAGRAM parser change saves allocation, but its original tail-latency noninferiority bound was not established. The [changelog](CHANGELOG.md#recorded-measurements) and [evidence index](docs/audit-evidence.md) provide context and links.

The [assembled support decision](https://github.com/GridSwarm/wiremux/blob/main/docs/research/e02-support.md) records native external packet-I/O and managed-reuse correctness on its specific Linux, Windows and Darwin hosts. Its final performance comparisons were unavailable; it retains ordinary upstream defaults and makes no assembled-Wire speedup or resource-preservation claim.

Unit CI exercises Linux, macOS, and Windows on Go 1.26.x and 1.27.x. Integration CI covers Linux on both versions and macOS/Windows on Go 1.27.x, with additional Linux race coverage. Other cross-compiled targets are build-only. Intermittent macOS dial/HTTP timeouts remain unresolved; see the [known limitations](CHANGELOG.md#known-limitations). The [path-MTU convergence fixture](https://github.com/the-sarge/quic-go-fast/blob/9e8cec69d76a80812737750709289d4925a927c1/docs/audits/issue-178-convergence/README.md) now keeps traffic active until its existing tolerance is reached; the historical hosted probe-loss source remains unknown. Evaluate the prerelease against your application's workloads.

## Use the fork

Keep existing `github.com/quic-go/quic-go` imports and select the fork through a `replace` directive in your application's main module. The published `v0.62.1-fast.4` tag includes the external packet-I/O, managed-endpoint and fixed-peer extensions described above, plus the HTTP/3 compatibility and correctness changes in the changelog:

```sh
go mod edit -replace=github.com/quic-go/quic-go=github.com/the-sarge/quic-go-fast@v0.62.1-fast.4
go mod tidy
go list -m github.com/quic-go/quic-go
```

For another version, select an exact published tag from [GitHub Releases](https://github.com/the-sarge/quic-go-fast/releases). Avoid `@latest`: inherited upstream release tags can take precedence over fork prereleases. In `go list` output, the version after `=>` identifies the selected fork. Commit the resulting `go.mod` and `go.sum` changes.

A dependency's replacement does not propagate to its consumers: each application must select the fork explicitly. The replacement applies to every selected version of `github.com/quic-go/quic-go`; verify compatibility if another dependency expects APIs newer than the upstream v0.62.0 baseline.

To return to upstream, first remove or adapt fork-only factory, registration and fixed-peer calls, including any required managed-endpoint option. Then remove the replacement and tidy:

```sh
go mod edit -dropreplace=github.com/quic-go/quic-go
go mod tidy
```

## Development and attribution

Report fork-specific issues in [the-sarge/quic-go-fast](https://github.com/the-sarge/quic-go-fast/issues). The [development journal](docs/DEV-JOURNAL.md) records merged work and validation evidence; [CONTEXT.md](CONTEXT.md) defines domain terminology; the [release runbook](docs/runbooks/release.md) describes publication. Fork releases follow [validated stable upstream releases](docs/adr/0003-follow-stable-upstream-releases.md).

quic-go-fast builds on the work of the quic-go authors and contributors. Code is licensed under the [MIT license](LICENSE). Upstream logo and brand assets have a [separate usage policy](assets/LICENSE.md).
