# Integration and platform guide

See the [README](../README.md#use-the-fork) for installation and the [published fast.8 release](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8) for the shipped feature set, defaults and limitations. The [changelog](../CHANGELOG.md) distinguishes released and unreleased changes. General QUIC and HTTP/3 usage is covered by the [upstream documentation](https://quic-go.net/docs/).

## Packet I/O and socket ownership

Receive coalescing is enabled on transport-created sockets and on supported external Linux and Windows sockets explicitly registered through `ConfigureExternalPacketIOV1`. External permission requires exclusive packet I/O and terminal socket disposal by the existing owner. Registration does not transfer close ownership, and `Transport.Close` does not restore ordinary raw reads; borrowed sockets intended for reuse must not grant receive-coalescing permission. The [registration API comments](../external_packet_io.go) define the exact contract.

For reusable sockets, `NewManagedPacketEndpointV1` creates an endpoint with exclusive leases. `ConfigureManagedPacketIOV1` binds a lease to the transport. Managed Linux and Windows endpoints retain a normalizing receiver across leases, so endpoint/lease `ReadFrom` continues returning one UDP datagram. Lease close interrupts and joins its I/O and permits reuse; `Transport.Close` alone does not release the lease. Lease return discards already consumed QUIC storage without draining kernel-queued traffic. Endpoint close is terminal. See the [managed endpoint contract](../managed_packet_endpoint.go).

`ConfigureFixedPeerV1` fixes a transport's permitted UDP peer before initialization, guarding receive, ordinary/batched/stateless output and alternate paths. It does not authenticate peer identity or enable migration. The [release notes](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8#bundle-behavior-and-consumer-decisions) describe the opt-in network/setup admission, candidate selection and receive-phase extensions.

## Platform controls

Offload availability depends on the host, packet-I/O path and explicit permissions. Generic platform CI does not prove native offload engagement. Qualified macOS batching uses the private `sendmsg_x` syscall with kernel qualification and a startup self-check. Build with `-tags quic_go_no_private_syscalls` to disable private-syscall paths.

On macOS, a participating wrapper implementing `ReadBatch` and `ReadBatchSlotsV1Conn` can opt into multiple datagrams per read; the default is one slot. Each buffer is one byte larger than the largest packet; a completely filled buffer is treated as truncated and dropped. The adapter owns its receive call; see the [read-slot API contract](../sys_conn.go) for bounds and platform exclusions. This is distinct from the fork's retired standalone `recvmsg_x` receive-batching experiment. Apple-silicon macOS also offers opt-in send-thread QoS through `ConfigureSendThreadQoSV1`; see its [contract and per-connection status](../send_thread_qos.go).

Set these environment variables to `true` to disable the named behavior:

| Variable | Effect |
| --- | --- |
| `QUIC_GO_DISABLE_GSO` | Disable segmented send, including Windows USO |
| `QUIC_GO_DISABLE_GRO` | Disable Linux GRO and Windows URO receive coalescing |
| `QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS` | Disable equal-size short-DATAGRAM batching; full-size segmentation remains available |
| `QUIC_GO_DISABLE_SENDMSG_X` | Disable macOS batch send |
| `QUIC_GO_DISABLE_RECVMSG_X` | Keep one macOS adapter read slot |
| `QUIC_GO_DISABLE_SEND_QOS` | Disable send-thread QoS for new connections |

## Trace privacy

Qlog traces can contain sensitive HTTP headers and QUIC tokens. The QUIC and HTTP/3 default tracers create missing `QLOGDIR` directories with mode `0700` and new trace files with mode `0600`, subject to umask on Unix. Existing directory permissions are preserved; choose a trusted directory and parent path. An existing trace path, including a symlink, disables tracing for that connection instead of overwriting it. Restrict access to trace archives and review them before sharing.

## Return to upstream

First remove or adapt fork-only factory, registration, fixed-peer, admission and congestion-control selection calls, including any required managed-endpoint option. Then remove the replacement and tidy:

```sh
go mod edit -dropreplace=github.com/quic-go/quic-go
go mod tidy
```

Verify the selected upstream version and your application's compatibility before deployment. Fork releases follow the [stable-upstream adoption policy](adr/0003-follow-stable-upstream-releases.md).

## Development toolchains

Use Go 1.27.3 for repository maintenance; [`.go-version`](../.go-version) records that choice for version managers that support it. The Go command itself does not read this file. Select the exact maintainer or compatibility toolchain explicitly:

```sh
GOTOOLCHAIN=go1.27.3 go test ./...
GOTOOLCHAIN=go1.26.10 go test ./...
```

These commands may download the named toolchain but do not permit automatic upgrades beyond it. CI installs the exact version for each lane, sets `GOTOOLCHAIN=local` to prevent automatic toolchain switching, and verifies the effective version. Integration CI also verifies the FIPS and vendor fixture modules. The root and nested integration modules retain their `go 1.26.0` language minimum. When reproducing a compatibility lane with a version manager, select Go 1.26.10 explicitly and use `GOTOOLCHAIN=local`.

The [development journal](DEV-JOURNAL.md) records merged work and validation; [CONTEXT.md](../CONTEXT.md) defines domain terminology; the [release runbook](runbooks/release.md) describes publication.
