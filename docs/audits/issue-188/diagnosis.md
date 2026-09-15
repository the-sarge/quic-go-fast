# #188: bounded HTTP/3 server hotswap diagnosis

**Result: non-reproduction; historical cause unresolved.** This completes the diagnosis requested by [OmniFocus task aVFcE3BmFvm](omnifocus:///task/aVFcE3BmFvm) and the [authoritative issue brief](https://github.com/the-sarge/quic-go-fast/issues/188#issuecomment-5631362322). It does not repair or close #188. Collection occurred on 2026-09-15 UTC. One additional seeded self-suite run and one targeted hypothesis experiment were used. Two experiment slots remain unused: neither execution supplied a failed boundary to minimize, and further undirected retries would not recover the missing hosted observation.

## Preserved historical evidence

The hosted replacement GET `/hello2` failed before response consumption with `timeout: no recent network activity`; the test took 5.07s. [Run 34549106858 / job 103108033710](https://github.com/the-sarge/quic-go-fast/actions/runs/34549106858/job/103108033710) checked out synthetic merge `9820d966cd72fbfe70d55829b971797b05d3ed3f`, with base `928fc604a1968c8df20379e34fc9778c86ce1102` and PR head `dfd03fc85ead6fa2f33b12cd0a660dea5e5d721a`. It used Go 1.27.1 darwin/arm64, non-race, `TIMESCALE_FACTOR=3`, QUIC v2 and seed `1789088800293547000`. The failure was logged at 2026-09-11T01:06:51Z. Command: `go test -v -timeout 5m -shuffle=on ./integrationtests/self -version=2`. Timeout wording and duration do not locate the stalled boundary.

The independently triggered [push job](https://github.com/the-sarge/quic-go-fast/actions/runs/34549103085/job/103108022258) passed on the same PR head. The original focused local macOS Go 1.27.0 attempt, `TIMESCALE_FACTOR=3 go test ./integrationtests/self -run '^TestHTTP3ServerHotswap$' -version=2 -shuffle=1789088800293547000 -count=1 -timeout=30s`, also passed, according to the issue body. Its separate raw output was not supplied in the triage directory. The later exact-synthetic-merge local Go 1.27.1 suite used `GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=3 go test -v -timeout 5m -shuffle=1789088800293547000 ./integrationtests/self -version=2` and passed in 24.850s, with hotswap passing in 0.07s. None of these passes resolves the hosted failure.

The original files were left untouched. Byte-identical copies and their verified SHA-256 values are retained here:

| Artifact | SHA-256 |
| --- | --- |
| [Failed step](prior/failed-step.log) | `d592898fbc2936362a2032980a8b21e160443abb28bbd4d552195118f77bad70` |
| [Full hosted job](prior/quic-go-fast-188-hosted.log) | `995b87eea65503d62c44950571281447ab91de1c412906f00c34ed1ec03c3aa3` |
| [Prior exact-source local suite](prior/quic-go-fast-188-local.log) | `f6efa08b68208d57a38d078afaae660d7a79b011577e8cf07d7e40ec1ae343c2` |

Original locations: `/Volumes/worktrees/quic-go-fast/triage-hotswap-188-state/` and `/Volumes/worktrees/quic-go-fast/repeated-path-validation-review-state/receipts/ci-failure.log`. The [original brief](prior/agent-brief.md) is preserved too.

## Current source reconciliation

New collection used main `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7`, tree `ad4ae2c7788f29de890c24ac18a522c5473b12fa`, plus the [temporary instrumentation patch](temporary-instrumentation.patch). The dedicated worktree is `/Volumes/worktrees/quic-go-fast/diagnose-hotswap-188`, branch `codex/diagnose-hotswap-188`. See [environment](environment.json) and [source reconciliation commands/output](source-reconciliation.txt).

- The hotswap fixture, `http3/transport.go`, `http3/client.go` and `client.go` are unchanged from the failed synthetic merge. The fixture waits for server 1's serving goroutine to return before closing client 1 and creating client 2. It preserves the caller-owned listener.
- PR #290 (`96a7c9eb`) changed HTTP/3 server admission, stable completion signaling and shutdown. A connection accepted after sealing is now closed with H3_NO_ERROR instead of being abandoned by a canceled serving loop. That is relevant neighboring behavior, but the fresh replacement client's connection does not exist until after the old serving loop has returned. The known admission window therefore does not by itself explain the recorded `/hello2` failure. No historical endpoint trace establishes that #290 repaired #188.
- `baseServer.accept` still selects between cancellation, the connection queue and listener shutdown. Newer `server.go` changes concern receive/0-RTT/version-negotiation queue storage retention. Darwin sendmsg_x batching (#257), invalid send-progress handling (#292), incoming STREAM ownership (#286), coalesced receive delivery (#296), HTTP/3 datagram queue release (#298) and dependency updates (#306) also changed the surrounding transport. New current-source passes cannot be attributed to any one of these changes. Platform-specific Linux/Windows receive work is not evidence of a macOS repair.
- PR #312 added paired HTTP failure capture for two other fixtures, and #315 repaired the idle-timeout fixture's retry observation. Neither wires capture into `TestHTTP3ServerHotswap`; neither establishes its cause. Existing HTTP capture upload support may be reused by a separately scoped capture change without changing CI policy.

The new host was macOS 26.6.2 build 25G83, Go 1.27.1 darwin/arm64, CGO enabled, non-race, timescale 3. The prior local log contains 176 top-level tests and this suite contains 190. The same seed on the changed test inventory does not reproduce historical test order. Hosted machine load, scheduling, preceding job steps, port allocation and all random inputs were not reproduced.

## Hypotheses and temporary observations

The ranked boundary hypotheses were: (1) replacement connection progress stops before server 2 accepts it; (2) acceptance succeeds but handshake or HTTP/3 setup stops before handler entry; (3) handler entry occurs but response progress stops before client completion. These are falsifiable on a failure trace: successful acceptance excludes the first boundary; completed handshake plus handler entry excludes the second; successful client consumption excludes the third for that attempt. They are not established causes.

Temporary probes recorded both endpoint QUIC/HTTP3 qlog events with initial connection IDs, server-specific Accept entry/return and context state, admission, server handshake completion, handler entry/write results, GET/header/body progress, server/client closure and listener closure. The listener wrapper forwarded the same Accept context and result to the same underlying listener. The original client dial implementation, addresses, assertions, timeout values and lifecycle ordering were preserved. Only hotswap passing captures were retained through a temporary flag in the existing capture helper; `failed:false` remains accurate. Logging, synchronization and retained references can perturb scheduling, so these are instrumented observations, not uninstrumented reproductions.

### Sole additional seeded suite

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=3 go test -v -timeout 5m -shuffle=1789088800293547000 -count=1 ./integrationtests/self -version=2
```

Prediction: if the historical symptom reproduces, the paired events locate the first missing boundary. Changed inputs relative to the prior suite are the reconciled current source and observation-only probes. Result: **PASS 25.405s; hotswap PASS 0.07s**. [Command, capture-directory environment and UTC receipt](runs/seeded/receipt.json), [full output](runs/seeded/output.log), [paired capture](runs/seeded/http/slot-0/capture.jsonl), [selected observations with capture line numbers](runs/seeded/observations.json).

The replacement connection's paired initial ID was `824e92e87b1ead4fad03e613a97245ac7275f5a8`; listener address `127.0.0.1:51065`, replacement client source `127.0.0.1:55140`. All following times are 2026-09-15 UTC:

| Boundary | Observed time and result |
| --- | --- |
| Old accept / serving loop | `00:49:59.566949` context canceled; `.566955` serving loop returned `http: Server closed` |
| Old client closed / replacement GET begins | `.567158` successful client close; `.567174` GET enters |
| Replacement acceptance / admission | `.568486` server 2 accepts with nil error and uncanceled context; `.568538` admitted |
| Client request / server handshake | `.569087` client sends stream 0 with FIN; `.569268` server handshake watcher observes completion |
| Server HTTP handling | `.569304` `/hello2` handler enters; `.569312` writes 16 bytes with nil error |
| Client handshake confirmation / response | `.569396` receives HANDSHAKE_DONE; `.569470` receives response stream 0 through FIN |
| Client completion | `.569499` GET returns nil; `.569503` status 200; `.569517` consumes 16 expected bytes |
| Closure | `.569521` server 2 Close starts after body consumption; listener Close occurs during deferred cleanup |

Ordinary GET waits for the client's `HandshakeComplete` channel in `http3/client.go`; sending request stream 0 therefore establishes client handshake progress before the server watcher's observation. The watcher timestamp is an observation time, not the instant the channel closed. HANDSHAKE_DONE receipt supplies an independent client-side confirmation event. Server handler write success alone would not prove delivery; the received FIN and consumed body do so here. No HTTP server invoked the wrapper's listener Close method.

### Experiment 1: remove suite predecessors

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=3 go test -v -timeout 5m -shuffle=1789088800293547000 -count=1 -run '^TestHTTP3ServerHotswap$' ./integrationtests/self -version=2
```

Changed variable: only test selection, with a separate evidence output directory. Prediction declared before execution: a failure without predecessors would show that suite predecessors are unnecessary; a pass cannot establish or exclude interference. Result: **PASS 0.677s; hotswap PASS 0.13s**. [Receipt](runs/focused/receipt.json), [output](runs/focused/output.log), [paired capture](runs/focused/http/slot-0/capture.jsonl), [selected observations](runs/focused/observations.json).

The replacement ID was `5943f672cc7c7430f232195b`, listener `127.0.0.1:54723`, client source `127.0.0.1:59735`. Server 1's serving loop returned at `00:50:28.988031Z`, replacement GET entered at `.988319Z`, server 2 accepted at `.989398Z`, its handshake watcher fired at `.990099Z`, handler entered at `.990237Z`, and the client consumed the body at `.990496Z`. Client stream 0 send, HANDSHAKE_DONE receipt and response stream 0 FIN were observed at `.989845Z`, `.990273Z` and `.990380Z`. The same successful boundary progression was present.

The seeded and focused captures retained all 317 and 321 observed events respectively, plus their terminal summaries, with zero dropped events and zero encoding errors. Their recording-window completeness describes admitted observations through cleanup, not proof that every kernel or protocol event was instrumented. The historical stopping point remains unknown. No additional hypothesis experiment, stress campaign or hosted rerun was performed.

## Smallest future-failure capture and bounded next scope

The missing evidence is a paired trace from a **natural failure of this fixture**. A separately agreed follow-up should wire the existing bounded HTTP capture helper into hotswap, retaining the original assertions and lifecycle. Identify server 1, server 2 and each fresh client; capture Accept entry/return/context state, admission and connection identity, both endpoints' handshake/request-stream/response progress, typed GET and connection termination causes, and explicit closure/cleanup boundaries. Keep source SHA/tree/patch, actual seed, command, toolchain, environment, capture completeness and checksums together. Enable the tracer on the actual caller-created QUIC listener config; `http3.Server.QUICConfig` alone does not configure that listener.

Add existing `httptrace` DNS, connect and TLS callbacks around the original client request path to distinguish a missing dial attempt from missing QUIC progress. Treat `TLSHandshakeDone` carefully: the transport calls it after `DialEarly`, so also record `tls.ConnectionState.HandshakeComplete` or the actual connection handshake signal. Do not infer full handshake completion from that callback name alone. Observe client response headers, body read errors and FIN, not only server Write success. The temporary patch's body log follows its original successful-read assertion, so a follow-up must record the read result before that assertion to retain failed reads.

If the future trace has client sends but no server connection trace, the next missing observation is bounded listener-transport receive/drop evidence for the same address and connection ID; per-connection qlog alone cannot distinguish pre-connection packet loss, filtering or dispatch. Add socket-level or scheduling capture only if that observed gap requires it. No broad permanent diagnostic infrastructure is proposed.

Any correction must follow the demonstrated boundary: repair acceptance ownership only if a connection is shown lost there; repair transport/handshake progress only with paired evidence; repair request/response lifetime only if the trace locates that stall. The regression seam should retain two servers on one caller-owned listener, wait for the old serving loop, use a fresh client and assert the demonstrated boundary. A later correction requires its own agreed scope, feature worktree/PR, regression evidence and review. No timeout increase, quarantine or speculative shutdown rewrite is justified. #151, #169 and #241 remain separate; PR #187 remains completed and unblocked.

## Delivery and verification

All temporary executable source changes were removed after collection; `temporary-instrumentation.patch` is inert frozen evidence. The final change is this report and its evidence archive. Both Go test invocations compiled/typechecked the touched package and ran vet through `go test`; the one permitted full self-suite run passed. No post-cleanup full-repository test run was added because the final diff contains no executable changes and the diagnosis brief explicitly bounds executions. There is no fix or regression test to certify.

The [SHA-256 manifest](SHA256SUMS) covers the frozen input/output artifacts, including the patch, source reconciliation and exact run receipts; it excludes this editable report and itself. Checks verified all artifact hashes, both paired captures' terminal summaries, the report's local links, and byte-for-byte restoration of both instrumented files. The review baseline is pinned main `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7`.
