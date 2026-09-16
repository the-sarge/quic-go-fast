# Restricted Linux managed receive investigation

Issue [#379](https://github.com/the-sarge/quic-go-fast/issues/379) has a demonstrated usable restricted-socket case. The [current contract](../agents/linux-managed-fallback.md) bounds the resulting fallback to dual-family ECN `EPERM` with no required packet-info failure. This does not reopen R01-L qualification or claim deployment prevalence.

## Reproduction

Baseline source: `16f95f44e9c6987cff903ebb100d8fc0f0610779`. Environment: existing `wiremux-e01-l` VM, Linux `6.8.0-134-generic`, aarch64, Go `1.27.1`, unprivileged process, native UDP loopback. The fixture installs `PR_SET_NO_NEW_PRIVS` and a seccomp filter synchronized across Go runtime threads with `SECCOMP_FILTER_FLAG_TSYNC`. Only the two selected receive-option `setsockopt` operations return `EPERM`; ordinary socket creation, I/O, deadlines and other socket options remain permitted. Restrictions exist only in a disposable subprocess and its threads. No VM-wide policy is changed.

The original experiment used `Transport.NewManagedPacketEndpointV1("udp4", 127.0.0.1:0)`, acquired a factory lease and called `ConfigureManagedPacketIOV1`. A private fake was not used. A baseline `strace -f -e trace=setsockopt` capture recorded:

```text
setsockopt(6, SOL_IP, IP_RECVTOS, [1], 4) = -1 EPERM (Operation not permitted)
setsockopt(6, SOL_IPV6, IPV6_RECVTCLASS, [1], 4) = -1 EPERM (Operation not permitted)
```

Registration returned `activating ECN failed for both IPv4 and IPv6`. The same restricted process successfully exchanged a distinct datagram in both directions through the lease before registration and after the rejected registration, then through the parent after lease return and through a subsequent lease. No `UDP_GRO` activation was attempted. Closing the endpoint caused registration to return `use of closed network connection`.

The initial expectation of registration success failed against baseline production code with that exact ECN error. It passed after adding the internal error classification and managed fallback. The final focused fixture uses wildcard IPv4 binding to additionally exercise required packet-info handling and records four finite cases: ECN `EPERM` succeeds with ordinary reads; ECN `EINVAL`, packet-info `EPERM`, and simultaneous ECN/packet-info `EPERM` all retain fatal registration. All four run in separate restricted subprocesses. A native UDP close injected behind an otherwise active lease separately confirms fatal descriptor control and read/write failure; it is a fatal-path control, not the public reachability demonstration.

## Result and limits

The safe change retains the factory socket's existing ordinary datagram format, installs no normalizer and never attempts GRO after optional ECN denial. Managed diagnostics report `receive_mode=ordinary`, `coalescing=false`, `receive_eligible=true`, `receive_enabled=false` and `receive_disabled_reason=ancillary_setup_denied`. Bidirectional I/O remains usable after registration, transport close, lease return and subsequent lease acquisition/registration. Existing native decoder callers continue returning the historical setup error.

Packet-info fallback is deliberately not inferred from ordinary loopback usability: wildcard-address semantics warrant a separate contract. Errors other than the demonstrated ECN `EPERM` are not suppressed. The fixture supports native Linux arm64/amd64 only; seccomp installation failure or a skipped fixture is not qualifying native evidence. The observed baseline run and focused result ran without fixture skips. Buffer-size warnings reflected the VM's existing kernel limits and did not prevent I/O; no buffer policy was changed.

The maintained regression command is `go test -run '^TestManagedReceive(RestrictedSocket|ClosedSocket)$' -count=1 -v .`. The preservation gate is `CGO_ENABLED=1 go test -race . -run '^TestManaged|^TestExternalGRO' -count=1`, followed by affected-package tests/vet and module tidiness. Exact final candidate validation, review and hosted receipts are recorded in the product PR discussion. No performance, repeated sampling, new platform or fuzz campaign was performed.
