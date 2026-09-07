# Packet-emission handoff audit — 2026-09-07

This artifact records discovery and slice-audit evidence for [QGF-PE-2026-09](../adr/2026-09-07-packet-emission-program.md). The [current plan](../adr/2026-09-07-packet-emission-plan.md) owns the accepted contract; this record is not a second dispatch plan.

## Sources and existing-work disposition

The fork default branch is `main`, with inspected baseline `d0baa96481ad27eaa7cff3f0fbca0d6a42ad726e`. This includes setup PR [#22](https://github.com/the-sarge/quic-go-fast/pull/22), the completed datagram and handshake program, and its journal entries. It is not the straight upstream mirror examined in the initial conversation. No packet-emission implementation PR or issue was open at discovery.

The DEVONthink record [quic-go-packet-emission-architecture-review-2026-08-13](x-devonthink-item://2B919D39-BBB9-47DD-BF40-3652AB1C0574) examined upstream `9bdf5153b9362fbcc0c82204ce1cc1f6ba2de513`. Its packet-emission boundary, real-packer behavioral seam and preservation of connection lifecycle/fairness are retained. Its review is rationale, not certification of the current fork. The word “transaction” is deliberately clarified to mean an ordered emission sequence, without rollback of packet registration on local write failure.

The unmerged local transmission design at `a8e40d490c112f5821922f4273fee548235cb4d6` contains documentation only. Its accepted performance-preserving outcome and execution-model decision are retained; its T0/T1/T2/T3 breakdown is replaced with E1–E6. No code or unmerged runtime assumption is adopted from that branch. The original checkout and that design worktree remain untouched.

The completed QGF-2026-09 tracker [#7](https://github.com/the-sarge/quic-go-fast/issues/7), datagram track [#2](https://github.com/the-sarge/quic-go-fast/issues/2) and handshake track [#3](https://github.com/the-sarge/quic-go-fast/issues/3) remain closed. Conditional collector issue [#18](https://github.com/the-sarge/quic-go-fast/issues/18) is not a dependency unless a future slice deliberately reuses that collector. There is no unresolved packet-emission finding family being grandfathered or grouped by a broad module name.

The supplied OmniFocus task `iWdczzYqn7j`, “packet emission architecture,” belongs to project `aG3Zb4iz4VU`, “quic-go-fast.” Discovery found an empty note and no children. The new track and child mirrors belong under that task; no unrelated tasks or project metadata are in scope.

## Publication and validation policy discovery

The live repository had no rulesets, no main branch protection, no open PRs and no repository REVIEW-LOOP.md or CONTRACT-CLOSURE.md overlays. The inherited unit, integration, lint, cross-compile and interop workflows apply; draft PRs do not imply skipped CI. The handoff uses the shared skill baselines directly and waits for applicable triggered checks before a head-guarded squash merge. No portfolio CI standardization is included.

The user explicitly invoked architecture-handoff and delegated technical recommendations. This authorizes the docs publication/merge and GitHub/OmniFocus package, but no runtime implementation or implementation-agent dispatch. The modifying worktree is `/Volumes/worktrees/quic-go-fast/packet-emission-handoff`, branch `codex/packet-emission-handoff`, based on the fork commit above.

## Proposed graph and granularity judgment

E1 is a disposable feasibility experiment with a legitimate no-adoption outcome. E2 closes the existing queue lifetime independently. E3 migrates ordinary/GSO emission only after positive E1 evidence and E2's complete queue handoff. E4 migrates handshake/ACK/PTO and E5 migrates probes/path handoff independently after E3. E6 owns retained close output and removes the legacy seam after both migrations. Each slice includes its strongest split/merge alternatives, owner map, finite evidence, context budget and approach stops in the current plan.

The original full-extraction step is too broad for a single fresh context. The replacement follows expand–migrate–contract while keeping each packet class routed through one orchestration owner. E2 is a complete safety prefactor, not a horizontal helper-only stage. E4/E5 share files but not a behavioral prerequisite; integration serialization must not become a false blocking edge.

## Independent audit receipt

Independent read-only agent `audit_slices` audited a frozen snapshot of the five authored documents against architecture-handoff and the shared baselines, inspecting source only at the pinned fork baseline. Result: **PASS, no blocking findings**. No runtime tests, edits, implementation or dispatch were performed by the auditor. The root agent independently checked the source anchors, all relative links, glossary/ADR shape, ownership transitions and graph before accepting the result.

| Slice | Accepted independent judgment |
| --- | --- |
| E1 | Complete finite decision outcome; ≤45k context is credible with raw captures as artifacts. Positive feasibility is a prerequisite, not maintained-fixture approval. Reject a horizontal fixture-only split or immediate production adoption. |
| E2 | Complete independently useful queue lifecycle; successive exclusive owners and six scenarios fit ≤25k. Splitting current-entry from stopped/leftover cleanup would divide the same terminating lifecycle. |
| E3 | Normal/GSO share capacity and per-packet recovery ordering; ≤40k is credible. Positive E1 and merged E2 are genuine blockers; bounded legacy forwarding has E4–E6 removal edges. |
| E4 | Handshake/ACK/PTO are one bounded coalesced-registration family; synchronous key effects match source. ≤40k is credible; E5 is not a prerequisite. |
| E5 | Existing probe destinations, direct/queued execution and path lifetimes are explicit; ≤35k excludes policy redesign. E3 supplies the common owner; E4 is an integration concern only. |
| E6 | Retained close completes migration and permits deletion; ≤40k depends on earlier slices removing their own obsolete tests. E4/E5 truly block contraction; unexpected remaining breadth requires re-handoff. |

The auditor's sole optional clarification was independently dispositioned **fix-now** as a small contract-explanation correction: explicitly state why focused queue tests do not cover the separate private-construction, direct-send and retained-close transitions. This changes neither the accepted domain, graph nor evidence budget. The plan now says so. Lightweight local docs recertification replaces an additional review under the shared docs-only policy; no substantive finding remains deferred or stopped.

Local docs validation confirmed the cited baseline symbols, all authored relative links, newline termination and whitespace. The only repository changes are the five intended Markdown files. Hosted checks and merge receipts belong to the publication PR and post-merge journal, not to duplicated certification assertions in the normative plan.
