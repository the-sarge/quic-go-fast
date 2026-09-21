# Changelog

## v0.62.1-fast.4 — 2026-09-21

Compatibility and correctness rollup based on upstream quic-go v0.62.0. Go 1.26.0 remains the minimum; the declared module path and dependency versions are unchanged from fast.3. The [release record](docs/releases/v0.62.1-fast.4.md) and [GitHub release](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.4) supply the compatibility, support, source and publication receipts.

HTTP/3 stream and request APIs now consistently return `*http3.Error` (possibly wrapped) for QUIC stream and application errors, preserving HTTP/3 codes, local/remote identity, application messages, and HTTP/3 operation context. Callers inspecting raw `*quic.StreamError` or `*quic.ApplicationError` should migrate to `errors.As` with `*http3.Error` or `errors.Is` with `&http3.Error{ErrorCode: code, Remote: remote}`. Response-parsing failures from `Transport.RoundTrip`, `Transport.RoundTripOpt`, and `ClientConn.RoundTrip` that previously returned a bare `*http3.Error` may now wrap it with operation context; replace direct type assertions with `errors.As` and use `errors.Is` for matching. `Server.ServeQUICConn` returns contextually wrapped `*http3.Error` values for request-stream acceptance failures, while control-stream setup failures retain contextually wrapped `*quic.ApplicationError` values; shutdown classifiers should use `errors.As` for both types. HTTP/3 errors do not carry QUIC stream IDs or retain the raw QUIC error in their chain. EOF, context cancellation, deadlines, transport errors, and `quic.ErrWouldBlock` retain their existing behavior. See the [migration example](http3/README.md#error-handling). Adapted from upstream quic-go [#5852](https://github.com/quic-go/quic-go/pull/5852), commit `510e6fa00c66419d4a4d836036aae8671aa6d0a3`.

HTTP/3 server request parsing now follows `net/http` request-target conventions. Ordinary requests and Extended CONNECT leave `Request.URL.Scheme` and `Request.URL.Host` empty; handlers should use `Request.Host` for the authority and `Request.TLS` for TLS information. Extended CONNECT uses its path and query as `RequestURI` and retains its protocol identity. Regular CONNECT continues to use authority-form `RequestURI` and `URL.Host`. Absolute URIs and other invalid `:path` forms are rejected, and `*` is accepted only for OPTIONS. Adapted from upstream quic-go [#5839](https://github.com/quic-go/quic-go/pull/5839), [#5842](https://github.com/quic-go/quic-go/pull/5842), and [#5841](https://github.com/quic-go/quic-go/pull/5841).

- Closing a client immediately after dialing no longer risks overrunning the coalesced close-packet buffer when Initial, Handshake and 1-RTT packets share one datagram. The selected 1-RTT AEAD overhead is included before Initial padding. Adapted from upstream quic-go [#5858](https://github.com/quic-go/quic-go/pull/5858), commit `fcb5bedbbcd74a3a80cd247f9f02660b98fc36f6`, in [#474](https://github.com/the-sarge/quic-go-fast/pull/474).
- `http3.Transport` no longer writes `MaxIncomingStreams = -1` into a caller-supplied `quic.Config` when the field is zero. It clones the configuration first and applies the HTTP/3 client default only to that private effective copy; nonzero caller values, version selection and datagram validation retain their existing behavior. [#463](https://github.com/the-sarge/quic-go-fast/pull/463)
- Direct and callback configuration preparation now share numeric clipping for stream limits, receive windows and packet size while retaining their distinct ownership contracts. Direct preparation preserves the five historical caller-visible clipping writes; callback results are copied without caller mutation. Invalid-version ordering, defaults, negative stream sentinels and selected versions remain unchanged. [#493](https://github.com/the-sarge/quic-go-fast/pull/493)
- Closing a non-active outgoing path is terminal for initial and retry probe admission. Pending manager work and connection-ID allocation are retired once, while cancellation, successful reprobes, stale-response rejection, active-path rejection and switching behavior remain unchanged. [#491](https://github.com/the-sarge/quic-go-fast/pull/491)
- `TransportError.Unwrap` omits nil members while preserving `net.ErrClosed`, crypto-error identity and `errors.Is` / `errors.As` behavior. [#466](https://github.com/the-sarge/quic-go-fast/pull/466)
- Managed lease binding now validates generation, excludes active ordinary I/O, prepares native receive handling and commits the QUIC phase through one endpoint-owned operation. This is an ownership hardening with no new public API, platform qualification or raw-socket handback guarantee. [#495](https://github.com/the-sarge/quic-go-fast/pull/495)

The maintained test fixtures now join more of their workers, retain bounded failure captures and use deterministic ownership assertions. The integration-test qlog helper also allocates exclusive owner-only files so concurrent same-second test transports do not truncate one another. Those repairs improve validation but do not change the shipped qlog packages, resolve the documented intermittent macOS failures, or establish arbitrary-loss recovery, general performance improvement, or new native offload support. [#419](https://github.com/the-sarge/quic-go-fast/pull/419)

## v0.62.1-fast.3 — 2026-09-16

Qualified external packet I/O and managed endpoint reuse, based on upstream quic-go v0.62.0. Go 1.26.0 remains the minimum; the declared module path and dependency versions are unchanged from fast.2. The [release record](docs/releases/v0.62.1-fast.3.md) and [GitHub release](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.3) supply the support, source and publication receipts.

- Explicit `ConfigureExternalPacketIOV1` registration binds the exact supplied connection and a checked batch writer before transport initialization. Participating wrappers retain receive and destination policy. Receive-format permission is separate from close ownership; inherited methods do not grant it. `UDPBatchWriterV1` keeps platform batching in the fork.
- `NewManagedPacketEndpointV1` creates a parent endpoint with exclusive leases; `ConfigureManagedPacketIOV1` binds the original lease through its participating wrapper. Closing a lease joins its I/O and permits reuse; closing the parent ends the socket. Linux GRO and Windows URO normalization persists across leases, while Darwin receive stays ordinary. Already consumed QUIC data is not replayed. Raw socket return after coalescing remains unsupported.
- `ConfigureFixedPeerV1` fixes a transport's permitted UDP peer before initialization and guards receive, ordinary/batched/stateless output and alternate paths. It does not authenticate identity or enable migration. Wiremux retains its selected-peer wrapper.
- Checked external paths support Linux GSO/GRO, Windows USO/URO and qualified Darwin `sendmsg_x`. Permission, platform qualification and existing disable switches still govern activation. Diagnostics distinguish receive eligibility from activation. A specifically denied optional ECN setup leaves a managed Linux endpoint in ordinary receive mode; descriptor and required packet-info failures remain errors.
- Ordinary OOB wrappers without `ReadBatch` now retain their supplied `ReadMsgUDP` policy. Temporary read retries are bounded, and HTTP/3 qlog shutdown joins admitted GOAWAY, handler and stream producers. Maintained integration fixtures and failure captures were also repaired; those changes do not establish a general timing or packet-loss guarantee.

The [assembled support decision](https://github.com/GridSwarm/wiremux/blob/main/docs/research/e02-support.md) qualifies example-level native correctness and managed reuse on its recorded Linux, Windows and Darwin domains. All final performance comparisons were unavailable with zero admitted pairs: retain ordinary upstream defaults, with no assembled-Wire CPU, allocation, latency, memory or capacity claim. Historical fork measurements remain scoped to their original revisions and workloads. See the [release support matrix](docs/releases/v0.62.1-fast.3.md#support-and-limitations) for tested versus build-only platforms, adoption and rollback.

## v0.62.1-fast.2 — 2026-09-14

Dependency and tooling refresh after the first fork prerelease. The upstream baseline remains quic-go v0.62.0, the minimum Go version remains 1.26.0, and the existing public API and module-replacement adoption model are unchanged.

| Dependency or tool | Previous | Updated |
| --- | --- | --- |
| `golang.org/x/crypto` | `v0.54.0` | `v0.57.0` |
| `golang.org/x/net` | `v0.56.0` | `v0.59.0` |
| `golang.org/x/sync` | `v0.22.0` | `v0.23.0` |
| `golang.org/x/sys` | `v0.47.0` | `v0.48.0` |
| `golang.org/x/text` | `v0.40.0` | `v0.42.0` |
| `golang.org/x/tools` | `v0.47.0` | `v0.50.0` |
| `golang.org/x/mod` | `v0.37.0` | `v0.41.0` |
| `go.uber.org/mock` / mockgen | `v0.5.2` | `v0.6.0` |
| golangci-lint | `v2.13.0` | `v2.13.2` |
| Interop image Go toolchain | `1.27.0` | `1.27.1` |

The FIPS and module-vendor integration fixtures follow the refreshed dependency graph. Generated code is regenerated with the updated mockgen and checked for consistency. Go 1.26.x and 1.27.x remain in CI; upgrading the interop image's build toolchain does not raise the library's Go requirement.

The [first prerelease](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.1) records the fork's changes relative to upstream, scoped performance measurements, tested versus build-only platforms, and unresolved investigations. Those limitations still apply; dependency updates do not establish fixes for the intermittent dial, HTTP, or path-MTU failures. This release distributes library source without binary or container assets.

## v0.62.1-fast.1 — 2026-09-14

First quic-go-fast prerelease for evaluation, based on upstream **quic-go v0.62.0**. Existing public APIs, the `github.com/quic-go/quic-go` module identity, and QUIC/HTTP/3 wire compatibility are preserved. Go **1.26.0 or newer** is required. This release keeps the validated dependency baseline; dependency and tool updates are a separate follow-up.

The sections below describe shipped fork changes relative to upstream v0.62.0. Architecture experiments that were retired are identified explicitly and are not release features.

### DATAGRAM receive allocation

- **Drop overflow before copying.** A full receive queue allocated and copied each incoming payload before discarding it, spending memory and CPU on messages it could not admit. Capacity is now checked first, preserving the existing 128-message limit, FIFO ordering, and drop-new behavior.
- **Remove the parser's extra payload copy.** The parser copied each DATAGRAM payload, then the receive queue copied it again. Parsing now borrows a bounded view of packet bytes during synchronous processing. The queue still makes the owning copy before delivery, so applications retain independent buffers with the existing lifetime contract.

The parser adoption accepted the remaining tail-latency uncertainty: the measurements did not establish the original 5% noninferiority bound. No throughput or tail-latency improvement is promised by that change. These changes ship through the existing DATAGRAM APIs. The ring and head-index receive queues evaluated during development remain archived experiments; production retains the existing queue representation. See [overflow admission #9](https://github.com/the-sarge/quic-go-fast/pull/9), [parser copy removal #16](https://github.com/the-sarge/quic-go-fast/pull/16), and the [measurement evidence index](docs/audit-evidence.md).

### Large stream writes

Large `TryWriteAll` calls repeatedly copied already queued bytes when appending and the remaining unsent buffer when emitting each packet. Copying work could therefore grow quadratically with the amount of queued data. Pending storage now grows geometrically, and segmentation copies packet-sized prefixes while advancing the unsent tail, making total copying linear. Retransmission keeps bounded packet storage; cancellation copies the reliable prefix once so a small retained range cannot keep a large discarded suffix alive. Atomic admission and caller-buffer ownership are preserved. See [large stream write changes #190](https://github.com/the-sarge/quic-go-fast/pull/190).

### Platform datapath offloads

Beyond Linux's existing GSO send path and `recvmmsg` receive batching, platform facilities for moving more data per socket call sat unused: Linux kernels can coalesce received datagrams, Windows can segment sends and coalesce receives, and macOS can batch sends. The transport now adopts datapath offloads on all three major platforms, each landed through a precommitted measurement protocol with predeclared pass bounds and its own correctness gate (the protocol and results pairs live under [docs/audits](docs/audit-evidence.md); the [program index](docs/adr/2026-09-11-datapath-offload-program.md) and [binding plan](docs/adr/2026-09-11-datapath-offload-plan.md) record the decisions):

- **Linux coalesced receive (UDP_GRO).** The kernel merges consecutive same-flow datagrams into one buffer per read, and the transport splits them back into individual datagrams before any packet parsing, preserving `ReadPacket()`'s one-datagram contract. Sibling segments can route to different connections and release concurrently, so coalesced storage uses an atomically reference-counted slab confined to the segments of a single read (a recorded amendment to [ADR 0005](docs/adr/0005-incoming-packet-lifetime.md)), with a dedicated 64 KiB pool tier and retention queues that copy segments out of slabs under per-connection byte budgets. See [storage contract #236](https://github.com/the-sarge/quic-go-fast/pull/236) and [activation #239](https://github.com/the-sarge/quic-go-fast/pull/239).
- **Windows datapath rebuild with segmented send (USO) and coalesced receive (URO).** Windows previously used a featureless plain-socket path. It now reads and writes through the standard library's message I/O (`WSARecvMsg`/`WSASendMsg` on the runtime's IOCP poller) with control-message encoding, packet-info parity, and preserved deadline/close semantics ([foundation #242](https://github.com/the-sarge/quic-go-fast/pull/242)); probes `UDP_SEND_MSG_SIZE` and reuses the existing platform-neutral segmented-send logic ([USO #245](https://github.com/the-sarge/quic-go-fast/pull/245)); and probes `UDP_RECV_MAX_COALESCED_SIZE`, feeding `UDP_COALESCED_INFO` segment sizes to the same split seam Linux uses ([URO #248](https://github.com/the-sarge/quic-go-fast/pull/248)).
- **macOS batch send (`sendmsg_x`).** Queued datagrams go out in batches through XNU's private `sendmsg_x` syscall under fail-closed qualification: a Darwin-kernel-major allowlist recording tested version floors, a startup self-check that exercises the production call shape and verifies delivered bytes, destinations, ECN marks, and accepted counts semantically, per-call accepted-count bounds, and a process-lifetime latch on any structurally invalid result. Partial kernel acceptance never resends accepted entries and keeps message-size errors and handshake MTU feedback attached to the correct packet. iOS builds and the `quic_go_no_private_syscalls` opt-out tag exclude the private-syscall path at compile time. See [batch send #257](https://github.com/the-sarge/quic-go-fast/pull/257). The matching `recvmsg_x` receive-batching experiment was built, measured, and **retired** by its own predeclared gates — shallow receive fills never amortize the private call's per-invocation cost — with the experimental path never merged ([experiment #259](https://github.com/the-sarge/quic-go-fast/pull/259), [record #260](https://github.com/the-sarge/quic-go-fast/pull/260)).

Receive coalescing is enabled only on transport-owned sockets, and Windows offload probes are also restricted to those sockets. Platform capability checks and macOS qualification guard activation. Runtime controls follow the existing convention: `QUIC_GO_DISABLE_GSO` (segmented send, now including Windows), `QUIC_GO_DISABLE_GRO` (coalesced receive on Linux and Windows), and `QUIC_GO_DISABLE_SENDMSG_X` (macOS batch send). When an offload is unavailable, the transport falls back to its ordinary socket path. Existing public APIs are preserved.

The shared send queue now rejects negative or oversized batch-acceptance counts as terminal errors instead of treating them as zero progress and retrying an unknown remainder. This prevents ambiguous progress from causing duplicate sends. [#292](https://github.com/the-sarge/quic-go-fast/pull/292)

Linux and Windows now share ownership of the undelivered datagrams from each coalesced read. The helper establishes all sibling references before returning the first datagram, preserves metadata, clears consumed slots, and releases pending views without invalidating already returned data. This consolidates the existing behavior rather than introducing a new offload. [#296](https://github.com/the-sarge/quic-go-fast/pull/296)

### Handshake and path recovery

- **Recover from local handshake message-size errors.** When the socket rejected an oversized Initial/Handshake flight, recovery could keep sending packets at the unusable size until the handshake timed out. Eligible errors now reach the connection, which lowers handshake packetization to 1200 bytes and uses existing loss/PTO recovery. Feedback is guarded by handshake phase and path generation. This addresses explicit local errors, not paths that silently drop oversized traffic. See [handshake MTU recovery #20](https://github.com/the-sarge/quic-go-fast/pull/20).
- **Complete repeated successful path probes.** Once a path had been validated, another probe could receive a matching PATH_RESPONSE without completing its waiter; old challenge and retry state could also survive into the next probe. Responses now complete the current probe independently of previous validation, and new probes clear stale challenges and retries while preserving path-switch eligibility. See [path probe repair #187](https://github.com/the-sarge/quic-go-fast/pull/187).
- **Synchronize send-path publication.** Active send-connection changes and the socket worker's GSO fallback updates could race with reads in other goroutines. Both updates now have synchronized publication. See [active connection #125](https://github.com/the-sarge/quic-go-fast/pull/125) and [GSO fallback #122](https://github.com/the-sarge/quic-go-fast/pull/122).

### Outgoing packet ownership

Sending paths spread capacity checks, destructive packet construction, recovery registration, accounting, logging, and buffer handoff across callers. Each caller had to get the ordering and cleanup right, making changes difficult to reason about and test. A private packet-emission component now owns that sequence across ordinary/GSO sends, handshake packets, ACKs, PTO probes, path/MTU probes, and connection close. The connection goroutine still owns protocol state, and the asynchronous send worker still performs socket I/O.

Fatal writes and stopped workers could leave packet buffers unreleased, and the pooled buffer used to construct a CONNECTION_CLOSE remained pinned for the closed-connection retention window. Cleanup now releases failed and abandoned sends, while close emission copies the retained payload once and releases its construction buffer. Version-negotiation recreation also avoids emitting a close packet in the old version. Follow-up work removed partially assembled emission state from constructors and moved maintained tests onto the shipped sending path, reducing the chance of testing a composition that production never uses. See the [ownership decision](docs/adr/0004-packet-emission-ownership.md), [completed emission program](docs/adr/2026-09-07-packet-emission-program.md), and [constructor assembly #183](https://github.com/the-sarge/quic-go-fast/pull/183).

### Incoming packet ownership and cleanup

Several QUIC packets can share one received UDP buffer. Previously, processing could continue with a zero reference count, making the bytes vulnerable to premature recycling when a retained view was released. Early returns and shutdown paths also left gaps in buffer disposal. Receive processing now holds an explicit active reference, and each handoff transfers or disposes of ownership. The changes address:

- **Parsing and retained views:** rejected retention could be reported as successful, and deferred-decryption/replay cleanup could leave views behind. Admission now reports whether it actually retained a view, and processing exits and shutdown dispose of their owned references.
- **Queued and discarded inputs:** terminal transport routes, response/non-QUIC queues, and server-held 0-RTT groups lacked complete disposal paths. Their owners now release abandoned storage.
- **Server shutdown:** enqueue could race with closure, allowing input to arrive after cleanup. Admission is now sealed before workers drain their queues, and active Retry responses release their input.
- **Socket readers:** read failures could miss buffer returns, and failed batches could leave stale entries available for replay. Readers now explicitly transfer slot ownership, discard failed batches, retry zero-progress reads, and reclaim unread storage on termination.
- **Failed Initial construction:** a connection that never reached the protocol loop could retain its Initial packet and TLS/qlog resources without ordinary teardown running. Construction and registration failures now abort those resources without starting or waiting for that loop.

The address-based dial/listen helpers could also leave their internally allocated UDP sockets open when setup failed. They now close those sockets on failure, preserving caller-owned socket lifetimes. See the [receive ownership decision](docs/adr/0005-incoming-packet-lifetime.md), [completed receive lifetime plan](docs/adr/2026-09-08-incoming-lifetime-plan.md), and [socket cleanup #127](https://github.com/the-sarge/quic-go-fast/pull/127).

### Receive STREAM lifetime and cancellation

- **Wake readers after cancellation of a partially reset stream.** `CancelRead` now wakes both `Read` and `Peek` waiters that were blocked waiting for a remote reset's reliable prefix, preserving the established remote error and avoiding an extra STOP_SENDING. [#284](https://github.com/the-sarge/quic-go-fast/pull/284)
- **Release incoming frame storage on rejected dispatch.** STREAM frames skipped after an earlier frame error, rejected by stream lookup or validation, or discarded by the frame sorter's gap limit now surrender their owned storage. Metadata needed for qlog remains valid. [#286](https://github.com/the-sarge/quic-go-fast/pull/286)
- **Retire unread storage when reading becomes terminal.** Cancellation, effective remote resets, connection shutdown, and consumed FIN release the current frame and queued storage, including data beyond gaps. Partial-reset reliable prefixes remain readable until the reset becomes effective. Smaller reliable-prefix updates wake affected waiters; cleanup preserves EOF and error precedence. [#288](https://github.com/the-sarge/quic-go-fast/pull/288)

### HTTP/3 lifetime and allocation fixes

- **Keep active exchanges out of idle cleanup.** A pooled connection could be counted as idle while its response was still being consumed or its request upload was still running. Usage now remains held through both response consumption and asynchronous upload cleanup, including compressed responses. One lifetime owner also prevents duplicate input closure and preserves untouched input for supported stream-opening retries. See [exchange lifetime #136](https://github.com/the-sarge/quic-go-fast/pull/136).
- **Preserve replacement connections.** A delayed failure from an old attempt could remove a newer connection cached under the same hostname. Failure cleanup now evicts the entry only if it still belongs to that attempt. See [conditional eviction #138](https://github.com/the-sarge/quic-go-fast/pull/138).
- **Honor Extended CONNECT cancellation.** A request waiting for peer SETTINGS could remain blocked after its context was canceled. The wait now observes request cancellation. See [cancellation #129](https://github.com/the-sarge/quic-go-fast/pull/129).
- **Skip tracing-only header collection when no recorder exists.** Request and response decoding allocated logging fields even when nothing would record them. Collection is now conditional on a recorder, preserving decoded headers and complete tracing when enabled. See [header allocation #197](https://github.com/the-sarge/quic-go-fast/pull/197).

- **Make connection admission atomic with server shutdown.** `ServeQUICConn` and listener acceptance reserve the managed connection lifetime under the same lock that seals admission. Shutdown closes owned listeners without holding that lock, and overlapping shutdown calls track pending listener-close work. A connection accepted after sealing is closed instead of starting untracked work. [#290](https://github.com/the-sarge/quic-go-fast/pull/290)
- **Give normal response completion one owner.** The response writer now owns implicit Content-Length handling, buffered flush, and trailer completion. The server's request loop invokes that operation while retaining the existing hijack, panic, HEAD, and explicit-length behavior. This is a behavior-preserving consolidation with regressions at the server boundary. [#294](https://github.com/the-sarge/quic-go-fast/pull/294)
- **Clear consumed HTTP/3 DATAGRAM queue references.** Receiving a queued datagram now clears the consumed backing-array slot before advancing the queue. FIFO delivery and returned payload ownership stay unchanged; the queue no longer retains the consumed payload through that slot. [#298](https://github.com/the-sarge/quic-go-fast/pull/298)

### Logging and defensive fixes

- **Make optional tracing failures nonfatal and close resources reliably.** A qlog directory error could terminate the process, HTTP/3 shutdown could wait on a different producer group from the one doing the logging, and a flush error could skip closing the sink. Directory failures now return without exiting, recorder shutdown waits for the correct producers, and buffered sinks attempt close even after a failed flush. See [tracing resource ownership #185](https://github.com/the-sarge/quic-go-fast/pull/185).
- **Identify the code that produced a trace.** Qlog could report the upstream dependency version even when a fork replacement supplied the code, and interop linker flags targeted the wrong package. It now reports the replacement version, identifies local replacements, and honors explicit linker overrides through the corrected target. See [qlog provenance #192](https://github.com/the-sarge/quic-go-fast/pull/192).
- **Reject oversized close reasons safely on 32-bit systems.** Narrowing an untrusted reason length before checking its bounds could lead to an allocation panic. Validation now happens before narrowing. See [parser hardening #104](https://github.com/the-sarge/quic-go-fast/pull/104).
- **Reject short HTTP/0.9 requests safely.** The interop server sliced the request prefix without first checking its length, so short input could panic. It now checks the length before parsing. See [request validation #194](https://github.com/the-sarge/quic-go-fast/pull/194).

Malformed HTTP/0.9 request URLs now reset the response stream and return from the handler instead of proceeding with invalid parsed input. [#215](https://github.com/the-sarge/quic-go-fast/pull/215)

### Tests, diagnostics, and module packaging

Some existing checks gave misleading coverage: the unit CI step named for race detection omitted `-race`, version-specific self-suite fixtures could use the default QUIC version, and the leak guard looked for a retired connection-loop name. These checks now exercise the behavior they claim to cover. Fixtures that left transport/TLS workers and sockets alive now close resources and join their workers, preventing cleanup from spilling into later tests. The fork also adds regressions for the runtime changes above.

Random loss and corruption could produce permitted fault sequences that exceeded fixed test deadlines, while missing or cleanup-deleted diagnostics made those failures difficult to explain. Mandatory tests now use deterministic cases with bounded failure diagnostics and retained corruption captures; historical random stress remains opt-in. See [packet-loss tests #145](https://github.com/the-sarge/quic-go-fast/pull/145), [corruption tests #157](https://github.com/the-sarge/quic-go-fast/pull/157), and the [development journal](docs/DEV-JOURNAL.md) for the individual repairs and validation records.

This fork's archived audits, captures, profiles, and experimental patches inflated every Go module download despite being unnecessary to build or use the library. A nested module boundary now excludes that archive from the published module while preserving it in Git. Consumers get the [evidence index](docs/audit-evidence.md) with pinned repository links. See [module packaging #176](https://github.com/the-sarge/quic-go-fast/pull/176).

Additional maintained-test repairs make assertions observe packet metadata, routing, transfer/socket outcomes, applied HTTP/3 priorities, asynchronous completion, and graceful-shutdown deadlines. Packet-loss direction filtering and handshake-case names are corrected; blocked-data fixtures synchronize delivery and credit and use a bounded protocol-time regression. The unused packet-handler mock and its generator input are removed. See [#202](https://github.com/the-sarge/quic-go-fast/pull/202), [#204](https://github.com/the-sarge/quic-go-fast/pull/204), [#209](https://github.com/the-sarge/quic-go-fast/pull/209), [#211](https://github.com/the-sarge/quic-go-fast/pull/211), [#219](https://github.com/the-sarge/quic-go-fast/pull/219), [#221](https://github.com/the-sarge/quic-go-fast/pull/221), [#263](https://github.com/the-sarge/quic-go-fast/pull/263), and [#300](https://github.com/the-sarge/quic-go-fast/pull/300).

Automatic CodSpeed benchmarks are disabled pending a fork runner, and automatic ClusterFuzzLite runs are disabled pending toolchain compatibility. Neither is claimed as first-release certification. Ordinary unit benchmarks and deterministic integration checks remain in the active CI workflows.

### Recorded measurements

These are bounded observations from individual changes, not a benchmark of the entire current fork against upstream:

| Change | Recorded result | Scope |
| --- | --- | --- |
| DATAGRAM overflow admission | 1 allocation and 1152 bytes per rejected 1071-byte record → zero | Queue microbenchmark on macOS/arm64, Go 1.27.0 |
| DATAGRAM parser copy removal | About 47% lower receiver allocation per delivered record, with smaller CPU savings | Native Linux QUIC loopback workload; tail-latency uncertainty remains |
| HTTP/3 tracing-only header collection | Request fixture: 14 → 10 allocations; response fixture: 12 → 9 | Isolated decoder fixtures without collection; throughput unmeasured |
| Linux coalesced receive (GRO) | Receive syscalls per delivered datagram ×0.263; loopback throughput ×1.26 | Paired 10-round loopback bulk protocol on a Linux host |
| Windows segmented send (USO) | Send submissions per packet ×0.0815 (12.27 packets per submission); loopback throughput ×3.01 | Paired 10-round loopback bulk protocol on hosted `windows-latest` |
| Windows coalesced receive (URO) | Receive syscalls per delivered datagram ×0.098; 95.7% of datagrams coalesced; throughput ×1.34 | Two-endpoint KVM virtual-NIC transfer — single-host Windows traffic cannot engage URO |
| macOS batch send (sendmsg_x) | Send syscalls per packet ×0.126 (7.99 packets per submission); loopback throughput ×1.17 | Paired 10-round loopback bulk protocol, Darwin 25 arm64 |
| macOS receive batching (recvmsg_x) | Retired: syscall ratio only ×0.905 with throughput ×0.671 — predeclared gates failed | Same protocol shape; experimental path never merged |

The [evidence index](docs/audit-evidence.md) covers the earlier archive. The [datapath program](docs/adr/2026-09-11-datapath-offload-program.md) links the later offload protocols and results; the [journal](docs/DEV-JOURNAL.md) records subsequent validation and known limitations.

### Compatibility and validation boundaries

- Applications retain `github.com/quic-go/quic-go` imports and select the fork with an explicit main-module `replace` directive. A dependency's replacement does not propagate to its consumers. See [adoption instructions](README.md#use-the-fork).
- Tested CI platforms: Linux, macOS, and Windows on Go 1.26.x and 1.27.x for unit tests; Linux on both Go versions and macOS/Windows on Go 1.27.x for integration tests. Linux additionally runs race detection.
- Other targets accepted by the [cross-compilation script](.github/workflows/cross-compile.sh) are build-only. Android is excluded; iOS validation compiles library packages only. Cross-compilation does not establish runtime or datapath-offload support.
- This release ships library source, with no binary or container assets. Interop images are built in CI but are not published. No asset checksums are needed.
- macOS batching uses a qualified private syscall with fail-closed fallback and the `quic_go_no_private_syscalls` build-tag opt-out. Offload availability depends on the host; passing generic platform CI does not prove every offload is active.

### Known limitations

Intermittent macOS dial, initial-request, reconnection, and server-hotswap timeouts remain under investigation: [#151](https://github.com/the-sarge/quic-go-fast/issues/151), [#169](https://github.com/the-sarge/quic-go-fast/issues/169), [#188](https://github.com/the-sarge/quic-go-fast/issues/188), [#241](https://github.com/the-sarge/quic-go-fast/issues/241), and [#301](https://github.com/the-sarge/quic-go-fast/issues/301). The fixture repair for the Linux path-MTU convergence assertion in [#178](https://github.com/the-sarge/quic-go-fast/issues/178) is complete; the historical hosted probe-loss source remains unknown. Passing runs do not establish infrastructure causality or resolve these failures.

The historical server-first packet-loss investigation [#44](https://github.com/the-sarge/quic-go-fast/issues/44) remains open. Bounded deterministic recovery tests do not promise successful recovery within a deadline under arbitrary loss patterns. Evaluate the prerelease against application workloads before adoption.
