# GOAWAY qlog shutdown contract (#346)

## Outcome and boundary

The server's GOAWAY event participates in rawConn's existing admission and shutdown boundary. This is the first of two user-approved sequential fixes; stream/handler lifetime work belongs to #345 and is excluded here. Preserve event contents, GOAWAY wire handling, graceful shutdown, nil-qlog behavior, and #344's serialized admission invariant. No public APIs, dependencies, packet-I/O changes, or second lifecycle owner.

## Acceptance criteria

- [ ] The actual server GOAWAY recording path uses the existing connection-owned admission boundary and releases admitted recording work when the call finishes.
- [ ] A finite synchronized regression demonstrates that recorder closure waits while an admitted GOAWAY RecordEvent is blocked, then completes after that event returns.
- [ ] A finite synchronized regression demonstrates that a GOAWAY recording attempt after admission is sealed does not call RecordEvent or add late tracked work.
- [ ] GOAWAY event contents and ordinary GOAWAY wire/graceful-shutdown behavior remain unchanged; skipping a rejected logging attempt does not independently suppress the existing wire path. Preserve qlog-disabled operation.
- [ ] Run the focused regressions, existing relevant graceful-shutdown tests, and the HTTP/3 package tests; run focused race coverage for the changed synchronization. Diagnose failures rather than seeking a passing repetition.

## Representation, artifacts, and evidence

Domain: the existing server GOAWAY path and typed qlog.FrameCreated / qlog.GoAwayFrame values. rawConn owns producer admission, completion accounting, and recorder closure; existing protocol parsers retain representation ownership. The lifecycle guarantee is universal within that domain; tests provide finite evidence, not exhaustive scheduling proof.

The runtime change is shipped behavior; beginQlogWork and completion accounting are required safety enforcement. Tests/recorders are verification aids, not a separately maintained generic harness. This contract and receipts are process metadata. No verification-harness completeness obligation blocks delivery.

| Semantic class | Disposition and owner | Evidence |
| --- | --- | --- |
| GOAWAY admitted before sealing | rawConn waits until recording returns | TestServerGOAWAYRecorderShutdown through ServeQUICConn/Shutdown |
| GOAWAY attempted after sealing | rawConn rejects recording without adding work | TestServerGOAWAYRecorderAfterShutdown using the production recording path |
| Ordinary graceful shutdown / qlog disabled | Preserve event and wire behavior | Existing graceful-shutdown tests and event-content assertion |

Closure is triggered by the recorder lifecycle consequence and independently reachable admission/closure orderings. One regression per ordering; no guard mutations or repetition/platform campaign. Run focused regressions once, HTTP/3 package tests once, focused race coverage once, go vet ./http3/..., and go mod tidy -diff. Diagnose failures; extend evidence only for a concrete uncovered in-contract case.

## Review and delivery

One initial fully briefed RAS review; independently disposition findings; verify accepted fixes; at most one replacement review. Stop for an unresolvable in-contract obligation, repeated precise semantic root under the shared policy, representation mismatch, or expansion into public ownership/API changes. The implementation context is this contract, the issue brief, the changed HTTP/3 paths, and unresolved review findings; keep historical receipts in PR discussion.

Follow docs/REVIEW-LOOP.md: certify the exact pushed head locally, require applicable existing hosted PR checks, mark ready, squash merge that head, then append the journal without RAS and complete the OmniFocus task. The second product PR starts after this journal is merged.
