# E5 probe emission and path handoff receipt

The concrete emission owner constructs, registers and disposes direct server responses and client alternate-transport probes. Direct writes retain their destinations, ECN and best-effort error behavior. Temporary bytes remain live through each synchronous write and are released afterward. Queued MTU probes check capacity before consuming probe intent, then use the existing registration and consuming queue handoff. Private path/MTU constructors release unreturned storage on key or append failures without refunding packet numbers.

The connection retains path selection, generation, MTU policy and worker failure handling. The active send-connection slot is bound once alongside packer/recovery/queue slots; rebinding uses that slot rather than accepting an unrelated send connection from its caller. Queue replacement joins the old worker before publishing a new queue and starts that exact queue through the connection lifecycle. In-place rebinding retains the queue and its existing current-at-syscall destination behavior. The old connection registration forwarding method is removed. Close construction, initial queue startup/final shutdown and packer administration retain their existing boundaries for E6.

## Bounded ownership evidence

The supported domain is the two existing direct-probe paths, queued MTU probing and replacement/in-place migration. Existing wire/TLS and native socket code owns representation. Ownership is universal within these operations; tests and performance observations are example-level. Runtime extraction is shipped behavior and byte cleanup is safety enforcement. Tests/captures are verification aids; this receipt and plan transitions are metadata. No maintained verification framework is introduced.

| Semantic class | Owner and evidence |
| --- | --- |
| Path/MTU constructor key and append failures | Allocating constructor cleanup; `TestProbeConstructorFailureLifetime` first observed live storage in all four cases and now observes release and no returned buffer. |
| Server direct destination, packet info and retained qlog checksum | `TestEmissionDirectProbe/server` uses real packet construction/recovery, decodes PATH_CHALLENGE, accepts its ACK during I/O, retains checksum 42 and observes storage before/after the write. Existing path-validation regression first failed on unreleased direct storage. |
| Client alternate transport and direct write errors | `TestEmissionDirectProbe/client` observes the alternate transport's raw socket boundary; both direct operations preserve ignored write errors, avoid the ordinary queue and release temporary storage. |
| Queued MTU success, fatal write and capacity | `TestEmissionMTUProbe` checks a 1300-byte non-GSO output, pre-I/O registration, ACK-driven MTU progress and worker release on success/failure. `TestEmissionMTUProbeFullQueue` preserves pending probe intent. |
| Replacement with active and pending output | `TestEmissionPathReplacement` blocks the old syscall, observes that both buffers and the old queue stay live until drain, then verifies release and the replacement's shared feedback owner. |
| In-place migration and feedback preservation | `TestEmissionPathRebinding` verifies a pre-existing queued entry uses the new destination without replacing the queue. Existing real migration, native MTU classifiers and stale/current-generation handshake feedback tests remain preservation evidence. |

The plan's fifth and sixth closure rows are covered by these bounded regressions. No guard mutation is required. Path-validation algorithms, receive storage, retained-close storage, post-confirmation fallback changes and physical-link/application performance remain outside this slice. No public API, wire format, migration policy or generation semantics change.

## Validation and measurement binding

The final runtime candidate is `b88e7763b7d2afe6308e01df8b42edfe53e8c32a`, compared with parent `d9c6fc17c7b953d6bef8650b647f3f440b50feb5`. Local full uncached tests, focused ownership/path/feedback race checks, vet, golangci-lint and gcassert passed. The opt-in allocation fixture initially omitted packet registration and tripped recovery's sequential-number assertion; it was corrected before qualification and is not a runtime failure. The paired base/candidate fixture includes real registration and ACK processing.

E5 requires P3 and focused probe/path allocations on the same native Linux toolchain, plus the existing ordinary/GSO allocation observations. The initial campaign uses ten alternating base/candidate pairs and the unchanged plan thresholds. The capture retains source/toolchain/binary hashes, payload/probe ledgers and CPU-isolation/restoration receipts. Both performance campaigns are recorded below. Final exact-head local/hosted certification remains in the linked product PR discussion, separate from plan and mirror state.


## Initial qualification and revision

The initial runtime candidate is `64d3b0cb7dfa90bc633d0701531bdb6423851ee4`, compared with parent `d9c6fc17c7b953d6bef8650b647f3f440b50feb5` using native Linux Go 1.27.1. Full uncached `go test -count=1 ./...`, focused `go test -race . -run '^TestEmission|^Test.*Constructor.*Lifetime|^TestConnection(GSOBatch|SendQueue|ReceivePrioritization|PathValidation)|^TestHandshakeMTU|^TestSendQueue' -count=1`, and `go vet ./...` passed. macOS Go 1.27.0 full tests, focused races, vet, golangci-lint and gcassert also passed. Hosted compilation caught one fixture's nonportable `ifIndex` literal; test-only commit `388f14a8a5faba7b7cd1e7b3efb6a9e6675bbc9f` uses the portable address field, and Windows/OpenBSD test builds plus hosted checks pass. This correction changes no runtime or measured fixture.

The [initial P3 summary](initial-summary.json) records ten valid alternating base/candidate pairs. Goodput lower bound is 1.000143; CPU/unit upper bound is 1.004803; allocated bytes/unit upper bound is 0.999927; bad-probe upper difference is zero. Probe p99 fails the unchanged 1.05 margin: geometric mean ratio 1.046023 and one-sided 95% upper bound 1.070867. No payload, failed-probe, missed-probe or host-qualification failure occurred. This is failed performance qualification, not an infrastructure failure or a proven causal regression.

Identical-source focused observations show no added allocations: ordinary/GSO batches 37/104, path construction/registration 32, queued MTU 30 and cold path replacement 17. The initial connection value remains 1216 bytes. The later cold-path fixture addition `0c0c77a5018ce4cfab54c478cd8a96e8428d3537` changes no production source or P3 endpoint; its paired source/binary/launcher hashes are retained separately in the initial archive.

The complete [initial archive](initial-capture.tar.gz), SHA-256 `959a9fe3f020efc049d9b29bcea21a0b864cfac0192573f8d25b1dcb70fb7098`, retains raw ledgers, manifests, build logs, scripts, allocation observations, compiled-path diagnosis and restoration receipts. P3 uses four physical cores and GOMAXPROCS=4 per endpoint, active native GSO, QUIC v1, qlog off, 1071-byte DATAGRAMs at 4 Gbps, 2 s warmup, 60 s measurement and 1 s drain. Client/server cores are 8–11/12–15 with SMT siblings 24–31 reserved; controller and monitor use cores 0/1, and other work is restricted to 0–7 and 16–23. The qlog-on paired smoke is diagnostic. All phases restored CPU restrictions and removed their reservation/timer. The retained `build.py`, `phases.py`, `isolate.sh`, `collect.py` and `alloc.py` contain exact commands; the cold-path supplement uses `path-alloc.py`. Reproduce the fixed 20000-resample paired analysis with `python3 analyze.py <extracted campaign-01> --output summary.json`.

Initial review C-003 identified a pure rebinding passthrough. The implementing agent accepted it against E5's removal of legacy path access and bound the existing authoritative send slot once in runtime revision `b88e7763b7d2afe6308e01df8b42edfe53e8c32a`. The correction keeps path policy, the original connection-slot update sequence and worker lifetime intact, adds no shadow state and requires no contract re-audit. Other shape-only logging suggestions were rejected as unnecessary strengthening. Exact independent dispositions and review history remain in [PR #49](https://github.com/the-sarge/quic-go-fast/pull/49).

The compiled initial GSO loop retains its instruction count, with a changed policy-table load offset; the connection prelude is smaller. These observations establish no cause for the p99 miss. The one permitted replacement is declared for the in-contract active-slot revision, not a same-head rerun or discarded sample. It must retain the same workload, thresholds and sample budget, and its result is kept separate. The [source bundle](source-binding.bundle) retains the initial runtime, supplemental fixture and revised runtime against the parent. No third runtime campaign is authorized.


## Replacement qualification

The final runtime candidate `b88e7763b7d2afe6308e01df8b42edfe53e8c32a` passes the complete replacement P3 comparison against the same parent `d9c6fc17c7b953d6bef8650b647f3f440b50feb5`, on native Linux Go 1.27.1 with identical endpoint and focused-fixture sources. Native full uncached tests, focused races and vet passed before capture. The workload, core placement, toolchain, sample count, statistical method and thresholds are unchanged from the initial campaign.

The [replacement summary](summary.json) reports ten valid pairs and passing bounds: goodput lower bound 0.997506, CPU/unit upper bound 1.002985, allocated bytes/unit upper bound 1.000953, probe p99 upper bound 1.026829 and bad-probe upper difference zero. Probe p99 geometric mean ratio is 0.994770. There were no invalid/duplicate payloads, failed or missed probes, or host-qualification failures. The result qualifies this finite comparison; it does not establish why the initial p99 comparison failed or attribute the difference solely to slot binding.

Focused allocations remain 37/104 per ordinary/GSO batch, 32 per path construction/registration, 30 per queued MTU probe and 17 per cold path replacement on both variants. The candidate's bound send-slot pointer adds eight bytes to the connection value (1216 → 1224), with no new allocation. `testing.AllocsPerRun(1000)` temporarily uses one Go processor internally; these are allocation observations on a reserved four-core process placement, not endpoint latency measurements. P3 itself uses four Go processors per endpoint as declared.

The [replacement archive](replacement-capture.tar.gz), SHA-256 `8f4861c6faa518f7626f63e4fd971dbf4f167a651b5ad6c813d87f16c647594d`, retains every raw pair, exact source/toolchain/environment/fixture/binary binding, allocation output, qlog smoke and restoration receipt. Each replacement phase restored machine/system/user CPU restrictions and removed its reservation and restoration timer. No samples were pooled across candidates, dropped or replaced. The initial and replacement campaigns exhaust E5's accepted qualification budget.

The executed build/capture and analysis commands were:

```sh
python3 /home/josh/.cache/qgf-e5/build.py > /home/josh/.cache/qgf-e5/build.log 2>&1
python3 /home/josh/.cache/qgf-e5/phases.py > /home/josh/.cache/qgf-e5/phases.log 2>&1
python3 /home/josh/.cache/qgf-e5/path-alloc.py > /home/josh/.cache/qgf-e5/path-alloc.log 2>&1
python3 /home/josh/.cache/qgf-e5/analyze-e4.py /home/josh/.cache/qgf-e5/campaign-01 --output /home/josh/.cache/qgf-e5/summary.json
python3 /home/josh/.cache/qgf-e5-r2/build.py > /home/josh/.cache/qgf-e5-r2/build.log 2>&1
python3 /home/josh/.cache/qgf-e5-r2/phases.py > /home/josh/.cache/qgf-e5-r2/phases.log 2>&1
python3 /home/josh/.cache/qgf-e5-r2/analyze-e4.py /home/josh/.cache/qgf-e5-r2/campaign-01 --output /home/josh/.cache/qgf-e5-r2/summary.json
```

The scripts in each archive record the exact underlying build, test, CPU reservation and endpoint commands. Final metadata commits change neither the qualified runtime nor its measurement fixture. E5 has no deferred review follow-ups; retained close ownership and final interface contraction remain E6's work.
