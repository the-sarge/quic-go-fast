# W2 Windows native ECN qualification receipt

**Disposition:** The accepted native representation is confirmed on Windows 11 Pro 25H2 build **26200.8037** and Windows Server 2025 Standard Evaluation 24H2 build **26100.32230**, both amd64, with **Go 1.26.8 and 1.27.1** and **x/sys 0.48.0**. IPv4-only, IPv6-only, native IPv6 on a wildcard dual-stack socket and mapped IPv4 on that same socket preserve the required receive and outgoing marks. W3 is eligible to implement its existing contract. This PR changes no production behavior; managed Windows ECN remains false.

**Contract:** [W2](../../adr/2026-09-22-windows-managed-ecn-plan.md#slice-w2--qualify-windows-11-and-dual-stack-native-ecn), child [#537](https://github.com/the-sarge/quic-go-fast/issues/537), parent [#459](https://github.com/the-sarge/quic-go-fast/issues/459), accepted plan commit `4fcbded1e27feca73e87ba7cdfa9c927648a2cee`. The unchanged production baseline for preservation tests is `59143e0b98e5562539813f43abc52df646602b96`. This receipt adds bounded observations; it does not modify the frozen W1 evidence.

## Environment and evidence

The native Windows guests ran on minimax KVM with an independent Linux `7.0.0-30-generic` / Python `3.14.4` peer. Windows 11 used its existing licensed stable UUID/MAC and 12 vCPU / 48 GiB shape under the shared admission lock; Server 2025 used a disposable Evaluation guest with 4 vCPU / 8 GiB. Both used task-owned disk overlays, NVRAM and TPM copies. The dedicated network provided `192.168.154.0/24` and `fd71:537::/64`; the Linux peer bound `.1` / `::1`, Windows 11 used `.104`, and Server used `.105`. No shared guest, baseline disk, firmware or TPM state was modified.

- [Environment](environment.json): native OS identity, addresses, official Go archive hashes and disposable probe/peer source hashes.
- [Native rows](native.jsonl): each OS, runtime, socket binding/native family/V6ONLY, constructed send CMSG, raw send result, peer payload length/mark, received CMSG and assertion result.
- [Independent peer rows](peer.jsonl): complete Windows request payloads, Linux ancillary values and reply lengths, in collection order: Windows 11 positive/negative cells, two bounded bypass experiments, then Server cells. Repeated request identifiers across OS cells are distinguished by this order and the OS-tagged native rows; identifiers are not globally unique receipt IDs.
- [Discrimination](discrimination.txt): the two expected failing observations, not native product failures.
- [Preservation](preservation.txt): the unchanged `TestWindows` group on each OS with Go 1.27.1, including explicitly gated skips.
- [Cleanup](cleanup.txt): guest/network/process retirement, admission release and preserved baseline hashes.

These are frozen verification evidence and process metadata, not maintained tools or exhaustive platform conformance. Contract closure is not triggered: no runtime authority or lifecycle changes. There is no restart, stress, throughput, NIC, older-release or native ARM64 claim.

## Finite construction and commands

A disposable Go program used `net.ListenUDP` with `udp4` / `0.0.0.0:0`, `udp6` / `[::]:0`, and `udp` / `[::]:0`. It inspected `windows.Getsockname` and `IPV6_V6ONLY` under `SyscallConn.Control`: the IPv6-only socket returned 1 and the dual-stack socket returned 0. The same dual socket exchanged both IPv4 and IPv6 traffic. The incidental successful V6ONLY query on the IPv4 socket is recorded but is not used to identify its family.

Before positive cases, the probe and independent peer used four-second operation deadlines and checked the echoed request identifier, complete peer-observed payload length, outgoing mark, received mark and read flags. Each request specified a reply mark; Linux `recvmsg` independently observed the outgoing packet with `IP_RECVTOS` or `IPV6_RECVTCLASS`, and `sendmsg` returned the requested mark with `IP_TOS` or `IPV6_TCLASS`. Windows received with `ReadMsgUDP`. The peer originated CE; Windows did not forge CE reception locally.

Receive setup called `SetsockoptInt` with option 50 at level 0 for IPv4 and level 41 for IPv6, enabling both separately on the dual socket. Send setup built a native four-byte integer CMSG using `windows.WSACMSGHDR` size/alignment, then used `WriteMsgUDP`. The temporary decoder checked header/data bounds, exact four-byte values and duplicate ECN before interpreting a row. No packet-info option was enabled in this probe. Winsock/Go/x/sys own the representation; the probe makes example-level observations, not a claim to implement an open ancillary grammar.

The bounded commands inside each disposable guest were:

```powershell
# Verified official archives; versions and SHA-256 values are in environment.json.
$env:GOTOOLCHAIN = 'local'
Set-Location C:\qgf-w2\probe
& C:\qgf-w2\go1.26.8\go\bin\go.exe build -o C:\qgf-w2\probe-go1.26.8.exe .
& C:\qgf-w2\go1.27.1\go\bin\go.exe build -o C:\qgf-w2\probe-go1.27.1.exe .
& C:\qgf-w2\probe-go1.26.8.exe
$env:W2_NEGATIVE = '1'
& C:\qgf-w2\probe-go1.27.1.exe
Set-Location C:\qgf-w2\source
& C:\qgf-w2\go1.27.1\go\bin\go.exe test -count=1 -v -run '^TestWindows' .
```

Each OS/toolchain cell performed one pass: incoming 0, 2, 1, 3 and outgoing 0, 2, 1 on each of the four paths, plus one CE-send rejection. On Go 1.27.1 only, each OS also checked absent/cleared receive options on all paths, disabled one receive family at a time on the dual socket, and checked URO enabled/disabled on IPv4 with all four incoming marks. The complete campaign contains **112 main successful exchanges, 40 option/URO exchanges and four CE rejections**. All 152 successful exchanges matched; no required path skipped.

## Native representation and results

On these amd64 builds the header is 16 bytes, alignment is 8 bytes, `cmsg_len` is 20 bytes and the padded outgoing OOB buffer is 24 bytes. The data is a native little-endian 32-bit integer. The complete CMSG rows in `native.jsonl` tie each constructed outgoing value to the independent peer observation.

| Traffic path | Native socket | Receive level/type/length | Outgoing level/type/length | Values confirmed |
| --- | --- | --- | --- | --- |
| IPv4-only | AF_INET, wildcard IPv4 | 0 / 50 / 20 | 0 / 50 / 20 | Receive 0/2/1/3; send 0/2/1 |
| IPv6-only | AF_INET6, V6ONLY=1 | 41 / 50 / 20 | 41 / 50 / 20 | Receive 0/2/1/3; send 0/2/1 |
| Mapped IPv4 on dual-stack | AF_INET6, V6ONLY=0 | 0 / 50 / 20 | 0 / 50 / 20 | Receive 0/2/1/3; send 0/2/1 |
| Native IPv6 on dual-stack | Same AF_INET6 socket, V6ONLY=0 | 41 / 50 / 20 | 41 / 50 / 20 | Receive 0/2/1/3; send 0/2/1 |

All four OS/toolchain cells confirmed this table. In particular, mapped IPv4 uses the **IP-level** send CMSG even though the socket itself is AF_INET6; socket family alone is insufficient to choose the outgoing level. The separate receive options and native integer layout agree with the [Microsoft Winsock ECN contract](https://learn.microsoft.com/en-us/windows/win32/winsock/winsock-ecn).

Every successful Windows send reported **`n=0` with nil error**, with and without ECN OOB, while the peer received the entire submitted payload. OOB byte counts were 24 for constructed ECN and 0 without it. There was no differing send-count behavior between these toolchains requiring another Go-source investigation; the [W1 completion-path explanation](../2026-09-23-windows-ecn-w1/README.md) remains a bounded explanation, not a claim about every Winsock completion. Each CE request returned `WSAEINVAL`; its raw payload count happened to equal the submitted length despite the error, and is not success evidence. W3 must retain the native error and limit zero-count success normalization to its already accepted endpoint-owned boundary.

Before enabling and after clearing receive options, metadata was absent while ordinary payloads still arrived. Clearing IPv4 on the dual socket suppressed only IPv4 ECN; IPv6 remained marked. Clearing IPv6 suppressed only IPv6 ECN; mapped IPv4 remained marked. Each row then restored or cleared the relevant options within the task-owned socket.

`UDP_RECV_MAX_COALESCED_SIZE=65535` and then `0` both succeeded on each OS. ECN survived both settings for all four incoming values. No `UDP_COALESCED_INFO` appeared in these request/reply exchanges, so this is option coexistence evidence and **not hardware coalescing engagement**. The [Microsoft URO rules](https://learn.microsoft.com/en-us/windows-hardware/drivers/network/udp-rsc-offload) require matching ECN within coalesced segments; actual managed segment propagation remains W3's accepted test obligation.

## Discrimination, preservation and retirement

One decoder bypass on Windows 11/Go 1.27.1 replaced the decoded mark with absent: the first Not-ECT row failed despite its native value-0 CMSG. One encoder bypass omitted outgoing OOB: the ECT(0) row failed because the peer observed Not-ECT. Both exited 2. These are the only two bypass experiments; they demonstrate that the new disposable probe checks the observations rather than option success.

The unchanged `TestWindows` group passed once per OS on Go 1.27.1. `TestWindowsExternalURONativeEngagement` and `TestWindowsManagedReceiveNativeHandback` retained their explicit opt-in skips. The native ECN matrix itself had no skips. This preserves the existing unsupported ECN behavior and Windows packet-info, USO/URO, managed registration and lifecycle tests without adding repository Go code.

Probe source/binaries, exported source trees, toolchain copies, guests, overlays, copied NVRAM/TPM state, peer/HTTP processes and the dedicated network were retired after capture. Source fingerprints remain in `environment.json`; rebuilding a probe follows the bounded construction above, not a maintained harness contract. The initial boot attempt placed a TPM copy outside the host's existing permitted qualification directory; moving that task-owned copy to the existing approved run directory resolved access without changing host policy. The earlier administrative access stop was superseded; its chronology remains in the child issue, outside the normative contract.

W3 receives this confirmed representation and the unchanged accepted managed-endpoint boundary. It still owns direct/checked/batch routing, wrapper correlation, complete datagrams, URO segment metadata, lease reuse, terminal cleanup and native result normalization. No W3 implementation or automatic dispatch is included here.
