# Strong candidates under scrutiny

Four bounded work items survive deeper scrutiny. Three architectural refactors should wait. The strongest evidence supports repairing ownership and lifecycle contracts; it does not support adding larger modules merely to reorganize the same behavior.

Source: `a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf`. RAS run: `20260908T140201-379bc9b4a91f2859f270b80e`. Assessment only; implementation has not started.

## Recommended sequence

1. **Correct the full HTTP/3 request/response lifecycle.** Premature idle closure can interrupt an active response. Address the full lifecycle together; this is broader than two local edits, but still has a narrower ownership surface than ingress.

2. **Repair received-buffer ownership.** The broadest confirmed correctness concern. Establish the full ownership contract first, then use independently reviewable transfer and connection-lifetime slices.

3. **Shrink the published dependency archive.** A measured 95% archive reduction with no intended runtime change. Preserve access to the historical receipts.

4. **Remove parallel send orchestration from connection tests.** Strengthen the evidence for future emission changes without changing production behavior.

The four items are independent except that future emission construction/readiness work should follow the test-entrypoint cleanup. Packaging can run alongside correctness work. The following cards preserve original synthesis order.

## 1. Receive-buffer ownership

**KEEP · Narrow the design.** C-002 · C-003 · C-019. Sequence 2 · correctness.

Repair the rules for handing received packet storage from one part of the library to another. Keep the current execution model and use small, explicit ownership contracts. A new receive engine is unnecessary.

### Is there a demonstrated problem?

Yes. Disposable tests against unchanged production code found five terminal drop paths that leave the buffer reference count at one: connection overflow, server overflow, empty input, a routing parse rejection, and disabled non-QUIC reception. A sixth check found that a full deferred-decryption queue reports that it retained a packet when it did not. These establish bookkeeping failures, not a measured throughput regression or permanent memory leak.

### What should the central rule be?

Every receive handoff must clearly transfer responsibility. A handler either retains the packet for later work or finishes with its storage. A queue must report actual admission, not an attempted admission. Protocol state remains on the connection goroutine; socket reading and existing transport/server boundaries remain intact.

### Why not just add a release to every return?

Several QUIC packets can share one received UDP datagram. The existing counter can reach zero between siblings while the outer parser still needs the bytes; blindly returning storage to the pool at that point could corrupt later parsing. Release() also assumes the last reference, while MaybeRelease() does not decrement a reference. Each terminal path needs the right operation at the right lifetime boundary.

### How broad must the review be?

Cover raw-read errors, routing and queue rejection, non-QUIC delivery, coalesced parsing, deferred decryption, replay, handshake completion, and shutdown. Code inspection also identified closed-connection handlers, discarded 0-RTT queues, and queued response work as cases that must be accounted for. These additional cases are inspection findings requiring individual regressions; they were not all reproduced during this review.

### What is the most easily missed shutdown rule?

Stop further admission before draining retained packets. Replacing a routing-map entry cannot stop a caller that already holds the old handler pointer. Also account for the unvisited suffix of a replay queue already moved into a local variable. Closing a listener must preserve established connections, and different packet views sharing one buffer may each own a reference.

### Which design wins?

Three independent design sketches compared minimal contract repairs, a common receive module, and a more flexible explicit-lifetime abstraction. The minimal contract design wins: keep concrete components and introduce only private helpers whose responsibility is unambiguous. A global lifetime owner, new goroutines, atomic refcounts, or a generic lease framework would enlarge the hot-path change without demonstrated need.

### What would make the work complete?

First establish an ownership table and regression instrumentation that detects both missed returns and premature/double pool returns. Then fix terminal transfer paths, followed by connection-local coalesced/deferred/shutdown cases. Exercise ordinary and optimized socket readers, malformed and coalesced input, overflow, replay failure, cancellation and shutdown races. Keep the existing public API and allocation-sensitive fast path; use bounded existing performance checks for changed hot paths. Do not claim a speed gain without measurements.

### What decision record is needed?

A separate inbound-lifetime decision should define the transfer contract and admission/closure rules. Receive ownership was excluded from the completed emission program; that exclusion is a scope boundary for that program, not a permanent prohibition. Preserve its historical receipts and accepted uncertainty.

**Review focus:** Highest review risk of the retained work: an incorrect lifetime repair can return shared bytes too early. Split by ownership boundary, not by arbitrary file counts.

Evidence: [buffer_pool.go:21](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/buffer_pool.go#L21), [transport.go:564](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/transport.go#L564), [server.go:395](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/server.go#L395), [connection.go:1054](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L1054), [connection.go:1391](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L1391), [connection.go:1950](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L1950), [connection.go:2654](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L2654), [sys_conn.go:117](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/sys_conn.go#L117), [closed_conn.go:34](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/closed_conn.go#L34).

## 2. Published module payload

**KEEP · Measured benefit.** C-015. Sequence 3 · distribution cost.

Exclude the audit subtree from the Go dependency archive while keeping all historical evidence in Git. Use a nested module boundary and repair the links that must remain useful to library consumers.

### How much does this actually save?

A local archive generated from the pinned Git commit with Go’s module-zip library is 22,410,202 bytes. A projection using a nested docs/audits/go.mod boundary is 1,118,190 bytes: 21,292,012 bytes saved, or 95.01%. The audit tree contains 393 files and 22,754,924 uncompressed bytes. This is an archive measurement, not a published-release download benchmark.

### Should the evidence move to an external storage service?

No. A nested module marker provides the exclusion without relocating historical files, provisioning storage, or changing their Git history. Go’s documented archive rules exclude nested module subtrees. There are no tracked Go source files under this audit tree and the source scan found no go:embed directive consuming it.

### What is the catch?

All 42 Markdown receipts under the subtree disappear from the main module archive too. Shipped ADRs currently link to those receipts using relative paths. Convert those references to durable, commit-pinned repository links, or provide a small shipped evidence index with those links. Keep the actual decision documents and glossary in the parent module.

### Does this make QUIC faster?

No runtime or executable-size improvement is established. The benefit is smaller dependency downloads and module caches. Git clones still contain the historical evidence. Consumers already using a warm module cache will see little immediate benefit.

### What must be checked before shipping?

Generate the real archive after the marker and link changes, compare its complete file list and compressed size, verify all remaining source, licenses and decision docs, and test a consumer using the documented replace-and-pin mechanism. Check repository tooling that enumerates nested modules. Do not publish the diagnostic archive or give the audit marker a new library dependency role.

### Is a new architecture program needed?

No. Record a small packaging convention explaining that audit evidence stays in Git and is excluded from the dependency payload. Preserve exact base/candidate provenance; do not rewrite old evidence or invent a new retention service.

**Review focus:** Low runtime risk. The important risks are broken evidence links and tooling that treats every nested module as a build target.

Evidence: [go.mod:1](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/go.mod#L1), [docs/adr/0002-adopt-through-module-replacement.md:1](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/docs/adr/0002-adopt-through-module-replacement.md#L1), [docs/adr/0004-packet-emission-ownership.md:15](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/docs/adr/0004-packet-emission-ownership.md#L15), [docs/adr/2026-09-07-packet-emission-plan.md:67](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/docs/adr/2026-09-07-packet-emission-plan.md#L67).

## 3. HTTP/3 shared-connection lifecycle

**KEEP · Full request/response lifetime.** C-018. Sequence 1 · correctness.

Keep a shared connection in use from acquisition through the active response lifetime. Release exactly once on terminal completion, cancellation, or closure, and let failures evict only the exact cached entry they used. Returning response headers is not completion.

### What failures were reproduced?

The original diagnostics showed a canceled dial waiter retaining its usage count and an old failed request evicting a replacement entry. A third diagnostic now returns headers with an unfinished response body through the real transport code: useCount is already zero, and CloseIdleConnections closes the real QUIC connection. The HTTP exchange and body are controlled fixtures, not a complete network streaming test. These demonstrations establish three failures; they do not certify a complete correction.

### What was wrong with the initial recommendation?

It treated the lifetime of the RoundTrip function call as the lifetime of the request. Successful RoundTrip returns after response headers, while the application may still be reading the body. Balancing acquisition solely with a function-exit defer would fix early-exit leaks but preserve premature idle eligibility for successful requests. The original two-fix completion claim is withdrawn.

### What is the complete ownership contract?

Each acquisition creates one usage obligation. Before response delivery, cancellation or an error must finish that obligation exactly once. Successful delivery transfers responsibility to the active response lifetime without decrementing at the return of headers. Retain usage while the response is active; finish it exactly once on terminal body completion or error, explicit closure, or cancellation that terminates the exchange. Eviction separately checks hostname and expected entry identity under the existing lock.

### How should competing completion events behave?

EOF, a terminal read failure, Body.Close, request cancellation, and connection teardown can overlap. They must converge on one idempotent completion path, so the count cannot go negative or be decremented twice. Cancellation must finish the exchange even if the application never performs another body read. Remove any cancellation callback or observer when its job ends; do not add a persistent goroutine or leave observers retained after completion. An unread, unclosed response on a live context is still active, not idle.

### Can the existing body-completion machinery be reused?

Prefer integrating with the existing request/body lifecycle over building a parallel pool framework, but do not equate any single existing signal with the complete contract. ClientConn already keeps cancellation handling alive after returning headers; hijackableBody signals read completion or Close with sync.Once. Request cancellation follows a separate path. Compressed bodies add another layer that can report completion or failure. Explicitly reconcile those events, bodyless responses, and early responses while request-body uploads are still active; any still-active request-side work must be accounted for or terminated before the connection becomes idle. These are required design and regression cases, not additional reproduced failures.

### Should we extract a new pool module or restructure shutdown?

No new pool abstraction is required by the demonstrated failures. The focused scope now crosses transport acquisition, request cancellation, and response completion, so it is broader than two local edits. Small private lifecycle plumbing is appropriate. Keep moving shutdown waits outside the mutex deferred: this review did not establish a shutdown deadlock or unacceptable delay, and changing that ordering needs a separate justification.

### Which existing behavior must survive?

Preserve shared dialing, the initiating request’s dial context, cancellation causes, tracing, OnlyCachedConn, idle closure, and current retry/body-replay rules. Every retry attempt has its own usage obligation. Keep public response-body behavior, error propagation, decompression, and direct NewClientConn behavior intact. CloseIdleConnections must leave active responses usable; explicit Transport.Close retains its documented shutdown role.

### Must upstream accept the fixes first?

No. ADR 0003 explicitly permits independently reviewable fork fixes without upstream acceptance. The inspected transport file matches stable v0.62.0 and the upstream HEAD file observed during this review. Keep this complete lifecycle correction easy to compare and retire when upstream provides an equivalent remedy. No upstream message or issue was sent.

### What proves completion?

Retain the cancellation-count and stale-eviction regressions, and add a real streaming HTTP/3 test proving that idle cleanup between headers and body completion preserves the connection and readable body. Cover balanced early exits; body EOF and terminal errors; explicit Close; cancellation before and after headers, including without another Read; repeated and concurrent termination; compressed and bodyless responses; multiple active responses on one connection; retry attempts; early responses with ongoing uploads; and current-versus-stale eviction. Show that one finished response does not make other active responses idle, and that after all usages terminate the connection becomes eligible for idle cleanup. Run focused HTTP/3 and race checks. The three diagnostics alone are not the acceptance suite.

**Review focus:** Concurrency and lifetime risk spans acquisition through response completion. The termination signals must agree without double release, premature idle closure, or retained observers. A two-line defer adjustment is insufficient.

Evidence: [http3/transport.go:216](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/transport.go#L216), [http3/transport.go:294](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/transport.go#L294), [http3/transport.go:420](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/transport.go#L420), [http3/transport.go:475](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/transport.go#L475), [http3/transport.go:530](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/transport.go#L530), [docs/adr/0003-follow-stable-upstream-releases.md:7](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/docs/adr/0003-follow-stable-upstream-releases.md#L7), [http3/client.go:306](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/client.go#L306), [http3/body.go:97](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/body.go#L97), [http3/stream.go:407](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/stream.go#L407), [http3/gzip_reader.go:23](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/http3/gzip_reader.go#L23).

## 4. Path changes and packet-size policy

**DEFER · No demonstrated defect.** C-011 · C-017. No standalone work scheduled.

Keep path policy with the connection. Do not introduce a coordinator or collapse the several packet-size facts into one value. Revisit a small helper when an actual path-policy change needs it.

### Does the different client/server ordering prove a bug?

No. Client path replacement and server address rebinding perform related steps in different orders, but both run on the connection goroutine. Generation-tagged feedback is not consumed halfway through those synchronous sequences. The ordering difference alone does not establish a stale-feedback window.

### Why are there several packet-size values?

They serve different purposes: the conservative initial size, discovered path size, peer/local limits, an atomic estimate read by application goroutines, and congestion accounting. The handshake fallback intentionally replaces discovery state without starting probes. Treating these as duplicate copies of one value would erase real distinctions.

### What duplication is real?

The peer/local payload-bound calculation appears in three places, and client/server transitions repeat some recovery and MTU-reset steps. A small connection-owned calculation or reset helper could reduce duplication. That benefit is limited and does not require a new architectural owner.

### What would a future change have to preserve?

Keep client queue replacement distinct from server in-place rebinding; preserve generation advancement, stale/current feedback handling, peer bounds, no pre-confirmation discovery, safe application reads, and congestion floors. Do not move these decisions into packet emission.

### What would make this worth reopening?

A reproduced transition defect, a new supported transition that materially duplicates the protocol, or repeated maintenance errors at these sites. In that situation, characterize client and server behavior first and prefer one small helper. Any broader policy reorganization needs its own decision outside the closed emission program.

**Review focus:** A broad unification can change handshake probing, migration ordering, or application-visible packet limits without a corresponding demonstrated benefit.

Evidence: [connection.go:920](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L920), [connection.go:1287](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L1287), [connection.go:2444](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L2444), [connection.go:2457](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L2457), [connection.go:2558](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L2558), [docs/adr/0004-packet-emission-ownership.md:9](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/docs/adr/0004-packet-emission-ownership.md#L9).

## 5. Packet-emission construction and binding

**DEFER · Useful only alongside related work.** C-007 · C-021 · C-028. No standalone work scheduled.

The construction sequence could be clearer, but it is not demonstrably unsafe today. Preserve the current recovery binding and connection-owned path identity. Consider a small constructor helper only when adjacent work makes it useful.

### Can the send worker observe the partially built object?

The queue is assigned during setup, the packer and policy bindings are completed in the constructors, and the worker starts later in Conn.run. Inspection did not find a runtime observer of the intermediate state. The current sequence is awkward to read; that is weaker evidence than an initialization defect.

### Would a complete constructor improve the code?

Possibly. It could remove the self-reading bind step and make the concrete dependency set explicit. However, client/server crypto setup differs, and direct packet-packer unit fixtures legitimately have their own construction. Do not replace these distinctions with a generic dependency bag or pass all of Conn through an opaque factory merely to shorten call sites.

### Can the recovery pointer become a captured value?

Not safely as an incidental cleanup. Multiple fixtures replace sentPacketHandler after binding and rely on the pointer observing that replacement while real packet registration remains active. A value capture would silently change those tests. Keep that binding unless a separately tested replacement contract is justified.

### Are three references three competing path owners?

No. Conn, packet emission, and the send worker hold coordinated references with different responsibilities. An alias write during a connection-commanded transition does not establish competing policy ownership. Do not move the whole send-path identity into emission.

### What is the recommendation after considering cost?

Defer the standalone refactor. After the test-entrypoint cleanup, a future production change may justify a small complete-construction helper. Require deletion of real setup duplication with no broader interface or lifetime change. Constructor completeness and path-policy redesign should not be bundled together.

**Review focus:** The most attractive-looking simplifications can invalidate load-bearing test bindings or blur the connection’s path authority.

Evidence: [connection.go:372](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L372), [connection.go:499](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L499), [connection.go:515](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L515), [connection.go:593](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L593), [connection.go:2502](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L2502), [packet_emission.go:527](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/packet_emission.go#L527), [connection_emission_test.go:23](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection_emission_test.go#L23).

## 6. Applying send outcomes and readiness

**DEFER · Preserve context-sensitive behavior.** C-020 · outcome portion of C-007. No standalone work scheduled.

Do not redesign the result type or automatically derive blocking from a stop reason. A small application helper may be useful during a future scheduling change, after tests explicitly cover the context it must preserve.

### Is the returned capacity channel dead code?

No. The run loop uses result.available as the next capacity wakeup. The nil-restating else branch is redundant, and some state assignments repeat normalization, but the capacity result itself is live. Removing it would lose behavior.

### Can each stop reason map to one blocked state?

Not by itself. Entering a send opportunity already hard-blocked sets the connection’s blocked state. Sending a real packet and then discovering the same recovery limit can return the same stop category while preserving a different blocked projection. Inspection of the stable upstream loops shows the same contextual distinction. A tidy lookup table could change behavior.

### Should pre-send and post-send capacity handling merge?

They have different jobs. The pre-send capacity gate prevents destructive packing and preserves the required ordering before handshake feedback. The final result accounts for capacity after emitted batches. Both must remain represented, even if the connection eventually uses one helper to apply their effects.

### Is a sealed outcome algebra warranted?

No evidence here warrants changing the hot-path result representation. Preserve progress, retry, deadlines, errors, and wakeups as distinct facts. One carefully scoped application helper could remove repeated assignments, but a few duplicated lines are insufficient justification for a standalone scheduling refactor.

### What would permit a future change?

First cover already-blocked versus progress-then-blocked cases, final-slot occupancy, worker wakeups, pending receives, paced sends, probe retries, fatal partial construction, and close handling through production entrypoints. Then require a concrete scheduling defect or meaningful deletion of duplicated policy. Do not reopen the completed performance campaign or relabel its uncertainty.

**Review focus:** Scheduling behavior depends on what happened before the stop, not only why sending stopped. Simplifying the shape without retaining that context is risky.

Evidence: [packet_emission.go:60](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/packet_emission.go#L60), [packet_emission.go:89](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/packet_emission.go#L89), [packet_emission.go:100](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/packet_emission.go#L100), [connection.go:721](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L721), [connection.go:2470](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection.go#L2470), [packet_emission_test.go:61](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/packet_emission_test.go#L61).

## 7. Connection tests through the shipped send entrypoint

**KEEP · Focused test cleanup.** C-001 · C-010. Sequence 4 · confidence in future changes.

Migrate the remaining connection-level ownership scenarios away from test-only Conn send orchestration. Keep legitimate low-level packet-emission tests and preserve the meaning of historical experiment fixtures.

### Is the production send path currently untested?

No. Existing tests cover triggerSending, full-queue execution through Conn.run, handshake fallback, hard blocking, and probe timeout behavior. The focused existing emission/handshake test selection passed in this review. The gap is narrower: several ordinary/GSO ownership scenarios use a test-only composition that bypasses production dispatch and part of the state application.

### Why do those few cases still matter?

The test-only Conn.sendPackets/emitPackets helpers compose finish(sendAny(...)) themselves. They can keep passing when the real send entrypoint changes. Tests claiming connection-level behavior should exercise triggerSending or the connection loop and observe the resulting packets, ownership, and scheduling state.

### Should every test use the full connection loop?

No. Use the narrowest production boundary that covers the claim. Low-level packing, buffer, and batching tests can call packet-emission internals directly. Use the loop where worker capacity and wakeup behavior are part of the assertion. Avoid a large simulated connection framework.

### How should controlled behavior work?

Retain the real packer, frame producers, recovery registration, and send queue. Use narrow policy wrappers and controlled protection where already appropriate. When migrating a test from the bypass to dispatch, adjust the fixture’s allowed send modes so it still reaches the intended failure; do not simply delete assertions that stop passing.

### What about the tagged allocation experiments?

The emission_experiment fixtures also use the historical helper. Inventory those callers before removal. Keep any historical composition explicitly confined to the experiment fixture or migrate it with clear semantics, compile the affected tag, and preserve old receipts unchanged. Moving an entrypoint does not make old base/candidate measurements directly comparable and does not authorize a new campaign.

### What counts as completion?

The migrated tests still prove receive fairness, decoded ordinary/GSO output, empty and partial-fatal ownership, and registration before asynchronous I/O. Production composition is no longer reimplemented by the connection-level test helper. Existing composed-path tests remain. This is a test-maintenance change with no intended production behavior change.

**Review focus:** A careless migration can weaken the test or change the experiment being measured. Preserve assertions and historical interpretation, not just green test output.

Evidence: [connection_emission_test.go:38](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection_emission_test.go#L38), [connection_emission_test.go:66](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection_emission_test.go#L66), [connection_emission_test.go:207](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection_emission_test.go#L207), [connection_emission_test.go:264](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection_emission_test.go#L264), [packet_emission_test.go:100](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/packet_emission_test.go#L100), [connection_emission_experiment_test.go:1](https://github.com/the-sarge/quic-go-fast/blob/a5bb7f9145755a6c40482eda8cdde4c9a68b4dbf/connection_emission_experiment_test.go#L1).

## Validation limits

Diagnostic failures establish specific baseline behavior; they do not measure production frequency, throughput, or permanent leakage. Existing focused emission/handshake tests pass. Packaging uses a local Go module archive projection, not a published download. See the companion HTML evidence section, diagnostic sources, logs, and archive results for method and limitations.

The Go archive exclusion rule is documented in the [official module reference](https://go.dev/ref/mod#zip-files). No source implementation, commit, push, issue, formal ADR update, or new performance campaign was performed.
