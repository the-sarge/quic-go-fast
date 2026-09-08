# E6 contract re-slice audit

## Trigger and existing-work disposition

The [E6 preflight](https://github.com/the-sarge/quic-go-fast/issues/30#issuecomment-5577959166) froze dispatch at default head `c10fb5a79508b858ac9259ccbed1b326b5c7f75f`. The owner authorized this scoped handoff after the stop. No E6 implementation, review or performance campaign occurred. The clean `codex/e6-close-emission` branch contains only that base; retain it untouched as a stop receipt, not as implementation authority. E1–E5 remain complete. The original E6 contract is split into three independently green test-consumer prefactors and a smaller final E6 product slice; child #30 retains final E6 identity.

## Finite consumer census

The initial connection_test.go census has 44 `tc.packer.EXPECT()` sites in 22 functions, totaling 1,801 function lines. This is a source census, not a claim that line count proves inability or test equivalence. Inspection found distinct existing lifecycle/scheduling/path assertions that the newer emission tests do not automatically replace. Independent audit additionally found `TestHandshakeMTUFallbackBeforePacking` in connection_handshake_mtu_test.go (one expectation, 11 lines) and the broad-interface embedding `emissionObservedPacker` with two caller-buffer test users. The complete expectation census is therefore 45 sites in 23 functions; the wrapper is a separate type-level dependency. The two shared constructors also install `MockPacker`. The current plan assigns each function once and preserves assertions before deleting interactions.

| Batch | Functions | Baseline lines | Existing behavioral boundary |
| --- | --- | --- | --- |
| E6a | Nine handshake/lifecycle/buffering/version/MTU functions | 636 + 11 | Handshake events, parameters, timeout/error and receive buffering |
| E6b | Nine scheduling/batching functions plus the two wrapper users | 709 plus caller-buffer tests/wrapper | Receive ordering, timer wakeups, idle/keepalive/ACK and GSO/queue output |
| E6c | Three ID/path functions | 371 | Retry/ID selection, validation and migration policy |
| E6 | Two close/error functions | 85 | Close fields, suppression/error precedence and retained payload |

The full per-symbol assignment and current acceptance contract are in the normative plan. The mechanically counted original connection_test.go function-line total is 1,801; the separate MTU function and wrapper users are now included in the assigned repository census. No plan context budget is inferred from line count alone.

## Self-grill and gate decisions

A monolithic migration would mix handshake events, virtual-time connection scheduling, path policy, close retention and final measurement. A generic replacement packer mock would keep the rejected taxonomy. Deleting every old test with a similarly named outcome test would lose assertions. Instead, real construction is installed only for each migrated batch; existing default mock setup remains coherent for unconverted callers until E6. There is no new runtime state or interface in these prefactors and no requirement to build a reusable measurement/test framework.

The three batches have no mutual behavioral dependency. They can edit disjoint test functions against the same completed runtime; shared-helper conflicts require serialized integration, not invented blocking edges. The final contraction depends on all three because their remaining mock consumers would otherwise fail compilation after interface deletion. E6 retains close behavior and full qualification in the same product PR. Performance margins and the full initial-plus-one-replacement budget are unchanged; no new capture runs in this handoff.

No product or representation choice changes. Production codecs and existing runtime owners retain authority; test observations are example-level and negative source claims range only over the explicit census. Closure is not applied recursively to the test migration. Existing outgoing-buffer closure still applies to final close ownership. No security, persisted state, protocol policy or public interface change is approved. The live mirrors remain frozen until the audited contract is merged and reachable from default HEAD.

## Independent slice audit

The independent read-only slice audit passed against runtime pinned to `c10fb5a79508b858ac9259ccbed1b326b5c7f75f`. Two substantive findings were independently accepted as `fix-now`: the omitted MTU test is now assigned to E6a, and the nonmechanical observation-wrapper migration is assigned to E6b. Both fit the existing batch outcomes and budgets; neither requires a new runtime owner or capture. The wrapper replacement explicitly covers ordinary/large pools, sequential execution without workers/concurrent allocation, matching capacities, cleanup restoration and first/later failure plus empty-buffer behavior through the existing sealing-manager boundary.

The revised audit confirms independently green batches, context fit, genuine blocking edges, coherent retained defaults, bounded artifacts/representation/evidence, and final E6's retained close/contraction/qualification boundary. There are no unresolved substantive audit findings, no repeated-root conclusion and no runtime implementation. Relative links, source anchors/census and Markdown whitespace are checked before commit. Hosted publication receipts belong to the docs PR; issue pointers are synchronized only after its exact merged plan commit is reachable from the default branch.
