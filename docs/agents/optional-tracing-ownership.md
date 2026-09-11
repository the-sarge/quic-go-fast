# Optional tracing resource ownership

The normative acceptance criteria are from [issue #76](https://github.com/the-sarge/quic-go-fast/issues/76):

- [ ] Preserve rawConn identity between the server wrapper and the cancellation callback so recorder Close waits for the same producer group.
- [ ] Use a nonfatal policy for optional qlog directory creation failure, consistent with failure to open its log file.
- [ ] Make bufferedWriteCloser.Close always attempt underlying Close and document error precedence or joining.
- [ ] Add focused regressions for producer-before-recorder shutdown, directory failure without process exit, and a failing Flush that still closes the sink once.
- [ ] Keep this one PR centered on optional tracing resource ownership; exclude trace provenance and unrelated header allocation work.


## Boundary and evidence

The supported domain is the existing HTTP/3 server wrapper and its registered qlog producers, the default QUIC tracing factory (including its schema variant and HTTP/3 delegation), and the buffered sink adapter. Go contexts, synchronization primitives, filesystem APIs, and bufio own their representations. This change preserves the rawConn pointer captured by cancellation, treats optional directory setup errors like optional file setup errors, and joins flush and close errors after attempting both operations in order. Public API/module identity and packet-emission ownership remain unchanged.

Shipped behavior comprises the three ownership fixes. Tests are verification aids; this document is traceability metadata. Evidence is example-level: TestRawServerConnQloggerWaitsForProducer holds a real SETTINGS producer across cancellation, TestQLOGDIRCreationFailure isolates process-exit behavior in a subprocess, and TestBufferedWriteCloserClosesSinkAfterFlushError checks one underlying close attempt with either successful or failing sink closure. Existing successful directory setup, schema forwarding and buffer flushing tests provide positive controls. Contract closure is not triggered: these focused tests cover the three concrete failure mechanisms without an open state-space claim.

The terminating local plan is one red/green cycle per mechanism, affected-package tests, a focused race run, one uncached full local suite, go vet, go mod tidy -diff and diff formatting checks. Review is independent Standards and Spec review plus one fully briefed initial RAS review, verification of accepted fixes if any, and at most one fresh replacement review. No mutation campaign, random stress expansion, producer-admission redesign, recorder-idempotency guarantee, trace provenance, or unrelated allocation work is included.

This repository uses upstream-style push/pull_request workflows even for drafts, with no task preflight or ci.yml/ci-* gate. Require applicable existing hosted PR checks successful on the exact live head, then mark ready and squash merge with a pinned head. Do not change CI to introduce portfolio naming. Record review history and exact-head receipts separately from this contract. Journal and update the OmniFocus task after merge.
