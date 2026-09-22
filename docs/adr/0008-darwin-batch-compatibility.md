---
status: accepted
---

# Separate Darwin batch compatibility from performance adoption

Retain explicit kernel admission for the private `sendmsg_x` path because a startup delivery probe cannot establish every partial-send and error convention on which safe retry depends. Extend support through the bounded [Darwin batch compatibility policy](../darwin-batch-compatibility.md), with native evidence matching the architectures enabled by the runtime gate, while preserving the startup self-check and ordinary-send fallback.

For future kernel qualifications, this decision clarifies the scope of the [datapath offload plan](2026-09-11-datapath-offload-plan.md) and [Darwin batch plan](2026-09-11-darwin-batch-plan.md): the original statistical performance-adoption campaign is not repeated automatically. Compatibility requires native correctness, engagement, supported error assumptions and fallback evidence; failure or incomplete evidence does not enable the target. The original D1 protocol/results remain frozen. Darwin 27 [#516](https://github.com/the-sarge/quic-go-fast/issues/516) is the first application, not a declaration that Darwin 27 is qualified.

Automatic admission based only on a successful startup probe would reduce release maintenance but leave error paths unqualified. Repeating the full performance campaign for each OS major would conflate compatibility with re-adopting an existing optimization. We accept delayed activation on new kernels in exchange for bounded compatibility evidence; changes within an admitted major can still require requalification when they invalidate that evidence.
