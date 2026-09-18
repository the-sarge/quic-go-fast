# Audit evidence and limits

Source is pinned to `07d8716ddd00b193f2c331d443a123c62a516cd6`; issue/PR state was retrieved on 2026-09-18. This document records source assessment and prior evidence, not a new reproduction campaign. [Report](README.md) · [Inventory](inventory.json) · [Current contract](../../agents/test-quality-357.md).

## Census and representation

A temporary Go 1.27.1 standard-library parser read Git-tracked Go files independent of host build selection. The finite selection was all `_test.go` files plus shared testutils, integration tools, certificate helpers, generated mock sources/inputs, FIPS/vendor fixtures, and external-packet-I/O testdata. It recorded declarations/source spans, hashes, build expressions, direct assertion sites, `Run` and range domains, same-package declaration candidates, and lexical concurrency/time/environment/random signals. Every parsed maintained declaration receives a seven-dimensional assessment and a deeper-inspection disposition. The source files own exact dynamic scenario membership; previews in JSON are intentionally shortened, with the full owning declaration span retained. These are scenario groups, not an invented count of runtime-expanded subtests.

The parser is authoritative for Go structure, not behavioral correctness or type resolution. Signal matching is a triage aid only: unknown assertion aliases, indirect calls, escaping callbacks, mutation of shared state and actual readiness require source inspection. `require.NoError` may protect setup rather than the intended behavior. A helper wrapper can have no direct assertions while enforcing its contract through another helper or gomock. A declared `synctest.Test` does not make code outside that bubble virtual; a helper without a lexical bubble may run inside its caller's bubble. Initial screening records such limits instead of labeling unflagged tests safe.

The temporary extractor, raw AST observations and JSON assembler were kept outside the repository under `/tmp/quic-test-quality-357/`; they are disposable verification aids, not a new maintained tool. Inventory hashes can be independently checked with Git at the pinned commit, and entrypoint/member accounting can be checked using the standard Go parser. No Go source was modified to build the inventory. No new product-test, fuzz, benchmark, causal, mutation or stress invocation was run for audit evidence.

Initial assessment accounting: 356 Go source files; 2550 maintained declarations; 2489 initial-screen, 31 source-inspected and 30 finding-linked entries. These statuses are mutually exclusive per declaration. Additional non-Go/module/data infrastructure files: 34. File classifications and per-kind counts are in the inventory.

### Exclusions

- Generated mock output is accounted by file/hash and generated provenance; its handwritten generator input and the expectations in maintained callers remain in scope. Generated method bodies are not independent behavioral tests.
- Thirteen root experiment fixtures are individually listed below. Their build tags and finite adoption/measurement contracts make them historical verification aids, including helpers outside `docs/audits/`. They are not silently reclassified as maintained tests or re-executed.
- The pinned `docs/audits/` subtree, object `ca7179b75ced3a406170921a0c94f7ff48b5ea82`, accounts for historical receipts, compressed logs, txt diagnostic sources, `d2-burst/test_collect.py` and its nested Go module. The archive index and maintained-code conventions govern preservation. New files in this report directory were not present at the audited source revision.
- Production implementation is read as needed to judge test contracts, not inventoried as a test or repaired. Ordinary example programs and interop runtime programs are not standalone test entrypoints; maintained Go tests in interop and explicitly invoked testdata/tooling are included.

- `connection_emission_experiment_test.go` — //go:build emission_experiment.
- `connection_probe_experiment_test.go` — //go:build emission_experiment.
- `d1_measurement_test.go` — //go:build darwin && !ios && !quic_go_no_private_syscalls && d1bench.
- `datagram_queue_external_pacer_experiment_test.go` — //go:build linux && queue_experiment.
- `datagram_queue_linux_experiment_test.go` — //go:build linux && queue_experiment.
- `datagram_queue_paced_experiment_test.go` — //go:build linux && queue_experiment.
- `gro_measurement_test.go` — //go:build linux && grobench.
- `send_queue_lifetime_bench_test.go` — //go:build queue_lifetime_experiment.
- `w1_measurement_test.go` — //go:build windows && w1bench.
- `w2_measurement_test.go` — //go:build windows && w2bench.
- `w3_measurement_sender_test.go` — //go:build linux && w3bench.
- `w3_measurement_shared_test.go` — //go:build w3bench.
- `w3_measurement_test.go` — //go:build windows && w3bench.

The source headers and corresponding [D1](../2026-09-12-d1-sendmsgx-protocol.md), [G2](../2026-09-11-g2-gro-protocol.md), [W1](../2026-09-11-w1-foundation-protocol.md), [W2](../2026-09-11-w2-uso-protocol.md), [W3](../2026-09-11-w3-uro-protocol.md), [queue](../2026-09-07-d2-evidence-closeout.md), [emission](../e1-emission/README.md) and [queue-lifetime](../e2-buffer-lifetimes/README.md) evidence distinguish these from maintained platform tests. See also [maintained-code conventions](../../agents/conventions.md).

## Bounded deeper inspections and positive coverage

This table defines what `source-inspected` means for the selected inventory entries. It is not a claim that an entire package has undergone full semantic verification. Other entries retain their explicit initial-screen/deferred status.

| Surface | Observed protection or limitation | Assessment boundary |
| --- | --- | --- |
| Root `quic_test.go` and #317/#319 server cleanup | Conn.run positive control starts an actual connection; the package guard recognizes current Conn and Transport names. ListenAddr fixture repair joins its receive completion. | Preserve these guards; a process-wide observation is not proof that every unrelated worker is owned. |
| `integrationtests/self/self_test.go`, `http3/http3_helper_test.go` | Shared UDP helpers register socket cleanup; TLS configs clone shared baselines; HTTP3 conn-pair setup registers connection cleanup. | Success-path mechanics inspected; full failure-path cleanup of every caller remains deferred. |
| `startHTTPServerWithCapture`, `newHTTP3Client` | Server helper closes the supplied socket and waits for Serve to return; client helper closes its transport. Capture remains a separately bounded helper. | A Serve-return join is not a blanket proof that every handler/callback is complete; F03 identifies specific worker contexts. |
| HTTP idle-timeout helper | Atomic latest-connection publication, body consumption and connection-context observation replace the old blocking channel handoff. Retry regression exists. | #314 is repaired; #151 original timeout and newer natural idle-expiry evidence remain distinct. |
| HTTP retry-after-idle fixture | Capacity-two publication matches an explicit two-socket scenario; assertions check remote-address change, cached-only failure, headers and exhausted socket list. | Do not mechanically replace every channel merely because the old capacity-one fixture failed. Additional unexpected retries remain a deeper question. |
| HTTP reconnection after a deliberate dial error | First sentinel is asserted; second response headers are checked; retained capture explicitly reports body not consumed. | The test's declared header boundary is not proof of response-body transfer; no invented body criterion added. |
| HTTP hotswap | Two servers share caller-owned listener; success path explicitly closes and joins ServeListener results, uses a fresh client, and asserts both bodies. A 20ms sleep is an observational assumption. | #188 diagnosis remains exhausted/unresolved; assertion-failure cleanup and retained capture require separately scoped work. |
| MTU discovery | Verified initial echo, bounded continuing traffic until tolerance/terminal observation, stream completion and connection close before final MTU/DATAGRAM sampling. Oversized count remains separate. | #178/#310 and #145 preserve observation consistency; #322 remains unresolved. |
| Blocked-data delivery | Real QUIC/TLS over simnet inside synctest; complete batch reads and receive-credit observations; fatal frame counts before offset indexing. | Exact counts are justified by the current declared controlled scenario. #320's old real-UDP fixture discussion needs reconciliation with #363. |
| Shared real-UDP proxy and its tests | Payload, direction, drop counts and timing are explicitly asserted; Close signals but does not join; client reader ownership omissions are concrete. | F01/F02. No claim that these observations diagnose an unrelated hosted timeout. |
| Simulator and event recorder | Link shutdown closes queues then waits for both link workers. Event recorder serializes append/snapshot acquisition and returns shallow event representations. | These are bounded helper observations; deep immutability of every qlog payload and recursive harness certification are not established. |
| Certificate fixtures and cipher selection | Two testdata certificate tests use ephemeral sockets, deadlines and server completion after accepted-socket close. Cipher-selection helper has a distinct accepted-socket omission. | #80 remains completed; F02 targets a different fixture. Testdata execution is a separate configured-mode gap. |
| Wire pool smoke tests | Get/put and foreign-buffer acceptance deliberately use non-panic completion; wrong-capacity behavior has a panic assertion. | Absence of explicit equality is not a demonstrated defect. Pool reset-state sensitivity beyond these smoke contracts is deferred. |
| Version/randomness/readiness/optional qlog | Exact owners and predicate/cleanup paths inspected as documented in F04–F07. | Source-confirmed mechanisms and evidence-backed risks are separated from unperformed reproductions. |

### Remaining source-family initial assessments

The per-declaration inventory records the actual oracle/scenario anchors. These summaries explain how initial observations were interpreted; they do not upgrade all rows to deeper-inspected status.

| Family | Meaning and observed oracle forms | Main deferred depth |
| --- | --- | --- |
| Wire, varint, protocol, qerr | Explicit encoded bytes/fields, malformed-input errors, length limits, string/error identity, boundary tables and round trips. Exact values can be compatibility contracts. | Full independent RFC/API comparison; round-trip agreement can mask paired encoder/decoder errors. |
| ACK/recovery/congestion/MTU | Explicit packet histories, ACK ranges, loss/PTO/ECN transitions, synthetic monotime inputs, congestion-window and pacing calculations. | Independence of expected-value arithmetic, random sample contracts, and all transition combinations. F05 isolates the sampled skip-period predicate. |
| Streams/framer/queues/flow control | Payload/offset/reliable-prefix checks, terminal errors, admission/window counts, real frame construction and targeted mocks; synctest is common for blocked operations. | Failure-path joins, all random operation schedules, expected-value coupling and precise global-state restoration. |
| Packet emission, packer, retained/coalesced storage | Reference counts, real packet data and recovery registration, queue handoff outcomes, retained-view release, charge transfer and partial-acceptance assertions. | Complete sensitivity for fork-modified branches and all lifetime transition families. Preserve the real-packer contract in the emission test plan. |
| Native/external/managed packet I/O | Platform-specific bytes and capabilities, permission/registration errors, kill-switch setup, progress counts, leased lifetime, deadline and close observations. | Native engagement on each supported host, admission/handback failures, capability skip effects; F06 identifies a precondition weakness. |
| HTTP3 unit and lifecycle | Header/body/stream state, request validation, replay compatibility, client pooling/eviction, qlog producer shutdown and listener ownership. | Every callback's failure path and in-flight upload/response combination. Exact error identities and existing ownership semantics remain binding. |
| Crypto/TLS/FIPS | Known vectors, key-phase transitions, decrypt/parse failures, tokens/tickets, context completion and subprocess transfer/key-update observations. | Cipher/platform/FIPS cross-products and statistical dependence; no cryptographic completeness claim. |
| Qlog, JSON and utilities | Serialized fields, event names, writer completion/close races, heap/ring behavior, error precedence and bounds. | Full event-payload lifetime, uncommon writer failures and recursive diagnostic-tool coverage. |
| Integration and interop | Real TLS/UDP or explicitly simulated transport, payload equality/EOF, protocol frame facts, cancellation, deadlines, migration and HTTP semantics. | Natural scheduling/loss effects, late callbacks, syscall completion and missing natural captures; no attribution from platform clustering. |
| Fuzzers, benchmarks and maintenance tools | Seed domains, bounded parser invariants, helper-owned behavioral checks, no-panic behavior, allocation/throughput measurements and subprocess hook assertions. | Continuous fuzzing engagement, benchmark equivalence and external-tool failures. A benchmark without equality is not automatically a faulty test. |

## Execution modes and gaps

| Configuration at the audited revision | Actual configured scope | Limits relevant to this audit |
| --- | --- | --- |
| `.github/workflows/unit.yml` | Ubuntu/Windows/macOS, Go 1.26.x/1.27.x; shuffled non-integration `./...`, Ubuntu race, Ubuntu privileged socket tests, selected FIPS140 handshake tests and benchmarks | Unit step deletes integrationtests in its job workspace. Testdata/dot directories and nested modules are not automatically included. |
| `.github/workflows/integration.yml` | Tools/proxy, version negotiation, nested FIPS; self tests v1/v2, Ubuntu race v1, Linux GSO-disabled and non-race ECN-disabled modes; integration benchmarks; retained HTTP/corruption evidence | TIMESCALE_FACTOR=3. Native capability availability and skips differ. Failed captures are evidence, not repair by themselves. |
| `.github/workflows/lint.yml` | Explicit `.githooks` tests, parent/FIPS module tidiness, go fix/generators, vendor fixture, compiler assertions, vulnerability check and multi-GOOS lint | Static/cross-target checks do not execute native behavior. No explicit invocation of the two certificate testdata test packages was found. |
| `.github/workflows/cross-compile.yml` | Supported build target matrix and Darwin private-syscall opt-out build | Build success does not test blocking I/O or socket options. |
| ClusterFuzzLite PR/batch/coverage workflows | `workflow_dispatch` only at this revision; automatic triggers commented out | The recorded reason is the upstream OSS-Fuzz Go-version mismatch. This audit did not verify current external availability or authorize re-enabling. |
| `.clusterfuzzlite/build.sh` | Seven named native fuzz adapters and seed extraction | Native fuzz inventory has ten entrypoints. Priority, JSON encoder and SNI fuzzers have native seeds but no listed adapter here. |
| `.github/workflows/codspeed.yml` | Manual dispatch only, awaiting a registered macro runner | Ordinary benchmark invocations still exist in unit/integration workflows; sustained performance qualification is separate. |
| `.github/scripts/http-capture.py` and in-test capture helpers | Bounded run output/provenance, checked file quotas and checksum finalization; failure tails and completeness reporting | Source identity can be qualified by untracked output; event capture is not a packet trace or total causal order. Existing capture contracts remain authoritative. |
| `testdata/externalpacketio`, generator inputs, cert data, nested vendor/FIPS modules | Explicit fixture and tooling boundaries, all enumerated | Runtime snippets, private fixture keys and generated output are not separate maintained test functions; no data bytes changed. |

## Prior work and live issue reconciliation

| References | Retrieved state / source reconciliation | Disposition |
| --- | --- | --- |
| #317 / #319 | Closed repair: ListenAddr joins its own transport receive loop; real-fixture regression and finite confirmation evidence retained. | Do not reopen the completed campaign or weaken process-wide observer. |
| #178 / #310 | Closed convergence-observation repair: keep verified traffic active until tolerance; finish before sampling. | Preserve numeric assertions. #322 is a later different assertion. |
| #314 / #315 | Closed retry handoff repair: atomic connection publication avoids capacity-one callback deadlock; retry regression retained. | Does not resolve original #151 timeout. |
| #145 | Merged deterministic loss corpus/controls and final MTU observation ordering repair. | Historical captured mechanism is bounded evidence; not an explanation of every #44 occurrence. |
| #44 | Open; informative natural failure evidence required. The 200-invocation campaign is exhausted; current waiting scope permits zero new test invocations. | Preserve waiting state and actual loss directions; no random-loss campaign. |
| #151 | Open; original five-second timeout unresolved. New [natural capture](https://github.com/the-sarge/quic-go-fast/issues/151#issuecomment-5704589413) shows the 30ms idle timer firing before handshake, with a different ~40ms application-error symptom. | Separate bounded lifecycle-contract decision needed; no common-root claim or retry. |
| #169 | Open; HTTP/0.9 initial RoundTrip timeout. [Current triage](https://github.com/the-sarge/quic-go-fast/issues/169#issuecomment-5631287666) requires natural evidence, with #168 cleanup/capture already installed. | Keep distinct from HTTP3 failures and completed ownership work. |
| #188 | Open; hotswap diagnosis non-reproducing and complete; proposed retained capture must include caller-owned listener and both endpoints. | No new experiment from unused conditional slots; #312 does not instrument this fixture. |
| #241 | Open; post-cancellation socket-rebind assertion, not a dial/DNS deadline. Bounded diagnosis complete, concrete bind/owner observations still needed. | Do not infer socket leak, runner fault or #317 root. |
| #301 | Open; second GET after deliberate dial failure, capture installed by #312; diagnosis complete without cause. | Await informative natural capture; #315 does not establish a fix. |
| #320 / #363 | #320 remains open with the old offset-300 real-UDP failure. At the audited source, merged #363 moved the fixture to simnet/synctest while retaining all byte, error, offset, count and bundling assertions. [Current fixture contract](../../agents/blocked-data-delivery.md). | Recommend current-state reconciliation; do not re-propose the implemented migration or retrospectively identify the exact old scheduling cause. |
| #322 | Open; oversized-packet count in MTU fixture still at line 228. Its failure passed earlier convergence assertions. | Existing separate finite diagnosis remains unapproved by this audit; no threshold relaxation. |
| #73 / #80 / #76 / #141 / #165 | Earlier repairs cover observable MTU/dial assertions, root/TLS/certificate cleanup, production qlog ownership and specific HTTP fixture completion/timing. | Read as bounded prior repairs; new findings name different remaining owners or predicates. |

Issue-body retrieval identities (UTC last-update timestamps and SHA-256 of retrieved body text; linked comments were also inspected where relevant). These are traceability metadata, not a guarantee that live issues will remain unchanged.

| Source | State | Updated at retrieval | Body SHA-256 |
| --- | --- | --- | --- |
| [#44](https://github.com/the-sarge/quic-go-fast/issues/44) | open | 2026-09-15T02:50:32Z | `181c78ab61bbacbe4a48d5b7dae1849f8e3ea22c5b0e5f3ca09139bd69feb465` |
| [#151](https://github.com/the-sarge/quic-go-fast/issues/151) | open | 2026-09-16T21:13:53Z | `f17af9c664ca5987f9253ecd5d3a504b3d08d2f73168caf985a6aa2a6f484089` |
| [#169](https://github.com/the-sarge/quic-go-fast/issues/169) | open | 2026-09-11T07:56:50Z | `a2bfe639c85a9032d1f642bf60ba1a0fcfaea429d64f5a72b1cf87f2a08db5d2` |
| [#188](https://github.com/the-sarge/quic-go-fast/issues/188) | open | 2026-09-15T02:50:26Z | `00ee87bed50b1d7cc344d1eede65cf16bb3b4d7fe008dfb44142882cfbd799ae` |
| [#241](https://github.com/the-sarge/quic-go-fast/issues/241) | open | 2026-09-15T02:50:23Z | `9837e5f36b2efa9892f4ee173d52a71b650da44b6d1b62015f925552dbcbe6a2` |
| [#301](https://github.com/the-sarge/quic-go-fast/issues/301) | open | 2026-09-15T02:50:29Z | `dd72e492664b5b4242b00017c5b7330a8f9fc5a97ce7b82055c9eee36c98b8bb` |
| [#317](https://github.com/the-sarge/quic-go-fast/issues/317) | closed | 2026-09-15T03:58:23Z | `e844139fef864b01eb632719cab00baea2e45dafec873fe312e9129bf36830b0` |
| [#178](https://github.com/the-sarge/quic-go-fast/issues/178) | closed | 2026-09-14T18:36:48Z | `d93803e8967616184076a285bfd5887ecd9cc40a19914490629cfa997ec18aa9` |
| [#314](https://github.com/the-sarge/quic-go-fast/issues/314) | closed | 2026-09-14T23:29:26Z | `74795254670e505c0dd601de94a6fde0ecfd8d514478a6a2c67da1e532af3396` |
| [#145](https://github.com/the-sarge/quic-go-fast/pull/145) | closed | 2026-09-09T23:09:06Z | `b007d38414723fb0b78c2dbe61936d1035b0f296d521a50727c516e4c33842b7` |
| [#320](https://github.com/the-sarge/quic-go-fast/issues/320) | open | 2026-09-15T03:47:35Z | `6c3e2642974c434115d08ca25d1cce81da0c8ae3a0983b5f5802f034077e0676` |
| [#322](https://github.com/the-sarge/quic-go-fast/issues/322) | open | 2026-09-15T03:57:39Z | `82e6997e3871389396430eadcd113889da8cac804f118bcd624ca63397c6400e` |
| [#363](https://github.com/the-sarge/quic-go-fast/pull/363) | closed | 2026-09-16T04:30:38Z | `d0bd89a87efa3df474aa95ad52f3d50f0f1c04ebb28fa335d7879bbded55b347` |
| [#73](https://github.com/the-sarge/quic-go-fast/issues/73) | closed | 2026-09-11T07:31:01Z | `d83db7a1a7880f46dab7c597f47b9862226f3cff60147ffa82897c1a7e0fe97e` |
| [#80](https://github.com/the-sarge/quic-go-fast/issues/80) | closed | 2026-09-11T05:01:32Z | `71067c05f1b05bf731ed2663691712ee78fa66ced5305181a96fd1774946bf24` |
| [#76](https://github.com/the-sarge/quic-go-fast/issues/76) | closed | 2026-09-11T00:50:09Z | `eb8bccc0b448a79dac353744cccd9b3628e130bd1736a4d06d8fc58a942bba83` |
| [#141](https://github.com/the-sarge/quic-go-fast/issues/141) | closed | 2026-09-09T17:01:37Z | `47a26b5de6a9fde7e35e291d6e669400fdba3675625a745eb2e90a9f0829a3d1` |
| [#165](https://github.com/the-sarge/quic-go-fast/issues/165) | closed | 2026-09-11T15:12:46Z | `985eac593d02253d774d055dece53a41a82a153cc0ee9034879fce28f26692cc` |

## Local evidence checks

The local checks reconcile the 327 tracked `_test.go` paths, every Go parser declaration in the selected finite domain, file hashes and declaration spans, all seven assessment dimensions/statuses, scenario/helper references, explicit frozen/generated exclusions, finding IDs, and added relative/pinned source links. They do not execute tests or certify semantic correctness. The publication PR holds exact pushed-head/base certification, RAS run IDs, independent dispositions, and hosted check receipts. Keeping those receipts outside this current source-assessment document avoids changing the audited snapshot to chase administrative SHAs.
