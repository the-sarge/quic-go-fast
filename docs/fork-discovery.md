# Fork discovery

The accepted decisions are recorded in ADRs [0001](adr/0001-upstream-compatibility.md), [0002](adr/0002-adopt-through-module-replacement.md), and [0003](adr/0003-follow-stable-upstream-releases.md). The normative implementation contracts are in the [program index](adr/2026-09-06-fork-program.md). This discovery receipt is historical context, not a dispatch contract.

## Evidence and scope

Source inspection began at upstream `816f839038f72256fa7b7a971f64cc7411f0f426`. The stable implementation baseline is v0.62.0, `793f74d8e03368c5aded128af6f48d21dbb47f73`. The inspected datagram queue, connection, packet packer, send queue, and module files are identical between those commits. No fork benchmark or handshake reproduction was executed during planning.

The primary workload is bulk file transfer using 1071-byte application records. Its application and transport-integration sweep remain externally owned; this repository supplies independently measured native transport patches. Existing external measurements against v0.61.0 cannot establish the benefit of a v0.62.0 fork. Match the upstream revision, compiler, workload and environment when comparing patches.

Receive admission copies data before checking the queue limit; the receive slice removes entries without clearing them; and sending copies caller data before enqueueing. These are candidate costs, not evidence of an unbounded memory leak or a promised throughput gain. The packet packer takes one DATAGRAM frame per packet, but two primary-workload records cannot fit in an ordinary-MTU packet. Multi-message packing, new ownership APIs, jumbo packets, and application-level multipath are deferred.

[Upstream #5557](https://github.com/quic-go/quic-go/pull/5557) proposes a receive ring but its original unbounded-leak rationale is incorrect. The proposed implementation is not an accepted dependency; D2 owns a fresh bounded decision. [Upstream #5815](https://github.com/quic-go/quic-go/issues/5815) reports handshake message-size failures; H1 owns reproduction and recovery. Its older explanation that the MTU finder is absent throughout the handshake is corrected by current source: transport-parameter processing constructs the finder before handshake confirmation starts probing.

The user provided private consumer strategy notes during grilling. They informed the workload and ownership boundaries without becoming public source dependencies. Application transfer state, Symbol scheduling, repair, multi-path policy, storage, and physical-network qualification remain outside these plans.
