# GridCast fixture review dispositions

Initial full-panel review `20261031T030018-1037002ab379d3400ba2b246` reviewed PR 1062 at `65ad718e1c2cc3f9d83f6758fc5a9961011e9abd`, coordinated with infra review `20261031T030016-ebd111648672076a877103d4`. This is the sole prior fresh review of this PR. The fixed topology, profiles, fast.9 competitor, existing receipt schema and source-only evidence boundary remain unchanged. No historical campaign is rerun.

| Cluster | Disposition | Owner, correction and evidence |
| --- | --- | --- |
| C-003 (C-004/C-007/C-012 duplicates) | fix-now | Router lifetime consumes the infra sender-owned Linux monotonic deadline, with bounded preparation and no export tail. Fixed configuration and exact-600-second clock tests; Linux router cross-build; companion delayed-launch/common-expiry test. |
| C-005 | fix-now | Reconciliation accepts 60 nonnegative useful-byte bins with equal peer receipts and consistent totals; zero bins remain stalls. Zero-bin regression and inconsistent-total/overlap checks pass. |
| C-008 | fix-now | Malformed receipt shapes produce inconclusive JSON and nonzero status; subprocess CLI tests cover an empty offset pair and list-shaped run. |
| C-010 | fix-now | Gate cancellation discards have a separate counter. Actual socket/frame failures and historical cancellation retain SendErrors. Deterministic counter test and Linux build cover the changed source path. Application failure and insufficient overlap remain independent refusals. |
| C-011 | fix-now | Competitor activity uses its shared Linux sender/router clock; focal coverage retains peer-clock uncertainty. Tests cover ±50 ms bounds, varied sample phases, idle pre-start/post-end counters and an interior activity gap. |
| C-006 | fix-now after source investigation | observeControl records each opportunity, skips while a previous request is pending, and retains a later complete latency. Accept complete delayed replies only when offered + skipped equals all 60 opportunities and every offered probe replied. Preserve skipped counts. Unresolved probes remain censored/inconclusive, without replacement authority. Complete-delay and unresolved tests pass. |
| C-009 | reject as a flag change; evidence resolved | The canonical fixture decision explicitly retains conservative ACKs for this preparation. Frozen clean Reno/default-ACK vectors are outside the eight held physical slots. Infra retains and tests fixed conservative application vectors; competitor ACK behavior is unchanged. |
| C-001 | reject | No threshold-two behavioral counterexample was established. Different branches alone do not demonstrate different ACK policy. |
| C-002 | reject | Packet counters are explicitly activity evidence, not continuous useful delivery. No minimum-rate gate follows from the accepted contract. |

Focused evidence: model/router configuration/existing fixture tests against `gridcast.mod`, six Python receipt tests including malformed CLI input, changed-package go vet, tidy -diff, and Linux router cross-build. Exact-head verification and owning checks remain required before claiming reviewed completion. Rebuilt final identities must supersede initial draft hashes.

No traffic, calibration, address allocation, route mutation or release check has been performed. The canonical [fixture decision](https://github.com/GridCastIO/gridcast/issues/1705#issuecomment-6537913556) remains authoritative. Registration remains held for the real router allocation, final identities and accepted live-validation disposition.
