# Managed ECN architecture handoff audit — 2026-09-22

**Program:** `QGF-ECN-20260922`
**Status:** Passed; author audit and focused independent verification complete
**Source revision:** `03d92a3ac1d3e95be3c41de80b03257121a56fb2`

## Provenance and authority

The user explicitly invoked `$architecture-handoff` after `$implement-architecture-slice` correctly rejected issue #455 as an unmarked triage brief. The target OmniFocus parent is `ckO4QjD_Y7P`, the #354 umbrella. Existing issues #455–#459 and their tasks are retained as platform-track identities; they are not implementation child contracts. The current plans are the normative source and no implementation is dispatched by this packaging run.

The accepted product direction comes from #354 and platform briefs #455–#459: Linux first, Darwin and FreeBSD qualification using the accepted managed representation, OpenBSD and Windows independent feasibility, no partial capability, no raw socket/public metadata authority and no throughput campaign. Code inspection resolved technical ownership and graph questions without a new product choice.

## Existing-work census

GitHub returned no open ECN implementation PR for #455–#459. The local `codex/triage-354` branch contains commit `928648a9b3fad855482cbccd6a84b9232e51ba98`, which adds only three glossary terms and ADR 0007. It is not reachable from `origin/main` and is not an implementation baseline.

| Existing work | Disposition and precise relation |
| --- | --- |
| `codex/triage-354` glossary and ADR draft | Rework into this package. The terms and decision are retained after source verification; the unmerged commit is superseded and no issue pointer may name it as the plan commit. |
| Merged managed endpoint, registration, Linux/Windows normalization and lease-binding work | Retain. These establish endpoint socket/generation ownership, wrapper composition, cleanup and the current basic-connection gap. They do not establish managed ECN. |
| Merged native OOB code on Linux/Darwin/FreeBSD | Retain as platform representation candidates. Its direct-socket ECN behavior is evidence, not a managed capability claim. |
| Merged Darwin sendmsg_x and ordinary registered batch callbacks | Retain their progress/error owners. Neither establishes managed receive correlation or full platform qualification. |
| Merged Windows packet-info, USO and URO | Retain and explicitly separate from ECN. `windowsConn` still reports ECN false and rejects marked sends. |
| OpenBSD buffer-specific code and generic no-OOB connection | Retain. There is no existing OpenBSD ECN representation to grandfather. |

No unresolved review history, failed ECN implementation branch or retain/rework implementation disposition exists. Review history from unrelated managed/offload PRs is not loaded into dispatch contexts.

## Source findings that govern the graph

- The endpoint already owns the native socket, retained receiver, active operations and lease generation, and its generation guard is central (`managed_packet_endpoint.go:63-172`).
- Public managed `ReadFrom` releases the native packet after copying payload/address, so ECN metadata cannot reach QUIC through the current outer wrapper (`managed_packet_endpoint.go:174-213`).
- Managed ordinary `WriteTo` has no ancillary channel, while the generation-checked batch method accepts OOB (`managed_packet_endpoint.go:216-237`).
- Managed registration preserves an exact lease plus caller outer wrapper, but transport initialization wraps that outer value as `basicConn`; marked writes are unavailable there (`external_packet_io.go:88-131`; `transport.go:387-409`; `sys_conn.go:127-167`).
- The registered batch callback already carries checked destination policy, shared ECN OOB, definite-prefix progress and terminal errors (`send_conn.go:138-168`; `external_packet_io_oob.go`).
- Linux, Darwin and FreeBSD share the native OOB parser/writer, but only Linux managed setup exists and it currently retains the reader only with GRO (`sys_conn_oob.go`; `managed_packet_receive_linux.go`). Darwin and FreeBSD therefore depend on one central managed metadata adapter rather than independent platform adapters.
- OpenBSD uses `basicConn`; Windows explicitly omits ECN and panics on a marked send. Their native representation owners are unresolved, so feasibility must precede implementation (`sys_conn_no_oob.go`; `sys_conn_windows.go:24-250`).

## Audited graph and slice decisions

| Slice | Blocked by | End-to-end delivery | Owner after merge | Temporary seams | Blast radius / untraced effects | Context fit | Split / merge result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| L1 | None | Complete Linux managed receive/send ECN through direct and selected-peer registrations, fallback, reuse and cleanup | Endpoint native reader/writer + one managed adapter; wrapper policy and send worker unchanged | None | Managed endpoint/registration/OOB Linux/tests; none accepted untraced | One adapter and table-driven native evidence fit one fresh context | Receive/send or direct/wrapped split would create half-capability/duplicate owner; other platforms must not merge |
| D1 | L1 | Native Darwin supported or explicit unsupported qualification | Existing Darwin OOB/sendmsg_x + L1 adapter | None | Darwin setup/tests/status only; none accepted untraced | Reuses merged seam; native tables fit one context | Receive/send cannot split; separate host/build tags prevent merge with L1/F1 |
| F1 | L1 | Native FreeBSD supported or explicit unsupported qualification | Existing FreeBSD OOB/ordinary callback + L1 adapter | None | FreeBSD setup/tests/status only; none accepted untraced | Reuses merged seam; native tables fit one context | Receive/send cannot split; separate host/build tags prevent merge with L1/D1 |
| O1 | None | Native feasible or unsupported representation disposition | No runtime owner; ADR/status/receipt only | Temporary probe retired in O1 | Docs and disposable native probe; no runtime effect | Six bounded probe classes fit one context | Feasibility must split from implementation because representation is unknown; no L1 dependency for the API question |
| W1 | None | Native feasible or unsupported representation disposition | No runtime owner; ADR/status/receipt only | Temporary probe retired in W1 | Docs and disposable native probe; no runtime effect | Six bounded probe classes fit one context | Feasibility must split from implementation because representation is unknown; packet-info/USO/URO do not justify merging |

L1, O1 and W1 are the genuine parallel frontier. D1 and F1 are blocked by L1 because their plans explicitly reuse the central managed metadata correlation/route/capability owner. No convenience-only edge remains. O1/W1 supported findings require a scoped handoff update; pre-creating speculative implementation slices would fail the representation gate.

## Representation, artifact and closure audit

L1 declares the exact factory lease plus direct or synchronous policy-wrapper/callback domain, existing authoritative kernel control-message parser, universal guarantee within that typed/correlated domain, example-level arbitrary-wrapper preservation and finite evidence. Its contract closure is triggered by material congestion/policy/lifecycle consequences across independently reachable family, route, metadata-validity and generation states. The matrix names one enforcement seam and bounded guard mutations.

D1 and F1 inherit the accepted adapter representation only after L1 and require independent native evidence. Their closure matrices apply only to a supported result; unsupported removes partial capability. O1 and W1 remain example-level feasibility and do not trigger runtime closure because they accept no shipped invariant. A feasible result must return to the representation gate before implementation.

Runtime capability is shipped behavior; exact correlation, generation, route and cleanup checks are required safety enforcement. Tests and temporary probes are verification aids, not maintained products; receipts, plans, issues and journals are process metadata. No recursive harness-completeness obligation or disproportional platform/repetition budget was admitted.

## Rejected graph alternatives

- Reusing #455–#459 directly as implementation children was rejected because each issue is a platform-track brief without an exact plan pointer and OpenBSD/Windows have unresolved representations.
- One cross-platform implementation slice was rejected because it requires multiple native hosts, build-tag owners and unresolved Windows/OpenBSD representation decisions and cannot remain independently green.
- Linux receive then send slices were rejected because neither may safely advertise managed ECN alone and both would mutate the same adapter/capability owner.
- Blocking all work on Linux was rejected as convenience ordering: OpenBSD/Windows native feasibility does not depend on the Linux managed implementation.
- Preplanning OpenBSD/Windows implementation was rejected because a handwritten or guessed representation would fail the shared representation gate.

## Independent design review and dispositions

RAS consideration run `20260922T043238-b66fd2ea8db6ce2f7263ad17` reviewed the program index, five track plans, ADR 0007 and this audit against source revision `03d92a3ac1d3e95be3c41de80b03257121a56fb2`. Codex Astra, Codex Sol, Claude Opus and Claude Fable completed both review/adjudication rounds; Grok failed before review with `structured delivery execution: process_failed`, which is recorded as unavailable coverage rather than hidden or retried. The synthesis accepted ten actionable process-contract findings. Every finding is fixed in the plan package; none changes the five-slice graph, product ceiling or approved frontier.

| # | Accepted finding | Disposition | Plan change / evidence | Verification at that round |
| ---: | --- | --- | --- | --- |
| 1 | Singleton marked output could lose message-size, permission or terminal errors through the existing batch result shape. | Fixed | L1 defines one operation-scoped checked-singleton route and central result translator for success, native error and invalid callback results while preserving first-send retry and batch semantics. | Pending RAS verify |
| 2 | Wrapper receive authority and exact returned-datagram correlation were underspecified. | Fixed | Program, ADR and L1 make configuration an authority-bearing synchronous exact-forwarding declaration and require current generation, same caller buffer, returned range and address correlation. | Pending RAS verify |
| 3 | The non-GRO retained reader could truncate ordinary UDP payloads by inheriting coalescing-sized storage. | Fixed | L1 requires full accepted UDP-datagram storage, truncation detection and a native boundary-size preservation row. | Pending RAS verify |
| 4 | Capability did not cover every usable family of a dual-stack socket. | Fixed | L1 and follow-ups record the admitted socket family domain, require all usable families, and distinguish single-family endpoints from dual-family failure. | Pending RAS verify |
| 5 | Correlation risked importing packet info beyond the ECN contract. | Fixed | L1 limits correlated metadata and marked-send OOB to ECN; packet info remains cleared/out of scope. | Pending RAS verify |
| 6 | Managed capability projection was incomplete and could accidentally inherit native GSO or unrelated facts. | Fixed | L1 explicitly gates ECN only, keeps GSO false, preserves endpoint GRO and retains current DF/other managed behavior. | Pending RAS verify |
| 7 | Non-GRO read-ahead, release behavior and the ECN opt-out storage path were underspecified. | Fixed | L1 requires one kernel datagram per non-GRO ECN read, no stranded prefetch, and no ECN-only decoder when both ECN and GRO are disabled. | Pending RAS verify |
| 8 | IPv6 skips and IPv4-mapped dual-stack traffic could leave Linux qualification incomplete. | Fixed | L1 requires exact-head native `udp4`, `udp6` and admitted `udp`/mapped evidence; a local skip is an evidence gap, not completion. | Pending RAS verify |
| 9 | The public `ConfigureManagedPacketIOV1` documentation change was absent from the implementation blast radius. | Fixed | L1 blast radius and acceptance now require the synchronous exact-forwarding contract in the public comment without changing its signature. | Pending RAS verify |
| 10 | Darwin/FreeBSD support could conflict with the shared unsupported stub's build tags. | Fixed | D1/F1 require platform-specific narrowing and Darwin/FreeBSD/OpenBSD cross-build proof that exactly one receive setup definition is selected. | Pending RAS verify |

The synthesis also rejected expansion into arbitrary-wrapper honesty proofs, concurrent ordinary lease holders, new public/raw metadata authority, an L1 receive/send split, D1/F1 mutual blockers, broader native matrices, recovery/wiremux ownership, silent oversized-datagram loss or support inference from compilation/options/another platform. Those are retained as contract ceilings rather than new work.

The first source-aware verification attempt correctly refused because changing context-file fingerprints requires a new consideration baseline. Post-fix consideration run `20260922T050640-89f2c2e31f7451f01edd3338` then re-reviewed the revised package. It confirmed the graph and ceilings, found five remaining contract gaps, and reported one false traceability gap caused by omission of `CONTEXT.md` from that review bundle.

| # | Re-review finding | Disposition | Plan change / evidence | Verification at that round |
| ---: | --- | --- | --- | --- |
| 1 | The wrapper could normalize a checked singleton's zero-progress native error before the adapter saw it. | Fixed | L1, the program, ADR 0007 and follow-up representation contracts now require the callback to return the singleton lease result unchanged. L1 requires injected `EPERM`, `EMSGSIZE`, first-send retry and invalid normalized `(0,nil)` evidence. | Pending clean re-review |
| 2 | Retained-reader presence was still conflated with GRO/coalescing activation and diagnostics. | Fixed | L1 assigns a separate endpoint-owned activation fact, adds `managed_packet_buffers.go` and the diagnostics contract to blast radius, defines `receive_enabled`, and adds the ECN-on/GRO-off case. | Pending clean re-review |
| 3 | The required closure rows named more mutations than the four-mutation budget. | Fixed | The matrix now enumerates exactly M1 correlation, M2 full-size/single-read, M3 callback route/result and M4 central capability gate; projection uses an ordinary negative assertion. | Pending clean re-review |
| 4 | The source of per-family setup outcomes was not specified. | Fixed | L1 makes endpoint-managed setup retain detailed private outcomes from a refactored OOB constructor while preserving the existing `newConn` signature/behavior for unregistered connections. | Pending clean re-review |
| 5 | The audit claimed glossary retention although the review bundle did not contain `CONTEXT.md`. | Rejected as false | The worktree contains all three retained terms at `CONTEXT.md:51-55`; `git show 928648a9... -- CONTEXT.md` matches their provenance, and the qualification term is updated for admitted-family semantics. `CONTEXT.md` is included in the next review bundle. | Local source comparison passed |
| 6 | D1 cited the transport `sendmsg_x` entrypoint rather than the actual managed callback/endpoint path. | Fixed | D1 now traces `send_conn` → registered callback → lease `WriteBatchV1` → endpoint-owned `newUDPBatchWriter`, separating eligible multi-buffer `sendmsg_x` from singleton fallback. | Pending clean re-review |

Clean consideration run `20260922T052825-8170d8da4364e7ff0b3cf9ce` confirmed the prior dispositions and retained the graph, then identified eight narrower contract hardenings. All are fixed without changing slice count, dependencies, mutation budget or product scope.

| # | Clean-review finding | Disposition | Plan change / evidence | Verification at that round |
| ---: | --- | --- | --- | --- |
| 1 | Direct registrations with a live batch callback lacked an explicit ECN qualification rule. | Fixed | L1, the program and ADR 0007 require nil or synchronous exact payload/OOB/destination forwarding through the exact lease under existing general-batch semantics; the existing batch table is extended, not split. | Pending focused re-review |
| 2 | Concurrent checked singletons could be read as one overwriteable operation slot. | Fixed | L1 requires distinct operation identity and payload/OOB/generation/result association across overlapping calls, permits serialization and folds a two-connection case into the race budget. | Pending focused re-review |
| 3 | ECN-only retained-reader diagnostics did not define `receive_mode`. | Fixed | L1 requires `receive_mode=ordinary`, `coalescing=false` and `receive_enabled=false` without normalization activation and adds the maintained Linux fallback contract to blast radius. | Pending focused re-review |
| 4 | Partial-family failure could retain an ECN-only reader and publish metadata despite capability false. | Fixed | L1 makes family-complete receive qualification a retention/publication gate; only independently active coalescing normalization may retain the decoder on partial-family failure. | Pending focused re-review |
| 5 | Managed malformed ancillary continuation was still implicitly keyed to published GRO. | Fixed | L1 assigns managed drop-and-continue to endpoint-retained managed-reader mode/private state and explicitly preserves fatal unregistered non-GRO behavior. | Pending focused re-review |
| 6 | D1/F1 inherited Linux's first-permission retry although non-Linux classification treats EPERM as terminal. | Fixed | D1/F1 require native terminal no-retry EPERM and preserve message-size/other terminal results without importing the Linux expectation. | Pending focused re-review |
| 7 | D1/F1 named a nonexistent receive setup symbol. | Fixed | Both plans now name `(*managedPacketEndpoint).configureReceive` and retain the three-target exactly-one-definition cross-build gate. | Local symbol search passed; focused re-review pending |
| 8 | L1/D1/F1 dispatch inputs were not bounded to exact test and adapter ranges. | Fixed | Plans name exact current ranges/functions and post-L1 declarations, ban whole diffs/files, and use documented `max(bytes/4, words*2)` measurements: L1 29,826, D1 proxy 16,726, F1 proxy 14,802 tokens with mandatory dispatch-time remeasurement. | Publication-revision manifests measured; focused re-review pending |

Focused verification run `20260922T060749-046cc0bb4b453d96be7c3080` used Codex Astra and Codex Sol against the complete source-aware package and returned no actionable findings or source-backed contradiction. It confirmed all recorded dispositions closed, exactly four L1 mutations, singular representation/enforcement ownership, finite evidence/context ceilings, the five-slice graph and the L1/O1/W1 frontier. One non-semantic infrastructure warning reported linked-worktree config discovery timeout and fallback to the global configuration; both requested reviewers completed and the synthesis was clean.

## Audit result

The author audit and focused independent verification accept the five-track, five-slice graph, finite evidence budgets, single-owner designs, authority-complete in-memory boundaries, zero runtime transitional seams and current frontier. Every accepted finding is fixed or source-rejected with evidence, and the publication gate passed. Any later representation change, graph change or concrete counterexample returns to this audit before dispatch.
