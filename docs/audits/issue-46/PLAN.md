# Issue #46 diagnostic execution plan

Pinned source: `901f53e0fcc8e8e5ff004de4f84a78728a61a150`. This plan precedes all diagnostic executions. The accepted brief is retained in `raw/issue.json`. No production fix, assertion relaxation, full-suite execution, CI rerun, or merge is authorized.

| Hypothesis | Supporting evidence to seek | Contradicting evidence to seek |
| --- | --- | --- |
| H1: selected byte replacements remove enough handshake progress to exceed the unchanged deadline | Datagram checksums and packet boundaries associate replacements with authentication/header drops; PTOs continue, with next recovery after context expiry | Usable handshake traffic is received and acknowledged but required recovery or TLS progress stops |
| H2: replacement writes or proxy delivery fail | Short/failed direct writes, or forwarded datagrams absent from receiver traces; account for bypassed delay and source address | Full replacement writes and matching receiver datagram checksums; normally forwarded handshake data received |
| H3: transport recovery fails independently of expected corrupt-packet rejection | Outstanding handshake data with no corresponding recovery activity, or successful retransmission received without handshake progress | Expected PTO/probe and ACK progression, followed by completed handshake when usable packets arrive |

Run random cases individually, stopping globally at the first matching Dial timeout: M1 then (if needed) M2 on macOS arm64 Go 1.27.0 without race, scale 1; L1 then (if needed) L2 on existing minimax Linux amd64 Go 1.27.1 with race, GSO disabled, scale 3. Each tests H1/H2/H3 by retaining all callback decisions, replacement details and write outcomes, endpoint qlog events and explicit Dial start/deadline/result. No extra random draws are introduced. No environment allocation is transferred. The existing Lima guest has no Go on PATH; minimax is available without provisioning.

At most eight additional focused executions may be used. Each will be declared in the execution ledger before invocation and must use a fixed schedule, deterministic mechanism, or matched control. No fresh random selections in that allocation. Stop as soon as a supported explanation and next action are available; unused allocation is not a reason to run more cases.

Temporary instrumentation will be retained as an unapplied patch, with no changes left in active Go sources. It uses in-memory event retention and existing qlog encoders, then writes at cleanup. Observer effects include allocation, clocks, checksum calculation, serialization, mutex contention and cleanup I/O. Instrumented timing is not equivalent to historical timing. Callback order is datagram observation order, not QUIC packet numbering. No shuffle seed reproduces global math/rand/v2 choices or live encrypted datagrams.
