# External packet-I/O integration: fork execution companion

## Authority and status

Planning artifact; no implementation, benchmark or release has run. The user delegated technical design choices and requested the complete path through consumer adoption, rather than a restricted milestone with forgotten follow-up work. [ADR 0006](0006-explicit-external-packet-io.md) records the fork decisions; [ADR 0002](0002-adopt-through-module-replacement.md) retains application-owned replacement.

The single normative ledger and full slice contracts live in `GridSwarm/wiremux:docs/adr/2026-09-14-quic-packet-io-program.md`. The local review source is wiremux branch `codex/quic-socket-contract-design`; this fork companion is on `codex/wiremux-socket-contract-design`. Publication stage P00 must establish real durable document links and linked issues before code dispatch. These identifiers are not claims that the branches or documents are already published. This companion does not duplicate mutable completion state.

Source reasoning was pinned to fork `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7` and wiremux `b290852d134d7f45795219671951336d32477ae4`; implementation must recheck the touched surface on current default-branch code in dedicated worktrees. Existing audit archives remain frozen.

## What the fork owns

The fork owns explicit registration, platform probing, enabled-state diagnostics, batched submission, receive-format decoding, coalesced storage, managed endpoint leases and native fixed-peer enforcement. wiremux owns candidate gathering/probing, selected-path policy, the exact-resource permission grant, its checked wrapper callback, and its existing transfer/finality rules. A factory creating a native writer does not transfer socket ownership or authorize bypassing any wrapper.

The selected interface shape is two optional methods on `*quic.Transport`: `ConfigureExternalPacketIOV1(net.PacketConn, bool, func([][]byte, []byte, *net.UDPAddr) (int, error)) error` and `UDPBatchWriterV1(*net.UDPConn) (func([][]byte, []byte, *net.UDPAddr) (int, error), error)`. These are proposed new signatures; Q01 must compile and freeze them. Callback types must remain unnamed function types or true aliases so independently declared interfaces in upstream-building consumers match exactly. No root import under two module identities, fork-only subpackage in an upstream build, mandatory build tag, or separate public contract module is introduced.

Registration is explicit and immutable before every transport-initializing entrypoint. Its target is the exact stable pointer connection, with safe rejection of nil, typed-nil, unsupported identity, replacement, repeat and late registration. An absent extension or unsupported optimization keeps ordinary behavior; invalid registration is an error. Public fields may not be concurrently mutated by callers. The contract does not sandbox malicious in-process code that already holds descriptors.

The wrapper composes its policy around a private native writer, and the transport calls that checked writer. The writer borrows buffers synchronously, has one destination and OOB block per batch, returns exact accepted-prefix progress on nil error, and preserves terminal handling for unknown progress on non-nil error. Invalid counts fail closed. No per-message destination generalization, retained callback buffers, second retry queue, or Darwin syscall implementation copied into wiremux.

## Fork-owned slices

The dependencies and acceptance criteria in the normative program apply directly. This index names the concrete source context an implementer should load; it is not a replacement contract.

| Slice | Source context and result | Essential preservation |
| --- | --- | --- |
| Q01 | `transport.go`, `sys_conn.go`, current init and Close: register external packet I/O and freeze optional signatures | Existing upstream methods, initialization ordering, ordinary socket support and ownership |
| Q02 | `sys_conn_windows.go`, Windows USO tests: separate external segmented-send probing from receive-format permission | Wrapper WriteMsgUDP, no new receive mutation, standard poller/deadlines |
| Q03 | `sys_conn_oob.go`, Linux GRO setup, coalesced delivery/storage: allow explicit disposal-guaranteed receive permission | Wrapper ReadBatch, full metadata, split-before-parse, buffer terminal paths |
| Q04 | `sys_conn_windows.go`, URO tests: enable permissioned external receive coalescing | Windows metadata restrictions, native two-endpoint engagement, USO and ordinary paths |
| Q05 | `send_conn_sendmsg_x_darwin.go`, syscall qualification, `send_queue.go`: reusable helper and checked-wrapper submission | Known prefix/unknown progress, qualified-host policy, opt-outs, MTU attribution |
| R01 | Receive-format owner plus a new managed endpoint/lease module: persistent normalization and exclusive lease lifecycle | Ordinary datagram semantics, endpoint/lease close distinction, no raw detach promise |
| R03 | Native Linux/Windows handback semantics and bounded experiment | No unbounded drain, unrelated-data discard or inferred empty queues |
| P01 | `transport.go`, socket reads/writes, stateless queues, connection path APIs: fixed-peer admission before initialization | Normal defaults, packet emission/storage ownership, authentication separation |
| E01/E02 | Existing diagnostics and qualified native/full-Wire harnesses | Report actual engagement; no speedup inference from a disabled path |
| L01 | Release runbook, module archive and actual consumer graph | Immutable version, exact-head verification, honest support and rollback |

Q01 provides a working ordinary native writer fallback so its interface is usable without an unmerged Darwin successor. A permission registration never enables a platform implementation before that implementation's own gate. Add capability diagnostics to the existing tracing/reporting route: distinguish requested permission, platform availability, effective enablement, inactive reason and actual exercised counters. Avoid a new monitoring service or public API solely to inspect private test state.

## Managed reuse is a required destination

The managed packet endpoint retains ownership of the native socket and receive interpretation for its lifetime. Ordinary reads return single datagrams even when residual kernel data was coalesced. A packet-path lease spans ordinary gathering/STUN/probing and the joined transition to QUIC; it is the per-attempt resource handed to wiremux. Lease Close joins its I/O and releases that interval, whereas endpoint Close disposes the native socket and interrupts any lease. No two simultaneous leases and no parent/raw reads during a lease. Lease reads support ordinary establishment datagrams before optimized QUIC receive handling begins.

The endpoint owns bounded split storage and logical deadline restoration. It must account for failed initialization, canceled reads/writes, parent close during handback, repeated lease acquisition and queued coalesced data. Old user-space QUIC-owned data is disposed according to the ended lease's contract, not replayed into a subsequent lease. A fatal endpoint cannot claim successful ordinary-read readiness. Existing raw borrowed sockets remain unchanged and do not receive coalescing merely because they expose OOB operations.

R03 must settle raw-socket return explicitly. Restoring one socket option does not establish native queue format or absence of pending aggregates. If no bounded safe procedure exists, document that limitation and the supported managed endpoint; do not leave a TODO. If a procedure is viable, create its explicit implementation/certification children under R03 before it can finish. The managed endpoint cannot be dropped merely to close the program early.

## Native fixed-peer scope

The mode is bound to one transport before any incoming packet, not to an accepted connection. It deep-copies a standard-library peer address and preserves the consumer's declared IPv4/mapped and IPv6-zone semantics. No later mutation or different target for another Dial on that transport is permitted. It must cover all in-contract ingress and egress, including Initial, Retry, version negotiation, reset, ordinary/non-QUIC transport operations, batches, segmentation, alternate paths and teardown.

Use one central peer-policy representation consumed by actual shared send/receive owners; do not add scattered ad hoc address checks. A foreign source with a valid connection ID still fails address admission. Keep existing TLS and packet protection; an address is not identity. The fork default remains unrestricted. wiremux's P02 may remove only its own filter on qualifying direct native UDP inputs after native enforcement is acknowledged and validated; caller policy wrappers and TURN composites remain opaque.

## Evidence, publication and completion

Every slice receives its current normative contract, this source index, relevant invariants and a bounded governing diff. One initial review and at most one replacement review, with independent finding disposition, is the budget. The shared review/contract-closure baselines compose with [the repository overlay](../REVIEW-LOOP.md), [maintained conventions](../agents/conventions.md) and existing ADRs. The fork's existing draft workflows run normally; do not introduce wiremux's CI naming scheme. Docs-only validation follows the overlay. Code changes use the slice's focused tests, affected packages/vet, module tidiness and applicable native/race/hosted gates on the exact candidate.

The program's finite evidence matrix covers binding/authority, lifecycle, batch progress, receive metadata, lease return and native peer admission. Tests are ordinary verification aids, not a new maintained analysis product. Native Linux/Windows/macOS runs qualify OS behavior; a fake successful probe or cross-compilation is not equivalent. E01 freezes the shared protocol; E01-L/W/D own independent platform baselines so an unavailable host blocks only its branch. Performance protocols are committed before measurements, report ten paired rounds per declared cell, fix their metric and adoption/noninferiority thresholds, and retain failures. Native-correct implemented capabilities can finish with explicit opt-in support and ordinary defaults when measured benefit is insufficient; correctness or unperformed native qualification cannot be waived. Existing initial-fork measurements are context, not the assembled product's certification.

Follow [the release runbook](../runbooks/release.md) only after assembled qualification. Publish real immutable versions, verify the actual module archive and consumer selection, and retain the independent upstream build. wiremux release and consumer main-module replacements follow as separate program rows. Every consumer has a named adoption, compatibility or reasoned non-applicability disposition; a dormant repo is not silently called migrated.

P00 must create the full issue graph before implementation, including R01/R02/R03, P01/P02, final qualification, both releases, consumer children and Z01 closeout. Every merged slice updates the one program ledger and pointer-based trackers. No stage PR closes the program parent. Z01 closes only after mandatory deliverables and each conditional design disposition have evidence, the supported platform/socket matrix is published, and rollout/rollback is verified.
