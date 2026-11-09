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

## Independent interoperability matrix

[R17 (#1037)](https://github.com/the-sarge/quic-go-fast/issues/1037) adds the fixed draft-21 QUIC v1 matrix on Linux amd64 and Go 1.27.3. `peer.c` is an application callback and UDP adapter linked to the same pinned picoquic library. Its commands call that library's APIs; picoquic owns its protocol parsing, protection, recovery and path state. The adapter can hold and replay one opaque UDP packet, map a client's existing path to a replacement socket, blackhole path zero, and impose a 1280-byte UDP payload ceiling on path one. It has no second QUIC implementation. The Go cases use the V1 config, transport, path and lifecycle APIs under the existing internal activation guard.

Each row runs once with quic-go-fast as client and once as server. Missing, duplicate, failed or skipped cases fail the aggregate claim. An unsupported peer operation stops qualification; changing the peer pin or substituting a self-to-self exchange requires disposition under the accepted contract.

| Scenario | Observed behavior |
| --- | --- |
| `useful-transfer` | Exact STREAM and DATAGRAM round trips, with useful sent and received payload recorded on paths zero and one. |
| `absent-negotiation` | A locally enabled V1 constructor falls back to an ordinary peer, reports no negotiated paths, refuses `OpenPathV1`, and transfers the fixed workload. |
| `path-zero-ack` | Literal ACK and PATH_ACK vectors both acknowledge packet zero; decoded 1-RTT events identify both forms and useful transfer continues. |
| `key-update-silent-path` | The independent peer starts a key update on path zero while path one sends and receives no packets, then useful data resumes on path one. |
| `status-backup-failover` | Backup/available status crosses the wire; useful traffic initially stays off the backup, then survives the original client's endpoint failure or path-zero packet loss through path one. |
| `abandon-reopen-late-feedback` | Path one retires, a new wire ID replaces it, and an encrypted peer packet held before retirement arrives after replacement while useful transfer continues. |
| `same-id-migration-rebinding` | A client calls `MigrateV1`, or the independent client undergoes NAT rebinding; wire ID and packet-number continuity survive a changed tuple without adding a third protocol path. |
| `unequal-mtu-invalid-frame` | Path-specific PMTU discovery converges across unequal UDP payload limits; a literal PATH_STATUS_BACKUP for a never-granted ID causes the expected protocol violation. |

The negotiated-zero gap is explicit: the pinned picoquic negotiator requires positive `initial_max_path_id` values and cannot act as the zero oracle. `TestMultipathNegotiatedZeroVectors` checks literal absent, zero and positive transport-parameter vectors in both roles, plus ACK/PATH_ACK(0) vectors through the existing wire parser. `TestMultipathNegotiatedZeroControlledPeer` checks a real self-to-self V1 exchange with both caps set to one, which advertises zero. These are separate normative and controlled evidence; they do not supply any of the 16 independent instances.

Freeze a clean candidate before building. Pass its commit and tree to Docker as `QGF_SOURCE_COMMIT` and `QGF_SOURCE_TREE`, and pass the resulting immutable image ID as `QGF_IMAGE_ID` to every container. The runner records those identities, the primary toolchain, kernel, both peer pins, driver and binary hashes, command, start time and duration. `/src/build-packages.txt` retains installed packages. Preserve the build log, image inspection and all per-case logs, including failed attempts. The scoped owner waiver changes cumulative ceilings, while the finite inventory, venues, accounting and per-invocation timeouts remain binding.

Run one case per invocation, with a fresh output directory. For example, this selects only the path-zero ACK case with quic-go-fast as server:

```sh
python3 -B interop/multipath/run.py --matrix --scenario path-zero-ack --role server --binary /usr/local/bin/qgf-r17-peer --test-binary /src/peer-matrix.test --output /results/path-zero-ack/server
```

After all 16 cases, run the separate zero evidence once, then validate the retained logs and receipts together:

```sh
python3 -B interop/multipath/run.py --zero-vectors --binary /usr/local/bin/qgf-r17-peer --test-binary /src/peer-matrix.test --output /results/negotiated-zero
python3 -B interop/multipath/run.py --validate-matrix --output /results
```

Each role context and the C adapter are bounded at 30 seconds; the Go test timeout remains 90 seconds, the runner remains 120 seconds, and each outer container remains 150 seconds. Build/setup has the inherited 1200-second bound. There is no longer monolithic matrix timeout. A failed operation, unavailable prerequisite or unresolved identity mismatch stops the invocation and retains its evidence; do not repeat until passing. Cancellation kills and joins the runner's process group, and Go cleanup cancels and joins its peer before returning the granted sockets.

This is example-level evidence for these fixed scenarios, pins and venue. It does not claim universal draft conformance, arbitrary peer support, all key-update directions, Internet failure timing or other platforms. The early ordinary and smoke receipts remain historical evidence. The transport integration suite owns this maintained fixture; retire it only with equivalent independent coverage or feature removal.

The required [independent peer workflow](../../.github/workflows/multipath-interop.yml) executes this fixed inventory on a GitHub-hosted Linux amd64 runner for a ready source PR, checking out its exact head. Draft pushes and documentation-only changes do not start the matrix. It records the actual hosted runner, kernel, native architecture, image and packages and rejects an unexpected self-hosted venue before peer launch. For R17 qualification, this job supplies the original 16 instances; do not run a duplicate manual matrix first. The workflow retains failed setup/case attempts and raw peer/Go JSON as an artifact and stops at the first failure.

## Native deadline qualification

[E6 (#1020)](https://github.com/the-sarge/quic-go-fast/issues/1020) runs six native classes on real UDP sockets, once each on Go 1.26.10 and 1.27.3: 12 instances. `native_qualification_test.go` in the root package is the fixture, under `//go:build nativequal && linux && amd64`, and `native_qualification.py` is its driver. The transport test suite owns both, and retirement requires equivalent replacement or feature removal.

Run accounting regressions with the discovery command above. Execution needs Linux amd64 with `strace`, `nft`, `ip`, `unshare` and passwordless `sudo`, and it charges the [program ledger](../../docs/adr/2026-10-24-multipath-reno-plan.md#program-qualification-ledger-and-sequencing). This command is one explicit invocation, not a per-push campaign, and it stops at the first failure:

```sh
python3 interop/multipath/native_qualification.py --candidate "$(git rev-parse HEAD)" --output /path/to/new/results
```

`EAGAIN-to-deadline`, `retry-fallback` and `queued-expiry` run under strace. Their timing is attributed separately and never compared to the 20 ms tolerance. The other three classes are uninstrumented and are compared to it. The GSO `EIO` in `retry-fallback` is simulated by a test wrapper, because this kernel gave no real trigger. Everything after it is native. See the [re-audit](../../docs/audits/2026-11-03-e6-native-qualification-reaudit.md) for the method and the receipt for the executed identities.
