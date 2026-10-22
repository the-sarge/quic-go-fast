# Real-UDP proxy switch admission (#978)

The normative acceptance criteria are the [accepted issue #978 agent brief](https://github.com/the-sarge/quic-go-fast/issues/978#issuecomment-6333727344). This document records the approved one-PR repair boundary. The existing [proxy shutdown and ownership contract](proxy-shutdown-396.md) remains authoritative.

## Admission and representation

A successful outgoing socket read must pass packet admission ordered with socket switching before entering drop, delay, or forwarding processing. `connection` owns both decisions under its existing mutex. A read from a socket retired before admission is discarded. A packet admitted first may finish after switching, including delayed delivery and a packet whose callback calls `SwitchConn`. Switching neither waits for nor flushes admitted work. The admission mutex is released before callbacks and socket reads or writes.

Read observations describe completed socket operations, independently of forwarding eligibility. They remain before admission, so a discarded successful read is still observed. Existing deadline-based reader interruption and error handling remain in place.

The supported representation is real `net.UDPConn` identity during the proxy's supported single-start lifetime, switching among distinct sockets with callbacks satisfying their existing completion obligations. The guarantee applies universally within that domain; finite regressions demonstrate the relevant ordering classes rather than proving every schedule. Socket reactivation, deadline restoration, restart, general error reporting (#425), production QUIC/API/wire changes, and changes to shutdown ownership are outside this repair.

The private admission method uses the existing state owner and mutex, adding one short lock acquisition per successful outgoing read. It adds no worker, queue, mirrored generation state, public API, or dependency. Generation tracking would add unnecessary state for this domain; reconsider it only if socket reactivation receives its own approved contract. Draining admitted work or holding admission synchronization across callbacks conflicts with the accepted contract and is rejected on merit.

## Maintained verification boundary

The proxy fixture and its regressions are maintained verification aids, owned by `integrationtests/tools/proxy`. Their operational payoff is reliable socket switching for real-UDP integration fixtures. The approved plan makes these regressions required for this fixture repair; retain them until the behavior is removed or equivalent coverage replaces them. This contract and certification receipts are process metadata. No production transport behavior is changed.

Full contract closure is not triggered: the single state owner and the following focused cases cover the relevant semantic distinctions without a larger path or platform matrix.

| Case | Required result | Regression |
| --- | --- | --- |
| Successful old-socket read held in its observer across retirement | Preserve the real read observation, reject its payload before drop/delay/forward processing, and deliver replacement traffic | `TestProxySwitchDiscardsRetiredRead` |
| An outgoing drop or delay callback switches its admitted packet | Switching completes and the admitted packet and replacement traffic reach the client | `TestProxySwitchFromOutgoingCallback` |
| Delayed handoff completed before switching while its consumer is held | Switching returns before the held work is released; the queued packet subsequently reaches the client | `TestProxySwitchPreservesQueuedOutgoingPacket` |
| Existing switch, observation, shutdown, and ownership behavior | Preserve the original assertions, including the retired-payload assertion | Existing proxy package tests |

The new tests exercise real UDP and the real outgoing reader through public proxy operations and existing socket observers. Channels establish ordering; deadlines and watchdogs bound missing completion. Cleanup releases held callbacks before joining the proxy. The temporary toolchain overlay from triage is not a maintained dependency.

## Terminating evidence and review

Retain one failing old-behavior result and one passing repaired result for the retired-read regression. Run the affected proxy package once normally and once with the race detector, and existing NAT-rebinding and directly affected network-admission coverage once, with explicit timeouts. Run `go vet ./integrationtests/tools/proxy`, `go mod tidy -diff`, and diff/format checks. Failures require diagnosis before reruns. No additional mutation, repetition, stress, or platform campaign is required; existing hosted workflows remain applicable.

Review is bounded to one fully briefed initial RAS review and at most one replacement after independently accepted fixes, with targeted verification of accepted findings. The current contract, the linked acceptance criteria and shutdown invariants, and the three-file implementation diff form the bounded review context. Review history and exact-head test/certification receipts belong in the PR and linked RAS artifacts. A required scope expansion, representation mismatch, or repeated semantic counterexample at the same precise invariant and enforcement owner stops implementation for a decision under the shared review policy.

Follow the [repository execution overlay](../REVIEW-LOOP.md): certify the exact pushed candidate, require applicable existing hosted PR checks to succeed on that head, mark ready, and squash merge with a matching head. Append the dev journal only after the implementation reaches remote main, then reconcile issue and task pointers.
