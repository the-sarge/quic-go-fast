# D1 managed Darwin ECN qualification

## Disposition and boundary

Supported on the natively qualified m4mini host: macOS 26.6.2 build 25G83, Darwin 25.6.0 arm64, Go 1.27.0. The factory-created endpoint and current lease retain native authority; synchronous exact-forwarding wrappers use the existing checked callback and shared L1 correlation/singleton adapter. Public signatures, ordinary datagrams, unrelated managed capabilities and sendmsg_x qualification/progress policy remain unchanged. Darwin 27 sendmsg_x qualification remains separate in [#516](https://github.com/the-sarge/quic-go-fast/issues/516).

The universal guarantee is bounded to the plan's typed endpoint/lease and forwarding domain and native parser-owned messages; selected-peer wrapper evidence is example-level. No external-grammar scanner, new adapter owner, transitional seam or untraced effect was introduced. Runtime setup is shipped behavior and required capability enforcement; tests are verification aids without a maintained-product exception; this receipt and completion/frontier state are metadata.

## Native family matrix

| Socket/traffic | Observed admission | Required receive option | Receive evidence | Marked send evidence |
| --- | --- | --- | --- | --- |
| udp4 / IPv4 | AF_INET, IPv4 only | IP_RECVTOS | Not-ECT, ECT(0), ECT(1), CE exact | Ordinary, checked singleton, registered native batch and disabled-batch fallback exact |
| udp6 / IPv6 | AF_INET6, IPV6_V6ONLY=1 | IPV6_RECVTCLASS | All four values exact | Same routes exact |
| udp / mapped IPv4 | AF_INET6, IPV6_V6ONLY=0, mapped traffic available | IPV6_RECVTCLASS supplies IPV6_TCLASS; IP_RECVTOS returns EINVAL | All four values exact | Same routes exact |
| udp / IPv6 | AF_INET6, IPV6_V6ONLY=0 | IPV6_RECVTCLASS | All four values exact | Same routes exact |

Required-option failure disables capability for every affected admitted family. An optional-only ECN failure after successful descriptor access and required packet-info setup leaves ordinary I/O usable; descriptor and required packet-info failures remain fatal. The shared setup receipt reports this distinction without changing Linux or unregistered error policy.

## Finite coverage receipt

| Accepted semantic class | Enforcement owner | Evidence | Disposition |
| --- | --- | --- | --- |
| Four receive marks and family admission | Native OOB parser, endpoint family policy, shared adapter | TestDarwinManagedECNReceive; TestDarwinManagedECNFamilyGate | Covered |
| Ordinary, checked and registered marked sends; selected/foreign peers | Existing native writer, shared checked singleton, caller callback | TestDarwinManagedECNSendRoutes | Covered |
| Native batch engagement and ordinary fallback | Existing sendmsg_x owner | TestDarwinManagedECNNativeBatch; existing native progress/error tests | Covered on Darwin 25, no native skips |
| Full ordinary payloads, truncation and no read-ahead across release | Existing full-datagram OOB reader and endpoint lease | TestDarwinManagedECNLeaseDatagrams: empty, above ordinary QUIC-buffer boundary, 8192-byte datagrams, truncated caller buffer and queued handback | Covered |
| Singleton success, EMSGSIZE, EPERM, terminal/short writes and invalid callback results | Shared operation-scoped translator and native send owner | TestDarwinManagedECNSingletonResults; actual oversized native send; injected syscall-boundary results, EPERM called once | Covered |
| Opt-out and optional/fatal setup failures | Endpoint setup plus OOB setup receipt | TestDarwinManagedECNFallback; TestDarwinManagedECNSetupFailure | Covered |
| Missing/malformed metadata without stale ECN | Native parser and shared correlation owner | TestDarwinManagedECNMissingMetadata; existing ancillary/correlation tests | Covered |
| Revocation, reacquisition, terminal close and joined active reads | Existing endpoint generation/lifecycle owner | TestDarwinManagedECNLeaseDatagrams; TestDarwinManagedECNCloseJoinsRead; shared stale/concurrent-singleton tests | Covered |
| Managed capability projection and build selection | Shared adapter and Go build constraints | Existing projection/diagnostic tests; Darwin/FreeBSD/OpenBSD test-binary cross-builds, iOS library/opt-out builds, exactly one configureReceive and managedPacketRawFactory each; iOS selects the unsupported stub | Covered |

Ten new table-driven test functions were used. No correlation mutation was needed: that enforcement is unchanged and existing direct behavioral tests remain. No benchmark, timing campaign, extra platform qualification or recursive verification-aid closure was added. The one native package race gate and final certification bind to the reviewed product head in the product PR receipt. The product PR owns D1 completion and leaves only F1, O1 and W1 on the committed frontier; it creates no newly ready successor.

## Reproduction and certification

Run `go test . -run '^TestDarwinManagedECN' -count=1 -v` on a sendmsg_x-qualified Darwin host; native/family skips cannot certify this supported result. Preserve the existing native batch, ancillary, managed registration and shared adapter tests through `go test . -count=1`. Certification additionally runs the accepted native package race gate, `go vet .`, `go mod tidy -diff`, lint, diff checks and Darwin/FreeBSD/OpenBSD cross-builds. Exact head/base, commands and hosted run receipts belong in the product PR discussion, so this document does not predict a future merge SHA or journal receipt.
