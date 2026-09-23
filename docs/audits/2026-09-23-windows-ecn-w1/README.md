# W1 Windows ECN native feasibility receipt

**Disposition:** Feasible for separate IPv4-only and IPv6-only UDP sockets on the tested environment. This is example-level feasibility, not managed ECN qualification. Runtime Windows ECN remains unsupported. Scoped `$architecture-handoff` must define any implementation and qualification slice before production edits.

**Contract:** [W1](../../adr/2026-09-22-windows-managed-ecn-plan.md#slice-w1--decide-windows-native-ecn-feasibility), child [#507](https://github.com/the-sarge/quic-go-fast/issues/507), parent [#459](https://github.com/the-sarge/quic-go-fast/issues/459). Source and preservation tests use `4905bb52d83174a0a9133265a70c87d7dca6d323`. [Native observations and test summary](native.txt) are frozen evidence, not a maintained test product. The selected transcript normalizes line endings and trailing whitespace; it omits successful test subcases and shell progress noise.

## Environment and representation

Collection used one disposable Windows Server 2025 Standard Evaluation 24H2/amd64 KVM guest, exact version `10.0.26100.32230`, 4 vCPU and 8 GiB RAM, Go `go1.27.0`, and `golang.org/x/sys v0.48.0`. The minimax Linux `7.0.0-30-generic` host, Python 3.14.4, served as an independent UDP peer across a dedicated virtual Ethernet network. Windows had IPv4 `192.168.153.104` and a native IPv6 address on `fd71:507::/64`; the peer used `192.168.153.1` and `fd71:507::1`. No loopback-only or cross-compilation result establishes this disposition.

The Windows SDK `10.0.26100.0` declares `IP_RECVECN`, `IPV6_RECVECN`, `IP_ECN` and `IPV6_ECN` as 50. x/sys supplies `SetsockoptInt` and `WSACMSGHDR`; this x/sys revision does not export those ECN names, so the disposable probe used the SDK-verified numeric values. `*net.UDPConn.ReadMsgUDP` and `WriteMsgUDP` retained the standard Go Winsock/IOCP boundary. `SyscallConn.Control` configured only the probe's own disposable socket; no handle or public authority escaped it.

Receive enablement used option 50 at `IPPROTO_IP` (0) or `IPPROTO_IPV6` (41). The delivered control message used the same family level and type 50, with one native 32-bit `INT` ECN codepoint. On amd64 its header was 16 bytes, `cmsg_len` 20 bytes and padded control buffer 24 bytes, aligned to 8 bytes. Parsing checked header bounds, exact four-byte payload, a single matching message and values 0–3. The candidate representation owner is the native Winsock control-message API, whose layout is supplied by the SDK/x/sys, not a new general parser contract.

[Microsoft's ECN documentation](https://learn.microsoft.com/en-us/windows/win32/winsock/winsock-ecn) describes these messages and forbids application-generated CE. [Microsoft's receive-option helper reference](https://learn.microsoft.com/en-us/windows/win32/api/ws2tcpip/nf-ws2tcpip-wsasetrecvipecn) lists build 20348 as the API minimum. That documented minimum is not a tested support floor: **the evidence-backed boundary here is Windows build 26100.32230 / Go 1.27.0 / x/sys 0.48.0 only**. Older builds and runtimes require their own qualification if admitted by the later handoff.

## Finite probe and results

A small Go executable opened separate `udp4` and `udp6` sockets and exchanged identified JSON datagrams with the Python peer. The peer independently inspected outgoing Windows marks through Linux `recvmsg` with `IP_RECVTOS` or `IPV6_RECVTCLASS`, then replied with a requested ECN value through Linux `sendmsg`. Windows checked payload identity, source address/port, flags, ancillary bounds and the observed value. The Linux peer supplied CE because Windows correctly refuses to originate CE through `WSASendMsg`. The probe never requested packet info, USO or URO; those features do not establish this result.

| Probe class | IPv4 result | IPv6 result |
| --- | --- | --- |
| Receive | `IP_RECVECN=1` succeeds; 0, 2, 1, 3 delivered as Not-ECT, ECT(0), ECT(1), CE | `IPV6_RECVECN=1` succeeds; the same four values delivered |
| Send | Peer observes requested 0, 2, 1; requested CE=3 returns `WSAEINVAL` | Peer observes requested 0, 2, 1; requested CE=3 returns `WSAEINVAL` |
| Absent/malformed/no-option | Before enablement and after clearing the option, received OOB is absent; omitted send OOB yields peer-observed Not-ECT; value 4 and one-byte ECN payload return `WSAEINVAL` | Same observations |
| Close/cancellation | Starting a receive goroutine then closing the socket joins it with `net.ErrClosed` within the probe's four-second timeout | Same observation |

These are the six admitted classes: two family receive classes, two family send classes, one negative-behavior class and one lifecycle class. The close observation covers close/read completion; it does not claim a formally synchronized already-blocked syscall or a timing SLA. No restart, dual-stack, mapped-address, coalescing interaction, performance, fuzzing or platform matrix is claimed.

Both positive receive tables contain the four native values. One extraction-bypass mutation replaced the decoded value with absent (`-1`); the positive Not-ECT row failed with `got=-1 peer=0` despite a native type-50 control message. One marking-bypass mutation omitted outgoing OOB; the peer reported Not-ECT instead of the requested ECT(0), and the observation check failed. Each mutation was exercised once against the same representation seam. The extraction run's expected panic also stopped its PowerShell wrapper; the subsequent send mutation and preservation tests used a wrapper that captured the expected nonzero exit. These are discriminating examples, not a completeness proof of the temporary probe.

All successful probe `WriteMsgUDP` calls reported `n=0` with nil error, including the omitted-OOB case, while the independent peer received and echoed the complete payload. The first probe stopped on its initial assumption that success must report the payload length; the corrected characterization records both the native count and independent delivery. A later implementation must explicitly address send-result semantics at the accepted owner; it must not infer a failed send or silently enable ECN from this feasibility receipt.

## Preservation and retirement

`go test -count=1 -v -run '^TestWindows' .` passed natively at the source SHA above. This includes packet-info behavior, ECN remaining unsupported, USO/URO encoding and interaction, managed registration, fallback, wrapper lifecycle and handback. `TestWindowsExternalURONativeEngagement` and `TestWindowsManagedReceiveNativeHandback` skipped because their separately gated native-engagement fixture was not enabled. W1 claims no new USO/URO engagement result. No maintained Go characterization, production edit, dependency edit or verification infrastructure remains in the PR.

The disposable final probe source (including the two mutation switches) had SHA-256 `d484730ef492a8b400372ee2fb65afee972448b54eeb448d5f92be36de4694f3`; the Linux peer source had SHA-256 `3e6ed33587879343deb3c9108a1086152604aaed7e2715aaa181c318bdaa3a0f`. The positive table preceded the addition of those explicit mutation switches. Probe source/binaries and the exported source tree are retired after receipt capture; only this construction recipe and observations remain. [Cleanup receipt](cleanup.txt) confirms that the exact-owned guest, overlay, NVRAM, TPM state, network, remote source tree and probe files are absent. The retained base SHA-256 is `fd9928a795fed5dd8f10c41fb95cdc424b2de674ef749efe405956ee9e0a5e59`, matching its sealed image metadata. The [Linux peer observations](peer.jsonl) include setup and mutation traffic as well as the positive table. The original checkout remains untouched.

## Next boundary

The next action is scoped `$architecture-handoff`, not automatic implementation or a newly ready slice. It must decide the admitted Windows/runtime and family boundary, name the runtime enforcement owner, preserve the managed lease's private socket authority, account for native send byte counts, and define proportionate qualification of the accepted receive/send/lifecycle paths. IPv4-mapped dual-stack sockets, managed wrappers, offload interaction and older builds are unqualified here. These explicit limits do not reduce the current unsupported runtime behavior.
