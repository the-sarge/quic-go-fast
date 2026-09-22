# Linux managed receive fallback

This is the approved standalone contract for [issue #379](https://github.com/the-sarge/quic-go-fast/issues/379), not a QUIC-IO slice or release prerequisite. The [investigation receipt](../audits/2026-09-16-linux-managed-fallback.md) records native evidence. Review chronology and exact-head certification belong in the product PR discussion.

## Outcome and acceptance

Factory-created Linux UDP endpoints may complete managed registration with ordinary reads when both ECN receive-option attempts return `EPERM` and required packet-info setup has not failed. Classification belongs to the existing decoder setup; fallback belongs solely to `managedPacketEndpoint.configureReceive`. No caller outside the Linux managed receive boundary suppresses the classified error.

- Demonstrate dual-family ECN setup denial on a real factory-created socket and ordinary `ReadFrom` / `WriteTo` success under the same restrictions before and after the rejected setup attempt.
- Separately demonstrate an unusable socket and preserve fatal descriptor/setup failures.
- With safe fallback established, registration succeeds, receive mode remains ordinary, coalescing remains disabled, eligibility remains accurate, and the disabled reason is `ancillary_setup_denied`.
- Preserve lease return/reuse, descriptor hiding, wrapper policy, close ownership, lease exclusivity, receive-format ownership, deadlines, enabled-GRO normalization and unavailable-GRO fallback.
- Required packet-info failures, other ECN error classes and native/unregistered decoder failures remain fatal. Packet-info fallback is outside this contract.
- If the usable case or safe fallback is not established, retain runtime behavior and record a bounded negative or inconclusive result. Missing Linux access or unavailable restriction injection is an evidence blocker, not a negative result.

## Boundary and representation

The supported domain is factory-created Linux UDP endpoints and the narrowly classified dual-family ECN `EPERM` case. Go socket operations and structured internal errors own setup representation; Linux owns syscall results. The behavioral rule is a canonical subset of setup failures with example-level native evidence, not a universal claim about restricted environments or socket health.

The approved blast radius is Linux managed receive setup, minimal internal error classification in the shared decoder, focused tests and maintained diagnostic documentation. Shared decoder callers retain their error text and failure behavior. No public API, descriptor exposure, close owner, receive decoder, dependency or CI change is authorized. Required wildcard packet-info failure must prevent fallback even when ECN also fails; simultaneous failure retains the historical ECN-first error text. No performance, fuzz, extra-platform or open-ended restriction campaign is authorized; existing hosted coverage remains applicable.

Runtime fallback is shipped behavior. Narrow error classification and fatal propagation are required safety enforcement. Tests and the restricted subprocess are verification aids, not a general-purpose restriction harness or a new maintained product dependency. This contract, receipt, journal and tracker pointers are process metadata. The test helper supports only native Linux arm64/amd64 subprocesses and can be retired or replaced with the regression when the setup boundary changes; unsupported fixture environments do not establish reachability evidence.

Contract closure is not triggered: ordinary focused tests cover the finite accepted decision states. The representation gate does not require a parser or an exhaustive restricted-environment model. No mutation campaign or recursive harness validation is required.

## One-PR execution and terminating evidence

First reproduce the denied setup, bidirectional I/O and fatal control. Observe the public registration regression fail before changing production. Introduce the narrow classification and managed fallback, then verify success, diagnostics and reuse. Preserve the original public factory and registration seams; a native socket close injected by the fixture separately exercises descriptor failure beyond the public closed-endpoint guard.

| Semantic case | Expected disposition | Finite evidence |
| --- | --- | --- |
| Both ECN setup calls return `EPERM`, required packet info succeeds | Ordinary managed registration | Restricted native subprocess; bidirectional I/O, diagnostics, transport close, parent and subsequent lease reuse |
| ECN setup returns another error | Fatal registration | Restricted native `EINVAL` control |
| Both required packet-info calls fail, alone or alongside ECN denial | Fatal registration | Two restricted native controls on wildcard IPv4 |
| Native socket unusable | Fatal registration and I/O | Closed-endpoint and injected native-close controls |
| Native decoder outside managed registration | Existing failure | Native decoder under the ECN restriction |
| Existing enabled/disabled GRO and lifecycle behavior | Preserved | Existing managed, external GRO and ancillary regressions |

Run focused regressions, one final Linux race run of `^TestManaged|^TestExternalGRO`, affected root-package tests/vet and module tidiness. Run repository lint on the changed Linux surface; existing hosted workflows supply their existing platform/compiler and integration coverage. Repeat only after code changes or to diagnose a failure. Keep the experiment subprocess-local, with a bounded timeout and no VM-wide restrictions.

Use one initial RAS review, verification of independently accepted fixes and at most one replacement review. A broader fallback, changed ownership, incompatible error behavior, required evidence beyond this budget or a repeated precise semantic root is a stop for decision. The implementation context consists of this contract, the linked issue acceptance criteria, receipt, changed source and relevant unresolved review findings.

Follow the [repository execution overlay](../REVIEW-LOOP.md): draft PR, exact-head local certification, successful applicable hosted checks on that head, ready transition and matched-head squash merge. There is no `task preflight` or portfolio `ci-*` gate. A changed base requires reconciliation and renewed applicable gates. After the product merge, append the dev journal without RAS, revalidate surviving deferred findings against the merged code, then complete the linked OmniFocus task.

## Managed ECN integration

The later Linux managed ECN qualification records each admitted family separately. The original dual-`EPERM` fallback above remains the only suppressed setup error when both receive options fail. A one-family receive-option failure on an admitted dual-stack socket is instead a usable partial-family fallback: managed ECN is false, the failed family is reported by `ecn_failed_family`, and an ECN-only reader is not retained when coalescing is inactive. Required packet-info failures and the fatal cases above remain unchanged.
