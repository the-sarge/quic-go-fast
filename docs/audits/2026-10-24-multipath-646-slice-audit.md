# Multipath #646 slice audit

**Status:** passed independent slice audit and scoped correction verification; all 31 slices accepted for documentation publication. No implementation or experiment was performed.

Three read-only agents independently inspected immutable draft snapshots against source `3b5399df8da2888a9f6e7748dbe75aaf825cb12e`. Endpoint audit covered E1–E6; protocol audit R1–R10; lifecycle/BBR audit R11–R19 and B1–B6. Each used the architecture-handoff, design-cost and contract-closure gates. No implementation, experiment, branch mutation or external write was delegated.

## Initial inputs

| Snapshot | SHA-256 |
| --- | --- |
| `2026-10-24-multipath-program.md` | `c9e4a8caffc5db7da93d3a62283ac5669d71374808323c82afeb5c3dcbad22f2` |
| `2026-10-24-multipath-reno-plan.md` | `47dd4e073532a6d9a9033cecc93b6595cd87f59c1ad62234a99a1dd878b7cc7b` |
| `2026-10-24-multipath-endpoint-plan.md` | `b6a4b607b1c06d9309266896a158c5c3a09794e069cc702aad47a44b32edffb6` |
| `2026-10-24-multipath-bbr-plan.md` | `c50127a946c83f7cad10ef87c1e4ed1615b5171edcde2c1e03d23740e1e1d401` |
| `quic-multipath-d0.md` | `fc097defeb0f2a6b83caf3fc065c897cad444018519b9a5a46302a36150a3bb2` |

Snapshots and full local review material: `/Volumes/worktrees/quic-go-fast/multipath-646-handoff-review/slice-audit-v1/`. Source files were pinned; snapshot fingerprints identify uncommitted planning input, not an accepted plan commit.

## Independent author dispositions

| Finding | Obligation / owner | Disposition | Current correction |
| --- | --- | --- | --- |
| SA1 | E5 ordinary live close and drain | fix-now | Assign connection.go destruction/drain and packet_emission.go live-close submission to E5; current-write/close/late-return regression. R14 extends this for multipath. |
| SA2 | E6 native contention evidence | fix-now | Qualify native endpoint participant/path request classes in E6; actual negotiated mixed traffic remains R17/R18. Keep 12 native instances; no new wire dependency or false integrated claim. |
| SA3 | E2 completion consumer context | fix-now | Name connection event-loop/mailbox and ackhandler interface consumers, terminal disposal and late completion. Protocol policy stays connection-owned. |
| SA4 | Endpoint representation domain | fix-now | Include granted ordinary v1/v2, stateless/control and public non-QUIC writes separately from the narrower v1 multipath negotiation domain. |
| SA5 | R8/R9 mandatory terminal transitions | fix-now | R8 owns unused-issued-ID abandonment; R9 extends the same owner to consumed/validating/live initial path teardown. R12 adds replacement credit/late-delivery integration rather than first cleanup. |
| SA6 | Early independent peer smoke | fix-now | Keep R10 deterministic owner integration; move independent smoke to R14 after path-qualified close/cache handling. Move its R1 edge with it. |
| SA7 | R5/R6 producer-consumer context | fix-now | Include sealing through ACK pooling/connection forwarding for actual key generation, plus lost-carrier receipt lifetime for ACK pruning. No forced slice merge. |
| SA8 | Stable connection RTT observation | fix-now | R7 creates one stable observation for connection and existing stream flow controllers; R12/R15 update it on zero retirement/primary changes. Add focused preservation evidence. |
| SA9 | R11 public DATAGRAM admission | fix-now | Include Conn.SendDatagram, max-payload observation publication and both packing consumers; one public error/size regression within budget. |
| SA10 | B5 negative result and B6 fallback | fix-now | A valid negative comparison completes evidence delivery and denies aggregate support. Invalid/incomplete evidence stops. B6 can publish qualified BBR primary/backup with aggregate unsupported. |

All ten corrections serve the accepted runtime/safety or dispatch-contract boundary. They are bounded documentation changes; none adds a product capability, new platform or operational campaign. They were checked against the relevant source owners before acceptance. The audit also removes redundant transitive edges E3→R9 and R1→R17, and adds exact per-slice context imports. The graph remains 31 slices and acyclic. Complete negative evidence is not confused with missing evidence.

The initial audit found E1/E3/E4, R1–R7 and B1–B4 coherent; the remaining verdicts were conditional on the corrections above or their predecessor contracts. Source anchors and the audit alternatives are recorded in the current plans. The final D0 consideration was separately dispositioned and its accepted corrections were included in the scoped verification below.

## Existing work

PR #1010 was inspected at `fb0a8d467b83cef67582bf5daac069f2304c310b`: seven documentation files only, no review/comment history and no implementation. Its hosted checks were successful when inspected. Retain those design/research/audit artifacts as history in this branch; supersede the exploratory PR with the audited handoff publication rather than treating it as an established implementation dependency. Q2 #599 remains outstanding under its own execution gates; this audit grants it no readiness or new budget.

## Scoped correction verification

All three independent auditors passed the v3 contracts: endpoint E1–E6; protocol R1–R10; lifecycle R11–R19 and B1–B6. They verified immutable v2→v3 diffs against the same pinned source. The final consideration response supplies F1–F10 dispositions and the precise-root comparison. The scoped verification corrected PTO candidate extraction and Retry/reset as consumers of pending-local state, copied the complete retained-history/attachment contract into the Reno plan, separated primary eligibility from transient send capacity, corrected the root flow-controller source path, and reconciled R1/R8/R9 evidence/owner pointers. No slice, blocker, context ceiling, supported platform or operational campaign was added.

| Verified snapshot | SHA-256 |
| --- | --- |
| `2026-10-24-multipath-endpoint-plan.md` | `be8821298c08343a0a791de3fcb8b977258c227ca43929cbb5521b49ebc1e218` |
| `2026-10-24-multipath-reno-plan.md` | `0da6512b127e7602a25c9bf0f5fc1ea98bcec35009fe9f942e1385404795e596` |
| `2026-10-24-multipath-bbr-plan.md` | `8b4f16b42ee230c7d67ed02251d92d74fdaa43a561f011010698cd8477da2ca3` |
| `quic-multipath-d0.md` | `69928db74977ef944908937d0df1c19101fd1a781d863a63096e4be28e452baf` |

Snapshots: `/Volumes/worktrees/quic-go-fast/multipath-646-handoff-review/slice-audit-v3/`. After these passes, publication edits only change status/history pointers and summarize the already-audited 27 construction/support plus four qualification deliveries; they do not change runtime or slice contracts. Final local certification validates relative links, exact graph/identity agreement with the publication index, acyclicity, frontier E1/R1/R2, JSON forecast sums, the 140+40 run ledger, unchanged synthesis hash and whitespace. Exact pushed-head certification and same-head hosted CI remain required by the repository overlay before merge.
