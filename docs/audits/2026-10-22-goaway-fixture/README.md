# GOAWAY recorder fixture diagnosis

The [release preparation race failure](https://github.com/the-sarge/quic-go-fast/issues/851#issuecomment-6348262796) occurred before the GOAWAY recorder scenario: `TestServerGOAWAYRecorderShutdown` required successful client request `Close`, but its HTTP/3 handler returned without reading the request body. Normal server response completion calls `CancelRead(H3_NO_ERROR)` for unread input. If that STOP_SENDING reaches the client before its `Close`, `Close` correctly reports `close called for canceled stream 0`.

## Reproduction and scope

Diagnosis used source `d7236cf0165bc4ac24f3168199ad720703bd8113`, Go 1.27.3, darwin/arm64. The relevant test, server completion and send-stream code is byte-identical to failed preparation head `66e0f47cb2d03210b62c3b93d4fa9cb3d57aaf04`. The unmodified focused test passed 100 race iterations with the failed job's shuffle seed `1792693549660301723`; that does not invalidate the recorded Linux failure.

The temporary [probe patch](cancellation-probe.patch.gz) changes only scheduling before client `Close`: wait for the send-stream context cancellation and record its cause. Applying it to the diagnosis source and running `TIMESCALE_FACTOR=20 go test -race -v -run '^TestServerGOAWAYRecorderShutdown$' -count=3 ./http3` produced the exact failure in all three runs. The [complete probe output](cancellation-probe.log.gz) records remote stream cancellation with code 256 (`H3_NO_ERROR`) in every run. No Shutdown call or GOAWAY recording had started. This distinguishes ordinary handler completion from connection shutdown or recorder cancellation. The patch is diagnostic evidence, not part of the compiled test.

The raw patch and log are gzip-compressed without changing their decompressed bytes, preserving the original tab/space formatting while keeping ordinary source whitespace checks meaningful. [SHA256SUMS](SHA256SUMS) records the uncompressed hashes. To inspect them, copy the two `.gz` files and `SHA256SUMS` to a scratch directory, run `gzip -dk cancellation-probe.patch.gz cancellation-probe.log.gz`, then `shasum -a 256 -c SHA256SUMS`. Apply the extracted patch only in a separate diagnosis worktree at the source SHA above; it deliberately produces a failing test and must not be applied to the corrected test.

## Correction and bounded evidence

The fixture handler now drains its request body through EOF before returning. Since the client sends FIN through `Close`, the handler cannot perform normal response completion before that close. Reading EOF also makes the server's later `CancelRead` a no-op. The strict successful request-close assertion and all recorder overlap, late-event and GOAWAY-content assertions remain intact. No production source, slice behavior, default or release gate changes.

On the corrected source, both `TestServerGOAWAYRecorder*` tests passed 100 race iterations with the same shuffle seed, and the entire HTTP/3 package passed once under race with that seed and `TIMESCALE_FACTOR=20`. `go vet ./http3` and `go mod tidy -diff` passed. These are native macOS evidence; the exact-head hosted workflows supply the Linux/platform checks separately. No minimax workload was run. The terminating evidence plan is these focused/package checks plus one exact-head hosted check set, one fully briefed review and at most one replacement review if an accepted in-scope finding requires a fix. Repeating hosted CI until green is excluded.

This directory is historical diagnostic evidence. Its patch and output describe the pre-fix source and must not be mechanically refreshed to later code or paths.
