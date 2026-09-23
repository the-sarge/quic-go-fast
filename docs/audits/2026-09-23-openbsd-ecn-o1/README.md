# OpenBSD O1 native feasibility receipt

## Disposition

O1 is complete: **IPv6-only native ECN is feasible in the tested environment; IPv4 receive ECN is unsupported through the tested Go/x/sys UDP ancillary surface.** No managed ECN capability is introduced. The OpenBSD track awaits scoped `$architecture-handoff` before any IPv6 implementation/qualification slice can be dispatched. No successor is currently defined or ready.

This is example-level evidence from one OpenBSD 7.9 `GENERIC.MP#449` amd64 guest with Go 1.27.0, `golang.org/x/sys v0.48.0` and `golang.org/x/net v0.59.0`. It does not qualify the managed adapter, another kernel, architecture, network path or workload.

## Construction and identity

The disposable probe used separate `net.ListenUDP` loopback sender/receiver sockets for `udp4` and `udp6`, two-byte payloads, two-second read deadlines, sender-address checks, `MSG_CTRUNC` rejection and native `ReadMsgUDP` / `WriteMsgUDP`. `SyscallConn.Control` applied socket options privately. `unix.ParseSocketControlMessage` owned control-message framing; `ipv6.ControlMessage.Parse` decoded IPv6 traffic class. Outgoing messages used the native `unix.Cmsghdr`, `CmsgLen`, `CmsgSpace` and native-endian four-byte integer representation. There was no handwritten ancillary parser.

The retired [probe source and driver](https://github.com/the-sarge/quic-go-fast/tree/4aa448eaf141477f693306dcc8abdf39d05e5e47/o1probe) belong to experiment commit `4aa448eaf141477f693306dcc8abdf39d05e5e47`, based on product head `c9b820c61eea586750aae282c6acd796c1f55f30`. They are absent from the delivered tree and are not maintained tests or infrastructure. The source is historical construction evidence only. This PR changes no production Go file, dependency or runtime owner.

The run used the existing infra `test-vm:openbsd-amd64` candidate-driver path, controlled from `m4mini` and executed on minimax's disposable KVM guest. Infra controller commit: `9b3526628c4d4dacaa96fe560d5511e7b943ad47`. Run: `run-20260923T001048.801788000Z`, workload interval `2026-09-23T00:11:32Z`–`00:11:47Z`. Invocation selected the exact experiment commit, `WORKLOAD=candidate-driver`, `DRIVER_PATH=o1probe/run.sh` and `RESULT_NAME=o1.txt`, with `GOTOOLCHAIN=local` selecting the target's required controller Go 1.27.0.

The [native transcript](native-output.txt) is 12,778 bytes, SHA-256 `a743512d39f7fc1468693019783f5cd50d5dd0c1057750e4e8e6f81faf76e292`. The [infra receipt](receipt.json) binds this result to the exact candidate, driver, authenticated dependency bundle and guest/lifecycle facts. Its `pass` means the characterization and cleanup succeeded; it is not a platform-capability verdict.

## Six bounded probe classes

| Class | API and socket-option result | Native observation | Disposition |
| --- | --- | --- | --- |
| IPv4 receive | Native compilation of `unix.IP_RECVTOS` fails as undefined; installed `netinet/in.h` has no receive-TOS option. `IP_RECVDSTADDR` and `IP_RECVTTL` enable successfully. Socket-wide `IP_TOS` accepts 0, 2, 1, 3. | Four datagrams arrive with destination-address and TTL ancillary data, without TOS. Peer ECN is unobservable through this surface. | Unsupported receive boundary; successful socket-wide setup is not ECN metadata evidence. |
| IPv6 receive | `IPV6_RECVTCLASS` (57) enables successfully; sender socket-wide `IPV6_TCLASS` generates four representative inputs. | `IPPROTO_IPV6` (41), `IPV6_TCLASS` (61), four-byte native integer payloads decode to 0, 2, 1, 3: Not-ECT, ECT(0), ECT(1), CE. | Feasible native receive representation for `udp6`. |
| IPv4 send | `WriteMsgUDP` accepts a native `IPPROTO_IP` / `IP_TOS` integer control message for each of 0, 2, 1, 3. | Payloads arrive, but no peer TOS measurement is available through the admitted receive API. | Outgoing marks **unproven**; neither successful send nor absent peer metadata establishes whether the kernel applied or ignored the control message. |
| IPv6 send | `WriteMsgUDP` accepts explicit `IPV6_TCLASS` integer control messages, including zero. | The peer independently observes 0, 2, 1, 3 despite sender socket default `0x20`. Explicit Not-ECT therefore overrides a nonzero default. | Feasible native per-datagram marking for `udp6`. |
| Absent/malformed/no-option | Without receive-TCLASS enabled, an ECT(0) datagram supplies no ancillary value. With it enabled, an unmarked/default socket delivers an explicit zero. | An oversized `cmsg_len` is rejected by `unix.ParseSocketControlMessage` and by the kernel send path with `EINVAL`. | Absence is distinct from Not-ECT; one malformed framing case is covered, with no general malformed-input claim. |
| Close/cleanup | Closed `udp4` and `udp6` senders reject writes; test cleanup closes remaining sockets. | Infra records guest shutdown, cleanup, restored powered-off baseline, lock release and deletion of Host staging/evidence. | Disposable state retired; no persistent socket setting or runtime seam. |

The normal probe completed all six classes. Before that run, bypassing receive parsing made all four IPv6 receive assertions fail; omitting outgoing control messages made all four send assertions observe the socket default 32 and fail. Restoring those operations passed. These are the two discrimination checks required by O1, not a broader mutation or stress campaign.

## Representation and limits

The candidate representation owner is OpenBSD's kernel control-message ABI exposed by Go/x/sys, with x/net's IPv6 parser. The native IPv6 representation is level 41/type 61 with a four-byte native-endian integer; the captured amd64 messages have `cmsg_len=20` and 24 bytes of padded storage. Zero must be explicitly encoded when overriding a socket default: x/net's outgoing `ControlMessage.Marshal` omits a traffic-class field whose value is zero, so a future implementation must account for that API behavior rather than equating omitted data with Not-ECT.

The [OpenBSD 7.9 IPv6 manual](https://man.openbsd.org/OpenBSD-7.9/ip6.4) documents the receive traffic-class integer and states that IPv6 sockets are IPv6-only. The [IPv4 manual](https://man.openbsd.org/OpenBSD-7.9/ip.4) documents socket-wide TOS and receive destination/TTL options. Native header, compiler and datagram observations above determine this receipt's disposition; documentation alone is not delivery evidence.

IPv4 cannot qualify both directions through this surface. IPv6 is a feasibility result only: the managed endpoint, wrapper forwarding, ordinary/batch sends, lease reuse, filtering, disabled fallback and public compatibility still require an accepted implementation contract and native qualification. Dual-stack/mapped behavior, raw/BPF capture, privileged packet injection, concurrent socket-wide marking, throughput, fuzzing, repeated kernels and additional hosts were not exercised or authorized. No universal guarantee or new public/raw authority is claimed.

## Operational history and retirement

Before guest execution, one invocation was rejected for an abbreviated candidate SHA. A subsequent attempt (`run-20260923T000904.882552000Z`) failed controller dependency preparation because the infra module automatically selected Go 1.27.1 instead of its VM target's required 1.27.0. Its lifecycle fact recorded shutdown/restoration and no workload fact. The retained admission lease rejected the next launch; the documented target cleanup command then restored the baseline and released the run state. Selecting `GOTOOLCHAIN=local` resolved the identified infrastructure failure. Only the successful run above executed the six-class native probe; these admission failures carry no ECN evidence.

The local probe executable and the remote controller's dedicated candidate worktree were removed after capture. Probe source/driver are removed from the final product tree; only this concise receipt, the immutable transcript and infra receipt remain. They are process/traceability evidence, not blocking maintained verification aids. Contract closure remains not triggered. O1's finite evidence budget is exhausted and complete; no additional native run is required for these documentation changes.
