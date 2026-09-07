# E2 queued-buffer lifetime receipt

E2 establishes the queued-buffer ownership contract through the existing `sendQueue.Send`, `Run` and `Close` seam. Fatal writes release their current buffer before returning the original error; stopped submissions release their buffer; `Close` joins the worker and then releases any leftover entries. A submission that races worker exit can either enqueue or take the stopped branch; both paths terminate ownership by the end of `Close`. The sole producer must stop submitting before calling `Close`, as the connection shutdown and path-replacement callers already do. Queue capacity, successful write/release ordering, capacity wakeups, handshake feedback and socket ownership remain unchanged. No success-path lock, completion message, worker or credit ledger is added.

## Finite lifetime evidence

The supported domain is one connection-side producer, one asynchronous worker, the existing eight-entry channel and existing socket success/error outcomes. The queue centrally enforces the lifetime invariant in this domain. `packetBuffer` and the existing queue operations own the internal representation; this is not a new protocol parser or public API. Runtime releases and join/drain are required safety enforcement; tests and the opt-in benchmark are verification aids; this receipt and the decision/frontier edits are metadata. No verification aid is a maintained product dependency.

| Semantic case | Enforcement | Evidence |
| --- | --- | --- |
| Successful write | Worker releases after socket call | `TestSendQueueSendOnePacket` observes release |
| Full queue and resume | Existing capacity check and wakeup | `TestSendQueueBlocking` preserves full/resume behavior |
| Graceful drain | Worker sends queued entries; `Close` joins | `TestSendQueueBlocking` observes retained storage during blocked writes and all releases after close |
| Fatal active write | Worker releases before returning the original error | `TestSendQueueWriteError` failed with refcount 1 before the correction; passes afterward |
| Stopped submission and pending leftovers | `Send` consumes rejected storage; `Close` drains after join | `TestSendQueueStoppedSubmission` failed separately for the rejected buffer and pending buffers before each correction; passes afterward |
| Enqueue around worker exit | Producer finishes before `Close`; cleanup follows worker join | `TestSendQueueEnqueueAtWorkerExit` covers before, concurrent and after exit, including a blocked writer that prevents `Close` returning |

The regressions use the real queue with a controlled socket adapter and check existing buffer lifetime state after synchronization. Buffers used for lifetime assertions are allocated before release, so pool reuse cannot disguise the result. Focused queue and handshake-MTU race checks, the full Go suite and vet passed with Go 1.27.1 on macOS and native minimax Linux. Existing handshake eligibility and coalesced feedback tests preserve native message-size handling. No extra mutation or stress campaign was needed. These examples exercise the finite enforcement argument; they are not an exhaustive scheduling proof.

The call-site trace covers connection-loop shutdown and path replacement, both of which stop producing on that goroutine before `Close`. Direct probes, private packer construction failures, retained close bytes and receive-side refcounts remain outside E2 and retain their assigned later slices. The [adoption decision](../2026-09-07-emission-adoption-decision.md) accepts E1 uncertainty without claiming that this queue fix adopts the experimental extraction.

## Queue comparison

The build-tagged `send_queue_lifetime_bench_test.go` fixture measures real queue handoff/drain and capacity pressure with only socket I/O substituted. It is excluded from ordinary test and benchmark enumeration. Each variant uses the identical fixture, four physical cores, GOMAXPROCS=4 and native Go 1.27.1. Ten adjacent alternating pairs use one-second benchmark samples. The fixed analyzer uses geometric candidate/base time ratios and a one-sided 95% whole-pair bootstrap bound with 20000 resamples and seed 20260907; the accepted bound is 1.05, with no added success-path allocations.

The replacement completed all twenty subprocesses successfully on minimax. The benchmark ran on physical cores 8–11; SMT siblings 24–27 were reserved. Unrelated slices excluded both sets. The controller was explicitly pinned to core 0 and the monitor to core 1; every benchmark subprocess's observed affinity was 8–11. Temporary restrictions were restored after both attempts. The [capture archive](capture.tar.gz) retains logs, exact manifests, binary/fixture hashes, launcher/controller source, CPU observations and restoration. [Summary](summary.json) and [analyzer](analyze.py) are retained separately.

| Workload | Baseline ns/op | Candidate ns/op | Candidate/base time | One-sided 95% upper bound | Allocations/op, both | Result |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Handoff/drain | 283.916 | 285.608 | 1.0060 | 1.0118 | 0 | Pass |
| Capacity pressure | 118.519 | 118.456 | 0.9995 | 1.0044 | 0 | Pass |

Both variants reported 0 B/op and 0 allocs/op in every replacement sample. The measured candidate is `16a20ff6a210be891962fac2e23f669057b8b433`; the matched baseline fixture commit and exact candidate are bound by `bench-02/manifest.json`. Baseline production code is unchanged from `322c98991993736861f1d02e69d24497bf7376e6`. Subsequent metadata changes do not alter runtime or fixture bytes; exact final-head certification remains separate.

The first attempt stopped at the third invocation because the pressure fixture waited for availability once and then submitted without rechecking capacity. Availability is a wakeup hint, so this violated the existing `Send` precondition and caused the observed full-queue panic. The fixture was corrected to recheck in a loop, identically on baseline and candidate; production code did not change. The first failed attempt remains in `bench-01`, is excluded from replacement analysis, and is not represented as passing. The single permitted replacement is `bench-02`; no further comparison or broadened traffic campaign was run.

## Retained source and build binding

Review identified that the original archive named the synthetic baseline commit without retaining its source or build command. The [source bundle](source-binding.bundle) now retains both measured source revisions relative to declared base `322c98991993736861f1d02e69d24497bf7376e6`. The baseline differs from that base only by the identical opt-in benchmark fixture. The [build binding](build-binding.json) records both trees, changed files, fixture hashes, original build commands, toolchain/environment and embedded binary build information.

The [bounded binding script](bind-builds.py) rebuilt both retained revisions without running any benchmarks. Each rebuilt binary exactly matches the SHA-256 recorded in the pre-measurement manifest: baseline `43a7f809df5e3fd4cbb071b18e737bae65a48f5803f2c0345d17d3b4d3f30644`, candidate `9047ad53e760b18d297249f19682bbc4938db4fb7bed1f232fc712695baee6de`. This is retrospective evidence recovery, not a claim that these extra receipts existed before capture. It resolves source correspondence without changing or replacing samples. The source bundle can be verified in a clone containing the declared base with `git bundle verify source-binding.bundle`.

The [host observation](host-observation.json) summarizes the retained monitor's 48 complete intervals: zero measured-core guest CPU and maximum mean sibling busy time 0.061875%. The monitor was terminated after capture, leaving only the enclosing JSON footer absent; that footer was restored in memory to read the complete observations, and raw bytes remain unchanged in the archive.

## Review and certification

The declared budget is one initial review and at most one replacement under the shared review-loop baseline. The product PR owns the explicit adoption-decision publication, E2 completion and E3 frontier transition. Final review dispositions and exact-head local/hosted certification are recorded in the PR discussion; neither the measured runtime commit nor this receipt predicts a future certified head. E3 and later modes remain separate implementations.

The [review disposition](review.md) records the one accepted metadata correction. A further RAS review/verify was skipped under the shared docs-only correction policy; local source/hash/format checks and final-head certification apply to the corrected head.
