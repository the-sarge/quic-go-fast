# R01-W Windows managed receive qualification

The [R01-W contract](../adr/2026-09-15-external-packet-io-plan.md#r01-w--windows-managed-coalesced-normalization) is qualified on the existing Windows Server 2025 / Linux KVM venue. This is bounded correctness and engagement evidence for managed handback; it makes no throughput, CPU, physical-network, raw-socket-return or additional OS/architecture claim.

## Candidate and venue

- Source: `df67ca88d0e2e33529dc7882b3ae91f8e28d80a5`; clean dedicated worktree. Binary built with installed Go `go1.27.1 darwin/arm64`, `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c`.
- Test binary SHA-256, independently checked on Linux and Windows: `9d6bf20d7c8bb18be65ddbc6381d49bbc9bb6ecfbdab2a6769cc9503d861bb78`.
- Receiver: Windows Server 2025 Standard Evaluation, build `10.0.26100`, amd64, 4 vCPU, 8 GiB. Disposable `r01-w-native-20260916` overlay of minimax's sealed GARM Windows Server 2025 v2 image; unique device state and the existing virtual NIC. Controller and host used clean infra revision `94457792453e71830154625da85ef3488f4cfce5` under the existing exclusive host admission.
- Sender: minimax Linux, `192.168.122.1:8515`; receiver `192.168.122.152`. Traffic crossed the guest NIC. The [Q04 sender](q04-windows-uro/native-sender.py) was reused unchanged in payload/segmentation behavior, with only its temporary HTTP root and ports adapted for this run.
- Collection: 2026-09-16. Windows first-boot module installation completed normally. Launcher setup required quoting PowerShell arguments and staging the repository's public test certificates at the compiled fixture path before the full suite could run; no product correction or native repetition campaign resulted.

## Result

`TestWindowsManagedReceiveNativeHandback` registered the factory lease, joined the transport reader, then requested the existing Q04 burst through the still-active lease. Linux submitted 38,692 bytes as 31 × 1232-byte datagrams plus a 500-byte tail, each filled with its one-based index. The read-only Winsock `FIONREAD` query observed **38,692 queued bytes before lease Close**. The test then returned the lease, read the first datagram through the ordinary endpoint and read the remaining 31 through its next lease. Every source address, payload and datagram length matched. The retained Windows decoder consumed **one native coalesced read** across that handback.

`FIONREAD` is used only to observe queued data, following the [Winsock IOCTL contract](https://learn.microsoft.com/en-us/windows/win32/winsock/winsock-ioctls). It does not decode metadata or establish a general raw-socket return protocol. The existing Windows decoder remains the only metadata interpreter.

The same binary passed managed registration, disabled 60,000-byte datagram fallback, ordinary sibling retention, consumed QUIC sibling disposal, logical deadlines, source-address detachment, truncated control rejection, bounded retained storage, IPv4/IPv6 wrapper filtering, cancellation during lease return and parent Close. Existing managed endpoint generation, join, restoration-failure and QUIC handshake regressions also passed, as did the selected external Windows and URO/USO preservation tests. [Raw output](r01-w-managed-receive/native-tests.log) records the complete successful suite; the external Q04 engagement test intentionally skipped because this run assigned the peer to R01-W instead.

The guest invocation used `GITHUB_ACTIONS=true`, `QUIC_GO_R01_W_URO_PEER=192.168.122.1:8515`, and `quic.test.exe '-test.run=TestWindowsManaged|TestManagedEndpoint|TestManagedPacketIO|TestWindowsExternal|TestWindowsURO|TestWindowsUSO' '-test.v' '-test.timeout=90s'`. Without the peer variable the R01-W native test skips; hosted CI cannot substitute that skip for this receipt.

## Ownership and scope

Runtime setup is shipped behavior and receive/lifecycle guards are required safety enforcement. Tests are verification aids; this receipt is process metadata. There is no new maintained verification framework. The existing receive decoder, coalesced delivery/storage owner and endpoint generation/join guards are reused, with no second parser, receive queue or retry owner. Ordinary fallback on other platforms is unchanged.

After collection, guarded runner deletion and absence checks removed the disposable domain, overlay and reservation, and the exclusive host admission was released. The sealed image and other tasks' checkouts were untouched. Review and exact-head certification receipts belong to PR discussion, separately from this native source identity.
