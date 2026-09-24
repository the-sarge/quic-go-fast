---
status: accepted
---

# Opt-in BBRv3 with explicit QUIC integration

Use pinned BBR draft-06 as the behavior authority for an optional built-in sender, with the deviations and complete contracts in the [BBRv3 design](../designs/bbrv3.md). The owner accepted the authority and ProbeRTT choices and then delegated the remaining technical choices with advance acceptance of the agent's recommendations. This is design acceptance, not implementation or shipping approval.

A wholesale TCP or QUICHE port would import incompatible recovery, ECN and timing assumptions. Preserve connection-owned packet registration and model state, add a private BBR event adapter and bounded metadata only for opted-in connections, and keep Reno as the unchanged default. Expose a small structural per-Config selection API so consumers can remain buildable with upstream; avoid a public controller interface that freezes internal packet/lifecycle contracts.

The design adds a saved ProbeRTT cap, an explicit conservative classic-ECN response and byte-based pending-work limits. These choices trade some utilization for controlled probing, durable congestion response and bounded local queues; their native cost and coexistence behavior remain qualification requirements. Same-connection migration revalidates ECN only after old marked traffic is completely accounted for, otherwise continuing with loss-based BBR. This preserves trustworthy feedback at the cost of ECN availability after some migrations. GridCast's fresh-connection migration validates anew.

The accepted quic-go-fast evidence campaign owns qualification; consumer changes and production rollout are outside this planning effort. Existing API/wire contracts, module replacement, packet-emission ownership and managed ECN qualification remain binding. Later evidence or upstream changes require an explicit design revision rather than silently changing the selected profile.

## Narrow compatibility exception

Adding private per-Config selection changes the struct field count and makes external positional Config literals invalid. This is a narrow exception to an absolute reading of ADR 0001's source-compatibility promise: existing keyed literals, fields, methods and runtime contracts remain stable, while positional literals require conversion to keyed form. The agent selects this under the owner's delegated design authority and requires disclosure in eventual release notes. A process-global side table or transport-only selection was rejected because it complicates lifetime/copy semantics or loses the per-connection configuration contract. Upstream-compatible consumers use keyed configuration plus structural capability detection.
