# Packet-loss failure diagnostics

This test-only change follows the operator's approval to capture the next naturally occurring failure of `TestHandshakeWithPacketLoss`. It does not claim a cause or fix for [issue #44](https://github.com/the-sarge/quic-go-fast/issues/44). The preceding bounded investigation completed 200 passing invocations without a failure; its local receipt is commit `6c5ec24f8e79bd6b421f5ab610407adc39f0c2ac` on `codex/issue-44-investigation`.

## Accepted outcome

The operator approved these criteria in the implementation task:

- “Identify every scenario explicitly.”
- “Record which handshake/read/close phase stalled.”
- “Preserve bounded packet and transport traces when it fails.”
- “Keep timeout, payload, and loss behavior unchanged.”

The owner is the handshake test fixture. A failure report identifies the requested direction, loss pattern, retry, speaking order, post-quantum setting, certificate-chain setting, and QUIC version. The original diagnostics change left existing subtest names unchanged. It retains the latest client/server helper phases separately from a bounded tail of router decisions and existing qlog events. Actual packet directions are recorded separately from the requested direction, since the historical one-third callback applied bidirectionally. The subsequent [issue #79](https://github.com/the-sarge/quic-go-fast/issues/79) correction honors the selected direction and gives each subtest an explicit name containing all configuration dimensions, while preserving the explicit `both` case with historical bidirectional semantics. Historical evidence remains unchanged; this fixture correction does not resolve issue #44 or concern issue #46.

The supported representation is these fixture configurations, its helper phase observations, actual callback decisions, and events serialized by the existing qlog encoder. Retained event data is limited to 256 records of at most 4096 bytes each; latest helper phases and the scenario header survive event eviction. Reports disclose eviction and truncation. Normal test assertion failures emit through `testing.T` cleanup into standard test output, including hosted integration logs. Passing cases emit no diagnostic dump. Fatal process termination, `go test`'s process timeout, scheduler-neutral observation, complete packet histories, and deterministic replay are outside the guarantee. A reported phase is the latest observed operation, not a causal attribution.

This is a maintained verification aid, not production transport behavior or a replay framework. No changes to random draws, loss decisions, simulated timers, payload, protocol recovery, timeout-helper ownership, or CI exceptions are authorized by this PR. Encoding, hashing, and locking add observation overhead; a passing instrumented test cannot exclude a timing-sensitive regression.

## Terminating evidence and review

The verification seams are the diagnostic collector's failure report and qlog recorder interface, the transparent loss-callback observer, and the fixture's cleanup integration. Deterministic tests cover failure-only output, scenario/latest-phase retention, bounded eviction and truncation, concurrent producers, qlog event serialization, and forwarding each supplied loss decision exactly once. A synthetic failing child test exercises actual cleanup without a randomized-loss campaign. These regressions discharge the diagnostic obligations; no comprehensive replay validation or probability claim is required.

Run focused deterministic tests during implementation, one focused race check on the final candidate, one final uncached full local suite, and repository static checks. Use the existing inherited hosted workflows as the exact-head CI gate: this repository has no `Taskfile`, `preflight`, or draft-gated `ci.yml`/`ci-*` workflow. Do not change CI in this PR. Require successful applicable hosted checks on the final head before guarded squash merge; a new unexplained test failure stops certification rather than triggering retries until green.

Review allowance: Standards and Spec review, one initial fully briefed RAS review, verification of accepted fixes if needed, and at most one replacement review. No new random capture campaign or historical-failure exception is included. Merge completes the diagnostic improvement; issue #44 and its original OmniFocus investigation remain open until causal evidence supports their resolution.
