# Isolated burst diagnosis evidence

Read the [protocol](../2026-09-06-d2-burst-protocol.md) and [result](../2026-09-06-d2-burst-results.md). These are bounded experimental artifacts, not a maintained benchmark framework or a runtime change. The previous paced evidence is retained unchanged on the parent branch.

Frozen source: baseline `5b2a33f146eaa268f0e10e59a6dd25c1a76a765d`; head-index `992efe2c371d9634baf39fcb371acc3eded2a5c7`. Their only differing source file is `datagram_queue.go`; [candidate.patch](candidate.patch) applies with `git apply --unidiff-zero`. [source-hashes.log](source-hashes.log) records `git archive` checksums and [binary-hashes.log](binary-hashes.log) records native normal/race binaries. The measured snapshots and binaries remain under `/home/josh/benchmarks/quic-go-fast/d2-burst-20260906` on minimax. All Git editing was performed in dedicated local worktrees.

## Reproduce analysis without rerunning measurements

The timing manifest has exactly 240 records. `profiles.jsonl.gz` separately records all 36 profiler invocations. `smoke/samples.jsonl.gz` records 12 short receiver race smokes; these are not timing evidence. Compressed manifests, traces, and host monitoring preserve their original bytes. Other exported text logs have trailing whitespace normalized. The analyzer validates order, source-mode accounting, IPC/pacer/expiry counts, successful exits, CPU partition receipts, and rate qualification without filtering samples.

```sh
python3 docs/audits/d2-burst/analyze.py docs/audits/d2-burst > /tmp/d2-burst-analysis.json
diff /tmp/d2-burst-analysis.json docs/audits/d2-burst/analysis.json
python3 docs/audits/d2-burst/plot.py docs/audits/d2-burst
```

Analysis uses the Python standard library; plotting uses Matplotlib. The image shows descriptive medians, while the report and JSON also contain paired estimates and bootstrap intervals. Internal-process CPU includes busy pacing; external-process CPU excludes the pacer but includes Unix IPC and common instrumentation. Cross-mode CPU ratios are not a queue optimization claim.

## Profiles and trace interpretation

Each of the 12 rate/mode/variant combinations has a CPU profile, block/mutex profiles, and a compressed raw execution trace. The four trace-derived `sched`, `sync`, `syscall`, and `net` profiles are included alongside readable `*.top.log` summaries. `profile-summary.json` records raw sample-count/duration totals and each diagnostic run's delivered/offered counts. Profiles may have different traffic/latency behavior from unprofiled timing samples. Low-rate external CPU profiles contain only five and six samples, insufficient for fine attribution.

`profiles.py` runs on the remote experiment root after collection and cleanup, using the frozen native binaries. It invokes `go tool trace -pprof=TYPE` and `go tool pprof -top/-raw`. To inspect a trace locally, decompress its `.trace.gz` into a scratch directory and use `go tool trace`. To regenerate all diagnostic summaries, restore the manifests and traces to the recorded remote layout, retain the frozen binaries, and run `python3 profiles.py /absolute/experiment/root`. Do not run the collector merely to regenerate analysis.

The Go 1.26 trace tool's `computePprofSched` measures time from a goroutine becoming runnable until it runs. It attributes that delay to the transition's stack, so `runtime.selectnbsend` beneath `HandleDatagramFrame` identifies a queue-notification wakeup site; the accumulated delay is not CPU time spent executing the send operation. Synchronization/net profiles include expected waiting for the next burst and testing/trace support goroutines. Aggregate profile duration can exceed wall duration and is not interchangeable with packet p99.

## Collection and host restoration

`collect.py` requires a fresh experiment root with `base.test`, `head-index.test`, and the frozen baseline scripts. `--smoke` additionally requires the two receiver race binaries. It refuses an existing manifest. The script temporarily creates the exact exclusive partition in `isolate.py` using sudo, runs samples as user josh with explicit affinity, monitors every CPU, and restores the partition in a finally block. The `root` partition preserves load balancing within the experimental CPU set. No persistent service, governor, IRQ, or boot settings change.

The [before](isolation-before.json) and [after](isolation-after.json) receipts show the reservation and return of all CPUs. Every sample has partition receipts before/after its measured process. `cpu-activity.log.gz` contains the complete host monitor across timing and profiles, and `precheck.log` is the all-core check just before timing. The observed selected/sibling cores had zero guest activity throughout. `host-toolchain.log` and `validation.log` complete the provenance. Further collection requires a new explicit investigation; the current result does not justify promoting head-index.

## Closeout source packaging

The retained Linux Go measurement tests require `-tags queue_experiment` and `QUEUE_EXPERIMENT=1`. The external pacer additionally requires `QUEUE_PACER=1`, as already supplied by the explicit collector invocation. The closeout adds the general opt-in guard to that subprocess entry point and groups its constants for repository formatting; the historical measured commits, tool hashes, samples and conclusions remain unchanged.

## Prerequisite for future collector reuse

The archived run restored all CPUs, as its receipts show. The original collector could bypass cleanup after receipt-writing or process-teardown failures. The subsequent [issue #18 repair and reuse contract](collector-reuse.md) protects the successful-setup lifetime and bounds tracked-process teardown. This repair does not authorize new collection; analysis reproduction from the saved artifacts does not invoke the collector. The historical receipts, hashes, samples and conclusions still describe the original run, not the repaired script.
