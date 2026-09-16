# Q04 external Windows URO qualification

The [Q04 contract](../adr/2026-09-15-external-packet-io-plan.md#q04--enable-permissioned-windows-coalesced-receive) is qualified on the existing Windows Server 2025 / Linux KVM venue. This is one bounded correctness and engagement regression through an explicitly registered wrapper; it makes no throughput, CPU, physical-network or additional OS/architecture claim.

## Candidate and venue

- Source: `8cdc0399cf2a19c93d2989deb95656cdcf2443b3`, clean worktree; Go `go1.27.1 darwin/arm64` cross-compilation with `GOOS=windows GOARCH=amd64 go test -c`.
- Test binary SHA-256, independently read back in Windows: `ecfdf965b153bee1ba4be71d2dee5c5d269b7f2840dbee94795192db42c8ce04`.
- Receiver: Windows Server 2025 Standard Evaluation, build `10.0.26100.0`, amd64, 4 vCPU, 8 GiB; disposable overlay of minimax's sealed GARM Windows Server 2025 v2 golden, with unique device state and the existing `e1000e` virtual NIC.
- Sender: the minimax Linux host, bound to the libvirt bridge `192.168.122.1`; receiver `192.168.122.156`. The datagrams cross the guest NIC. This is not same-host Windows loopback evidence.
- Collection: 2026-09-16 13:53 UTC. The first guest boot stalled before display/agent readiness; one cold restart recovered it before any test collection. The qualification ran once, passed, and stopped. Guarded runner deletion verified the domain, overlay and reservation absent, then the exclusive host admission was released. The golden image was never booted or modified.

## Test and result

`TestWindowsExternalURONativeEngagement` registered receive permission on the selected-peer message-I/O wrapper, initialized the actual transport and sent the `Q04 URO` trigger. The [one-shot sender](q04-windows-uro/native-sender.py) replied from the same socket with one Linux UDP-segmented submission: 31 × 1232-byte datagrams and a final 500-byte datagram, each filled with its one-based index. Windows observed **one coalesced read through the wrapper and delivered all 32 exact datagrams**. Source addresses and every payload were checked. The sender reported 38,692 bytes submitted. No performance comparison or repetition campaign was run.

The same binary also passed the permission/disabled/opaque table, IPv4/IPv6 selected-peer wrapper and cancellation regressions, setup failure after mutation, partial-read error with no replay, invalid segment/control/truncation table including wrapped Winsock `WSAEMSGSIZE`, existing URO splitting/packet-info/pending-view tests, the four USO/URO combinations, and external USO ordinary/registered/disabled/unavailable cases. [Raw output](q04-windows-uro/native-tests.log) includes intentional malformed-fixture warnings; every selected test passed. Hosted Windows loopback is ordinary functional evidence only, even when it happens to coalesce on this virtual-NIC surface.

Invocation in the guest used `GITHUB_ACTIONS=true`, `QUIC_GO_Q04_URO_PEER=192.168.122.1:8505`, and `q04.test.exe -test.run 'TestWindowsExternal|TestWindowsURO|TestWindowsUSOUROInteractionMatrix' -test.v -test.timeout=60s`. The test defaults to a wildcard IPv4 bind; `QUIC_GO_Q04_URO_LOCAL` can specify that bind explicitly. Without the peer environment variable the engagement test skips, so a hosted skip never supplies this receipt.

## Evidence reuse and scope

The [W3 adoption receipt](2026-09-11-w3-uro-results.md) retains its original performance conclusions. Comparing its recorded `6b480b91` source with Q04's base shows the already-landed shared coalesced delivery holder and Q02's independent read-only USO probe; Q04 changes permission admission and receive validation, not the kernel I/O algorithm. Existing shared coalesced storage and mixed-ID routing tests remain the evidence for unchanged ownership paths. This receipt does not qualify managed handback, raw reuse, another Windows release, or assembled wiremux performance.

The runtime artifacts are shipped behavior and safety enforcement; tests and the frozen sender are verification aids; this receipt and raw output are process metadata. No new maintained measurement framework or recursive completeness obligation is introduced. The PR discussion contains TDD and later review/certification history; those receipts are separate from the current normative contract.
