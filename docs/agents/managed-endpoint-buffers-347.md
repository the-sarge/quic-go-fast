# Managed endpoint buffers and diagnostics (#347)

## Accepted outcome and boundary

The [implementation brief](https://github.com/the-sarge/quic-go-fast/issues/347#issuecomment-5687311249) and user-approved one-PR plan govern this change. Configure both private UDP buffer directions before publishing an endpoint using existing platform targets; retain immutable setup evidence across leases; keep setup failures nonfatal; report accurate managed provenance, queue limits, receive mode and registered callback availability. Preserve public signatures, ordinary datagrams, selected-peer policy, exact registration binding, generation revocation, deadline restoration, active-I/O joining and close ownership.

Implementation is bounded to endpoint construction/state, managed registration metadata, transport wrapping/diagnostics, existing warning handling and focused tests/documentation. Constructor-time setup raises configured queue capacity even for endpoints that never reach QUIC. Evidence adds no lifecycle owner or new synchronization. Unknown wrappers and ordinary external registrations retain existing behavior. No raw-socket interfaces, new dependencies, tuning options, global registries, background retries, wiremux implementation, packet-profile changes, benchmark campaign or CI redesign. DF/PMTU (#353), ECN (#354), segmented sends and coalesced receives remain separate.

## Representation, artifacts and terminating evidence

The supported domain is exact Go factory values and validated registration identities. The endpoint owns setup evidence; existing registration owns binding; existing platform helpers own sizing and inspection. Identity and one-time-setup invariants apply throughout this supported domain. Native observations are example-level evidence, not universal OS-capacity or performance guarantees. Unknown inspection results never establish configured capacity. Existing sizing helpers own warning policy; errors from the additional diagnostic inspection remain visible in debug/tracer output without consuming the warning budget. Queue sizes are operating-system reported limits, not physical memory or throughput claims.

Runtime setup and diagnostics are shipped behavior. Existing binding/lifecycle guards are required safety enforcement. Tests are verification aids, and this contract and review/certification records are process metadata. No new maintained analyzer or verification framework is an approved deliverable. Expanded contract closure is not triggered: immutable evidence does not move packet authority, and focused tests cover the new paths. A required ownership/lifecycle redesign stops for a decision.

| Behavior | Finite evidence |
| --- | --- |
| Independent setup and reuse | `TestManagedEndpointBufferSetupOnce` (isolated process): both setters succeed, receive fails, send fails, both fail; publication ordering; usable socket; two lease/transport intervals; retained errors and honest unknown capacity where inspection is unavailable |
| Native queues and ordinary I/O | `TestManagedEndpointNativeBuffers`: inspect the private native socket and compare diagnostics to its actual limits; capped hosts remain asserted, not skipped; actual UDP datagram and zero offload flags |
| Provenance and callback availability | `TestManagedEndpointDiagnosticProvenance`: direct parent/lease, direct registration and explicitly registered wrappers, callback absent/present, unknown and ordinarily externally registered wrappers |
| Warning policy and durable diagnostics | `TestManagedEndpointBufferWarnings`: isolated processes for enabled, disabled, consumed budget, and closed-receive/failed-send cases; both failures in one warning; managed debug and tracer status retained |
| Inspection failure | `TestManagedEndpointInspectionFailure` (isolated process): real usable UDP socket with denied inspection; unknown status and error in both directions; `TestManagedEndpointDiagnosticInspectionDoesNotWarn` retains diagnostic errors while preserving the warning budget for a later sizing failure |
| Wrapper packet authority | `TestManagedEndpointPreservesPeerPolicy`: ordinary and registered batch writes reject a foreign peer; non-QUIC reads remain filtered |
| Existing lifecycle and interfaces | Existing `TestManagedEndpoint*`, `TestManagedPacketIO*`, `TestExternalPacketIO*` and structural registration/factory tests; no public signature changes |

Local evidence consists of the focused managed tests with race detection, affected root-package tests, `go vet .`, `go mod tidy -diff`, lint and formatting/diff checks. Native macOS evidence runs locally; existing hosted unit jobs supply Linux/macOS/Windows evidence. Missing native evidence must be reported explicitly; cross-compilation is supplementary. No new mutation campaign, fuzzing, timing-boundary certification or repeated stress counts are required. Existing hosted checks remain required by the repository overlay.

## Review and delivery

Use one fully briefed initial RAS review, manual finding disposition and fixes, scoped verification and at most one replacement review. Quote the brief's acceptance criteria verbatim in the review prompt. A stronger contract, boundary expansion, or two distinct counterexamples at the same precise invariant and enforcement seam triggers the shared stop-for-decision policy. Tests and metadata cannot recursively strengthen the shipped contract.

The bounded implementation context is this contract, the linked brief, ADR 0006, the named source owners and focused tests, plus relevant unresolved findings. Keep review chronology and candidate receipts outside this normative contract. Certify the clean exact pushed head against the current base, require applicable existing hosted PR checks, mark ready and squash-merge with head matching. If main advances, update and repeat required review/certification/CI. After the product merge, append the dev journal without RAS, revalidate deferred findings against merged code, and complete OmniFocus task `b4aknw-D1KJ`.
