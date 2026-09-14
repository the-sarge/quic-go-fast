# Issue 301: bounded HTTP/3 reconnection diagnosis

## Result and current contract

The historical second-request timeout was not reproduced, and its cause remains unresolved. One instrumented seeded suite replay and one isolated-test experiment passed on 2026-09-14 UTC. The two conditional experiment slots remain unused: no failure or concrete predecessor signal justified further minimization. This completes the approved diagnosis, not a repair or closure of [#301](https://github.com/the-sarge/quic-go-fast/issues/301).

The normative scope is the [diagnosis-only Agent Brief](https://github.com/the-sarge/quic-go-fast/issues/301#issuecomment-5657605975), supplemented by the user-approved execution plan: one documentation PR, one additional instrumented seeded suite replay, at most three single-invocation experiments, and temporary source instrumentation removed before delivery. The user's subsequent instruction required reading [#151's completed experiments](https://github.com/the-sarge/quic-go-fast/issues/151#issuecomment-5637200610) and [#169's triage notes](https://github.com/the-sarge/quic-go-fast/issues/169#issuecomment-5631287666) before investigating, prioritizing existing experiments without repeating completed work, and keeping issue limits separate.

The report and receipts are process/traceability metadata; the archived temporary probes and captures are verification aids, retired at investigation completion. There is no shipped behavior or required safety-enforcement change. The supported representation is these recorded Go test executions and existing QUIC/HTTP event encoders, owned by their existing implementations; the guarantee is example-level evidence, not universal protocol coverage. Contract closure is not triggered. No aid is promoted into a maintained merge gate. Logging and event encoding can perturb scheduling; their effect on the original rare failure is unknown.

Preserve upstream API/wire compatibility, HTTP exchange-lifetime behavior, existing assertions, timeout configuration, request order, and fixture cleanup. Product/fixture corrections, timeout increases, quarantine, CI changes, additional hosted reruns, stress/repetition campaigns, permanent diagnostic infrastructure, and merging other timeout investigations are outside this contract. Stop when the bounded observations are complete or unavailable; a correction or further evidence scope requires a separate decision.

## Preserved inputs and revision reconciliation

The [original failed job](https://github.com/the-sarge/quic-go-fast/actions/runs/34787145873/job/103804558816) tested synthetic merge `d60084a4e0bf369c3cca89416fe1bdd7aceb8fba`, parents base `a7db4f7c5516f153b90c9120aa94f71d63ca14eb` and PR head `7247e5a10eb951ae5cb8266b3979e4611c85b8ba`. Synthetic merge and PR head have identical trees. It used Go 1.27.1 darwin/arm64, macOS 26.6.2 build 25G83, runner image macos-26-arm64 `20260907.0351.1`, non-race, `TIMESCALE_FACTOR=3`, QUIC v2, and shuffle seed `1789338906802733000`. Command: `go test -v -timeout 5m -shuffle=on ./integrationtests/self -version=2`. The first deliberate dial error satisfied its assertion; the second GET failed at original `http_test.go:657` with `timeout: no recent network activity`, target duration 5.00s. The log cannot locate the stalled boundary.

Prior evidence, not rerun here: the independently passing push run, the [operator-authorized attempt 2](https://github.com/the-sarge/quic-go-fast/actions/runs/34787145873/job/103819050481), a passing Go 1.27.0 local replay in 24.532s, and triage's exact-synthetic-merge Go 1.27.1 local replay in 24.405s. The latter used `GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=3 go test -count=1 -v -timeout 5m -shuffle=1789338906802733000 ./integrationtests/self -version=2` on the matching macOS build; target PASS 0.00s. Passing attempts establish neither a fix nor infrastructure causality.

This investigation used `/Volumes/worktrees/quic-go-fast/diagnose-reconnect-301`, branch `codex/diagnose-reconnect-301`, base HEAD `f0c7afd23dd2355110453a8cd1bcc1f5996c6f77`, plus the archived temporary patch. The initiating checkout and prior triage worktree were left untouched. Local platform was Darwin 25.6.0 arm64, macOS 26.6.2 build 25G83, Go 1.27.1 darwin/arm64, non-race, timescale 3 and QUIC v2. HTTP/3, self-integration tests, and root `client.go`, `server.go`, `transport.go`, and `connection.go` have no diff between the failed synthetic merge and this base. The current-main run is not described as an exact historical checkout. Comparison of actual logs found the same 179 top-level test names in the same order; this still does not reproduce scheduler decisions, machine load, preceding job processes, ports, or random protocol inputs.

## How previous investigations changed the priority

| Prior evidence | Implication for #301 |
| --- | --- |
| #151's delayed dial handoff filled its one-slot channel on a retry and hung until its 20s process timeout. | #301 has no such channel. Do not repeat the injection or attribute its returned five-second error to that demonstrated deadlock. |
| #151's early-server delay let the 30ms HTTP idle timer fire before handshake completion, but produced a remote application error in about 0.11s. Its listener-delay experiment passed. | #301 has HTTP `IdleTimeout=0`; the HTTP timer is disabled. Do not repeat the timer/startup injections or equate their outcome with the historical timeout. |
| #169's 50 isolated requests per Go version passed; its cleanup and failure-tail changes did not establish the timeout's cause. It now waits for a natural failure capture. | Do not repeat the HTTP/0.9 campaign. Capture both endpoints and distinguish trace absence from missing or truncated observations. No separate #169 test was invoked. |

The ranked observations were: (1) fresh QUIC connection establishment, because different HTTP fixtures reach the same dial path; (2) failed-entry eviction/fresh dialing specific to #301; (3) concrete prior-test interference. These are priorities for falsifiable observations, not ranked causal conclusions. There was no additional dedicated #151 experiment; its ordinary inclusion in the approved #301 full-suite replay did not reopen its exhausted investigation.

## Experiments and observations

Temporary probes recorded the listener/Serve boundary, custom Dial entry/return with attempt number, pre-default configuration, client/server handshake observations, request handler entry/write return, response headers, transport cleanup, and closure causes. Existing qlog event encoders supplied per-connection transport and HTTP/3 events, serialized on observation and retained without tail truncation until cleanup. Endpoint addresses, local connection pointers, and shared initial connection ID correlate the records. No process-wide qlog flag, delay injection, deadline change, response-body read, or repair was introduced.

The sole additional suite command was:

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=3 go test -count=1 -v -timeout 5m -shuffle=1789338906802733000 ./integrationtests/self -version=2
```

Prediction: a stalled fresh dial/handshake would differ from a failed-entry reuse that never enters real dial attempt 2; endpoint and HTTP events would distinguish later request/response failure. Result: suite PASS 24.325s; target PASS 0.00s. The three preceding top-level tests were `TestTokensFromNewTokenFrames`, `TestHTTPHeaders`, and `TestKeyUpdates`, matching the historical log. The immediate predecessor changes the key-update interval and registers its reset with `t.Cleanup`; inspection and this passing replay do not establish interference or prove its absence.

| Suite milestone, UTC 2026-09-14 | Observation |
| --- | --- |
| 01:44:18.894034 | Listener `127.0.0.1:49776`, HTTP idle timeout zero. |
| .894055–.894065 | Dial attempt 1 returns the deliberate sentinel; first GET reports it. |
| .894071–.894074 | Second GET starts and enters dial attempt 2: fresh dialing was reached after the failed entry. |
| .894512–.894764 | Both endpoint tracers identify initial CID `9d45d99be1a24932`. Initial and Handshake events are present. |
| .895006 | Server accepts with TLS incomplete, local `127.0.0.1:49776`, peer `127.0.0.1:64997`. |
| .895270–.895385 | Client dial returns successfully with TLS complete; both handshake-completion signals are observed. Client socket address is `[::]:64997`, resolved remote is IPv4 loopback. |
| .895425–.895503 | Handler runs and writes 14 bytes; client receives stream 0 data with FIN and parses HTTP 200 headers; second GET succeeds in about 1.4ms. |
| .895506 onward | Client cleanup closes transport; client reports local application error 0, server reports remote application error 0. Listener shutdown returns its socket-closed error. |

Experiment 1 changed only test selection, keeping the same probes, seed, toolchain, timescale, version, count, and process timeout:

```sh
GOTOOLCHAIN=go1.27.1 TIMESCALE_FACTOR=3 go test -count=1 -v -timeout 5m -shuffle=1789338906802733000 -run '^TestHTTPReestablishConnectionAfterDialError$' ./integrationtests/self -version=2
```

Prediction: reproducing without predecessors would demonstrate that prior suite activity is unnecessary; a pass cannot exclude intermittent interference. Result: package PASS 0.474s; target PASS 0.00s. Attempt 2 again establishes a fresh connection and returns HTTP 200, about 4.1ms after second GET entry. Initial CID `066c4a3b79e330e9e38fa9` correlates listener `127.0.0.1:59841` and client port 64974. Both handshakes complete. During cleanup, the client reports local application error 0, while the server reports listener socket closure instead of observing the peer close first. This is an observed teardown-order difference after a successful response, not evidence of the historical failure.

Both captures contain 54 client and 61 server events with no reported encoding errors and no tail truncation. These are snapshots taken after normal fixture cleanup, not a claim that future recorder events are impossible or a pre-demultiplexing UDP capture. Watcher timestamps describe when a completion signal was observed, not the precise protocol transition. Header arrival and receipt of stream FIN do not establish application response-body consumption; the unchanged test does not read or close that body before transport cleanup.

Experiments 2 and 3 were not run. They were conditional predecessor minimization/boundary probes; no reproduced failure or concrete signal admitted such a comparison. The approved finite budget is one of one additional suite runs and one of three targeted experiments used, without hidden repetitions, new stress gates, or dedicated cross-issue runs.

## Connection to #151 and #169, and remaining uncertainty

The concrete connection is shared implementation reachability: #151 and #301 use a custom callback calling [DialAddrEarly](https://github.com/the-sarge/quic-go-fast/blob/f0c7afd23dd2355110453a8cd1bcc1f5996c6f77/client.go#L44); [HTTP/0.9](https://github.com/the-sarge/quic-go-fast/blob/f0c7afd23dd2355110453a8cd1bcc1f5996c6f77/interop/http09/client.go#L91) also calls it. This path resolves the address, creates a socket-backed transport, and enters QUIC dialing. Unset handshake-idle configuration is populated with the five-second default. Neither that default nor matching error text establishes that a historical handshake stalled. #301's observed server acceptance before handshake completion also matches a successful observation in #151, but with the HTTP idle timer disabled.

No concrete causal evidence connects the three failures. Their HTTP request boundaries, timeout configuration, and fixture handoffs differ. #301's two passing captures show no stalled establishment, failed eviction, loss, or premature close to compare against an informative failing capture from either other issue. No issue is merged, closed, or granted additional investigation budget by this report.

The smallest missing observation is one natural occurrence of the exact second-GET failure with paired endpoint capture: dial attempts/results and effective configuration; listener and resolved destination; Initial/Handshake packet progress; server acceptance and both handshake signals; request stream ID/handler entry; response header/write/FIN progress; and closure causes before versus during teardown. Bind it to tested SHA, instrumentation patch, toolchain, platform, command, timescale, version, seed, and complete checksummed log. Include event counts, truncation and encoding errors. Missing connection events alone cannot distinguish packets never arriving, being dropped before demultiplexing, or incomplete capture.

Proposed next scope: analyze one such natural failure if available. Installing retained diagnostics requires a separately approved narrow change; the temporary patch is a capture design reference, not maintained infrastructure. No correction or causal regression seam is established yet. Once an owner is demonstrated, the existing deliberate-first-error/real-second-dial test is the candidate end-to-end seam; a narrower regression should be chosen from the actual failed boundary. Do not spend the unused slots or launch new repetitions solely for additional confidence.

## Evidence and review boundary

The [frozen evidence archive](evidence.tar.gz) contains original hosted/triage logs, issue snapshots, the two new captures, command/environment metadata, top-level order comparison, original test snapshot, and exact temporary source patch/helper. [SHA256SUMS](SHA256SUMS) covers the archive; its internal manifest covers its individual inputs. The original retained evidence directory remains unchanged.

| Capture | SHA-256 |
| --- | --- |
| Historical hosted attempt 1 | `7e6f31281afa71589ab2dc149cc6f3d181994e2820f713ccaf4699fc80d52e72` |
| Historical triage seeded replay | `336b38e824399f63decb31845dc3efd8a23d82bcbe2e7c1448f5e9536244524c` |
| Instrumented seeded suite | `c0d4767a1c1793a3e6e8eb7959ef5e653a771ba5686dbaa58754ad8bc1a32a42` |
| Instrumented isolated test | `d3c6b33dd75c50a1c9d72081a9d22f5b837906f0ef41f66d59fb6f25340b69d1` |

Cleanup restored only the investigation's edited test from its exact-HEAD byte snapshot and removed the added diagnostic helper. The worktree was verified clean before this documentation was created; there is no final Go source diff. No extra test execution was needed after restoring unchanged source.

The terminating evidence is the bounded investigation, faithful report, verified captures and source restoration. Documentation review is limited to one initial RAS review, verification of accepted findings if required, and at most one replacement review; reviewers must not convert the archived probes into a maintained verification obligation. Apply the [repository execution overlay](../../REVIEW-LOOP.md): exact-head/base documentation certification, whitespace/link/hash validation, applicable existing hosted checks, and pinned-head squash merge. Keep certification and review chronology in PR discussion. Append the journal only after merge and reconcile the diagnosis task; #301 remains open while its cause is unresolved.
