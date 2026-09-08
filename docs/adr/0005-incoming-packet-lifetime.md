---
status: accepted
---

# Preserve incoming packet lifetime through successive owners

Keep incoming UDP storage with successive concrete receive owners, rather than introducing a global receive module. A consuming handoff transfers or disposes responsibility; connection parsing keeps an explicit active reference while counted QUIC packet views may be retained, and admission is sealed before terminal drains. This replaces the hidden zero-count-but-still-parsing convention so synchronous retained-view cleanup cannot recycle active bytes, while preserving the connection goroutine, socket ownership and protocol behavior.

The [incoming lifetime plan](2026-09-08-incoming-lifetime-plan.md) owns the staged contracts and finite evidence. This is separate work from the completed emission program’s receive exclusion; it does not reopen that program, change its historical results or authorize a receive engine, atomic refcounts or another performance campaign.
