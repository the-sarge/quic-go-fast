# DATAGRAM parser adoption contract

The owner authorized adoption of PR #16 after the completed traffic investigations, accepting the remaining tail-latency uncertainty in exchange for consistent resource savings. This supersedes the earlier experimental-only disposition for this PR; it does not change any historical measurement or the original 5% noninferiority result. At 4 Gbps the longer-window interval remains +4.04% [−0.20%, +8.45%], and the four-core diagnostic interval remains +2.08% [−1.38%, +5.09%]. Neither proves the original bound. Receiver allocation fell about 47%; CPU savings were smaller. No throughput or tail-latency improvement is promised by this parser change.

## Accepted preservation criteria

The following criteria are copied verbatim from the original ownership audit:

Preserve length-present and length-absent parsing, zero-length records, exact consumption and malformed/truncated errors; retain negotiation/encryption-level checks. The owning queue copy, FIFO, capacity 128, drop-new, notifications, close/cancellation precedence and independent application buffers must remain unchanged. No pool, queue representation change, send-side change, new dependency or public API is included.

Add a parser regression for bounded borrowing with present/absent length, empty data and trailing frames. Add connection-level characterization that poisons/reuses the packet bytes after parsing and retains/mutates returned datagrams, with qlog retention on and off. Add a connection overflow case proving a prefilled queue remains ordered after a parsed packet is discarded and reused. Reuse existing queue ownership, concurrency, cancellation, closure, malformed parser and real-QUIC datagram tests. Run the new tests before the candidate; the borrowing assertion is expected to fail, while public ownership characterization must already pass.

## Boundary and guarantee

The existing internal wire parser continues to own syntax and validation for every DATAGRAM frame it accepts. This patch changes only payload ownership: Data borrows a length-bounded, capacity-bounded input slice during synchronous packet processing. The queue remains the sole owning-copy boundary before admitted payloads can escape to applications. The guarantee applies to all supported DATAGRAM receives, including qlog enabled or disabled, empty records, rejected overflow and directly constructed frames. No new input grammar or public API is introduced.

The two parser files are shipped behavior; the unchanged queue copy enforces lifetime safety. Ownership and parser tests are verification aids, and the aggregate benchmark files are historical evidence and traceability. Private traffic-generator source, raw traces and harness internals remain outside this public repository. Existing focused tests cover the semantic lifetime cases; no review finding has yet triggered additional contract closure.

## Terminating adoption evidence

Run one fully briefed independent RAS review, independently disposition its findings, and permit at most one replacement fresh review after accepted code fixes and exact-head verification. Review is read-only; the implementing agent owns any fixes. A material ownership-boundary redesign returns to the owner rather than expanding this PR.

After review, certify the exact pushed head with the repository's ordinary package tests on macOS Go 1.27.0 and Linux Go 1.27.1, focused root/wire/real-QUIC DATAGRAM race tests on both, Linux full package race tests, real-QUIC integration suites for v1 and v2, and static analysis. Run one 20-second, four-worker parser fuzz check. Earlier baseline characterization and the completed finite benchmark matrices remain evidence; no new performance runs or larger platform cross-product are budgeted. Investigate failures before rerunning tests.

Use the inherited repository workflows for hosted verification; this PR does not migrate CI to the portfolio standard. Keep the PR draft until local certification succeeds, then confirm actual hosted checks on the same live head before an exact-head squash merge. Any unavailable hosted evidence must be reported rather than represented as passing. Preserve the initial checkout and all unrelated worktrees.

D1/D2 and their issue and OmniFocus tracking remain completed and are not reopened. Record the merge in the development journal only after the product merge.
