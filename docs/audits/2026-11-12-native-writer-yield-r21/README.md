# R21 native writer-yield receipt

**Slice:** [R21 — Qualify native contention and shutdown with the delivered writer yield](../../adr/2026-10-24-multipath-reno-plan.md#r21) (`QGF-MP646-20261024-R21`, #1313). **Applicability audit:** [native writer-yield handoff](../2026-11-12-native-writer-yield-handoff.md). **Outcome:** four valid passes under unchanged bounds. This is supplemental, example-level native timing evidence for `request-contention` and `shutdown-join` on the delivered successful short software DATAGRAM-group yield. It is not full six-class qualification, a universal latency proof or negotiated multipath traffic evidence. It enables the preflight of [R19 #1039](https://github.com/the-sarge/quic-go-fast/issues/1039); it does not certify or activate the public profile.

## Candidate and source

The counted candidate is `4aa238ed644f325753d95f0b42ec892ac3c3472f` (tree `6535a62767dc2c2f7504658ead49a151bd172781`), staged on the venue as `git archive` SHA-256 `05a73ed6864f62f3e562918302824a349cf9b33aa315711e662d59e8400e7fc9`. Against the audited delivered source `08c6cf28420e78c75f69c7a7ee8268be615a3f88`, its only non-documentation differences are `native_qualification_test.go`, `interop/multipath/native_qualification.py` and `interop/multipath/test_native_qualification.py`. Production code is identical: no non-test `.go` file differs. Later commits on the PR add only this receipt and status documentation.

## Venue, toolchains and command

The venue is `minimax`: Linux amd64, Ubuntu 26.04.1 LTS, kernel `7.0.0-30-generic #30-Ubuntu SMP PREEMPT_DYNAMIC Fri Jul 31 18:22:54 UTC 2026`, boot ID `c0ea222a-3837-450f-8bbe-b1d45620cef8`, uid 1000. The process inherited affinity 0–31, with no GOMAXPROCS, GOGC or GOMEMLIMIT override. This matches the accepted native package venue. Load was 0.06/0.09/0.16 before and 0.20/0.12/0.17 after. CI runners, `transfer.test`, `gridcast.test`, qemu guests and `nativerig` were absent before and after, and no test process remained ([host.log](host.log)).

| Go | `internal/poll/fd_unix.go` SHA-256 | Test binary SHA-256 |
| --- | --- | --- |
| go1.26.11 | `689fb78bb32b8e810fa1dd2ded475a79574f24a8f40260af2d4a3273392bc2cc` | `7af70b2074bfeb9e9c034b0525897d4a6347307f9a4626117fe8901cbc2e6c86` |
| go1.27.4 | `675f74e8cbfc73170e08b43947c131706cd59e498d722555fe3b793618f5f162` | `0fbaea2684959f6569676df9a6f0b00cd6f258be2abc88977ee2e5eefb429701` |

The driver ran once from the extracted candidate as `python3 -I -B interop/multipath/native_qualification.py --output <run>/out --candidate 4aa238ed644f325753d95f0b42ec892ac3c3472f --profile writer-yield`. It built each binary with `CGO_ENABLED=0 GOFLAGS=-buildvcs=false go test -c -tags=nativequal`. Each instance used `-test.count=1 -test.timeout=30s -test.v`, one class per process, under the driver's 60-second subprocess bound. Neither case used strace or injection. Strace was 6.19 and present only as a prerequisite. The driver exited 0 and printed `{"profile": "writer-yield", "instances": 4, "status": "pass", "full_qualification": false}`. Its [results.json](results.json) holds the exact commands and per-instance load. The run directory on the venue is `/home/josh/diagnostics/quic-go-fast/issue-1313-r21-native-2026-11-12`. Its archive `r21-native-results.tar` has SHA-256 `d6bf62906d7bc5515841ba98dcdb5b9bf4b7346d34c5891025ead05467f99b78`, and its contents are reproduced here.

## Instances

All four records exist exactly once and passed on the first attempt. There were no failed attempts, reruns or warm-ups.

| Go | Case | Exit | Seconds | Record |
| --- | --- | ---: | ---: | --- |
| go1.26.11 | shutdown-join | 0 | 0.006 | [evidence](go1.26.11/shutdown-join/evidence.json), [log](go1.26.11/shutdown-join/test.log) |
| go1.26.11 | request-contention | 0 | 1.067 | [evidence](go1.26.11/request-contention/evidence.json), [log](go1.26.11/request-contention/test.log) |
| go1.27.4 | shutdown-join | 0 | 0.008 | [evidence](go1.27.4/shutdown-join/evidence.json), [log](go1.27.4/shutdown-join/test.log) |
| go1.27.4 | request-contention | 0 | 1.076 | [evidence](go1.27.4/request-contention/evidence.json), [log](go1.27.4/request-contention/test.log) |

### Retained timing

B = 100 ms, and every `timing_ms` field must stay at or below 20 ms. Join stays in `facts`, where the Go fixture asserts it is less than B + 20 ms.

| Go | public write return | native return overrun | publication | pressure native return overrun | pressure publication | client / server join |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| go1.26.11 | 0.059882 | 0 | 0.739235 | 0 | 1.286692 | 0.101240 / 0.104346 |
| go1.27.4 | 0.033883 | 0 | 0.432882 | 0 | 0.881963 | 0.206136 / 0.056325 |

All values are in milliseconds.

Contention assertions held on both versions:

- Admission refusals were positive: 16,825 and 16,869.
- Each lane made progress.
- Public and stateless writes all succeeded: 53/53 and 55/55 on Go 1.26.11, 63/63 and 65/65 on Go 1.27.4.
- The endpoint never faulted, after either phase.
- The receiver read 197,263 and 208,025 datagrams.

Peak queued requests were 57 and 56. There were zero expired and zero uncertain results. Pressure accepted 184,867 and 195,609 datagrams, with zero expired and zero uncertain.

### Reachability witnesses

A witness pairs three things for one request:

- a marked request sampled in the writer under the endpoint lock;
- the successful result its producer took (no member or reset error, every datagram accepted);
- the immutable completion fact for that request (no fault, whole-borrow return before deadline + 20 ms).

On Linux, those conjuncts are the writer's yield predicate at `native_endpoint.go:1388–1398`. No production counter, hook, scheduler override or observer build exists.

| Go | Phase | Geometry (members × datagrams, segment bytes) | Offered / taken successes | Sampled / witnessed | Smallest cutoff margin |
| --- | --- | --- | ---: | ---: | ---: |
| go1.26.11 | contention | 1 × 4, 10 B | 2,048 / 2,048 | 123 / 123 | 118.296 ms |
| go1.26.11 | pressure | 1 × 4, 300 B | 30,818 / 30,818 | 40 / 40 | 117.605 ms |
| go1.26.11 | shutdown activity | 32 × 4, 300 B | 5 / 1 | 2 / 1 | 119.670 ms |
| go1.27.4 | contention | 1 × 4, 10 B | 2,048 / 2,048 | 119 / 119 | 118.021 ms |
| go1.27.4 | pressure | 1 × 4, 300 B | 32,608 / 32,608 | 36 / 36 | 119.042 ms |
| go1.27.4 | shutdown activity | 32 × 4, 300 B | 5 / 1 | 2 / 1 | 119.640 ms |

### Shutdown preconditions

On each version, the client endpoint closed while a freshly claimed marked group was still in the writer. That group was first sampled after a witnessed success from the same bounded activity. Its published whole-borrow return came 0.014377 ms (Go 1.26.11) and 0.065663 ms (Go 1.27.4) after close began. Sealing revoked the rest, which is the disposition the contract retains: 36 accepted and 92 unsubmitted on Go 1.26.11, 19 accepted and 109 unsubmitted on Go 1.27.4. Neither had uncertain datagrams; both report `quic: native endpoint request revoked`. Both fact records show no fault, and the client endpoint never faulted.

The existing checks held on both versions:

- The pre-close join check returned `ErrMultipathNotStoppingV1`.
- Both joins finished under 120 ms.
- A post-close `WriteTo` was refused with `net.ErrClosed`.
- The caller's socket regained deadline and write authority: an expired deadline failed, a cleared deadline let a write through, and that datagram was read back.
- The bounded activity ended with the seal.

## Carried evidence and limits

The [applicability audit](../2026-11-12-native-writer-yield-handoff.md#native-evidence-applicability) carries `expired-before-dispatch`, `eagain-to-deadline`, `retry-fallback` and `queued-expiry` on both Go versions from the accepted native package. That package's source is `4e115954e7e54e747a9f3292d1561c6447c299c8`, its archive SHA-256 `c98bddda2ee6ab6d00a945b43d973b4eb28f83c458d80c54ab7126a425046248`. This candidate changes none of their paths in production code. The historical unmarked `request-contention` and drained `shutdown-join` observations remain historical context; this receipt supplements them for the yield.

The [R18 #1038 delivery receipt](../2026-11-12-multipath-r18-delivery-receipt.md) keeps all 36 ordinary-peer observations. Its CPU 1.10 and RSS 10% limits stand, along with the two exact owner-accepted CPU exceptions (DATAGRAM/batching-on 1.121686× and mixed-bulk 1.108585×); none is remeasured. R17 #1037's independent-peer evidence is carried from the [PR #1310 run](https://github.com/the-sarge/quic-go-fast/actions/runs/43319049861). R19 #1039 still owns its own activation tests and its exact-head hosted checks.

## Observation limitations

- Timing and scheduling evidence is example-level. It doesn't cover every close/yield interleaving or any particular instant inside `Gosched`.
- Both pressure phases observed zero sampled native poll waits (`pressure_stack_waits` 0 of 192 and 191 samples) and zero `SndbufErrors`. That is not evidence of kernel pressure traversal.
- `lane_0_accepted` and `pressure_accepted` count datagrams. Lane 0 carries four-datagram marked groups, so these values aren't comparable with historical request counts. Because lane 0's workload shape changed, `contention_ms` (57.1 and 66.8 ms) and `received` aren't compared with E6 either.
- The shutdown activity uses 32 members of four 300-byte datagrams per request. That is larger than a typical short group, chosen so the sampled borrow outlasts the sample-to-close interval. Geometry is recorded per record.
- The active-close selection can still lose the borrow between the final completion check and the close timestamp. The fixture fails closed there: it asserts a positive return-after-close interval rather than skipping. Both counted instances recorded a positive interval.
- Witness sampling uses the existing synchronized endpoint test seam: the contention-phase sampler every 100 µs, plus one endpoint-lock acquisition per 5 ms pressure sample. Witnessed counts are samples, not every marked request.
- The real scheduler's load sensitivity remains an observation limitation, not a reason to weaken thresholds.

## Local verification and review

- Driver unit suite (`python3 -B interop/multipath/test_native_qualification.py`): 25 tests passed. They include writer-yield instances that are missing, duplicated, extra or partial-claiming full; missing or invalid witness facts; and unchanged twelve-instance full accounting.
- `TestNativeQualMarkedWitness`, `TestNativeQualBlastKeepsPartialTiming` and `TestNativeQualRaiseKeepsMaximum` ran on minimax via `go test -tags nativequal`, using a held fake native IO in `synctest`, not UDP. Guard deletions of the witness cutoff check and the pairing check each failed their regression.
- One focused race run of `^(TestNativeSoftware|TestE21|TestNativeEndpoint|TestNativeShutdown|TestNativeClaimFact|TestNativeQualMarkedWitness|TestNativeQualRaiseKeepsMaximum)` passed (50 tests) on minimax. It was invoked twice, the second time only to count passes, on the pre-review fixture.
- Root package `go test -count=1 .` (darwin), `go vet .`, `GOOS=linux GOARCH=amd64 go vet -tags nativequal .`, `golangci-lint run --build-tags nativequal` (v2.14.0, linux) and `go mod tidy -diff` all passed.
- Review history is in PR #1315: the code-review preparation receipt, RAS review `20261113T000513-88116b5eca57aac7bfb73bd1` and its verification at `76dd2800c4333a56aaee457fdc20a7d65d71550c`. That review's partial-timing and driver-status findings were fixed before this candidate ran.

## Accounting

Six subjects were charged once each: two builds and four instances, with no failed attempts. The charge is 11.665979264 seconds of driver wall time. The cumulative ledger moves from 863 activities / 28620.89369800330706986 Host-seconds lower bound ([R18 final receipt](../2026-11-12-multipath-r18-final-receipt.md)) to **869 activities / 28632.55967726730706986 Host-seconds lower bound**, with no new cumulative cap ([ledger.json](ledger.json)). The venue run directory is retained, and no custody, process or session remains active.
