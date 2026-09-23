# Combined HTTP/3 convenience server: bounded investigation

## Result

**Remaining uncertainty; repair priority is unproven.** At source commit `19d90fd966cd438f7828cce6491a9d694b3ebd63`, the combined `http3.ListenAndServeTLS` helper still returns from its QUIC-error branch without closing the TCP server. The normal-serving baseline passed on Darwin/arm64 with Go 1.27.1. This investigation did not identify a deterministic QUIC-first trigger controllable through the helper's public arguments, and did not reproduce TCP serving after the helper returns. That is a bounded reachability gap, not proof that QUIC-first failure is impossible.

Issue [#424](https://github.com/the-sarge/quic-go-fast/issues/424) and parent [#422](https://github.com/the-sarge/quic-go-fast/issues/422) currently scope this as an investigation. The older repair brief in #424 is not this work's acceptance contract. No production hook, ownership redesign, runtime fix, or maintained regression test was added. No historical failure is attributed to this pattern.

## Approved contract and evidence limit

The approved plan specified the following execution and reporting criteria:

> Trace QUIC startup and serving errors through the public combined helper. Identify a controllable trigger without changing production code. If that requires a new hook or lifecycle design, document the gap and stop.

> Run at most **one normal-serving baseline and one controlled error case** on this host. Use an isolated subprocess, explicit cleanup, and a 30-second timeout per case. Check normal TCP/QUIC handler dispatch and Alt-Svc; for the error case, capture the originating error and whether TCP still serves after the helper returns. No repetition or platform campaign.

> Commit a concise report recording the source SHA, commands, observations, limitations, and one disposition: confirmed defect, unsupported trigger assumption, or remaining uncertainty.

The first baseline attempt used the hostname in a stale fixture comment and failed certificate verification. The user explicitly authorized **one corrected baseline** with the certificate's actual `localhost` identity. Both attempts are retained. Zero controlled-error cases were run: a suitable public control was not established, and fabricated socket failure through a new production seam would cross the stop boundary. No mutation, race, stress, or additional platform campaign was authorized or run.

The representation domain is the public combined helper with a concrete IPv4 loopback address, repository certificate/key, and explicit handler. Go's TLS, network, and HTTP implementations and the repository QUIC implementation own their protocol representations. The guarantee is example-level observation plus source trace, not universal failure coverage. Contract closure is not triggered: this report establishes no new lifecycle enforcement obligation.

Production behavior and required safety enforcement are unchanged. The archived diagnostics are disposable verification aids, not maintained product dependencies or CI gates; retire them from execution after these observations. This report and identity record are traceability metadata. No new dependency or public API is introduced. Frozen evidence elsewhere is unchanged. Review budget: one initial RAS review, verification of accepted corrections, and at most one replacement review; documentation-only polish follows the shared lightweight-check exemption. Review receipts belong in the PR discussion rather than extending this normative contract.

## Source trace

All links below are pinned to the inspected source commit.

| Surface | Observation and implication |
| --- | --- |
| [Combined helper, lines 752–808](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/http3/server.go#L752) | Certificate loading, address resolution, and UDP bind precede both serving goroutines. Failure there is not QUIC-first termination after TCP starts. The helper constructs its own UDP socket and fresh HTTP/3 server, with no caller handle to either. TCP is started through the package-level `http.ListenAndServeTLS`, with no retained `http.Server`. |
| [Error selection, lines 802–808](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/http3/server.go#L802) | TCP-first exit calls `quicServer.Close`; QUIC-first exit simply returns. Deferred UDP closure does not own the TCP listener. This is the source-level cleanup gap, not a reproduced leak. |
| [HTTP/3 Serve and listener setup](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/http3/server.go#L257) | `Serve` returns setup errors or the accept-loop result. Setup uses loaded TLS certificates and default QUIC configuration. Caller-driven invalid QUIC configuration, caller-owned socket closure, and explicit server shutdown are not controls exposed by the combined helper. |
| [Socket setup](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/sys_conn.go#L79) and [socket options](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/sys_conn_oob.go#L101) | Descriptor access, DF configuration, ECN setup, and required packet-info setup can return errors after UDP bind. These remain plausible environment-dependent QUIC-first triggers; this investigation does not rule them out. No such failure was observed or controllably induced here. |
| [Transport receive loop](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/transport.go#L588), [transport closure](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/transport.go#L540), and [listener accept](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/server.go#L391) | A terminal packet-read error closes the transport and listening server, eventually reaching the HTTP/3 accept loop. The helper does not expose the socket needed for a direct close/deadline control. Temporary read errors are retried; per-connection errors do not by themselves terminate the listener. |
| [HTTP/3 accept loop](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/http3/server.go#L370) | Request/connection handling runs separately from listener acceptance. Breaking a client handshake or request is not equivalent to forcing the helper's QUIC serving loop to return. |
| [Example caller](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/example/main.go#L159) | The TCP flag selects the combined helper. The example prints a returned error and waits for its configured listener goroutines. A single-listener process can then exit, letting the OS reclaim sockets; this does not establish cleanup for a long-lived caller or a process still serving other listeners. |

## Runtime observations

The diagnostic chose one ephemeral TCP port, released its reservation, and invoked the unchanged combined helper on the corresponding loopback address. Port-selection races were not retried. A connection-only readiness poll had a five-second deadline; its connect-and-close explains the expected TLS handshake EOF in the logs. Each HTTP client had a five-second timeout. HTTP/3 was requested first so listener registration preceded the TCP Alt-Svc check.

The [first attempt](initial-output.txt) failed both TLS verifications because [the fixture comment](https://github.com/the-sarge/quic-go-fast/blob/19d90fd966cd438f7828cce6491a9d694b3ebd63/internal/testdata/cert.go#L27) names `quic.clemente.io`, while `openssl x509 -in internal/testdata/cert.pem -noout -ext subjectAltName` reports `DNS:localhost`. This was a diagnostic error, not evidence of helper termination. Its exact source is [archived separately](initial-baseline.go.txt).

The approved corrected attempt changed only the diagnostic TLS `ServerName` to `localhost`. The [output](baseline-output.txt) records HTTP/3.0 and HTTP/1.1 responses, both status 200 with body `issue-424-baseline`, and TCP `Alt-Svc: h3=":52920"; ma=2592000`. No helper-return event was observed before the diagnostic finished. The [corrected source](baseline.go.txt) uses the repository root CA rather than disabling certificate verification.

The parent waited for each diagnostic subprocess to exit before successfully rebinding both TCP and UDP at its selected address. Clients were closed explicitly by the diagnostic; the server's remaining goroutines and sockets were reclaimed on process exit. **Post-process-exit rebind is only cleanup of the experiment; it is not evidence that the helper closes TCP or joins workers on return.** TCP survival after a QUIC-first helper return remains unmeasured.

## Commands and receipts

The [identity record](identity.json) records the host, source SHA, Go version, attempt counts, authorization exception, and hashes of the diagnostic sources and output. From the recorded source checkout, the corrected diagnostic was built in the disposable directory `investigation424-tmp` (the archived `.go.txt` file was `main.go` there):

```python
subprocess.run(['go', 'build', '-o', 'investigation424-tmp/baseline-corrected', './investigation424-tmp'], check=True, timeout=120)
p = subprocess.run(['./investigation424-tmp/baseline-corrected'], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=30)
```

The initial attempt used the archived initial source and binary name `baseline`. The parent appended the process exit code, extracted the address from `ADDRESS: 127.0.0.1:<port>`, and used Python `socket.socket(AF_INET, SOCK_STREAM/SOCK_DGRAM)` with `SO_REUSEADDR=1` to bind the same port; TCP additionally called `listen()`. Each probe socket used a context manager and was closed. `subprocess.run` kills and waits for the diagnostic if its timeout expires; no timeout occurred. Only the diagnostics themselves were subprocesses; they did not spawn descendants.

## Disposition and next decision

The finite investigation is complete with remaining uncertainty. Preserve the concern without claiming a confirmed runtime defect or an impossible path. A further reproduction needs either a concrete environment that produces the traced socket-setup/read failure, or separately approved fault injection and its supported domain. A repair additionally needs an approved ownership, close/join, and error-selection contract. Do not convert this report into authority to implement the older repair brief, reopen standalone HTTP/3 admission work, or rerun a broad campaign. Keep #424 available for that scope decision and remove any implication that a repair is ready for automatic dispatch.
