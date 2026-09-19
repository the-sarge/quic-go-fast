# Retained hotswap failure capture

## Accepted contract

Extend the [HTTP capture contract](http-failure-captures.md) to `TestHTTP3ServerHotswap` for [#188](https://github.com/the-sarge/quic-go-fast/issues/188). Installed diagnostics locate a stalled boundary on a future natural failure; they do not resolve the investigation. Reconsider retention when #188 resolves. The existing #151/#301 contract remains in force.

Acceptance requires retained observations identifying both fresh clients, both HTTP/3 servers and the actual shared listener; Accept entry/result/context, admission, handshake, request, response headers and body-read results, and closure boundaries; a pre-teardown marker before cleanup even on assertion failure; and a controlled fixture failure demonstrating attributable paired evidence. Passing captures must release their slots, and inherited quota/incomplete/finalization regressions must pass.

Preserve the caller-owned listener, real early dialing, server ordering, assertions, body-consumption behavior and deadlines. Exclude transport repairs, TLS secrets, raw payloads, timeout changes, broad reproduction campaigns and claims about the historical cause. Explicit lifecycle operations inside the scenario remain test-phase observations; deferred closure belongs to cleanup.

The fixture, listener forwarding wrapper and regression are maintained verification aids; this document is process metadata. There are no production changes or new dependencies. The approved blast radius is fixture-local synchronous recording, scalar HTTP trace callbacks and connection watchers, which can perturb scheduling. Existing Go/qlog encoders own representations; the existing recorder owns event admission, quota and persistence. The supported domain is this two-server fixture and the existing integration matrix, with example-level diagnostic coverage. Contract closure is not triggered. Missing connection events cannot distinguish pre-demultiplexing loss from filtering or dispatch.

Reuse the existing eight-slot 256 MiB directory, checksums, command/source metadata and 14-day CI upload without changing the wrapper or workflow. Early dial return and TLS trace callbacks do not imply handshake completion; record the server connection handshake signal, client qlog progression and the response TLS handshake state. Client connection handles remain owned by the unchanged HTTP transport. Observer timestamps are not a causal total order.

## Finite evidence and delivery

The approved seams are the real fixture subprocess and retained files, plus inherited recorder lifecycle tests. One red/green controlled-failure case extends `TestHTTPCaptureFixtureFailure` to hotswap and checks both clients/servers, listener tracing, body consumption and cleanup phase. One passing subprocess checks slot removal. Existing `TestHTTPCapture` limit, encoding/write failure, quota and finalization cases discharge shared storage obligations. No new mutation or platform cross-product is allocated.

Final certification: `go test -race -count=1 -run 'TestHTTPCapture|TestHTTP3ServerHotswap' ./integrationtests/self -version=2`; `go test -count=1 ./integrationtests/self`; `go vet ./integrationtests/self`; `go mod tidy -diff`; repository lint/formatting and `git diff --check`. Existing hosted checks must pass on the live PR head. Additional focused iterations require an actual failure or accepted review correction.

Use one initial RAS review, verification of independently accepted fixes and at most one replacement review. Apply the shared four-way dispositions and repository [execution overlay](../REVIEW-LOOP.md). Merge with an exact-head guard, then deliver the journal before starting socket-rebind capture. Complete capture task `aVFcE3BmFvm` and retain a waiting-for-natural-evidence task; keep #188 open. Review history and certification receipts belong in PR discussion.
