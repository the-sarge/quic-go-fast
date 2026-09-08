# E5 probe emission and path handoff receipt

The concrete emission owner constructs, registers and disposes direct server responses and client alternate-transport probes. Direct writes retain their destinations, ECN and best-effort error behavior. Temporary bytes remain live through each synchronous write and are released afterward. Queued MTU probes check capacity before consuming probe intent, then use the existing registration and consuming queue handoff. Private path/MTU constructors release unreturned storage on key or append failures without refunding packet numbers.

The connection retains path selection, generation, MTU policy and worker failure handling. Queue replacement joins the old worker before publishing a new queue and starts that exact queue through the connection lifecycle. In-place rebinding retains the queue and its existing current-at-syscall destination behavior. The old connection registration forwarding method is removed. Close construction, initial queue startup/final shutdown and packer administration retain their existing boundaries for E6.

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

The runtime candidate is recorded in the product PR and the capture source manifest. Local full uncached tests, focused ownership/path/feedback race checks, vet, golangci-lint and gcassert passed. The opt-in allocation fixture initially omitted packet registration and tripped recovery's sequential-number assertion; it was corrected before qualification and is not a runtime failure. The paired base/candidate fixture includes real registration and ACK processing.

E5 requires P3 and focused probe/path allocations on the same native Linux toolchain, plus the existing ordinary/GSO allocation observations. The initial campaign uses ten alternating base/candidate pairs and the unchanged plan thresholds. The capture retains source/toolchain/binary hashes, payload/probe ledgers and CPU-isolation/restoration receipts. Performance results and exact-head local/hosted certification are required before merge; final results are recorded here or in the linked product PR discussion.
