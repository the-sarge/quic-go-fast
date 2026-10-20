---
status: accepted
---

# Preserve incoming packet lifetime through successive owners

Keep incoming UDP storage with successive concrete receive owners, rather than introducing a global receive module. A consuming handoff transfers or disposes responsibility; connection parsing keeps an explicit active reference while counted QUIC packet views may be retained, and admission is sealed before terminal drains. This replaces the hidden zero-count-but-still-parsing convention so synchronous retained-view cleanup cannot recycle active bytes, while preserving the connection goroutine, socket ownership and protocol behavior.

The [incoming lifetime plan](2026-09-08-incoming-lifetime-plan.md) owns the staged contracts and finite evidence. This is separate work from the completed emission program’s receive exclusion; it does not reopen that program, change its historical results or authorize a receive engine, atomic refcounts or another performance campaign.

Amendment (2026-09-11): the [datapath offload plan](2026-09-11-datapath-offload-plan.md) authorizes one narrow exception to the atomic-refcount exclusion — coalesced receive storage may use an atomically reference-counted slab, confined to backing the segments of a single coalesced socket read, because sibling segments of one read can route to owners that release concurrently. The exception extends no further: ordinary incoming storage keeps this decision's non-atomic counted-view contract, and no general receive engine or performance campaign is authorized by it.

Retirement (2026-10-20): the exception is retired by [#860](https://github.com/the-sarge/quic-go-fast/issues/860). Windows now copies each datagram of a coalesced read out of its reader's buffer, as the Linux GRO reader has since [#853](https://github.com/the-sarge/quic-go-fast/issues/853), so no coalesced slab, atomic view count or retained-bytes budget remains, and all incoming storage keeps this decision's non-atomic counted-view contract. The frozen W3 (`w3bench`) and G2 (`grobench`) measurement harnesses reference retired symbols and no longer compile on `main`; build them at their recorded source commits.
