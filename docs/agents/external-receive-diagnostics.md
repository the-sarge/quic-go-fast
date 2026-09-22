# External receive diagnostics

The `external_packet_io` debug event distinguishes receive-format permission, known receive-path eligibility, and actual activation. This is the maintained diagnostic contract for [issue #372](https://github.com/the-sarge/quic-go-fast/issues/372), approved as one maintenance PR. [ADR 0006](../adr/0006-explicit-external-packet-io.md) continues to own registration and socket authority.

## Fields and compatibility

`receive_requested` retains its existing meaning and matches `receive_permitted`: the external registration grants receive-format permission, or registration uses a managed endpoint. `receive_eligible` means the platform and supplied receive path meet the implementation's prerequisites. It does not assert that this kernel or socket can activate coalescing. Eligibility is independent of permission and explicit opt-out. `receive_enabled` means native GRO/URO or persistent managed normalization is active; `connCapabilities.GRO` retains its runtime meaning of enabled native coalescing. A Linux endpoint may retain an ECN-only native reader while `receive_enabled`, `GRO`, `coalescing` and `receive_mode=normalized` all remain false; ECN reader retention is not coalescing activation.

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
| Managed Linux setup retained ordinary reads after dual-family ECN `EPERM`, without required packet-info failure | false | `ancillary_setup_denied` |
| Eligible setup explicitly opted out through `QUIC_GO_DISABLE_GRO` | false | `explicit_opt_out` |
| Otherwise eligible activation remains unavailable | false | `activation_failed_or_unavailable` |

The final reason covers the existing Linux kernel-version gate, socket-control failure, and failed activation. It does not distinguish native lack of support from another activation failure. Unattempted capability is not presented as conclusively unsupported.

The [managed Linux fallback](linux-managed-fallback.md) produces `ancillary_setup_denied` before GRO activation or its environment opt-out is evaluated. Required packet-info failures and other setup errors remain fatal, so they do not produce a successful fallback registration event.

Setup captures its opt-out result when it evaluates the existing environment setting. Event formatting does not reread the environment or probe the socket. Managed setup retains the result under the endpoint's existing lock and copies it into the registration. Once a managed normalizer is active, it stays active across leases even if the environment later disables new activation attempts; reporting follows that existing behavior.

The `managed_packet_io` diagnostic reports `ecn_admitted_ipv4`, `ecn_admitted_ipv6`, `ecn_ipv4_mapped`, `ecn_ipv6_only`, `ecn_disabled`, `ecn_kernel_unsupported` and `ecn_failed_family` from the endpoint-owned setup record. `ecn_failed_family` names only an admitted family whose ancillary setup failed; opt-out and the Linux kernel gate have their own fields. These are setup facts, not a claim that an unknown wrapper is honest. The event's `ecn` capability is true only when every admitted family qualifies and the registration has a policy-safe marked-send route.

## Boundary and finite evidence

The representation domain is the finite setup states produced by the existing platform constructors and external/managed registration paths. Those owners supply the facts, and `wrapExternalPacketIO` owns the diagnostic mapping. The mapping guarantee covers this internal state domain; native tests provide example-level execution evidence, not universal kernel support. There is no external grammar or new parser.

Shipped artifacts are diagnostic state, event formatting, and documentation. Focused tests are verification aids; review and certification receipts are traceability metadata. No new safety enforcement, maintained verification framework, or recursive evidence obligation is introduced. Material-risk contract closure is not triggered: ordinary focused tests cover the diagnostic distinctions while existing permission and lifetime tests protect the unchanged behavior.

Acceptance evidence is `TestExternalPacketIODiagnostics` (permission, platform, opaque wrapper, opt-out and reason precedence), `TestExternalGRONonBatchWrapper` and `TestExternalGROUnavailableWrapper` (Linux wrapper eligibility), `TestExternalGROPermission` and `TestExternalGROWrapperNative` (permission/opt-out enforcement and successful Linux activation), `TestWindowsExternalReceivePermission` and `TestWindowsUROProbeErrorClassification` (Windows activation, wrapper distinctions and bounded failure classification), and `TestManagedPacketIOReceiveDiagnostics` (ordinary outer wrapper, inactive and active setup snapshots, and active normalization retained across leases). Existing managed receive diagnostics and packet/ownership assertions remain intact. Native activation assertions must execute successfully on Linux and Windows; a skip or cross-compilation does not satisfy this gate. Windows activation-dependent tests use the existing `requireUROCapableHost` policy: an unsupported local host may skip those assertions, while hosted CI must activate URO. Opt-out diagnostics remain exercised without that guard.

Run affected-package tests, focused race tests, `go vet .`, `go mod tidy -diff`, formatting and diff checks. Existing hosted workflows supply the repository's platform, integration, lint and cross-compilation checks. No new stress repetitions, performance campaign, or CI redesign is required. Review is bounded to one initial RAS review, verification of accepted fixes, and at most one replacement review, using the [repository execution overlay](../REVIEW-LOOP.md).

The blast radius is private setup-state retention, external/managed diagnostics, focused tests and maintained prose. Preserve receive policy, activation ordering, wrapper policy, packet delivery, buffers, lifetimes, batching and close ownership. Add no public Go API, dependencies, global state or telemetry-driven socket mutation. The decoder-setup fallback from #379 is governed by its [separate contract](linux-managed-fallback.md); this table documents its diagnostic integration without expanding the #372 implementation scope. Stop for a decision if truthful reporting requires changed receive policy, broader ownership changes, missing required native evidence or work beyond this boundary. Implementation context is this contract, the linked issue's acceptance criteria, relevant setup owners/tests and unresolved review findings; historical architecture-program records are not required.

## Approved integration-fixture correction

`TestVersionNegotiationFailure` binds its listener to `127.0.0.1`, matching the proxy's IPv4-only forwarding socket, and asserts `VersionNegotiationError` before its unchanged one-to-two-RTT timing check. This narrow, approved exception changes only the version-negotiation test fixture. It does not change production negotiation, proxy behavior, timeouts or timing tolerances. Evidence is the forced-IPv6 reproduction of the forwarding failure and idle timeout, the IPv4 reproduction returning the expected negotiation error, the complete version-negotiation package with the failed hosted run's shuffle seed, and the existing hosted integration jobs on the final candidate. No broader networking investigation is part of this PR.
