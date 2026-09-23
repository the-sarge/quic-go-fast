# Managed endpoint DF and path-MTU discovery

This is the approved implementation contract for [#353](https://github.com/the-sarge/quic-go-fast/issues/353), executed as one product PR followed by the required journal PR. The endpoint owns a reversible, lease-scoped DF transaction. Endpoint-lifetime DF would change ordinary datagram behavior after QUIC releases the socket.

## Acceptance contract

At `bindQUIC`, after registration validation and with no active I/O, inspect the socket's address families, snapshot the applicable option values, and enable DF. Publish capability only after setup succeeds for every admitted family.

Run DF setup after existing fallible receive setup so a later receive-setup failure cannot strand DF changes. Unsupported options permit ordinary operation with `DF=false` only when the socket is known unchanged or rollback succeeds. Descriptor failures remain errors. Failed or ambiguous rollback terminates the endpoint.

On lease release, revoke and join I/O, restore the exact saved DF options and parent deadlines, then permit reacquisition. Restoration failure closes the endpoint and returns an error consistently to concurrent closers. `Transport.Close` retains its existing ownership semantics; lease release performs restoration.

Update the private registration state and `externalPacketConn.capabilities()` so managed DF does not depend on whether the ECN adapter exists. Capability must refer to the current, successfully configured lease and become false after revocation.

Preserve the existing handshake rule: discovery starts only when DF is available and configuration permits it. Keep wrapper-controlled reads and sends, selected-peer filtering, exact supplied-socket identity, batch progress/error handling, and ordinary external registrations unchanged.

Each native qualification lane must demonstrate option engagement, an attributable MTU-related send rejection using a valid UDP payload, actual managed QUIC discovery, restored option values and ordinary datagrams after release, and a second lease. Oversized UDP payload errors alone do not qualify DF. The finite socket matrix is IPv4-only, IPv6-only, dual-stack to native IPv6, and dual-stack to mapped IPv4 on each proposed platform.

Use an isolated native test path with known MTU; do not alter shared host routes or interfaces. Missing native evidence leaves the affected platform or socket shape explicitly unqualified and disabled. If no useful platform qualifies, return with findings instead of declaring implementation complete.

Exercise the direct 1232-byte starting profile and 1200-byte discovery-disabled TURN profile without changing public DATAGRAM admission policy. These are profile-preservation tests, not a new TURN deployment campaign.

## Representation, ownership and bounds

The supported domain is factory-created UDP endpoints, exact managed leases, and conforming existing policy wrappers. Go's networking types and native socket APIs own the representation. Lifecycle invariants apply throughout that supported domain; native qualification is evidence for the recorded environments, not every OS release or network. Unsupported socket shapes do not gain capability by inference.

The endpoint's DF transaction is the single enforcement owner. Runtime capability is shipped behavior; rollback and terminal cleanup are required safety enforcement. Tests are verification aids. The plan, native matrix and receipts are traceability metadata. No new analyzer, framework, or permanently maintained qualification harness is proposed; native probes are disposable and receipts are frozen evidence. Ordinary focused tests protect the shipped owner without recursive harness completeness obligations.

Blast radius includes per-socket options, registration failure behavior, release ordering and capability reporting. Production changes are bounded to the managed endpoint, private external registration capability propagation and new private native option helpers. No public API, dependency, packet-emission, ECN, coalescing or wiremux implementation changes. The ordinary `setDF` paths and MTU algorithm remain unchanged. Kernel PMTU-cache effects and platform-specific error timing are not a universal restoration guarantee; a material preservation failure stops that platform's enablement.

## Semantic closure matrix

Contract closure is triggered because incorrect capability or restoration can break reusable-socket behavior across several independently reachable states. The transaction owns native mutation and restoration; endpoint admission and release own synchronization; external registration reports that owner's current generation.

| Semantic class | Required result | Finite evidence |
| --- | --- | --- |
| Invalid registration or active establishment I/O | No DF mutation | Admission regression and existing ordinary-I/O join test |
| Successful setup | Truthful DF capability, including with ECN disabled | Native option tests and real QUIC integration |
| Unsupported setup or partial family failure | Unchanged/restored socket; no DF claim | Native-boundary denial fixture and capability check |
| Failed rollback or release restoration | Terminal endpoint; visible error | Failed-restoration table, including concurrent closers |
| Release, initialization failure, concurrent close | Joined I/O and one consistent restoration outcome | Failed-init test, blocked-I/O restoration test and existing lifecycle tests |
| Stale lease and later acquisition | Revoked authority; fresh setup for the new lease | Native two-generation option tests, blocked-close capability check |
| Discovery disabled or DF unavailable | No discovery | Handshake matrix and TURN-profile integration |
| Policy wrapper and batch fallback | Preserved routing, filtering and error attribution | Existing wrapper/ECN routes and native zero-progress batch fallback |
| Native IPv4, IPv6 and both dual-stack paths | Native behavior supports the capability | Four-lane qualification matrix per enabled platform |

## Terminating evidence and delivery

Evidence ends after the matrix passes, focused tests and one focused race run pass, affected-package tests/vet, formatting and module checks pass, and existing hosted checks succeed. No fuzz campaign, arbitrary repetitions, performance campaign, or mutation suite. A failed check or changed candidate permits the directly relevant rerun. Missing native qualification is an explicit non-goal for platform enablement, not a successful test result.

Keep the implementation context to this contract, relevant ownership docs, changed seams and unresolved findings. Review budget: one fully briefed RAS review, verification of accepted fixes, and at most one replacement review. Apply the shared independent-disposition and precise-root stop policies. Certify the exact final head under [the repository overlay](../REVIEW-LOOP.md), require existing hosted checks, and squash-merge that head. Append the journal only after product merge, then reconcile #353, the [native qualification receipt](../audits/2026-09-23-managed-df/qualification.md) and OmniFocus task `gwn6NoUxk-e`. Any changed approach or expanded boundary returns for approval.
