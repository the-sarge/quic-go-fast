# External packet I/O design contract

**Status:** Accepted; not implemented. Fork-owned contract supporting [its slice plan](2026-09-15-external-packet-io-plan.md). The wiremux adapter details are boundary constraints; wiremux implementation is owned by its plan.

## Destination

Applications can benefit from quic-go-fast on externally created UDP sockets without bypassing packet policy or confusing permission with ownership. wiremux keeps its same-socket candidate gathering, authenticated probing, selected-path handoff and exact-resource teardown. The ordinary build works with upstream quic-go. Fork users gain supported wrapper-preserving offloads; reusable sockets gain a safe managed endpoint; native fixed-peer enforcement is implemented and evaluated for selective wrapper removal; real consumers receive qualified releases and rollback instructions.

First-stage acceleration is a milestone, not program completion. Reusable endpoints, fixed-peer enforcement, consumer adoption and closeout are required tracked work. General raw-socket handback after coalescing is explicitly excluded from the supported architecture unless its dedicated feasibility gate establishes a bounded safe procedure. A negative result there must retain the delivered managed alternative and explain the limitation.

## Decided architecture

| ID | Decision and reason | Consequence |
| --- | --- | --- |
| D01 | Keep upstream quic-go buildable and functional; fork integration is optional | No ordinary wiremux source may require a fork-only symbol or subpackage |
| D02 | Enable offloads through the existing wrapper first; evaluate native filtering separately | Useful acceleration does not wait for relocating selected-peer enforcement |
| D03 | Keep socket creation, discovery, authenticated probing and path selection with wiremux | QUIC uses the same selected socket; no fresh post-probe socket and no QUIC-based replacement for wiremux establishment |
| D04 | Register packet-I/O permission explicitly on the exact QUIC transport before initialization | `SyscallConn`, socket type, method promotion and eventual success do not grant permission |
| D05 | Keep close ownership with its existing owner; grant configuration permission separately | No changes to commit, any-non-nil-stream, caller reuse or exact-resource disposal rules |
| D06 | Initially permit receive coalescing only when terminal disposal is guaranteed and the adapter supports its receive format | Internal or immediately transferred native sockets can qualify; borrowed raw sockets remain ordinary-receive |
| D07 | Complete reusable acceleration with a lifetime-stable managed packet endpoint and exclusive per-attempt leases | The endpoint normalizes residual coalesced reads; raw socket handback is not assumed safe |
| D08 | Use small versioned structural extensions with standard-library types, backed by implementation inside the fork | No mandatory build tag or separately versioned interface/helper module in this program |
| D09 | Offer the fork's native batch writer to an explicitly participating wrapper; register that wrapper's checked writer | Kernel code stays in the fork; unknown caller wrappers are never unwrapped |
| D10 | Native fixed-peer policy is immutable and transport-wide, installed before Dial/Listen | Filtering covers pre-connection traffic and stateless responses, not just an accepted connection |
| D11 | Require correctness plus observed native engagement before claiming acceleration | Cross-compilation, successful socket options and published native-socket results do not establish wrapper performance |
| D12 | Adopt per executable/main module using explicit immutable fork versions | A wiremux or codemux replacement does not propagate to applications |
| D13 | Keep one program ledger with named downstream stages and blocking edges | No child or early milestone closes the parent; unqualified platforms stay visibly blocked |

Three interface designs were compared. Automatic socket capability markers were smallest but could inherit authority through embedding. An independently versioned packet-I/O module offered broad extension at the cost of another release contract. Explicit transport registration plus a native writer factory concentrates the implementation, preserves upstream compilation, and avoids both costs for the current consumers. Revisit a standalone helper module only if a concrete future consumer needs packet I/O without the QUIC module; it is not a hidden prerequisite here.

## Interface and ownership contract

### Optional fork extension

The selected initial shape is two methods on the fork's `*quic.Transport`: one registers external packet-I/O permission and a policy-preserving batch callback; the other creates a native batch writer for a known `*net.UDPConn`. Names below are proposed new interfaces, not existing callable code. Q01 freezes names and exact signatures after compiling the two-module examples; it may simplify names without changing this contract.

```go
// Illustrative structural interface; every parameter is a standard-library type.
type externalPacketIOV1 interface {
    ConfigureExternalPacketIOV1(
        conn net.PacketConn,
        allowReceiveCoalescing bool,
        sendBatch func([][]byte, []byte, *net.UDPAddr) (int, error),
    ) error
    UDPBatchWriterV1(
        conn *net.UDPConn,
    ) (func([][]byte, []byte, *net.UDPAddr) (int, error), error)
}
```

wiremux discovers the extension with a structural assertion on its concrete upstream-compatible transport value. If absent, it retains its existing behavior. Fork-only named parameter types must not leak into the signature; aliases are allowed only where they preserve Go type identity. Do not import the root QUIC module under both repository paths. Use separate fixture main modules to validate upstream and fork selections, since a Go replacement applies to the whole selected module graph.

Registration binds one stable pointer connection identity to one transport before any initializing entrypoint, including Close or WriteTo. It rejects repeat registration, changed `Transport.Conn`, registration after initialization, nil/typed-nil targets, and unsupported non-pointer identities without panicking on interface comparison. Ordinary unregistered packet connections keep existing support. Callers are trusted in-process code; this is an explicit capability contract, not a sandbox against malicious code holding raw descriptors. Post-configuration mutation or concurrent direct use of public fields remains invalid caller behavior and is checked at supported initialization transitions, not promised to be race-proof against arbitrary mutation.

No optimization activates solely because a method was inherited by embedding. The registering adapter must preserve the entire known wrapper chain. A wrapper added outside an already configured object requires a new binding. Opaque wrappers retain ordinary I/O. A valid registration may request an optimization that the OS cannot support; that is a reported fallback, distinct from invalid permission/configuration, which is an error.

### Permission from wiremux

`upgrade_plan.go` and its packet lifecycle already know the origin and eventual disposition of a direct socket. Add a narrowly scoped immutable permission record attached to the prepared attempt and explicitly handed to the exact built-in QUIC session. It identifies the exact direct packet resource and a disposal guarantee; it is not another close owner or a global socket registry. Carry this immutable grant through the existing attempt context with a private key and constructor in an internal package, only when the configured transport has the exact built-in value/pointer identity. NewSession retains the grant and Dial/Listen verifies the exact resource before registration. Preserve the context's cancellation and deadline behavior; do not store mutable lifecycle state in the context. Do not carry authority through an arbitrary wrapper's promoted method or reclassify a custom transport as built-in. This avoids adding a field to the public two-field `transport/quic.Transport` struct, which could break existing unkeyed literals; its public layout and Session interfaces remain unchanged.

The root default transport and explicitly configured built-in value/pointer must receive equivalent permission while retaining configured keepalive/idle fields. Custom transports receive the original socket and existing contracts. Lower-level callers without root authority default to no receive-format mutation. TURN retry must not inherit a direct-socket grant; it has a different resource owner and packet profile. Session/Dial/Listen interfaces remain unchanged.

Permission can be activated only after STUN/probe readers have joined and the transport has exclusive packet I/O. Root origin is necessary but not sufficient: native capability, exact resource identity and complete reader handoff are also required. When setup fails after mutation, the existing owner still disposes the socket on every terminal path. Session.Close continues to interrupt and release session state without silently closing a borrowed socket. Audit fallible operations after socket setup as well as ordinary transport Close.

### Packet I/O and batching

Retain wrapper-provided ReadBatch/ReadMsgUDP and WriteMsgUDP as the receive and segmented-send paths. Large buffers, ancillary metadata, source addresses and truncation flags must survive filtering. Keep coalesced splitting before QUIC packet parsing and the existing buffer ownership rules. Receive coalescing permission is independent of whether segmentation can be probed without mutating the socket.

The native batch helper is constructed only with a known underlying UDP socket and remains private to an explicitly participating wrapper. It neither owns socket Close nor starts a reader. The wrapper checks destination policy before calling it, then registers its checked callback. Native qualification, syscall encoding, scratch storage and fallback remain in the fork. No descriptor unwrapping around an unknown wrapper, copied Darwin syscall implementation in wiremux, second send queue, or publicly accessible bypass writer is introduced.

A batch has one destination and one ancillary-data block, distinct complete UDP payloads and synchronous borrowed buffers. `(n, nil)` means precisely the accepted prefix; only the suffix may fall back. Zero progress with nil error takes the existing single-packet fallback, never a spin loop. Any non-nil batch error is terminal under the current unknown-progress contract; uncertain data is not resent. Counts outside `[0, offered]` are fatal. Preserve packet-specific message-size feedback, error identity, cancellation and group-buffer disposal. Do not introduce per-message destinations or asynchronous buffer retention without a separate demonstrated need.

### Complete reusable-socket outcome

Deliver a managed packet endpoint whose owner retains the native socket for its lifetime. It supports ordinary datagram use and one exclusive packet-path lease at a time, spanning candidate gathering, STUN, authenticated probing and QUIC; it does not support unsynchronized parent/raw reads alongside that lease. Lease packet methods initially serve ordinary establishment datagrams, and optimized QUIC receive handling starts only after those readers join. Normalization remains installed across leases, so a read after QUIC stops can safely interpret a kernel-queued coalesced receive. Split-buffer storage and the endpoint's logical deadlines remain owned by that endpoint. The ordinary packet interface never returns multiple datagrams as one; any optimized coalesced representation remains inside the explicitly registered QUIC I/O seam.

A lease is the exact per-attempt `net.PacketConn` supplied to wiremux. Closing it joins its I/O and releases the lease; it does not close the parent endpoint. The caller deliberately creates this resource and can transfer it with the existing immediate-transfer option, so setup failure, fallback and Wire.Close all release the exact supplied lease. Endpoint.Close terminates the native socket and interrupts an active lease. Do not silently change Close semantics on an existing raw `WithPacketConn` input or pass the parent endpoint as if it were the disposable lease.

The endpoint admits ordinary reads only after the lease's readers/writers have joined, restores its own logical deadlines, and preserves one-datagram semantics, address metadata and bounded retained storage. User-space data already consumed for an ended QUIC lease may be discarded under its documented UDP/session semantics; do not replay consumed data into a later lease or promise reliable delivery across handoff. Endpoint failure is terminal and explicit rather than a successful return to unsafe reads. The selected-peer filter is per lease, not permanently attached to the reusable parent.

Legacy raw borrowed sockets keep receive coalescing disabled. R03 must explicitly settle whether any native raw-return protocol can be supported: changing an option back does not establish that queued aggregates disappeared, and draining indefinitely or silently discarding unrelated traffic is not a valid restoration contract. Managed reuse is mandatory delivery; a supported raw-return variant is conditional on the bounded evidence gate. A negative raw-return finding is a documented limitation with a working managed alternative, not an indefinitely deferred feature.

### Later fixed-peer enforcement

Add an optional immutable fixed-peer mode to one QUIC transport before it processes its first packet. Use a standard-library address representation and deep-copy it. Preserve wiremux's existing IPv4/mapped-address and directional IPv6-zone matching semantics, including explicit invalid-input rejection; do not replace them with a superficially equivalent equality test. Ordinary fork transports retain existing behavior.

Central enforcement must cover receive admission before parsing/routing, Initial/Retry/version-negotiation/stateless-reset traffic, all connection and transport writes, batch and segmented sends, path creation/probing/switching, rebinding, preferred addresses, and terminal output. A known connection ID does not authorize a different source. TLS fingerprint authentication remains necessary and unchanged. This mode authorizes an address, not a peer identity, and is not an authenticated migration design.

Only a successfully acknowledged supported mode can replace wiremux's own selected-peer wrapper. Initially admit direct concrete UDP sockets only. Never unwrap a caller's policy wrapper or a TURN composite to reach that mode. Keep the upstream wrapper path indefinitely. P02 must select removal or retention with measured evidence; neither the mere availability of the setter nor passing compilation authorizes removal.

## Material-risk matrix and finite evidence

Representation domain: existing QUIC parsing remains owned by quic-go; this work owns explicit pointer-bound registration, supported standard-library UDP addresses, batch prefix counts, OOB/coalesced metadata through the platform decoder, and the endpoint lease state machine. Arbitrary malicious in-process descriptor users, arbitrary concurrent mutation of public transport fields, arbitrary wrapper transparency, and new wire grammars are outside the guarantee. Runtime enforcement is universal within these supported representations; tests provide finite regression evidence, not exhaustive network proof. Native performance and raw-return investigations are example-level evidence on qualified hosts.

Contract closure is triggered for authority, selected-peer enforcement, buffer/lifecycle disposal and lease handback because materially different entry/exit paths can violate them. It is not recursively applied to the fixtures, docs or measurement harness. Use the following semantic classes, crossing axes only when behavior changes:

| Owner | Classes requiring distinct evidence | Expected result |
| --- | --- | --- |
| Registration | absent; valid before-init; duplicate/late; swapped target; nil/typed-nil/non-pointer; opaque outer wrapper | Ordinary fallback or explicit accepted binding; invalid binding rejected without authority leakage |
| Wiremux permission | internal/transferred native; borrowed raw; managed lease; custom transport; TURN composite; probe still active | Only exact permitted resource can change receive format; no socket-owner substitution |
| Setup/teardown | before mutation; after mutation before loop; listener before commit; any-non-nil stream with error; cancellation during I/O; aggregate Close | Existing ownership truth table and one disposal owner; lease case returns safe endpoint |
| Receive owner | ordinary/coalesced; selected/foreign; mixed IDs; invalid size or metadata; truncated input; partial batch error; retained sibling | No filter bypass, merged QUIC datagrams, stale replay or abandoned owned storage |
| Send worker | full/short/zero prefix; negative/over-count; unknown progress; wrong peer; changed OOB class | No uncertain resend or duplicate close owner; metadata and per-packet error attribution preserved |
| Endpoint owner | ordinary/leased/returning/terminal; repeated lease; simultaneous acquisition; parent close; pending coalesced data | Exclusive I/O and normalized post-lease reads, explicit terminal failure |
| Fixed-peer owner | listener pre-connection; established known CID; stateless response; normal/batched send; alternate path; IPv4/mapped/IPv6-zone | Foreign endpoint cannot establish, redirect, or receive transport output |

Ordinary tests: one representative positive case per behavior and negative case per distinct failure mode; reuse existing meaningful tests. At most one guard-bypass demonstration per new authority/filter/lease owner when it is the clearest evidence, otherwise a red regression is sufficient. No open-ended fuzzing, stress-count escalation, repeated flaky retries or testing-framework project. Existing mandatory repository gates remain mandatory.

Native correctness: Linux, Windows and qualified macOS; IPv4 and IPv6-minimum path classes where behavior differs; direct native and participating wrapper paths, disabled/unavailable modes, and TURN fallback preservation. URO requires separate endpoints. Native helper changes preserve current architecture/OS qualification lists and opt-outs; extending those lists needs its own named evidence row. Loopback can validate local semantics but cannot establish physical capacity or cross-host receive coalescing.

Performance: reuse the maintained harness and publish the protocol before measurement. Budget ten paired baseline/candidate rounds per declared performance cell, 5 s warmup and 10 s measured traffic, profiling in separate captures. Primary full-Wire cells per platform are reliable stream, 1071-byte Datagram load, and mixed traffic on a qualified direct two-endpoint path. Retain the existing 100 Mbit/s paced workload as a resource/latency control; declare a separate capacity workload before running it. A bounded pilot of at most five offered rates per platform may select that workload, with pilot data retained and excluded from adoption statistics. One fixed impairment cell per platform uses a declared 20 ms added RTT and 0.1% packet loss where the venue can implement and verify it; otherwise that cell is explicitly blocked, not simulated as physical evidence. TURN gets correctness plus one paced preservation cell; no claim that inner QUIC offloads accelerate the relay composite.

Declare the primary metric before each comparison. Default-enablement requires at least 10% lower CPU per delivered byte at fixed load or at least 10% higher receiver goodput in the capacity cell, with a paired 95% interval supporting that threshold. A separately targeted allocation improvement uses the same 10% bound and cannot be reported as throughput. Require no correctness failure, no new unexplained delivery deficit, and paired upper bounds below 5% regression for p99 latency and sampled resident memory; a baseline too noisy to decide is inconclusive for default-enablement. P02's maintainability path may use the same noninferiority gates without a speedup claim. A correct implemented capability with native engagement and a completed finite measurement may finish with explicit opt-in support and ordinary defaults when these benefit bounds are not met; record the measured limitations and whether a specific remediation merits a named child. This does not block unrelated later stages or claim a speedup. Correctness failures and absent native qualification remain blockers. Keep one unchanged-code measurement campaign per protocol; a new campaign requires a material fix or a distinct recorded hypothesis, not a search for passing numbers.

Artifact classes: runtime extensions/endpoints and observable diagnostics are shipped behavior; permission/filter/progress/disposal enforcement is required safety enforcement; ordinary tests and existing measurement facilities are verification aids; this plan, receipts and trackers are process metadata. No new maintained analyzer or verification framework is a blocking deliverable. The finite regression/native evidence is the certification boundary, not a demand to prove the measurement harness complete.


## Managed endpoint and fixed-peer refinements

The managed factory creates a fresh socket from network/local address; it does not adopt or detach a pre-existing native socket. R01-A owns ordinary exclusive leases, R01-B owns explicit QUIC binding and generation-safe joining, and R01-L/W own persistent platform normalization. A lease generation is checked by packet operations, deadline operations and any native writer closure. Transport.Close joins reception but is not sufficient to establish handback: the endpoint revokes new operations and joins active I/O before another generation is admitted. No stale queued write may target a later lease.

The managed factory and binding signatures are fixed in the fork slice plan. R02 must preserve capability only for factory-proven leases through a checked adapter; ordinary arbitrary wrappers remain opaque. A fixed-peer transport preserves its local socket as well as remote address: fixed-origin connections reject AddPath and unrelated connections cannot attach to fixed target transports.

Managed registration is an alternative to ordinary external registration and claims the same single immutable configuration slot; do not call both setters on one transport. A factory-proven lease additionally exposes `WriteBatchV1([][]byte, []byte, *net.UDPAddr) (int, error)` through a standard-type structural assertion. The endpoint constructs its private native writer internally and the lease method applies active-generation accounting around every submission. The wiremux managed wrapper composes its selected-peer check around that lease method, then passes the checked callback to managed registration. No raw UDP pointer is exposed. Methods inherited by an unknown wrapper confer no provenance.

Wiremux provenance enters through its explicit WithManagedPacketEndpoint facade option, which carries the internal owner receipt and acquires the exact attempt lease during preparation. Raw packet options do not acquire managed authority. The new option is mutually exclusive with raw socket options and requires the exact built-in transport before acquisition; custom transport behavior remains unchanged. P02 compares one declared native platform and keeps the wrapper default until E02 records per-platform decisions.

Batch callbacks may be invoked concurrently by different connection send workers on one transport. Factory writers are concurrency-safe: each owns synchronized scratch or per-call storage, not shared unsynchronized per-connection scratch. Registered wrapper callbacks must support concurrent calls and borrow each call's buffers only until return. A managed batch call participates in generation checks and active-I/O joining, including while waiting for internal writer serialization. Native writer synchronization never becomes a second retry queue. One two-connection/concurrent-callback race regression and one lease-close-during-batch case are the terminating extra evidence for this distinction.
