# Preparation-owned packet construction for N3 #1202

**Verdict:** the native ordinary path now populates its existing recovery packet directly, and generated code confirms removal of the unconditional 128-byte packet-metadata forwards without a new preparation allocation. Correctness and race checks pass. **Both observations in the one matched diagnostic are contaminated, so this run establishes no performance gain or target pass.** The latest clean evidence remains [canonical geometry](2026-11-05-native-canonical-geometry-prototype.md): 78.1% baseline goodput and 124.3% CPU/useful byte, against the unchanged 95% and 110% targets.

## Source and ownership

Frozen diagnostic source is `302ccab12fa200f013259aded0d61aefbef449c8`, based on `2c319d852387b6a91fa14c6939cf98d636347317`; source archive SHA-256 is `44e9c5a46515bbaf7c066e65d670568bf96b18fd931fe74de6a6143f48cbe65e`. Compared with the last measured `990a7a4`, this composition also includes the separately proved [empty-loss-prefix guard](2026-11-05-native-lifecycle-proof-followup.md), which had not previously been measured. The diagnostic source remains on its feature branch; it is not a shipping change or a new dispatch contract.

Eligible native path-zero ordinary packets write final construction facts into the packet already owned by preparation. The pooled recovery packet's size is unchanged. A 32-byte preparation handle stages the final STREAM slice header while leaving the original cap-one backing reachable for failure cleanup. A 32-byte borrowed view serves logging and activity before adoption; wire-only facts remain in a transient 40-byte header. After registration, only a 32-byte receipt containing the original control-frame slice header and byte size survives. No recovery packet or borrowed view is read after callbacks can ACK and pool it.

Seal, header-protection and packet-number commitment order remain unchanged. Key generation is sampled immediately before Seal. Logging, terminal-receipt capture and activity precede the largest-ACK read; recovery validation precedes canonical identity allocation and handle consumption. Both preparation fields are consumed before recovery callbacks, while final STREAM adoption and canonical packet linkage remain after the accounting/logger prelude. Recovery and local-sender receivers are resolved at their original late points. The two DATAGRAM classifications still read the original control backing at their respective post-callback points; a precomputed flag would change behavior. The registration diagnostic span retains its original boundary.

Legacy, routed, path-credit, probe, coalesced and DATAGRAM-only behavior continues through the value path. The three ordinary send callers select the new path directly using an inlined eligibility predicate. An initial non-inlined receipt adapter was removed after disassembly showed that it added an avoidable call and wide copy to baseline non-GSO sends. Independent callback review and a complete field/transition census found no remaining blocker. Native mode's one-time initialization invariant is documented; no test-only mode-flip fallback was invented.

## Compiler evidence and baseline cost

The prior ordinary append/registration/logging chain performed eight 128-byte metadata transfers, covering 1,024 logical bytes, plus three 128-byte clear blocks; GSO callers had another returned-value copy/clear. These are metadata counts, not payload bytes or hardware memory-traffic measurements. The new native chain bypasses those unconditional aggregate transfers. Rich logging materializes the old value only when debug frame logging or qlog is enabled. Narrow registration also removes the old wide argument forwarding. Required initial packet stores, canonical acquisition, ACK-target copies and per-packet recovery policy remain.

Go 1.27.3 Linux amd64/v1 `ordinarycost` inspection and escape analysis find no new successful-path allocation for the preparation handle, header or receipt. Preparation contents reach existing retained packet/frame backing; the header itself does not escape. The final local inspection binary has SHA-256 `cc268ea4bc3d3eebfabad657bd1bfd9fc5028e7d01c86f39982868030b6335d2`. This is distinct from the remote measurement binary below.

This is not an isolated native-only change. Shared serialization adds a non-inlined helper call, additional CID/payload argument traffic and a 40-byte result transfer; shared recovery accounting/publication add two calls. The legacy preparation variable also clears 32 rather than 8 bytes. Those costs are disclosed rather than treating relocated work as eliminated work or attributing a slower denominator to native improvement. The compiler evidence proves a structural change, not its net CPU or throughput effect.

## Validation

Existing-API growth/refusal and callback-order characterizations passed on the unmodified base. New tests cover original-slot cleanup after slice growth, refusal without ownership transfer, consumption before callbacks, late STREAM adoption, nested ACK and pool churn, both late DATAGRAM reads, failed sealing without PN consumption, post-seal refusal without PN refund, generation sampling and late ACK observation. An initial paired test incorrectly expected a packet-number generator to survive `CloseLocal`; both implementations failed that fixture. It was corrected to refuse via invalid registration time, with no production fix, and both logs are retained.

Final full root tests passed in 12.134 seconds; full recovery race tests passed in 74.274 seconds; native/construction race checks passed in 3.508 seconds. Recovery source was unchanged by the final direct-caller correction. Focused tests, vet, tidy-diff and diff checks also passed. Linux native semantic checks and the full recovery package passed before measurement. These checks do not constitute the full mixed-traffic, hostile-resource or release qualification.

## Contaminated observation receipt

Both observations used the same remote Linux amd64 Go 1.27.3 binary, SHA-256 `f814f4040934b1f045c0e8fccd628b30a42970a0f5fd26e5baa0e258fd9f1080`, now downloaded and locally rehashed. The fixture retained server CPUs 8–11, client CPUs 12–15, monitoring of those CPUs plus siblings 24–31, seed 1038, 10-second warmup, 60-second measurement and 90-second observation limit. Both server/client pairs exited successfully. Neither observation was repeated.

| Raw observation, not acceptance evidence | Baseline | Native |
| --- | ---: | ---: |
| Useful bytes | 87,993,221,120 | 74,200,252,416 |
| Useful MB/s | 1466.553685 | 1236.670874 |
| Server window CPU seconds | 108.627013 | 96.915388 |
| CPU ns/useful byte | 1.234492971 | 1.306132861 |
| Foreign CPU seconds across whole observation | 74.438790 | 36.900708 |
| Existing foreign-CPU limit, seconds | 3.852020 | 3.852727 |
| Control p99 µs | 1245.183 | 1048.575 |
| Application send-call p99 µs | 118.783 | 147.455 |
| Peak RSS KiB | 30,660 | 33,064 |
| Lost packets | 534 | 1578 |

Contamination is derived from scheduler time on the fixture CPUs and their sibling threads, subtracting the test processes and measured softirq-thread CPU. It is not inferred merely from a named background process or activity on unrelated efficiency cores. The raw scheduler snapshots are not separately retained, so the review verifies the calculation's source scope and recorded totals rather than independently reconstructing the subtraction. Quiet checks passed before each observation; they cannot prevent later interference.

Raw arithmetic yields 84.3% goodput and 105.8% CPU/byte. **The apparently passing CPU ratio is not an acceptance result.** It cannot replace the latest clean 0.147581579 ns/byte target gap or establish that either lane improved. Profiles, sampled allocations and counters are retained for provenance and exploratory source attribution, not comparative timing claims. Native counters show 1,951,023 groups / 4,531,775 members, no refusal or expiry, and 4,531,770 native calls; separately recorded window counts differ by five and are not interchangeable.

## Evidence and next decision

Raw evidence resides in `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/owned-construction-v1/`; the source archive, measured binary and 54-file `owned-construction-v1-local-validation/` manifest are adjacent. Local evidence preserves source/test patches, failed and successful tests, independent reviews, before/after adapter evidence, disassembly and escape analysis. Analysis explicitly recognizes both prepared-registration entries and the new construction entry so costs do not disappear into another owner bucket; baseline allocation attribution includes `SentPacketWithContext`. Accessor package names can shift mechanism-only labels, so the separate callable-owner view is the comparison boundary. An independent evidence audit checks source/binary identities, available hashes, arithmetic, contamination and accounting.

This adds five invocations / 186.921215591 measured host-seconds: 32.826269277 for build and semantic checks, plus 154.094946314 for observations. Preparation and quiet checks add 64.212770410 seconds separately. Cumulative history is **208 invocations / at least 4147.341475609 measured minimax host-seconds**, including failed and contaminated evidence. Local tests and compiler inspection are not minimax workload invocations. No workload remains running and no rerun is queued.

Carry the verified ownership/copy reduction into the larger design while leaving its performance benefit unresolved. Generic canonical-record recycling remains unsafe: generation-tagged external tokens do not protect old packet, ECN, rich-feedback, ACK-carrier and callback-stack references. A narrower epoch-local ACK-boundary cache would need explicit borrower/quiescence proof and larger tokens; it is future design input, not implemented or selected here. Continue investigating construction/registration and ordered completion work while retaining the independent writer and exact native-return semantics.

Recommendation: retain this diagnostic source and its limitations, keep [N3 #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) stopped and [draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) held, and use the latest clean pair for current target status. This run authorizes no performance claim, automatic retry or new implementation graph.
