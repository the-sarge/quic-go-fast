---
status: accepted
---

# Separate Darwin batch compatibility from performance adoption

Retain explicit kernel admission for the private `sendmsg_x` path because a startup delivery probe cannot establish every partial-send and error convention on which safe retry depends. Extend support through the bounded [Darwin batch compatibility policy](../darwin-batch-compatibility.md), with native evidence matching the architectures enabled by the runtime gate, while preserving the startup self-check and ordinary-send fallback.

For future kernel qualifications, this decision clarifies the scope of the [datapath offload plan](2026-09-11-datapath-offload-plan.md) and [Darwin batch plan](2026-09-11-darwin-batch-plan.md): the original statistical performance-adoption campaign is not repeated automatically. Compatibility requires native correctness, engagement, supported error assumptions and fallback evidence; failure or incomplete evidence does not enable the target. The original D1 protocol/results remain frozen. Darwin 27 [#516](https://github.com/the-sarge/quic-go-fast/issues/516) was the first application; its [qualification record](../audits/2026-09-22-darwin27-errors/results.md) establishes its arm64 admission rather than this decision alone.

The maintained policy incorporates that qualification's lessons: compare transient-error behavior with a qualified reference in the first round, permit verified target-binary evidence for precisely identified assumptions when source is unavailable, and check architecture exclusions against the shipped admission table. Evidence of a shared batch-count owner remains distinct from native UDP error induction or a proof of every lower-layer failure. This refines the evidence procedure without changing the retained runtime safeguards or requiring a new performance campaign.

Automatic admission based only on a successful startup probe would reduce release maintenance but leave error paths unqualified. Repeating the full performance campaign for each OS major would conflate compatibility with re-adopting an existing optimization. We accept delayed activation on new kernels in exchange for bounded compatibility evidence; changes within an admitted major can still require requalification when they invalidate that evidence.
