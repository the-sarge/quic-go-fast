# Datapath Offload Program — 2026-09-11

**Identity:** QGF-DP-2026-09. **Status:** Accepted; G1 complete (#236).

## What this is

The dispatch index for the datapath offload program: Linux coalesced receive, the Windows datapath rebuild with USO/URO, and Darwin batch send plus a receive-batching experiment. The grilled decisions live in the [datapath offload plan](2026-09-11-datapath-offload-plan.md); each track plan below owns its slice contracts and is the normative source of truth for implementers. Audit history: [handoff receipt](../audits/2026-09-11-datapath-handoff/README.md), [KeibiSoft discovery](../audits/2026-09-11-keibisoft-gso-discovery.md), [stack comparison](../audits/2026-09-11-quic-stack-datapath-comparison.md), and the consideration/verification chain referenced in the receipt. This program is separate from the 2026-09-08 architecture deepening program (QGF-AD-2026-09) and creates no cross-program blocking edge; G shares `sys_conn_oob.go` and retention-queue surfaces with that program's I track — serialize shared-file integration in worktrees as that program already requires, since shared filenames alone are not dependency edges.

## Outcomes that require no implementation

io_uring and XDP datapaths are rejected for this program (off-by-default or platform-locked even in msquic; unfit for a Go library's portability contract). Porting the KeibiSoft receive half as-is is rejected (it has never batched). A direct `x/sys/windows` overlapped Windows datapath is rejected in favor of standard-library message I/O. Version sniffing is rejected in favor of runtime probes. The ADR 0005 amendment (2026-09-11) authorizing the coalesced slab is settled; do not re-litigate it or the caller-owned-socket rule.

## Tracks, dependencies, and frontier

| # | Track | Plan | Parent issue | Blocked by | Slices | Status |
|---|---|---|---|---|---|---|
| G | Linux coalesced receive (GRO) | [G plan](2026-09-11-linux-gro-plan.md) | #225 | None | G1 done (#236), G2 | G2 FRONTIER |
| W | Windows datapath (foundation, USO, URO) | [W plan](2026-09-11-windows-datapath-plan.md) | #226 | None (W3's G1 edge satisfied; W3 still requires W1) | W1, W2, W3 | W1 FRONTIER |
| D | Darwin batch send + receive experiment | [D plan](2026-09-11-darwin-batch-plan.md) | #227 | None | D1, D2 | D1 FRONTIER |

Cross-track slice edges: W3 requires G1 (coalesced-storage contract and split helper) — satisfied by #236. In-track edges: G2 requires G1 (satisfied); W2 requires W1; W3 requires W1; D2 requires D1. The frontier is G2, W1, and D1; W1 and D1 touch disjoint files and are parallel-safe, and G2 activates G1's merged machinery. The program's evidence-per-effort ordering recommends completing G and W adoption before D by preference; that recommendation is not a blocking edge. Seven intended PRs, seven fresh implementation contexts; G1 (#236) is merged.

## Rules that bind every track

ADRs [0001](0001-upstream-compatibility.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md), and [0005](0005-incoming-packet-lifetime.md) (as amended 2026-09-11), the root `CONTEXT.md` glossary (coalesced receive, receive batch, segmented send), and the shared REVIEW-LOOP/CONTRACT-CLOSURE baselines supplied by `$implement-architecture-slice` govern; no repository-specific overlay exists. The [datapath offload plan](2026-09-11-datapath-offload-plan.md) binds every slice: no public API change; runtime probes with the `QUIC_GO_DISABLE_GSO`/`QUIC_GO_DISABLE_GRO`/`QUIC_GO_DISABLE_SENDMSG_X` switches; offload socket options only on transport-owned sockets; adoption only through a precommitted protocol and results pair under `docs/audits` with predeclared statistical bounds and an engagement metric.

Child issues pin the exact verified default-branch plan commit; issue and OmniFocus mirrors contain only current state and pointers. The per-slice ritual is bounded review → exact-head local and same-head hosted checks → guarded squash merge → append-dev-journal after merge → complete that slice's OmniFocus task. Every child uses `$implement-architecture-slice` in a fresh context; a representation/owner/topology/adjacent-scope/product/irreversible-authority/one-PR change returns for decision or architecture-handoff. This handoff publishes the package and reports the frontier; it dispatches nothing.
