# Independent peer control

This maintained fixture implements [R1 (#1021)](https://github.com/the-sarge/quic-go-fast/issues/1021): ordinary QUIC v1 transfer with quic-go-fast in each endpoint role. It does not advertise multipath or stand in for the later interoperability matrix. The transport integration suite owns the fixture; retire it only with equivalent independent coverage or feature removal.

The image pins picoquic `01e124ebdf9fc50df307e01e71f2744471ddc470`, its default non-AEGIS PicoTLS `bfa67875982afc4c24f21e146cef4747fa189c2f`, Go 1.27.3, the base image digest, and a Debian package snapshot. The build verifies both source revisions and retains installed package identities in `/src/build-packages.txt`. Record the resulting image ID and execution environment with every receipt. This follows the endpoint/build structure in [qoke's MIT-licensed reference](https://github.com/qoke/mp-quic-go/tree/084c13ea3834fbe58cedb63db9208951d714f839/interop/multipath); the ordinary driver and strict accounting are local implementations.

Run accounting regressions without starting a peer:

```sh
python3 -m unittest discover -s interop/multipath -p 'test_*.py'
```

Before operational execution, reserve the exact candidate, two smoke cases, build/setup cost, timeout and remaining capacity in the [program ledger](../../docs/adr/2026-10-24-multipath-reno-plan.md#candidate-and-run-ledger). Every failed attempt counts. Stop at the first unresolved prerequisite or budget failure; do not repeat until passing. The following commands are a single explicit invocation, not an automatic per-push campaign:

```sh
docker build --platform linux/amd64 -f interop/multipath/Dockerfile -t quic-go-fast-r1 .
docker create --platform linux/amd64 --network none --name quic-go-fast-r1-control quic-go-fast-r1 --output /results
docker start --attach quic-go-fast-r1-control
docker inspect quic-go-fast-r1-control --format '{{.State.ExitCode}}'
docker cp quic-go-fast-r1-control:/results ./r1-results
docker rm quic-go-fast-r1-control
```

Both `TestPicoquicOrdinaryControl/client` and `TestPicoquicOrdinaryControl/server` must execute and pass exactly once. The runner fails for unavailable prerequisites, a nonzero test exit, missing/duplicate cases, or skips. It retains Go JSON and each peer's output. Each role has a 20-second context, Go has a 90-second test timeout, and the outer runner has a 120-second timeout. Processes are canceled and joined before test cleanup finishes. TLS authentication uses a fresh local CA; workload bytes are fixed and compared in full at the receiver. The picoquic process receives no `-M` option. Ordinary public constructors remain unchanged.

The result is example-level evidence over this fixed scenario and toolchain, not universal protocol coverage or native endpoint qualification. Linux amd64 execution under emulation must be recorded as such. The negotiated-zero oracle gap remains for later normative vectors and a controlled peer; no multipath scenario is stubbed or reported as executed here. R14 and R17 own their later scenarios and any dedicated qualification workflow.

## Two-path smoke

[R14 (#1034)](https://github.com/the-sarge/quic-go-fast/issues/1034) adds one fixed two-path STREAM run per role: useful data on both paths, a stream abort and a close, with the private activation guard set by a `picoquic`-tagged root package test. Pass `--smoke` to the runner to select it. Both `TestPicoquicMultipathSmoke/client` and `TestPicoquicMultipathSmoke/server` must execute and pass exactly once. It is example-level evidence, charged to the program ledger, and [R17 (#1037)](https://github.com/the-sarge/quic-go-fast/issues/1037) owns the full matrix.

## Native deadline qualification

[E6 (#1020)](https://github.com/the-sarge/quic-go-fast/issues/1020) runs six native classes on real UDP sockets, once each on Go 1.26.10 and 1.27.3: 12 instances. `native_qualification_test.go` in the root package is the fixture, under `//go:build nativequal && linux && amd64`, and `native_qualification.py` is its driver. The transport test suite owns both, and retirement requires equivalent replacement or feature removal.

Run accounting regressions with the discovery command above. Execution needs Linux amd64 with `strace`, `nft`, `ip`, `unshare` and passwordless `sudo`, and it charges the [program ledger](../../docs/adr/2026-10-24-multipath-reno-plan.md#program-qualification-ledger-and-sequencing). This command is one explicit invocation, not a per-push campaign, and it stops at the first failure:

```sh
python3 interop/multipath/native_qualification.py --candidate "$(git rev-parse HEAD)" --output /path/to/new/results
```

`EAGAIN-to-deadline`, `retry-fallback` and `queued-expiry` run under strace. Their timing is attributed separately and never compared to the 20 ms tolerance. The other three classes are uninstrumented and are compared to it. The GSO `EIO` in `retry-fallback` is simulated by a test wrapper, because this kernel gave no real trigger. Everything after it is native. See the [re-audit](../../docs/audits/2026-11-03-e6-native-qualification-reaudit.md) for the method and the receipt for the executed identities.
