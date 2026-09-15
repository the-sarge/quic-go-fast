# HTTP/3 final-event lifetime contract (#345)

## Outcome and boundary

Protect admitted handler and stream event production independently of stream-map cleanup. rawConn remains the single owner of admission sealing, completion accounting, and recorder closure. Preserve #344 admission serialization, existing errors and I/O, stream-map and idle-timer semantics, response completion, and active-connection qlog event contents. An inactive retained stream does not hold a recording obligation. GOAWAY is already covered by #346; managed leases, public APIs, wire changes, public shutdown/handler ownership changes, generic logging frameworks, and performance campaigns are non-goals.

## Acceptance criteria

- [ ] Incomplete-header recording through the real request handler cannot overlap recorder closure or occur afterward; preserve the invalid-header event for admitted work.
- [ ] A returned stream's post-cancellation DATA write preserves its error without accessing a closed recorder; already admitted recording finishes safely before closure.
- [ ] Stream-map cleanup, request handling, response completion, and active-connection event contents remain intact. Cover the corresponding response-writer recording boundary when implementing the shared lifetime fix.
- [ ] Preserve serialized qlog producer admission during shutdown and existing late-work rejection; do not introduce an Add/Wait race, duplicate release, or shutdown dependency on unused retained streams.
- [ ] Start with the two semantic regression families above, using a strict recorder that detects both overlap and late events. Run focused regressions once and the HTTP/3 race suite once, alongside applicable existing repository checks. Retain existing event-content tests; expand or repeat only for a concrete uncovered case or diagnosed failure, with an explicit finite budget.

## Implementation boundary

Use a private scoped recording obligation governed by rawConn's existing mutex and wait group. An admitted handler owns a scope through parsing, ServeHTTP, and response completion. Stream operations borrow their own scope; operations inside a still-active handler may continue its already admitted work after sealing. A retained stream's pointer to a finished handler grants no admission. Protect that state under the existing admission mutex, with balanced release and no recorder mutation visible to concurrent stream operations.

Thread operation-local recorder permission through request/response headers, DATA, and trailer callbacks. Preserve underlying operations and their errors when logging admission is rejected. Keep stream tracking cleanup independent and do not change public ownership or Close waiting semantics. Explicitly stop if implementation requires such a change.

## Representation and artifact contract

Domain: existing HTTP/3 handler, HTTPStreamer.HTTPStream, ClientConn.OpenRequestStream, Stream/RequestStream operations, response-writer recording, and typed qlog events. Existing QUIC/QPACK/frame parsers own protocol representation. rawConn owns recording lifetime. Guarantee: universal lifetime enforcement within these entrypoints, supported by finite regression evidence rather than an exhaustive scheduling proof.

Runtime changes are shipped behavior. Admission and balanced completion are required safety enforcement. Tests and strict recorders are verification aids without a generic maintained-harness deliverable. Contract, review receipts, journal, and tracker pointers are process metadata.

## Finite semantic coverage

| Semantic class | Expected behavior at rawConn owner | Evidence |
| --- | --- | --- |
| Incomplete-header event after stream cleanup | Admitted handler retains final event; Close waits | TestQlogLifetimeIncompleteHeader (covered) |
| Returned server stream after cancellation and recorder closure | Original error, no recorder access, no retained idle obligation | TestQlogLifetimeReturnedStreamData/after_recorder_closure (covered) |
| Admitted stream operation records across shutdown | Event completes before Close | TestQlogLifetimeReturnedStreamData/during_admitted_recording (covered) |
| Handler response completion after cancellation | Preserve final response-writer event under handler obligation | TestQlogLifetimeResponseCompletion (covered) |
| Client-returned stream recording | Same operation lifetime and post-close rejection semantics | TestQlogLifetimeClientResponse (covered) |
| Active connection, existing early errors, and qlog disabled | Preserve contents, I/O, cleanup, and response behavior | Existing package/event-content/late-work tests (covered) |

Closure is triggered by the recorder lifecycle consequence and independently reachable producer/cleanup states. No parser syntax enumeration, guard-mutation campaign, additional platforms, or repeated stress runs. Focused regressions once, HTTP/3 package tests once, HTTP/3 race suite once, go vet ./http3/..., go mod tidy -diff, and configured lint. Expand only for a concrete uncovered in-contract case or diagnosed failure with a finite budget.

## Review and delivery

One initial RAS review, verification of independently accepted fixes, and at most one replacement review. Stop for an unresolvable accepted obligation, a repeated precise semantic root under the shared policy, representation mismatch, or expanded public ownership/API/blast radius. Context is bounded to this contract, its issue brief, changed HTTP/3 paths, and unresolved review evidence. Keep chronology behind PR links.

Certify the exact pushed head under docs/REVIEW-LOOP.md, require applicable existing hosted checks, mark ready, squash merge, append the journal without RAS, and complete the OmniFocus task after revalidating any deferred findings against the merged head.
