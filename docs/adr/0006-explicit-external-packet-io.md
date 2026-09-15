---
status: accepted
---

# Explicit external packet I/O and managed reuse

The fork will support optimized I/O on externally created sockets through explicit registration on the exact transport before initialization. Permission to change receive format is separate from socket-close ownership and from native capability. Existing unregistered callers retain their contracts under [ADR 0001](0001-upstream-compatibility.md); registration does not make the fork the socket's close owner. A socket method inherited through embedding or access to its descriptor is not sufficient authority.

Use a small versioned structural extension with standard-library types so wiremux can discover it without requiring fork-only names in upstream builds. A fork-owned native batch-writer factory supplies reusable platform submission to a deliberately participating wrapper; that wrapper checks its policy and explicitly registers the checked callback. Unknown wrappers are never unwrapped. A new independent helper module and automatic permission-marker discovery were rejected for the initial contract because they respectively add release coupling and risk inherited authority. Exact names and signatures are frozen by the bounded compile/contract stage before consumer integration.

Initial receive coalescing requires an exact-resource grant that guarantees terminal disposal and exclusive packet I/O, or later a managed endpoint that retains receive-format responsibility. Close ownership remains unchanged. The existing [datapath plan](2026-09-11-datapath-offload-plan.md) admitted no new interfaces and deferred caller opt-in; this is a subsequent explicit opt-in design, not a retroactive relaxation of unregistered caller behavior. Keep platform qualification, disabled/unavailable fallbacks, and packet/storage ownership from ADRs [0004](0004-packet-emission-ownership.md) and [0005](0005-incoming-packet-lifetime.md).

The complete program includes a lifetime-stable managed packet endpoint with exclusive per-attempt leases and normalized ordinary reads after release. Disabling coalescing alone is not a raw-socket restoration guarantee. Legacy raw reusable sockets keep ordinary receive behavior; the dedicated raw-handback stage must deliver a supported bounded procedure or explicitly reject it while retaining the managed alternative. Managed reuse itself remains required work.

An optional fixed-peer mode will be immutable and transport-wide before Dial/Listen, covering packet admission, stateless output, connection output and path changes. It preserves ordinary transport defaults and is not peer identity or authenticated migration. wiremux keeps its filtering wrapper for the first integration; later wrapper removal requires complete equivalent enforcement and its own adoption evidence. Upstream wiremux builds retain their wrapper.

The [fork execution companion](2026-09-14-wiremux-packet-io-integration.md) maps these decisions to the joint program. The initial milestone cannot close that program: reusable endpoints, native filtering, qualification, consumer releases and closeout all remain tracked stages. This ADR records the selected design, not completed runtime behavior.
