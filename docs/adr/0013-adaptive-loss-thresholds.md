---
status: accepted
---

# Adapt application loss thresholds after surviving spurious proof

The owner accepted [#971](https://github.com/the-sarge/quic-go-fast/issues/971) after the independent history correction and publication of v0.62.1-fast.8. After an admitted, unexpired ordinary application-loss receipt, suppress application packet-distance loss until migration. Grow the application time multiplier through 9/8, 5/4, 3/2 and 2 RTT only when eligible observed reordering strictly exceeds the current effective delay. Clamp reported ACK delay and require a loss of a packet registered after the previous increase to permit another step. Allow at most one step per ACK. Initial and Handshake retain their default thresholds; 0-RTT and 1-RTT share application policy.

The sent-packet handler owns this path-local policy. Its single effective-delay calculation serves both ordinary and PTO-retired classification after current-ACK receipt admission, legitimate RTT update and adaptation. Late-only proof reschedules affected deadlines without sampling RTT. Reno restoration callbacks remain after applicable CE and genuine losses. Existing disposal fences and migration reset prevent stale receipts from training a new context. Persistent-congestion duration stays three PTOs, and PTO alone remains insufficient to establish loss.

This intentionally changes recovery classification for both default Reno and selected BBR, narrowly amending [ADR 0009](0009-opt-in-bbrv3.md), design D04 and [#617's effective-delay contract](2026-09-24-pto-retired-outcomes-plan.md). It preserves controller selection, Reno undo and typed prefixes, independent BBR ordinals/episode/delivery/cap owners, bounded stores, public/wire contracts and ADRs 0001/0003/0004's adoption and emission ownership. The [execution contract](../agents/adaptive-loss-thresholds-971.md) owns finite evidence.

Greater reordering tolerance can delay genuine-loss detection. Fixed thresholds are permitted policy; this decision makes no throughput or latency improvement claim and does not authorize the deferred comparison, native qualification or rollout. Frozen assessments retain their original source pins and limits.

Constant-sized handler state and a transient receipt batch preserve synchronous ownership. A dependency on BBR's optional retained store was rejected because generic recovery must remain independently authoritative; revisit only under an accepted evidence-ownership redesign. A shared wire-number/history import changes closed transport contracts; revisit only if those contracts are reopened. Splitting suppression and time growth was rejected on merit because intermediate policy states provide no useful independent outcome.
