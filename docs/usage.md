# Integration and platform guide

See the [README](../README.md#use-the-fork) for installation, the [published fast.9 maintenance release](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.9) for the idle path-MTU timer fix, and the [fast.8 release](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8) for the inherited feature set, defaults and limitations. The [changelog](../CHANGELOG.md) distinguishes released and unreleased changes. General QUIC and HTTP/3 usage is covered by the [upstream documentation](https://quic-go.net/docs/).

## Release and main selection

| Revision | Included behavior | Evidence and adoption boundary |
| --- | --- | --- |
| `v0.62.1-fast.8` (`8095c72244755f0c1e24f4158253bf09cfbe6e17`) | Eleven-slice bundle, corrected BBR sender and Reno undo; excludes adaptive application-loss thresholds | Published October 23, 2026, with candidate-specific memory, baseline and Linux readiness decisions |
| `v0.62.1-fast.9` (`0f717339f767df5d2e4091b2515266d61f66e2ac`) | Fast.8 plus the idle path-MTU timer fix only; excludes adaptive application-loss thresholds | Published October 25, 2026, from an approved maintenance branch; inherits fast.8 limitations and adds exact-source timer-fix validation |
| `main` | Includes the timer fix and adaptive application-loss thresholds (#971) | Development branch; #971 remains unreleased and its proposed performance comparison remains deferred |

Use the exact published fast.9 tag for the latest maintenance release. A higher fork prerelease suffix does not imply that all intervening `main` changes are included. Selecting `main` or a pseudo-version requires checking that exact revision's behavior and evidence; fast.8's measurements and advancement exceptions do not transfer automatically. The [#971 implementation closeout](https://github.com/the-sarge/quic-go-fast/issues/971) and [recovery assessment #962](https://github.com/the-sarge/quic-go-fast/issues/962) retain the unmeasured policy and deferred comparison context.

Reno remains default and BBRv3 remains experimental, opt-in and unqualified in both published versions and current `main`. Full [Q2 qualification #599](https://github.com/the-sarge/quic-go-fast/issues/599) awaits its execution preflight and separate operator dispatch. Publishing a fork version does not complete an application's certification or adoption; each consumer owns those decisions and its module replacement.

## Packet I/O and socket ownership

Receive coalescing is enabled on transport-created sockets and on supported external Linux and Windows sockets explicitly registered through `ConfigureExternalPacketIOV1`. External permission requires exclusive packet I/O and terminal socket disposal by the existing owner. Registration does not transfer close ownership, and `Transport.Close` does not restore ordinary raw reads; borrowed sockets intended for reuse must not grant receive-coalescing permission. The [registration API comments](../external_packet_io.go) define the exact contract.

For reusable sockets, `NewManagedPacketEndpointV1` creates an endpoint with exclusive leases. `ConfigureManagedPacketIOV1` binds a lease to the transport. Managed Linux and Windows endpoints retain a normalizing receiver across leases, so endpoint/lease `ReadFrom` continues returning one UDP datagram. Lease close interrupts and joins its I/O and permits reuse; `Transport.Close` alone does not release the lease. Lease return discards already consumed QUIC storage without draining kernel-queued traffic. Endpoint close is terminal. See the [managed endpoint contract](../managed_packet_endpoint.go).

`ConfigureFixedPeerV1` fixes a transport's permitted UDP peer before initialization, guarding receive, ordinary/batched/stateless output and alternate paths. It does not authenticate peer identity or enable migration. The [release notes](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.8#bundle-behavior-and-consumer-decisions) describe the opt-in network/setup admission, candidate selection and receive-phase extensions.

Multipath V1 is experimental and opt-in; disabled is the default, and connections without it behave as before. Its one supported profile is draft-21 QUIC v1 with Reno primary/backup and at most two paths, on Linux amd64. It was qualified with Go 1.26.11 and 1.27.4 on the recorded Ubuntu 26.04.1 image with kernel `7.0.0-30-generic`; another toolchain or kernel is outside the tested evidence. Enable it with `Config.ConfigureMultipathV1(true, 2, "primary-backup")`. A caller's socket needs an explicit `Transport` with the `Transport.ConfigureMultipathIOV1` grant, an exact unconnected `*net.UDPConn`; `Dial`, `Listen` and HTTP/3 `Server.Serve` refuse it, while the `Addr` helpers and HTTP/3's default client grant the sockets they own. Other platforms, a `Config.Versions` that lists QUIC v2, BBR and excluded transport configurations (fixed-peer or network admission, candidate groups, send-thread QoS, packet ceilings, external or managed packet I/O) fail with `ErrMultipathUnsupportedV1` before anything is advertised; `ConfigureMultipathV1` rejects any other policy, including aggregation, and more than two paths with `ErrMultipathInvalidConfigV1`. A connection that negotiates QUIC v2, or whose peer doesn't negotiate multipath, stays ordinary; check `Conn.MultipathNegotiatedV1`. Before closing a `Transport` that surviving paths may still use, wait on `Conn.WaitTransportDetachedV1`; after `Transport.Close`, `Transport.WaitMultipathIOStoppedV1` returns the socket's write authority. The qualification evidence is the [native receipt](audits/2026-11-03-multipath-e6-receipt.md), carried to those Go patch releases by the [November compiler reassessment](adr/2026-10-24-multipath-endpoint-plan.md#november-go-patch-reassessment), with its [writer-yield supplement](audits/2026-11-12-native-writer-yield-r21/README.md), the [independent interoperability receipt](audits/2026-11-09-multipath-r17-receipt.md) and the [ordinary-peer cost receipt](audits/2026-11-12-multipath-r18-delivery-receipt.md), which keeps two owner-accepted CPU-per-useful-byte exceptions. No consumer benefit has been measured, so this is not an adoption recommendation. Isolate these calls in an integration adapter, like the other fork additions.

A grant covers the whole transport, not only its multipath connections. Every connection dialed or accepted on a granted transport, including ordinary ones that never negotiate multipath, sends through the native endpoint, the socket writer the grant installs, and so does `Transport.WriteTo`. For connections, an expired local queue or write deadline there is backpressure, not a send failure: the connection stays open, bounded by its usual handshake, idle and path-validation timeouts. `WriteTo` instead returns `ErrMultipathBackpressureV1` when the endpoint can't take the packet, plus `ErrMultipathDeadlineV1` when its deadline expired. Each endpoint has a budget of 4,096 participants, shared by ordinary and multipath connections: a connection takes one on its own transport and one on each other granted transport it sends from through `Conn.OpenPathV1`, `Path.MigrateV1` or `Conn.AddPath`. When a transport's budget is full, a dial on it fails with `ErrMultipathResourceLimitV1`, an `OpenPathV1` or `MigrateV1` to it fails the same way, an `AddPath` probe from it is skipped until its probe timer retries, and its server refuses a new connection with CONNECTION_REFUSED, or drops the first packet when it cannot send the refusal in time. Closing a connection does not promise to free its participant at once.

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

Use Go 1.27.4 for repository maintenance; [`.go-version`](../.go-version) records that choice for version managers that support it. The Go command itself does not read this file. Select the exact maintainer or compatibility toolchain explicitly:

```sh
GOTOOLCHAIN=go1.27.4 go test ./...
GOTOOLCHAIN=go1.26.11 go test ./...
```

These commands may download the named toolchain but do not permit automatic upgrades beyond it. CI installs the exact version for each lane, sets `GOTOOLCHAIN=local` to prevent automatic toolchain switching, and verifies the effective version. Integration CI also verifies the FIPS and vendor fixture modules. The root and nested integration modules retain their `go 1.26.0` language minimum. When reproducing a compatibility lane with a version manager, select Go 1.26.11 explicitly and use `GOTOOLCHAIN=local`.

The [development journal](DEV-JOURNAL.md) records merged work and validation; [GLOSSARY.md](../GLOSSARY.md) defines domain terminology; the [release runbook](runbooks/release.md) describes publication.
