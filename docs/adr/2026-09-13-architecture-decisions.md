# Architecture decisions requiring no implementation — A13

**Status:** Accepted current dispositions; no dispatchable slices
**Normative scope:** Rejected/deferred directions and guardrails for the 2026-09-13 program
**Audit history:** [Grilling and existing-work evidence](../audits/2026-09-13-architecture-handoff/README.md)

These outcomes do not authorize new tasks or opportunistic edits. “When touched” means a later authorized change may include a locally necessary clarification; it is not backlog work for this program. The seven implementation plans own their accepted changes and more specific rejected alternatives. Preserve ADRs 0001–0005. Reopen a deferred design only with concrete new change pressure and a bounded decision.

## N01 — Standalone HEADERS consolidation

Defer. Existing parseHeaders semantics are already shared; encoded-size outcomes belong to request/response roles (request 431/ExcessiveLoad, response FrameError). Request and response logs preserve partial decoded fields before semantic failure; trailer semantic failure logs an invalid-HEADERS event without parsed fields, while truncated input returns before logging. A generic parser/logger/error-mapper callback bundle moves coordination into a shallow interface. Keep allocation behavior and TestHeaderCollectionAllocations. Reconsider concrete role functions only after repeated real changes.

**Source boundary:** `http3/headers.go:55–75; http3/headers.go:446–472`.

## N02 — Standalone routing registry

Defer. packetHandlerMap is a defined type, not an alias. Transport must coordinate dial closed-check/construction/publication, empty-map server policy, unlocking around destruction and expiration/listener stop. A held-lock helper extraction exposes that coordination; moving all policy recreates Transport. Keep stable Transport ConnRunner identity: callbacks from different connections remain distinct; A→B→A identity matters. Do not add another mutex, promise deletion of three callbacks, or assert contention gains. Reconsider on concrete routing changes.

**Source boundary:** `transport.go:294–326,496–534,800,886–895; conn_id_generator.go:199–210`.

## N03 — wrappedConn and server construction seam

Defer. Production connection hooks support seven test overrides; no production nil-hook defect was established. Concrete public Conn return and abortUnstarted remain. Do not bundle this root-QUIC testability question with HTTP/3 admission.

**Source boundary:** `connection.go:243; conn_wrapped_test.go`.

## N04 — Sender constructor shape

Optional only when touching the worker. A concrete private return may aid ergonomics; preserve the production sender interface and live MockSender. Close-before-Run semantics are separate. Captured queued path facts must not be replaced by a live emission-policy lookup.

**Source boundary:** `send_queue.go; packet_emission.go`.

## N05 — HTTP/3 parser disposition C024

Defer. Reserved frames close with FrameUnexpected before returning the ordinary parser error; the first close wins in the real test. Do not claim wrong wire codes from later caller classification alone. Reconsider in a specific framing behavior change, not a generic connection driver.

**Source boundary:** `http3/frames.go:118–127; http3/frames_test.go:34`.

## N06 — Receive log assembly C008

Defer standalone. Local typed construction may be useful during an actual schema change. Preserve eager checksums before in-place packet unpacking, retained/replay identity, and logging ownership; no drop-only fatal owner.

**Source boundary:** `packet_unpacker.go:137,156; connection_logging.go`.

## N07 — Initial and constructor consolidation C025

Defer broad pure/admission merging. Time, routing and user callbacks are effects. Named struct fields improve readability without proving type safety. Preserve completed I8 publication, abort and 0-RTT transfer contracts.

**Source boundary:** `connection.go; server.go; docs/adr/2026-09-08-emission-construction-plan.md`.

## N08 — 0-RTT store C001

Keep the existing small owner and its capacity, expiration and retirement responsibilities. No replacement module or new implementation task.

**Source boundary:** `server.go:38`.

## N09 — Retention helpers C004/C017

Keep retainForRetentionQueue as the shared copy/charge transfer seam. Queue-specific owners retain capacity locks, budgets and disposal. Another thin common wrapper would not remove those decisions.

**Source boundary:** `coalesced_slab.go:115; docs/adr/2026-09-08-incoming-lifetime-plan.md`.

## N10 — Flow-control lock boundaries

Document locally only when touched. Send mutation is under SendStream.mutex; receive paths combine their outer mutex with controller internal synchronization. Notifications discover/wake work. No new controller lock or pull scheduler is justified. Nested mutex names alone do not establish a deadlock.

**Source boundary:** `send_stream.go; receive_stream.go; internal/flowcontrol`.

## N11 — Pool observation fixtures

Keep serial fixture ownership and restoration. A bounded test-local helper is optional for actual reuse. Do not create a production pool owner or replace capacity validation with tags; no global instrumentation framework is authorized.

**Source boundary:** `internal/wire/pool.go; buffer_pool_test.go`.

## N12 — Offload switches C005/C011/C020

Keep current equivalent ParseBool semantics and evaluation timing, including Darwin once-only capture. Constants or local documentation may be improved when touched. No shared engine/configuration lifecycle.

**Source boundary:** `sys_conn_oob.go; sys_conn_windows.go; sys_conn_sendmsg_x_qual_darwin.go`.

## N13 — Emission policy hooks

Keep the 13 synchronous mutating hooks; local naming/order documentation only. No observer, cache or atomic holder. Feedback consumes queued facts rather than current handshake/path state.

**Source boundary:** `packet_emission.go; packet_emission.go`.

## N14 — HTTP/3 forwarding wrappers

Keep the live MockDatagramStream, public QUICStream method promotion, RequestStream guards/method set and requestLifetime adapter. No deletion by forwarding count and no negotiation change.

**Source boundary:** `http3/state_tracking_stream.go; http3/stream.go`.

## N15 — Crypto manager

Keep level dispatch and Finish validity ownership. Constructor cleanup does not require moving this owner. R2 must preserve nil sorter callbacks from crypto streams.

**Source boundary:** `crypto_stream.go`.

## N16 — Pending send storage

Keep within SendStream. Recent accepted coverage protects capacity reservation/prefix behavior; separate extraction would create a shallow interface.

**Source boundary:** `send_stream.go; docs/agents/large-accepted-stream-writes.md`.

## N17 — Deadlines C006/C018

Keep the single maybeResetTimer owner and existing readiness/deadline semantics. Do not reopen completed timer or readiness work.

**Source boundary:** `connection.go:872`.

## N18 — Typed Darwin capability C013/C021

Local typed cleanup only if a concrete change needs it. Structural support differs from live availability; preserve custom-adapter/build test seams. Type-assertion cost is unmeasured.

**Source boundary:** `sys_conn_sendmsg_x_qual_darwin.go`.

## N19 — Unified backend C016

Reject flattening socket, destination and process capability scopes into one backend state machine. Current separate owners encode materially different lifetimes.

**Source boundary:** `docs/adr/2026-09-11-datapath-offload-plan.md`.

## N20 — Closed programs and evidence

Preserve closed path, MTU, readiness, emission and receive work; D2 remains retired. No new private syscall, offload qualification, two-core adoption requirement or performance campaign. Preserve frozen experiments, manifests and generator provenance. The new program does not resolve or close the separate foundational engine research issues.

**Source boundary:** `docs/adr/0004-packet-emission-ownership.md; docs/adr/0005-incoming-packet-lifetime.md; docs/adr/2026-09-11-datapath-offload-program.md`.

## Operating discipline

Use the shared review-loop and contract-closure baselines supplied by the architecture skills, with the [repository execution overlay](../REVIEW-LOOP.md). These are process decisions and traceability metadata, not new runtime guarantees or maintained verification-aid deliverables. No evidence campaign is needed to keep a rejected direction closed.
