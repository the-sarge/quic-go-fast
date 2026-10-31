# Multipath #646: protocol and interoperability research

Research date: 2026-10-24, America/New_York. Local baseline: `f35df9d7b016a06174288901f3337ccc2fd7347c`. This is evidence for [#646](https://github.com/the-sarge/quic-go-fast/issues/646)'s sizing/design exploration. It records no implementation decision, API acceptance, upstream-policy change, or authorization to implement. External source and issue reads were performed; no implementation was copied, built, or run, and no interoperability result was produced here.

## Findings that change the starting assumptions

The IETF datatracker still identifies draft-21, published March 17, 2026, as the latest revision; its October 19 status is RFC Editor queue, blocked on final approval, with intended status Proposed Standard. Treat it as the pinned design authority and recheck the eventual published text before implementation. The tracker does not substantiate a guaranteed publication date, so #646 and wiremux #1738's “frozen”/“imminent” language should not be treated as a scheduling guarantee. [IETF status](https://datatracker.ietf.org/doc/draft-ietf-quic-multipath/)

“No Go implementation exists” in #646 is obsolete. `qoke/mp-quic-go` at `084c13ea3834fbe58cedb63db9208951d714f839` explicitly targets draft-21 and contains wire codecs, path state, packet protection changes, tests and a picoquic harness. This is a useful reference and a possible source of selectively adapted work; its production-readiness and test-result claims are not an independent audit. [Pinned release commit](https://github.com/qoke/mp-quic-go/commit/084c13ea3834fbe58cedb63db9208951d714f839), [source tree](https://github.com/qoke/mp-quic-go/tree/084c13ea3834fbe58cedb63db9208951d714f839)

Upstream [quic-go #3343](https://github.com/quic-go/quic-go/issues/3343) remains open. Its maintainer's March 2022 response calls the work large and asks for an application and sophisticated benchmarks; the December 2025 comment is from a user. This is evidence of no commitment in that issue, not evidence that upstream has formally rejected all future work.

## Compact protocol checklist

The following is a reading checklist for the exact [draft-21 text](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21), rather than a replacement specification:

| Area | Requirement or distinction | Sections |
| --- | --- | --- |
| Negotiation | Both advertise `initial_max_path_id`; zero enables path-zero-only operation. Nonzero CIDs required. Parameter not remembered for resumption. | 2–2.2 |
| Packet numbers | Initial/Handshake unchanged; 0-RTT belongs to path 0. New paths have independent application spaces. | 1, 2.3 |
| ACKs | New frames: 1-RTT only. PATH_ACK recommended; ordinary ACK must remain accepted for path 0. | 2.3, 4 |
| Protection | Nonce: IV XOR padded `(32-bit path ID || 64-bit packet number)`. Never reuse path IDs. AEAD limits aggregate across paths. | 2.4, 7.3 |
| Keys | Shared key phases; recommended update spacing: three largest-path PTOs after prior-update ACK. | 2.5 |
| Creation | Client initiates; validate unless tuple previously validated. PATH_RESPONSE uses challenge's path/ID. Validate 1200-byte support. | 3.1 |
| Identity | Same-tuple paths allowed; NAT rebinding/migration can retain path ID. | 5.1–5.2 |
| Recovery | PATH_ACK can return anywhere. Retransmission scheduling unspecified. Per-path congestion state required; coupling not mandated. | 3.1, 5.3–5.7 |
| Backup | Peer preference, not a delivery/failover guarantee. | 3.3 |
| Bounds | Monotonic maximum ID ≤2³²−1; CID limit per path. Prefer deferred allocation, few paths, bounded validation. | 2.1–2.2, 7.1–7.2 |

The existing anti-amplification rule limits responses to an unvalidated address to three times received bytes; QUIC also distinguishes reachability validation from minimum-MTU validation. These are existing protections to preserve when partitioning state. [RFC 9000 §8](https://www.rfc-editor.org/rfc/rfc9000.html#section-8), [§8.2.3](https://www.rfc-editor.org/rfc/rfc9000.html#section-8.2.3)

IANA lists the following permanent registrations, dated March 30, 2026. There are ten frame type values across nine named frame forms, counting PATH_ACK's ECN variant separately. This is the concrete correction to #646's “seven-plus” placeholder. [IANA QUIC registries](https://www.iana.org/assignments/quic)

| Registry | Name | Value |
| --- | --- | --- |
| Transport parameter | `initial_max_path_id` | `0x3e` |
| Frame | `PATH_ACK` / ECN variant | `0x3e` / `0x3f` |
| Frame | `PATH_ABANDON` | `0x3e75` |
| Frame | `PATH_STATUS_BACKUP` | `0x3e76` |
| Frame | `PATH_STATUS_AVAILABLE` | `0x3e77` |
| Frame | `PATH_NEW_CONNECTION_ID` | `0x3e78` |
| Frame | `PATH_RETIRE_CONNECTION_ID` | `0x3e79` |
| Frame | `MAX_PATH_ID` | `0x3e7a` |
| Frame | `PATHS_BLOCKED` | `0x3e7b` |
| Frame | `PATH_CIDS_BLOCKED` | `0x3e7c` |
| Error | `APPLICATION_ABANDON_PATH` | `0x3e` |
| Error | `PATH_RESOURCE_LIMIT_REACHED` | `0x3e75` |
| Error | `PATH_UNSTABLE_OR_POOR` | `0x3e76` |
| Error | `NO_CID_AVAILABLE_FOR_PATH` | `0x3e77` |

## Independent implementation evidence and its limits

Picoquic's current inspected revision is `c2daecaeee77fd910e3dfdbccef6c9c8d250a257`, committed October 24. Its frame constants match the current registered values; their comments still say draft-20. Matching codepoints supports a compatible wire dialect, but does not establish complete draft-21 conformance. [Pinned constants](https://github.com/private-octopus/picoquic/blob/c2daecaeee77fd910e3dfdbccef6c9c8d250a257/picoquic/picoquic_internal.h#L155)

An observed incompatibility matters directly to #646's proposed negotiated-single-path first stage: picoquic only emits `initial_max_path_id` when positive and enables multipath only when both values are positive. A zero value is therefore not usable as the sole interoperability acceptance case with this peer. This remains present in current source, not merely an old report. [Negotiation and encoding](https://github.com/private-octopus/picoquic/blob/c2daecaeee77fd910e3dfdbccef6c9c8d250a257/picoquic/transport.c#L283)

Picoquic's own multipath guide also records this deviation, warns that its default-multipath-option API is currently incorrect, and describes independently paced paths, shortest-latency ACK routing, stream affinity, and backup failover after detected trouble. It explicitly describes idle middlebox expiry and a scheduler that remains under development. These observations weaken wiremux #1738's blanket “sub-RTT failover” and permanently ready backup assumptions. A validated backup is useful, but an application SLO requires measurements of failure detection, retained reachability, congestion state, and useful delivery. [Pinned multipath guide](https://github.com/private-octopus/picoquic/blob/c2daecaeee77fd910e3dfdbccef6c9c8d250a257/doc/multipath.md)

For additional implementation detail, qoke's packet protection code keeps per-path packet-number/key bookkeeping while sharing key updates and counting integrity/confidentiality use across paths. Its received-PATH_ACK handling distinguishes the sending path from the ACK return path. These are concrete locations for a future comparison against this fork's BBR delivery sampling and key-update logic, not a claim that those algorithms have been audited. [AEAD implementation](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/internal/handshake/updatable_aead.go), [per-path recovery implementation](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/internal/ackhandler/sent_packet_handler.go)

qoke's documented choices include uncoupled Reno by default, optional OLIA, retransmitted frames eligible on another path, PTO probes on their source path, minimum-across-paths DATAGRAM sizing, and allocation of packet-number/controller/key state before validation while delaying other state. It does not reset minimum RTT when ACK routing changes. These choices expose policy and resource-budget questions for our design; they are not decisions to inherit. In particular, adding one BBR instance per path would leave an ACK-route-sensitive minimum-RTT question unanswered. [Pinned multipath decisions](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/docs/MP_QUIC_README.md#decisions-for-should-and-may-statements)

## Concrete interop candidate

Use picoquic as the proposed non-Go partner, with two explicitly different pins: `01e124ebdf9fc50df307e01e71f2744471ddc470` is the October 7 peer used by the existing qoke harness; `c2daecaeee77fd910e3dfdbccef6c9c8d250a257` is current inspected source. Reproduce the former before considering the latter. A change of peer version requires a new result record.

qoke provides a reusable harness design: Linux, Docker host networking, real UDP over `127.0.0.1` and `127.0.0.2`, `picoquicdemo`, and `hq-interop`. The source starts both client/server roles and retains endpoint qlogs plus process output. It checks actual traffic on both paths, key updates, abandoning/reopening paths, rebinding and status changes. Read-only inspection found no `.github/workflows/` invocation with either `-tags picoquic` or `interop/multipath`; the tagged suite is a runnable tool, not evidence of an automatic required CI gate. [Harness source](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/interop/multipath/picoquic_test.go), [migration/status cases](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/interop/multipath/migration_test.go)

The documented command, from a pinned qoke checkout on Linux, is:

```sh
go test -tags picoquic -count=1 -v ./interop/multipath/... -args -logdir=/absolute/artifact/directory
```

This command tests qoke, not quic-go-fast. Our future implementation would need a deliberately adapted harness endpoint; no such adapter is part of this research. The harness skips on non-Linux systems or when Docker is absent, so a future required job must fail preflight on those conditions and verify that expected test cases actually executed. [Build/run documentation](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/interop/multipath/README.md)

The Dockerfile pins picoquic, then runs:

```sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DPICOQUIC_FETCH_PTLS=ON -DBUILD_TESTING=OFF
cmake --build build --target picoquicdemo -j 4
```

It uses Debian Bookworm, a C/C++ toolchain, CMake, OpenSSL development libraries, and Git; PicoTLS is fetched at the peer's configured revision. For reproducibility, a future job should also record the base-image digest and resolved dependency revisions, rather than treating the pinned picoquic commit as the entire build identity. [Pinned Dockerfile](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/interop/multipath/Dockerfile), [peer CMake dependency pins](https://github.com/private-octopus/picoquic/blob/01e124ebdf9fc50df307e01e71f2744471ddc470/CMakeLists.txt)

The qoke README reports 23 passing interop tests against the October 7 peer, but that total also includes single-path, version, address-discovery and other extension cases. It is not 23 independent multipath proofs. The report describes workaround-sensitive handshake CID behavior, a server crash when a path challenge arrives before the server has a client CID, a demo-server address-observation callback problem, and the zero-limit incompatibility. Its second path is opened after a successful request. Those exclusions belong in our negative-test and early-path-opening matrix. Nothing here verifies whether every historical defect remains in current picoquic. [Reported results and observations](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/interop/multipath/README.md#results)

Proposed evidence gaps for our own acceptance plan are simultaneous 0-RTT/handshake transitions, nonce vectors and aggregate AEAD limits, reordered key phases across unequal delays, saturated path-ID/CID limits, partially validated path storms, late ACKs after abandonment, independent path MTUs, same-tuple paths, a failed versus idle backup, ACK-route changes under BBR, useful DATAGRAM delivery, and shared-bottleneck fairness. Loopback protocol tests and a second implementation can exercise framing/state transitions; netem or two-host experiments must establish delay/loss asymmetry, path failure behavior and application benefit. These are research recommendations, not a claim that qoke lacks every corresponding unit test.

## Reuse as a third sizing option

qoke is a GitHub fork of `AeonDave/mp-quic-go`, with `quic-go/quic-go` as root source. The draft-21 commit is a merge whose parents are older mp-quic-go `4dfa088333223f33487db14afba95f69d16a46d0` and upstream `9d085cc690f7c96451e8ae5659eb0e64671da47a`; the latter is the dereferenced v0.63.0 tag. Its module uses Go 1.26 and a different module/import path. The code carries MIT licensing; the README separately excludes logo/brand assets. This supports considering adaptation of code and tests with attribution, subject to a proper file-level provenance review when selecting material. It does not prove that the implementation is correct or suitable wholesale. [Merge parents](https://github.com/qoke/mp-quic-go/commit/084c13ea3834fbe58cedb63db9208951d714f839), [upstream tag](https://github.com/quic-go/quic-go/tree/v0.63.0), [license](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/LICENSE), [module](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/go.mod)

Measured text deltas compare the two immutable GitHub source archives, decoding text and applying Python `difflib.unified_diff` to physical lines. The normalization changes only `github.com/qoke/mp-quic-go` to `github.com/quic-go/quic-go` before comparison. These are source-diff counts, not effort estimates or a semantic correctness analysis. Binary files are excluded. [Pinned reference tree](https://github.com/qoke/mp-quic-go/tree/084c13ea3834fbe58cedb63db9208951d714f839), [pinned upstream tree](https://github.com/quic-go/quic-go/tree/9d085cc690f7c96451e8ae5659eb0e64671da47a)

| Category | Raw changed files | Raw added / deleted lines | Module-normalized changed files | Normalized added / deleted lines |
| --- | ---: | ---: | ---: | ---: |
| Production Go | 181 | 18,864 / 1,252 | 114 | 18,531 / 919 |
| Go tests (`*_test.go`) | 250 | 39,760 / 1,216 | 176 | 39,305 / 761 |
| Go support/examples/mocks | 23 | 1,536 / 128 | 16 | 1,471 / 63 |
| Other text | 39 | 4,109 / 155 | 34 | 4,093 / 139 |

Classification precedence: `_test.go` first; then Go under `integrationtests/`, `interop/`, `testutils/`, `example/`, `fuzzing/`, `internal/mocks/`, or a basename starting `mock_`; then remaining Go as production; then other text. Thus “production” is a reproducible filename category, not an assertion that every included file ships. This whole-fork comparison includes address discovery, compatible version negotiation, preferred addresses, HTTP fixes, documentation and other work. It is not a multipath-only upper/lower bound on this fork's change.

A narrower name-selected subset includes Go paths containing `multipath`, plus basenames beginning `path_ack`, `path_abandon`, `path_status`, `path_new_connection_id`, `path_retire_connection_id`, `max_path_id`, `paths_blocked` or `path_cids_blocked`. With the same normalization/classification, it contains 24 new production files (+7,440 lines), 43 new test files (+18,329), and two new support/example files (+201), with no deletions. This excludes multipath changes embedded in existing `connection.go`, packet handling, recovery, TLS and transport-parameter files. Conversely, it includes optional OLIA, duplication/reinjection, address advertisement and broad interop tests. It is a traceable comparison sample, not a minimum compliant implementation size.

Selective reuse deserves investigation before committing to a from-scratch estimate. Wire codecs, test cases and the external harness are plausible first candidates. The integration is substantial: qoke places concurrent sending in a connection-owned `multipath_send.go` and exposes controller/scheduler policy, optional duplication and OLIA. Those ownership and API decisions must be reconciled with this fork's [packet-emission ownership](../adr/0004-packet-emission-ownership.md), [upstream compatibility](../adr/0001-upstream-compatibility.md), and [private opt-in BBR integration](../adr/0009-opt-in-bbrv3.md). Importing its merge wholesale would also import unrelated features and choices. [Sending code](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/multipath_send.go), [controller](https://github.com/qoke/mp-quic-go/blob/084c13ea3834fbe58cedb63db9208951d714f839/multipath_controller.go)

## Consumer interpretation

[wiremux #708](https://github.com/GridSwarm/wiremux/issues/708) explicitly assigns GridCast fresh, distinct rateless symbols across paths and ownership of symbol-usefulness scheduling. It says multiple independent Wires can already prototype this; wiremux's question is reusable connectivity, authentication and lifecycle. The requirement is neither mandatory duplication nor retransmission of every lost symbol. #646 should test whether its path-selection seam can preserve this choice without importing product-level symbol scheduling into QUIC.

[wiremux #1738](https://github.com/GridSwarm/wiremux/issues/1738) remains an exploration of alternatives A–E, including retaining independent Wires. Its references to identity-bound authorization, fixed-peer sets and exact socket ownership remain consumer design questions; transport reachability proof alone does not settle those policies. Its “NAT rebinding as path-open” premise should be replaced by the path-ID/tuple distinction in the protocol checklist and picoquic evidence above. The QUIC fork need not decide wiremux's whole layering design to size path mechanics, but should keep authorization and scheduling seams reviewable by that consumer.

The proposed comparison for rateless traffic is one connection with multiple paths versus multiple independent connections at matched offered useful content, path capacities/delays/loss, congestion algorithms and resource limits. Count unique useful symbols at application acceptance, consistent with this repository's [application-goodput vocabulary](../../GLOSSARY.md); do not report packet acknowledgments or duplicated wire bytes as goodput. Also compare ordinary single-path migration for the backup consumer. These measurements remain future work.
