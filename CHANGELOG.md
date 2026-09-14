# Changelog

## v0.62.1-fast.1 — first fork prerelease

This is the first quic-go-fast prerelease for evaluation, based on upstream quic-go v0.62.0. It preserves the upstream module path, existing public APIs, and QUIC/HTTP/3 wire compatibility. It requires Go 1.26.0 or newer. Dependency versions are unchanged from the validated fork baseline; dependency and tool updates will be released separately.

### Changes relative to upstream v0.62.0

- Reduce DATAGRAM receive allocations by rejecting overflow before copying and removing an intermediate parser copy.
- Make copying for large stream writes grow linearly, while preserving admission and caller-buffer ownership.
- Add Linux coalesced receive, Windows segmented send/coalesced receive, and qualified macOS batch send with runtime fallbacks.
- Centralize packet-emission ownership and repair buffer/resource cleanup across receive queues, handshake setup, shutdown, and socket failures.
- Repair handshake MTU error recovery, repeated path probes, and concurrent send-path publication.
- Correct HTTP/3 exchange lifetimes, replacement-connection eviction, Extended CONNECT cancellation, and consumed DATAGRAM reference retention.
- Reduce optional tracing allocations, make tracing failures nonfatal, and report the selected fork version in qlog.
- Strengthen race, protocol-version, cleanup, and deterministic loss/corruption coverage; exclude archived audit data from Go module downloads.

The [README](README.md) describes the changes and their individual evidence. Performance measurements are scoped observations, not an aggregate benchmark of this release against upstream.

### Compatibility and validation boundaries

- Applications retain `github.com/quic-go/quic-go` imports and select the fork with an explicit main-module `replace` directive. A dependency's replacement does not propagate to its consumers. See [adoption instructions](README.md#use-the-fork).
- Tested CI platforms: Linux, macOS, and Windows on Go 1.26.x and 1.27.x for unit tests; Linux on both Go versions and macOS/Windows on Go 1.27.x for integration tests. Linux additionally runs race detection.
- Other targets accepted by the [cross-compilation script](.github/workflows/cross-compile.sh) are build-only. Android is excluded; iOS validation compiles library packages only. Cross-compilation does not establish runtime or datapath-offload support.
- This release ships library source, with no binary or container assets. Interop images are built in CI but are not published. No asset checksums are needed.
- macOS batching uses a qualified private syscall with fail-closed fallback and the `quic_go_no_private_syscalls` build-tag opt-out. Offload availability depends on the host; passing generic platform CI does not prove every offload is active.

### Known limitations

Intermittent macOS dial, initial-request, reconnection, and server-hotswap timeouts remain under investigation: [#151](https://github.com/the-sarge/quic-go-fast/issues/151), [#169](https://github.com/the-sarge/quic-go-fast/issues/169), [#188](https://github.com/the-sarge/quic-go-fast/issues/188), [#241](https://github.com/the-sarge/quic-go-fast/issues/241), and [#301](https://github.com/the-sarge/quic-go-fast/issues/301). The Linux path-MTU convergence assertion in [#178](https://github.com/the-sarge/quic-go-fast/issues/178) also remains unexplained. Passing runs do not establish infrastructure causality or resolve these failures.

The historical server-first packet-loss investigation [#44](https://github.com/the-sarge/quic-go-fast/issues/44) remains open. Bounded deterministic recovery tests do not promise successful recovery within a deadline under arbitrary loss patterns. Evaluate the prerelease against application workloads before adoption.
