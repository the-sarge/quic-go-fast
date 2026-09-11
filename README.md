# quic-go-fast

A fork of [quic-go](https://github.com/quic-go/quic-go) focused on lower allocation costs, explicit packet buffer ownership, and transport correctness. The fork is based on **quic-go v0.62.0** and preserves its existing public APIs, import paths, and QUIC/HTTP/3 wire compatibility.

The work started with bulk transfer using 1071-byte application DATAGRAM records and has expanded to handshake recovery, stream writes, HTTP/3 exchange lifetimes, and shutdown cleanup. This README describes the changes shipped here relative to that upstream baseline. General QUIC and HTTP/3 usage is covered by the [upstream documentation](https://quic-go.net/docs/).

## What changed

### DATAGRAM receive allocation

- **Drop overflow before copying.** A full receive queue allocated and copied each incoming payload before discarding it, spending memory and CPU on messages it could not admit. Capacity is now checked first, preserving the existing 128-message limit, FIFO ordering, and drop-new behavior.
- **Remove the parser's extra payload copy.** The parser copied each DATAGRAM payload, then the receive queue copied it again. Parsing now borrows a bounded view of packet bytes during synchronous processing. The queue still makes the owning copy before delivery, so applications retain independent buffers with the existing lifetime contract.

These changes ship through the existing DATAGRAM APIs. The ring and head-index receive queues evaluated during development remain archived experiments; production retains the existing queue representation. See [overflow admission #9](https://github.com/the-sarge/quic-go-fast/pull/9), [parser copy removal #16](https://github.com/the-sarge/quic-go-fast/pull/16), and the [measurement evidence index](docs/audit-evidence.md).

### Large stream writes

Large `TryWriteAll` calls repeatedly copied already queued bytes when appending and the remaining unsent buffer when emitting each packet. Copying work could therefore grow quadratically with the amount of queued data. Pending storage now grows geometrically, and segmentation copies packet-sized prefixes while advancing the unsent tail, making total copying linear. Retransmission keeps bounded packet storage; cancellation copies the reliable prefix once so a small retained range cannot keep a large discarded suffix alive. Atomic admission and caller-buffer ownership are preserved. See [large stream write changes #190](https://github.com/the-sarge/quic-go-fast/pull/190).

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

### HTTP/3 lifetime and allocation fixes

- **Keep active exchanges out of idle cleanup.** A pooled connection could be counted as idle while its response was still being consumed or its request upload was still running. Usage now remains held through both response consumption and asynchronous upload cleanup, including compressed responses. One lifetime owner also prevents duplicate input closure and preserves untouched input for supported stream-opening retries. See [exchange lifetime #136](https://github.com/the-sarge/quic-go-fast/pull/136).
- **Preserve replacement connections.** A delayed failure from an old attempt could remove a newer connection cached under the same hostname. Failure cleanup now evicts the entry only if it still belongs to that attempt. See [conditional eviction #138](https://github.com/the-sarge/quic-go-fast/pull/138).
- **Honor Extended CONNECT cancellation.** A request waiting for peer SETTINGS could remain blocked after its context was canceled. The wait now observes request cancellation. See [cancellation #129](https://github.com/the-sarge/quic-go-fast/pull/129).
- **Skip tracing-only header collection when no recorder exists.** Request and response decoding allocated logging fields even when nothing would record them. Collection is now conditional on a recorder, preserving decoded headers and complete tracing when enabled. See [header allocation #197](https://github.com/the-sarge/quic-go-fast/pull/197).

### Logging and defensive fixes

- **Make optional tracing failures nonfatal and close resources reliably.** A qlog directory error could terminate the process, HTTP/3 shutdown could wait on a different producer group from the one doing the logging, and a flush error could skip closing the sink. Directory failures now return without exiting, recorder shutdown waits for the correct producers, and buffered sinks attempt close even after a failed flush. See [tracing resource ownership #185](https://github.com/the-sarge/quic-go-fast/pull/185).
- **Identify the code that produced a trace.** Qlog could report the upstream dependency version even when a fork replacement supplied the code, and interop linker flags targeted the wrong package. It now reports the replacement version, identifies local replacements, and honors explicit linker overrides through the corrected target. See [qlog provenance #192](https://github.com/the-sarge/quic-go-fast/pull/192).
- **Reject oversized close reasons safely on 32-bit systems.** Narrowing an untrusted reason length before checking its bounds could lead to an allocation panic. Validation now happens before narrowing. See [parser hardening #104](https://github.com/the-sarge/quic-go-fast/pull/104).
- **Reject short HTTP/0.9 requests safely.** The interop server sliced the request prefix without first checking its length, so short input could panic. It now checks the length before parsing. See [request validation #194](https://github.com/the-sarge/quic-go-fast/pull/194).

### Tests, diagnostics, and module packaging

Some existing checks gave misleading coverage: the unit CI step named for race detection omitted `-race`, version-specific self-suite fixtures could use the default QUIC version, and the leak guard looked for a retired connection-loop name. These checks now exercise the behavior they claim to cover. Fixtures that left transport/TLS workers and sockets alive now close resources and join their workers, preventing cleanup from spilling into later tests. The fork also adds regressions for the runtime changes above.

Random loss and corruption could produce permitted fault sequences that exceeded fixed test deadlines, while missing or cleanup-deleted diagnostics made those failures difficult to explain. Mandatory tests now use deterministic cases with bounded failure diagnostics and retained corruption captures; historical random stress remains opt-in. See [packet-loss tests #145](https://github.com/the-sarge/quic-go-fast/pull/145), [corruption tests #157](https://github.com/the-sarge/quic-go-fast/pull/157), and the [development journal](docs/DEV-JOURNAL.md) for the individual repairs and validation records.

This fork's archived audits, captures, profiles, and experimental patches inflated every Go module download despite being unnecessary to build or use the library. A nested module boundary now excludes that archive from the published module while preserving it in Git. Consumers get the [evidence index](docs/audit-evidence.md) with pinned repository links. See [module packaging #176](https://github.com/the-sarge/quic-go-fast/pull/176).

## What the measurements establish

These are bounded observations from individual changes, not a benchmark of the entire current fork against upstream:

| Change | Recorded result | Scope |
| --- | --- | --- |
| DATAGRAM overflow admission | 1 allocation and 1152 bytes per rejected 1071-byte record → zero | Queue microbenchmark on macOS/arm64, Go 1.27.0 |
| DATAGRAM parser copy removal | About 47% lower receiver allocation per delivered record, with smaller CPU savings | Native Linux QUIC loopback workload; tail-latency uncertainty remains |
| HTTP/3 tracing-only header collection | Request fixture: 14 → 10 allocations; response fixture: 12 → 9 | Isolated decoder fixtures without collection; throughput unmeasured |

The [evidence index](docs/audit-evidence.md) links the measurements and adoption decisions; the [journal](docs/DEV-JOURNAL.md) records subsequent validation and known limitations.

## Use the fork

The module still declares `github.com/quic-go/quic-go`. Keep existing imports and select the fork with a `replace` directive in your application's main module. For example, this Go-resolved pseudo-version pins commit `a534677ca097`, which includes the changes described above:

```sh
go mod edit -replace=github.com/quic-go/quic-go=github.com/the-sarge/quic-go-fast@v0.62.1-0.20260911050246-a534677ca097
go mod tidy
go list -m github.com/quic-go/quic-go
```

In the final command's output, the module and version after `=>` identify the selected fork; the left side shows the upstream requirement, which may be a placeholder version in a new application. Commit the resulting `go.mod` and `go.sum` changes in your application. A dependency's replacement does not propagate to its consumers: each application must select the fork explicitly. The module requires Go 1.26.0 or newer; see [go.mod](go.mod) for the declared requirement.

This replacement applies to every selected version of `github.com/quic-go/quic-go`; if another dependency expects APIs newer than v0.62.0, verify that the application still builds against this fork.

To return to upstream, remove the replacement and tidy:

```sh
go mod edit -dropreplace=github.com/quic-go/quic-go
go mod tidy
```

## Development and attribution

Report fork-specific issues in [the-sarge/quic-go-fast](https://github.com/the-sarge/quic-go-fast/issues). The [development journal](docs/DEV-JOURNAL.md) records merged work, evidence, and follow-ups; [CONTEXT.md](CONTEXT.md) defines the transport terminology used in the design documents.

quic-go-fast builds on the work of the quic-go authors and contributors. Code is licensed under the [MIT license](LICENSE). Upstream logo and brand assets have a [separate usage policy](assets/LICENSE.md).
