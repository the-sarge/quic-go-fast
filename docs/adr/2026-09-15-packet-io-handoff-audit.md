# Packet-I/O handoff audit — 2026-09-15

## Scope and existing-work disposition

The prior local design commits are retained as design input and replaced as dispatch contracts by the two current plans. No runtime implementation is adopted as a baseline. GitHub open-PR/issue searches found no implementation for this integration; wiremux has an unrelated dependency PR, and the fork has no open PR. Fork main advanced through fixture/journal repairs, which were incorporated before this audit; they change no proposed socket API. The initiating checkouts remain read-only.

Wiremux inspected source: `b290852d134d7f45795219671951336d32477ae4`. Fork inspected source: `8d3d151a4da565a21c6c2e77c17d83bd5e07ff06`. The active fork design branch was rebased onto that default branch before drafting. Prior source reasoning remains valid on the unchanged packet owners.

## Slice audit decisions

The source audit identified transport send workers that may outlive Transport.Close, concrete-native-only capability preservation in the wiremux wrapper, transport-local coalesced readers, and AddPath admission that can change the local socket. The accepted plan resolves these through endpoint generation revocation/joining, a provenance-checked managed adapter, persistent normalization and complete fixed-origin/fixed-target path restrictions. These are first-pass architecture corrections, not a claim of repeated review roots.

R01 was split into ordinary endpoint, QUIC binding, Linux normalization and Windows normalization. P01 was split into inert hook consolidation and complete policy activation. Neither split leaves partially active security authority. E02 was split by native platform plus a support decision. C01 now has separate codemux compatibility/release, GridCast and tapmux children before reconciliation. P00 is fulfilled by this handoff, not dispatched recursively. Every mandatory eventual outcome remains represented.

Removed convenience edges: E01 need not wait for wiremux runtime; Windows URO need not wait for Linux GRO; standalone endpoints and fixed-peer hooks need not wait for the first full-Wire milestone. W02 is independently useful with the ordinary writer supplied by Q01. Native baseline edges remain because their receipts are required by the capability's accepted qualification gate.

All slice contracts record their owners, representations, artifact classes, finite evidence, context bounds and split/merge judgment. No maintained verification framework is approved. No repeated-root conclusion is asserted; future such conclusions require the shared exact-invariant/enforcement-owner comparison.

## Consumer inventory

| Repository | Inspected revision | Source and disposition |
| --- | --- | --- |
| GridSwarm/codemux | `f91792a8a92d0c980211633d5eb5d6bd8d17e076` | `go.mod:8,23`: wiremux v0.2.1 / QUIC v0.61.0; C01-C compatibility and C01-CR release |
| GridCastIO/gridcast | `676b2de773802685a1813b9c2793466ca36a3f40` | `go.mod:9,10,31`: codemux v0.5.0, wiremux pseudo-version, QUIC v0.62.0; C01-G main-module adoption |
| SwarmCast/tapmux | `9fb76ebd693ee4887fa26bcffc74b6930e4ca177` | `go.mod:8,9,23`: codemux v0.5.0, wiremux v0.2.1, QUIC v0.61.0; C01-T main-module adoption |
| GridSwarm/keymux | `67bdae31a7f933d1e0699d08e0042543eaf839a0` | No Go module; AGENTS documents deferred implementation. No fabricated migration |
| SwarmCast/swarmcast | `c14986943679dd20faedd5dd254f130957e96027` | Current module has no wiremux/QUIC dependency; recheck at C01 |
| SwarmCast/legacy/swarmcast-legacy | `ea5eea40c6b133ac5d922412d6fb5b04251d58b1` | `go.mod:12` pins old QUIC v0.59.0; legacy migration excluded from this integration |

The repository-root module search also found upstream quic-go, go-libp2p examples/test plans and go-btfs checkouts. They are not identified owned active consumers of this wiremux integration; do not migrate third-party/example checkouts by inference. Fork integration fixtures and wiremux tools are dependency/validation contexts, not separate application adopters. C01 rechecks the actual portfolio before closeout and requires named audited children for any newly active in-scope consumer.

## Handoff publication audit

Independent read-only agent audit completed against the current contracts. Accepted fix-now findings were resolved: R01-B gained its Q01 dependency and callable generation-checked writer; R02 gained the explicit owner-carrying managed option; P02 received a one-platform preliminary comparison with defaults deferred to E02; generic join stops were scoped to slice obligations; transport-wide callbacks gained explicit concurrent-use/scratch ownership. No further split was required after the 32-slice graph audit. Documentation corrections received targeted link, graph and whitespace checks; another review/verify cycle is unnecessary under the shared docs-only correction policy. Publication and exact default-branch receipts belong to the docs PRs and final handoff report. This file is historical audit context, not the current implementation contract or live blocker ledger.
