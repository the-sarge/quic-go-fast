# HTTP idle-timeout fixture retry repair

## Accepted outcome

Repair the independently demonstrated retry/channel deadlock in `TestHTTPServerIdleTimeout`. After the fixture's single GET succeeds, its idle-close assertion observes the connection that served the successful attempt. An already-closed first attempt must not satisfy that assertion. Tracked in [repair issue #314](https://github.com/the-sarge/quic-go-fast/issues/314). The approved task is [OmniFocus hdsml2cfy7e](omnifocus:///task/hdsml2cfy7e); the [original diagnosis](https://github.com/the-sarge/quic-go-fast/issues/151#issuecomment-5637200610) supplies causal evidence for this separate fixture defect.

## Acceptance criteria

- The ordinary GET still completes, consumes and closes its response, and observes HTTP idle closure.
- The controlled unusable-first-connection case makes two dial attempts and completes GET.
- The observed connection is the successful second connection, distinct from the closed first connection.
- Transport, server and recorder cleanup finish.
- The regression fails against the original handoff and passes after correction.
- Existing capture failure tests retain attributable observations through cleanup.

## Implementation and boundaries

One repair PR replaces the capacity-one channel with a fixture-local `atomic.Pointer[quic.Conn]`. Each successful dial publishes its connection before returning. This fixture issues one GET, the existing transport retries sequentially, and no later request starts before the observation: the final pointer therefore identifies the successful attempt. An immediate nonnil assertion replaces the obsolete channel-receive wait. There is no promise for shared transports or concurrent requests outside this fixture.

Share the actual fixture callback and idle assertions with `TestHTTPServerIdleTimeoutAfterRetry` through a narrow test helper. A regression-only callback completes the first handshake, explicitly closes that connection and observes closure before returning it, letting the real transport exercise its normal unusable-connection retry. No arbitrary sleep, production hook or channel-capacity increase is admitted. The regression checks the second connection's identity and returns through transport, server and recorder cleanup.

The approved surface is the idle-timeout fixture, its narrow shared helper/regression, directly affected capture assertions and maintained documentation. Preserve server idle-timer behavior, transport retry/cache semantics, response consumption, socket/connection ownership, network deadlines, idle-close and cleanup bounds, paired endpoint recording, retention and cleanup ordering. Update channel-send capture milestones to connection-publication milestones. No production changes, dependencies, global state or CI redesign are authorized. Test-local pointer publication removes the consumer-dependent wait; it does not change transport ownership or retries. Existing capture I/O and scheduling costs remain.

Do not close #151 or its waiting task `lOLFMmA0JFX`, and make no causal claim about #169 or #301. This work needs no natural capture of the original timeout. Existing frozen diagnosis artifacts remain unchanged.

## Representation and artifacts

The domain is concrete Go connection pointers and the existing typed transport lifecycle for the ordinary and controlled retry paths. The fixture owns observation; transport code owns retry sequencing. The guarantee is example-level regression coverage, not arbitrary concurrent-request support. Contract closure is not triggered: focused tests cover the bounded lifecycle cases; there is no recursive harness audit, mutation campaign or semantic cross-product.

Fixtures and regressions are maintained verification aids. Their payoff is preventing deadlocks and false idle-close passes. Maintain them with this fixture and reconsider them if its observation mechanism is removed. Approval makes these regressions acceptance gates for this repair only. The plan, issue, receipts and journal are process metadata. No shipped behavior or required safety enforcement changes.

## Finite evidence and review

Run one bounded red/green regression cycle and retain the original handoff's failing stack/output. Bound the red execution externally so cleanup cannot hang the working session. Final certification runs focused ordinary/retry/capture tests once for QUIC v1 and v2, one focused native race invocation, one full affected-package run, package vet, configured formatting/lint, `go mod tidy -diff`, and diff checks. New focused executions require a concrete failure or accepted correction. Existing hosted checks supply the platform/toolchain matrix; there is no extra repetition or random campaign.

Use one initial RAS review, verification of accepted fixes, and at most one replacement review. Brief reviewers with these acceptance criteria verbatim, this representation domain and artifact classification, the declared ceilings, evidence budgets and unresolved history. Independently disposition findings using the shared review policy. Stop for a decision if production changes, broader concurrency support, a repeated precise-root approach failure or evidence beyond the approved budget becomes necessary.

Follow the [repository execution overlay](../REVIEW-LOOP.md): draft PR, review and exact-head local certification, successful applicable hosted checks on that head, readiness and pinned-head squash merge. After merge reaches remote main, append the journal without RAS, revalidate deferred findings, and complete only this repair issue and OmniFocus task. Audit history and exact-head receipts belong in PR discussion; implementers need this current contract, the referenced diagnosis, the directly affected capture contract and any unresolved findings.
