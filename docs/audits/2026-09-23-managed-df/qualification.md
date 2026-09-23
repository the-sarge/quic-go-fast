# Managed DF native qualification

The [approved contract](../../agents/managed-df-353.md) owns scope and acceptance. These are example-level native observations for the product PR, not platform conformance or a performance claim. No shared routes, interfaces, guest disks, or host socket policy were changed.

## Current qualification

| Platform | Native environment | IPv4-only | IPv6-only | Dual/native IPv6 | Dual/mapped IPv4 | Disposition |
| --- | --- | --- | --- | --- | --- | --- |
| macOS | macOS 27.0 build 26A428, arm64, Go 1.27.1; lo0 MTU 16384 | Passed | Passed | Passed | Passed | Enable managed DF after per-socket setup; preserve existing macOS 11 minimum and macOS 15 dual-stack boundary |
| Linux | Linux 7.0.0-30-generic, amd64, Go 1.27.1 test binaries; temporary network namespace, loopback MTU 1500 | Passed | Passed | Passed | Passed | Enable managed DF after per-socket setup for all admitted families |
| Windows | Source inspection only; no native DF lifecycle run | Unqualified | Unqualified | Unqualified | Unqualified | Managed DF remains false |
| Other platforms | Outside this native qualification | Unqualified | Unqualified | Unqualified | Unqualified | Managed DF remains false |

Each passing lane includes native option engagement, a valid UDP datagram rejected specifically with the platform message-size error during DF, delivery before setup and after release, and a later lease repeating setup/restoration. Payloads were 20000 bytes on macOS and 2000 bytes on Linux, below UDP's payload ceiling and above the known interface MTU. Adequate socket buffers prevent a socket-buffer limit from masquerading as DF evidence. Native batching reports the existing zero-progress fallback result on the oversized send, followed by successful small sends. The same-socket before/during/after sequence distinguishes option enforcement from a generic maximum UDP length failure.

Real QUIC integration covers the same four family lanes through two successive managed leases. With a 1232-byte initial packet size, observed MTU updates increase above 1232 (the Linux native run recorded 1342, with a later 1397 update in one lane). Discovery also works with ECN explicitly disabled. The 1200-byte TURN-profile case completes traffic without MTU growth. Oversized public DATAGRAM submissions remain rejected. Existing selected-peer, wrapper, batch, receive-normalization and endpoint tests provide preservation evidence; no wiremux code or live TURN deployment was changed.

## Native option findings

Linux's existing ordinary helper sets `IP_MTU_DISCOVER=IP_PMTUDISC_PROBE` and `IPV6_MTU_DISCOVER=IPV6_PMTUDISC_PROBE`, treating either successful family as sufficient. The managed helper instead inspects the native family and `IPV6_V6ONLY`, snapshots every admitted family's option, and qualifies only after each succeeds. Native baseline values were 1 and returned to 1 after release. See [Linux socket-option documentation](https://man7.org/linux/man-pages/man2/IP_MTU_DISCOVER.2const.html).

Darwin's existing ordinary helper uses `IP_DONTFRAG` for IPv4 and `IPV6_DONTFRAG` for IPv6 and dual-stack sockets. The latter controls mapped IPv4 too. The managed helper retains the existing version gates, snapshots the applicable option before mutation, and restores it after joining I/O. Native baseline values were 0 and returned to 0 after release. The ordinary helper remains unchanged.

Windows's existing helper sets `IP_DONTFRAGMENT` / `IPV6_DONTFRAG` and accepts either family's success. Winsock documents readable/writable fragmentation controls, but that is not evidence for the managed lifecycle, both dual-stack paths or actual QUIC discovery. Prior ECN qualification is not DF qualification. No native Windows qualification guest was running during this work; the product includes no Windows DF setup and makes no Windows support claim. See [Winsock options](https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options).

Restoring socket options does not rewind kernel route/PMTU caches or guarantee identical error timing across arbitrary networks. The accepted evidence demonstrates ordinary same-endpoint reuse in these native cases. No broader restoration claim is made.

## Reproduction

On macOS, the native tests use the existing loopback path. Confirm its MTU before qualification:

```sh
ifconfig lo0
go test -count=1 -v -run '^TestManagedDF' .
go test -count=1 -v -run '^TestManagedPathMTUDiscovery$' ./integrationtests/self
```

On Linux, compile the root and self-test binaries for the native host and run them in one disposable network namespace. Namespace exit retires the interface configuration. `QUIC_TEST_DF_PAYLOAD` is a test-only input identifying a payload above this controlled MTU; a normal Linux unit run skips that opt-in native test rather than claiming qualification.

```sh
sudo unshare --net sh -c 'ip link set lo up; ip link set lo mtu 1500; QUIC_TEST_DF_PAYLOAD=2000 ./quic-managed-df-353.test -test.run ^TestManagedDF -test.v -test.count=1 && ./quic-managed-df-353-self.test -test.run ^TestManagedPathMTUDiscovery$ -test.v -test.count=1 -test.timeout=2m'
```

Local certification and hosted exact-head receipts belong to the product PR. Review chronology and dispositions stay in the PR discussion, not this contract or the task-manager mirror.
