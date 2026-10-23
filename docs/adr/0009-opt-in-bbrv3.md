---
status: accepted
---

# Opt-in BBRv3 with explicit QUIC integration

Use pinned BBR draft-06 as the behavior authority for an optional built-in sender, with the deviations and complete contracts in the [BBRv3 design](../designs/bbrv3.md). The owner accepted the authority and ProbeRTT choices and then delegated the remaining technical choices with advance acceptance of the agent's recommendations. This is design acceptance, not implementation or shipping approval.

A wholesale TCP or QUICHE port would import incompatible recovery, ECN and timing assumptions. Preserve connection-owned packet registration and model state, add a private BBR event adapter and bounded metadata only for opted-in connections, and keep Reno as the default. [ADR 0011](0011-recovery-registration-prefix.md) authorizes a narrow correction of its cross-space recovery ordering while preserving its selection and parameters. Expose a small structural per-Config selection API so consumers can remain buildable with upstream; avoid a public controller interface that freezes internal packet/lifecycle contracts.

[ADR 0012](0012-reno-spurious-loss-undo.md) separately authorizes restoring the latest fully spurious default-Reno cutback with complete surviving evidence. This adds bounded controller-owned undo to the ordering correction; Reno selection and parameters and BBR's independent sidecar remain governed by this decision.

[ADR 0013](0013-adaptive-loss-thresholds.md) separately authorizes adaptive application loss classification for both controllers after surviving ordinary spurious-loss proof. It suppresses application packet-distance loss and permits fresh-send-gated time growth up to 2 RTT, with one effective per-space delay for ordinary and PTO-retired classification. This can delay genuine loss and does not change controller identity or evidence ownership.

The design adds a saved ProbeRTT cap, an explicit conservative classic-ECN response and byte-based pending-work limits. These choices trade some utilization for controlled probing, durable congestion response and bounded local queues; their native cost and coexistence behavior remain qualification requirements. Same-connection migration revalidates ECN only after old marked traffic is completely accounted for, otherwise continuing with loss-based BBR. This preserves trustworthy feedback at the cost of ECN availability after some migrations. GridCast's fresh-connection migration validates anew.

The accepted quic-go-fast evidence campaign owns qualification; consumer changes and production rollout are outside this planning effort. Existing API/wire contracts, module replacement, packet-emission ownership and managed ECN qualification remain binding. Later evidence or upstream changes require an explicit design revision rather than silently changing the selected profile.

## Narrow compatibility exception

Adding private per-Config selection changes the struct field count and makes external positional Config literals invalid. This is a narrow exception to an absolute reading of ADR 0001's source-compatibility promise: existing keyed literals, fields, methods and runtime contracts remain stable, while positional literals require conversion to keyed form. The agent selects this under the owner's delegated design authority and requires disclosure in eventual release notes. A process-global side table or transport-only selection was rejected because it complicates lifetime/copy semantics or loses the per-connection configuration contract. Upstream-compatible consumers use keyed configuration plus structural capability detection.
