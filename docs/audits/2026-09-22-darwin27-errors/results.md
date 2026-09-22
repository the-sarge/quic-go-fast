# Native Darwin 25 / 27 transient-error comparison

All 17 declared observations passed on each host on the first collection. Darwin 25 and Darwin 27 produced identical syscall returns, errno values, exact received payloads and ECN metadata in every paired observation. No behavioral difference was observed. These are direct native measurements; no syscall return was injected and no kernel identity was spoofed.

## Reproducibility

The [protocol](protocol.md) was committed at `22be9e1f2113d716bc428adba44c611197e3271a` before collection. The [probe](probe.c) was committed at `3a94a54793c2ce3700088359bb341bee55f7af3b`; its SHA-256 is `7bbab78fba9fe8df99f4341e910742e361b3b1efe11baad15f52be96f7bad5f8` on both hosts. The [collector](collect.py) was committed at `28c0eef4` before execution. The common repository base is `16c130fbd70f2e232a19f53fbb6a5873cdd57d59`. No product files changed.

Each frozen commands record contains the exact compile/run arguments, start timestamp, duration, stdout, stderr and exit status. All 19 commands per host (identity, compilation and 17 observations) exited zero. No cases skipped, timed out or required correction/repetition. The collector records the probe candidate commit separately from its own later commit. The same committed C source was compiled natively on each host.

| Host | Product/build | Darwin / XNU | Architecture | Kernel UUID | Compiler / SDK |
| --- | --- | --- | --- | --- | --- |
| Baseline `m4mini` | macOS 26.6.2 / 25G83 | 25.6.0 / 12377.161.14~5 | arm64 | `3F7964F9-C5A6-39D4-942D-02750C0980E0` | Apple clang 21.0.0 (2100.1.1.101) / 26.5 |
| Local target | macOS 27.0 / 26A428 | 27.0.0 / 13432.1.9~1 | arm64 | `D46FC75B-A889-3796-A66C-D28FDD50FB68` | Apple clang 21.0.0 (2100.3.34.2) / 27.0 |

[Darwin 25 commands and outputs](darwin25/commands.json), [Darwin 25 source receipt](darwin25/source.json), [Darwin 27 commands and outputs](darwin27/commands.json), [Darwin 27 source receipt](darwin27/source.json).

## Observations

`single` is ordinary `sendmsg` applied to the failing element on a fresh socket. `batch-0` submits only that element through syscall 481. `batch-1` submits a small valid prefix followed by the failing element. The positive controls establish valid ancillary data and exact ECT(0) delivery. All delivered packets were the exact 21-byte prefix, and all delivered UDP packets had traffic-class bits 2 (ECT(0)); no suffix was received within the declared 250 ms drain window.

| Case | Native return on both hosts | Peer observation on both hosts | Comparison |
| --- | --- | --- | --- |
| `ipv4-positive` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |
| `ipv4-EAGAIN-single` | `result=-1 errno=35 errno_text=Resource temporarily unavailable` | `received=0 bad_packets=0` | Identical |
| `ipv4-EAGAIN-batch-0` | `result=-1 errno=35 errno_text=Resource temporarily unavailable` | `received=0 bad_packets=0` | Identical |
| `ipv4-EAGAIN-batch-1` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |
| `ipv4-EINTR-single` | `result=-1 errno=4 errno_text=Interrupted system call` | `received=0 bad_packets=0` | Identical |
| `ipv4-EINTR-batch-0` | `result=-1 errno=4 errno_text=Interrupted system call` | `received=0 bad_packets=0` | Identical |
| `ipv4-EINTR-batch-1` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |
| `ipv6-positive` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |
| `ipv6-EAGAIN-single` | `result=-1 errno=35 errno_text=Resource temporarily unavailable` | `received=0 bad_packets=0` | Identical |
| `ipv6-EAGAIN-batch-0` | `result=-1 errno=35 errno_text=Resource temporarily unavailable` | `received=0 bad_packets=0` | Identical |
| `ipv6-EAGAIN-batch-1` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |
| `ipv6-EINTR-single` | `result=-1 errno=4 errno_text=Interrupted system call` | `received=0 bad_packets=0` | Identical |
| `ipv6-EINTR-batch-0` | `result=-1 errno=4 errno_text=Interrupted system call` | `received=0 bad_packets=0` | Identical |
| `ipv6-EINTR-batch-1` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |
| `unix-ENOBUFS-single` | `result=-1 errno=55 errno_text=No buffer space available` | `received=0 bad_packets=0` | Identical |
| `unix-ENOBUFS-batch-0` | `result=-1 errno=55 errno_text=No buffer space available` | `received=0 bad_packets=0` | Identical |
| `unix-ENOBUFS-batch-1` | `result=1 errno=0 errno_text=Undefined error: 0` | `received=1 bad_packets=0` | Identical |

UDP send capacity was 4096 bytes and the failing payload was exactly 4096 bytes plus 16 bytes of valid ancillary data. The nonblocking sends produced native EAGAIN (35). Blocking sends were interrupted by one thread-directed signal without SA_RESTART; all returned in approximately 102–111 ms with the signal observed before syscall return. Zero-progress calls exposed EINTR (4); prefix calls returned count 1 and suppressed the error. The two-second send timeout was not reached.

The Unix-datagram receiver capacity was 1024 bytes, the failing payload was 1280 bytes and sender capacity was 16384 bytes. Ordinary sends and zero-progress batches exposed native ENOBUFS (55); a batch with an accepted prefix returned count 1 and suppressed the error.

## Interpretation and limits

The native evidence directly supports the measured IPv4/IPv6 UDP EAGAIN and EINTR zero-progress and accepted-prefix behavior on both builds. It supplements the original [qualification evidence](../2026-09-22-darwin27/results.md), whose full/partial acceptance, EMSGSIZE, integration, fallback and architecture/build results remain frozen and unchanged. The first qualification should have attempted these deterministic transient-error probes; lack of target source did not make the native experiment impossible.

The ENOBUFS observation is an AF_UNIX datagram control through the real `sendmsg_x` syscall. It is not a native UDP ENOBUFS observation. Older public XNU shares a generic per-message loop and error epilogue across these unconnected socket types, but that older source alone does not establish the target binary's ownership. Safe bounded UDP loopback constructions found for buffer-space shortage return EAGAIN or EMSGSIZE; allocation-failure ENOBUFS would require resource pressure outside this protocol. No such pressure was applied.

This record makes no claim that all Darwin 27 behavior is identical to Darwin 25. Production policy remains unchanged while the remaining ENOBUFS ownership/zero-progress assumption is evaluated against the qualification contract.
