# External receive diagnostics

The `external_packet_io` debug event distinguishes receive-format permission, known receive-path eligibility, and actual activation. This is the maintained diagnostic contract for [issue #372](https://github.com/the-sarge/quic-go-fast/issues/372), approved as one maintenance PR. [ADR 0006](../adr/0006-explicit-external-packet-io.md) continues to own registration and socket authority.

## Fields and compatibility

`receive_requested` retains its existing meaning and matches `receive_permitted`: the external registration grants receive-format permission, or registration uses a managed endpoint. `receive_eligible` means the platform and supplied receive path meet the implementation's prerequisites. It does not assert that this kernel or socket can activate coalescing. Eligibility is independent of permission and explicit opt-out. `receive_enabled` means native GRO/URO or persistent managed normalization is active; `connCapabilities.GRO` retains its runtime meaning of enabled native coalescing.

`receive_eligible` replaces `receive_supported`, whose old value merely duplicated activation. Consumers of this debug-event text must use the new field for eligibility, or `receive_enabled` when preserving their old activation check. Event identity, requested/permitted fields, and batch fields are unchanged.

Linux eligibility requires an OOB-capable exact UDP socket or a participating `ReadBatch` wrapper. An OOB wrapper using ordinary `ReadMsgUDP` is ineligible for Linux GRO. Windows eligibility requires the existing OOB-capable receive path, including participating `ReadMsgUDP` wrappers. Other platforms report ineligible. Managed registration uses the endpoint's native normalization path, so an ordinary-datagram outer wrapper does not make that endpoint ineligible.

## Disabled-reason precedence

Use the first matching row. Eligibility remains independently reported.

| Condition | `receive_enabled` | `receive_disabled_reason` |
| --- | --- | --- |
| Native coalescing or managed normalization active | true | `none` |
| No receive-format permission | false | `no_permission` |
| Platform lacks the receive path | false | `unsupported_platform` |
| Supplied receive path is ineligible | false | `ineligible_wrapper` |
| Eligible setup explicitly opted out through `QUIC_GO_DISABLE_GRO` | false | `explicit_opt_out` |
| Otherwise eligible activation remains unavailable | false | `activation_failed_or_unavailable` |

The final reason covers the existing Linux kernel-version gate, socket-control failure, and failed activation. It does not distinguish native lack of support from another activation failure. Unattempted capability is not presented as conclusively unsupported.

Setup captures its opt-out result when it evaluates the existing environment setting. Event formatting does not reread the environment or probe the socket. Managed setup retains the result under the endpoint's existing lock and copies it into the registration. Once a managed normalizer is active, it stays active across leases even if the environment later disables new activation attempts; reporting follows that existing behavior.

## Boundary and finite evidence

The representation domain is the finite setup states produced by the existing platform constructors and external/managed registration paths. Those owners supply the facts, and `wrapExternalPacketIO` owns the diagnostic mapping. The mapping guarantee covers this internal state domain; native tests provide example-level execution evidence, not universal kernel support. There is no external grammar or new parser.

Shipped artifacts are diagnostic state, event formatting, and documentation. Focused tests are verification aids; review and certification receipts are traceability metadata. No new safety enforcement, maintained verification framework, or recursive evidence obligation is introduced. Material-risk contract closure is not triggered: ordinary focused tests cover the diagnostic distinctions while existing permission and lifetime tests protect the unchanged behavior.

Acceptance evidence is `TestExternalPacketIODiagnostics` (permission, platform, opaque wrapper, opt-out and reason precedence), `TestExternalGRONonBatchWrapper` and `TestExternalGROUnavailableWrapper` (Linux wrapper eligibility), `TestExternalGROPermission` and `TestExternalGROWrapperNative` (permission/opt-out enforcement and successful Linux activation), `TestWindowsExternalReceivePermission` and `TestWindowsUROProbeErrorClassification` (Windows activation, wrapper distinctions and bounded failure classification), and `TestManagedPacketIOReceiveDiagnostics` (ordinary outer wrapper, inactive and active setup snapshots, and active normalization retained across leases). Existing managed receive diagnostics and packet/ownership assertions remain intact. Native activation assertions must execute successfully on Linux and Windows; a skip or cross-compilation does not satisfy this gate.

Run affected-package tests, focused race tests, `go vet .`, `go mod tidy -diff`, formatting and diff checks. Existing hosted workflows supply the repository's platform, integration, lint and cross-compilation checks. No new stress repetitions, performance campaign, or CI redesign is required. Review is bounded to one initial RAS review, verification of accepted fixes, and at most one replacement review, using the [repository execution overlay](../REVIEW-LOOP.md).

The blast radius is private setup-state retention, external/managed diagnostics, focused tests and maintained prose. Preserve receive policy, activation ordering, wrapper policy, packet delivery, buffers, lifetimes, batching and close ownership. Add no public Go API, dependencies, global state or telemetry-driven socket mutation. The separate decoder-setup fallback investigation in #379 is out of scope. Stop for a decision if truthful reporting requires changed receive policy, broader ownership changes, missing required native evidence or work beyond this boundary. Implementation context is this contract, the linked issue's acceptance criteria, relevant setup owners/tests and unresolved review findings; historical architecture-program records are not required.
