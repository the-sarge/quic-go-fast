# E6 close emission and final contraction

This receipt records E6's close-storage correction and the final packet-emission wiring change against parent `805e86c643abc8f1fbccd6ff9759cafaf5cc5402`. The normative contract remains [E6 in the packet-emission plan](../../adr/2026-09-07-packet-emission-plan.md#e6--own-close-emission-and-retire-the-legacy-seam). **Disposition: complete under the owner’s explicit continuation decision; final performance qualification remains unestablished.** Both permitted campaigns are consumed. After the failed qualification and stop report, the owner instructed “no. implement E6 and stop fucking around”, authorizing completion of the retained implementation with that uncertainty recorded. The E6 section of the plan owns this exception; correctness, ownership, review and exact-head hosted gates remain required.

## Ownership and source map

| Previous seam | Final owner and preservation evidence |
| --- | --- |
| Connection close construction and direct write | `packetEmission.close` constructs through the real packer, copies the retained payload, writes synchronously, then releases pooled construction storage on both write success and error. `TestEmissionCloseRetainedLifetime` checks storage release and retained retransmission after poisoning that storage. |
| Private close-constructor append failure | `packetPacker.packConnectionClose` releases its unreturned buffer on both long- and short-header append errors. `TestCloseConstructorFailureLifetime` preserves packet-number error behavior while checking release. |
| Two remaining close/error packer-mock consumers | `testConnectionClose` decodes transport/application close fields and preserves qlog, original error and idempotent-close assertions; queued writes must precede close output. `testConnectionUnpackFailureFatal` retains unpack-error precedence through real construction. |
| Broad packer interface, generated mock and test defaults | Removed from `packet_packer.go`, `mockgen.go`, `mock_packer_test.go` and `testConnection`. Existing real-packer fixture assignments now target emission's concrete packer. Packer conformance and unrelated mocks remain. |
| Connection packet-shape dispatch and legacy outcomes | `packetEmission.send` consumes recovery dispatch and PTO continuation; `sendAny` selects coalesced, ordinary/GSO or typed probe intent. `Conn.prepareEmission` retains path/MTU/control policy. `triggerSending` applies the returned scheduling outcome. No legacy result or forwarding arm remains. |
| Setup, token administration and queue capacity/lifecycle | One concrete packer and one queue are held by emission. Token updates target that packer; the connection requests startup, drain and initial capacity through the emission lifecycle surface. Emission completes post-send capacity accounting. Existing replacement/rebinding policy remains unchanged. |

The source-removal guarantee covers the accepted finite census. Wire representation remains owned by the existing codecs and TLS implementation. Runtime wiring is shipped behavior; close cleanup is required safety enforcement; tests and disposable measurements are verification aids; this receipt and source map are traceability metadata. No maintained analyzer or measurement framework is introduced.

The seventh ownership-matrix row is discharged by positive/write-error retained-close observations and representative long/short constructor errors. Both lifetime regressions were observed failing before their cleanup fixes. Existing remote/immediate/never-sent-client suppression, close/error precedence, queue, handshake/0-RTT/key-update, feedback, receive-fairness, GSO, MTU and migration coverage remains. No additional guard mutation, platform sweep or repetition program was added. Receive storage, external socket ownership, path-policy redesign and physical-link qualification remain outside this work.

## Local preservation

The initial implementation and the layout-only revision each passed `go test -count=1 ./...`, the declared focused ownership/close/feedback race run, `go vet ./...`, `golangci-lint run --timeout=3m` and `go tool gcassert ./...` on macOS. Native Linux full tests, focused races and vet are also required before each capture. These are working-candidate receipts; final pushed-head certification and hosted checks remain separate.

The race command is `go test -race . -run '^TestEmission|^Test.*Constructor.*Lifetime|^TestConnection(Close|Unpack|GSOBatch|SendQueue|ReceivePrioritization|PathValidation)|^TestHandshakeMTU|^TestSendQueue' -count=1`.

## Initial qualification

Initial candidate `a4ef2124b15068d68dad16b0035e81cee19dae22` uses native Linux Go 1.27.1 against the exact parent, with identical measured fixture sources and the existing dedicated-core placement. Ordinary/GSO allocations remain 37/104 per batch. The initial repeated-close allocation observation (62/61 allocations) is retained only as invalid-domain diagnostic evidence: the replacement precheck exposed that repeatedly constructing close output on one connection can violate packet-number skip accounting. Correct cold-close evidence uses one emission per independently prepared connection, as recorded below. Connection size changes from 1224 to 1200 bytes; emission's inline value changes from 56 to 64 bytes, with no separately allocated module.

The initial handshake and both transfer benchmarks pass the unchanged 5% time gate. Stream churn does not establish noninferiority: geometric mean time ratio 1.0422348839619615 and one-sided 95% upper bound 1.0866302052917856. All ten alternating pairs are retained; variance does not establish an external infrastructure fault or a causal explanation. The initial traffic campaign was stopped at this known failed gate, and its completed/interrupted samples are retained as incomplete evidence. They cannot qualify any traffic cell. All CPU restrictions, reservation markers and restoration timers were cleared and checked.

The sole permitted replacement tests an in-contract, behavior-preserving revision that groups the byte-sized `emissionResult` flags together to avoid padding in the returned value. This revision is not claimed to explain the initial churn result. Replacement candidate `85651b3c11ade08f7a27eb590c0c412d77acaa6d` uses the same parent and unchanged workloads, timing, sample count, statistical method and margins. Samples are never pooled across candidates.

## Replacement qualification

The first replacement allocation precheck failed on the unchanged parent because its fixture repeatedly constructed close packets on one connection. The corrected fixture prepares 1001 independent connections outside measurement, then consumes one close per connection (one warmup plus 1000 measured observations), preserving real packer/recovery behavior and adding no close registration. That precheck and its correction are retained; no replacement benchmark or traffic samples preceded the correction. The replacement budget has been assigned and must not be renewed. Exact source, binary, fixture, host, raw-sample, analysis and restoration records are retained in the campaign archives. Review, final correctness certification and hosted verification remain separate from these performance observations; adoption follows the explicit E6 continuation decision.

The corrected cold-close observation is 63 allocations on the parent versus 62 on the candidate for a 27-byte serialized payload. Ordinary/GSO allocations remain 37/104. These are example-level observations with test-boundary overhead; they do not claim that every close serializes to that length.

All ten replacement benchmark pairs pass: the one-sided upper time ratios are 1.0079749331068881 for handshake, 1.008845721959441 for stream churn, 1.006863829774766 for 500 kb transfer and 0.9982379431214904 for 51200 kb transfer. The qlog diagnostic smoke passes. The change in churn result does not establish a causal attribution to field layout.

P1 completes all ten valid alternating pairs but fails the unchanged successful-probe p99 margin. The geometric mean ratio is 1.1261588997389695 and the one-sided 95% upper bound is 1.2358177429874126, exceeding 1.05. Its goodput lower bound is 0.9996081035068848; CPU/unit upper bound is 1.0077496565134814; allocated-bytes/unit upper bound is 1.000235999761979; bad-probe upper difference is zero. Those four gates pass. There are no failed or missed probes or invalid/duplicate payloads in the valid P1 observations. This is unsuccessful noninferiority qualification, not a proven external infrastructure fault.

The remaining traffic collection was stopped at the failed required P1 gate. P3/P4/P5/P6 remain unqualified; all completed or interrupted samples are retained. The systemd launcher returned zero when its service was intentionally stopped, which is not a completed-campaign receipt: the missing required pairs and explicit disposition control acceptance. Final restoration checks confirm empty machine/system/user CPU restrictions, no reservation marker, and an inactive restoration timer.

At the campaign stop, no RAS review, PR publication, exact pushed-head certification, merge or journal had been performed. The owner subsequently directed completion of E6, accepting the reported qualification uncertainty without further sampling or changed margins. This product PR owns the resulting completion and empty-frontier transition; its PR discussion records review and final certification receipts. The failed P1 latency result remains unresolved, and the unmeasured traffic cells remain unqualified. No causal attribution or performance pass is claimed.

## Artifact integrity

The initial archive retains the failed benchmark, original allocation diagnostic, partial traffic observations, exact sources/builds/fixtures, host records and restoration evidence. The replacement archive additionally retains the invalid allocation precheck and corrected fixture/build receipt, passing benchmarks, full P1 data and the explicit stop disposition. Source bundles in the two archives are incremental: restore the initial bundle before the replacement bundle, using their recorded prerequisites. No samples are pooled or selectively replaced.

- `initial-capture.tar.gz` SHA-256: `0d152dc7ebf559816912b28ac5ec03b5d9bdff5b1bb51817bac1cae33c758273`.

- `replacement-capture.tar.gz` SHA-256: `1700a2560269f492cb36fe5e981b87ea8d8af30ab78ac3b2b0dc2921f514c33c`.
