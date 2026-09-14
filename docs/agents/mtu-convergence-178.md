# MTU convergence investigation (#178)

The approved outcome is a bounded diagnosis and focused fixture correction for [issue #178](https://github.com/the-sarge/quic-go-fast/issues/178#issuecomment-5631301976), conditional on identifying the cause. The issue's acceptance criteria remain normative. If evidence identifies a production discovery or scheduling defect, present the concrete runtime change and blast radius for approval before implementation.

## Boundary and preservation

Capture echo completion, probe eligibility/emission, ACK/loss progress, discovered MTU, DATAGRAM publication and closure. Correct only the demonstrated fixture owner, with a controlled regression showing the old lifecycle fails and the corrected lifecycle succeeds. Preserve the 1375-byte minimum on the 1400-byte path, existing initial/final DATAGRAM relationships, disabled server discovery with 1234-byte server packets, at most one recorded client packet above the discovered MTU, and PR #145's close-before-final-sampling guarantee.

No arbitrary sleeps, increased deadlines, weakened thresholds, public completion API, maintained diagnostics framework, CI changes, unrelated timeout work or speculative transport changes. Temporary instrumentation is removed from the candidate. An observed terminal MTU result below tolerance must reach the existing numeric assertion; convergence-loop I/O errors retain the MTU progression in their diagnostics. Reconcile README current-status wording while preserving released changelog history. A production correction's blast radius remains untraced and requires a separate decision.

## Representation and artifacts

The guarantee is example-level regression coverage over the existing real UDP fixture and controlled probe states. The finder owns discovery state; connection ACK processing owns DATAGRAM publication; the fixture owns observation timing. Regression tests and temporary captures are verification aids, not universal protocol guarantees or a new maintained diagnostic dependency. This contract and receipts are traceability metadata. Shipped MTU/DATAGRAM behavior remains unchanged under the approved fixture branch. Contract closure is not triggered for this bounded fixture correction; a broader runtime obligation requires reassessment.

## Terminating evidence

Investigation budget: one full-suite Ubuntu/Go 1.27 replay with seed 1789072237978249249, plus at most two controlled experiments addressing specific observation gaps. Record platform limitations. Passing runs are not causal evidence. If the budget does not establish the cause, retain evidence, name the missing observation and propose a finite next capture; leave the defect open.

Correction evidence: controlled old/new regression, focused MTU discovery/DATAGRAM-limit/snapshot tests for QUIC v1 and v2, one focused race check, affected-package tests, vet, formatting, module tidiness, and applicable existing hosted checks including ordinary Ubuntu/Go 1.27 integration. Review budget: one initial RAS review, verification of independently accepted fixes, and at most one replacement review. Stop for contract expansion or the shared repeated-root rule. Follow the repository execution overlay for exact-head certification and guarded squash merge. Journal only after product merge, then complete OmniFocus task cqsnN4h09xQ and revalidate any deferred findings against the merged head.

## Implementation context

One conditional correction PR. Read this current contract, the linked issue brief, `integrationtests/self/mtu_test.go`, `integrationtests/self/mtu_snapshot_test.go`, relevant DATAGRAM tests, and the finder/ACK/emission seams implicated by the capture. Keep chronology, commands, candidate hashes, review dispositions and receipts in linked evidence rather than extending this contract with history.
