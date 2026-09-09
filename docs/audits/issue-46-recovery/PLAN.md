# Controlled recovery follow-up for #46

The maintainer approved this sequence in the current conversation: build deterministic corruption fixtures, stop corruption and reliably deliver subsequent traffic, trace the recovery chain, then retain permanent behavior tests or fix a demonstrated transport defect. The original randomized MITM test remains intact. Source parent is `16423f270ebcc3e0cb784b87033d6d73bcf49819` (audit-only on runtime `901f53e0fcc8e8e5ff004de4f84a78728a61a150`), feature worktree `/Volumes/worktrees/quic-go-fast/issue-46-recovery-fixtures`, branch `codex/issue-46-recovery-fixtures`.

## Agreed seam and oracle

Public `Transport.Listen`/`Transport.Dial`, a simulated `net.PacketConn` boundary, and a bidirectional stream exchange are the seams accepted by the maintainer's “go”. Use real TLS, packet processing and recovery with `testing/synctest`, a fixed 5 ms RTT and an unchanged one-second Dial budget. A 4096-byte exchange bounds the new fixture's transfer work; the original 500 KiB MITM assertions are not edited. No forced internal recovery state, mocks, shuffled-test seeds or replayed cryptographic bytes.

RFC 9002 sections [6.2.1](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.2.1), [6.2.2.1](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.2.2.1) and [6.2.4](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.2.4) provide the independent diagnostic oracle: PTO uses backoff, server sending may be amplification-limited before address validation, and an expired PTO requires an ack-eliciting probe in the corresponding space when sending is permitted. Do not equate a canceled timer, unavailable keys or a context deadline with an unjustified recovery stall. Permanent assertions check completion, transferred bytes and that the intended damage occurred; exact event timing/order remains evidence, not implementation-coupled assertions.

## Hypotheses and first four cases

| Hypothesis | Evidence supporting it | Evidence contradicting it |
| --- | --- | --- |
| H1: bounded authenticated-packet damage recovers normally | Receiver authentication rejection followed by probe submission, successful receipt, ACKs and complete handshake/transfer before deadline | Valid delivery opportunity before deadline without recovery progression |
| H2: recovery fails to schedule or submit usable handshake data | A due eligible PTO or outstanding CRYPTO data with no justified probe/submission | Correlated probe packets reach the socket and receiver, then progress |
| H3: receiver/TLS or fixture delivery fails after submission | Successful writes followed by missing reads, unexplained rejection of unchanged data, or no TLS progress after usable CRYPTO receipt | Read checksums and decoded packet/ACK events complete the chain |

C1: replace the final protected byte of the first server Initial packet with its XOR-1 value; forward all later traffic unmodified. Expect one authentication rejection and successful handshake/transfer through Initial recovery. C2: same selection, no byte mutation. Expect successful handshake/transfer without that authentication failure. Each is one focused execution, no race, macOS Go 1.27.0.

C3: replace the final protected byte in each of the first three server Handshake-bearing datagrams, locating the target QUIC packet within any coalesced datagram. Then deliver all traffic unchanged. Expect consecutive authentication failures followed by a usable Handshake recovery probe and completion before deadline. C4: same selection rule without byte mutation. Expect completion; successful TLS progress may mean fewer selected Handshake datagrams than in C3. Each is one focused execution in the same environment.

Selection and the virtual clock are controlled; TLS material, packetization and runnable-goroutine ordering can still differ. These are constructed counterfactuals, not historical replay. Simnet delivers via one endpoint path with fixed latency; it deliberately isolates recovery from the real MITM proxy's immediate replacement/source-address difference. Socket write/read, routed-datagram mutation, and endpoint qlog capture will identify this distinction explicitly. Stop and assess after these four executions. Prior usage is 4/4 random plus 2/8 focused; these four bring focused usage to 6/8. No random execution, full suite or CI rerun is scheduled. The maintainer also authorized useful budget extensions; any later case must be predeclared and counted.

## Decision after C1–C4

C1–C4 all pass. C1 recovers Initial damage at 205 ms, C3 recovers three Handshake mutations at 50 ms, and controls complete Dial at 5 ms. There is no demonstrated recovery obligation violation. Retain the four public-interface cases as permanent coverage and add the complementary deadline outcome without weakening the original randomized test.

C5 (predeclared, focused usage 7/8): corrupt every server Handshake packet selected by the same protected-byte rule until the one-second Dial context expires. Use a practically unbounded selection limit (`math.MaxInt32`) so the timer, not a count cutoff, ends the case. Expect repeated authentication failures and continued PTO/probe activity, `context.DeadlineExceeded`, no returned connection, and all transport/network goroutines shut down by fixture teardown. This validates cancellation when the network supplies no usable Handshake data; it is not evidence of a recovery defect. No successful transfer is expected in this separate failure-outcome test.

Final validation (predeclared): remove temporary qlog/socket/route tracing, compile with `-race`, and execute each of the five permanent cases once in one focused invocation. This simultaneously checks that the assertions do not depend on tracing and checks fixture synchronization/teardown. Count five case executions, bringing cumulative focused usage to 12 (four beyond the original eight, under the maintainer's explicit useful-extension authorization). No extra random run, full suite or CI run. Additional validation only if a concrete failure or review correction requires it, with a new predeclared purpose.
